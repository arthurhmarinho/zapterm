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

func TestAcoesNaMensagem(t *testing.T) {
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
	c := m.list[0]
	m.active, m.activeChat, m.focus = c.JID, c, paneChat
	m.layout()
	m.Update(m.loadMessages(c.JID, 0)())
	alt := func(r rune) { m.Update(tea.KeyPressMsg{Code: r, Mod: tea.ModAlt}) }

	// A última mensagem é sua e recente: editar põe o texto na caixa.
	alt('e')
	if m.editing == nil || m.input.Value() != "fechado, te vejo lá!" {
		t.Fatalf("editar: %v %q", m.editing, m.input.Value())
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Editando mensagem") {
		t.Fatal("painel de edição não apareceu")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.editing != nil || m.input.Value() != "" {
		t.Fatal("esc não cancelou a edição")
	}

	// Apagar pede confirmação.
	alt('d')
	if m.delArmed == "" || !strings.Contains(m.flash, "de novo") {
		t.Fatalf("apagar sem confirmação: %q", m.flash)
	}

	// Encaminhar abre a busca.
	alt('w')
	if m.forwarding == nil || !m.searching {
		t.Fatal("encaminhar não abriu a busca")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.forwarding != nil {
		t.Fatal("esc não cancelou o encaminhamento")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	if !m.help || !strings.Contains(ansi.Strip(m.View().Content), "apagar para todos") {
		t.Fatal("ajuda não abriu")
	}
}
