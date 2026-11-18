package tui

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Cores ANSI 0–15: a TUI herda o tema do terminal em vez de impor o seu.
var (
	cAccent = lipgloss.Color("5")
	cDim    = lipgloss.Color("8")
	cOwn    = lipgloss.Color("6")

	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sAccent = lipgloss.NewStyle().Foreground(cAccent)
	sBold   = lipgloss.NewStyle().Bold(true)
	sBadge  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(cAccent).Bold(true)

	senderPalette = []string{"1", "2", "3", "4", "5", "6", "9", "10", "11", "12", "13", "14"}
)

func senderStyle(jid string) lipgloss.Style {
	h := fnv.New32a()
	h.Write([]byte(jid))
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(senderPalette[h.Sum32()%uint32(len(senderPalette))]))
}

// box desenha uma moldura arredondada com o título embutido na borda de
// cima, como no tuigram. body é cortado/preenchido para caber em w×h.
func box(title, body string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	bc := lipgloss.NewStyle().Foreground(cDim)
	if focused {
		bc = lipgloss.NewStyle().Foreground(cAccent)
	}
	iw, ih := w-2, h-2

	title = ansi.Truncate(title, iw-3, "…")
	top := "╭"
	if title != "" {
		top += "─ " + title + " "
	}
	top += strings.Repeat("─", max(0, w-1-lipgloss.Width(top))) + "╮"

	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(bc.Render(top))
	for i := 0; i < ih; i++ {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], iw, "")
		}
		pad := iw - lipgloss.Width(line)
		b.WriteString("\n" + bc.Render("│") + line + strings.Repeat(" ", max(0, pad)) + bc.Render("│"))
	}
	b.WriteString("\n" + bc.Render("╰"+strings.Repeat("─", iw)+"╯"))
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func shortTime(ms int64) string {
	if ms == 0 {
		return ""
	}
	t := time.UnixMilli(ms)
	now := time.Now()
	switch {
	case sameDay(t, now):
		return t.Format("15:04")
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "ontem"
	case now.Sub(t) < 6*24*time.Hour:
		return [...]string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}[t.Weekday()]
	}
	return t.Format("02/01/06")
}

func dayLabel(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Hoje"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Ontem"
	}
	return t.Format("02/01/2006")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// renderChatItem: duas linhas, nome + hora e prévia + não lidas.
func renderChatItem(c ipc.Chat, w int, selected, focused bool) string {
	marker := "  "
	name := c.Name
	if selected {
		if focused {
			marker = sAccent.Render("▌ ")
		} else {
			marker = sDim.Render("▌ ")
		}
		name = sBold.Render(name)
	}
	iw := w - 2
	when := shortTime(c.LastTS)
	name = ansi.Truncate(name, iw-lipgloss.Width(when)-1, "…")
	l1 := name + strings.Repeat(" ", max(1, iw-lipgloss.Width(name)-lipgloss.Width(when))) + sDim.Render(when)

	badge := ""
	if c.Unread > 0 {
		badge = sBadge.Render(fmt.Sprintf(" %d ", c.Unread))
	}
	prev := ansi.Truncate(oneLine(c.LastText), iw-lipgloss.Width(badge)-1, "…")
	l2 := sDim.Render(prev) + strings.Repeat(" ", max(1, iw-lipgloss.Width(prev)-lipgloss.Width(badge))) + badge
	return marker + l1 + "\n" + "  " + l2
}

// renderMessages monta a conversa inteira como balões: os seus à direita,
// os dos outros à esquerda, com separador quando o dia muda.
func renderMessages(msgs []ipc.Message, w int, isGroup bool) string {
	if w < 10 {
		return ""
	}
	maxBubble := max(20, w*3/4)
	var out []string
	var lastDay time.Time
	for i, m := range msgs {
		t := time.UnixMilli(m.TS)
		if i == 0 || !sameDay(t, lastDay) {
			label := " " + dayLabel(t) + " "
			out = append(out, sDim.Render(lipgloss.PlaceHorizontal(w, lipgloss.Center, label, lipgloss.WithWhitespaceChars("─"))))
			lastDay = t
		}
		out = append(out, renderBubble(m, w, maxBubble, isGroup))
	}
	return strings.Join(out, "\n")
}

func renderBubble(m ipc.Message, w, maxBubble int, isGroup bool) string {
	inner := maxBubble - 4 // borda + padding
	text := m.Text
	if m.Kind != "text" {
		text = sDim.Render(text)
	}
	body := ansi.Wrap(text, inner, " ")
	stamp := sDim.Render(time.UnixMilli(m.TS).Format("15:04"))

	var lines []string
	if isGroup && !m.FromMe {
		lines = append(lines, senderStyle(m.Sender).Render(ansi.Truncate(m.SenderName, inner, "…")))
	}
	lines = append(lines, body)
	content := strings.Join(lines, "\n")
	cw := min(inner, max(lipgloss.Width(content), lipgloss.Width(stamp)))
	content += "\n" + lipgloss.PlaceHorizontal(cw, lipgloss.Right, stamp)

	border := cDim
	align := lipgloss.Left
	if m.FromMe {
		border, align = cOwn, lipgloss.Right
	}
	bubble := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Render(content)
	return lipgloss.PlaceHorizontal(w, align, bubble)
}
