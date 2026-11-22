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
	// A duração vem junto do marcador: "[vídeo 0:32]", "[áudio 0:12]".
	dur := ""
	if m := durRe.FindStringSubmatch(tag); m != nil {
		dur = " " + m[1] + ":" + m[2]
	}
	switch {
	case tag == "imagem":
		icon, name = icons.photo, "Foto"
	case strings.HasPrefix(tag, "vídeo"):
		icon, name = icons.video, "Vídeo"+dur
	case strings.HasPrefix(tag, "áudio"), strings.HasPrefix(tag, "arquivo de áudio"):
		icon, name = icons.audio, "Áudio"+dur
	case tag == "figurinha":
		icon, name = icons.sticker, "Figurinha"
	case tag == "GIF":
		icon, name = icons.video, "GIF"
	case strings.HasPrefix(tag, "documento: "):
		icon, name = icons.doc, strings.TrimPrefix(tag, "documento: ")
	default:
		if l := otherLabel(s); l != s {
			return l
		}
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
// ticks: ✓ enviada, ✓✓ entregue, ✓✓ azul lida/ouvida; nada se não sabemos
// (mensagens antigas, de antes de existir o acompanhamento).
func ticks(status int) string {
	switch status {
	case 1:
		return sText2.Render("✓")
	case 2:
		return sText2.Render("✓✓")
	case 3, 4:
		return fg(cReadTick).Bold(true).Render("✓✓")
	}
	return ""
}

// renderChatItem: typing, se não vazio, substitui a prévia ("digitando…");
// draft, o rascunho deixado na conversa.
func renderChatItem(c ipc.Chat, w int, selected bool, av string, typing, draft string) string {
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
	shown := sText.Render(name)
	if c.Community != "" {
		// Grupo de comunidade: "Comunidade › Grupo", a comunidade apagada.
		group := oneLine(c.Name)
		if c.CommunityAnnounce {
			group = "Avisos"
		}
		name = ansi.Truncate(oneLine(c.Community)+" › "+group, tw-lipgloss.Width(when)-1, "…")
		if cut := strings.Index(name, " › "); cut >= 0 {
			shown = sText2.Render(name[:cut+len(" › ")]) + sText.Render(name[cut+len(" › "):])
		} else {
			shown = sText2.Render(name)
		}
	}
	l1 := shown + strings.Repeat(" ", max(1, tw-lipgloss.Width(name)-lipgloss.Width(when))) + whenSt.Render(when)

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
	prev := previewLabel(oneLine(plainText(c.LastText)))
	tick := ""
	if strings.HasPrefix(prev, "você: ") {
		prev = strings.TrimPrefix(prev, "você: ")
		if t := ticks(c.LastStatus); t != "" {
			tick = t + " "
		} else {
			prev = "Você: " + prev
		}
	}
	prevSt := sText2
	switch {
	case typing != "":
		prev, tick, prevSt = typing, "", sGreen
	case draft != "":
		prev, tick, prevSt = "Rascunho: "+oneLine(draft), "", sGreen
	}
	prev = ansi.Truncate(prev, tw-lipgloss.Width(r)-lipgloss.Width(tick)-1, "…")
	l2 := tick + prevSt.Render(prev) + strings.Repeat(" ", max(1, tw-lipgloss.Width(tick)-lipgloss.Width(prev)-lipgloss.Width(r))) + r
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
	w              int
	isGroup        bool
	sel            int
	imgs           *images
	avatar         func(jid, name string) string // 4×2, para remetentes em grupo
	playID         string                        // áudio tocando
	elapsed        int                           // segundos tocados
	paused         bool
	speed          float64
	selFrom, selTo int    // seleção de várias (shift+↑/↓); -1 = sem
	unreadID       string // divisor "N mensagens não lidas" antes desta
	unreadN        int
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
		if m.ID == r.unreadID && r.unreadN > 0 {
			add(sGreen.Render(lipgloss.PlaceHorizontal(r.w, lipgloss.Center, unreadLabel(r.unreadN), lipgloss.WithWhitespaceChars("─"))))
		}
		firstOfRun := newDay || msgs[i-1].Sender != m.Sender || msgs[i-1].FromMe != m.FromMe
		spans[i][0] = line
		add(r.bubbleRow(m, firstOfRun, i == r.sel || i >= r.selFrom && i <= r.selTo && r.selFrom >= 0))
		spans[i][1] = line - 1
	}
	return strings.Join(out, "\n"), spans
}

func (r msgRenderer) bubbleRow(m ipc.Message, firstOfRun, selected bool) string {
	maxW := max(20, r.w*3/4)
	if m.FromMe {
		b := withReactions(r.bubble(m, maxW, false, selected), m.Reactions, lipgloss.Right)
		return lipgloss.PlaceHorizontal(r.w, lipgloss.Right, b)
	}
	if !r.isGroup {
		return withReactions(r.bubble(m, maxW, false, selected), m.Reactions, lipgloss.Left)
	}
	left := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", avatarSmallCols)+"\n", avatarSmallRows), "\n")
	if firstOfRun && r.avatar != nil {
		left = r.avatar(m.Sender, m.SenderName)
	}
	b := withReactions(r.bubble(m, maxW-avatarSmallCols-1, firstOfRun, selected), m.Reactions, lipgloss.Left)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", b)
}

// withReactions põe as reações numa linha logo abaixo do balão, do lado
// dele: "❤️ 2  👍". As suas ficam na cor de acento.
func withReactions(bubble string, rs []ipc.Reaction, align lipgloss.Position) string {
	if len(rs) == 0 {
		return bubble
	}
	var parts []string
	for _, r := range rs {
		t := r.Emoji
		if r.Count > 1 {
			t += " " + fmt.Sprint(r.Count)
		}
		if r.Mine {
			parts = append(parts, sGreen.Bold(true).Render(t))
		} else {
			parts = append(parts, sText2.Render(t))
		}
	}
	line := " " + strings.Join(parts, "  ") + " "
	return lipgloss.JoinVertical(align, bubble, line)
}

// quickReactions são as reações da barra do ctrl+e, as mesmas do app.
var quickReactions = []string{"❤️", "👍", "😂", "😮", "😢", "🙏"}

// bubble desenha o balão com borda arredondada, no estilo de terminal:
// seus em ciano, dos outros em cinza, o selecionado com borda grossa na
// cor de acento.
func (r msgRenderer) bubble(m ipc.Message, maxW int, showName, selected bool) string {
	inner := maxW - 4 // borda + padding

	pic, thumb := "", false
	if r.imgs != nil && m.Kind != "revoked" {
		cols, rows := min(inner, 44), 22
		switch {
		case m.Kind == "sticker":
			cols, rows = min(inner, 16), 8
		case isVideoNote(m):
			// Recado de vídeo: círculo menor, como no app (célula ~1:2).
			cols, rows = min(inner, 24), 12
		}
		switch {
		case m.Media != nil && m.Media.Preview != "":
			pic = r.imgs.media(m.Media, cols, rows)
		case m.Thumb != nil:
			// Miniatura embutida: foto não baixada ou vídeo.
			pic, thumb = r.imgs.media(m.Thumb, cols, rows), true
		}
	}
	text := m.Text
	switch {
	case m.Kind == "revoked":
		t := "Mensagem apagada"
		if m.FromMe {
			t = "Você apagou esta mensagem"
		}
		text = sText2.Italic(true).Render("🚫 " + t)
	case m.Kind == "audio" && (m.HasRaw || m.Media != nil):
		text = r.audioLine(m, inner)
	case pic != "" && thumb:
		label := ""
		switch {
		case m.Kind == "video" && m.Media != nil:
			label = videoLabel(text) + " · ctrl+o assiste"
		case m.Kind == "video":
			label = videoLabel(text) + " · ctrl+o baixa e assiste"
		default:
			label = "ctrl+o baixa a foto"
		}
		text = sText2.Render(label)
		if c := stripTag(m.Text); c != "" {
			text += "\n" + c
		}
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
	case m.Card != nil && m.Kind == "other":
		text = "" // o cartão diz tudo
	case m.Kind == "other" && isRingingCall(m):
		text = sText2.Render(otherLabel(text) + " · ctrl+o recusa")
	case m.Kind == "other":
		text = sText2.Render(otherLabel(text))
	}

	var lines []string
	if showName {
		lines = append(lines, fg(senderColor(m.Sender)).Bold(true).Render(ansi.Truncate(m.SenderName, inner, "…")))
	}
	if m.Forwarded {
		lines = append(lines, sText2.Italic(true).Render(icons.forward+" Encaminhada"))
	}
	if m.Quote != nil {
		lines = append(lines, inlineQuote(m.Quote, inner))
	}
	if m.Card != nil {
		lines = append(lines, r.card(m.Card, inner))
	}
	if pic != "" {
		lines = append(lines, pic)
	}
	if text != "" {
		// Texto e legenda levam a formatação do WhatsApp; os rótulos de
		// mídia, áudio e "apagada" já vêm estilizados.
		if m.Kind == "text" || (pic != "" && !thumb) {
			lines = append(lines, formatText(text, inner, sText))
		} else {
			lines = append(lines, ansi.Wrap(text, inner, " "))
		}
	}
	content := strings.Join(lines, "\n")
	stampText := time.UnixMilli(m.TS).Format("15:04")
	if m.Edited && m.Kind != "revoked" {
		stampText = "editada " + stampText
	}
	if m.Starred {
		stampText = "★ " + stampText
	}
	if m.Kept {
		stampText = "salva · " + stampText
	}
	stamp := sText2.Render(stampText)
	if m.FromMe && m.Kind != "revoked" {
		stamp += " " + ticks(m.Status)
	}
	cw := min(inner, max(lipgloss.Width(content), lipgloss.Width(stamp)))
	content += "\n" + lipgloss.PlaceHorizontal(cw, lipgloss.Right, stamp)

	border, style := cOtherBorder, lipgloss.RoundedBorder()
	if m.FromMe {
		border = cOwnBorder
	}
	if selected {
		border, style = cGreen, lipgloss.ThickBorder()
	}
	return lipgloss.NewStyle().Border(style).BorderForeground(border).Padding(0, 1).Render(content)
}

// card desenha contato, localização, evento, enquete ou prévia de link
// dentro do balão: ícone e título em destaque, detalhes na cor fraca e uma
// barra de acento à esquerda, como a citação.
func (r msgRenderer) card(c *ipc.Card, inner int) string {
	icon, hint := icons.link, ""
	switch c.Type {
	case "contact":
		icon = icons.contact
	case "location":
		icon, hint = icons.location, "ctrl+o abre o mapa"
	case "event":
		icon = icons.event
	case "poll":
		icon = icons.poll
	}
	w := inner - 2
	bar := sGreen.Render("▎")
	var out []string
	if c.Thumb != nil && r.imgs != nil {
		if pic := r.imgs.media(c.Thumb, min(w, 30), 6); pic != "" {
			out = append(out, pic)
		}
	}
	title := c.Title
	if title == "" {
		title = map[string]string{"contact": "Contato", "location": "Localização", "event": "Evento",
			"poll": "Enquete", "link": "Link"}[c.Type]
	}
	for i, l := range strings.Split(ansi.Wrap(title, w-2, " "), "\n") {
		lead := icon + " "
		if i > 0 {
			lead = "  "
		}
		out = append(out, bar+sText.Bold(true).Render(lead+l))
	}
	if c.Type == "poll" && len(c.Options) > 0 {
		for _, l := range pollLines(c, w) {
			out = append(out, bar+l)
		}
	}
	for _, l := range c.Lines {
		st := sText2
		if c.Type == "poll" && strings.HasPrefix(l, "○ ") {
			st = sText
		}
		if c.Type == "event" && l == "cancelado" {
			st = fg(cDanger)
		}
		for _, wl := range strings.Split(ansi.Wrap(l, w, " "), "\n") {
			out = append(out, bar+st.Render(wl))
		}
	}
	if hint != "" {
		out = append(out, bar+sText2.Italic(true).Render(hint))
	}
	return strings.Join(out, "\n")
}

// otherLabel: marcadores de mensagens sem cartão (visualização única,
// chamada) com o ícone certo.
// isRingingCall: linha de chamada que ainda está tocando (recente: depois
// de 2 minutos ela já acabou, mesmo que o fim não tenha chegado).
func isRingingCall(m ipc.Message) bool {
	return strings.HasPrefix(m.ID, "call-") && strings.HasSuffix(m.Text, "· atenda no celular") &&
		time.Since(time.UnixMilli(m.TS)) < 2*time.Minute
}

func otherLabel(s string) string {
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	switch {
	case strings.HasPrefix(s, "[contato: "):
		return icons.contact + " " + strings.TrimPrefix(inner, "contato: ")
	case strings.HasPrefix(s, "[enquete: "):
		return icons.poll + " Enquete: " + strings.TrimPrefix(inner, "enquete: ")
	case strings.HasPrefix(s, "[evento"):
		head, _, _ := strings.Cut(strings.TrimPrefix(s, "["), "]")
		return icons.event + " " + strings.TrimPrefix(strings.TrimPrefix(head, "evento"), ": ")
	case s == "[localização]":
		return icons.location + " Localização"
	case s == "[localização em tempo real]":
		return icons.location + " Localização em tempo real · veja no celular"
	case s == "[visualização única]":
		return icons.eye + " Visualização única · abra no celular"
	case strings.HasPrefix(s, "📞 "), strings.HasPrefix(s, "📹 "):
		_, rest, _ := strings.Cut(s, " ")
		return icons.phone + " " + rest
	}
	return s
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
		bar + sText2.Render(ansi.Truncate(mediaLabel(oneLine(plainText(q.Text))), w-1, "…"))
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
	l2 := bar + sText2.Render(ansi.Truncate(mediaLabel(oneLine(plainText(q.Text))), w-2, "…"))
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

// audioLine: o player dentro do balão. Parado: ▶ e a duração; tocando:
// pausa, barra de progresso e tempo decorrido.
func (r msgRenderer) audioLine(m ipc.Message, inner int) string {
	total := audioSeconds(m.Text)
	barW := max(8, min(inner-16, 28))
	if m.ID != r.playID {
		dur := fmtDur(total)
		if total == 0 {
			dur = "áudio"
		}
		return sText.Render(icons.play) + " " + sText2.Render(strings.Repeat("─", barW)+" "+dur)
	}
	pos := 0
	if total > 0 {
		pos = min(barW-1, r.elapsed*barW/total)
	}
	bar := fg(cGreen).Render(strings.Repeat("━", pos)+"●") + sText2.Render(strings.Repeat("─", barW-pos-1))
	icon := icons.pause // tocando: o botão é de pausar
	if r.paused {
		icon = icons.play
	}
	line := fg(cGreen).Render(icon) + " " + bar + " " + sText2.Render(fmtDur(r.elapsed)+" / "+fmtDur(total))
	if r.speed > 1 {
		line += " " + sGreen.Render(speedLabel(r.speed))
	}
	return line
}

// tagDuration lê a duração do marcador do daemon: "[vídeo 0:32] legenda"
// devolve "0:32"; sem duração, "".
func tagDuration(text string) string {
	if !strings.HasPrefix(text, "[") {
		return ""
	}
	end := strings.Index(text, "]")
	if end < 0 {
		return ""
	}
	if m := durRe.FindStringSubmatch(text[1:end]); m != nil {
		return m[1] + ":" + m[2]
	}
	return ""
}

// videoLabel: "▶ Vídeo 0:32", sem a legenda (que vai na linha de baixo).
// isVideoNote: recado de vídeo (o vídeo redondo).
func isVideoNote(m ipc.Message) bool {
	return m.Kind == "video" && strings.HasPrefix(m.Text, "[recado de vídeo")
}

func videoLabel(text string) string {
	l := icons.play + " Vídeo"
	if strings.HasPrefix(text, "[recado de vídeo") {
		l = icons.play + " Recado de vídeo"
	}
	if d := tagDuration(text); d != "" {
		l += " " + d
	}
	return l
}
