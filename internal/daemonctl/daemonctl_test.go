package daemonctl

import (
	"errors"
	"testing"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

func TestDecide(t *testing.T) {
	hello := func(v string, proto int) ipc.Hello { return ipc.Hello{Version: v, Proto: proto, PID: 42} }
	legacy := errors.New(`operação desconhecida: "hello"`)
	legacyPareando := errors.New("não conectado")
	timeout := errors.New("daemon não respondeu a tempo")

	casos := []struct {
		nome    string
		h       ipc.Hello
		err     error
		mine    string
		deleted bool
		want    Action
	}{
		{"mesma versão", hello("v0.2.0", ipc.ProtoVersion), nil, "v0.2.0", false, Keep},
		{"dev dos dois lados", hello("dev", ipc.ProtoVersion), nil, "dev", false, Keep},
		{"dev dos dois lados, TUI apagada", hello("dev", ipc.ProtoVersion), nil, "dev", true, Keep},
		{"versão diferente", hello("v0.1.0", ipc.ProtoVersion), nil, "v0.2.0", false, Shutdown},
		{"daemon dev, TUI de release", hello("dev", ipc.ProtoVersion), nil, "v0.2.0", false, Shutdown},
		{"daemon de release, TUI dev", hello("v0.2.0", ipc.ProtoVersion), nil, "dev", false, Shutdown},
		{"versão diferente, TUI velha", hello("v0.3.0", ipc.ProtoVersion), nil, "v0.2.0", true, WarnStale},
		{"protocolo diferente com a mesma versão", hello("dev", ipc.ProtoVersion+1), nil, "dev", false, Shutdown},
		{"daemon anterior ao hello", ipc.Hello{}, legacy, "v0.2.0", false, Kill},
		{"daemon anterior ao hello, pareando", ipc.Hello{}, legacyPareando, "v0.2.0", false, Kill},
		{"daemon anterior ao hello, TUI velha", ipc.Hello{}, legacy, "v0.2.0", true, WarnStale},
		{"daemon anterior, os dois dev", ipc.Hello{}, legacy, "dev", false, Kill},
		{"erro qualquer não mexe", ipc.Hello{}, timeout, "v0.2.0", false, Keep},
		{"conexão fechada não mexe", ipc.Hello{}, ipc.ErrClosed, "v0.2.0", true, Keep},
	}
	for _, c := range casos {
		if got := Decide(c.h, c.err, c.mine, c.deleted); got != c.want {
			t.Errorf("%s: got %d, quer %d", c.nome, got, c.want)
		}
	}
}
