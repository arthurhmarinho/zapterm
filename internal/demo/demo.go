// Package demo é um daemon falso, com contatos e conversas inventados, para
// tirar prints e gravar o README sem expor conversas de verdade. Não fala
// com o WhatsApp: responde ao protocolo da TUI com dados fixos.
package demo

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

const me = "5500000000000@s.whatsapp.net"

var demoNames = map[string]string{
	"5511900000011@s.whatsapp.net": "Lu",
	"5511900000001@s.whatsapp.net": "Marina Alves",
	"5511900000010@s.whatsapp.net": "Rafa",
}

type server struct {
	lastSent ipc.Request // para os testes conferirem o que a TUI mandou
	srv      *ipc.Server
	mu       sync.Mutex
	chats    []ipc.Chat
	msgs     map[string][]ipc.Message
}

// Serve abre o socket do modo demonstração e responde em segundo plano.
func Serve(path string) (*ipc.Server, error) {
	d := &server{}
	d.seed(time.Now())
	srv, err := ipc.Listen(path, d.handle, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return nil, err
	}
	d.srv = srv
	servers[srv] = d
	go srv.Serve()
	return srv, nil
}

func (d *server) handle(_ uint64, req ipc.Request) (any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch req.Op {
	case ipc.OpStatus:
		return ipc.Status{State: "online", Me: me, Name: "Você (demonstração)"}, nil
	case ipc.OpChats:
		return d.chats, nil
	case ipc.OpMessages:
		all := d.msgs[req.Chat]
		out := []ipc.Message{}
		for _, m := range all {
			if (req.Before == 0 && req.After == 0) || (req.Before > 0 && m.TS < req.Before) || (req.After > 0 && m.TS > req.After) {
				out = append(out, m)
			}
		}
		return out, nil
	case ipc.OpSearch:
		q := strings.ToLower(req.Text)
		hits := []ipc.SearchHit{}
		for _, c := range d.chats {
			if req.Chat != "" && c.JID != req.Chat {
				continue
			}
			for _, m := range d.msgs[c.JID] {
				if m.Kind == "text" && strings.Contains(strings.ToLower(m.Text), q) {
					// Do mais novo ao mais antigo, como o daemon.
					hits = append([]ipc.SearchHit{{Chat: c, Message: m}}, hits...)
				}
			}
		}
		return hits, nil
	case ipc.OpRead:
		for i := range d.chats {
			if d.chats[i].JID == req.Chat {
				d.chats[i].Unread = 0
				go d.srv.Broadcast(ipc.EvChat, d.chats[i])
			}
		}
		return nil, nil
	case ipc.OpSend:
		// Enviar funciona dentro da demonstração: a mensagem aparece com ✓✓.
		// Como o daemon real: "@<número>" marcado vira o nome, entre isolantes.
		text := req.Text
		for _, j := range req.Mentions {
			user, _, _ := strings.Cut(j, "@")
			text = strings.ReplaceAll(text, "@"+user, "@\u2068"+demoNames[j]+"\u2069")
		}
		m := ipc.Message{ID: time.Now().Format("150405.000"), Chat: req.Chat, Sender: me, SenderName: "você",
			FromMe: true, TS: time.Now().UnixMilli(), Kind: "text", Text: text, Status: 2}
		d.lastSent = req
		d.msgs[req.Chat] = append(d.msgs[req.Chat], m)
		for i := range d.chats {
			if d.chats[i].JID == req.Chat {
				d.chats[i].LastText, d.chats[i].LastTS, d.chats[i].LastStatus = "você: "+req.Text, m.TS, 2
				go d.srv.Broadcast(ipc.EvChat, d.chats[i])
			}
		}
		go d.srv.Broadcast(ipc.EvMessage, m)
		return m, nil
	case ipc.OpPollVote:
		d.lastSent = req
		return nil, nil
	case ipc.OpRefetch:
		return nil, nil // sem celular na demonstração
	case ipc.OpGallery:
		kinds := map[string]string{"fotos": "image", "videos": "video", "docs": "document", "audios": "audio"}
		page := ipc.GalleryPage{Items: []ipc.Message{}, Counts: map[string]int{}}
		for _, m := range d.msgs[req.Chat] {
			for tab, k := range kinds {
				if m.Kind == k {
					page.Counts[tab]++
				}
			}
			if strings.Contains(m.Text, "https://") && m.Kind == "text" {
				page.Counts["links"]++
				if req.Text == "links" {
					page.Items = append([]ipc.Message{m}, page.Items...)
				}
			}
			if m.Kind == kinds[req.Text] {
				page.Items = append([]ipc.Message{m}, page.Items...)
			}
		}
		return page, nil
	case ipc.OpProfile:
		for _, c := range d.chats {
			if c.JID != req.Chat {
				continue
			}
			p := ipc.Profile{JID: c.JID, Name: c.Name, Kind: c.Kind, Media: []ipc.Message{}}
			if c.Kind == "group" {
				p.About, p.MemberCount = "Trilhas leves aos sábados. Levar água!", 3
				p.Members = []ipc.ProfileMember{{Name: "Lu"}, {Name: "Marina Alves"}, {Name: "Rafa", Admin: true}}
			} else {
				p.Phone, p.About = "+"+strings.Split(c.JID, "@")[0], "Disponível"
			}
			return p, nil
		}
		return nil, errors.New("chat não encontrado")
	case ipc.OpChatAction:
		for i := range d.chats {
			c := &d.chats[i]
			if c.JID != req.Chat {
				continue
			}
			switch req.Text {
			case "pin", "unpin":
				c.Pinned = req.Text == "pin"
			case "archive", "unarchive":
				c.Archived = req.Text == "archive"
			case "unmute":
				c.Muted = false
			case "mute8h", "mute1w", "muteforever":
				c.Muted = true
			case "read":
				c.Unread = 0
			}
			go d.srv.Broadcast(ipc.EvChat, *c)
		}
		return nil, nil
	case ipc.OpMembers:
		if req.Chat == "120000000000000001@g.us" {
			return []ipc.Member{
				{JID: "5511900000011@s.whatsapp.net", Name: "Lu"},
				{JID: "5511900000001@s.whatsapp.net", Name: "Marina Alves"},
				{JID: "5511900000010@s.whatsapp.net", Name: "Rafa"},
			}, nil
		}
		return []ipc.Member{}, nil
	case ipc.OpViewing, ipc.OpTyping, ipc.OpAvatar:
		return nil, nil // sem foto: a TUI desenha as iniciais
	case ipc.OpStickers:
		return ipc.StickerPage{Stickers: []ipc.Sticker{}, Next: -1}, nil
	}
	return nil, errors.New("indisponível no modo demonstração")
}

func (d *server) seed(now time.Time) {
	at := func(minAgo int) int64 { return now.Add(-time.Duration(minAgo) * time.Minute).UnixMilli() }
	d.chats = []ipc.Chat{
		{JID: "5511900000001@s.whatsapp.net", Name: "Marina Alves", Kind: "dm", Pinned: true, LastTS: at(3), LastText: "você: fechado, te vejo lá!", LastStatus: 3},
		{JID: "120000000000000001@g.us", Name: "Trilha de sábado 🥾", Kind: "group", IsGroup: true, LastTS: at(9), LastText: "Rafa: alguém leva protetor?", Unread: 4, LastStatus: -1},
		{JID: "5511900000002@s.whatsapp.net", Name: "Diego Rocha", Kind: "dm", LastTS: at(42), LastText: "[áudio 0:18]", Unread: 1, LastStatus: -1},
		{JID: "120000000000000002@g.us", Name: "Família", Kind: "group", IsGroup: true, Muted: true, LastTS: at(95), LastText: "Mãe: [imagem] olha o bolo", Unread: 12, LastStatus: -1},
		{JID: "5511900000003@s.whatsapp.net", Name: "Bia Costa", Kind: "dm", LastTS: at(60 * 20), LastText: "você: valeu demais!", LastStatus: 2},
		{JID: "120000000000000003@g.us", Name: "Clube do livro 📚", Kind: "group", IsGroup: true, LastTS: at(60 * 26), LastText: "Lu: próximo é Duna", LastStatus: -1},
		{JID: "5511900000004@s.whatsapp.net", Name: "Oficina do Zé", Kind: "dm", LastTS: at(60 * 50), LastText: "[documento: orcamento.pdf]", LastStatus: -1},
		{JID: "120000000000000004@newsletter", Name: "Notícias do Bairro", Kind: "channel", LastTS: at(60 * 3), LastText: "Feira de domingo confirmada", Unread: 2, LastStatus: -1},
		{JID: "120000000000000005@g.us", Name: "Vendas e trocas", Kind: "group", IsGroup: true, Archived: true, Muted: true, LastTS: at(30), LastText: "[imagem] bicicleta aro 29", Unread: 31, LastStatus: -1},
	}

	marina, trilha := d.chats[0].JID, d.chats[1].JID
	rafa, lu := "5511900000010@s.whatsapp.net", "5511900000011@s.whatsapp.net"
	d.msgs = map[string][]ipc.Message{
		marina: {
			{ID: "m1", Chat: marina, Sender: marina, SenderName: "Marina Alves", TS: at(60 * 25), Kind: "text", Text: "Oi! Você vai na *exposição* amanhã? A programação tá em https://museu.exemplo.org/agenda"},
			{ID: "m2", Chat: marina, Sender: me, SenderName: "você", FromMe: true, TS: at(60 * 24), Kind: "text", Text: "Vou sim! Que horas você chega?", Status: 3},
			{ID: "m3", Chat: marina, Sender: marina, SenderName: "Marina Alves", TS: at(20), Kind: "text", Text: "Umas 15h. Dá pra gente almoçar antes",
				Reactions: []ipc.Reaction{{Emoji: "👍", Count: 1, Mine: true}}},
			{ID: "m4", Chat: marina, Sender: me, SenderName: "você", FromMe: true, TS: at(15), Kind: "text", Text: "Perfeito, conheço um lugar _ótimo_ perto do museu", Edited: true, Status: 3},
			{ID: "m5", Chat: marina, Sender: marina, SenderName: "Marina Alves", TS: at(8), Kind: "revoked"},
			{ID: "m6", Chat: marina, Sender: marina, SenderName: "Marina Alves", TS: at(5), Kind: "text", Text: "Manda o endereço?"},
			{ID: "m7", Chat: marina, Sender: me, SenderName: "você", FromMe: true, TS: at(3), Kind: "text", Text: "fechado, te vejo lá!", Status: 3,
				Quote: &ipc.Quote{ID: "m6", Sender: marina, SenderName: "Marina Alves", Text: "Manda o endereço?"}},
		},
		trilha: {
			{ID: "t1", Chat: trilha, Sender: rafa, SenderName: "Rafa", TS: at(70), Kind: "text", Text: "Bora confirmar quem vai sábado?"},
			{ID: "t2", Chat: trilha, Sender: lu, SenderName: "Lu", TS: at(65), Kind: "text", Text: "Eu vou! Levo o lanche",
				Reactions: []ipc.Reaction{{Emoji: "❤️", Count: 3}, {Emoji: "😂", Count: 1}}},
			{ID: "t3", Chat: trilha, Sender: me, SenderName: "você", FromMe: true, TS: at(40), Kind: "text", Text: "Tô dentro, saio às 6h", Status: 2},
			{ID: "t4", Chat: trilha, Sender: rafa, SenderName: "Rafa", TS: at(12), Kind: "audio", Text: "[áudio 0:24]", HasRaw: true},
			{ID: "t5", Chat: trilha, Sender: rafa, SenderName: "Rafa", TS: at(11), Kind: "other", Text: "[evento: Trilha do Pico]",
				Card: &ipc.Card{Type: "event", Title: "Trilha do Pico", Lines: []string{"🕒 sáb, 22/11 às 06:00", "📍 Portaria do parque", "Levem água e lanche"}}},
			{ID: "t6", Chat: trilha, Sender: lu, SenderName: "Lu", TS: at(10), Kind: "other", Text: "[enquete: Carona?]",
				Card: &ipc.Card{Type: "poll", Title: "Quem precisa de carona?", Voters: 3, Options: []ipc.CardOption{
					{Name: "Eu", Votes: 1}, {Name: "Vou de ônibus", Votes: 0}, {Name: "Tenho carro", Votes: 2}}}},
			{ID: "t7", Chat: trilha, Sender: lu, SenderName: "Lu", TS: at(10), Kind: "other", Text: "[localização]", Forwarded: true,
				Card: &ipc.Card{Type: "location", Title: "Portaria do parque", Lines: []string{"Estrada do Pico, km 3"},
					URL: "https://www.openstreetmap.org/?mlat=-5.8&mlon=-35.2#map=17/-5.8/-35.2"}},
			{ID: "t8", Chat: trilha, Sender: rafa, SenderName: "Rafa", TS: at(9), Kind: "text", Text: "levar:\n* protetor\n* boné\n> alguém leva protetor?"},
		},
	}
}

// LastSent é o último pedido de envio recebido (para testes).
func LastSent(srv *ipc.Server) ipc.Request { return servers[srv].lastSent }

var servers = map[*ipc.Server]*server{}
