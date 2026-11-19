package demo

import (
	"path/filepath"
	"testing"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

func TestDemoResponde(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	srv, err := Serve(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	c, err := ipc.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var chats []ipc.Chat
	if err := c.Call(ipc.Request{Op: ipc.OpChats}, &chats); err != nil || len(chats) < 5 {
		t.Fatalf("chats: %v %d", err, len(chats))
	}
	var msgs []ipc.Message
	if err := c.Call(ipc.Request{Op: ipc.OpMessages, Chat: chats[0].JID}, &msgs); err != nil || len(msgs) == 0 {
		t.Fatalf("mensagens: %v %d", err, len(msgs))
	}
	var sent ipc.Message
	if err := c.Call(ipc.Request{Op: ipc.OpSend, Chat: chats[0].JID, Text: "teste"}, &sent); err != nil || sent.Text != "teste" {
		t.Fatalf("envio: %v %+v", err, sent)
	}
	if err := c.Call(ipc.Request{Op: ipc.OpSendFile, Chat: chats[0].JID}, nil); err == nil {
		t.Fatal("envio de arquivo deveria ser recusado na demonstração")
	}
}
