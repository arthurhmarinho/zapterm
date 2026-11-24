package wa

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

func TestLockDoDaemon(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	a, err := lockDaemon()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockDaemon(); err == nil || !strings.Contains(err.Error(), "já está rodando") {
		t.Fatalf("o segundo daemon deveria falhar: %v", err)
	}
	a.Close() // o sistema solta o lock quando o arquivo fecha (ou o processo morre)
	b, err := lockDaemon()
	if err != nil {
		t.Fatalf("depois de soltar, o lock deveria voltar: %v", err)
	}
	b.Close()
}

// Hello e shutdown valem também sem sessão (pareamento), antes de qualquer
// outra checagem.
func TestHelloEShutdownSemSessao(t *testing.T) {
	d := daemonSemSessao(t)
	if d.paired() {
		t.Fatal("o teste precisa de um daemon sem sessão")
	}
	got, err := d.handle(1, ipc.Request{Op: ipc.OpHello})
	h, _ := got.(ipc.Hello)
	if err != nil || h.Proto != ipc.ProtoVersion || h.Version == "" || h.PID != os.Getpid() {
		t.Fatalf("hello: %+v %v", got, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.stop = cancel
	if _, err := d.handle(1, ipc.Request{Op: ipc.OpShutdown}); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(exitDelay + 2*time.Second):
		t.Fatal("o shutdown não encerrou o Run")
	}
}
