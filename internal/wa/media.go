package wa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Largura máxima da prévia em pixels: a TUI mostra no máximo ~40 colunas,
// e o kitty reduz o resto. Prévia menor = menos disco e transmissão mais
// rápida para o terminal.
const previewMaxPx = 720

// Limite do cache de mídia; ao subir, o daemon apaga os mais antigos.
const mediaCacheMax = 500 << 20

var safeName = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func mediaBase(id string) (string, error) {
	dir, err := paths.MediaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, safeName.ReplaceAllString(id, "_")), nil
}

// cachedMedia devolve a mídia já baixada de uma mensagem, sem rede.
func cachedMedia(id string) *ipc.Media {
	base, err := mediaBase(id)
	if err != nil {
		return nil
	}
	matches, _ := filepath.Glob(base + ".*")
	if len(matches) == 0 {
		return nil
	}
	md := &ipc.Media{}
	for _, p := range matches {
		switch {
		case strings.HasSuffix(p, ".preview.png"):
			md.Preview = p
		case strings.HasSuffix(p, ".thumb.png"):
			// miniatura embutida (thumbFor), não é a mídia baixada
		case strings.HasSuffix(p, ".anim"):
			// pasta dos quadros, lida abaixo
		default:
			md.Path = p
		}
	}
	if md.Path == "" {
		return nil
	}
	if md.Preview == "" {
		loadAnim(base, md)
	}
	if md.Preview != "" {
		md.Width, md.Height = pngSize(md.Preview)
	}
	return md
}

// downloadMedia baixa (ou pega do cache) a mídia de uma mensagem. Só
// funciona para mensagens com o protobuf original guardado.
func (d *Daemon) downloadMedia(ctx context.Context, chat, id string) (*ipc.Media, error) {
	if md := cachedMedia(id); md != nil {
		// Figurinha animada baixada antes de existir o decodificador: gera
		// os quadros agora a partir do arquivo em cache.
		if md.Preview == "" && strings.HasSuffix(md.Path, ".webp") {
			if data, err := os.ReadFile(md.Path); err == nil {
				d.animPreview(id, data, md)
			}
		}
		return md, nil
	}
	_, raw, err := d.st.Message(chat, id)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("mídia de mensagem antiga (do pareamento) não pode ser baixada")
	}
	var msg waE2E.Message
	if err := proto.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}

	var dl whatsmeow.DownloadableMessage
	var mimeType, fileName string
	isImage := false
	switch {
	case msg.GetImageMessage() != nil:
		m := msg.GetImageMessage()
		dl, mimeType, isImage = m, m.GetMimetype(), true
	case msg.GetStickerMessage() != nil:
		m := msg.GetStickerMessage()
		dl, mimeType, isImage = m, m.GetMimetype(), true
	case msg.GetVideoMessage() != nil:
		m := msg.GetVideoMessage()
		dl, mimeType = m, m.GetMimetype()
	case msg.GetAudioMessage() != nil:
		m := msg.GetAudioMessage()
		dl, mimeType = m, m.GetMimetype()
	case msg.GetDocumentMessage() != nil:
		m := msg.GetDocumentMessage()
		dl, mimeType, fileName = m, m.GetMimetype(), m.GetFileName()
	default:
		return nil, errors.New("essa mensagem não tem mídia")
	}

	data, err := d.cli.Download(ctx, dl)
	if err != nil {
		return nil, fmt.Errorf("baixando: %w", err)
	}
	base, err := mediaBase(id)
	if err != nil {
		return nil, err
	}
	md := &ipc.Media{Mime: mimeType, Path: base + extFor(mimeType, fileName)}
	if err := os.WriteFile(md.Path, data, 0o600); err != nil {
		return nil, err
	}
	if isImage {
		md.Preview = base + ".preview.png"
		md.Width, md.Height, err = writePreview(data, md.Preview)
		if err != nil {
			md.Preview = ""
			os.Remove(base + ".preview.png")
			if !d.animPreview(id, data, md) {
				d.log.Warn("prévia da imagem", "id", id, "err", err)
			}
		}
	}
	return md, nil
}

type animManifest struct {
	Frames []string `json:"frames"`
	Delays []int    `json:"delays"`
}

// animPreview decodifica um webp animado e grava os quadros em
// <id>.anim/NNN.png com um manifesto de durações. Devolve false se o
// arquivo não for animado (ou não der para ler).
func (d *Daemon) animPreview(id string, data []byte, md *ipc.Media) bool {
	frames, err := decodeAnimatedWebP(data)
	if err != nil {
		return false
	}
	base, err := mediaBase(id)
	if err != nil {
		return false
	}
	dir := base + ".anim"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	var man animManifest
	for i, f := range frames {
		p := filepath.Join(dir, fmt.Sprintf("%03d.png", i))
		out, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return false
		}
		err = png.Encode(out, f.img)
		out.Close()
		if err != nil {
			return false
		}
		man.Frames = append(man.Frames, p)
		man.Delays = append(man.Delays, f.delay)
	}
	b, _ := json.Marshal(man)
	if os.WriteFile(filepath.Join(dir, "frames.json"), b, 0o600) != nil {
		return false
	}
	md.Frames, md.Delays = man.Frames, man.Delays
	md.Preview = man.Frames[0]
	md.Width, md.Height = pngSize(md.Preview)
	return true
}

// loadAnim completa md com os quadros já gravados, se houver.
func loadAnim(base string, md *ipc.Media) {
	b, err := os.ReadFile(filepath.Join(base+".anim", "frames.json"))
	if err != nil {
		return
	}
	var man animManifest
	if json.Unmarshal(b, &man) != nil || len(man.Frames) == 0 || len(man.Delays) != len(man.Frames) {
		return
	}
	// A limpeza do cache apaga arquivos soltos: quadro faltando invalida tudo.
	for _, f := range man.Frames {
		if _, err := os.Stat(f); err != nil {
			return
		}
	}
	md.Frames, md.Delays, md.Preview = man.Frames, man.Delays, man.Frames[0]
}

func extFor(mimeType, fileName string) string {
	if ext := filepath.Ext(fileName); ext != "" && len(ext) <= 6 && !strings.HasSuffix(ext, ".png") {
		return safeExt(ext)
	}
	mt, _, _ := strings.Cut(mimeType, ";")
	switch mt {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "audio/ogg":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	}
	if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
		return safeExt(exts[0])
	}
	return ".bin"
}

// safeExt evita colidir com o sufixo da prévia e com caracteres estranhos.
func safeExt(ext string) string {
	ext = "." + safeName.ReplaceAllString(strings.TrimPrefix(ext, "."), "")
	if ext == "." {
		return ".bin"
	}
	return ext
}

// writePreview decodifica a imagem (jpeg, png, gif, webp), reduz para no
// máximo previewMaxPx de largura e grava PNG, o formato que o kitty lê
// direto do arquivo.
func writePreview(data []byte, path string) (int, int, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > previewMaxPx {
		h = h * previewMaxPx / w
		w = previewMaxPx
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	if err := png.Encode(f, dst); err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func pngSize(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// pruneMediaCache apaga os arquivos mais antigos até o cache caber no
// limite. Roda uma vez ao subir o daemon.
func pruneMediaCache() (removed int, err error) {
	dir, err := paths.MediaDir()
	if err != nil {
		return 0, err
	}
	type file struct {
		path string
		size int64
		mod  int64
	}
	var files []file
	var total int64
	err = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		files = append(files, file{p, info.Size(), info.ModTime().Unix()})
		total += info.Size()
		return nil
	})
	if err != nil || total <= mediaCacheMax {
		return 0, err
	}
	slices.SortFunc(files, func(a, b file) int { return int(a.mod - b.mod) })
	for _, f := range files {
		if total <= mediaCacheMax*8/10 {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			removed++
		}
	}
	return removed, nil
}

// Lado maior da imagem enviada: o WhatsApp recomprime acima disso, e print
// de tela grande vira alguns MB à toa.
const sendMaxPx = 1600

// sendImage envia um arquivo de imagem (o print colado na TUI) como foto,
// com legenda opcional. Converte para JPEG, que é o que o app espera.
func (d *Daemon) sendImage(ctx context.Context, chat, path, caption, replyTo string) (any, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("não é uma imagem: %w", err)
	}
	full, w, h := scaleJPEG(src, sendMaxPx, 85)
	thumb, _, _ := scaleJPEG(src, 72, 60)

	up, err := d.cli.Upload(ctx, full, whatsmeow.MediaImage)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	ci, quote, err := d.replyContext(chat, replyTo)
	if err != nil {
		return nil, err
	}
	im := &waE2E.ImageMessage{
		Mimetype:      proto.String("image/jpeg"),
		URL:           proto.String(up.URL),
		DirectPath:    proto.String(up.DirectPath),
		MediaKey:      up.MediaKey,
		FileEncSHA256: up.FileEncSHA256,
		FileSHA256:    up.FileSHA256,
		FileLength:    proto.Uint64(up.FileLength),
		Width:         proto.Uint32(uint32(w)),
		Height:        proto.Uint32(uint32(h)),
		JPEGThumbnail: thumb,
		ContextInfo:   ci,
	}
	if caption = strings.TrimSpace(caption); caption != "" {
		im.Caption = proto.String(caption)
	}
	m := ipc.Message{Kind: "image", Text: tag("imagem", caption), Quote: quote}
	return d.sendAndStore(ctx, jid, &waE2E.Message{ImageMessage: im}, m, func(id string) *ipc.Media {
		// Já entra no cache com o ID definitivo: aparece no balão na hora.
		base, err := mediaBase(id)
		if err != nil {
			return nil
		}
		md := &ipc.Media{Mime: "image/jpeg", Path: base + ".jpg", Preview: base + ".preview.png"}
		if os.WriteFile(md.Path, full, 0o600) != nil {
			return nil
		}
		if md.Width, md.Height, err = writePreview(full, md.Preview); err != nil {
			md.Preview = ""
		}
		return md
	})
}

func scaleJPEG(src image.Image, maxSide, quality int) ([]byte, int, int) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if s := max(w, h); s > maxSide {
		w, h = w*maxSide/s, h*maxSide/s
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fundo branco: PNG com transparência viraria preto no JPEG.
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality})
	return buf.Bytes(), w, h
}

// recentStickers monta o seletor: figurinhas recentes sem repetir (mesmo
// arquivo = mesmo SHA-256), baixando as que ainda não estão no cache.
func (d *Daemon) recentStickers(ctx context.Context) ([]ipc.Sticker, error) {
	rows, err := d.st.RecentStickers(300)
	if err != nil {
		return nil, err
	}
	type cand struct{ chat, id string }
	seen := map[string]bool{}
	var cands []cand
	for _, r := range rows {
		var msg waE2E.Message
		if proto.Unmarshal(r[2].([]byte), &msg) != nil || msg.GetStickerMessage() == nil {
			continue
		}
		key := string(msg.GetStickerMessage().GetFileSHA256())
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		cands = append(cands, cand{r[0].(string), r[1].(string)})
		if len(cands) == 48 {
			break
		}
	}

	// Baixa em paralelo (4 por vez); quem falhar (mídia expirada) fica de fora.
	out := make([]*ipc.Sticker, len(cands))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, c := range cands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			md, err := d.downloadMedia(ctx, c.chat, c.id)
			if err != nil || md.Preview == "" {
				return
			}
			out[i] = &ipc.Sticker{Chat: c.chat, ID: c.id, Preview: md.Preview, Width: md.Width, Height: md.Height,
				Frames: md.Frames, Delays: md.Delays}
		}()
	}
	wg.Wait()
	list := []ipc.Sticker{}
	for _, st := range out {
		if st != nil {
			list = append(list, *st)
		}
	}
	return list, nil
}

// sendSticker reenvia uma figurinha já vista. Sobe o arquivo de novo em vez
// de reaproveitar URL e chave da mensagem original, que podem ter expirado.
func (d *Daemon) sendSticker(ctx context.Context, chat, srcChat, srcID, replyTo string) (any, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	md, err := d.downloadMedia(ctx, srcChat, srcID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(md.Path)
	if err != nil {
		return nil, err
	}
	_, raw, err := d.st.Message(srcChat, srcID)
	if err != nil {
		return nil, err
	}
	var orig waE2E.Message
	if err := proto.Unmarshal(raw, &orig); err != nil || orig.GetStickerMessage() == nil {
		return nil, errors.New("a mensagem de origem não é uma figurinha")
	}
	o := orig.GetStickerMessage()

	up, err := d.cli.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	ci, quote, err := d.replyContext(chat, replyTo)
	if err != nil {
		return nil, err
	}
	sm := &waE2E.StickerMessage{
		Mimetype:      proto.String("image/webp"),
		URL:           proto.String(up.URL),
		DirectPath:    proto.String(up.DirectPath),
		MediaKey:      up.MediaKey,
		FileEncSHA256: up.FileEncSHA256,
		FileSHA256:    up.FileSHA256,
		FileLength:    proto.Uint64(up.FileLength),
		Width:         proto.Uint32(o.GetWidth()),
		Height:        proto.Uint32(o.GetHeight()),
		IsAnimated:    proto.Bool(o.GetIsAnimated()),
		ContextInfo:   ci,
	}
	m := ipc.Message{Kind: "sticker", Text: "[figurinha]", Quote: quote}
	return d.sendAndStore(ctx, jid, &waE2E.Message{StickerMessage: sm}, m, func(id string) *ipc.Media {
		// Mesma prévia no cache com o ID novo: aparece no balão na hora.
		base, err := mediaBase(id)
		if err != nil {
			return nil
		}
		cp := &ipc.Media{Mime: "image/webp", Path: base + ".webp", Preview: base + ".preview.png", Width: md.Width, Height: md.Height}
		if os.WriteFile(cp.Path, data, 0o600) != nil {
			return nil
		}
		if pv, err := os.ReadFile(md.Preview); err == nil && os.WriteFile(cp.Preview, pv, 0o600) == nil {
			return cp
		}
		cp.Preview = ""
		return cp
	})
}

// thumbDisplayW é a largura "de mentira" da miniatura para a TUI: a
// imagem tem ~100px, mas deve ocupar o balão como a foto de verdade (o
// kitty amplia, borrada, como o app antes de baixar).
const thumbDisplayW = 360

// fillThumbs põe a miniatura embutida nas fotos e vídeos ainda não
// baixados. Gera <id>.thumb.png a partir do protobuf na primeira vez.
func (d *Daemon) fillThumbs(msgs []ipc.Message) {
	for i := range msgs {
		m := &msgs[i]
		if !m.HasRaw || (m.Kind != "image" && m.Kind != "video") {
			continue
		}
		if m.Media != nil && m.Media.Preview != "" {
			continue // já tem a prévia de verdade
		}
		m.Thumb = d.thumbFor(m.Chat, m.ID)
	}
}

func (d *Daemon) thumbFor(chat, id string) *ipc.Media {
	base, err := mediaBase(id)
	if err != nil {
		return nil
	}
	path := base + ".thumb.png"
	if _, err := os.Stat(path); err != nil {
		_, raw, err := d.st.Message(chat, id)
		if err != nil || len(raw) == 0 {
			return nil
		}
		var msg waE2E.Message
		if proto.Unmarshal(raw, &msg) != nil {
			return nil
		}
		jpg := msg.GetImageMessage().GetJPEGThumbnail()
		if v := msg.GetVideoMessage(); v != nil {
			jpg = v.GetJPEGThumbnail()
		}
		if len(jpg) == 0 {
			return nil
		}
		src, _, err := image.Decode(bytes.NewReader(jpg))
		if err != nil {
			return nil
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return nil
		}
		err = png.Encode(f, src)
		f.Close()
		if err != nil {
			os.Remove(path)
			return nil
		}
	}
	w, h := pngSize(path)
	if w == 0 || h == 0 {
		return nil
	}
	return &ipc.Media{Preview: path, Width: thumbDisplayW, Height: thumbDisplayW * h / w}
}
