package tui

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Cores do tema do terminal: o fundo e o texto padrão (os do kitty, com a
// transparência e o papel de parede) e a paleta ANSI 0–15. O layout imita o
// WhatsApp Web; as cores, não.
var (
	cDefault      color.Color = lipgloss.NoColor{}
	cBgList                   = cDefault            // lista de chats
	cBgChat                   = cDefault            // fundo da conversa
	cBgPanel                  = lipgloss.Color("0") // cabeçalho, busca, barra de escrita, pílulas
	cBgHover                  = lipgloss.Color("0") // item selecionado (com a barra de destaque)
	cDivider                  = lipgloss.Color("8")
	cText                     = cDefault
	cText2                    = lipgloss.Color("8")
	cGreen                    = lipgloss.Color("5") // destaque: o acento do tema
	cBadge                    = lipgloss.Color("5")
	cChipOnBg                 = lipgloss.Color("5")
	cChipOnFg                 = lipgloss.Color("0")
	cBubbleIn                 = lipgloss.Color("0")
	cBubbleOut                = lipgloss.Color("8")
	cBubbleInSel              = cBubbleIn // seleção é a barra ▌ ao lado do balão
	cBubbleOutSel             = cBubbleOut
	cQuoteIn                  = cDefault
	cQuoteOut                 = lipgloss.Color("0")
	cTimeOut                  = lipgloss.Color("7")
	cDayPill                  = lipgloss.Color("0")
	cDanger                   = lipgloss.Color("1")

	// Nomes em grupo: cores da paleta do terminal.
	senderColors = []string{"1", "2", "3", "4", "5", "6", "9", "10", "11", "12", "13", "14"}
	// Círculos com iniciais são imagens: precisam de RGB de verdade, então
	// usam tons suaves fixos.
	avatarColors = []string{"#53bdeb", "#fc9775", "#a5b337", "#e26ab6", "#25d366",
		"#ffd279", "#7f66ff", "#06cf9c", "#ff8a8c", "#5fc9ff", "#d3a4ff", "#ffbc38"}
)

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

var (
	sText  = fg(cText)
	sText2 = fg(cText2)
	sGreen = fg(cGreen)
	sBold  = fg(cText).Bold(true)
)

// Ícones da Nerd Font (o terminal dele usa JetBrainsMono NF). Sem Nerd
// Font, ZAPTERM_NERDFONT=0 troca por texto simples.
type iconSet struct {
	search, mute, pin, archive, back, reply, send, attach, close string
	photo, audio, video, doc, sticker, pillL, pillR, check       string
	play, pause, rec                                             string
}

var icons = func() iconSet {
	if os.Getenv("ZAPTERM_NERDFONT") == "0" {
		return iconSet{search: "/", mute: "(m)", pin: "^", archive: "[A]", back: "<", reply: ">",
			send: ">", attach: "+", close: "x", photo: "[foto]", audio: "[áudio]", video: "[vídeo]",
			doc: "[doc]", sticker: "[fig]", pillL: "", pillR: "", check: "v",
			play: ">", pause: "||", rec: "*"}
	}
	return iconSet{
		search:  "\U000f0349", // nf-md-magnify
		mute:    "\U000f009b", // nf-md-bell_off
		pin:     "\U000f0403", // nf-md-pin
		archive: "\U000f003c", // nf-md-archive
		back:    "\U000f004d", // nf-md-arrow_left
		reply:   "\U000f045a", // nf-md-reply
		send:    "\U000f048a", // nf-md-send
		attach:  "\U000f0066", // nf-md-attachment
		close:   "\U000f0156", // nf-md-close
		photo:   "\U000f02e9", // nf-md-image
		audio:   "\U000f036c", // nf-md-microphone
		video:   "\U000f0567", // nf-md-video
		doc:     "\U000f0219", // nf-md-file_document
		sticker: "\U000f0785", // nf-md-sticker_emoji
		pillL:   "",          // meia-lua esquerda (powerline)
		pillR:   "",
		check:   "\U000f012c", // nf-md-check
		play:    "\U000f040a", // nf-md-play
		pause:   "\U000f03e4", // nf-md-pause
		rec:     "\U000f044a", // nf-md-record
	}
}()

// pill desenha um rótulo com pontas arredondadas sobre o fundo outer.
func pill(text string, textFg, pillBg, outer color.Color, bold bool) string {
	edge := lipgloss.NewStyle().Foreground(pillBg).Background(outer)
	body := lipgloss.NewStyle().Foreground(textFg).Background(pillBg).Bold(bold)
	if icons.pillL == "" {
		return body.Render(" " + text + " ")
	}
	return edge.Render(icons.pillL) + body.Render(text) + edge.Render(icons.pillR)
}

// paint pinta o fundo de um bloco inteiro: cada linha começa com a cor de
// fundo, que é reaplicada depois de cada reset dos trechos internos (o
// lipgloss fecha cada estilo com \x1b[m, o que apagaria o fundo) e
// completada com espaços até w colunas. Trechos com fundo próprio (balão,
// contador, imagem) continuam valendo até o reset deles.
func paint(block string, bg color.Color, w int) string {
	seq := bgSeq(bg)
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			l = ansi.Truncate(l, w, "")
		}
		l = strings.ReplaceAll(l, "\x1b[m", "\x1b[m"+seq)
		l = strings.ReplaceAll(l, "\x1b[0m", "\x1b[0m"+seq)
		pad := max(0, w-ansi.StringWidth(l))
		lines[i] = seq + l + strings.Repeat(" ", pad) + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}

// bgSeq devolve o SGR de fundo: padrão do terminal, cor ANSI do tema ou,
// só se vier uma cor RGB, truecolor.
func bgSeq(c color.Color) string {
	switch v := c.(type) {
	case nil, lipgloss.NoColor:
		return "\x1b[49m"
	case ansi.BasicColor:
		if v < 8 {
			return fmt.Sprintf("\x1b[%dm", 40+int(v))
		}
		return fmt.Sprintf("\x1b[%dm", 100+int(v)-8)
	case ansi.IndexedColor:
		return fmt.Sprintf("\x1b[48;5;%dm", int(v))
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// blank devolve h linhas vazias pintadas.
func blank(bg color.Color, w, h int) string {
	if h <= 0 {
		return ""
	}
	return paint(strings.Repeat("\n", h-1), bg, w)
}

// fitHeight corta ou completa um bloco para exatamente h linhas.
func fitHeight(block string, bg color.Color, w, h int) string {
	lines := strings.Split(block, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	out := strings.Join(lines, "\n")
	if n := h - len(lines); n > 0 {
		out += "\n" + blank(bg, w, n)
	}
	return out
}
