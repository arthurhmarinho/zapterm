package tui

import (
	"bytes"
	"errors"
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

// clipTool é a ferramenta de área de transferência da sessão: wl-clipboard no
// Wayland, xclip ou xsel no X11. Cada campo é um comando pronto (argv).
type clipTool struct {
	name  string
	copy  []string                   // lê o texto do stdin
	types []string                   // lista os tipos disponíveis (nil: só texto)
	text  []string                   // escreve o texto no stdout
	image func(mime string) []string // idem, para a imagem (nil: sem imagem)
}

// pickClipboard escolhe a ferramenta: com WAYLAND_DISPLAY, wl-copy/wl-paste;
// senão (ou se faltarem) xclip; senão xsel, que só faz texto. getenv e
// lookPath são parâmetros para o teste não depender da sessão de quem roda.
func pickClipboard(getenv func(string) string, lookPath func(string) (string, error)) (clipTool, error) {
	has := func(n string) bool { _, err := lookPath(n); return err == nil }
	if getenv("WAYLAND_DISPLAY") != "" && has("wl-copy") && has("wl-paste") {
		return clipTool{
			name:  "wl-clipboard",
			copy:  []string{"wl-copy"},
			types: []string{"wl-paste", "--list-types"},
			text:  []string{"wl-paste", "--no-newline"},
			image: func(mime string) []string { return []string{"wl-paste", "--no-newline", "--type", mime} },
		}, nil
	}
	if has("xclip") {
		return clipTool{
			name:  "xclip",
			copy:  []string{"xclip", "-selection", "clipboard"},
			types: []string{"xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"},
			text:  []string{"xclip", "-selection", "clipboard", "-o"},
			image: func(mime string) []string { return []string{"xclip", "-selection", "clipboard", "-t", mime, "-o"} },
		}, nil
	}
	if has("xsel") {
		return clipTool{name: "xsel", copy: []string{"xsel", "-b", "-i"}, text: []string{"xsel", "-b", "-o"}}, nil
	}
	return clipTool{}, errors.New("sem área de transferência: instale wl-clipboard (Wayland) ou xclip (X11)")
}

func systemClipboard() (clipTool, error) { return pickClipboard(os.Getenv, exec.LookPath) }

// copyText põe o texto na área de transferência.
func copyText(text string) error {
	t, err := systemClipboard()
	if err != nil {
		return err
	}
	cmd := exec.Command(t.copy[0], t.copy[1:]...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", t.copy[0], err)
	}
	return nil
}

// readClipboard lê a área de transferência: a imagem (e o tipo dela) se
// houver, senão o texto.
func readClipboard() (text, img []byte, imgType string, err error) {
	t, err := systemClipboard()
	if err != nil {
		return nil, nil, "", err
	}
	if t.types != nil {
		out, err := exec.Command(t.types[0], t.types[1:]...).Output()
		if err != nil {
			return nil, nil, "", fmt.Errorf("área de transferência vazia (%s)", t.types[0])
		}
		imgType = pickImageType(strings.Fields(string(out)))
	}
	if imgType == "" {
		text, err = exec.Command(t.text[0], t.text[1:]...).Output()
		return text, nil, "", err
	}
	argv := t.image(imgType)
	img, err = exec.Command(argv[0], argv[1:]...).Output()
	return nil, img, imgType, err
}

// pickImageType: o tipo image/* da lista, preferindo PNG.
func pickImageType(types []string) string {
	var imgType string
	for _, t := range types {
		if strings.HasPrefix(t, "image/") && (imgType == "" || t == "image/png") {
			imgType = t
		}
	}
	return imgType
}

// pasteClipboard lê a área de transferência. Se for imagem (um print do
// grim, flameshot…), vira anexo com prévia; se for texto, entra no campo
// como um paste comum.
//
// No kitty, ctrl+v chega ao programa como tecla (o paste do kitty é
// ctrl+shift+v), então dá para tratar aqui.
func pasteClipboard() tea.Cmd {
	return func() tea.Msg {
		text, data, imgType, err := readClipboard()
		if err != nil {
			return errMsg{err}
		}
		if imgType == "" {
			return pasteTextMsg(text)
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
		image:   true,
		name:    "print colado",
		size:    int64(len(data)),
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
