package tui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	_ "golang.org/x/image/webp"

	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// pasteClipboard lê a área de transferência do Wayland (wl-paste). Se for
// imagem (um print do grim/flameshot, por exemplo), vira anexo com prévia;
// se for texto, entra no campo como um paste comum.
//
// No kitty, ctrl+v chega ao programa como tecla (o paste do kitty é
// ctrl+shift+v), então dá para tratar aqui.
func pasteClipboard() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("wl-paste", "--list-types").Output()
		if err != nil {
			return errMsg{fmt.Errorf("área de transferência vazia ou wl-paste ausente")}
		}
		var imgType string
		for _, t := range strings.Fields(string(out)) {
			if strings.HasPrefix(t, "image/") && (imgType == "" || t == "image/png") {
				imgType = t
			}
		}
		if imgType == "" {
			text, err := exec.Command("wl-paste", "--no-newline").Output()
			if err != nil {
				return errMsg{err}
			}
			return pasteTextMsg(text)
		}
		data, err := exec.Command("wl-paste", "--no-newline", "--type", imgType).Output()
		if err != nil {
			return errMsg{err}
		}
		a, err := saveAttachment(data, imgType)
		if err != nil {
			return errMsg{fmt.Errorf("imagem colada: %w", err)}
		}
		return attachMsg(a)
	}
}

// saveAttachment grava o original e uma prévia PNG (o kitty só lê PNG do
// arquivo) em ~/.cache/zapterm/paste/.
func saveAttachment(data []byte, mimeType string) (attachment, error) {
	base, err := paths.CacheDir()
	if err != nil {
		return attachment{}, err
	}
	dir := filepath.Join(base, "paste")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return attachment{}, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return attachment{}, err
	}
	stamp := time.Now().Format("20060102-150405.000")
	ext := "." + strings.TrimPrefix(mimeType, "image/")
	a := attachment{
		path:    filepath.Join(dir, stamp+ext),
		preview: filepath.Join(dir, stamp+".preview.png"),
		w:       src.Bounds().Dx(),
		h:       src.Bounds().Dy(),
	}
	if err := os.WriteFile(a.path, data, 0o600); err != nil {
		return attachment{}, err
	}
	f, err := os.OpenFile(a.preview, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return attachment{}, err
	}
	defer f.Close()
	return a, png.Encode(f, src)
}
