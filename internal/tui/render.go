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

// render monta a conversa inteira (fundo pintado) e devolve também a linha
// inicial e final de cada balão, para o cursor e o clique.
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
			add(blank(cBgChat, r.w, 1))
			add(paint(lipgloss.PlaceHorizontal(r.w, lipgloss.Center,
				pill(dayLabel(t), cText2, cDayPill, cBgChat, false)), cBgChat, r.w))
			lastDay = t
		}
		// Em grupo, avatar e nome só na primeira mensagem de uma sequência
		// do mesmo remetente, como no app; entre sequências, uma linha vazia.
		firstOfRun := newDay || msgs[i-1].Sender != m.Sender || msgs[i-1].FromMe != m.FromMe
		if firstOfRun {
			add(blank(cBgChat, r.w, 1))
		}
		spans[i][0] = line
		add(r.bubbleRow(m, firstOfRun, i == r.sel))
		spans[i][1] = line - 1
	}
	add(blank(cBgChat, r.w, 1))
	return strings.Join(out, "\n"), spans
}

func (r msgRenderer) bubbleRow(m ipc.Message, firstOfRun, selected bool) string {
	maxW := max(20, r.w*65/100)
	left := ""
	if !m.FromMe && r.isGroup {
		maxW -= avatarSmallCols + 1
		left = strings.Repeat(" ", avatarSmallCols)
		if firstOfRun && r.avatar != nil {
			left = r.avatar(m.Sender, m.SenderName)
		}
	}
	b := r.bubble(m, maxW, firstOfRun && r.isGroup && !m.FromMe, selected)
	if selected {
		// Barra de destaque na cor de acento do tema, colada ao balão.
		bar := strings.TrimSuffix(strings.Repeat(fg(cGreen).Render("▌")+"\n", lipgloss.Height(b)), "\n")
		b = lipgloss.JoinHorizontal(lipgloss.Top, bar, b)
	} else {
		b = lipgloss.JoinHorizontal(lipgloss.Top, " ", b)
	}
	bw := lipgloss.Width(b)
	var row string
	switch {
	case m.FromMe:
		row = lipgloss.JoinHorizontal(lipgloss.Top, strings.Repeat(" ", max(0, r.w-bw-2)), b)
	case left != "":
		row = lipgloss.JoinHorizontal(lipgloss.Top, " ", left, " ", b)
	default:
		row = lipgloss.JoinHorizontal(lipgloss.Top, " ", b)
	}
	return paint(row, cBgChat, r.w)
}

// bubble desenha o balão: nome (grupo), citação, imagem, texto e hora,
// tudo sobre o fundo verde (seu) ou cinza (dos outros), sem borda.
func (r msgRenderer) bubble(m ipc.Message, maxW int, showName, selected bool) string {
	bg, qbg, timeSt := cBubbleIn, cQuoteIn, sText2
	if m.FromMe {
		bg, qbg, timeSt = cBubbleOut, cQuoteOut, fg(cTimeOut)
	}
	if selected {
		bg = cBubbleInSel
		if m.FromMe {
			bg = cBubbleOutSel
		}
	}
	inner := maxW - 2 // um espaço de cada lado

	pic := ""
	if md := m.Media; md != nil && md.Preview != "" && r.imgs != nil {
		cols, rows := min(inner, 44), 22
		if m.Kind == "sticker" {
			cols, rows = min(inner, 16), 8
		}
		pic = r.imgs.block(md.Preview, md.Width, md.Height, cols, rows)
	}
	text := m.Text
	switch {
	case pic != "":
		text = stripTag(text)
	case m.Kind != "text" && m.Kind != "other":
		hint := ""
		switch {
		case m.Media != nil:
			hint = "  · ctrl+o abre"
		case m.HasRaw:
			hint = "  · ctrl+o baixa"
		}
		text = mediaLabel(text) + sText2.Render(hint)
	}
	body := ""
	if text != "" {
		body = sText.Render(ansi.Wrap(text, inner, " "))
	}
	stamp := time.UnixMilli(m.TS).Format("15:04")

	var parts []string
	if showName {
		parts = append(parts, fg(senderColor(m.Sender)).Bold(true).Render(ansi.Truncate(m.SenderName, inner, "…")))
	}
	quoteAt := -1
	if m.Quote != nil {
		quoteAt = len(parts)
		parts = append(parts, "") // a citação entra depois, com a largura final
	}
	if pic != "" {
		parts = append(parts, pic)
	}
	if body != "" {
		parts = append(parts, body)
	}

	// Largura do balão: a do conteúdo, no mínimo a da hora.
	cw := lipgloss.Width(stamp) + 2
	for _, p := range parts {
		cw = max(cw, lipgloss.Width(p))
	}
	if m.Quote != nil {
		cw = max(cw, min(inner, lipgloss.Width(oneLine(m.Quote.Text))+2))
	}
	cw = min(cw, inner)
	if quoteAt >= 0 {
		parts[quoteAt] = quoteBlock(m.Quote, cw, qbg)
	}

	parts = append(parts, lipgloss.PlaceHorizontal(cw, lipgloss.Right, timeSt.Render(stamp)))
	var padded []string
	for _, l := range strings.Split(strings.Join(parts, "\n"), "\n") {
		padded = append(padded, " "+l)
	}
	return paint(strings.Join(padded, "\n"), bg, cw+2)
}

// quoteBlock: citação dentro do balão (ou acima do campo de escrita), com
// barra na cor do autor.
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
