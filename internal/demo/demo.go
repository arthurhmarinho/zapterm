// Package demo é um daemon falso, com contatos e conversas inventados, para
// tirar prints e gravar o README sem expor conversas de verdade. Não fala
// com o WhatsApp: responde ao protocolo da TUI com dados fixos.
package demo

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

const me = "5500000000000@s.whatsapp.net"

type server struct {
	srv   *ipc.Server
	mu    sync.Mutex
	chats []ipc.Chat
	msgs  map[string][]ipc.Message
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
		if req.Before > 0 {
			return []ipc.Message{}, nil
		}
		return d.msgs[req.Chat], nil
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
		m := ipc.Message{ID: time.Now().Format("150405.000"), Chat: req.Chat, Sender: me, SenderName: "você",
			FromMe: true, TS: time.Now().UnixMilli(), Kind: "text", Text: req.Text, Status: 2}
		d.msgs[req.Chat] = append(d.msgs[req.Chat], m)
		for i := range d.chats {
			if d.chats[i].JID == req.Chat {
				d.chats[i].LastText, d.chats[i].LastTS, d.chats[i].LastStatus = "você: "+req.Text, m.TS, 2
				go d.srv.Broadcast(ipc.EvChat, d.chats[i])
			}
		}
		go d.srv.Broadcast(ipc.EvMessage, m)
		return m, nil
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
			{ID: "m1", Chat: marina, Sender: marina, SenderName: "Marina Alves", TS: at(60 * 25), Kind: "text", Text: "Oi! Você vai na exposição amanhã?"},
			{ID: "m2", Chat: marina, Sender: me, SenderName: "você", FromMe: true, TS: at(60 * 24), Kind: "text", Text: "Vou sim! Que horas você chega?", Status: 3},
			{ID: "m3", Chat: marina, Sender: marina, SenderName: "Marina Alves", TS: at(20), Kind: "text", Text: "Umas 15h. Dá pra gente almoçar antes",
				Reactions: []ipc.Reaction{{Emoji: "👍", Count: 1, Mine: true}}},
			{ID: "m4", Chat: marina, Sender: me, SenderName: "você", FromMe: true, TS: at(15), Kind: "text", Text: "Perfeito, conheço um lugar ótimo perto do museu", Edited: true, Status: 3},
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
			{ID: "t5", Chat: trilha, Sender: rafa, SenderName: "Rafa", TS: at(9), Kind: "text", Text: "alguém leva protetor?"},
		},
	}
}
