// Package tui é a interface de terminal do zapterm, em Bubble Tea v2.
// Ela não fala com o WhatsApp: tudo passa pelo daemon via ipc.
package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

type pane int

const (
	paneList pane = iota
	paneChat
)

type mode int

const (
	modeNormal mode = iota
	modeInsert
	modeJump
)

const pageSize = 100

type model struct {
	cli *ipc.Client

	w, h  int
	focus pane
	mode  mode

	chats  []ipc.Chat
	cursor int
	offset int

	active      string // jid do chat aberto
	activeName  string
	activeGroup bool
	msgs        []ipc.Message
	loadingOld  bool
	noMoreOld   bool
	vp          viewport.Model
	input       textarea.Model

	jump    textinput.Model
	matches []int // índices em chats
	jumpSel int

	status  string
	flash   string
	flashAt time.Time
}

// ---------- mensagens internas ----------

type (
	chatsMsg []ipc.Chat
	msgsMsg  struct {
		chat  string
		msgs  []ipc.Message
		older bool
	}
	evMsg struct {
		cli *ipc.Client
		f   ipc.Frame
	}
	goneMsg      struct{ cli *ipc.Client }
	reconnectMsg struct {
		cli *ipc.Client
		err error
	}
	errMsg    struct{ err error }
	statusMsg ipc.Status
)

func Run() error {
	cli, err := connect()
	if err != nil {
		return err
	}
	defer func() { cli.Close() }()

	m := &model{cli: cli, input: newInput(), jump: newJump(), vp: newVP(), status: "conectando"}
	_, err = tea.NewProgram(m).Run()
	return err
}

func newInput() textarea.Model {
	in := textarea.New()
	in.Placeholder = "i para escrever…"
	in.ShowLineNumbers = false
	in.Prompt = ""
	in.CharLimit = 0
	in.DynamicHeight = true
	in.MinHeight = 1
	in.MaxHeight = 6
	// enter envia; alt+enter / ctrl+j quebram linha.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	in.SetHeight(1)
	return in
}

func newJump() textinput.Model {
	j := textinput.New()
	j.Prompt = "› "
	j.Placeholder = "nome do chat"
	return j
}

func newVP() viewport.Model { return viewport.New() }

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.loadChats(), m.loadStatus(), waitEvent(m.cli))
}

// ---------- comandos (rodam fora do loop de UI) ----------

func waitEvent(cli *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-cli.Events()
		if !ok {
			return goneMsg{cli}
		}
		return evMsg{cli, f}
	}
}

func (m *model) loadChats() tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		var out []ipc.Chat
		if err := cli.Call(ipc.Request{Op: ipc.OpChats}, &out); err != nil {
			return errMsg{err}
		}
		return chatsMsg(out)
	}
}

func (m *model) loadStatus() tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		var st ipc.Status
		if err := cli.Call(ipc.Request{Op: ipc.OpStatus}, &st); err != nil {
			return errMsg{err}
		}
		return statusMsg(st)
	}
}

func (m *model) loadMessages(chat string, before int64) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		var out []ipc.Message
		if err := cli.Call(ipc.Request{Op: ipc.OpMessages, Chat: chat, Limit: pageSize, Before: before}, &out); err != nil {
			return errMsg{err}
		}
		return msgsMsg{chat: chat, msgs: out, older: before > 0}
	}
}

func (m *model) markRead(chat string) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		if err := cli.Call(ipc.Request{Op: ipc.OpRead, Chat: chat}, nil); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *model) send(chat, text string) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		// A mensagem enviada volta como evento; aqui só interessa o erro.
		if err := cli.Call(ipc.Request{Op: ipc.OpSend, Chat: chat, Text: text}, nil); err != nil {
			return errMsg{fmt.Errorf("não enviou: %w", err)}
		}
		return nil
	}
}

func reconnect() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		cli, err := connect()
		return reconnectMsg{cli, err}
	})
}

// ---------- update ----------

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		m.refreshMessages(true)
		return m, nil

	case tea.KeyPressMsg:
		return m, m.onKey(msg)

	case chatsMsg:
		sel := m.selectedJID()
		m.chats = msg
		m.restoreCursor(sel)
		return m, nil

	case statusMsg:
		m.status = msg.State
		return m, nil

	case msgsMsg:
		return m, m.onMessages(msg)

	case evMsg:
		if msg.cli != m.cli {
			return m, nil
		}
		return m, tea.Batch(m.onEvent(msg.f), waitEvent(m.cli))

	case goneMsg:
		if msg.cli != m.cli {
			return m, nil
		}
		m.status = "daemon caiu, reconectando"
		return m, reconnect()

	case reconnectMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error())
			return m, reconnect()
		}
		m.cli.Close()
		m.cli = msg.cli
		cmds := []tea.Cmd{m.loadChats(), m.loadStatus(), waitEvent(m.cli)}
		if m.active != "" {
			cmds = append(cmds, m.loadMessages(m.active, 0))
		}
		return m, tea.Batch(cmds...)

	case errMsg:
		m.setFlash(msg.err.Error())
		return m, nil
	}
	return m, nil
}

func (m *model) onEvent(f ipc.Frame) tea.Cmd {
	switch f.Event {
	case ipc.EvStatus:
		var st ipc.Status
		if json.Unmarshal(f.Data, &st) == nil {
			m.status = st.State
		}
	case ipc.EvReload:
		return m.loadChats()
	case ipc.EvChat:
		var c ipc.Chat
		if json.Unmarshal(f.Data, &c) != nil {
			return nil
		}
		sel := m.selectedJID()
		if i := m.chatIndex(c.JID); i >= 0 {
			m.chats[i] = c
		} else {
			m.chats = append(m.chats, c)
		}
		slices.SortStableFunc(m.chats, func(a, b ipc.Chat) int { return int(b.LastTS/1000 - a.LastTS/1000) })
		m.restoreCursor(sel)
		if c.JID == m.active {
			m.activeName = c.Name
		}
	case ipc.EvMessage:
		var msg ipc.Message
		if json.Unmarshal(f.Data, &msg) != nil || msg.Chat != m.active {
			return nil
		}
		if slices.ContainsFunc(m.msgs, func(x ipc.Message) bool { return x.ID == msg.ID }) {
			return nil
		}
		m.msgs = append(m.msgs, msg)
		m.refreshMessages(m.vp.AtBottom() || msg.FromMe)
		if !msg.FromMe {
			// Chat aberto na tela = lido, como no app.
			return m.markRead(m.active)
		}
	}
	return nil
}

func (m *model) onMessages(msg msgsMsg) tea.Cmd {
	if msg.chat != m.active {
		return nil
	}
	if !msg.older {
		m.msgs = msg.msgs
		m.noMoreOld = len(msg.msgs) < pageSize
		m.refreshMessages(true)
		return nil
	}
	m.loadingOld = false
	if len(msg.msgs) == 0 {
		m.noMoreOld = true
		return nil
	}
	// Mantém o mesmo trecho na tela depois de inserir as antigas em cima.
	before := m.vp.TotalLineCount()
	off := m.vp.YOffset()
	m.msgs = append(msg.msgs, m.msgs...)
	m.refreshMessages(false)
	m.vp.SetYOffset(off + m.vp.TotalLineCount() - before)
	return nil
}

// ---------- teclas ----------

func (m *model) onKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		return tea.Quit
	}
	switch m.mode {
	case modeInsert:
		return m.keyInsert(k)
	case modeJump:
		return m.keyJump(k)
	}

	switch k.String() {
	case "q":
		return tea.Quit
	case "ctrl+p", "/":
		m.openJump()
		return nil
	case "tab":
		if m.focus == paneList && m.active != "" {
			m.focus = paneChat
		} else {
			m.focus = paneList
		}
		return nil
	}
	if m.focus == paneList {
		return m.keyList(k)
	}
	return m.keyChat(k)
}

func (m *model) keyList(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "ctrl+d", "pgdown":
		m.moveCursor(m.listRows() / 2)
	case "ctrl+u", "pgup":
		m.moveCursor(-m.listRows() / 2)
	case "g", "home":
		m.moveCursor(-len(m.chats))
	case "G", "end":
		m.moveCursor(len(m.chats))
	case "enter", "l", "right":
		if c, ok := m.selected(); ok {
			return m.open(c)
		}
	}
	return nil
}

func (m *model) keyChat(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "h", "left":
		m.focus = paneList
		return nil
	case "i", "a", "enter":
		m.mode = modeInsert
		m.layout()
		return m.input.Focus()
	case "j", "down":
		m.vp.ScrollDown(1)
	case "k", "up":
		m.vp.ScrollUp(1)
	case "ctrl+d", "pgdown":
		m.vp.ScrollDown(m.vp.Height() / 2)
	case "ctrl+u", "pgup":
		m.vp.ScrollUp(m.vp.Height() / 2)
	case "g", "home":
		m.vp.GotoTop()
	case "G", "end":
		m.vp.GotoBottom()
	}
	return m.maybeLoadOlder()
}

func (m *model) keyInsert(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		m.layout()
		return nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" || m.active == "" {
			return nil
		}
		m.input.Reset()
		m.layout()
		return m.send(m.active, text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	m.layout() // a altura do campo acompanha o texto
	return cmd
}

func (m *model) keyJump(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		return nil
	case "enter":
		m.mode = modeNormal
		if m.jumpSel < len(m.matches) {
			c := m.chats[m.matches[m.jumpSel]]
			m.cursor = m.matches[m.jumpSel]
			m.scrollList()
			return m.open(c)
		}
		return nil
	case "down", "ctrl+n", "ctrl+j":
		m.jumpSel = min(m.jumpSel+1, max(0, len(m.matches)-1))
		return nil
	case "up", "ctrl+k":
		m.jumpSel = max(m.jumpSel-1, 0)
		return nil
	}
	var cmd tea.Cmd
	m.jump, cmd = m.jump.Update(k)
	m.filterJump()
	return cmd
}

// ---------- ações ----------

func (m *model) open(c ipc.Chat) tea.Cmd {
	m.focus = paneChat
	if c.JID == m.active {
		return m.markRead(c.JID)
	}
	m.active, m.activeName = c.JID, c.Name
	m.activeGroup = strings.HasSuffix(c.JID, "@g.us")
	m.msgs = nil
	m.loadingOld, m.noMoreOld = false, false
	m.input.Reset()
	m.refreshMessages(true)
	cmds := []tea.Cmd{m.loadMessages(c.JID, 0)}
	if c.Unread > 0 {
		cmds = append(cmds, m.markRead(c.JID))
	}
	return tea.Batch(cmds...)
}

func (m *model) maybeLoadOlder() tea.Cmd {
	if !m.vp.AtTop() || m.loadingOld || m.noMoreOld || len(m.msgs) == 0 {
		return nil
	}
	m.loadingOld = true
	return m.loadMessages(m.active, m.msgs[0].TS)
}

func (m *model) openJump() {
	m.mode = modeJump
	m.jump.Reset()
	m.jump.Focus()
	m.filterJump()
}

type chatNames []ipc.Chat

func (c chatNames) String(i int) string { return c[i].Name }
func (c chatNames) Len() int            { return len(c) }

func (m *model) filterJump() {
	m.jumpSel = 0
	q := m.jump.Value()
	m.matches = m.matches[:0]
	if q == "" {
		for i := range min(len(m.chats), 10) {
			m.matches = append(m.matches, i)
		}
		return
	}
	for _, r := range fuzzy.FindFrom(q, chatNames(m.chats)) {
		m.matches = append(m.matches, r.Index)
		if len(m.matches) == 10 {
			break
		}
	}
}

// ---------- lista ----------

func (m *model) selected() (ipc.Chat, bool) {
	if m.cursor < 0 || m.cursor >= len(m.chats) {
		return ipc.Chat{}, false
	}
	return m.chats[m.cursor], true
}

func (m *model) selectedJID() string {
	c, _ := m.selected()
	return c.JID
}

func (m *model) chatIndex(jid string) int {
	return slices.IndexFunc(m.chats, func(c ipc.Chat) bool { return c.JID == jid })
}

func (m *model) restoreCursor(jid string) {
	if i := m.chatIndex(jid); i >= 0 {
		m.cursor = i
	}
	m.cursor = max(0, min(m.cursor, len(m.chats)-1))
	m.scrollList()
}

func (m *model) moveCursor(d int) {
	m.cursor = max(0, min(m.cursor+d, len(m.chats)-1))
	m.scrollList()
}

func (m *model) scrollList() {
	rows := m.listRows()
	if rows <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
}

func (m *model) setFlash(s string) {
	m.flash, m.flashAt = s, time.Now()
}

// ---------- layout ----------

func (m *model) listWidth() int { return max(24, min(42, m.w*30/100)) }
func (m *model) listRows() int  { return (m.h - 1 - 2) / 2 }

func (m *model) inputHeight() int { return m.input.Height() + 2 }

func (m *model) layout() {
	if m.w == 0 {
		return
	}
	cw := m.w - m.listWidth()
	m.input.SetWidth(cw - 2)
	m.vp.SetWidth(cw - 2)
	m.vp.SetHeight(max(1, m.h-1-m.inputHeight()-2))
	m.scrollList()
}

func (m *model) refreshMessages(toBottom bool) {
	if m.vp.Width() <= 0 {
		return
	}
	var content string
	switch {
	case m.active == "":
		content = sDim.Render("\n  Escolha um chat (enter) ou pule com ctrl+p.")
	case len(m.msgs) == 0:
		content = sDim.Render("\n  Carregando…")
	default:
		content = renderMessages(m.msgs, m.vp.Width(), m.activeGroup)
	}
	m.vp.SetContent(content)
	if toBottom {
		m.vp.GotoBottom()
	}
}

// ---------- view ----------

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.WindowTitle = m.title()
	if m.w < 50 || m.h < 12 {
		v.SetContent("janela pequena demais para o zapterm")
		return v
	}

	lw := m.listWidth()
	cw := m.w - lw
	list := m.viewList(lw, m.h-1)

	chatTitle := "2"
	if m.active != "" {
		chatTitle = "2 · " + m.activeName
	}
	chat := box(chatTitle, m.vp.View(), cw, m.h-1-m.inputHeight(), m.focus == paneChat && m.mode != modeInsert)
	input := box("3", m.input.View(), cw, m.inputHeight(), m.mode == modeInsert)

	screen := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, list, lipgloss.JoinVertical(lipgloss.Left, chat, input)),
		m.viewFooter(),
	)
	if m.mode == modeJump {
		pop := m.viewJump()
		x := (m.w - lipgloss.Width(pop)) / 2
		y := max(1, m.h/5)
		screen = lipgloss.NewCompositor(
			lipgloss.NewLayer(screen),
			lipgloss.NewLayer(pop).X(x).Y(y).Z(1),
		).Render()
	}
	v.SetContent(screen)
	return v
}

func (m *model) title() string {
	n := 0
	for _, c := range m.chats {
		if c.Unread > 0 {
			n++
		}
	}
	if n == 0 {
		return "zapterm"
	}
	return fmt.Sprintf("zapterm (%d)", n)
}

func (m *model) viewList(w, h int) string {
	rows := m.listRows()
	var items []string
	for i := m.offset; i < min(len(m.chats), m.offset+rows); i++ {
		items = append(items, renderChatItem(m.chats[i], w-2, i == m.cursor, m.focus == paneList && m.mode == modeNormal))
	}
	title := fmt.Sprintf("1 · %d chats", len(m.chats))
	return box(title, strings.Join(items, "\n"), w, h, m.focus == paneList && m.mode == modeNormal)
}

func (m *model) viewJump() string {
	w := max(40, min(70, m.w/2))
	lines := []string{m.jump.View(), ""}
	for i, idx := range m.matches {
		c := m.chats[idx]
		line := "  " + c.Name
		if i == m.jumpSel {
			line = sAccent.Render("▸ ") + sBold.Render(c.Name)
		}
		lines = append(lines, line, "    "+sDim.Render(oneLine(c.LastText)))
	}
	if len(m.matches) == 0 {
		lines = append(lines, sDim.Render("  nada encontrado"))
	}
	return box("Pular para chat", strings.Join(lines, "\n"), w, len(lines)+2, true)
}

func (m *model) viewFooter() string {
	var help string
	switch {
	case m.mode == modeInsert:
		help = "enter enviar · alt+enter nova linha · esc sair da escrita"
	case m.mode == modeJump:
		help = "digite para filtrar · ↑/↓ mover · enter abrir · esc fechar"
	case m.focus == paneList:
		help = "j/k mover · enter abrir · ctrl+p pular · tab painel · q sair"
	default:
		help = "j/k rolar · ctrl+u/d página · g/G topo/fim · i escrever · esc lista"
	}
	left := sAccent.Render(help)
	if m.flash != "" && time.Since(m.flashAt) < 6*time.Second {
		left = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(m.flash)
	}

	dot := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("●")
	switch m.status {
	case "online":
		dot = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("●")
	case "offline", "logged_out":
		dot = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("●")
	}
	right := dot + " " + m.status
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}
