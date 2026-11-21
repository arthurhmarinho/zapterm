package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/config"
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

	// Apagar pergunta para quem (a sua, recente: as duas opções).
	alt('d')
	if m.popup == nil || len(m.popup.opts) != 3 || m.popup.opts[1].label != "Apagar para todos" {
		t.Fatalf("apagar: %+v", m.popup)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Apagar mensagem?") {
		t.Fatal("pergunta não aparece")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.popup != nil {
		t.Fatal("esc não fechou a pergunta")
	}

	// Menu da mensagem: só o que vale (a sua, de texto, recente).
	alt('.')
	var labels []string
	for _, o := range m.popup.opts {
		labels = append(labels, o.label)
	}
	if got := strings.Join(labels, ","); got != "Responder,Reagir,Copiar,Editar,Encaminhar,Fixar…,Favoritar,Dados da mensagem,Apagar…" {
		t.Fatalf("menu: %s", got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	// Apagar para mim: some na hora, alt+z devolve.
	n := len(m.msgs)
	alt('d')
	m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if len(m.msgs) != n-1 || m.pendingDel == nil {
		t.Fatal("apagar para mim não tirou a mensagem")
	}
	alt('z')
	if len(m.msgs) != n || m.msgs[n-1].ID != "m7" {
		t.Fatal("alt+z não devolveu a mensagem")
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
	if !m.help || !strings.Contains(ansi.Strip(m.View().Content), "para mim ou para todos") {
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
	for _, want := range []string{"Dados do grupo", "Grupo · 3 participantes", "Levar água", "Participantes (3)", "admin", "Mídia, links e docs"} {
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

// [keys] do config: alt+r passa a responder e ctrl+r para de responder.
func TestAtalhoTrocadoPeloConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "zapterm"), 0o700)
	os.WriteFile(filepath.Join(dir, "zapterm", "config.toml"), []byte("[keys]\nreply = \"alt+r\"\n"), 0o600)
	if err := applyConfig(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cfg = config.Config{Interface: config.Interface{Images: true, NerdFont: true}} })

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

	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.replyTo != nil {
		t.Fatal("ctrl+r ainda responde depois de trocado")
	}
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
	if m.replyTo == nil {
		t.Fatal("alt+r não respondeu")
	}
	if !strings.Contains(ansi.Strip(m.viewFooter()), "alt+r responder") {
		t.Fatalf("rodapé não mostra o atalho novo: %q", ansi.Strip(m.viewFooter()))
	}
}

// O texto meio escrito fica na conversa em que foi escrito.
func TestRascunhoPorConversa(t *testing.T) {
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
	a, b := m.list[0], m.list[1]
	runCmd(m, m.open(a))
	m.input.SetValue("meio escrito")
	runCmd(m, m.open(b))
	if v := m.input.Value(); v != "" {
		t.Fatalf("rascunho vazou para outra conversa: %q", v)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Rascunho: meio escrito") {
		t.Error("a lista não mostra o rascunho")
	}
	runCmd(m, m.open(a))
	if v := m.input.Value(); v != "meio escrito" {
		t.Fatalf("rascunho não voltou: %q", v)
	}
	if m.draftText(a.JID) != "" {
		t.Error("conversa aberta não deve aparecer como rascunho")
	}
}

// Cartões (evento, enquete, localização), selo de encaminhada e lista.
func TestCartoes(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 70})
	m.Update(m.loadChats()())
	var trilha ipc.Chat
	for _, c := range m.chats {
		if strings.HasPrefix(c.Name, "Trilha") {
			trilha = c
		}
	}
	runCmd(m, m.open(trilha))
	screen := ansi.Strip(m.View().Content)
	if os.Getenv("ZAPTERM_SHOW") != "" {
		t.Log("\n" + screen)
	}
	for _, want := range []string{"Trilha do Pico", "sáb, 22/11 às 06:00", "Quem precisa de carona?", "○ Tenho carro", "3 votos",
		"Encaminhada", "Portaria do parque", "ctrl+o abre o mapa", "• protetor", "▎ alguém leva protetor?"} {
		if !strings.Contains(screen, want) {
			t.Errorf("faltou %q", want)
		}
	}
}

func TestGaleria(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	runCmd(m, m.open(m.chatFor("5511900000001@s.whatsapp.net", "Marina")))
	runCmd(m, m.openProfile())
	runCmd(m, m.keyProfile("enter"))
	if m.gallery == nil {
		t.Fatal("enter no perfil não abriu a galeria")
	}
	runCmd(m, m.keyGallery("tab"))
	runCmd(m, m.keyGallery("tab"))
	runCmd(m, m.keyGallery("tab")) // fotos → vídeos → docs → links
	screen := ansi.Strip(m.View().Content)
	if os.Getenv("ZAPTERM_SHOW") != "" {
		t.Log("\n" + screen)
	}
	for _, want := range []string{"Mídia, links e docs", "Links 1", "museu.exemplo.org/agenda", "Marina Alves ·"} {
		if !strings.Contains(screen, want) {
			t.Errorf("faltou %q", want)
		}
	}
	runCmd(m, m.keyGallery("esc"))
	if m.gallery != nil || !m.profileOpen {
		t.Error("esc deveria voltar ao perfil")
	}
}

func TestVotarNaEnquete(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 70})
	m.Update(m.loadChats()())
	runCmd(m, m.open(m.chatFor("120000000000000001@g.us", "Trilha")))
	idx := -1
	for i, x := range m.msgs {
		if x.ID == "t6" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("enquete não carregou")
	}
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"3 votos · uma opção · ctrl+o vota", "██"} {
		if !strings.Contains(screen, want) {
			t.Errorf("faltou %q no placar", want)
		}
	}
	runCmd(m, m.openMedia(idx))
	if m.voting == nil {
		t.Fatal("ctrl+o não abriu o painel")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "espaço marca · enter vota") {
		t.Error("painel não aparece")
	}
	runCmd(m, m.keyPoll("1"))
	runCmd(m, m.keyPoll("3")) // uma opção só: o 3 troca o 1
	runCmd(m, m.keyPoll("enter"))
	got := demo.LastSent(srv)
	if got.Op != ipc.OpPollVote || got.Msg != "t6" || len(got.Options) != 1 || got.Options[0] != "Tenho carro" {
		t.Fatalf("voto enviado: %+v", got)
	}
	if m.voting != nil {
		t.Error("painel deveria fechar")
	}
}

func TestNavegacao(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 70})
	m.Update(m.loadChats()())

	// Divisor: 4 não lidas na trilha → antes da t5 (t3 é sua e não conta).
	var trilha ipc.Chat
	for _, c := range m.list {
		if strings.HasPrefix(c.Name, "Trilha") {
			trilha = c
		}
	}
	runCmd(m, m.open(trilha))
	if m.unreadID != "t5" {
		t.Fatalf("divisor antes de %q, queria t5", m.unreadID)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "4 mensagens não lidas") {
		t.Error("divisor não aparece")
	}

	// alt+↓ / alt+↑ andam pela lista; alt+n acha a próxima não lida.
	i := 0
	for j, c := range m.list {
		if c.JID == trilha.JID {
			i = j
		}
	}
	runCmd(m, m.switchChat(1))
	if m.active != m.list[i+1].JID {
		t.Fatal("alt+↓ não abriu a conversa de baixo")
	}
	runCmd(m, m.switchChat(-1))
	if m.active != trilha.JID {
		t.Fatal("alt+↑ não voltou")
	}
	runCmd(m, m.nextUnread())
	if c, _ := m.chatByJID(m.active); c.Unread == 0 || c.Muted || c.JID == trilha.JID {
		t.Fatalf("alt+n abriu %q", c.Name)
	}

	// ctrl+f: busca só nesta conversa e seleciona o achado.
	runCmd(m, m.open(m.chatFor("5511900000001@s.whatsapp.net", "Marina")))
	runCmd(m, m.openFind())
	m.finding.input.SetValue("almoçar")
	runCmd(m, m.keyFind(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if len(m.finding.hits) != 1 || m.sel() < 0 || m.msgs[m.sel()].ID != "m3" {
		t.Fatalf("achados %d, seleção %d", len(m.finding.hits), m.sel())
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "1 de 1") {
		t.Error("barra da busca sem a contagem")
	}
	runCmd(m, m.keyFind(tea.KeyPressMsg{Code: tea.KeyEscape}))
	if m.finding != nil {
		t.Error("esc não fechou a busca")
	}
}

func TestEditarUltima(t *testing.T) {
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
	runCmd(m, m.open(m.chatFor("5511900000001@s.whatsapp.net", "Marina")))
	runCmd(m, m.editLast())
	if m.editing == nil || m.editing.ID != "m7" || m.input.Value() != "fechado, te vejo lá!" {
		t.Fatalf("ctrl+↑ editando %+v", m.editing)
	}
}

func TestResponderEmPrivado(t *testing.T) {
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
	const grupo = "120000000000000001@g.us"
	runCmd(m, m.open(m.chatFor(grupo, "Trilha")))
	for i, x := range m.msgs {
		if x.ID == "t1" {
			m.msgSel = i
		}
	}
	runCmd(m, m.openMsgMenu())
	if m.popup.opts[1].label != "Responder em privado a Rafa" {
		t.Fatalf("menu: %q", m.popup.opts[1].label)
	}
	runCmd(m, m.keyPopup("2"))
	if m.active == grupo || m.replyTo == nil || m.replyTo.ID != "t1" {
		t.Fatalf("ativo %q, resposta %+v", m.active, m.replyTo)
	}
	if got := m.replyRef(); got != grupo+"|t1" {
		t.Fatalf("referência %q", got)
	}
}

func TestFavoritas(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	runCmd(m, m.open(m.chatFor("5511900000001@s.whatsapp.net", "Marina")))
	for i, x := range m.msgs {
		if x.ID == "m3" {
			m.msgSel = i
		}
	}
	runCmd(m, m.toggleStar())
	if m.flash != "mensagem favoritada" {
		t.Fatalf("flash %q", m.flash)
	}
	runCmd(m, m.setTab(tabStarred))
	if len(m.list) != 1 || !strings.HasPrefix(m.list[0].Name, "★ Marina") {
		t.Fatalf("lista de favoritas: %+v", m.list)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Dá pra gente almoçar") {
		t.Error("a favorita não aparece na lista")
	}
	m.msgs = nil
	runCmd(m, m.open(m.list[0]))
	if m.sel() < 0 || m.msgs[m.sel()].ID != "m3" {
		t.Fatal("enter na favorita não pulou para a mensagem")
	}
}

func TestFixarMensagem(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	chat := "5511900000001@s.whatsapp.net"
	runCmd(m, m.open(m.chatFor(chat, "Marina")))
	for i, x := range m.msgs {
		if x.ID == "m3" {
			m.msgSel = i
		}
	}
	vpBefore := m.geom().vpH
	runCmd(m, m.openMsgMenu())
	idx := -1
	for i, o := range m.popup.opts {
		if o.label == "Fixar…" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("menu sem Fixar…")
	}
	runCmd(m, m.keyPopup(fmt.Sprint(idx+1)))
	runCmd(m, m.keyPopup("2"))  // 7 dias
	runCmd(m, m.loadPins(chat)) // (o EvPins chega pelo canal de eventos)
	if len(m.pins) != 1 || m.pins[0].ID != "m3" {
		t.Fatalf("fixadas: %+v", m.pins)
	}
	if m.geom().vpH != vpBefore-1 {
		t.Error("a faixa não ganhou sua linha")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Marina Alves: Umas 15h") {
		t.Error("faixa da fixada não aparece")
	}
	m.msgSel = -1
	runCmd(m, m.jumpPin())
	if m.sel() < 0 || m.msgs[m.sel()].ID != "m3" {
		t.Fatal("alt+p não pulou para a fixada")
	}
}

func TestDadosDaMensagem(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	runCmd(m, m.open(m.chatFor("5511900000001@s.whatsapp.net", "Marina")))
	runCmd(m, m.openMsgInfo(m.msgs[len(m.msgs)-1]))
	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"Dados da mensagem", "Lida por (1)", "Reações (1)", "👍"} {
		if !strings.Contains(screen, want) {
			t.Errorf("faltou %q", want)
		}
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.msgInfo != nil {
		t.Error("tecla não fechou")
	}
}

func TestSelecaoEEncaminharParaVarios(t *testing.T) {
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
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	runCmd(m, m.open(m.chatFor("5511900000001@s.whatsapp.net", "Marina")))
	shift := func(code rune) { m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModShift}) }
	shift(tea.KeyUp)
	shift(tea.KeyUp)
	// m7, m6 e m5, mas a m5 é "mensagem apagada": fica de fora.
	if got := len(m.selectedMsgs()); got != 2 {
		t.Fatalf("selecionadas: %d", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "2 selecionadas") {
		t.Error("rodapé sem a contagem")
	}
	runCmd(m, m.startForward())
	if len(m.forwarding) != 2 || !m.searching {
		t.Fatal("encaminhar não pegou a seleção")
	}
	m.toggleTarget(m.chatFor("5511900000002@s.whatsapp.net", "Diego"))
	m.toggleTarget(m.chatFor("5511900000003@s.whatsapp.net", "Bia"))
	runCmd(m, m.sendForward())
	got := demo.LastSent(srv)
	if got.Op != ipc.OpForward || got.Chat != "5511900000003@s.whatsapp.net" || got.Msg != "m7" {
		t.Fatalf("último encaminhamento: %+v", got)
	}
	if m.forwarding != nil || m.selAnchor >= 0 {
		t.Error("estado não foi limpo")
	}
	// Seta sem shift desfaz a seleção.
	shift(tea.KeyUp)
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if _, _, ok := m.selRange(); ok {
		t.Error("seta sem shift não desfez")
	}
}
