package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// geometry concentra as coordenadas da tela, usadas pelo desenho e pelo
// clique do mouse. Mudou o layout, muda aqui.
type geometry struct {
	listW, chatX, chatW int
	titleY, searchY     int
	chipsY, archRowY    int // archRowY = -1 sem a linha "Arquivadas"
	itemsY, listRows    int
	headerH, vpY, vpH   int
	extraY, extraH      int // painel de resposta/anexo acima da escrita
	inputY, inputH      int
	fieldW              int // largura do texto na caixa de escrita
	stickerBtnX         int // botões dentro da caixa de escrita
	clipBtnX            int
	footerY             int
}

const headerH = 3

func (m *model) geom() geometry {
	g := geometry{}
	g.listW = listWidth(m.w)
	g.chatX = g.listW + 1 // uma coluna de divisor
	g.chatW = m.w - g.chatX
	g.footerY = m.h - 1

	// título com sua foto (2 linhas), caixa de busca (3), filtros, vazia
	g.titleY, g.searchY, g.chipsY = 0, 2, 5
	g.archRowY = -1
	g.itemsY = 7
	if m.showArchivedRow() {
		g.archRowY = 7
		g.itemsY = 9
	}
	g.listRows = max(0, (g.footerY-g.itemsY)/itemH)

	g.inputH = m.input.Height() + 2
	g.inputY = g.footerY - g.inputH
	// Caixa: borda+espaço, "❯ ", texto, "  󰞅  󰏢", espaço+borda.
	g.fieldW = max(10, g.chatW-12)
	g.stickerBtnX = g.chatX + 2 + 2 + g.fieldW + 2
	g.clipBtnX = g.stickerBtnX + 3
	g.extraH = m.extraHeight()
	g.extraY = g.inputY - g.extraH
	g.headerH = headerH
	g.vpY = headerH
	g.vpH = max(1, g.extraY-g.vpY)
	return g
}

func (m *model) listRows() int { return m.geom().listRows }

// Célula da grade de figurinhas: imagem 10×5 dentro de uma borda.
const stickCellW, stickCellH = 12, 7

func (m *model) stickPerRow() int { return max(1, (m.geom0chatW()-2)/(stickCellW+1)) }

func (m *model) geom0chatW() int { return m.w - listWidth(m.w) - 1 }

func (m *model) extraHeight() int {
	switch {
	case m.rec != nil:
		return 3
	case m.reacting:
		return 3
	case m.editing != nil:
		return 4
	case m.picking:
		return 1 + 2*stickCellH
	case m.attach != nil && m.attach.image:
		return m.attachRows() + 3
	case m.attach != nil:
		return 4
	case m.replyTo != nil:
		return 4
	}
	return 0
}

func (m *model) attachRows() int { return 12 }

func (m *model) layout() {
	if m.w == 0 {
		return
	}
	g := m.geom()
	m.input.SetWidth(g.fieldW)
	m.search.SetWidth(max(10, g.listW-10))
	m.vp.SetWidth(g.chatW)
	m.vp.SetHeight(g.vpH)
	m.scrollList()
}

// ---------- view ----------

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.title()
	v.BackgroundColor = cBgList
	if m.w < 70 || m.h < 16 {
		v.SetContent("janela pequena demais para o zapterm")
		return v
	}
	if m.viewing != nil {
		v.SetContent(m.viewPhoto())
		return v
	}
	if m.help {
		v.SetContent(m.viewHelp())
		return v
	}
	g := m.geom()

	left := m.viewList(g)
	divider := paint(strings.TrimSuffix(strings.Repeat(fg(cDivider).Render("│")+"\n", g.footerY), "\n"), cBgList, 1)
	var right string
	if m.active == "" {
		right = m.viewEmpty(g)
	} else {
		parts := []string{m.viewHeader(g), fitHeight(m.vp.View(), cBgChat, g.chatW, g.vpH)}
		if g.extraH > 0 {
			parts = append(parts, m.viewExtra(g))
		}
		parts = append(parts, m.viewInput(g))
		right = strings.Join(parts, "\n")
	}
	screen := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right) + "\n" + m.viewFooter()
	v.SetContent(screen)

	// Cursor real do terminal no campo em uso.
	switch {
	case m.searching:
		if c := m.search.Cursor(); c != nil {
			c.Position.X += 6 // margem, borda, espaço, lupa, dois espaços
			c.Position.Y += g.searchY + 1
			v.Cursor = c
		}
	case m.active != "":
		if c := m.input.Cursor(); c != nil {
			c.Position.X += g.chatX + 4 // borda, espaço e prompt
			c.Position.Y += g.inputY + 1
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

// ---------- coluna da esquerda ----------

func (m *model) viewList(g geometry) string {
	w := g.listW
	var rows []string

	// Título em 2 linhas: sua foto, "zapterm" e seu nome (ou "← Arquivadas")
	if m.archived {
		rows = append(rows, paint("  "+sText.Render(icons.back)+"  "+sBold.Render("Arquivadas"), cBgList, w), blank(cBgList, w, 1))
	} else {
		name := m.myName
		if name == "" {
			name = "conectando…"
		}
		// Seu nome em destaque; o nome do app, discreto, embaixo.
		text := sBold.Render(ansi.Truncate(name, w-10, "…")) + "\n" + sText2.Render("zapterm")
		av := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", avatarSmallCols)+"\n", avatarSmallRows), "\n")
		if m.me != "" {
			av = m.smallAvatar(m.me, m.myName)
		}
		rows = append(rows, paint(lipgloss.JoinHorizontal(lipgloss.Top, "  ", av, "  ", text), cBgList, w))
	}

	// Busca: caixa com borda arredondada, como a de escrita.
	field := m.search.View()
	if !m.searching && m.searchQuery() == "" {
		field = sText2.Render("Pesquisar conversa")
	}
	icon := sText2.Render(icons.search)
	border := cText2
	if m.searching {
		icon, border = sGreen.Render(icons.search), cGreen
	}
	boxW := w - 2 // uma coluna de margem de cada lado
	inner := boxW - 4
	line := icon + "  " + ansi.Truncate(field, inner-3, "…")
	line += strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)))
	search := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Render(line)
	rows = append(rows, paint(lipgloss.JoinHorizontal(lipgloss.Top, " ", search), cBgList, w))

	// Filtros: texto simples; o ativo em destaque e sublinhado.
	if m.archived {
		rows = append(rows, paint("  "+sText2.Render("Conversas arquivadas · esc volta"), cBgList, w))
	} else {
		labels, _ := m.chipLabels()
		var tabs []string
		for i, l := range labels {
			if i == m.tab && m.searchQuery() == "" {
				tabs = append(tabs, sGreen.Bold(true).Underline(true).Render(l))
			} else {
				tabs = append(tabs, sText2.Render(l))
			}
		}
		rows = append(rows, paint("  "+strings.Join(tabs, "  "), cBgList, w))
	}
	rows = append(rows, blank(cBgList, w, 1))

	// Linha "Arquivadas"
	if g.archRowY >= 0 {
		_, unread := m.archivedUnread()
		count := ""
		if unread > 0 {
			count = sGreen.Render(fmt.Sprint(unread))
		}
		label := "   " + sText2.Render(icons.archive) + "    " + sText.Render("Arquivadas")
		gap := max(1, w-lipgloss.Width(label)-lipgloss.Width(count)-2)
		rows = append(rows, paint(label+strings.Repeat(" ", gap)+count, cBgList, w), blank(cBgList, w, 1))
	}

	// Chats
	for i := m.offset; i < min(len(m.list), m.offset+g.listRows); i++ {
		c := m.list[i]
		sel := i == m.cursor && (m.focus == paneList || c.JID == m.active)
		rows = append(rows, renderChatItem(c, w, sel, m.bigAvatar(c.JID, c.Name), m.typingText(c.JID, false)))
	}
	if len(m.list) == 0 {
		msg := "Nenhuma conversa aqui"
		if m.searchQuery() != "" {
			msg = "Nenhum chat encontrado"
		}
		rows = append(rows, paint("  "+sText2.Render(msg), cBgList, w))
	}
	return fitHeight(strings.Join(rows, "\n"), cBgList, w, g.footerY)
}

// chipLabels devolve os rótulos das pílulas e onde cada uma começa.
// chipLabels devolve os rótulos dos filtros e onde cada um começa. Usa
// os nomes completos se couberem na largura da lista; senão, os curtos.
func (m *model) chipLabels() ([]string, []int) {
	short := [numTabs]string{"Tudo", "N.lidas", "Grupos", "Canais"}
	tiny := [numTabs]string{"Tudo", "NL", "Gr", "Can"}
	width := listWidth(m.w) - 2
	for _, names := range [][numTabs]string{tabNames, short, tiny} {
		var labels []string
		var starts []int
		x := 2 // margem
		for i, n := range names {
			l := n
			if i != tabAll && i != tabChannels {
				if c := m.tabCount(i); c > 0 {
					l += fmt.Sprintf(" %d", c)
				}
			}
			labels = append(labels, l)
			starts = append(starts, x)
			x += lipgloss.Width(l) + 2
		}
		if x-2 <= width || names == tiny {
			return labels, starts
		}
	}
	return nil, nil
}

func (m *model) chipAt(x int) int {
	labels, starts := m.chipLabels()
	for i := range labels {
		if x >= starts[i] && x < starts[i]+lipgloss.Width(labels[i]) {
			return i
		}
	}
	return -1
}

// ---------- coluna da direita ----------

func (m *model) viewEmpty(g geometry) string {
	h := g.footerY
	lines := []string{
		lipgloss.NewStyle().Foreground(cGreen).Bold(true).Render("zapterm"),
		"",
		sText2.Render("WhatsApp no terminal"),
		"",
		sText2.Render(icons.search + "  ctrl+p pesquisa um chat"),
		sText2.Render(icons.attach + "  ctrl+v cola um print para enviar"),
		sText2.Render(icons.reply + "  ctrl+r responde a mensagem selecionada"),
		sText2.Render(icons.photo + "  ctrl+o abre foto, vídeo ou documento"),
	}
	block := lipgloss.JoinVertical(lipgloss.Center, lines...)
	top := max(0, (h-len(lines))/2)
	var out []string
	for range top {
		out = append(out, "")
	}
	for _, l := range strings.Split(block, "\n") {
		out = append(out, lipgloss.PlaceHorizontal(g.chatW, lipgloss.Center, l))
	}
	return fitHeight(paint(strings.Join(out, "\n"), cBgPanel, g.chatW), cBgPanel, g.chatW, h)
}

func (m *model) viewHeader(g geometry) string {
	c := m.activeChat
	sub := map[string]string{"dm": "conversa", "group": "grupo", "channel": "canal"}[c.Kind]
	if c.Muted {
		sub += " · silenciado"
	}
	if c.Archived {
		sub += " · arquivado"
	}
	subSt := sText2
	switch p := m.presence[c.JID]; {
	case m.typingText(c.JID, true) != "":
		sub, subSt = m.typingText(c.JID, true), sGreen
	case p != nil && p.online != nil && *p.online:
		sub, subSt = "online", sGreen
	case p != nil && p.lastSeen > 0:
		sub = "visto por último " + lastSeenText(p.lastSeen)
	}
	tw := g.chatW - avatarCols - 6
	text := "\n" + sText.Render(ansi.Truncate(oneLine(c.Name), tw, "…")) + "\n" + subSt.Render(sub)
	row := lipgloss.JoinHorizontal(lipgloss.Top, "  ", m.bigAvatar(c.JID, c.Name), "  ", text)
	return fitHeight(paint(row, cBgPanel, g.chatW), cBgPanel, g.chatW, headerH)
}

// viewExtra: citação da resposta ou prévia do print colado, acima da
// barra de escrita.
func (m *model) viewExtra(g geometry) string {
	w := g.chatW
	if m.picking {
		return fitHeight(m.viewStickers(w), cDefault, w, g.extraH)
	}
	if e := m.editing; e != nil {
		q := quoteBlock(&ipc.Quote{Sender: e.Sender, SenderName: "Editando mensagem  (enter salva · esc cancela)",
			Text: e.Text}, w-12, cDefault)
		return fitHeight(paint("\n"+lipgloss.JoinHorizontal(lipgloss.Top, "    ", q), cDefault, w), cDefault, w, g.extraH)
	}
	if m.reacting {
		var opts []string
		for i, e := range quickReactions {
			opts = append(opts, sText2.Render(fmt.Sprint(i+1))+" "+e)
		}
		line := "  " + sText.Render("Reagir:") + "  " + strings.Join(opts, "   ") + "    " +
			sText2.Render("0 tira a sua · esc fecha")
		return fitHeight("\n"+line, cDefault, w, g.extraH)
	}
	if r := m.rec; r != nil {
		secs := int(time.Since(r.started).Seconds())
		line := "  " + fg(cDanger).Render(icons.rec+" gravando") + " " + sText.Render(fmtDur(secs)) + "   " +
			sText2.Render("enter envia · esc descarta")
		return fitHeight("\n"+line, cDefault, w, g.extraH)
	}
	if a := m.attach; a != nil {
		hint := sText2.Render("escreva uma legenda e aperte enter · esc cancela")
		if !a.image {
			icon := icons.doc
			switch strings.ToLower(filepath.Ext(a.name)) {
			case ".mp4", ".mkv", ".webm", ".mov":
				icon = icons.video
			case ".mp3", ".ogg", ".m4a", ".opus", ".wav", ".flac":
				icon = icons.audio
			}
			line := "  " + sText.Render(icon+"  "+ansi.Truncate(a.name, w-30, "…")) + "  " + sText2.Render(humanSize(a.size))
			return fitHeight(paint("\n"+line+"\n  "+hint, cDefault, w), cDefault, w, g.extraH)
		}
		pic := m.imgs.block(a.preview, a.w, a.h, min(48, w-8), m.attachRows())
		if pic == "" {
			pic = sText.Render(icons.photo + " " + a.name)
		}
		head := "  " + sText.Render(icons.attach+"  Enviar foto") + "   " + hint
		body := lipgloss.JoinHorizontal(lipgloss.Top, "    ", pic)
		return fitHeight(paint(head+"\n\n"+body, cDefault, w), cDefault, w, g.extraH)
	}
	r := m.replyTo
	name := r.SenderName
	switch {
	case r.FromMe:
		name = "Você"
	case m.activeChat.Kind == "channel":
		name = m.activeChat.Name
	}
	q := quoteBlock(&ipc.Quote{Sender: r.Sender, SenderName: name, Text: r.Text}, w-12, cDefault)
	closeHint := sText2.Render(icons.close + " esc")
	row := lipgloss.JoinHorizontal(lipgloss.Top, "    ", q, "  ", closeHint)
	return fitHeight(paint("\n"+row, cDefault, w), cDefault, w, g.extraH)
}

// viewInput desenha a caixa de escrita no estilo do Claude Code: borda
// arredondada, prompt "❯" e o fundo do terminal. A borda ganha a cor de
// acento quando há texto, resposta ou anexo em andamento.
func (m *model) viewInput(g geometry) string {
	fieldW := g.fieldW
	lines := strings.Split(m.input.View(), "\n")
	for i, l := range lines {
		prefix := "  "
		if i == 0 {
			prefix = sText2.Render("❯") + " "
			if m.input.Value() != "" {
				prefix = sGreen.Render("❯") + " "
			}
		}
		l = ansi.Truncate(l, fieldW, "")
		// Figurinha e clipe à direita, na última linha (clicáveis).
		buttons := "      "
		if i == len(lines)-1 {
			st, cl := sText2, sText2
			if m.picking {
				st = sGreen
			}
			if m.attach != nil && !m.attach.image {
				cl = sGreen
			}
			buttons = "  " + st.Render(icons.sticker) + "  " + cl.Render(icons.attach)
		}
		lines[i] = prefix + l + strings.Repeat(" ", max(0, fieldW-ansi.StringWidth(l))) + buttons
	}
	border := cText2
	if m.input.Value() != "" || m.replyTo != nil || m.attach != nil {
		border = cGreen
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

func (m *model) viewFooter() string {
	var help string
	switch {
	case m.searching && m.forwarding != nil:
		help = "encaminhar: digite para filtrar · ↑/↓ escolher · enter encaminha · esc cancela"
	case m.searching:
		help = "digite para filtrar · ↑/↓ escolher · enter abrir · esc sair da busca"
	case m.attach != nil:
		help = "enter envia a imagem · esc cancela"
	case m.picking:
		help = "setas escolhem · enter ou clique envia · esc fecha"
	case m.rec != nil:
		help = "gravando pelo microfone · enter envia · esc descarta"
	case m.reacting:
		help = "1–6 reage · 0 tira a sua reação · esc fecha"
	case m.focus == paneList:
		help = "↑/↓ chats · enter abrir · tab conversa · ctrl+p pesquisar · alt+1..4 filtros · alt+a arquivadas · F1 ajuda"
	default:
		help = "↑/↓ mensagem · ctrl+r responder · ctrl+e reagir · ctrl+o abrir · alt+c copiar · alt+e editar · alt+d apagar · alt+w encaminhar · F1 ajuda"
	}
	left := sText2.Render(help)
	if m.flash != "" && time.Since(m.flashAt) < 6*time.Second {
		left = fg(cDanger).Render(m.flash)
	}
	dot := fg(lipgloss.Color("#ffd279")).Render("●")
	switch m.status {
	case "online":
		dot = sGreen.Render("●")
	case "offline", "logged_out":
		dot = fg(cDanger).Render("●")
	}
	right := dot + " " + sText2.Render(m.status) + " "
	left = ansi.Truncate(" "+left, m.w-lipgloss.Width(right)-1, "…")
	gap := max(1, m.w-lipgloss.Width(left)-lipgloss.Width(right))
	return paint(left+strings.Repeat(" ", gap)+right, cBgList, m.w)
}

func (m *model) viewStickers(w int) string {
	tabs := [2]string{"Favoritas", "Recentes das conversas"}
	for i, t := range tabs {
		if i == m.stickTab {
			tabs[i] = sGreen.Bold(true).Underline(true).Render(t)
		} else {
			tabs[i] = sText2.Render(t)
		}
	}
	head := "  " + sText.Render(icons.sticker) + "  " + tabs[0] + "  " + tabs[1] + "   " +
		sText2.Render("tab troca · setas escolhem · enter envia · esc fecha")
	switch {
	case m.stickLoad && len(m.stickers) == 0:
		return head + "\n\n  " + sText2.Render("carregando… (a primeira vez baixa as figurinhas)")
	case len(m.stickers) == 0 && m.stickTab == stickFav:
		return head + "\n\n  " + sText2.Render("nenhuma favorita ainda: favorite figurinhas no celular e elas aparecem aqui")
	case len(m.stickers) == 0:
		return head + "\n\n  " + sText2.Render("nenhuma figurinha ainda: as que chegarem nas conversas aparecem aqui")
	}
	per := m.stickPerRow()
	var rows []string
	for r := m.stickTop; r < m.stickTop+2; r++ {
		var cells []string
		for c := 0; c < per; c++ {
			i := r*per + c
			if i >= len(m.stickers) {
				break
			}
			st := m.stickers[i]
			pic := m.imgs.media(&ipc.Media{Preview: st.Preview, Width: st.Width, Height: st.Height,
				Frames: st.Frames, Delays: st.Delays}, stickCellW-2, stickCellH-2)
			if pic == "" {
				pic = sText2.Render(icons.sticker)
			}
			border := cText2
			if i == m.stickSel {
				border = cGreen
			}
			// Place centraliza num espaço fixo de 10×5; a borda vem por fora
			// (Width/Height do lipgloss v2 contam a borda e quebrariam a imagem).
			inner := lipgloss.Place(stickCellW-2, stickCellH-2, lipgloss.Center, lipgloss.Center, pic)
			cell := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(inner)
			cells = append(cells, cell, " ")
		}
		if len(cells) > 0 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, append([]string{" "}, cells...)...))
		}
	}
	return head + "\n" + strings.Join(rows, "\n")
}

// stickerAt devolve a figurinha sob o clique (coordenadas da tela), ou -1.
func (m *model) stickerAt(g geometry, x, y int) int {
	if !m.picking || y < g.extraY+1 || x < g.chatX+1 {
		return -1
	}
	r := (y-g.extraY-1)/stickCellH + m.stickTop
	c := (x - g.chatX - 1) / (stickCellW + 1)
	per := m.stickPerRow()
	if c >= per {
		return -1
	}
	if i := r*per + c; i < len(m.stickers) {
		return i
	}
	return -1
}

// viewer é a foto aberta em tela cheia (ctrl+o numa foto já baixada).
type viewer struct {
	media   *ipc.Media
	caption string
}

// viewPhoto ocupa a tela inteira com a foto centralizada, a legenda e os
// atalhos.
func (m *model) viewPhoto() string {
	v := m.viewing
	pic := m.imgs.media(v.media, m.w-4, m.h-4)
	if pic == "" {
		pic = sText2.Render("não deu para desenhar a foto")
	}
	body := pic
	if v.caption != "" {
		body += "\n" + sText.Render(ansi.Truncate(oneLine(v.caption), m.w-4, "…"))
	}
	screen := lipgloss.Place(m.w, m.h-1, lipgloss.Center, lipgloss.Center, body)
	help := sText2.Render(" esc fecha · o abre no programa externo")
	return screen + "\n" + help
}

// listWidth: 40 colunas no mínimo, para os filtros caberem com os nomes
// completos ("Tudo  Não lidas 14  Grupos 7  Canais").
func listWidth(w int) int { return max(40, min(52, w*32/100)) }

func lastSeenText(ms int64) string {
	t := time.UnixMilli(ms)
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "hoje às " + t.Format("15:04")
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "ontem às " + t.Format("15:04")
	}
	return "em " + t.Format("02/01 às 15:04")
}

// viewHelp: todos os atalhos numa tela só (F1 ou alt+h; qualquer tecla fecha).
func (m *model) viewHelp() string {
	sections := []struct {
		title string
		keys  [][2]string
	}{
		{"Navegar", [][2]string{
			{"↑ ↓", "chats na lista, ou mensagens na conversa"}, {"tab", "troca entre lista e conversa"},
			{"pgup pgdn", "rolar a conversa"}, {"ctrl+p", "pesquisar conversa"},
			{"alt+1…4", "filtros (Tudo, Não lidas, Grupos, Canais)"}, {"alt+a", "arquivadas"},
			{"esc", "cancela o que estiver aberto"}, {"ctrl+c", "limpa o campo; vazio, duas vezes, sai"},
		}},
		{"Escrever", [][2]string{
			{"enter", "envia"}, {"shift+enter", "nova linha"}, {"ctrl+v", "cola print (ou texto)"},
			{"alt+f", "anexar arquivo"}, {"ctrl+s", "figurinhas (tab: favoritas/recentes)"},
			{"ctrl+t", "gravar mensagem de voz"},
		}},
		{"Mensagem selecionada", [][2]string{
			{"ctrl+r", "responder (ou botão direito)"}, {"ctrl+e", "reagir"},
			{"ctrl+o", "abrir foto, vídeo, documento, link, ou tocar áudio"},
			{"alt+c", "copiar texto"}, {"alt+e", "editar (sua, até 15 min)"},
			{"alt+d", "apagar para todos (sua, duas vezes)"}, {"alt+w", "encaminhar"},
		}},
	}
	var b strings.Builder
	b.WriteString(sBold.Render("zapterm · atalhos") + "\n")
	for _, sec := range sections {
		b.WriteString("\n" + sGreen.Bold(true).Render(sec.title) + "\n")
		for _, k := range sec.keys {
			b.WriteString("  " + sText.Bold(true).Render(fmt.Sprintf("%-12s", k[0])) + "  " + sText2.Render(k[1]) + "\n")
		}
	}
	b.WriteString("\n" + sText2.Render("mouse: clique abre e seleciona · botão direito responde · shift+clique abre link") + "\n")
	b.WriteString(sText2.Render("qualquer tecla fecha"))
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, b.String())
}
