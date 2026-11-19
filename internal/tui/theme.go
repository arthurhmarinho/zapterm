package tui

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Paleta do WhatsApp Web no tema escuro. A TUI imita o app de propósito,
// então não herda as cores do terminal.
var (
	cBgList       = lipgloss.Color("#111b21") // lista de chats
	cBgChat       = lipgloss.Color("#0b141a") // fundo da conversa
	cBgPanel      = lipgloss.Color("#202c33") // cabeçalho, busca, barra de escrita
	cBgHover      = lipgloss.Color("#2a3942") // item selecionado, campo de texto
	cDivider      = lipgloss.Color("#222d34")
	cText         = lipgloss.Color("#e9edef")
	cText2        = lipgloss.Color("#8696a0")
	cGreen        = lipgloss.Color("#00a884")
	cBadge        = lipgloss.Color("#25d366")
	cChipOnBg     = lipgloss.Color("#0a332c")
	cChipOnFg     = lipgloss.Color("#d9fdd3")
	cBubbleIn     = lipgloss.Color("#202c33")
	cBubbleOut    = lipgloss.Color("#005c4b")
	cBubbleInSel  = lipgloss.Color("#33444f")
	cBubbleOutSel = lipgloss.Color("#0a7a65")
	cQuoteIn      = lipgloss.Color("#1d282f")
	cQuoteOut     = lipgloss.Color("#025144")
	cTimeOut      = lipgloss.Color("#a3cfc5")
	cDayPill      = lipgloss.Color("#182229")
	cDanger       = lipgloss.Color("#f15c6d")

	// Nomes em grupo: cores vivas como as do app.
	senderColors = []string{"#53bdeb", "#fc9775", "#a5b337", "#e26ab6", "#25d366",
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
}

var icons = func() iconSet {
	if os.Getenv("ZAPTERM_NERDFONT") == "0" {
		return iconSet{search: "/", mute: "(m)", pin: "^", archive: "[A]", back: "<", reply: ">",
			send: ">", attach: "+", close: "x", photo: "[foto]", audio: "[áudio]", video: "[vídeo]",
			doc: "[doc]", sticker: "[fig]", pillL: "", pillR: "", check: "v"}
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

func bgSeq(c color.Color) string {
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
