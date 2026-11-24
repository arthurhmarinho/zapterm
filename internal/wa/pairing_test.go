package wa

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/store"
)

// daemonSemSessao monta um daemon como o do modo de pareamento: aparelho sem
// JID (sem repositório de contatos, LIDs e configurações) e banco com uma
// conversa da conta anterior.
func daemonSemSessao(t *testing.T) *Daemon {
	t.Helper()
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	container, err := sqlstore.New(context.Background(), "sqlite",
		"file:"+filepath.Join(dir, "session.db")+"?_pragma=foreign_keys(1)", waLogger{log})
	if err != nil {
		t.Fatal(err)
	}
	dev, err := container.GetFirstDevice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "zapterm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.SaveMessage(ipc.Message{ID: "1", Chat: "5511900000001@s.whatsapp.net",
		Sender: "5511900000001@s.whatsapp.net", TS: 1000, Kind: "text", Text: "oi"}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	srv, err := ipc.Listen(filepath.Join(dir, "d.sock"), func(uint64, ipc.Request) (any, error) { return nil, nil }, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	d := &Daemon{log: log, st: st, srv: srv, status: ipc.Status{State: "pairing"}, notif: newNotifier(), avatars: newAvatars()}
	d.cli = whatsmeow.NewClient(dev, waLogger{log})
	return d
}

func TestSemSessaoNaoQuebra(t *testing.T) {
	d := daemonSemSessao(t)
	if d.paired() {
		t.Fatal("aparelho sem JID não está pareado")
	}
	// O que lê o banco responde, sem passar pelo whatsmeow.
	got, err := d.handle(1, ipc.Request{Op: ipc.OpChats})
	chats, _ := got.([]ipc.Chat)
	if err != nil || len(chats) != 1 || chats[0].Kind != "dm" || chats[0].Name == "" {
		t.Fatalf("chats sem sessão: %+v %v", got, err)
	}
	got, err = d.handle(1, ipc.Request{Op: ipc.OpMessages, Chat: chats[0].JID, Limit: 10})
	if msgs, _ := got.([]ipc.Message); err != nil || len(msgs) != 1 {
		t.Fatalf("mensagens sem sessão: %+v %v", got, err)
	}
	// O resto pede conexão e avisa, sem panic.
	for _, op := range []string{ipc.OpSend, ipc.OpMe, ipc.OpSetMe, ipc.OpProfile, ipc.OpRead, ipc.OpMedia, ipc.OpNewGroup,
		ipc.OpStatuses, ipc.OpReact, ipc.OpSendFile, ipc.OpMembers, ipc.OpChatAction, ipc.OpCheckNumber} {
		if _, err := d.handle(1, ipc.Request{Op: op, Chat: "5511900000001@s.whatsapp.net", Msg: "1", Text: "x"}); err == nil ||
			!strings.Contains(err.Error(), "não conectado") {
			t.Errorf("%s sem sessão: %v", op, err)
		}
	}
}

func TestPairPhoneSemQR(t *testing.T) {
	d := daemonSemSessao(t)
	// Antes do primeiro QR não dá para pedir o código.
	if _, err := d.pairPhone(context.Background(), "5511999999999"); err == nil || !strings.Contains(err.Error(), "conectando") {
		t.Fatalf("pedir o código antes do QR: %v", err)
	}
	d.setPair("2@abc", "", "aviso")
	if p := d.pairInfo(); p.QR != "2@abc" || p.Err != "aviso" {
		t.Fatalf("pairInfo: %+v", p)
	}
	got, _ := d.handle(1, ipc.Request{Op: ipc.OpStatus})
	if st, _ := got.(ipc.Status); st.State != "pairing" || st.Pair == nil || st.Pair.QR != "2@abc" {
		t.Fatalf("status: %+v", got)
	}
	// Sair do modo pairing limpa o que a tela de login mostrava.
	d.setStatus("connecting")
	if st := d.status; st.Pair != nil || st.State != "connecting" {
		t.Fatalf("status depois: %+v", st)
	}
}

func TestDonoTrocadoGuardaAsConversas(t *testing.T) {
	d := daemonSemSessao(t)
	d.claimOwner("5511111") // conta anterior
	if _, err := d.st.SaveMessage(ipc.Message{ID: "2", Chat: "5511900000002@s.whatsapp.net",
		Sender: "5511900000002@s.whatsapp.net", TS: 2000, Kind: "text", Text: "da conta antiga"}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	d.claimOwner("5511111") // a mesma conta pareando de novo: tudo fica
	if chats, _ := d.st.Chats(); len(chats) != 2 {
		t.Fatalf("a mesma conta perdeu conversas: %d", len(chats))
	}
	d.claimOwner("5522222") // outra conta: o banco recomeça
	if chats, _ := d.st.Chats(); len(chats) != 0 {
		t.Fatalf("conversas da conta antiga continuam: %d", len(chats))
	}
	if d.st.Owner() != "5522222" {
		t.Fatalf("dono = %q", d.st.Owner())
	}
}
