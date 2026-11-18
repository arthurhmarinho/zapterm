// Package tui é a interface de terminal do zapterm, em Bubble Tea v2.
// Ela não fala com o WhatsApp: tudo passa pelo daemon via ipc.
//
// Navegação no estilo do Claude Code: o campo de escrita está sempre ativo,
// setas/PgUp/PgDn/Tab e o mouse navegam, atalhos usam ctrl/alt.
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

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

type pane int

const (
	paneList pane = iota
	paneChat
)

// Abas da lista de chats.
const (
	tabAll = iota
	tabUnread
	tabGroups
	tabChannels
	tabArchived
	numTabs
)

// Três tamanhos de rótulo: usa o maior que couber na largura da lista.
var tabNames = [numTabs][3]string{
	{"Todos", "Todos", "Tod"},
	{"Não lidas", "N.lidas", "NL"},
	{"Grupos", "Grupos", "Gr"},
	{"Canais", "Canais", "Can"},
	{"Arquivados", "Arq.", "Arq"},
}

const pageSize = 100

type model struct {
	cli *ipc.Client

	w, h  int
	focus pane

	chats  []ipc.Chat // todos, como vieram do daemon
	list   []ipc.Chat // filtrados pela aba atual
	tab    int
	cursor int // em list
	offset int

	active     string // jid do chat aberto
	activeChat ipc.Chat
	msgs       []ipc.Message
	spans      [][2]int // linhas de cada balão no viewport
	msgSel     int      // mensagem sob o cursor; -1 = seguir a última
	replyTo    *ipc.Message
	loadingOld bool
	noMoreOld  bool
	vp         viewport.Model
	input      textarea.Model

	jumping bool
	jump    textinput.Model
	matches []int // índices em chats
	jumpSel int

	status  string
	flash   string
	flashAt time.Time

	imgs *images

	termFocused bool   // o kitty avisa quando a janela ganha/perde foco
	sentViewing string // último valor de "viewing" mandado ao daemon
	quitArmed   bool   // ctrl+c com campo vazio pede confirmação
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
	mediaMsg  struct {
		chat, id string
		media    *ipc.Media
		open     bool // abrir no programa externo quando chegar
	}
)

func Run() error {
	cli, err := connect()
	if err != nil {
		return err
	}
	m := newModel(cli)
	defer func() { m.cli.Close() }()
	_, err = tea.NewProgram(m).Run()
	m.imgs.cleanup()
	return err
}

func newModel(cli *ipc.Client) *model {
	m := &model{cli: cli, input: newInput(), jump: newJump(), vp: newVP(),
		status: "conectando", termFocused: true, msgSel: -1, imgs: newImages()}
	m.input.Focus()
	return m
}

func newInput() textarea.Model {
	in := textarea.New()
	in.Placeholder = "Escreva uma mensagem…"
	in.ShowLineNumbers = false
	in.Prompt = ""
	in.CharLimit = 0
	in.DynamicHeight = true
	in.MinHeight = 1
	in.MaxHeight = 6
	// enter envia; shift+enter / alt+enter / ctrl+j quebram linha.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	in.SetHeight(1)
	st := in.Styles()
	st.Focused.CursorLine = st.Focused.CursorLine.UnsetBackground()
	in.SetStyles(st)
	return in
}

func newJump() textinput.Model {
	j := textinput.New()
	j.Prompt = "› "
	j.Placeholder = "nome do chat"
	return j
}

func newVP() viewport.Model {
	vp := viewport.New()
	vp.MouseWheelEnabled = false // a roda é tratada em input.go
	return vp
}

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

// call faz um pedido ao daemon fora do loop de UI; wrap transforma a
// resposta (já decodificada em out) na mensagem que volta para o Update.
func (m *model) call(req ipc.Request, out any, wrap func() tea.Msg) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		if err := cli.Call(req, out); err != nil {
			return errMsg{err}
		}
		if wrap == nil {
			return nil
		}
		return wrap()
	}
}

func (m *model) loadChats() tea.Cmd {
	var out []ipc.Chat
	return m.call(ipc.Request{Op: ipc.OpChats}, &out, func() tea.Msg { return chatsMsg(out) })
}

func (m *model) loadStatus() tea.Cmd {
	var st ipc.Status
	return m.call(ipc.Request{Op: ipc.OpStatus}, &st, func() tea.Msg { return statusMsg(st) })
}

func (m *model) loadMessages(chat string, before int64) tea.Cmd {
	var out []ipc.Message
	return m.call(ipc.Request{Op: ipc.OpMessages, Chat: chat, Limit: pageSize, Before: before}, &out,
		func() tea.Msg { return msgsMsg{chat: chat, msgs: out, older: before > 0} })
}

func (m *model) markRead(chat string) tea.Cmd {
	return m.call(ipc.Request{Op: ipc.OpRead, Chat: chat}, nil, nil)
}

func (m *model) send(chat, text, replyTo string) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		// A mensagem enviada volta como evento; aqui só interessa o erro.
		if err := cli.Call(ipc.Request{Op: ipc.OpSend, Chat: chat, Text: text, ReplyTo: replyTo}, nil); err != nil {
			return errMsg{fmt.Errorf("não enviou: %w", err)}
		}
		return nil
	}
}

// syncViewing conta ao daemon qual chat está na tela, para ele não notificar
// mensagem que você já está vendo.
func (m *model) syncViewing() tea.Cmd {
	want := ""
	if m.termFocused {
		want = m.active
	}
	if want == m.sentViewing {
		return nil
	}
	m.sentViewing = want
	return m.call(ipc.Request{Op: ipc.OpViewing, Chat: want}, nil, nil)
}

func reconnect() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		cli, err := connect()
		return reconnectMsg{cli, err}
	})
}

// ---------- update ----------

// Update envolve update para mandar ao kitty as imagens que o desenho
// acabou de pedir.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	if f := m.imgs.flush(); f != nil {
		// A transmissão vai antes do quadro que usa os placeholders.
		return m, tea.Sequence(f, cmd)
	}
	return m, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layout()
		m.refreshMessages(true)
		return m, nil

	case tea.KeyPressMsg:
		prev := m.focus
		cmd := m.onKey(msg)
		m.afterNav(prev)
		return m, tea.Batch(cmd, m.syncViewing())

	case tea.MouseClickMsg:
		prev := m.focus
		cmd := m.onClick(msg)
		m.afterNav(prev)
		return m, tea.Batch(cmd, m.syncViewing())

	case tea.MouseWheelMsg:
		return m, m.onWheel(msg)

	case tea.PasteMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.layout()
		return m, cmd

	case tea.FocusMsg:
		m.termFocused = true
		cmds := []tea.Cmd{m.syncViewing()}
		if m.activeChat.Unread > 0 {
			// Voltou para a janela com mensagem nova no chat aberto.
			cmds = append(cmds, m.markRead(m.active))
		}
		return m, tea.Batch(cmds...)

	case tea.BlurMsg:
		m.termFocused = false
		return m, m.syncViewing()

	case chatsMsg:
		m.chats = msg
		m.sortChats()
		m.applyFilter()
		if c, ok := m.chatByJID(m.active); ok {
			m.activeChat = c
		}
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
		m.sentViewing = ""
		cmds := []tea.Cmd{m.loadChats(), m.loadStatus(), waitEvent(m.cli), m.syncViewing()}
		if m.active != "" {
			cmds = append(cmds, m.loadMessages(m.active, 0))
		}
		return m, tea.Batch(cmds...)

	case mediaMsg:
		return m, m.onMedia(msg)

	case errMsg:
		m.setFlash(msg.err.Error())
		return m, nil
	}
	return m, nil
}

// ---------- mídia ----------

// openMedia: mídia já baixada abre no programa externo (foto em tela
// cheia, vídeo, PDF); a não baixada é baixada e, se for imagem no kitty,
// aparece no balão.
func (m *model) openMedia(i int) tea.Cmd {
	if i < 0 || i >= len(m.msgs) {
		return nil
	}
	msg := m.msgs[i]
	if msg.Kind == "text" || msg.Kind == "other" {
		return nil
	}
	if msg.Media != nil {
		if err := openExternal(msg.Media.Path); err != nil {
			m.setFlash("xdg-open: " + err.Error())
		}
		return nil
	}
	if !msg.HasRaw {
		m.setFlash("mídia de mensagem antiga (do pareamento) não pode ser baixada")
		return nil
	}
	m.setFlash("baixando…")
	chat, id := m.active, msg.ID
	isPic := msg.Kind == "image" || msg.Kind == "sticker"
	open := !(isPic && m.imgs.enabled)
	var md ipc.Media
	return m.call(ipc.Request{Op: ipc.OpMedia, Chat: chat, Msg: id}, &md,
		func() tea.Msg { return mediaMsg{chat: chat, id: id, media: &md, open: open} })
}

func (m *model) onMedia(msg mediaMsg) tea.Cmd {
	m.flash = ""
	if msg.open {
		if err := openExternal(msg.media.Path); err != nil {
			m.setFlash("xdg-open: " + err.Error())
		}
	}
	if msg.chat != m.active {
		return nil
	}
	for i := range m.msgs {
		if m.msgs[i].ID == msg.id {
			m.msgs[i].Media = msg.media
			off := m.vp.YOffset()
			atBottom := m.vp.AtBottom()
			m.refreshMessages(atBottom)
			if !atBottom {
				m.vp.SetYOffset(off)
			}
			m.ensureSelVisible()
		}
	}
	return nil
}

// afterNav redesenha a conversa quando o foco de painel muda: o destaque da
// mensagem só existe com o painel da conversa em foco.
func (m *model) afterNav(prev pane) {
	if m.focus != prev {
		off := m.vp.YOffset()
		m.refreshMessages(false)
		m.vp.SetYOffset(off)
	}
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
		if i := m.chatIndex(c.JID); i >= 0 {
			m.chats[i] = c
		} else {
			m.chats = append(m.chats, c)
		}
		m.sortChats()
		m.applyFilter()
		if c.JID == m.active {
			m.activeChat = c
		}
	case ipc.EvMessage:
		var msg ipc.Message
		if json.Unmarshal(f.Data, &msg) != nil || msg.Chat != m.active {
			return nil
		}
		if slices.ContainsFunc(m.msgs, func(x ipc.Message) bool { return x.ID == msg.ID }) {
			return nil
		}
		following := m.msgSel < 0 || m.msgSel == len(m.msgs)-1
		m.msgs = append(m.msgs, msg)
		if following {
			m.msgSel = -1
		}
		m.refreshMessages(m.vp.AtBottom() || msg.FromMe)
		if !msg.FromMe && m.termFocused {
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
	if m.msgSel >= 0 {
		m.msgSel += len(msg.msgs)
	}
	m.msgs = append(msg.msgs, m.msgs...)
	m.refreshMessages(false)
	m.vp.SetYOffset(off + m.vp.TotalLineCount() - before)
	return nil
}

// ---------- ações ----------

func (m *model) open(c ipc.Chat) tea.Cmd {
	m.focus = paneChat
	if c.JID == m.active {
		if c.Unread > 0 {
			return m.markRead(c.JID)
		}
		return nil
	}
	m.active, m.activeChat = c.JID, c
	m.msgs = nil
	m.msgSel, m.replyTo = -1, nil
	m.loadingOld, m.noMoreOld = false, false
	m.layout()
	m.refreshMessages(true)
	cmds := []tea.Cmd{m.loadMessages(c.JID, 0)}
	if c.Unread > 0 {
		cmds = append(cmds, m.markRead(c.JID))
	}
	return tea.Batch(cmds...)
}

func (m *model) maybeLoadOlder() tea.Cmd {
	if !(m.vp.AtTop() || m.sel() == 0) || m.loadingOld || m.noMoreOld || len(m.msgs) == 0 {
		return nil
	}
	m.loadingOld = true
	return m.loadMessages(m.active, m.msgs[0].TS)
}

func (m *model) setFlash(s string) {
	m.flash, m.flashAt = s, time.Now()
}

// ---------- lista e filtros ----------

// sortChats: fixados primeiro, depois o mais recente, como no app.
func (m *model) sortChats() {
	slices.SortStableFunc(m.chats, func(a, b ipc.Chat) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		switch {
		case a.LastTS > b.LastTS:
			return -1
		case a.LastTS < b.LastTS:
			return 1
		}
		return 0
	})
}

func inTab(c ipc.Chat, tab int) bool {
	switch tab {
	case tabUnread:
		return c.Unread > 0 && !c.Archived && c.Kind != "channel"
	case tabGroups:
		return c.Kind == "group" && !c.Archived
	case tabChannels:
		return c.Kind == "channel"
	case tabArchived:
		return c.Archived
	}
	return !c.Archived && c.Kind != "channel"
}

// tabCount conta os chats não lidos de uma aba.
func (m *model) tabCount(tab int) int {
	n := 0
	for _, c := range m.chats {
		if inTab(c, tab) && c.Unread > 0 {
			n++
		}
	}
	return n
}

// applyFilter refaz a lista visível mantendo o cursor no mesmo chat.
func (m *model) applyFilter() {
	sel := m.selectedJID()
	list := make([]ipc.Chat, 0, len(m.chats))
	for _, c := range m.chats {
		if inTab(c, m.tab) {
			list = append(list, c)
		}
	}
	m.list = list
	if i := slices.IndexFunc(m.list, func(c ipc.Chat) bool { return c.JID == sel }); i >= 0 {
		m.cursor = i
	}
	m.cursor = max(0, min(m.cursor, len(m.list)-1))
	m.scrollList()
}

func (m *model) setTab(t int) {
	m.focus = paneList
	if t == m.tab {
		return
	}
	m.tab = t
	m.cursor, m.offset = 0, 0
	m.list = nil
	m.applyFilter()
}

func (m *model) selected() (ipc.Chat, bool) {
	if m.cursor < 0 || m.cursor >= len(m.list) {
		return ipc.Chat{}, false
	}
	return m.list[m.cursor], true
}

func (m *model) selectedJID() string {
	c, _ := m.selected()
	return c.JID
}

func (m *model) chatIndex(jid string) int {
	return slices.IndexFunc(m.chats, func(c ipc.Chat) bool { return c.JID == jid })
}

func (m *model) chatByJID(jid string) (ipc.Chat, bool) {
	if i := m.chatIndex(jid); i >= 0 {
		return m.chats[i], true
	}
	return ipc.Chat{}, false
}

func (m *model) moveCursor(d int) {
	m.cursor = max(0, min(m.cursor+d, len(m.list)-1))
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
	m.offset = max(0, min(m.offset, len(m.list)-rows))
}

// ---------- mensagens: cursor ----------

// sel devolve o índice real da mensagem sob o cursor.
func (m *model) sel() int {
	if len(m.msgs) == 0 {
		return -1
	}
	if m.msgSel < 0 || m.msgSel >= len(m.msgs) {
		return len(m.msgs) - 1
	}
	return m.msgSel
}

func (m *model) setSel(i int) {
	if len(m.msgs) == 0 {
		return
	}
	i = max(0, min(i, len(m.msgs)-1))
	m.msgSel = i
	if i == len(m.msgs)-1 {
		m.msgSel = -1
	}
	off := m.vp.YOffset()
	m.refreshMessages(false)
	m.vp.SetYOffset(off)
	m.ensureSelVisible()
}

// selFromViewport põe o cursor na primeira (ou última) mensagem visível
// depois de rolar por página.
func (m *model) selFromViewport(top bool) {
	y0, y1 := m.vp.YOffset(), m.vp.YOffset()+m.vp.Height()-1
	pick := -1
	for i, sp := range m.spans {
		if sp[1] >= y0 && sp[0] <= y1 {
			pick = i
			if top {
				break
			}
		}
	}
	if pick >= 0 {
		m.msgSel = pick
		off := m.vp.YOffset()
		m.refreshMessages(false)
		m.vp.SetYOffset(off)
	}
}

func (m *model) ensureSelVisible() {
	i := m.sel()
	if i < 0 || i >= len(m.spans) {
		return
	}
	sp := m.spans[i]
	h := m.vp.Height()
	switch {
	case sp[1]-sp[0]+1 > h, sp[0] < m.vp.YOffset():
		// balão acima da tela, ou maior que ela: mostra o começo dele
		m.vp.SetYOffset(sp[0])
	case sp[1] > m.vp.YOffset()+h-1:
		m.vp.SetYOffset(sp[1] - h + 1)
	}
}

func (m *model) msgAtLine(line int) int {
	for i, sp := range m.spans {
		if line >= sp[0] && line <= sp[1] {
			return i
		}
	}
	return -1
}

func (m *model) refreshMessages(toBottom bool) {
	if m.vp.Width() <= 0 {
		return
	}
	var content string
	switch {
	case m.active == "":
		content = sDim.Render("\n  Escolha um chat na lista (clique ou enter) ou pule com ctrl+p.")
		m.spans = nil
	case len(m.msgs) == 0:
		content = sDim.Render("\n  Carregando…")
		m.spans = nil
	default:
		sel := -2 // sem destaque fora do painel da conversa
		if m.focus == paneChat {
			sel = m.sel()
		}
		content, m.spans = renderMessages(m.msgs, m.vp.Width(), m.activeChat.Kind == "group", sel, m.imgs)
	}
	m.vp.SetContent(content)
	if toBottom {
		m.vp.GotoBottom()
	}
}

// ---------- salto (ctrl+p) ----------

func (m *model) openJump() {
	m.jumping = true
	m.jump.Reset()
	m.jump.Focus()
	m.input.Blur()
	m.filterJump()
}

func (m *model) closeJump() {
	m.jumping = false
	m.jump.Blur()
	m.input.Focus()
}

func (m *model) filterJump() {
	m.jumpSel = 0
	m.matches = m.matches[:0]
	q := strings.TrimSpace(m.jump.Value())
	if q == "" {
		for i := range min(len(m.chats), 10) {
			m.matches = append(m.matches, i)
		}
		return
	}
	for _, r := range fuzzyChats(q, m.chats) {
		m.matches = append(m.matches, r)
		if len(m.matches) == 10 {
			break
		}
	}
}
