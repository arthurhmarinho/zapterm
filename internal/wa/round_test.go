package wa

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"path/filepath"
	"testing"
)

// O recado de vídeo vira um círculo: quadrado do centro, cantos
// transparentes, miolo opaco e borda suavizada.
func TestMiniaturaRedonda(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 120, 80)) // deitada, como a miniatura
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			src.Set(x, y, color.RGBA{200, 40, 40, 255})
		}
	}
	out := roundCrop(src)
	if b := out.Bounds(); b.Dx() != 80 || b.Dy() != 80 {
		t.Fatalf("tamanho %v, quer 80×80", b)
	}
	if a := out.NRGBAAt(40, 40).A; a != 255 {
		t.Errorf("centro com alfa %d", a)
	}
	if a := out.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("canto com alfa %d", a)
	}
	edge := false
	for x := 0; x < 40; x++ {
		if a := out.NRGBAAt(x, 40).A; a > 0 && a < 255 {
			edge = true
		}
	}
	if !edge {
		t.Error("borda sem suavização")
	}

	// Pelo caminho da miniatura embutida: PNG quadrado, tamanho de balão.
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, src, nil); err != nil {
		t.Fatal(err)
	}
	md := pngThumb(filepath.Join(t.TempDir(), "x.round.png"), jpg.Bytes(), true)
	if md == nil || md.Width != md.Height {
		t.Fatalf("mídia redonda: %+v", md)
	}
}
