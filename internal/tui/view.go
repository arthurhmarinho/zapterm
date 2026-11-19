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
	footerY             int
}

const headerH = 3

func (m *model) geom() geometry {
	g := geometry{}
	g.listW = max(34, min(52, m.w*32/100))
	g.chatX = g.listW + 1 // uma coluna de divisor
	g.chatW = m.w - g.chatX
	g.footerY = m.h - 1

	g.titleY, g.searchY, g.chipsY = 0, 1, 2
	g.archRowY = -1
	g.itemsY = 4 // título, busca, pílulas, linha vazia
	if m.showArchivedRow() {
		g.archRowY = 4
		g.itemsY = 6
	}
	g.listRows = max(0, (g.footerY-g.itemsY)/itemH)

	g.inputH = m.input.Height() + 2
	g.inputY = g.footerY - g.inputH
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

func (m *model) geom0chatW() int { return m.w - max(34, min(52, m.w*32/100)) - 1 }

func (m *model) extraHeight() int {
	switch {
	case m.picking:
		return 1 + 2*stickCellH
	case m.attach != nil:
		return m.attachRows() + 3
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
	m.input.SetWidth(max(10, g.chatW-6))
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
			c.Position.X += 6
			c.Position.Y += g.searchY
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

	// Título (ou "← Arquivadas")
	if m.archived {
		rows = append(rows, paint("  "+sText.Render(icons.back)+"  "+sBold.Render("Arquivadas"), cBgList, w))
	} else {
		rows = append(rows, paint("  "+lipgloss.NewStyle().Foreground(cText).Bold(true).Render("zapterm"), cBgList, w))
	}

	// Busca
	field := m.search.View()
	if !m.searching && m.searchQuery() == "" {
		field = sText2.Render("Pesquisar ou começar uma nova conversa")
	}
	icon := sText2.Render(icons.search)
	if m.searching {
		icon = sGreen.Render(icons.search)
	}
	inner := paint(" "+icon+"  "+ansi.Truncate(field, w-10, "…"), cBgPanel, w-4)
	edge := lipgloss.NewStyle().Foreground(cBgPanel).Background(cBgList)
	if icons.pillL != "" {
		inner = edge.Render(icons.pillL) + inner + edge.Render(icons.pillR)
	}
	rows = append(rows, paint(" "+inner, cBgList, w))

	// Pílulas de filtro (ou explicação na vista de arquivadas)
	if m.archived {
		rows = append(rows, paint("  "+sText2.Render("Conversas arquivadas · esc volta"), cBgList, w))
	} else {
		labels, _ := m.chipLabels()
		var chips []string
		for i, l := range labels {
			if i == m.tab && m.searchQuery() == "" {
				chips = append(chips, pill(l, cChipOnFg, cChipOnBg, cBgList, true))
			} else {
				chips = append(chips, pill(l, cText2, cBgPanel, cBgList, false))
			}
		}
		rows = append(rows, paint(" "+strings.Join(chips, " "), cBgList, w))
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
		rows = append(rows, renderChatItem(c, w, sel, m.bigAvatar(c.JID, c.Name)))
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
func (m *model) chipLabels() ([]string, []int) {
	var labels []string
	var starts []int
	x := 1
	capW := 0
	if icons.pillL != "" {
		capW = 2
	}
	for i, n := range tabNames {
		l := n
		if i != tabAll && i != tabChannels {
			if c := m.tabCount(i); c > 0 {
				l += fmt.Sprintf(" %d", c)
			}
		}
		labels = append(labels, l)
		starts = append(starts, x)
		x += lipgloss.Width(l) + capW + 1
	}
	return labels, starts
}

func (m *model) chipAt(x int) int {
	labels, starts := m.chipLabels()
	capW := 0
	if icons.pillL != "" {
		capW = 2
	}
	for i := range labels {
		if x >= starts[i] && x < starts[i]+lipgloss.Width(labels[i])+capW {
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
	tw := g.chatW - avatarCols - 6
	text := "\n" + sText.Render(ansi.Truncate(oneLine(c.Name), tw, "…")) + "\n" + sText2.Render(sub)
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
	if a := m.attach; a != nil {
		pic := m.imgs.block(a.preview, a.w, a.h, min(48, w-8), m.attachRows())
		if pic == "" {
			pic = sText.Render(icons.photo + " imagem colada")
		}
		head := "  " + sText.Render(icons.attach+"  Enviar imagem") + "   " +
			sText2.Render("escreva uma legenda e aperte enter · esc cancela")
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
	w := g.chatW
	fieldW := w - 6 // borda, espaço, prompt (2), espaço, borda
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
		lines[i] = prefix + l + strings.Repeat(" ", max(0, fieldW-ansi.StringWidth(l)))
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
	case m.searching:
		help = "digite para filtrar · ↑/↓ escolher · enter abrir · esc sair da busca"
	case m.attach != nil:
		help = "enter envia a imagem · esc cancela"
	case m.picking:
		help = "setas escolhem · enter ou clique envia · esc fecha"
	case m.focus == paneList:
		help = "↑/↓ chats · enter abrir · tab conversa · ctrl+p pesquisar · alt+1..4 filtros · alt+a arquivadas · ctrl+c sair"
	default:
		help = "↑/↓ mensagem · ctrl+r responder · ctrl+o mídia · ctrl+v colar print · ctrl+s figurinha · tab lista"
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
	head := "  " + sText.Render(icons.sticker+"  Figurinhas recentes") + "   " +
		sText2.Render("setas escolhem · enter envia · esc fecha")
	switch {
	case m.stickLoad:
		return head + "\n\n  " + sText2.Render("carregando…")
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
			pic := m.imgs.block(st.Preview, st.Width, st.Height, stickCellW-2, stickCellH-2)
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
