package wa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/image/draw"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Foto vira figurinha: encaixada em 512×512 com fundo transparente e
// codificada em WebP (o formato das figurinhas) pelo GStreamer.

const stickerMaxBytes = 100 << 10 // limite do WhatsApp para figurinha parada

// makeSticker devolve o WebP e o PNG 512×512 (para a prévia).
func makeSticker(src image.Image) (webp, pngData []byte, err error) {
	dst := image.NewRGBA(image.Rect(0, 0, 512, 512))
	b := src.Bounds()
	w, h := 512, 512
	if b.Dx() > b.Dy() {
		h = 512 * b.Dy() / b.Dx()
	} else {
		w = 512 * b.Dx() / b.Dy()
	}
	x0, y0 := (512-w)/2, (512-h)/2
	draw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+w, y0+h), src, b, draw.Over, nil)
	var pbuf bytes.Buffer
	if err := png.Encode(&pbuf, dst); err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "zapterm-figurinha")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.png"), filepath.Join(dir, "out.webp")
	if err := os.WriteFile(in, pbuf.Bytes(), 0o600); err != nil {
		return nil, nil, err
	}
	// Qualidade cai até caber no limite.
	for _, q := range []int{90, 75, 60, 45} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-q", "filesrc", "location="+in, "!", "pngdec", "!",
			"videoconvert", "!", "video/x-raw,format=RGBA", "!", "webpenc", fmt.Sprintf("quality=%d", q), "!",
			"filesink", "location="+out)
		o, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return nil, nil, fmt.Errorf("gst webpenc: %v %s", err, bytes.TrimSpace(o))
		}
		data, err := os.ReadFile(out)
		if err != nil {
			return nil, nil, err
		}
		if len(data) <= stickerMaxBytes {
			return data, pbuf.Bytes(), nil
		}
	}
	return nil, nil, errors.New("a figurinha ficou grande demais (mais de 100 KB)")
}

func (d *Daemon) sendStickerFile(ctx context.Context, chat, path, replyTo string) (any, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("não é uma imagem: %w", err)
	}
	webp, pngData, err := makeSticker(src)
	if err != nil {
		return nil, err
	}
	up, err := d.cli.Upload(ctx, webp, whatsmeow.MediaImage)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	ci, quote, err := d.replyContext(chat, replyTo)
	if err != nil {
		return nil, err
	}
	sm := &waE2E.StickerMessage{
		Mimetype: proto.String("image/webp"), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
		MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
		FileLength: proto.Uint64(up.FileLength), Width: proto.Uint32(512), Height: proto.Uint32(512), ContextInfo: ci,
	}
	m := ipc.Message{Kind: "sticker", Text: "[figurinha]", Quote: quote}
	return d.sendAndStore(ctx, jid, &waE2E.Message{StickerMessage: sm}, m, func(id string) *ipc.Media {
		base, err := mediaBase(id)
		if err != nil {
			return nil
		}
		md := &ipc.Media{Mime: "image/webp", Path: base + ".webp", Preview: base + ".preview.png", Width: 512, Height: 512}
		if os.WriteFile(md.Path, webp, 0o600) != nil || os.WriteFile(md.Preview, pngData, 0o600) != nil {
			return nil
		}
		return md
	})
}
