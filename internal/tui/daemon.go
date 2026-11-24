package tui

import (
	"sync/atomic"

	"github.com/arthurhmarinho/zapterm/internal/daemonctl"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// staleUI vira true quando o binário foi trocado com esta TUI aberta: o
// daemon (já na versão nova) é mantido e o aviso pede para reabrir.
var staleUI atomic.Bool

const staleMsg = "o zapterm foi atualizado: feche e abra de novo"

// connect devolve um cliente ligado ao daemon, subindo o daemon se preciso e
// reiniciando o que for de outra versão (ver daemonctl.Connect).
func connect() (*ipc.Client, error) {
	cli, stale, err := daemonctl.Connect()
	if stale {
		staleUI.Store(true)
	}
	return cli, err
}
