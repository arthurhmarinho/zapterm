package wa

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestFotoDeGrupoQuadrada(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for x := 0; x < 300; x++ {
		for y := 0; y < 200; y++ {
			src.Set(x, y, color.RGBA{R: uint8(x), A: 255})
		}
	}
	path := filepath.Join(t.TempDir(), "foto.png")
	var buf bytes.Buffer
	png.Encode(&buf, src)
	os.WriteFile(path, buf.Bytes(), 0o600)
	jpg, err := squareJPEG(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 200 {
		t.Fatalf("tamanho %v, quer 200×200", b)
	}
	if _, err := squareJPEG(filepath.Join(t.TempDir(), "nada.png")); err == nil {
		t.Error("arquivo inexistente passou")
	}
}
