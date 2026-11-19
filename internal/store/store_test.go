package store

import (
	"path/filepath"
	"testing"

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
