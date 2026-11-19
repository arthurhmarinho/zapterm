package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Imagens no kitty com "Unicode placeholders": a imagem é transmitida uma
// vez (o kitty lê o PNG direto do arquivo) e depois desenhada como texto,
// um caractere especial por célula, colorido com o id da imagem. Por ser
// texto, ela rola com o viewport, respeita as bordas e convive com o
// renderer do Bubble Tea, que não sabe nada de gráficos.
//
// https://sw.kovidgoyal.net/kitty/graphics-protocol/#unicode-placeholders

// Tamanho presumido da célula em pixels (kitty, fonte ~11pt). Só serve para
// escolher quantas colunas e linhas a imagem ocupa; o kitty encaixa a
// imagem nesse retângulo mantendo a proporção.
const cellW, cellH = 9, 19

type kimage struct {
	id         uint32
	cols, rows int
}

type images struct {
	enabled bool
	next    uint32
	placed  map[string]*kimage // chave: caminho + tamanho
	pending []string           // comandos de transmissão ainda não enviados
}

func newImages() *images {
	on := os.Getenv("TERM") == "xterm-kitty" || os.Getenv("KITTY_WINDOW_ID") != ""
	switch os.Getenv("ZAPTERM_IMAGES") {
	case "0":
		on = false
	case "1":
		on = true // força, para testes fora do kitty
	}
	return &images{enabled: on, next: 1, placed: map[string]*kimage{}}
}

// block devolve a imagem como bloco de texto de até maxCols×maxRows
// células, ou "" se não houver suporte. A transmissão fica pendente até
// flush().
func (im *images) block(path string, pxW, pxH, maxCols, maxRows int) string {
	return im.place(path, nil, nil, pxW, pxH, maxCols, maxRows)
}

// media desenha a mídia de uma mensagem: animada (figurinha) se tiver
// quadros, senão a prévia estática.
func (im *images) media(md *ipc.Media, maxCols, maxRows int) string {
	if md == nil || md.Preview == "" {
		return ""
	}
	if len(md.Frames) > 1 && len(md.Delays) == len(md.Frames) {
		return im.place(md.Frames[0], md.Frames[1:], md.Delays, md.Width, md.Height, maxCols, maxRows)
	}
	return im.block(md.Preview, md.Width, md.Height, maxCols, maxRows)
}

// place transmite a imagem (e, se houver, os quadros da animação) e devolve
// os placeholders. Animação no kitty: o primeiro quadro vem no a=T, os
// outros em a=f com a duração em z, o r=1 recebe a duração do primeiro e
// s=3,v=1 põe para rodar em loop. O kitty anima sozinho; a TUI não
// redesenha nada.
func (im *images) place(path string, frames []string, delays []int, pxW, pxH, maxCols, maxRows int) string {
	if !im.enabled || path == "" || pxW <= 0 || pxH <= 0 || maxCols < 2 || maxRows < 1 {
		return ""
	}
	cols := min(maxCols, (pxW+cellW-1)/cellW)
	rows := max(1, (cols*cellW*pxH/pxW+cellH/2)/cellH)
	if rows > maxRows { // foto em pé muito alta: limita a altura
		rows = maxRows
		cols = max(2, min(cols, (rows*cellH*pxW/pxH)/cellW))
	}
	key := fmt.Sprintf("%s|%dx%d", path, cols, rows)
	k := im.placed[key]
	if k == nil {
		k = &kimage{id: im.next, cols: cols, rows: rows}
		im.next++
		im.placed[key] = k
		im.pending = append(im.pending, fmt.Sprintf("\x1b_Ga=T,U=1,f=100,t=f,i=%d,c=%d,r=%d,q=2;%s\x1b\\",
			k.id, cols, rows, base64.StdEncoding.EncodeToString([]byte(path))))
		if len(frames) > 0 {
			for i, f := range frames {
				im.pending = append(im.pending, fmt.Sprintf("\x1b_Ga=f,i=%d,f=100,t=f,z=%d,q=2;%s\x1b\\",
					k.id, delays[i+1], base64.StdEncoding.EncodeToString([]byte(f))))
			}
			im.pending = append(im.pending,
				fmt.Sprintf("\x1b_Ga=a,i=%d,r=1,z=%d,q=2\x1b\\", k.id, delays[0]),
				fmt.Sprintf("\x1b_Ga=a,i=%d,s=3,v=1,q=2\x1b\\", k.id))
		}
	}
	return placeholder(k)
}

func placeholder(k *kimage) string {
	// O id vai na cor do texto (24 bits).
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (k.id>>16)&0xff, (k.id>>8)&0xff, k.id&0xff)
	var b strings.Builder
	for r := 0; r < k.rows; r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fg)
		for c := 0; c < k.cols; c++ {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[39m")
	}
	return b.String()
}

// flush manda ao terminal as transmissões pendentes.
func (im *images) flush() tea.Cmd {
	if len(im.pending) == 0 {
		return nil
	}
	seq := strings.Join(im.pending, "")
	im.pending = nil
	return tea.Raw(seq)
}

// cleanup apaga do kitty as imagens desta sessão; roda depois que a TUI
// sai, com o terminal já fora do modo alternativo.
func (im *images) cleanup() {
	if !im.enabled {
		return
	}
	var b strings.Builder
	for _, k := range im.placed {
		fmt.Fprintf(&b, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", k.id)
	}
	os.Stdout.WriteString(b.String())
}

// openExternal abre um arquivo no programa padrão (visualizador de imagem,
// player, leitor de PDF), desligado do terminal.
func openExternal(path string) error {
	cmd := exec.Command("xdg-open", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
