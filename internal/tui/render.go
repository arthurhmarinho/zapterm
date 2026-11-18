package tui

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
	"unicode"

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
	// não lidas de chat silenciado ficam cinza, como no app
	sBadgeMuted = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(cDim)
	sTabOn      = lipgloss.NewStyle().Foreground(cAccent).Bold(true).Underline(true)

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

// avatarFallback desenha um quadrado 4×2 com as iniciais na cor do chat,
// usado enquanto (ou quando) não há foto de perfil.
func avatarFallback(jid, name string) string {
	ini := initials(name)
	h := fnv.New32a()
	h.Write([]byte(jid))
	bg := lipgloss.Color(senderPalette[h.Sum32()%uint32(len(senderPalette))])
	st := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("0")).Bold(true)
	return st.Render(" "+ini+strings.Repeat(" ", 3-lipgloss.Width(ini))) + "\n" + st.Render("    ")
}

func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		i := slices.IndexFunc(r, func(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) })
		if i < 0 {
			continue
		}
		c := r[i]
		if lipgloss.Width(string(c)) != 1 {
			continue // letra larga (CJK) quebraria o quadrado
		}
		out = append(out, []rune(strings.ToUpper(string(c)))...)
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

// renderChatItem: avatar à esquerda; à direita, nome + hora e prévia + não
// lidas. Ocupa itemH linhas.
func renderChatItem(c ipc.Chat, w int, selected, focused bool) string {
	marker := " "
	if selected {
		if focused {
			marker = sAccent.Render("▌")
		} else {
			marker = sDim.Render("▌")
		}
	}
	av := avatarFallback(c.JID, c.Name)
	iw := w - 1 - 4 - 1 // marcador, avatar, espaço

	name := c.Name
	if c.Pinned {
		name = "📌 " + name
	}
	if selected {
		name = sBold.Render(name)
	}
	when := shortTime(c.LastTS)
	name = ansi.Truncate(name, iw-lipgloss.Width(when)-1, "…")
	l1 := name + strings.Repeat(" ", max(1, iw-lipgloss.Width(name)-lipgloss.Width(when))) + sDim.Render(when)

	badge := ""
	if c.Unread > 0 {
		st := sBadge
		if c.Muted {
			st = sBadgeMuted
		}
		badge = st.Render(fmt.Sprintf(" %d ", c.Unread))
	} else if c.Muted {
		badge = sDim.Render("🔕")
	}
	prev := ansi.Truncate(oneLine(c.LastText), iw-lipgloss.Width(badge)-1, "…")
	l2 := sDim.Render(prev) + strings.Repeat(" ", max(1, iw-lipgloss.Width(prev)-lipgloss.Width(badge))) + badge

	return lipgloss.JoinHorizontal(lipgloss.Top, marker+"\n"+marker, av, " ", l1+"\n"+l2)
}

// renderMessages monta a conversa inteira como balões: os seus à direita,
// os dos outros à esquerda, com separador quando o dia muda. Devolve também
// a linha inicial e final de cada balão, para o cursor de mensagem rolar até
// ele.
func renderMessages(msgs []ipc.Message, w int, isGroup bool, sel int, imgs *images) (string, [][2]int) {
	if w < 10 {
		return "", nil
	}
	maxBubble := max(20, w*3/4)
	var out []string
	spans := make([][2]int, len(msgs))
	line := 0
	var lastDay time.Time
	for i, m := range msgs {
		t := time.UnixMilli(m.TS)
		if i == 0 || !sameDay(t, lastDay) {
			label := " " + dayLabel(t) + " "
			out = append(out, sDim.Render(lipgloss.PlaceHorizontal(w, lipgloss.Center, label, lipgloss.WithWhitespaceChars("─"))))
			line++
			lastDay = t
		}
		b := renderBubble(m, w, maxBubble, isGroup, i == sel, imgs)
		h := lipgloss.Height(b)
		spans[i] = [2]int{line, line + h - 1}
		line += h
		out = append(out, b)
	}
	return strings.Join(out, "\n"), spans
}

func renderQuote(q *ipc.Quote, inner int) string {
	bar := sAccent.Render("▎")
	name := q.SenderName
	if name == "" {
		name = "mensagem"
	}
	return bar + senderStyle(q.Sender).Render(ansi.Truncate(name, inner-1, "…")) + "\n" +
		bar + sDim.Render(ansi.Truncate(oneLine(q.Text), inner-1, "…"))
}

func renderBubble(m ipc.Message, w, maxBubble int, isGroup, selected bool, imgs *images) string {
	inner := maxBubble - 4 // borda + padding

	// Imagem e figurinha já baixadas aparecem no balão; a legenda vem
	// embaixo, sem o marcador "[imagem]".
	pic := ""
	if md := m.Media; md != nil && md.Preview != "" && imgs != nil {
		maxCols := min(inner, 44)
		if m.Kind == "sticker" {
			maxCols = min(inner, 16)
		}
		pic = imgs.block(md.Preview, md.Width, md.Height, maxCols)
	}
	text := m.Text
	if pic != "" {
		text = stripTag(text)
	} else if m.Kind != "text" && m.Kind != "other" {
		hint := ""
		switch {
		case m.Media != nil:
			hint = " · ctrl+o abre"
		case m.HasRaw:
			hint = " · ctrl+o baixa"
		}
		text = sDim.Render(text + hint)
	} else if m.Kind != "text" {
		text = sDim.Render(text)
	}
	body := ansi.Wrap(text, inner, " ")
	stamp := sDim.Render(time.UnixMilli(m.TS).Format("15:04"))

	var lines []string
	if isGroup && !m.FromMe {
		lines = append(lines, senderStyle(m.Sender).Render(ansi.Truncate(m.SenderName, inner, "…")))
	}
	if m.Quote != nil {
		lines = append(lines, renderQuote(m.Quote, inner))
	}
	if pic != "" {
		lines = append(lines, pic)
	}
	if body != "" {
		lines = append(lines, body)
	}
	content := strings.Join(lines, "\n")
	cw := min(inner, max(lipgloss.Width(content), lipgloss.Width(stamp)))
	content += "\n" + lipgloss.PlaceHorizontal(cw, lipgloss.Right, stamp)

	border := cDim
	align := lipgloss.Left
	if m.FromMe {
		border, align = cOwn, lipgloss.Right
	}
	style := lipgloss.RoundedBorder()
	if selected {
		border, style = cAccent, lipgloss.ThickBorder()
	}
	bubble := lipgloss.NewStyle().
		Border(style).
		BorderForeground(border).
		Padding(0, 1).
		Render(content)
	return lipgloss.PlaceHorizontal(w, align, bubble)
}

// stripTag tira o marcador "[imagem]" do começo e deixa só a legenda.
func stripTag(s string) string {
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i >= 0 {
			return strings.TrimSpace(s[i+1:])
		}
	}
	return s
}
