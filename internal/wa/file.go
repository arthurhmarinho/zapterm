package wa

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Limite do WhatsApp para documento (2 GB); acima disso nem tenta.
const maxFileBytes = 2 << 30

// sendFile envia um arquivo escolhido no clipe. O tipo decide a forma:
// foto vai como foto; MP4 como vídeo; áudio como arquivo de áudio; o resto
// (inclusive vídeo de outro formato, que o celular não toca) como
// documento.
func (d *Daemon) sendFile(ctx context.Context, chat, path, caption, replyTo string, o sendOpts) (any, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxFileBytes {
		return nil, fmt.Errorf("arquivo maior que 2 GB")
	}
	mt := fileMime(path)
	switch {
	case strings.HasPrefix(mt, "image/") && mt != "image/svg+xml" && o.Sticker:
		return d.sendStickerFile(ctx, chat, path, replyTo)
	case strings.HasPrefix(mt, "image/") && mt != "image/svg+xml":
		return d.sendImage(ctx, chat, path, caption, replyTo, o)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ci, quote, err := d.replyContext(chat, replyTo)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	caption = strings.TrimSpace(caption)
	var msg *waE2E.Message
	var m ipc.Message

	switch {
	case mt == "video/mp4":
		up, err := d.cli.Upload(ctx, data, whatsmeow.MediaVideo)
		if err != nil {
			return nil, fmt.Errorf("upload: %w", err)
		}
		secs := mediaSeconds(ctx, path)
		vm := &waE2E.VideoMessage{
			Mimetype: proto.String(mt), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: proto.Uint64(up.FileLength), Seconds: proto.Uint32(uint32(secs)), ContextInfo: ci,
		}
		if caption != "" {
			vm.Caption = proto.String(caption)
		}
		msg = &waE2E.Message{VideoMessage: vm}
		m = ipc.Message{Kind: "video", Text: tag(fmt.Sprintf("vídeo %d:%02d", secs/60, secs%60), caption)}
		if o.Once {
			vm.ViewOnce = proto.Bool(true)
			msg = &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: msg}}
			m = ipc.Message{Kind: "other", Text: "[visualização única]"}
		}

	case strings.HasPrefix(mt, "audio/"):
		up, err := d.cli.Upload(ctx, data, whatsmeow.MediaAudio)
		if err != nil {
			return nil, fmt.Errorf("upload: %w", err)
		}
		secs := mediaSeconds(ctx, path)
		msg = &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			Mimetype: proto.String(mt), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: proto.Uint64(up.FileLength), Seconds: proto.Uint32(uint32(secs)), PTT: proto.Bool(false),
			ContextInfo: ci,
		}}
		m = ipc.Message{Kind: "audio", Text: fmt.Sprintf("[arquivo de áudio %d:%02d]", secs/60, secs%60)}

	default:
		up, err := d.cli.Upload(ctx, data, whatsmeow.MediaDocument)
		if err != nil {
			return nil, fmt.Errorf("upload: %w", err)
		}
		dm := &waE2E.DocumentMessage{
			Mimetype: proto.String(mt), URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: proto.Uint64(up.FileLength), FileName: proto.String(name), Title: proto.String(name),
			ContextInfo: ci,
		}
		if caption != "" {
			dm.Caption = proto.String(caption)
		}
		msg = &waE2E.Message{DocumentMessage: dm}
		m = ipc.Message{Kind: "document", Text: tag("documento: "+name, caption)}
	}
	m.Quote = quote
	return d.sendAndStore(ctx, jid, msg, m, func(id string) *ipc.Media {
		// Cópia no cache: abre sem baixar de novo.
		base, err := mediaBase(id)
		if err != nil {
			return nil
		}
		md := &ipc.Media{Mime: mt, Path: base + extFor(mt, name)}
		if os.WriteFile(md.Path, data, 0o600) != nil {
			return nil
		}
		return md
	})
}

// fileMime: pela extensão; sem extensão conhecida, pelo conteúdo.
func fileMime(path string) string {
	if mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); mt != "" {
		mt, _, _ = strings.Cut(mt, ";")
		return mt
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	mt, _, _ := strings.Cut(http.DetectContentType(buf[:n]), ";")
	return mt
}

var discovererDur = regexp.MustCompile(`Duration: (\d+):(\d+):(\d+)`)

// mediaSeconds pergunta a duração ao gst-discoverer; 0 se não souber.
func mediaSeconds(ctx context.Context, path string) int {
	out, err := exec.CommandContext(ctx, "gst-discoverer-1.0", path).Output()
	if err != nil {
		return 0
	}
	m := discovererDur.FindStringSubmatch(string(out))
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	s, _ := strconv.Atoi(m[3])
	return h*3600 + mi*60 + s
}
