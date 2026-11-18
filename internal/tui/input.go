package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// ---------- teclado ----------

func (m *model) onKey(k tea.KeyPressMsg) tea.Cmd {
	if m.jumping {
		return m.keyJump(k)
	}
	s := k.String()
	if s != "ctrl+c" {
		m.quitArmed = false
	}

	switch s {
	case "ctrl+c":
		// Como no Claude Code: limpa o que está escrito; com o campo vazio,
		// o segundo ctrl+c sai.
		if m.input.Value() != "" || m.replyTo != nil {
			m.input.Reset()
			m.replyTo = nil
			m.layout()
			return nil
		}
		if m.quitArmed {
			return tea.Quit
		}
		m.quitArmed = true
		m.setFlash("ctrl+c de novo para sair")
		return nil
	case "ctrl+p":
		m.openJump()
		return nil
	case "tab", "shift+tab":
		if m.focus == paneList && m.active != "" {
			m.focus = paneChat
		} else {
			m.focus = paneList
		}
		return nil
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5":
		m.setTab(int(s[4] - '1'))
		return nil
	case "esc":
		switch {
		case m.replyTo != nil:
			m.replyTo = nil
			m.layout()
		case m.focus == paneChat && m.msgSel >= 0:
			m.setSel(len(m.msgs) - 1)
		default:
			m.focus = paneList
		}
		return nil
	case "ctrl+r":
		return m.startReply()
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

func (m *model) onEnter() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
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
	reply := ""
	if m.replyTo != nil {
		reply = m.replyTo.ID
	}
	m.replyTo = nil
	m.msgSel = -1
	m.input.Reset()
	m.layout()
	return m.send(m.active, text, reply)
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

func (m *model) keyJump(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.closeJump()
		return nil
	case "enter":
		m.closeJump()
		if m.jumpSel < len(m.matches) {
			c := m.chats[m.matches[m.jumpSel]]
			// Pular para um chat de outra aba leva para "Todos" ou para a
			// aba dele, para o cursor da lista acompanhar.
			if !inTab(c, m.tab) {
				for t := range numTabs {
					if inTab(c, t) {
						m.setTab(t)
						break
					}
				}
			}
			for i, x := range m.list {
				if x.JID == c.JID {
					m.cursor = i
					m.scrollList()
				}
			}
			return m.open(c)
		}
		return nil
	case "down", "ctrl+n", "ctrl+j":
		m.jumpSel = min(m.jumpSel+1, max(0, len(m.matches)-1))
		return nil
	case "up", "ctrl+k", "ctrl+p":
		m.jumpSel = max(m.jumpSel-1, 0)
		return nil
	}
	var cmd tea.Cmd
	m.jump, cmd = m.jump.Update(k)
	m.filterJump()
	return cmd
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
	g := m.geom()
	x, y := c.X, c.Y
	if m.jumping {
		m.closeJump()
		return nil
	}
	switch {
	case x < g.listW && y == g.tabsY:
		if t := m.tabAt(x); t >= 0 {
			m.setTab(t)
		}
		return nil

	case x < g.listW && y >= g.itemsY && y < g.itemsY+g.listRows*itemH:
		i := m.offset + (y-g.itemsY)/itemH
		if i < len(m.list) {
			m.cursor = i
			m.focus = paneList
			return m.open(m.list[i])
		}
		return nil

	case x >= g.listW && y >= g.vpY && y < g.vpY+m.vp.Height():
		if m.active == "" {
			return nil
		}
		m.focus = paneChat
		i := m.msgAtLine(y - g.vpY + m.vp.YOffset())
		if i < 0 {
			return nil
		}
		m.setSel(i)
		if c.Button == tea.MouseRight {
			return m.startReply()
		}
		return nil

	case x >= g.listW && y >= g.inputY:
		if m.active != "" {
			m.focus = paneChat
		}
	}
	return nil
}

func (m *model) onWheel(w tea.MouseWheelMsg) tea.Cmd {
	g := m.geom()
	d := 3
	if w.Button == tea.MouseWheelUp {
		d = -3
	} else if w.Button != tea.MouseWheelDown {
		return nil
	}
	if m.jumping {
		return nil
	}
	if w.X < g.listW {
		// A roda rola a lista sem mexer na seleção.
		m.offset = max(0, min(m.offset+d/3*2, len(m.list)-m.listRows()))
		return nil
	}
	if d < 0 {
		m.vp.ScrollUp(-d)
	} else {
		m.vp.ScrollDown(d)
	}
	return m.maybeLoadOlder()
}
