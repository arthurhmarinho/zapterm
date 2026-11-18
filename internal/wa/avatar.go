package wa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"golang.org/x/image/draw"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Fotos de perfil: a miniatura (preview) do WhatsApp, recortada em
// quadrado e guardada em ~/.cache/zapterm/avatars/. Revalida a cada
// avatarTTL; foto escondida ou inexistente também fica em cache, para não
// perguntar de novo a cada abertura.
const (
	avatarTTL  = 3 * 24 * time.Hour
	avatarPx   = 96
	avatarRate = 300 * time.Millisecond // um pedido ao servidor por vez, espaçados
)

type avatarMeta struct {
	PicID   string `json:"pic_id,omitempty"`
	File    string `json:"file,omitempty"`
	None    bool   `json:"none,omitempty"`
	Checked int64  `json:"checked"`
}

type avatars struct {
	mu       sync.Mutex
	inflight map[string]chan struct{}
	limiter  *time.Ticker
	http     *http.Client
}

func newAvatars() *avatars {
	return &avatars{
		inflight: map[string]chan struct{}{},
		limiter:  time.NewTicker(avatarRate),
		http:     &http.Client{Timeout: 20 * time.Second},
	}
}

func avatarMetaPath(jid string) (string, error) {
	dir, err := paths.AvatarDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, safeName.ReplaceAllString(jid, "_")+".json"), nil
}

func readAvatarMeta(jid string) (avatarMeta, string) {
	var meta avatarMeta
	p, err := avatarMetaPath(jid)
	if err != nil {
		return meta, ""
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &meta)
	}
	return meta, p
}

func (meta avatarMeta) media() *ipc.Media {
	if meta.None || meta.File == "" || !strings.HasSuffix(meta.File, ".round.png") {
		return nil
	}
	if _, err := os.Stat(meta.File); err != nil {
		return nil
	}
	return &ipc.Media{Preview: meta.File, Path: meta.File, Width: avatarPx, Height: avatarPx}
}

// avatar devolve a foto de perfil de um chat ou pessoa (nil = sem foto).
func (d *Daemon) avatar(ctx context.Context, chat string) (*ipc.Media, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	if jid.Server == types.NewsletterServer || jid.Server == types.BroadcastServer {
		return nil, nil // canal fica com as iniciais
	}
	key := jid.ToNonAD().String()

	meta, _ := readAvatarMeta(key)
	// Arquivo do formato antigo (quadrado) conta como vencido.
	fresh := meta.None || strings.HasSuffix(meta.File, ".round.png")
	if fresh && time.Since(time.Unix(meta.Checked, 0)) < avatarTTL {
		return meta.media(), nil
	}
	if !fresh {
		meta.PicID = "" // força baixar de novo
	}

	// Uma busca por JID de cada vez; quem chega depois espera a primeira.
	av := d.avatars
	av.mu.Lock()
	if ch, ok := av.inflight[key]; ok {
		av.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		meta, _ := readAvatarMeta(key)
		return meta.media(), nil
	}
	ch := make(chan struct{})
	av.inflight[key] = ch
	av.mu.Unlock()
	defer func() {
		av.mu.Lock()
		delete(av.inflight, key)
		av.mu.Unlock()
		close(ch)
	}()

	select {
	case <-av.limiter.C:
	case <-ctx.Done():
		return meta.media(), nil // sem tempo: devolve o que houver em cache
	}
	newMeta, err := d.fetchAvatar(ctx, jid, meta)
	if err != nil {
		d.log.Warn("foto de perfil", "jid", key, "err", err)
		return meta.media(), nil
	}
	return newMeta.media(), nil
}

func (d *Daemon) fetchAvatar(ctx context.Context, jid types.JID, old avatarMeta) (avatarMeta, error) {
	key := jid.ToNonAD().String()
	meta := avatarMeta{Checked: time.Now().Unix()}
	_, metaPath := readAvatarMeta(key)

	info, err := d.cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{
		Preview:    true,
		ExistingID: old.PicID,
	})
	switch {
	case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
		meta.None = true
		if old.File != "" {
			os.Remove(old.File)
		}
		return meta, writeAvatarMeta(metaPath, meta)
	case err != nil:
		return old, err
	case info == nil:
		// Mesma foto de antes (ExistingID bateu): só renova a validade.
		old.Checked = meta.Checked
		return old, writeAvatarMeta(metaPath, old)
	}

	dir, err := paths.AvatarDir()
	if err != nil {
		return old, err
	}
	// O ID da foto vai no nome: foto nova = arquivo novo, e o kitty não
	// reaproveita a imagem antiga que já tem na memória.
	file := filepath.Join(dir, fmt.Sprintf("%s_%s.round.png",
		safeName.ReplaceAllString(key, "_"), safeName.ReplaceAllString(info.ID, "_")))
	if err := d.downloadAvatar(ctx, info.URL, file); err != nil {
		return old, err
	}
	if old.File != "" && old.File != file {
		os.Remove(old.File)
	}
	meta.PicID, meta.File = info.ID, file
	return meta, writeAvatarMeta(metaPath, meta)
}

func (d *Daemon) downloadAvatar(ctx context.Context, url, file string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := d.avatars.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	// Recorte quadrado central, reduzido para avatarPx.
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	crop := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(side, side))
	sq := image.NewRGBA(image.Rect(0, 0, avatarPx, avatarPx))
	draw.CatmullRom.Scale(sq, sq.Bounds(), src, crop, draw.Src, nil)
	// Recorte redondo como no app: cantos transparentes deixam aparecer o
	// fundo da TUI.
	dst := image.NewRGBA(sq.Bounds())
	draw.DrawMask(dst, dst.Bounds(), sq, image.Point{}, roundMask(avatarPx), image.Point{}, draw.Over)

	tmp := file + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := png.Encode(f, dst); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func writeAvatarMeta(path string, meta avatarMeta) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// roundMask é um círculo opaco com borda suavizada em 1px.
func roundMask(px int) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, px, px))
	r := float64(px) / 2
	inner := (r - 1) * (r - 1)
	for y := 0; y < px; y++ {
		for x := 0; x < px; x++ {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			d2 := dx*dx + dy*dy
			switch {
			case d2 <= inner:
				m.SetAlpha(x, y, color.Alpha{255})
			case d2 < r*r:
				m.SetAlpha(x, y, color.Alpha{uint8(255 * (r*r - d2) / (r*r - inner))})
			}
		}
	}
	return m
}
