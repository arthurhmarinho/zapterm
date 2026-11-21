package tui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// O corte preenche a célula: uma imagem deitada vira quadrada, tirando
// as laterais (que são vermelhas aqui; o centro é azul).
func TestCorteNoCentro(t *testing.T) {
	src := filepath.Join(t.TempDir(), "deitada.png")
	img := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for x := 0; x < 300; x++ {
		for y := 0; y < 100; y++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 100 && x < 200 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	f, _ := os.Create(src)
	png.Encode(f, img)
	f.Close()
	out, ok := coverCrop(src, 64, 64)
	if !ok {
		t.Fatal("não cortou")
	}
	f, _ = os.Open(out)
	got, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if b := got.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Fatalf("tamanho %v", b)
	}
	for _, p := range [][2]int{{2, 32}, {61, 32}, {32, 32}} {
		r, _, b, _ := got.At(p[0], p[1]).RGBA()
		if r > b {
			t.Errorf("pixel %v ficou vermelho: sobrou lateral", p)
		}
	}
}
