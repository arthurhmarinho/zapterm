package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/demo"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Desenha a TUI contra o daemon de demonstração (dados fictícios): serve de
// teste de layout sem depender de conta nenhuma. ZAPTERM_SHOW=1 imprime.
func TestTelaDemo(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "demo.sock")
	srv, err := demo.Serve(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli, err := ipc.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	m := newModel(cli)
	m.imgs.enabled = false
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m.Update(m.loadChats()())
	m.Update(m.loadStatus()())
	c := m.list[0]
	m.active, m.activeChat, m.focus = c.JID, c, paneChat
	m.layout()
	m.Update(m.loadMessages(c.JID, 0)())
	screen := ansi.Strip(m.View().Content)
	if os.Getenv("ZAPTERM_SHOW") != "" {
		t.Log("\n" + screen)
	}
	for _, want := range []string{"Você (demonstração)", "Marina Alves", "Trilha de sábado", "Arquivadas",
		"Mensagem apagada", "editada", "fechado, te vejo lá!", "👍"} {
		if !strings.Contains(screen, want) {
			t.Errorf("faltou %q na tela", want)
		}
	}
}
