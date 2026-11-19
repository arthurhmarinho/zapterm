package tui

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Avatar de quem não tem foto: círculo colorido com as iniciais, gerado
// aqui mesmo (sem rede) e guardado no cache para o kitty ler do arquivo.

const fallbackPx = 96

var (
	faceOnce sync.Once
	face     font.Face
)

func initialsFace() font.Face {
	faceOnce.Do(func() {
		f, err := opentype.Parse(gobold.TTF)
		if err != nil {
			return
		}
		face, _ = opentype.NewFace(f, &opentype.FaceOptions{Size: 40, DPI: 72, Hinting: font.HintingFull})
	})
	return face
}

func colorFor(jid string) color.RGBA {
	h := fnv.New32a()
	h.Write([]byte(jid))
	var c color.RGBA
	fmt.Sscanf(senderColors[h.Sum32()%uint32(len(senderColors))], "#%02x%02x%02x", &c.R, &c.G, &c.B)
	c.A = 255
	return c
}

// fallbackAvatarFile devolve (gerando se preciso) o PNG do avatar com
// iniciais. "" se algo falhar; aí fica o bloco de texto.
func fallbackAvatarFile(jid, name string) string {
	dir, err := paths.AvatarDir()
	if err != nil {
		return ""
	}
	ini := initials(name)
	c := colorFor(jid)
	file := filepath.Join(dir, fmt.Sprintf("fallback-%02x%02x%02x-%x.png", c.R, c.G, c.B, ini))
	if _, err := os.Stat(file); err == nil {
		return file
	}
	img := circle(fallbackPx, c)
	if fc := initialsFace(); fc != nil {
		d := font.Drawer{Dst: img, Src: image.NewUniform(color.RGBA{0x11, 0x1b, 0x21, 0xff}), Face: fc}
		w := d.MeasureString(ini)
		m := fc.Metrics()
		d.Dot = fixed.Point26_6{
			X: (fixed.I(fallbackPx) - w) / 2,
			Y: (fixed.I(fallbackPx)+m.Ascent-m.Descent)/2 - fixed.I(2),
		}
		d.DrawString(ini)
	}
	tmp := file + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return ""
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return ""
	}
	f.Close()
	if os.Rename(tmp, file) != nil {
		return ""
	}
	return file
}

// circle devolve um quadrado transparente com um círculo de cor c, com a
// borda suavizada.
func circle(px int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	r := float64(px) / 2
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			a := coverage(dx*dx+dy*dy, r)
			if a > 0 {
				img.SetRGBA(x, y, color.RGBA{
					uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(255 * a)})
			}
		}
	}
	return img
}

// coverage: 1 dentro do círculo, 0 fora, rampa de 1px na borda.
func coverage(d2, r float64) float64 {
	switch {
	case d2 <= (r-1)*(r-1):
		return 1
	case d2 >= r*r:
		return 0
	}
	// aproximação linear na faixa da borda
	return (r*r - d2) / (r*r - (r-1)*(r-1))
}
