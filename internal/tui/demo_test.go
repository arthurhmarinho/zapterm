package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// "@ra" num grupo abre a lista, tab marca o Rafa e o envio leva o número
// no texto e o JID na lista de marcados, como o WhatsApp espera.
func TestMencaoEmGrupo(t *testing.T) {
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
	g, _ := m.chatByJID("120000000000000001@g.us")
	m.active, m.activeChat, m.focus = g.JID, g, paneChat
	m.layout()
	typ := func(s string) {
		for _, r := range s {
			_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
			runCmd(m, cmd)
		}
	}
	typ("valeu @ra")
	if !m.mention.open || m.mention.list[0].Name != "Rafa" {
		t.Fatalf("lista de menção: %+v", m.mention)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "valeu @Rafa " {
		t.Fatalf("inserção: %q", m.input.Value())
	}
	typ("!")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCmd(m, cmd)
	sent := demo.LastSent(srv)
	if sent.Text != "valeu @5511900000010 !" || len(sent.Mentions) != 1 || sent.Mentions[0] != "5511900000010@s.whatsapp.net" {
		t.Fatalf("enviado: %+v", sent)
	}
}

// runCmd executa os comandos (e lotes) que pedem ao daemon, devolvendo as
// respostas ao modelo; ignora os que esperam eventos ou tempo.
func runCmd(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				runCmd(m, c)
			}
		case nil:
		default:
			_, next := m.Update(v)
			runCmd(m, next)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestMenuDoChat(t *testing.T) {
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
	if !m.list[0].Pinned {
		t.Fatal("a primeira da demonstração devia estar fixada")
	}
	m.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
	if m.menu == nil || m.menu.opts[0].action != "unpin" {
		t.Fatalf("menu: %+v", m.menu)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Desafixar") {
		t.Fatal("menu não apareceu na tela")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	runCmd(m, cmd)
	if m.menu != nil || !strings.Contains(m.flash, "desafixado") {
		t.Fatalf("ação: menu=%v flash=%q", m.menu, m.flash)
	}
}

// Busca no texto: o resultado entra na lista e abrir pula para a mensagem.
func TestBuscaNoTexto(t *testing.T) {
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
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	for _, r := range "endere" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, cmd := m.Update(searchTickMsg{query: "endere"}) // a pausa na digitação
	runCmd(m, cmd)
	idx := -1
	for i, c := range m.list {
		if strings.HasPrefix(c.JID, hitMarker) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("resultado não entrou na lista: %+v", m.list)
	}
	m.cursor = idx
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	runCmd(m, cmd)
	if m.active != "5511900000001@s.whatsapp.net" || m.sel() < 0 || !strings.Contains(m.msgs[m.sel()].Text, "endereço") {
		t.Fatalf("não pulou para a mensagem: ativo=%s sel=%d", m.active, m.sel())
	}
}

func TestPerfil(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(m.loadChats()())
	g, _ := m.chatByJID("120000000000000001@g.us")
	m.active, m.activeChat, m.focus = g.JID, g, paneChat
	m.layout()

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Mod: tea.ModAlt})
	runCmd(m, cmd)
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"Dados do grupo", "Grupo · 3 participantes", "Levar água", "Participantes (3)", "admin", "Mídia (0)"} {
		if !strings.Contains(screen, want) {
			t.Errorf("faltou %q no perfil", want)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.profileOpen {
		t.Fatal("esc não fechou o perfil")
	}
	geo := m.geom()
	_, cmd = m.Update(tea.MouseClickMsg{X: geo.chatX + 10, Y: 1, Button: tea.MouseLeft})
	runCmd(m, cmd)
	if !m.profileOpen || m.profile == nil {
		t.Fatal("clique no cabeçalho não abriu o perfil")
	}
}
