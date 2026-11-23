package wa

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// daemonPareado: aparelho com sessão gravada no session.db (nunca conecta).
func daemonPareado(t *testing.T) (*Daemon, *sqlstore.Container) {
	t.Helper()
	d := daemonSemSessao(t)
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	container, err := sqlstore.New(context.Background(), "sqlite3",
		"file:"+filepath.Join(dir, "s.db")+"?_foreign_keys=on", waLogger{log})
	if err != nil {
		t.Fatal(err)
	}
	dev := container.NewDevice()
	jid := types.NewADJID("5511900000000", 0, 7)
	dev.ID = &jid
	dev.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{1}, AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := dev.Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.cli = whatsmeow.NewClient(dev, waLogger{log})
	d.status = ipc.Status{State: "online"}
	return d, container
}

func TestDesconectarSemConexaoNaoApagaASessao(t *testing.T) {
	d, container := daemonPareado(t)
	_, err := d.handle(1, ipc.Request{Op: ipc.OpLogout})
	if err == nil || err.Error() != "sem conexão com o WhatsApp; tente de novo" {
		t.Fatalf("erro = %v", err)
	}
	dev, err := container.GetFirstDevice(context.Background())
	if err != nil || dev.ID == nil || d.cli.Store.ID == nil {
		t.Fatalf("a sessão foi apagada sem o servidor confirmar: %+v %v", dev.ID, err)
	}
	if d.status.State != "online" {
		t.Fatalf("estado mudou para %q", d.status.State)
	}
}

func TestDesconectadoPeloCelular(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	logoutWait = 50 * time.Millisecond
	t.Cleanup(func() { logoutWait = 3 * time.Second })
	d, container := daemonPareado(t)
	var stopped atomic.Bool
	d.stop = func() { stopped.Store(true) }

	// Aqui o whatsmeow não apagou a sessão a tempo: o daemon apaga.
	d.onLoggedOut(&events.LoggedOut{Reason: events.ConnectFailureLoggedOut})

	if dev, _ := container.GetFirstDevice(context.Background()); dev.ID != nil {
		t.Fatal("a sessão continua no session.db")
	}
	if d.cli.Store.ID != nil || d.status.State != "logged_out" {
		t.Fatalf("estado: ID %v, %q", d.cli.Store.ID, d.status.State)
	}
	if got := takeLoggedOutNote(); got != loggedOutNote {
		t.Fatalf("aviso para a tela de login = %q", got)
	}
	if got := takeLoggedOutNote(); got != "" {
		t.Fatalf("o aviso devia valer uma vez só: %q", got)
	}
	for i := 0; i < 50 && !stopped.Load(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !stopped.Load() {
		t.Fatal("o daemon não encerrou")
	}
	// Daqui em diante é o modo de pareamento: nada de panic.
	if _, err := d.handle(1, ipc.Request{Op: ipc.OpSend, Chat: "1@s.whatsapp.net", Text: "oi"}); err == nil {
		t.Fatal("enviar sem sessão devia dar erro")
	}
}

func TestMensagensFicamDepoisDeDesconectar(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	logoutWait = 50 * time.Millisecond
	t.Cleanup(func() { logoutWait = 3 * time.Second })
	d, _ := daemonPareado(t)
	d.stop = func() {}
	d.onLoggedOut(&events.LoggedOut{})
	msgs, err := d.st.Messages("5511900000001@s.whatsapp.net", 10, 0)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("as mensagens locais sumiram: %+v %v", msgs, err)
	}
}
