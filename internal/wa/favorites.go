package wa

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
	"github.com/arthurhmarinho/zapterm/internal/store"
)

// Figurinhas favoritas: o celular sincroniza a sua biblioteca pelo app
// state (índice "favoriteSticker", com URL, chave e tamanho de cada uma).
// O whatsmeow só emite eventos de app state para mudanças depois do
// pareamento; a carga inicial, onde estão as favoritas antigas, é pedida
// de novo em refreshAppState com EmitAppStateEventsOnFullSync ligado.

var appStateOnce sync.Once

// refreshAppState pede de novo a carga completa dos patches de
// configurações, desta vez emitindo eventos, uma vez por execução do
// daemon. Reaplicar fixados/arquivados/mudo é idempotente.
func (d *Daemon) refreshAppState() {
	appStateOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			d.cli.EmitAppStateEventsOnFullSync = true
			defer func() { d.cli.EmitAppStateEventsOnFullSync = false }()
			for _, name := range []appstate.WAPatchName{appstate.WAPatchRegularLow, appstate.WAPatchRegularHigh, appstate.WAPatchRegular} {
				if err := d.cli.FetchAppState(ctx, name, true, false); err != nil {
					d.log.Warn("app state", "patch", name, "err", err)
				}
			}
			favs, _ := d.st.Favorites()
			d.log.Info("figurinhas favoritas sincronizadas", "total", len(favs))
		}()
	})
}

func (d *Daemon) onAppState(e *events.AppState) {
	if len(e.Index) < 2 || e.Index[0] != appstate.IndexFavoriteSticker {
		return
	}
	key := e.Index[1]
	sa := e.GetStickerAction()
	if sa == nil || !sa.GetIsFavorite() {
		if err := d.st.RemoveFavorite(key); err != nil {
			d.log.Warn("removendo favorita", "err", err)
		}
		return
	}
	f := store.FavoriteSticker{
		Key: key, DirectPath: sa.GetDirectPath(), MediaKey: sa.GetMediaKey(), FileEncSHA: sa.GetFileEncSHA256(),
		Mimetype: sa.GetMimetype(), Width: int(sa.GetWidth()), Height: int(sa.GetHeight()),
		FileLength: int64(sa.GetFileLength()), Lottie: sa.GetIsLottie(), TS: e.GetTimestamp(),
	}
	if f.Mimetype == "" {
		f.Mimetype = "image/webp"
	}
	// O índice costuma ser o SHA-256 do arquivo em base64; se for, serve de
	// conferência extra no download.
	if h, err := base64.StdEncoding.DecodeString(key); err == nil && len(h) == 32 {
		f.FileSHA = h
	}
	if f.DirectPath == "" || len(f.MediaKey) == 0 || f.Lottie {
		return // Lottie é animação vetorial; o zapterm não desenha
	}
	if err := d.st.PutFavorite(f); err != nil {
		d.log.Warn("gravando favorita", "err", err)
	}
}

// favoriteID dá um nome de arquivo estável e seguro para a favorita.
func favoriteID(key string) string {
	h := sha256.Sum256([]byte(key))
	return "fav_" + hex.EncodeToString(h[:12])
}

// downloadFavorite baixa (ou pega do cache) uma favorita e gera a prévia,
// animada se for o caso. Devolve também os bytes originais (para reenviar).
func (d *Daemon) downloadFavorite(ctx context.Context, key string) (*ipc.Media, []byte, error) {
	id := favoriteID(key)
	if md := cachedMedia(id); md != nil {
		data, err := os.ReadFile(md.Path)
		if err == nil && md.Preview == "" {
			d.animPreview(id, data, md)
		}
		return md, data, err
	}
	f, err := d.st.Favorite(key)
	if err != nil {
		return nil, nil, err
	}
	data := localSticker(f.FileSHA) // passou numa conversa: nem precisa de rede
	if data == nil {
		data, err = d.cli.DownloadMediaWithPath(ctx, f.DirectPath, f.FileEncSHA, f.FileSHA, f.MediaKey,
			whatsmeow.MediaImage, "image", true)
		// Sem o hash do arquivo, o whatsmeow reclama mesmo com o MAC conferido
		// (a integridade já está garantida por ele); o conteúdo vale.
		if err != nil && !(errors.Is(err, whatsmeow.ErrInvalidMediaSHA256) && f.FileSHA == nil && len(data) > 0) {
			return nil, nil, fmt.Errorf("baixando favorita: %w", err)
		}
	}
	base, err := mediaBase(id)
	if err != nil {
		return nil, nil, err
	}
	md := &ipc.Media{Mime: f.Mimetype, Path: base + ".webp"}
	if err := os.WriteFile(md.Path, data, 0o600); err != nil {
		return nil, nil, err
	}
	md.Preview = base + ".preview.png"
	if md.Width, md.Height, err = writePreview(data, md.Preview); err != nil {
		md.Preview = ""
		os.Remove(base + ".preview.png")
		d.animPreview(id, data, md)
	}
	return md, data, nil
}

// favoriteStickers monta uma página da aba "Favoritas" (são centenas, e
// cada uma é baixada na hora): percorre a biblioteca a partir de offset,
// pula as expiradas e segue até juntar limit figurinhas. As que o servidor
// recusar (403/404/410: o arquivo expirou) ficam marcadas para não tentar
// de novo.
func (d *Daemon) favoriteStickers(ctx context.Context, offset, limit int) (ipc.StickerPage, error) {
	page := ipc.StickerPage{Stickers: []ipc.Sticker{}, Next: -1}
	favs, err := d.st.Favorites()
	if err != nil {
		return page, err
	}
	if limit <= 0 {
		limit = 28
	}
	d.indexLocalStickers()
	var goneMu sync.Mutex
	gone := 0
	i := offset
	for i < len(favs) && len(page.Stickers) < limit {
		// Lote de até 4 downloads em paralelo, na ordem da biblioteca.
		var batch []store.FavoriteSticker
		for i < len(favs) && len(batch) < 4 {
			if !favs[i].Gone {
				batch = append(batch, favs[i])
			}
			i++
		}
		got := make([]*ipc.Sticker, len(batch))
		var wg sync.WaitGroup
		for j, f := range batch {
			wg.Add(1)
			go func() {
				defer wg.Done()
				md, _, err := d.downloadFavorite(ctx, f.Key)
				switch {
				case errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403),
					errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404),
					errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410):
					_ = d.st.MarkFavoriteGone(f.Key)
					goneMu.Lock()
					gone++
					goneMu.Unlock()
				case err != nil:
					d.log.Warn("favorita", "err", err)
				case md.Preview != "":
					got[j] = &ipc.Sticker{Chat: favoriteChat, ID: f.Key, Preview: md.Preview, Width: md.Width,
						Height: md.Height, Frames: md.Frames, Delays: md.Delays}
				}
			}()
		}
		wg.Wait()
		for _, st := range got {
			if st != nil {
				page.Stickers = append(page.Stickers, *st)
			}
		}
	}
	if i < len(favs) {
		page.Next = i
	}
	if gone > 0 {
		d.log.Info("favoritas expiradas no servidor", "novas", gone)
	}
	return page, nil
}

// ---------- cópias locais ----------

// As favoritas antigas costumam ter expirado no servidor, mas a figurinha
// pode estar no cache por ter passado numa conversa. O índice da favorita
// é o SHA-256 do arquivo: indexLocalStickers mapeia hash → arquivo.
var (
	localMu     sync.Mutex
	localByHash map[string]string
	localAt     time.Time
)

func (d *Daemon) indexLocalStickers() {
	localMu.Lock()
	defer localMu.Unlock()
	if time.Since(localAt) < time.Minute {
		return
	}
	dir, err := paths.MediaDir()
	if err != nil {
		return
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.webp"))
	idx := make(map[string]string, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		h := sha256.Sum256(data)
		idx[string(h[:])] = f
	}
	localByHash, localAt = idx, time.Now()
}

func localSticker(sha []byte) []byte {
	if len(sha) != 32 {
		return nil
	}
	localMu.Lock()
	path := localByHash[string(sha)]
	localMu.Unlock()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

// favoriteChat marca, em ipc.Sticker.Chat, que a figurinha é da biblioteca
// (o ID é a chave dela) e não de uma mensagem.
const favoriteChat = "favorite"
