package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// ---------- teclado ----------

func (m *model) onKey(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if s != "ctrl+c" {
		m.quitArmed = false
	}
	if m.viewing != nil {
		switch s {
		case "o":
			if err := openExternal(m.viewing.media.Path); err != nil {
				m.setFlash("xdg-open: " + err.Error())
			}
		case "esc", "enter", "q", "ctrl+o", "ctrl+c", "space":
			m.viewing = nil
		}
		return nil
	}
	if m.searching {
		return m.keySearch(k)
	}
	if m.picking {
		return m.keyStickers(k)
	}
	if m.reacting {
		return m.keyReact(s)
	}
	if m.rec != nil {
		switch s {
		case "enter", "ctrl+t":
			return m.finishRec(true)
		case "esc", "ctrl+c":
			return m.finishRec(false)
		}
		return nil // gravando: o resto das teclas fica de fora
	}

	switch s {
	case "ctrl+c":
		// Como no Claude Code: limpa o que está escrito; com tudo vazio, o
		// segundo ctrl+c sai.
		if m.input.Value() != "" || m.replyTo != nil || m.attach != nil {
			m.input.Reset()
			m.replyTo, m.attach = nil, nil
			m.layout()
			return nil
		}
		if m.quitArmed {
			return tea.Quit
		}
		m.quitArmed = true
		m.setFlash("ctrl+c de novo para sair")
		return nil
	case "ctrl+p", "ctrl+f":
		m.startSearch()
		return nil
	case "ctrl+s":
		return m.openStickers()
	case "ctrl+t":
		return m.startRec()
	case "ctrl+e":
		if m.active != "" && m.sel() >= 0 && m.msgs[m.sel()].Kind != "revoked" {
			m.reacting, m.picking = true, false
			m.layout()
		}
		return nil
	case "alt+f":
		return m.openChooser()
	case "ctrl+v":
		if m.active == "" {
			m.setFlash("abra um chat antes de colar")
			return nil
		}
		return pasteClipboard()
	case "tab", "shift+tab":
		if m.focus == paneList && m.active != "" {
			m.focus = paneChat
		} else {
			m.focus = paneList
		}
		return nil
	case "alt+1", "alt+2", "alt+3", "alt+4":
		m.setTab(int(s[4] - '1'))
		return nil
	case "alt+a":
		m.setArchived(!m.archived)
		return nil
	case "esc":
		switch {
		case m.attach != nil:
			m.attach = nil
			m.layout()
		case m.replyTo != nil:
			m.replyTo = nil
			m.layout()
		case m.focus == paneChat && m.msgSel >= 0:
			m.setSel(len(m.msgs) - 1)
		case m.focus == paneChat:
			m.focus = paneList
		case m.archived:
			m.setArchived(false)
		case m.searchQuery() != "":
			m.stopSearch(true)
		}
		return nil
	case "ctrl+r":
		return m.startReply()
	case "ctrl+o":
		if m.active != "" {
			return m.openMedia(m.sel())
		}
		return nil
	case "enter":
		return m.onEnter()
	case "pgup":
		return m.scrollChat(-m.vp.Height() / 2)
	case "pgdown":
		return m.scrollChat(m.vp.Height() / 2)
	case "ctrl+home":
		if m.focus == paneList {
			m.moveCursor(-len(m.list))
			return nil
		}
		m.vp.GotoTop()
		m.selFromViewport(true)
		return m.maybeLoadOlder()
	case "ctrl+end":
		if m.focus == paneList {
			m.moveCursor(len(m.list))
			return nil
		}
		m.setSel(len(m.msgs) - 1)
		m.vp.GotoBottom()
		return nil
	case "up", "down":
		// Com texto de várias linhas, as setas andam dentro do texto.
		if !strings.Contains(m.input.Value(), "\n") {
			d := 1
			if s == "up" {
				d = -1
			}
			if m.focus == paneList {
				m.moveCursor(d)
				return nil
			}
			m.setSel(m.sel() + d)
			return m.maybeLoadOlder()
		}
	}

	// Todo o resto é texto.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	m.layout() // a altura do campo acompanha o texto
	return cmd
}

func (m *model) keyStickers(k tea.KeyPressMsg) tea.Cmd {
	per := m.stickPerRow()
	n := len(m.stickers)
	switch k.String() {
	case "esc", "ctrl+s", "ctrl+c":
		m.closeStickers()
		return nil
	case "enter":
		return m.sendSticker(m.stickSel)
	case "left":
		m.stickSel--
	case "right":
		m.stickSel++
	case "up":
		m.stickSel -= per
	case "down":
		m.stickSel += per
	default:
		return nil
	}
	if n == 0 {
		m.stickSel = 0
		return nil
	}
	m.stickSel = max(0, min(m.stickSel, n-1))
	// Duas linhas visíveis: rola a grade para manter a escolhida à vista.
	row := m.stickSel / per
	if row < m.stickTop {
		m.stickTop = row
	} else if row > m.stickTop+1 {
		m.stickTop = row - 1
	}
	return nil
}

func (m *model) keySearch(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.stopSearch(true)
		return nil
	case "enter":
		if c, ok := m.selected(); ok {
			return m.open(c)
		}
		return nil
	case "up", "ctrl+k":
		m.moveCursor(-1)
		return nil
	case "down", "ctrl+j", "ctrl+n", "tab":
		m.moveCursor(1)
		return nil
	}
	var cmd tea.Cmd
	before := m.search.Value()
	m.search, cmd = m.search.Update(k)
	if m.search.Value() != before {
		m.cursor, m.offset = 0, 0
		m.applyFilter()
	}
	return cmd
}

func (m *model) onEnter() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if a := m.attach; a != nil {
		// send_file decide pelo tipo: foto, vídeo, áudio ou documento.
		req := ipc.Request{Op: ipc.OpSendFile, Chat: m.active, Path: a.path, Text: text}
		if m.replyTo != nil {
			req.ReplyTo = m.replyTo.ID
		}
		m.attach, m.replyTo, m.msgSel = nil, nil, -1
		m.input.Reset()
		m.layout()
		m.setFlash("enviando " + a.name + "…")
		return m.send(req)
	}
	if text == "" {
		if m.focus == paneList {
			if c, ok := m.selected(); ok {
				return m.open(c)
			}
		}
		return nil
	}
	if m.active == "" {
		m.setFlash("abra um chat antes de enviar")
		return nil
	}
	req := ipc.Request{Op: ipc.OpSend, Chat: m.active, Text: text}
	if m.replyTo != nil {
		req.ReplyTo = m.replyTo.ID
	}
	m.replyTo, m.msgSel = nil, -1
	m.input.Reset()
	m.layout()
	return m.send(req)
}

func (m *model) startReply() tea.Cmd {
	i := m.sel()
	if m.active == "" || i < 0 {
		return nil
	}
	msg := m.msgs[i]
	m.replyTo = &msg
	m.layout()
	return nil
}

func (m *model) scrollChat(d int) tea.Cmd {
	if d < 0 {
		m.vp.ScrollUp(-d)
	} else {
		m.vp.ScrollDown(d)
	}
	if m.focus == paneChat {
		m.selFromViewport(d < 0)
	}
	return m.maybeLoadOlder()
}

type chatNames []ipc.Chat

func (c chatNames) String(i int) string { return c[i].Name }
func (c chatNames) Len() int            { return len(c) }

func fuzzyChats(q string, chats []ipc.Chat) []int {
	var out []int
	for _, r := range fuzzy.FindFrom(q, chatNames(chats)) {
		out = append(out, r.Index)
	}
	return out
}

// ---------- mouse ----------

func (m *model) onClick(c tea.MouseClickMsg) tea.Cmd {
	if m.viewing != nil {
		m.viewing = nil
		return nil
	}
	g := m.geom()
	x, y := c.X, c.Y
	if x < g.listW {
		return m.clickList(g, x, y)
	}
	if m.searching {
		m.stopSearch(false)
	}
	if m.active == "" {
		return nil
	}
	if m.picking && y >= g.extraY && y < g.inputY {
		if i := m.stickerAt(g, x, y); i >= 0 {
			return m.sendSticker(i)
		}
		return nil
	}
	switch {
	case y >= g.vpY && y < g.vpY+g.vpH:
		wasChat := m.focus == paneChat
		m.focus = paneChat
		i := m.msgAtLine(y - g.vpY + m.vp.YOffset())
		if i < 0 {
			return nil
		}
		if c.Button == tea.MouseRight {
			m.setSel(i)
			return m.startReply()
		}
		// Clique na mensagem já selecionada abre a mídia dela.
		if wasChat && i == m.sel() {
			return m.openMedia(i)
		}
		m.setSel(i)
	case y >= g.inputY:
		m.focus = paneChat
		// Botões dentro da caixa: figurinha e clipe.
		switch {
		case abs(x-g.stickerBtnX) <= 1:
			return m.openStickers()
		case abs(x-g.clipBtnX) <= 1:
			return m.openChooser()
		}
	}
	return nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// keyReact: 1–6 escolhem a reação, 0 tira a sua, esc fecha.
func (m *model) keyReact(s string) tea.Cmd {
	close := func() {
		m.reacting = false
		m.layout()
	}
	switch {
	case s == "esc" || s == "ctrl+e" || s == "ctrl+c":
		close()
		return nil
	case len(s) == 1 && s[0] >= '0' && s[0] <= '6':
		i := m.sel()
		if i < 0 {
			close()
			return nil
		}
		emoji := ""
		if s[0] != '0' {
			emoji = quickReactions[s[0]-'1']
		}
		close()
		return m.call(ipc.Request{Op: ipc.OpReact, Chat: m.active, Msg: m.msgs[i].ID, Text: emoji}, nil, nil)
	}
	return nil
}

func (m *model) openChooser() tea.Cmd {
	if m.active == "" {
		m.setFlash("abra um chat antes de anexar")
		return nil
	}
	m.picking = false
	m.layout()
	m.setFlash("escolha o arquivo na janela que abriu…")
	return chooseFile()
}

func (m *model) clickList(g geometry, x, y int) tea.Cmd {
	m.focus = paneList
	switch {
	case y == g.titleY && m.archived:
		m.setArchived(false)
	case y >= g.searchY && y < g.searchY+3:
		m.startSearch()
	case y == g.chipsY && !m.archived:
		if t := m.chipAt(x); t >= 0 {
			if m.searching || m.searchQuery() != "" {
				m.stopSearch(true)
			}
			m.setTab(t)
		}
	case g.archRowY >= 0 && y == g.archRowY:
		m.setArchived(true)
	case y >= g.itemsY && y < g.itemsY+g.listRows*itemH:
		i := m.offset + (y-g.itemsY)/itemH
		if i < len(m.list) {
			m.cursor = i
			return m.open(m.list[i])
		}
	}
	return nil
}

func (m *model) onWheel(w tea.MouseWheelMsg) tea.Cmd {
	if m.viewing != nil {
		return nil
	}
	g := m.geom()
	d := 3
	switch w.Button {
	case tea.MouseWheelUp:
		d = -3
	case tea.MouseWheelDown:
	default:
		return nil
	}
	if w.X < g.listW {
		// A roda rola a lista sem mexer na seleção.
		step := 1
		if d < 0 {
			step = -1
		}
		m.offset = max(0, min(m.offset+step, len(m.list)-m.listRows()))
		return nil
	}
	if d < 0 {
		m.vp.ScrollUp(-d)
	} else {
		m.vp.ScrollDown(d)
	}
	return m.maybeLoadOlder()
}
