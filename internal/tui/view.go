package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

const (
	itemH   = 2 // linhas por chat na lista
	headerH = 2 // cabeçalho da conversa (avatar + nome)
)

// geometry concentra as coordenadas da tela, usadas pelo desenho e pelo
// clique do mouse. Mudou o layout, muda aqui.
type geometry struct {
	listW, chatW  int
	tabsY, itemsY int
	listRows      int
	chatH, inputH int
	headerY, vpY  int
	inputY        int
}

func (m *model) geom() geometry {
	g := geometry{}
	g.listW = max(28, min(44, m.w*30/100))
	g.chatW = m.w - g.listW
	g.tabsY = 1
	g.itemsY = 3 // borda, abas, linha
	g.listRows = max(0, (m.h-1-2-2)/itemH)
	g.inputH = m.input.Height() + 2
	if m.replyTo != nil {
		g.inputH += 2
	}
	g.chatH = m.h - 1 - g.inputH
	g.headerY = 1
	g.vpY = 1 + headerH + 1
	g.inputY = g.chatH
	return g
}

func (m *model) listRows() int { return m.geom().listRows }

func (m *model) layout() {
	if m.w == 0 {
		return
	}
	g := m.geom()
	m.input.SetWidth(g.chatW - 2)
	m.vp.SetWidth(g.chatW - 2)
	m.vp.SetHeight(max(1, g.chatH-2-headerH-1))
	m.scrollList()
}

// ---------- view ----------

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.title()
	if m.w < 60 || m.h < 14 {
		v.SetContent("janela pequena demais para o zapterm")
		return v
	}
	g := m.geom()

	list := m.viewList(g)
	chat := box("", m.viewHeader(g.chatW-2)+"\n"+sDim.Render(strings.Repeat("─", g.chatW-2))+"\n"+m.vp.View(),
		g.chatW, g.chatH, m.focus == paneChat)
	input := box("", m.viewInput(g.chatW-2), g.chatW, g.inputH, true)

	screen := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, list, lipgloss.JoinVertical(lipgloss.Left, chat, input)),
		m.viewFooter(),
	)
	if m.jumping {
		pop := m.viewJump()
		x := (m.w - lipgloss.Width(pop)) / 2
		y := max(1, m.h/5)
		screen = lipgloss.NewCompositor(
			lipgloss.NewLayer(screen),
			lipgloss.NewLayer(pop).X(x).Y(y).Z(1),
		).Render()
	}
	v.SetContent(screen)

	// Cursor do campo de escrita (o textarea calcula a posição relativa).
	if !m.jumping {
		if c := m.input.Cursor(); c != nil {
			c.Position.X += g.listW + 1
			c.Position.Y += g.inputY + 1
			if m.replyTo != nil {
				c.Position.Y += 2
			}
			v.Cursor = c
		}
	}
	return v
}

func (m *model) title() string {
	n := m.tabCount(tabUnread)
	if n == 0 {
		return "zapterm"
	}
	return fmt.Sprintf("zapterm (%d)", n)
}

// tabLabels devolve os rótulos e onde cada um começa (para o clique).
func (m *model) tabLabels(width int) ([]string, []int) {
	for size := range 3 {
		var labels []string
		var starts []int
		x := 1 // borda
		for i, t := range tabNames {
			l := t[size]
			if i == tabUnread {
				if n := m.tabCount(tabUnread); n > 0 {
					l += fmt.Sprintf("·%d", n)
				}
			}
			labels = append(labels, l)
			starts = append(starts, x)
			x += lipgloss.Width(l) + 1
		}
		if x-2 <= width || size == 2 {
			return labels, starts
		}
	}
	return nil, nil
}

func (m *model) tabAt(x int) int {
	g := m.geom()
	labels, starts := m.tabLabels(g.listW - 2)
	for i := range labels {
		if x >= starts[i] && x < starts[i]+lipgloss.Width(labels[i]) {
			return i
		}
	}
	return -1
}

func (m *model) viewList(g geometry) string {
	iw := g.listW - 2
	labels, _ := m.tabLabels(iw)
	var tabs []string
	for i, l := range labels {
		if i == m.tab {
			tabs = append(tabs, sTabOn.Render(l))
		} else {
			tabs = append(tabs, sDim.Render(l))
		}
	}
	lines := []string{strings.Join(tabs, " "), sDim.Render(strings.Repeat("─", iw))}

	focused := m.focus == paneList
	for i := m.offset; i < min(len(m.list), m.offset+g.listRows); i++ {
		lines = append(lines, renderChatItem(m.list[i], iw, i == m.cursor, focused))
	}
	if len(m.list) == 0 {
		lines = append(lines, "", sDim.Render("  nada por aqui"))
	}
	return box("", strings.Join(lines, "\n"), g.listW, m.h-1, focused)
}

func (m *model) viewHeader(w int) string {
	if m.active == "" {
		return sDim.Render(" zapterm") + "\n"
	}
	c := m.activeChat
	sub := map[string]string{"dm": "conversa", "group": "grupo", "channel": "canal"}[c.Kind]
	if c.Muted {
		sub += " · silenciado"
	}
	if c.Archived {
		sub += " · arquivado"
	}
	av := avatarFallback(c.JID, c.Name)
	name := ansi.Truncate(sBold.Render(c.Name), w-6, "…")
	return lipgloss.JoinHorizontal(lipgloss.Top, av, " ", name+"\n"+sDim.Render(sub))
}

func (m *model) viewInput(w int) string {
	body := m.input.View()
	if r := m.replyTo; r != nil {
		name := r.SenderName
		switch {
		case r.FromMe:
			name = "você"
		case m.activeChat.Kind == "channel":
			name = m.activeChat.Name
		}
		body = renderQuote(&ipc.Quote{Sender: r.Sender, SenderName: "respondendo " + name + "  (esc cancela)", Text: r.Text}, w) + "\n" + body
	}
	if m.active == "" && m.input.Value() == "" {
		body = sDim.Render("Abra um chat para escrever")
	}
	return body
}

func (m *model) viewJump() string {
	w := max(40, min(70, m.w/2))
	lines := []string{m.jump.View(), ""}
	for i, idx := range m.matches {
		c := m.chats[idx]
		tag := ""
		switch {
		case c.Archived:
			tag = sDim.Render("  arquivado")
		case c.Kind == "channel":
			tag = sDim.Render("  canal")
		}
		line := "  " + c.Name + tag
		if i == m.jumpSel {
			line = sAccent.Render("▸ ") + sBold.Render(c.Name) + tag
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
	case m.jumping:
		help = "digite para filtrar · ↑/↓ mover · enter abrir · esc fechar"
	case m.focus == paneList:
		help = "↑/↓ chats · enter abrir · tab conversa · ctrl+p pular · alt+1..5 abas · ctrl+c sair"
	default:
		help = "↑/↓ mensagem · ctrl+r responder · ctrl+o mídia · pgup/pgdn rolar · shift+enter linha · tab lista"
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
	left = ansi.Truncate(left, m.w-lipgloss.Width(right)-1, "…")
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

// tabStartX devolve a coluna onde começa o rótulo de uma aba.
func (m *model) tabStartX(tab int) int {
	_, starts := m.tabLabels(m.geom().listW - 2)
	return starts[tab]
}
