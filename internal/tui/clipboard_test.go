package tui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestSaveAttachment(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	src := image.NewRGBA(image.Rect(0, 0, 40, 20))
	src.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	a, err := saveAttachment(buf.Bytes(), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if a.w != 40 || a.h != 20 {
		t.Fatalf("tamanho %dx%d", a.w, a.h)
	}
	for _, p := range []string{a.path, a.preview} {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", p, err, st)
		}
	}
}

func TestFallbackAvatar(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := fallbackAvatarFile("5584@s.whatsapp.net", "Samuel Souza")
	if f == "" {
		t.Fatal("avatar com iniciais não foi gerado")
	}
	if g := fallbackAvatarFile("5584@s.whatsapp.net", "Samuel Souza"); g != f {
		t.Fatal("deveria reaproveitar o arquivo do cache")
	}
}

func instalados(nomes ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, x := range nomes {
			if x == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("não achei")
	}
}

func TestPickClipboard(t *testing.T) {
	wayland := func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-1"
		}
		return ""
	}
	x11 := func(string) string { return "" }
	casos := []struct {
		nome    string
		env     func(string) string
		instal  []string
		want    string
		imagem  bool
		wantErr bool
	}{
		{"wayland usa wl-clipboard", wayland, []string{"wl-copy", "wl-paste", "xclip"}, "wl-clipboard", true, false},
		{"x11 usa xclip mesmo com wl-clipboard", x11, []string{"wl-copy", "wl-paste", "xclip", "xsel"}, "xclip", true, false},
		{"x11 sem xclip cai no xsel (só texto)", x11, []string{"xsel"}, "xsel", false, false},
		{"wayland sem wl-paste cai no xclip", wayland, []string{"wl-copy", "xclip"}, "xclip", true, false},
		{"nada instalado", x11, nil, "", false, true},
	}
	for _, c := range casos {
		tool, err := pickClipboard(c.env, instalados(c.instal...))
		if (err != nil) != c.wantErr || tool.name != c.want || (tool.image != nil) != c.imagem {
			t.Errorf("%s: %q %v", c.nome, tool.name, err)
		}
	}
}

func TestComandosDoClipboard(t *testing.T) {
	x := func(string) string { return "" }
	tool, _ := pickClipboard(x, instalados("xclip"))
	if got := strings.Join(tool.copy, " "); got != "xclip -selection clipboard" {
		t.Errorf("copiar: %s", got)
	}
	if got := strings.Join(tool.image("image/png"), " "); got != "xclip -selection clipboard -t image/png -o" {
		t.Errorf("imagem: %s", got)
	}
	tool, _ = pickClipboard(x, instalados("xsel"))
	if got := strings.Join(tool.copy, " "); got != "xsel -b -i" {
		t.Errorf("xsel: %s", got)
	}
	if pickImageType([]string{"text/plain", "image/jpeg", "image/png"}) != "image/png" || pickImageType([]string{"text/plain"}) != "" {
		t.Error("pickImageType")
	}
}

func TestPickMicSource(t *testing.T) {
	tem := func(nomes ...string) func(string) bool {
		return func(e string) bool { return slices.Contains(nomes, e) }
	}
	if got := pickMicSource(tem("pulsesrc", "pipewiresrc")); got != "pulsesrc" {
		t.Errorf("pulsesrc primeiro: %s", got)
	}
	if got := pickMicSource(tem("pipewiresrc")); got != "pipewiresrc" {
		t.Errorf("pipewiresrc: %s", got)
	}
	if got := pickMicSource(tem()); got != "autoaudiosrc" {
		t.Errorf("autoaudiosrc: %s", got)
	}
}
