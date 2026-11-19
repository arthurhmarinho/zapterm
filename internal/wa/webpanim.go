package wa

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Decodificador de WebP animado em Go puro. O golang.org/x/image/webp só
// lê WebP estático; o animado é um contêiner RIFF com um quadro (ANMF) por
// chunk, e cada quadro carrega um bitstream VP8/VP8L comum (mais ALPH, se
// tiver transparência). Aqui cada quadro é embrulhado num WebP estático
// mínimo, decodificado pelo x/image, e composto na tela da animação
// respeitando mistura e descarte, como manda a especificação:
// https://developers.google.com/speed/webp/docs/riff_container

const (
	animMaxFrames = 60  // mais que isso: pula quadros e soma as durações
	animFramePx   = 160 // lado dos quadros guardados (a TUI mostra ~144px)
	animMinDelay  = 20  // ms; durações 0 viram isso, como nos navegadores
)

var errNotAnimated = errors.New("webp não é animado")

type animFrame struct {
	img   image.Image // tela inteira já composta, reduzida a animFramePx
	delay int         // ms
}

func decodeAnimatedWebP(data []byte) ([]animFrame, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, errors.New("não é webp")
	}
	var canvasW, canvasH int
	var frames []animFrame
	var canvas *image.RGBA
	var prevRect image.Rectangle
	prevDispose := false

	body := data[12:]
	for len(body) >= 8 {
		fourcc := string(body[0:4])
		size := int(binary.LittleEndian.Uint32(body[4:8]))
		if size < 0 || 8+size > len(body) {
			return nil, errors.New("chunk truncado")
		}
		payload := body[8 : 8+size]
		next := 8 + size + size&1 // chunks são alinhados em 2 bytes
		if next > len(body) {
			next = len(body)
		}
		body = body[next:]

		switch fourcc {
		case "VP8X":
			if len(payload) < 10 {
				return nil, errors.New("VP8X curto")
			}
			canvasW = int(u24(payload[4:7])) + 1
			canvasH = int(u24(payload[7:10])) + 1
			canvas = image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
		case "ANMF":
			if canvas == nil || len(payload) < 16 {
				return nil, errors.New("ANMF sem VP8X")
			}
			x, y := int(u24(payload[0:3]))*2, int(u24(payload[3:6]))*2
			w, h := int(u24(payload[6:9]))+1, int(u24(payload[9:12]))+1
			delay := int(u24(payload[12:15]))
			flags := payload[15]
			noBlend, dispose := flags&0x02 != 0, flags&0x01 != 0

			img, err := decodeFrame(payload[16:], w, h)
			if err != nil {
				return nil, err
			}
			if prevDispose {
				draw.Draw(canvas, prevRect, image.Transparent, image.Point{}, draw.Src)
			}
			rect := image.Rect(x, y, x+w, y+h).Intersect(canvas.Bounds())
			op := draw.Over
			if noBlend {
				op = draw.Src
			}
			draw.Draw(canvas, rect, img, img.Bounds().Min, op)
			frames = append(frames, animFrame{img: shrink(canvas), delay: max(delay, animMinDelay)})
			prevRect, prevDispose = rect, dispose
		}
	}
	if len(frames) < 2 {
		return nil, errNotAnimated
	}
	return thinFrames(frames), nil
}

// decodeFrame embrulha o bitstream de um quadro num WebP estático mínimo e
// usa o decodificador do x/image.
func decodeFrame(chunks []byte, w, h int) (image.Image, error) {
	var buf bytes.Buffer
	hasAlpha := len(chunks) >= 4 && string(chunks[0:4]) == "ALPH"
	if hasAlpha {
		// VP8X só com a flag de alfa (0x10) e o tamanho do quadro.
		vp8x := make([]byte, 10)
		vp8x[0] = 0x10
		put24(vp8x[4:7], uint32(w-1))
		put24(vp8x[7:10], uint32(h-1))
		writeChunk(&buf, "VP8X", vp8x)
	}
	buf.Write(chunks) // ALPH (se houver) + VP8/VP8L, já como chunks
	riff := new(bytes.Buffer)
	riff.WriteString("RIFF")
	binary.Write(riff, binary.LittleEndian, uint32(4+buf.Len()))
	riff.WriteString("WEBP")
	riff.Write(buf.Bytes())
	return webp.Decode(riff)
}

func shrink(src *image.RGBA) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if s := max(w, h); s > animFramePx {
		w, h = w*animFramePx/s, h*animFramePx/s
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	// ApproxBiLinear: em 160px não se vê diferença e é bem mais rápido que
	// CatmullRom para dezenas de quadros.
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return dst
}

// thinFrames limita a animMaxFrames pulando quadros; a duração dos pulados
// vai para o quadro mantido, então o tempo total não muda.
func thinFrames(frames []animFrame) []animFrame {
	if len(frames) <= animMaxFrames {
		return frames
	}
	step := (len(frames) + animMaxFrames - 1) / animMaxFrames
	var out []animFrame
	for i := 0; i < len(frames); i += step {
		f := frames[i]
		for j := i + 1; j < min(i+step, len(frames)); j++ {
			f.delay += frames[j].delay
		}
		out = append(out, f)
	}
	return out
}

func u24(b []byte) uint32 { return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 }

func put24(b []byte, v uint32) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }

func writeChunk(buf *bytes.Buffer, fourcc string, payload []byte) {
	buf.WriteString(fourcc)
	binary.Write(buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)
	if len(payload)&1 == 1 {
		buf.WriteByte(0)
	}
}
