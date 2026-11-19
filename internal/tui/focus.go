package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// openRequested abre o chat pedido pelo clique na notificação e traz a
// janela do kitty desta TUI para frente.
func (m *model) openRequested(jid string) {
	c, ok := m.chatByJID(jid)
	if !ok {
		// Chat que a lista ainda não conhece (primeira mensagem dele):
		// abre assim mesmo; o nome chega com o próximo evento.
		c = ipc.Chat{JID: jid, Name: jid, LastStatus: -1}
	}
	m.pendingOpen = c
	focusOwnWindow()
}

// focusOwnWindow sobe pela árvore de processos até o kitty que hospeda
// esta TUI e pede ao Sway foco nessa janela.
func focusOwnWindow() {
	pid := os.Getppid()
	for i := 0; i < 8 && pid > 1; i++ {
		comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if strings.TrimSpace(string(comm)) == "kitty" {
			_ = exec.Command("swaymsg", "-q", fmt.Sprintf("[pid=%d] focus", pid)).Run()
			return
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return
		}
		// stat: "pid (comm) state ppid ..."; o comm pode ter espaços.
		rest := string(stat)
		if i := strings.LastIndexByte(rest, ')'); i >= 0 {
			rest = rest[i+1:]
		}
		var state string
		if _, err := fmt.Sscan(rest, &state, &pid); err != nil {
			return
		}
	}
}
