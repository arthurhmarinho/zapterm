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
