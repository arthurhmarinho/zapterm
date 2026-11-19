package tui

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// ---------- texto ----------

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func senderColor(jid string) color.Color {
	h := fnv.New32a()
	h.Write([]byte(jid))
	return lipgloss.Color(senderColors[h.Sum32()%uint32(len(senderColors))])
}

// mediaLabel troca os marcadores do daemon ("[imagem] legenda") pelos
// ícones e nomes que o app mostra ("󰋩 Foto", "󰋩 legenda").
func mediaLabel(s string) string {
	if !strings.HasPrefix(s, "[") {
		return s
	}
	end := strings.Index(s, "]")
	if end < 0 {
		return s
	}
	tag, rest := s[1:end], strings.TrimSpace(s[end+1:])
	icon, name := "", ""
	switch {
	case tag == "imagem":
		icon, name = icons.photo, "Foto"
	case tag == "vídeo":
		icon, name = icons.video, "Vídeo"
	case tag == "áudio", tag == "arquivo de áudio":
		icon, name = icons.audio, "Áudio"
	case tag == "figurinha":
		icon, name = icons.sticker, "Figurinha"
	case strings.HasPrefix(tag, "documento: "):
		icon, name = icons.doc, strings.TrimPrefix(tag, "documento: ")
	default:
		return s
	}
	if rest == "" {
		rest = name
	}
	return icon + " " + rest
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
		return "Ontem"
	case now.Sub(t) < 6*24*time.Hour:
		return [...]string{"domingo", "segunda", "terça", "quarta", "quinta", "sexta", "sábado"}[t.Weekday()]
	}
	return t.Format("02/01/2006")
}

func dayLabel(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "HOJE"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "ONTEM"
	}
	return t.Format("02/01/2006")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		r := []rune(w)
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

// textAvatar é a reserva sem kitty: bloco cols×rows na cor do chat com as
// iniciais no meio.
func textAvatar(jid, name string, cols, rows int) string {
	st := lipgloss.NewStyle().Background(senderColor(jid)).Foreground(cBgList).Bold(true)
	ini := initials(name)
	var lines []string
	for r := range rows {
		l := strings.Repeat(" ", cols)
		if r == rows/2 {
			l = lipgloss.PlaceHorizontal(cols, lipgloss.Center, ini)
		}
		lines = append(lines, st.Render(l))
	}
	return strings.Join(lines, "\n")
}

// ---------- lista ----------

// renderChatItem desenha um chat em itemH linhas: avatar redondo à
// esquerda; nome e hora; prévia, mudo, fixado e não lidas; um divisor.
func renderChatItem(c ipc.Chat, w int, selected bool, av string) string {
	bg := cBgList
	if selected {
		bg = cBgHover
	}
	tw := w - 1 - avatarCols - 2 - 1 // margem, avatar, espaço, margem direita

	when := shortTime(c.LastTS)
	whenSt := sText2
	if c.Unread > 0 {
		whenSt = sGreen
	}
	name := ansi.Truncate(oneLine(c.Name), tw-lipgloss.Width(when)-1, "…")
	l1 := sText.Render(name) + strings.Repeat(" ", max(1, tw-lipgloss.Width(name)-lipgloss.Width(when))) + whenSt.Render(when)

	var right []string
	if c.Muted {
		right = append(right, sText2.Render(icons.mute))
	}
	if c.Pinned {
		right = append(right, sText2.Render(icons.pin))
	}
	if c.Unread > 0 {
		n := fmt.Sprint(c.Unread)
		if c.Unread > 999 {
			n = "999+"
		}
		right = append(right, pill(n, cBgList, cBadge, bg, true))
	}
	r := strings.Join(right, " ")
	prev := previewLabel(oneLine(c.LastText))
	prev = strings.Replace(prev, "você: ", "Você: ", 1)
	prev = ansi.Truncate(prev, tw-lipgloss.Width(r)-1, "…")
	l2 := sText2.Render(prev) + strings.Repeat(" ", max(1, tw-lipgloss.Width(prev)-lipgloss.Width(r))) + r
	l3 := fg(cDivider).Render(strings.Repeat("─", tw))

	text := l1 + "\n" + l2 + "\n" + l3
	marker := " \n \n "
	if selected {
		marker = strings.Repeat(fg(cGreen).Render("▌")+"\n", 2) + fg(cGreen).Render("▌")
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, marker, av, "  ", text)
	return paint(row, bg, w)
}

// ---------- conversa ----------

type msgRenderer struct {
	w       int
	isGroup bool
	sel     int
	imgs    *images
	avatar  func(jid, name string) string // 4×2, para remetentes em grupo
}

// render monta a conversa inteira e devolve também a linha inicial e final
// de cada balão, para o cursor e o clique.
func (r msgRenderer) render(msgs []ipc.Message) (string, [][2]int) {
	if r.w < 20 {
		return "", nil
	}
	var out []string
	spans := make([][2]int, len(msgs))
	line := 0
	add := func(block string) {
		out = append(out, block)
		line += strings.Count(block, "\n") + 1
	}
	var lastDay time.Time
	for i, m := range msgs {
		t := time.UnixMilli(m.TS)
		newDay := i == 0 || !sameDay(t, lastDay)
		if newDay {
			label := " " + dayLabel(t) + " "
			add(sText2.Render(lipgloss.PlaceHorizontal(r.w, lipgloss.Center, label, lipgloss.WithWhitespaceChars("─"))))
			lastDay = t
		}
		// Em grupo, avatar e nome só na primeira mensagem de uma sequência
		// do mesmo remetente, como no app.
		firstOfRun := newDay || msgs[i-1].Sender != m.Sender || msgs[i-1].FromMe != m.FromMe
		spans[i][0] = line
		add(r.bubbleRow(m, firstOfRun, i == r.sel))
		spans[i][1] = line - 1
	}
	return strings.Join(out, "\n"), spans
}

func (r msgRenderer) bubbleRow(m ipc.Message, firstOfRun, selected bool) string {
	maxW := max(20, r.w*3/4)
	if m.FromMe {
		return lipgloss.PlaceHorizontal(r.w, lipgloss.Right, r.bubble(m, maxW, false, selected))
	}
	if !r.isGroup {
		return r.bubble(m, maxW, false, selected)
	}
	left := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", avatarSmallCols)+"\n", avatarSmallRows), "\n")
	if firstOfRun && r.avatar != nil {
		left = r.avatar(m.Sender, m.SenderName)
	}
	b := r.bubble(m, maxW-avatarSmallCols-1, firstOfRun, selected)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", b)
}

// bubble desenha o balão com borda arredondada, no estilo de terminal:
// seus em ciano, dos outros em cinza, o selecionado com borda grossa na
// cor de acento.
func (r msgRenderer) bubble(m ipc.Message, maxW int, showName, selected bool) string {
	inner := maxW - 4 // borda + padding

	pic := ""
	if md := m.Media; md != nil && md.Preview != "" && r.imgs != nil {
		cols, rows := min(inner, 44), 22
		if m.Kind == "sticker" {
			cols, rows = min(inner, 16), 8
		}
		pic = r.imgs.media(md, cols, rows)
	}
	text := m.Text
	switch {
	case pic != "":
		text = stripTag(text)
	case m.Kind != "text" && m.Kind != "other":
		hint := ""
		switch {
		case m.Media != nil:
			hint = " · ctrl+o abre"
		case m.HasRaw:
			hint = " · ctrl+o baixa"
		}
		text = sText2.Render(mediaLabel(text) + hint)
	case m.Kind == "other":
		text = sText2.Render(text)
	}

	var lines []string
	if showName {
		lines = append(lines, fg(senderColor(m.Sender)).Bold(true).Render(ansi.Truncate(m.SenderName, inner, "…")))
	}
	if m.Quote != nil {
		lines = append(lines, inlineQuote(m.Quote, inner))
	}
	if pic != "" {
		lines = append(lines, pic)
	}
	if text != "" {
		lines = append(lines, ansi.Wrap(text, inner, " "))
	}
	content := strings.Join(lines, "\n")
	stamp := sText2.Render(time.UnixMilli(m.TS).Format("15:04"))
	cw := min(inner, max(lipgloss.Width(content), lipgloss.Width(stamp)))
	content += "\n" + lipgloss.PlaceHorizontal(cw, lipgloss.Right, stamp)

	border, style := cText2, lipgloss.RoundedBorder()
	if m.FromMe {
		border = lipgloss.Color("6")
	}
	if selected {
		border, style = cGreen, lipgloss.ThickBorder()
	}
	return lipgloss.NewStyle().Border(style).BorderForeground(border).Padding(0, 1).Render(content)
}

// inlineQuote: citação dentro do balão, só com a barra colorida.
func inlineQuote(q *ipc.Quote, w int) string {
	c := senderColor(q.Sender)
	bar := fg(c).Render("▎")
	name := q.SenderName
	if name == "" {
		name = "mensagem"
	}
	return bar + fg(c).Bold(true).Render(ansi.Truncate(name, w-1, "…")) + "\n" +
		bar + sText2.Render(ansi.Truncate(mediaLabel(oneLine(q.Text)), w-1, "…"))
}

// quoteBlock: citação acima do campo de escrita, com barra na cor do autor.
func quoteBlock(q *ipc.Quote, w int, bg color.Color) string {
	c := senderColor(q.Sender)
	bar := fg(c).Render("▌")
	name := q.SenderName
	if name == "" {
		name = "mensagem"
	}
	l1 := bar + fg(c).Bold(true).Render(ansi.Truncate(name, w-2, "…"))
	l2 := bar + sText2.Render(ansi.Truncate(mediaLabel(oneLine(q.Text)), w-2, "…"))
	return paint(l1+"\n"+l2, bg, w)
}

// previewLabel aplica mediaLabel também depois do "Fulano: " das prévias
// de grupo.
func previewLabel(s string) string {
	if i := strings.Index(s, ": ["); i >= 0 && i < 80 {
		return s[:i+2] + mediaLabel(s[i+2:])
	}
	return mediaLabel(s)
}
