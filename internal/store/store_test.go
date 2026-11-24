package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

func TestSaveMessageOrdemEDuplicata(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const g = "123@g.us"
	novo := ipc.Message{ID: "b", Chat: g, Sender: "1@s.whatsapp.net", SenderName: "Ana", TS: 2000, Kind: "text", Text: "nova"}
	velho := ipc.Message{ID: "a", Chat: g, Sender: "2@s.whatsapp.net", SenderName: "Bia", TS: 1000, Kind: "text", Text: "velha"}

	if ok, _ := s.SaveMessage(novo, nil, true, true); !ok {
		t.Fatal("primeira gravação deveria ser nova")
	}
	// Chega fora de ordem, como no history sync: não pode roubar a prévia.
	if _, err := s.SaveMessage(velho, nil, true, false); err != nil {
		t.Fatal(err)
	}
	// Duplicata não conta não lida de novo.
	if ok, _ := s.SaveMessage(novo, nil, true, true); ok {
		t.Fatal("duplicata gravada como nova")
	}

	c, err := s.Chat(g)
	if err != nil {
		t.Fatal(err)
	}
	if c.LastText != "Ana: nova" || c.LastTS != 2000 || c.Unread != 1 {
		t.Fatalf("chat inesperado: %+v", c)
	}

	msgs, _ := s.Messages(g, 10, 0)
	if len(msgs) != 2 || msgs[0].ID != "a" || msgs[1].ID != "b" {
		t.Fatalf("ordem cronológica errada: %+v", msgs)
	}
	antes, _ := s.Messages(g, 10, 2000)
	if len(antes) != 1 || antes[0].ID != "a" {
		t.Fatalf("paginação por before falhou: %+v", antes)
	}

	_ = s.MarkRead(g)
	if c, _ := s.Chat(g); c.Unread != 0 {
		t.Fatal("MarkRead não zerou")
	}
}

func TestCitacaoERaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := ipc.Message{ID: "r", Chat: "1@s.whatsapp.net", Sender: "1@s.whatsapp.net", TS: 1, Kind: "text", Text: "sim",
		Quote: &ipc.Quote{ID: "q", Sender: "2@s.whatsapp.net", Text: "vamos?"}}
	if _, err := s.SaveMessage(m, []byte{1, 2, 3}, false, false); err != nil {
		t.Fatal(err)
	}
	got, raw, err := s.Message(m.Chat, "r")
	if err != nil || got.Quote == nil || got.Quote.Text != "vamos?" || len(raw) != 3 {
		t.Fatalf("citação/raw perdidos: %+v %v %v", got, raw, err)
	}
	list, _ := s.Messages(m.Chat, 10, 0)
	if len(list) != 1 || list[0].Quote == nil || list[0].Quote.ID != "q" {
		t.Fatalf("Messages sem citação: %+v", list)
	}
	// Reabrir não pode rodar a migração de novo (ALTER TABLE falharia).
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	s2.Close()
}

func TestEdicaoRemocaoReacoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const c = "1@s.whatsapp.net"
	s.SaveMessage(ipc.Message{ID: "a", Chat: c, Sender: c, TS: 1, Kind: "text", Text: "oi"}, nil, false, false)
	s.SaveMessage(ipc.Message{ID: "b", Chat: c, Sender: c, TS: 2, Kind: "text", Text: "tudo bem?"}, nil, false, false)

	// Editar a última muda a prévia; editar a anterior não.
	if ok, _ := s.UpdateMessage(c, "b", "text", "tudo certo?", true); !ok {
		t.Fatal("não achou a mensagem")
	}
	if ch, _ := s.Chat(c); ch.LastText != "tudo certo?" {
		t.Fatalf("prévia: %q", ch.LastText)
	}
	s.UpdateMessage(c, "a", "revoked", "", false)
	if ch, _ := s.Chat(c); ch.LastText != "tudo certo?" {
		t.Fatalf("apagar a anterior mexeu na prévia: %q", ch.LastText)
	}
	msgs, _ := s.Messages(c, 10, 0)
	if msgs[0].Kind != "revoked" || !msgs[1].Edited || msgs[1].Text != "tudo certo?" {
		t.Fatalf("mensagens: %+v", msgs)
	}
	if ok, _ := s.UpdateMessage(c, "nao-existe", "text", "x", true); ok {
		t.Fatal("atualizou mensagem inexistente")
	}

	// Reações: uma por pessoa; trocar substitui; vazio remove.
	s.SetReaction(c, "b", "eu", "❤️", 10)
	s.SetReaction(c, "b", "ana", "❤️", 11)
	s.SetReaction(c, "b", "bia", "😂", 12)
	s.SetReaction(c, "b", "bia", "👍", 13)
	s.SetReaction(c, "b", "ana", "", 14)
	rs, err := s.Reactions(c, []string{"a", "b"}, "eu")
	if err != nil {
		t.Fatal(err)
	}
	got := rs["b"]
	if len(got) != 2 || got[0].Emoji != "❤️" || got[0].Count != 1 || !got[0].Mine || got[1].Emoji != "👍" || got[1].Mine {
		t.Fatalf("reações: %+v", got)
	}
	// Reação antiga chegando atrasada não sobrescreve a nova.
	s.SetReaction(c, "b", "bia", "😢", 5)
	if rs, _ := s.Reactions(c, []string{"b"}, "eu"); rs["b"][1].Emoji != "👍" {
		t.Fatalf("reação velha venceu: %+v", rs["b"])
	}
}

func TestStatusSoSobe(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const c = "1@s.whatsapp.net"
	s.SaveMessage(ipc.Message{ID: "m", Chat: c, Sender: "eu", FromMe: true, TS: 5, Kind: "text", Text: "oi"}, nil, false, false)
	s.SetStatus(c, []string{"m"}, 3)
	s.SetStatus(c, []string{"m"}, 2) // entregue atrasado
	msgs, _ := s.Messages(c, 10, 0)
	if msgs[0].Status != 3 {
		t.Fatalf("status %d", msgs[0].Status)
	}
	if ch, _ := s.Chat(c); ch.LastStatus != 3 {
		t.Fatalf("LastStatus %d", ch.LastStatus)
	}
	s.SaveMessage(ipc.Message{ID: "n", Chat: c, Sender: c, TS: 6, Kind: "text", Text: "opa"}, nil, false, false)
	if ch, _ := s.Chat(c); ch.LastStatus != -1 {
		t.Fatalf("última não é sua, LastStatus %d", ch.LastStatus)
	}
}

func TestFavoritas(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.PutFavorite(FavoriteSticker{Key: "a", DirectPath: "/a", MediaKey: []byte{1}, TS: 10})
	s.PutFavorite(FavoriteSticker{Key: "b", DirectPath: "/b", MediaKey: []byte{2}, TS: 20})
	s.PutFavorite(FavoriteSticker{Key: "a", DirectPath: "/a2", MediaKey: []byte{1}, TS: 5}) // atualiza, sem regredir ts
	favs, _ := s.Favorites()
	if len(favs) != 2 || favs[0].Key != "b" || favs[1].DirectPath != "/a2" || favs[1].TS != 10 {
		t.Fatalf("favoritas: %+v", favs)
	}
	s.RemoveFavorite("b")
	if favs, _ := s.Favorites(); len(favs) != 1 {
		t.Fatalf("remoção: %+v", favs)
	}
	// Expirada some da lista; favoritada de novo (link novo) volta.
	s.MarkFavoriteGone("a")
	if favs, _ := s.Favorites(); len(favs) != 1 || !favs[0].Gone {
		t.Fatalf("expirada não marcada: %+v", favs)
	}
	s.PutFavorite(FavoriteSticker{Key: "a", DirectPath: "/a3", MediaKey: []byte{1}, TS: 30})
	if favs, _ := s.Favorites(); favs[0].Gone {
		t.Fatal("favoritar de novo (link novo) não tirou a marca")
	}
}

func TestBuscaEDepois(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const c = "1@s.whatsapp.net"
	for i, txt := range []string{"bom dia", "Não sei", "nao vou", "tchau"} {
		s.SaveMessage(ipc.Message{ID: string(rune('a' + i)), Chat: c, Sender: c, TS: int64(i + 1), Kind: "text", Text: txt}, nil, false, false)
	}
	hits, _ := s.SearchMessages(SearchFilter{}, func(t string) bool { return strings.Contains(strings.ToLower(t), "na") }, 10)
	if len(hits) != 1 || hits[0].ID != "c" { // ToLower não tira acento: só "nao vou"
		t.Fatalf("busca: %+v", hits)
	}
	after, _ := s.MessagesAfter(c, 10, 2)
	if len(after) != 2 || after[0].ID != "c" || after[1].ID != "d" {
		t.Fatalf("depois: %+v", after)
	}
}

func TestLidoEmOutroAparelho(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const c = "1@s.whatsapp.net"
	for i, id := range []string{"a", "b", "c", "d"} {
		s.SaveMessage(ipc.Message{ID: id, Chat: c, Sender: c, TS: int64(i + 1), Kind: "text", Text: id}, nil, false, true)
	}
	if ch, _ := s.Chat(c); ch.Unread != 4 {
		t.Fatalf("antes: %d", ch.Unread)
	}
	// Leu no celular até a "b": continuam não lidas a "c" e a "d".
	if ok, _ := s.MarkReadUpTo(c, []string{"a", "b"}); !ok {
		t.Fatal("não achou as mensagens")
	}
	if ch, _ := s.Chat(c); ch.Unread != 2 {
		t.Fatalf("depois: %d, quer 2", ch.Unread)
	}
	if ok, _ := s.MarkReadUpTo(c, []string{"desconhecida"}); ok {
		t.Fatal("mexeu com id desconhecido")
	}
}

func TestTrancadaETemporarias(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const c = "1@s.whatsapp.net"
	now := time.Now().UnixMilli()
	s.SaveMessage(ipc.Message{ID: "velha", Chat: c, Sender: c, TS: now - 10000, Kind: "text", Text: "sumiu",
		Expires: now - 1000}, nil, false, false)
	s.SaveMessage(ipc.Message{ID: "nova", Chat: c, Sender: c, TS: now, Kind: "text", Text: "fica",
		Expires: now + 3600000}, nil, false, false)
	msgs, _ := s.Messages(c, 10, 0)
	if len(msgs) != 1 || msgs[0].ID != "nova" {
		t.Fatalf("vencida apareceu: %+v", msgs)
	}
	if n, _ := s.PurgeExpired(); n != 1 {
		t.Fatalf("purge apagou %d", n)
	}
	if changed, _ := s.SetEphemeral(c, 604800); !changed || s.Ephemeral(c) != 604800 {
		t.Fatal("prazo não gravou")
	}
	if ch, _ := s.Chat(c); ch.Ephemeral != 604800 {
		t.Fatalf("Chat.Ephemeral: %d", ch.Ephemeral)
	}
	s.SetLocked(c, true)
	if chats, _ := s.Chats(); len(chats) != 0 || !s.IsLocked(c) {
		t.Fatalf("trancada apareceu na lista: %+v", chats)
	}
	if hits, _ := s.SearchMessages(SearchFilter{}, func(string) bool { return true }, 10); len(hits) != 0 {
		t.Fatal("busca achou mensagem de conversa trancada")
	}
}

func TestVotoDeEnquete(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetPollVote("g", "p", "ana", []string{"aa"}, 200)
	if changed, _ := s.SetPollVote("g", "p", "ana", []string{"bb"}, 100); changed {
		t.Error("voto mais antigo sobrescreveu o mais novo")
	}
	s.SetPollVote("g", "p", "bia", nil, 300) // tirou o voto
	v, _ := s.PollVotes("g", "p")
	if len(v) != 1 || v["ana"][0] != "aa" {
		t.Fatalf("votos: %v", v)
	}
}

func TestSalvaNaConversa(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const c = "1@s.whatsapp.net"
	now := time.Now().UnixMilli()
	s.SaveMessage(ipc.Message{ID: "x", Chat: c, Sender: c, TS: now - 5000, Kind: "text", Text: "fica", Expires: now - 1000}, nil, false, false)
	if changed, _ := s.SetKept(c, "x", true); !changed {
		t.Fatal("não salvou")
	}
	if n, _ := s.PurgeExpired(); n != 0 {
		t.Fatal("apagou a salva")
	}
	m, _, err := s.Message(c, "x")
	if err != nil || !m.Kept || m.Expires == 0 {
		t.Fatalf("leitura: %+v %v", m, err)
	}
	s.SetKept(c, "x", false)
	if n, _ := s.PurgeExpired(); n != 1 {
		t.Fatal("desfeito o salvar, a vencida devia sumir")
	}
}

func TestLimparConversa(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const c = "5584@s.whatsapp.net"
	for i, txt := range []string{"um", "dois", "tres"} {
		m := ipc.Message{ID: string(rune('a' + i)), Chat: c, Sender: c, SenderName: "Ana",
			TS: int64(1000 * (i + 1)), Kind: "text", Text: txt}
		if _, err := s.SaveMessage(m, nil, false, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetReaction(c, "b", c, "❤️", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetStarred(c, "a", true); err != nil {
		t.Fatal(err)
	}

	// Até a segunda, guardando a favorita: sai só a "b".
	n, err := s.ClearChat(c, 2000, true)
	if err != nil || n != 1 {
		t.Fatalf("limpar até 2000: n=%d err=%v", n, err)
	}
	ms, _ := s.Messages(c, 10, 0)
	if len(ms) != 2 {
		t.Fatalf("deviam sobrar 2, sobraram %d", len(ms))
	}
	rs, _ := s.Reactions(c, []string{"b"}, "")
	if len(rs["b"]) != 0 {
		t.Fatal("reação da mensagem apagada ficou")
	}

	// Tudo: a conversa fica na lista, vazia e lida.
	if _, err := s.ClearChat(c, 0, false); err != nil {
		t.Fatal(err)
	}
	ch, err := s.Chat(c)
	if err != nil {
		t.Fatal(err)
	}
	if ch.LastText != "" || ch.Unread != 0 || ch.LastTS != 3000 {
		t.Fatalf("chat depois de limpar: %+v", ch)
	}
	if ms, _ := s.Messages(c, 10, 0); len(ms) != 0 {
		t.Fatalf("sobraram %d mensagens", len(ms))
	}
}

func TestBuscaComFiltro(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const c = "1@s.whatsapp.net"
	for i, m := range []ipc.Message{
		{Kind: "document", Text: "[documento: contrato.PDF] assinado"},
		{Kind: "document", Text: "[documento: planilha.xlsx]"},
		{Kind: "image", Text: "[imagem] praia"},
		{Kind: "sticker", Text: "[figurinha]"},
		{Kind: "other", Text: "[enquete: almoço?]"},
	} {
		m.ID, m.Chat, m.Sender, m.TS = string(rune('a'+i)), c, c, int64(1000*(i+1))
		s.SaveMessage(m, nil, false, false)
	}
	all := func(string) bool { return true }
	ids := func(f SearchFilter) string {
		hits, err := s.SearchMessages(f, all, 10)
		if err != nil {
			t.Fatal(err)
		}
		out := ""
		for _, h := range hits {
			out += h.ID
		}
		return out
	}
	for f, want := range map[SearchFilter]string{
		{Ext: "pdf"}:                   "a",
		{Type: "docs"}:                 "ba",
		{Type: "figurinhas"}:           "d",
		{Type: "enquetes"}:             "e",
		{From: 2000, To: 4000}:         "cb",
		{Type: "fotos", Chat: "outro"}: "",
		{Type: "fotos", From: 3000}:    "c",
	} {
		if got := ids(f); got != want {
			t.Errorf("%+v: %q, quer %q", f, got, want)
		}
	}
	if _, err := s.SearchMessages(SearchFilter{Type: "xyz"}, all, 10); err == nil {
		t.Error("tipo desconhecido passou")
	}
}

func TestCentralDeMidia(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, c := range []string{"1@s.whatsapp.net", "2@s.whatsapp.net", "3@s.whatsapp.net"} {
		s.SaveMessage(ipc.Message{ID: fmt.Sprint(i), Chat: c, Sender: c, TS: int64(i + 1), Kind: "image", Text: "[imagem]"}, nil, false, false)
	}
	s.SetLocked("3@s.whatsapp.net", true)
	items, more, err := s.Gallery("", "fotos", 0, 1)
	if err != nil || len(items) != 1 || items[0].ID != "1" || !more {
		t.Fatalf("primeira página: %+v %v %v", items, more, err)
	}
	if items, _, _ := s.Gallery("", "fotos", items[0].TS, 10); len(items) != 1 || items[0].ID != "0" {
		t.Fatalf("segunda página: %+v", items)
	}
	if n, _ := s.GalleryCounts(""); n["fotos"] != 2 {
		t.Fatalf("contagem: %v (a trancada não entra)", n)
	}
	if n, _ := s.GalleryCounts("1@s.whatsapp.net"); n["fotos"] != 1 {
		t.Fatalf("contagem do chat: %v", n)
	}
}

func TestListas(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.PutLabel("1", "", 3, 0, false)          // Favoritas
	s.PutLabel("7", "Trabalho", 5, 2, false)  // personalizada
	s.PutLabel("2", "Não lidas", 1, 1, false) // predefinida, não vira filtro
	s.SetChatLabel("1", "a@s.whatsapp.net", true)
	s.SetChatLabel("7", "a@s.whatsapp.net", true)
	s.SetChatLabel("7", "b@s.whatsapp.net", true)
	ls, _ := s.Labels([]int{0, 3, 4, 5})
	if len(ls) != 2 || ls[0].ID != "1" || ls[1].Name != "Trabalho" || ls[1].Count != 2 {
		t.Fatalf("listas: %+v", ls)
	}
	if got := s.ChatLabels("a@s.whatsapp.net"); len(got) != 2 {
		t.Fatalf("listas da conversa: %v", got)
	}
	s.SetChatLabel("7", "a@s.whatsapp.net", false)
	s.PutLabel("1", "", 3, 0, true) // apagou Favoritas
	if got := s.ChatLabels("a@s.whatsapp.net"); len(got) != 0 {
		t.Fatalf("depois de tirar: %v", got)
	}
	if ls, _ := s.Labels(nil); len(ls) != 2 {
		t.Fatalf("apagada continuou: %+v", ls)
	}
}

func TestComunidades(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.PutCommunity("c@g.us", "Bairro")
	s.SetGroupParent("avisos@g.us", "c@g.us", true)
	s.SetGroupParent("feira@g.us", "c@g.us", false)
	if j, n, def := s.GroupCommunity("avisos@g.us"); j != "c@g.us" || n != "Bairro" || !def {
		t.Fatalf("avisos: %q %q %v", j, n, def)
	}
	s.SetGroupParent("feira@g.us", "", false) // saiu da comunidade
	if j, _, _ := s.GroupCommunity("feira@g.us"); j != "" {
		t.Fatalf("feira continuou: %q", j)
	}
}

func TestStatusForaDaLista(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UnixMilli()
	s.SaveMessage(ipc.Message{ID: "s1", Chat: "status@broadcast", Sender: "1@s.whatsapp.net", TS: now, Kind: "image",
		Text: "[imagem] praia", Expires: now + 3600000}, nil, true, true)
	if chats, _ := s.Chats(); len(chats) != 0 {
		t.Fatalf("status apareceu na lista: %+v", chats)
	}
	if hits, _ := s.SearchMessages(SearchFilter{}, func(string) bool { return true }, 10); len(hits) != 0 {
		t.Fatal("busca achou status")
	}
	if items, _, _ := s.Gallery("", "fotos", 0, 10); len(items) != 0 {
		t.Fatal("central de mídia mostrou status")
	}
	if msgs, _ := s.Messages("status@broadcast", 10, 0); len(msgs) != 1 {
		t.Fatal("status não ficou guardado")
	}
}

func TestTrocaDeDono(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zapterm.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if moved, err := s.SwitchOwner("5511111"); err != nil || moved != "" || s.Owner() != "5511111" {
		t.Fatalf("primeiro dono: %q %v %q", moved, err, s.Owner())
	}
	if _, err := s.SaveMessage(ipc.Message{ID: "a", Chat: "1@s.whatsapp.net", Sender: "1@s.whatsapp.net",
		TS: 1000, Kind: "text", Text: "da conta antiga"}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	// A mesma conta de novo: nada muda.
	if moved, err := s.SwitchOwner("5511111"); err != nil || moved != "" {
		t.Fatalf("mesmo dono: %q %v", moved, err)
	}
	if msgs, _ := s.Messages("1@s.whatsapp.net", 10, 0); len(msgs) != 1 {
		t.Fatal("a mesma conta perdeu mensagens")
	}

	// Outra conta: o banco antigo vai para o lado e este recomeça vazio.
	moved, err := s.SwitchOwner("5522222")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(path), "zapterm-5511111.db"); moved != want {
		t.Fatalf("guardado em %q, esperava %q", moved, want)
	}
	if msgs, _ := s.Messages("1@s.whatsapp.net", 10, 0); len(msgs) != 0 {
		t.Fatal("o banco novo devia estar vazio")
	}
	if s.Owner() != "5522222" {
		t.Fatalf("dono = %q", s.Owner())
	}
	old, err := Open(moved)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if msgs, _ := old.Messages("1@s.whatsapp.net", 10, 0); len(msgs) != 1 || old.Owner() != "5511111" {
		t.Fatalf("o banco guardado perdeu o conteúdo: %+v dono %q", msgs, old.Owner())
	}
	// O novo continua gravável.
	if _, err := s.SaveMessage(ipc.Message{ID: "b", Chat: "2@s.whatsapp.net", Sender: "2@s.whatsapp.net",
		TS: 2000, Kind: "text", Text: "da conta nova"}, nil, false, false); err != nil {
		t.Fatal(err)
	}

	// Voltando à primeira conta, o arquivo guardado não é sobrescrito.
	moved2, err := s.SwitchOwner("5511111")
	if err != nil || moved2 == "" {
		t.Fatalf("volta: %q %v", moved2, err)
	}
	if moved2 == moved {
		t.Fatal("sobrescreveu o banco guardado")
	}
}

// A DSN do driver sem cgo muda o jeito de ligar os pragmas; se um deles não
// pegar, o upgrade do whatsmeow (que exige foreign_keys) quebra só em produção.
func TestPragmasDoDriver(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var fk, busy int
	var jm string
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d (%v), quer 1", fk, err)
	}
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&jm); err != nil || jm != "wal" {
		t.Errorf("journal_mode = %q (%v), quer wal", jm, err)
	}
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil || busy != 5000 {
		t.Errorf("busy_timeout = %d (%v), quer 5000", busy, err)
	}
}
