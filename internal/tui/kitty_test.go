package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// O desenho depende de cada placeholder (U+10EEEE + 2 diacríticos) contar
// como uma coluna para o lipgloss e o ansi; senão as bordas quebram.
func TestPlaceholderLargura(t *testing.T) {
	p := placeholder(&kimage{id: 70000, cols: 12, rows: 3})
	lines := strings.Split(p, "\n")
	if len(lines) != 3 {
		t.Fatalf("linhas = %d", len(lines))
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w != 12 {
			t.Fatalf("lipgloss.Width = %d, quer 12", w)
		}
		if w := ansi.StringWidth(ansi.Truncate(l, 12, "")); w != 12 {
			t.Fatalf("Truncate mudou a largura: %d", w)
		}
	}
	b := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(p)
	for _, l := range strings.Split(b, "\n") {
		if w := lipgloss.Width(l); w != 16 {
			t.Fatalf("balão com largura %d, quer 16: %q", w, l)
		}
	}
	if !strings.Contains(b, "\x1b[38;2;1;17;112m") {
		t.Fatal("a cor do id (70000 = 1,17,112) se perdeu dentro do balão")
	}
}

// Animação: um a=T, um a=f por quadro extra (com a duração), a duração do
// primeiro quadro e o comando que põe para rodar em loop.
func TestAnimacaoKitty(t *testing.T) {
	im := &images{enabled: true, next: 1, placed: map[string]*kimage{}}
	md := &ipc.Media{Preview: "/a/0.png", Width: 160, Height: 160,
		Frames: []string{"/a/0.png", "/a/1.png", "/a/2.png"}, Delays: []int{100, 50, 70}}
	if im.media(md, 10, 5) == "" {
		t.Fatal("sem placeholder")
	}
	seq := strings.Join(im.pending, "")
	for _, want := range []string{"a=T,U=1", "a=f,i=1,f=100,t=f,z=50", "a=f,i=1,f=100,t=f,z=70",
		"a=a,i=1,r=1,z=100", "a=a,i=1,s=3,v=1"} {
		if !strings.Contains(seq, want) {
			t.Errorf("faltou %q", want)
		}
	}
	// Desenhar de novo não retransmite.
	im.pending = nil
	im.media(md, 10, 5)
	if len(im.pending) != 0 {
		t.Fatal("retransmitiu a animação")
	}
}

// ctrl+o numa foto baixada abre em tela cheia (sem xdg-open) e a imagem é
// transmitida no mesmo Update, não só na próxima tecla.
func TestFotoEmTelaCheia(t *testing.T) {
	m := newModel(nil)
	m.imgs.enabled = true
	m.w, m.h = 120, 40
	m.active = "x@s.whatsapp.net"
	m.msgs = []ipc.Message{{ID: "f", Kind: "image", Text: "[imagem] praia",
		Media: &ipc.Media{Path: "/x/f.jpg", Preview: "/x/f.preview.png", Width: 720, Height: 480}}}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.viewing == nil {
		t.Fatal("não abriu em tela cheia")
	}
	if cmd == nil {
		t.Fatal("a transmissão da foto não saiu neste Update")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "praia") {
		t.Fatal("legenda não apareceu")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.viewing != nil {
		t.Fatal("esc não fechou")
	}
}

func TestOpener(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, x := range names {
				if x == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", os.ErrNotExist
		}
	}
	cases := []struct {
		path     string
		look     func(string) (string, error)
		wantProg string
	}{
		{"/c/a.pdf", has("zathura", "sioyek"), "zathura"},
		{"/c/a.pdf", has("sioyek"), "sioyek"},
		{"/c/a.pdf", has(), "xdg-open"},
		{"/c/v.mp4", has("mpv", "gst-launch-1.0"), "mpv"},
		{"/c/v.mp4", has("gst-launch-1.0"), "gst-launch-1.0"},
		{"/c/v.mp4", has(), "xdg-open"},
		{"/c/f.jpg", has("imv"), "imv"},
		{"/c/f.jpg", has(), "xdg-open"},
		{"/c/d.docx", has("zathura", "mpv"), "xdg-open"},
	}
	for _, c := range cases {
		if got, _ := opener(c.path, c.look); got != c.wantProg {
			t.Errorf("%s: %s, quer %s", c.path, got, c.wantProg)
		}
	}
}
