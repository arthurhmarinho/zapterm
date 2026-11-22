package tui

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	xdraw "golang.org/x/image/draw"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Galeria do perfil ("Mídia, links e docs"): abas por tipo, separação por
// mês, grade de miniaturas cortadas no centro (fotos e vídeos) e lista com
// remetente e data (docs, links, áudios). Carrega em páginas ao rolar.

var galleryLabels = map[string]string{
	"fotos": "Fotos", "videos": "Vídeos", "docs": "Docs", "links": "Links", "audios": "Áudios",
}

const (
	galleryPage  = 60
	galleryCellW = 18 // largura alvo da célula, em colunas
)

type gallery struct {
	chat    string // "" = central de mídia, de todas as conversas (alt+g)
	tab     int
	items   []ipc.Message
	counts  map[string]int
	more    bool
	loading bool
	sel     int
	top     int // primeira linha (de galleryRows) à vista

	// Mídia antiga sem o original: pede ao celular (um pedido por vez).
	refetching bool
	refetchID  string
	asked      map[string]bool
}

type galleryMsg struct {
	chat   string
	tab    int
	append bool
	reload bool // recarga depois do celular mandar o histórico: mantém a seleção
	page   *ipc.GalleryPage
}

type refetchDoneMsg struct {
	id  string
	err error
}

func (g *gallery) tabName() string { return ipc.GalleryTabs[g.tab] }
func (g *gallery) isGrid() bool    { return g.tab <= 1 }

func (g *gallery) all() bool { return g.chat == "" }

// openGallery abre a galeria do chat aberto (pelo perfil).
func (m *model) openGallery(tab int) tea.Cmd {
	if m.active == "" {
		return nil
	}
	m.gallery = &gallery{chat: m.active, tab: tab}
	return m.loadGallery(false)
}

// openMediaCenter abre a central de mídia: a galeria de todas as conversas.
func (m *model) openMediaCenter() tea.Cmd {
	if m.searching {
		m.stopSearch(false)
	}
	m.profileOpen, m.profile = false, nil
	m.gallery = &gallery{}
	return m.loadGallery(false)
}

// galleryShown: a galeria ocupa o painel da conversa.
func (m *model) galleryShown() bool {
	return m.gallery != nil && (m.profileOpen || m.gallery.all())
}

// switchGalleryTab troca de aba mantendo a galeria (do chat ou central).
func (m *model) switchGalleryTab(tab int) tea.Cmd {
	m.gallery = &gallery{chat: m.gallery.chat, tab: tab}
	return m.loadGallery(false)
}

func (m *model) loadGallery(more bool) tea.Cmd {
	g := m.gallery
	if g == nil || g.loading {
		return nil
	}
	g.loading = true
	req := ipc.Request{Op: ipc.OpGallery, Chat: g.chat, Text: g.tabName(), Limit: galleryPage}
	if more && len(g.items) > 0 {
		req.Before = g.items[len(g.items)-1].TS
	}
	chat, tab := g.chat, g.tab
	var out ipc.GalleryPage
	return m.call(req, &out, func() tea.Msg { return galleryMsg{chat: chat, tab: tab, append: more, page: &out} })
}

func (m *model) onGallery(msg galleryMsg) tea.Cmd {
	g := m.gallery
	if g == nil || msg.chat != g.chat || msg.tab != g.tab {
		return nil
	}
	g.loading = false
	g.counts, g.more = msg.page.Counts, msg.page.More
	switch {
	case msg.append:
		g.items = append(g.items, msg.page.Items...)
	case msg.reload:
		g.items, g.refetching = msg.page.Items, false
		g.sel = min(g.sel, max(0, len(g.items)-1))
	default:
		g.items, g.sel, g.top = msg.page.Items, 0, 0
	}
	return m.maybeRefetch()
}

// reloadGallery recarrega a aba aberta do mesmo tamanho (o celular mandou
// histórico: itens antigos ganharam miniatura).
func (m *model) reloadGallery() tea.Cmd {
	g := m.gallery
	if g == nil || g.loading || !g.refetching {
		return nil
	}
	g.loading = true
	req := ipc.Request{Op: ipc.OpGallery, Chat: g.chat, Text: g.tabName(), Limit: max(galleryPage, len(g.items))}
	chat, tab := g.chat, g.tab
	var out ipc.GalleryPage
	return m.call(req, &out, func() tea.Msg { return galleryMsg{chat: chat, tab: tab, reload: true, page: &out} })
}

// maybeRefetch: se perto da seleção há mídia sem o original (histórico do
// pareamento), pede ao celular as mensagens daquela época. Cada item é
// pedido uma vez; a resposta chega como recarga (EvReload).
func (m *model) maybeRefetch() tea.Cmd {
	g := m.gallery
	if g == nil || g.refetching || g.tabName() == "links" {
		return nil
	}
	if g.asked == nil {
		g.asked = map[string]bool{}
	}
	for i := g.sel; i < min(len(g.items), g.sel+24); i++ {
		it := g.items[i]
		if it.HasRaw || g.asked[it.ID] {
			continue
		}
		g.asked[it.ID], g.refetching, g.refetchID = true, true, it.ID
		m.setFlash("pedindo ao celular as mídias antigas…")
		req := ipc.Request{Op: ipc.OpRefetch, Chat: it.Chat, Before: it.TS - 1}
		return tea.Batch(m.call(req, nil, func() tea.Msg { return refetchDoneMsg{id: it.ID} }),
			tea.Tick(20*time.Second, func(time.Time) tea.Msg { return refetchDoneMsg{id: it.ID, err: errRefetchTimeout} }))
	}
	return nil
}

var errRefetchTimeout = fmt.Errorf("o celular não respondeu")

func (m *model) onRefetchDone(msg refetchDoneMsg) {
	g := m.gallery
	if g == nil || msg.err == nil || msg.id != g.refetchID {
		return // pedido aceito: a resposta chega como recarga
	}
	if g.refetching {
		g.refetching = false
		m.setFlash("o celular não mandou as mídias antigas (ele está online?)")
	}
}

func (m *model) keyGallery(s string) tea.Cmd {
	g := m.gallery
	n := len(g.items)
	per := 1
	if g.isGrid() {
		per, _, _ = m.galleryGrid()
	}
	switch s {
	case "esc", "ctrl+c", "alt+i", "alt+g":
		m.gallery = nil
		return nil
	case "tab", "shift+tab":
		d := 1
		if s == "shift+tab" {
			d = len(ipc.GalleryTabs) - 1
		}
		return m.switchGalleryTab((g.tab + d) % len(ipc.GalleryTabs))
	case "left":
		g.sel = max(0, g.sel-1)
	case "right":
		g.sel = min(max(0, n-1), g.sel+1)
	case "up":
		g.sel = max(0, g.sel-per)
	case "down":
		g.sel = min(max(0, n-1), g.sel+per)
	case "pgup":
		g.sel = max(0, g.sel-per*4)
	case "pgdown":
		g.sel = min(max(0, n-1), g.sel+per*4)
	case "home", "ctrl+home":
		g.sel = 0
	case "end", "ctrl+end":
		g.sel = max(0, n-1)
	case "enter", "ctrl+o":
		if n == 0 {
			return nil
		}
		it := g.items[g.sel]
		if g.tabName() == "links" {
			if u := firstURL(it.Text); u != "" {
				if err := openURL(u); err != nil {
					m.setFlash("xdg-open: " + err.Error())
				} else {
					m.setFlash("abrindo " + u)
				}
			}
			return nil
		}
		return m.openProfileMedia(it)
	case "ctrl+g":
		// Mostrar na conversa: fecha perfil e galeria e pula para a mensagem.
		if n == 0 {
			return nil
		}
		it := g.items[g.sel]
		m.gallery = nil
		m.closeProfile()
		c, ok := m.chatByJID(it.Chat)
		if !ok {
			c = ipc.Chat{JID: it.Chat, Name: it.Chat}
		}
		m.hits = append(m.hits, ipc.SearchHit{Chat: c, Message: it})
		return m.openHit(ipc.Chat{JID: fmt.Sprintf("%s%d", hitMarker, len(m.hits)-1)})
	}
	// Perto do fim do que foi carregado: pede a próxima página.
	if g.more && g.sel >= n-per*2 {
		return tea.Batch(m.loadGallery(true), m.maybeRefetch())
	}
	return m.maybeRefetch()
}

// galleryGrid: quantas células por linha e o tamanho de cada uma (com a
// borda). A célula é quadrada na tela: as linhas do terminal são mais
// altas que largas.
func (m *model) galleryGrid() (per, cw, ch int) {
	w := m.geom0chatW() - 4
	per = max(2, (w+1)/(galleryCellW+1))
	cw = (w - (per - 1)) / per
	ch = max(4, ((cw-2)*cellW+cellH/2)/cellH+2)
	return per, cw, ch
}

// galleryRow é uma linha da galeria: cabeçalho de mês ou um grupo de
// itens (uma fileira da grade, ou um item da lista).
type galleryRow struct {
	month string
	first int // índice do primeiro item
	n     int
}

func monthLabel(ts int64) string {
	months := [...]string{"Janeiro", "Fevereiro", "Março", "Abril", "Maio", "Junho", "Julho",
		"Agosto", "Setembro", "Outubro", "Novembro", "Dezembro"}
	t := time.UnixMilli(ts)
	if t.Year() == time.Now().Year() {
		return months[t.Month()-1]
	}
	return fmt.Sprintf("%s de %d", months[t.Month()-1], t.Year())
}

func (m *model) galleryRows(per int) []galleryRow {
	g := m.gallery
	var rows []galleryRow
	for i := 0; i < len(g.items); {
		mon := monthLabel(g.items[i].TS)
		rows = append(rows, galleryRow{month: mon})
		j := i
		for j < len(g.items) && monthLabel(g.items[j].TS) == mon {
			j++
		}
		for k := i; k < j; k += per {
			rows = append(rows, galleryRow{first: k, n: min(per, j-k)})
		}
		i = j
	}
	return rows
}

func (m *model) viewGallery(gm geometry) string {
	w, h := gm.chatW, gm.footerY
	g := m.gallery
	title := "Mídia, links e docs"
	if g.all() {
		title = "Central de mídia · todas as conversas"
	}
	top := "  " + sText.Render(icons.back) + "  " + sBold.Render(title) + "   " +
		sText2.Render("esc volta")
	var tabs []string
	for i, t := range ipc.GalleryTabs {
		label := galleryLabels[t]
		if n := g.counts[t]; n > 0 {
			label += " " + fmt.Sprint(n)
		}
		if i == g.tab {
			tabs = append(tabs, sGreen.Bold(true).Underline(true).Render(label))
		} else {
			tabs = append(tabs, sText2.Render(label))
		}
	}
	head := []string{top, "", "  " + strings.Join(tabs, "   "), ""}
	hint := "tab troca a aba · setas escolhem · enter abre · ctrl+g mostra na conversa"
	if g.all() && g.isGrid() && g.sel < len(g.items) {
		// Na grade não cabe o nome: a dica diz de onde é a selecionada.
		hint = m.galleryOrigin(g.items[g.sel]) + " · " + hint
	}
	room := h - len(head) - 2

	if len(g.items) == 0 {
		msg := "nada por aqui"
		if g.loading || g.counts == nil {
			msg = "carregando…"
		}
		body := append(head, "", lipgloss.PlaceHorizontal(w, lipgloss.Center, sText2.Render(msg)))
		return fitHeight(strings.Join(body, "\n"), cDefault, w, h)
	}

	per, cw, ch := 1, 0, 0
	if g.isGrid() {
		per, cw, ch = m.galleryGrid()
	}
	rowH := func(r galleryRow) int {
		switch {
		case r.month != "":
			return 2
		case g.isGrid():
			return ch
		}
		return 3
	}
	rows := m.galleryRows(per)
	selRow := 0
	for i, r := range rows {
		if r.month == "" && g.sel >= r.first && g.sel < r.first+r.n {
			selRow = i
		}
	}
	// Rolagem: a linha selecionada sempre à vista (com o mês dela, se
	// couber).
	if selRow < g.top {
		g.top = selRow
	}
	if g.top > 0 && selRow == g.top && rows[g.top-1].month != "" {
		g.top--
	}
	for {
		used := 0
		for i := g.top; i <= selRow; i++ {
			used += rowH(rows[i])
		}
		if used <= room || g.top >= selRow {
			break
		}
		g.top++
	}

	var body []string
	for i := g.top; i < len(rows) && len(body) < room; i++ {
		r := rows[i]
		switch {
		case r.month != "":
			body = append(body, "", "  "+sBold.Render(r.month))
		case g.isGrid():
			var cells []string
			for k := r.first; k < r.first+r.n; k++ {
				cells = append(cells, m.galleryCell(g.items[k], k == g.sel, cw, ch))
				if k < r.first+r.n-1 {
					cells = append(cells, " ")
				}
			}
			row := lipgloss.JoinHorizontal(lipgloss.Top, append([]string{"  "}, cells...)...)
			body = append(body, strings.Split(row, "\n")...)
		default:
			body = append(body, m.galleryItem(g.items[r.first], r.first == g.sel, w)...)
		}
	}
	if len(body) > room {
		body = body[:room]
	}
	if g.loading {
		hint = "carregando mais…"
	}
	out := append(head, body...)
	for len(out) < h-1 {
		out = append(out, "")
	}
	out = append(out[:h-1], "  "+sText2.Render(ansi.Truncate(hint, w-4, "…")))
	return fitHeight(strings.Join(out, "\n"), cDefault, w, h)
}

// galleryCell: miniatura cortada para preencher a célula, com borda
// arredondada; vídeo leva ▶ e a duração na borda de baixo.
func (m *model) galleryCell(msg ipc.Message, selected bool, cw, ch int) string {
	iw, ih := cw-2, ch-2
	md := msg.Media
	if md == nil || md.Preview == "" {
		md = msg.Thumb
	}
	pic := ""
	if md != nil && md.Preview != "" {
		if path, ok := coverCrop(md.Preview, iw*cellW, ih*cellH); ok {
			pic = m.imgs.block(path, iw*cellW, ih*cellH, iw, ih)
		}
	}
	if pic == "" {
		icon := icons.photo
		if msg.Kind == "video" {
			icon = icons.video
		}
		pic = sText2.Render(icon) + "\n" + sText2.Render("sem prévia")
	}
	inner := lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, pic)
	bc := fg(cDivider)
	if selected {
		bc = fg(cGreen)
	}
	label := ""
	if msg.Kind == "video" {
		label = " " + icons.play + videoDuration(msg.Text) + " "
	}
	lines := []string{bc.Render("╭" + strings.Repeat("─", iw) + "╮")}
	for _, l := range strings.Split(inner, "\n") {
		lines = append(lines, bc.Render("│")+l+bc.Render("│"))
	}
	fill := max(0, iw-lipgloss.Width(label)-1)
	bottom := bc.Render("╰─") + sText.Render(label) + bc.Render(strings.Repeat("─", fill)+"╯")
	if label == "" {
		bottom = bc.Render("╰" + strings.Repeat("─", iw) + "╯")
	}
	lines = append(lines, bottom)
	return strings.Join(lines, "\n")
}

var tagDurRe = regexp.MustCompile(`^\[[^\]]*?(\d+):(\d\d)\]`)

func videoDuration(text string) string {
	if mm := tagDurRe.FindStringSubmatch(text); mm != nil {
		return " " + mm[1] + ":" + mm[2]
	}
	return ""
}

// galleryItem: doc, link ou áudio em duas linhas (o que é; quem mandou e
// quando), com a barra de destaque no selecionado.
func (m *model) galleryItem(msg ipc.Message, selected bool, w int) []string {
	tw := w - 6
	var title, extra string
	st := sText
	switch m.gallery.tabName() {
	case "docs":
		label := mediaLabel(msg.Text) // "󰈙 nome.pdf"
		if c := stripTag(msg.Text); c != "" && !strings.Contains(label, c) {
			extra = c
		}
		title = label
	case "links":
		u := firstURL(msg.Text)
		title, st = icons.link+" "+u, sGreen
		if rest := oneLine(plainText(strings.Replace(msg.Text, u, "", 1))); rest != "" {
			extra = rest
		}
	default:
		title = mediaLabel(msg.Text)
	}
	who := msg.SenderName
	if msg.FromMe {
		who = "você"
	}
	sub := who + " · " + time.UnixMilli(msg.TS).Format("02/01/2006 15:04")
	if m.gallery.all() {
		sub = m.galleryOrigin(msg)
	}
	if extra != "" {
		sub += " · " + extra
	}
	mark := "  "
	if selected {
		mark = fg(cGreen).Render("▌") + " "
	}
	return []string{
		mark + st.Render(ansi.Truncate(oneLine(title), tw, "…")),
		mark + sText2.Render(ansi.Truncate(sub, tw, "…")),
		"",
	}
}

// galleryOrigin: de que conversa é o item da central, quem mandou e quando.
func (m *model) galleryOrigin(msg ipc.Message) string {
	name := msg.Chat
	if c, ok := m.chatByJID(msg.Chat); ok && c.Name != "" {
		name = c.Name
	}
	who := msg.SenderName
	if msg.FromMe {
		who = "você"
	}
	if who != "" && who != name {
		name += " · " + who
	}
	return name + " · " + time.UnixMilli(msg.TS).Format("02/01/2006 15:04")
}

// coverCrop grava (uma vez) a imagem cortada no centro na proporção
// pxW:pxH, reduzida ao tamanho da célula, ao lado da original.
var cropDone = map[string]bool{}

func coverCrop(src string, pxW, pxH int) (string, bool) {
	if pxW <= 0 || pxH <= 0 {
		return "", false
	}
	out := fmt.Sprintf("%s.c%dx%d.png", src, pxW, pxH)
	if cropDone[out] {
		return out, true
	}
	if _, err := os.Stat(out); err == nil {
		cropDone[out] = true
		return out, true
	}
	f, err := os.Open(src)
	if err != nil {
		return "", false
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return "", false
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	// Recorte com a proporção da célula, centrado.
	cw, chh := sw, sw*pxH/pxW
	if chh > sh {
		chh, cw = sh, sh*pxW/pxH
	}
	x0, y0 := b.Min.X+(sw-cw)/2, b.Min.Y+(sh-chh)/2
	dst := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, image.Rect(x0, y0, x0+cw, y0+chh), xdraw.Src, nil)
	tmp := out + ".tmp"
	wf, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", false
	}
	err = png.Encode(wf, dst)
	wf.Close()
	if err != nil || os.Rename(tmp, out) != nil {
		os.Remove(tmp)
		return "", false
	}
	cropDone[out] = true
	return out, true
}

// ---------- mídia antiga na conversa ----------

// refetchOld: mídia do histórico do pareamento não tem a chave para baixar.
// Pede ao celular as mensagens daquela época; quando a resposta chega
// (EvReload), recarrega o trecho e a mensagem fica pronta para o ctrl+o.
func (m *model) refetchOld(msg ipc.Message) tea.Cmd {
	m.oldWanted = &msg
	m.setFlash("mídia antiga: pedindo ao celular (ele precisa estar online)…")
	return m.call(ipc.Request{Op: ipc.OpRefetch, Chat: msg.Chat, Before: msg.TS - 1}, nil, nil)
}

func (m *model) afterRefetch() tea.Cmd {
	w := m.oldWanted
	if w == nil || w.Chat != m.active {
		return nil
	}
	m.oldWanted = nil
	m.setFlash("o celular mandou: ctrl+o de novo abre")
	return m.loadAround(w.Chat, w.ID, w.TS)
}
