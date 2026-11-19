package tui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
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
