package wa

import (
	"bytes"
	"image"
	"image/color"
	"os/exec"
	"testing"

	"golang.org/x/image/webp"
)

func TestFotoViraFigurinha(t *testing.T) {
	if exec.Command("gst-inspect-1.0", "webpenc").Run() != nil {
		t.Skip("sem webpenc no GStreamer")
	}
	src := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for x := 0; x < 800; x++ {
		for y := 0; y < 400; y++ {
			src.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	data, pngData, err := makeSticker(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > stickerMaxBytes || len(pngData) == 0 {
		t.Fatalf("tamanho %d", len(data))
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		t.Fatalf("%v", b)
	}
	// Foto deitada: em cima e embaixo sobra fundo transparente.
	if _, _, _, a := img.At(256, 10).RGBA(); a != 0 {
		t.Errorf("faixa de cima não ficou transparente (alfa %d)", a)
	}
	if _, _, _, a := img.At(256, 256).RGBA(); a == 0 {
		t.Error("o meio ficou transparente")
	}
}
