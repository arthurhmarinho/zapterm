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

	if ok, _ := s.SaveMessage(novo, true, true); !ok {
		t.Fatal("primeira gravação deveria ser nova")
	}
	// Chega fora de ordem, como no history sync: não pode roubar a prévia.
	if _, err := s.SaveMessage(velho, true, false); err != nil {
		t.Fatal(err)
	}
	// Duplicata não conta não lida de novo.
	if ok, _ := s.SaveMessage(novo, true, true); ok {
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
