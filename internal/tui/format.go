package tui

import (
	"regexp"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// Formatação do WhatsApp: *negrito*, _itálico_, ~riscado~, ```bloco``` e
// `código`, mais links. O texto vira uma lista de "células" (um caractere
// visível cada, como o terminal vê: emoji composto é um só), cada uma com
// seu estilo; a quebra de linha é feita aqui, sobre as células, para um
// estilo nunca atravessar a borda do balão.

type fmtBits uint8

const (
	fBold fmtBits = 1 << iota
	fItalic
	fStrike
	fMono
	fMention
)

type cell struct {
	s      string // grafema (pode ter vários runes)
	w      int    // largura no terminal
	bits   fmtBits
	url    string
	hidden bool // marcador de formatação: não aparece
}

var urlRe = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>]+`)

// trimURL tira pontuação final que costuma não fazer parte do link
// ("veja https://x.com/a." ou "(https://x.com/a)").
func trimURL(u string) string {
	for len(u) > 0 && strings.ContainsRune(".,;:!?)]}'\"", rune(u[len(u)-1])) {
		if u[len(u)-1] == ')' && strings.Count(u, "(") >= strings.Count(u, ")") {
			break // parêntese que abre dentro do link (Wikipédia)
		}
		u = u[:len(u)-1]
	}
	return u
}

// findURLs devolve [início, fim) em bytes de cada link.
func findURLs(s string) [][2]int {
	var out [][2]int
	for _, m := range urlRe.FindAllStringIndex(s, -1) {
		u := trimURL(s[m[0]:m[1]])
		out = append(out, [2]int{m[0], m[0] + len(u)})
	}
	return out
}

func cellsOf(s string) []cell {
	var cells []cell
	urls := findURLs(s)
	pos := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		c := cell{s: g.Str(), w: g.Width()}
		for _, u := range urls {
			if pos >= u[0] && pos < u[1] {
				link := s[u[0]:u[1]]
				if !strings.Contains(strings.ToLower(link[:min(8, len(link))]), "://") {
					link = "https://" + link
				}
				c.url = link
			}
		}
		pos += len(c.s)
		cells = append(cells, c)
	}
	return cells
}

func isWordCell(c cell) bool {
	r := []rune(c.s)
	return len(r) > 0 && (unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]))
}

func isSpaceCell(c cell) bool { return strings.TrimSpace(c.s) == "" }

// markPairs aplica um marcador (*, _, ~, `) nas células, com as regras do
// WhatsApp: abre no começo de palavra e não seguido de espaço, fecha no
// fim de palavra e não precedido de espaço, na mesma linha. Links e trechos
// em código ficam de fora.
func markPairs(cells []cell, marker string, bit fmtBits) {
	n := len(cells)
	for i := 0; i < n; i++ {
		c := cells[i]
		if c.s != marker || c.hidden || c.url != "" || c.bits&(fMono|fMention) != 0 {
			continue
		}
		if i > 0 && isWordCell(cells[i-1]) {
			continue
		}
		if i+1 >= n || isSpaceCell(cells[i+1]) || cells[i+1].s == marker {
			continue
		}
		for j := i + 1; j < n; j++ {
			if cells[j].s == "\n" {
				break
			}
			if cells[j].s != marker || cells[j].url != "" || isSpaceCell(cells[j-1]) {
				continue
			}
			if j+1 < n && isWordCell(cells[j+1]) {
				continue
			}
			cells[i].hidden, cells[j].hidden = true, true
			for k := i + 1; k < j; k++ {
				cells[k].bits |= bit
			}
			i = j
			break
		}
	}
}

// markCodeBlocks: ```...``` vira monoespaçado (pode ter várias linhas) e
// os marcadores somem.
func markCodeBlocks(cells []cell) {
	n := len(cells)
	isFence := func(i int) bool {
		return i+2 < n && cells[i].s == "`" && cells[i+1].s == "`" && cells[i+2].s == "`"
	}
	for i := 0; i < n; i++ {
		if !isFence(i) {
			continue
		}
		for j := i + 3; j < n; j++ {
			if isFence(j) {
				for k := i; k < i+3; k++ {
					cells[k].hidden = true
				}
				for k := j; k < j+3; k++ {
					cells[k].hidden = true
				}
				for k := i + 3; k < j; k++ {
					cells[k].bits |= fMono
				}
				i = j + 2
				break
			}
		}
	}
}

// markMentions: o daemon manda menções como "@" + U+2068 nome U+2069; o
// nome (com o @) fica em destaque e os isolantes somem.
func markMentions(cells []cell) {
	for i := 0; i < len(cells); i++ {
		if cells[i].s != "\u2068" {
			continue
		}
		for j := i + 1; j < len(cells); j++ {
			if cells[j].s == "\u2069" {
				cells[i].hidden, cells[j].hidden = true, true
				if i > 0 && cells[i-1].s == "@" {
					cells[i-1].bits |= fMention
				}
				for k := i + 1; k < j; k++ {
					cells[k].bits |= fMention
				}
				i = j
				break
			}
		}
	}
}

func parseFormatted(s string) []cell {
	cells := cellsOf(s)
	markMentions(cells)
	markCodeBlocks(cells)
	markPairs(cells, "`", fMono)
	markPairs(cells, "*", fBold)
	markPairs(cells, "_", fItalic)
	markPairs(cells, "~", fStrike)
	out := cells[:0]
	for _, c := range cells {
		if !c.hidden {
			out = append(out, c)
		}
	}
	return out
}

// plainText tira os marcadores: para a prévia da lista e citações.
func plainText(s string) string {
	var b strings.Builder
	for _, c := range parseFormatted(s) {
		b.WriteString(c.s)
	}
	return b.String()
}

// formatText formata e quebra o texto em até width colunas, com base como
// estilo de partida (cor do texto do balão).
func formatText(s string, width int, base lipgloss.Style) string {
	if width < 1 {
		width = 1
	}
	if hasBlockSyntax(s) {
		return formatBlocks(s, width, base)
	}
	lines := wrapCells(parseFormatted(s), width)
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = renderCells(l, base)
	}
	return strings.Join(out, "\n")
}

// wrapCells quebra por palavra; palavra maior que a linha é cortada.
func wrapCells(cells []cell, width int) [][]cell {
	var lines [][]cell
	var line []cell
	lineW := 0
	flush := func() {
		// espaço no fim da linha some, como no ansi.Wrap
		for len(line) > 0 && isSpaceCell(line[len(line)-1]) && line[len(line)-1].s != "\n" {
			lineW -= line[len(line)-1].w
			line = line[:len(line)-1]
		}
		lines = append(lines, line)
		line, lineW = nil, 0
	}
	i := 0
	for i < len(cells) {
		c := cells[i]
		if c.s == "\n" {
			flush()
			i++
			continue
		}
		if isSpaceCell(c) {
			if lineW > 0 && lineW+c.w <= width {
				line = append(line, c)
				lineW += c.w
			}
			i++
			continue
		}
		// palavra: até o próximo espaço ou quebra
		j := i
		ww := 0
		for j < len(cells) && !isSpaceCell(cells[j]) {
			ww += cells[j].w
			j++
		}
		if lineW > 0 && lineW+ww > width {
			flush()
		}
		for k := i; k < j; k++ {
			if lineW+cells[k].w > width && lineW > 0 {
				flush() // palavra maior que a linha: corta
			}
			line = append(line, cells[k])
			lineW += cells[k].w
		}
		i = j
	}
	flush()
	return lines
}

// renderCells agrupa células de mesmo estilo e desenha cada trecho.
func renderCells(cells []cell, base lipgloss.Style) string {
	var b strings.Builder
	for i := 0; i < len(cells); {
		j := i
		var run strings.Builder
		for j < len(cells) && cells[j].bits == cells[i].bits && cells[j].url == cells[i].url {
			run.WriteString(cells[j].s)
			j++
		}
		st := base
		bits := cells[i].bits
		if bits&fBold != 0 {
			st = st.Bold(true)
		}
		if bits&fItalic != 0 {
			st = st.Italic(true)
		}
		if bits&fStrike != 0 {
			st = st.Strikethrough(true)
		}
		if bits&fMono != 0 {
			st = st.Foreground(lipgloss.Color("6")) // o terminal já é mono: cor marca o código
		}
		if bits&fMention != 0 {
			st = st.Foreground(cGreen).Bold(true)
		}
		if u := cells[i].url; u != "" {
			st = st.Foreground(cGreen).Underline(true).Hyperlink(u)
		}
		b.WriteString(st.Render(run.String()))
		i = j
	}
	return b.String()
}

// firstURL devolve o primeiro link do texto (para o ctrl+o).
func firstURL(s string) string {
	for _, c := range cellsOf(s) {
		if c.url != "" {
			return c.url
		}
	}
	return ""
}

var listRe = regexp.MustCompile(`^(\d{1,3}\.|[*-]) +(\S.*)$`)

// hasBlockSyntax: o texto tem lista ("* ", "- ", "1. ") ou citação ("> ")
// no começo de alguma linha, fora de bloco de código.
func hasBlockSyntax(s string) bool {
	if strings.Contains(s, "```") {
		return false // blocos de código atravessam linhas; ficam como estão
	}
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "> ") || listRe.MatchString(l) {
			return true
		}
	}
	return false
}

// formatBlocks formata linha a linha: itens de lista com recuo pendurado
// (o marcador "*"/"-" vira "•") e citações com uma barra na cor fraca.
// Linhas comuns seguidas são formatadas juntas.
func formatBlocks(s string, width int, base lipgloss.Style) string {
	var out, plain []string
	flush := func() {
		if len(plain) > 0 {
			out = append(out, formatText(strings.Join(plain, "\n"), width, base))
			plain = nil
		}
	}
	indent := func(text, first, rest string) {
		w := max(1, width-lipgloss.Width(first))
		for i, l := range strings.Split(formatText(text, w, base), "\n") {
			lead := rest
			if i == 0 {
				lead = first
			}
			out = append(out, lead+l)
		}
	}
	for _, l := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(l, "> "):
			flush()
			bar := fg(cDivider).Render("▎") + " "
			indent(strings.TrimPrefix(l, "> "), bar, bar)
		case listRe.MatchString(l):
			flush()
			mm := listRe.FindStringSubmatch(l)
			mark := mm[1]
			if mark == "*" || mark == "-" {
				mark = "•"
			}
			first := sText2.Render(mark) + " "
			indent(mm[2], first, strings.Repeat(" ", lipgloss.Width(first)))
		default:
			plain = append(plain, l)
		}
	}
	flush()
	return strings.Join(out, "\n")
}
