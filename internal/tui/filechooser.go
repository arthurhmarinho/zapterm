package tui

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/godbus/dbus/v5"

	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Seletor de arquivos nativo pelo xdg-desktop-portal (FileChooser): abre a
// mesma janela de "Abrir arquivo" dos apps GTK, sem depender de zenity.
// O portal responde de forma assíncrona: a chamada devolve o caminho de um
// objeto Request, e o resultado chega no sinal Response dele.
// https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.FileChooser.html

type chosenMsg string

// chooseFile abre o seletor; media = só fotos e vídeos (pelo tipo).
func chooseFile(media bool) tea.Cmd {
	return func() tea.Msg {
		title := "Enviar arquivo pelo zapterm"
		if media {
			title = "Enviar foto ou vídeo pelo zapterm"
		}
		path, err := portalOpenFile(title, media)
		switch {
		case err != nil:
			return errMsg{fmt.Errorf("seletor de arquivos: %w", err)}
		case path == "":
			return nil // cancelou
		}
		return chosenMsg(path)
	}
}

// portalFilter é o filtro do portal: nome e regras (0 = glob, 1 = tipo).
type portalFilter struct {
	Name  string
	Rules []portalRule
}

type portalRule struct {
	Kind    uint32
	Pattern string
}

func portalOpenFile(title string, media bool) (string, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return "", err
	}
	tok := make([]byte, 8)
	rand.Read(tok)
	token := "zapterm_" + hex.EncodeToString(tok)
	// O caminho do Request é previsível: assinamos o sinal antes de chamar,
	// para não perder uma resposta rápida.
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	reqPath := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(reqPath),
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	); err != nil {
		return "", err
	}
	defer conn.RemoveMatchSignal(dbus.WithMatchObjectPath(reqPath),
		dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"))
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
	opts := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"multiple":     dbus.MakeVariant(false),
	}
	if media {
		f := portalFilter{"Fotos e vídeos", []portalRule{{1, "image/*"}, {1, "video/*"}}}
		opts["filters"] = dbus.MakeVariant([]portalFilter{f, {"Todos os arquivos", []portalRule{{0, "*"}}}})
		opts["current_filter"] = dbus.MakeVariant(f)
	}
	if err := obj.Call("org.freedesktop.portal.FileChooser.OpenFile", 0, "", title, opts).Err; err != nil {
		return "", err
	}

	timeout := time.After(15 * time.Minute)
	for {
		select {
		case sig := <-signals:
			if sig.Path != reqPath || len(sig.Body) < 2 {
				continue
			}
			if code, _ := sig.Body[0].(uint32); code != 0 {
				return "", nil // 1 = cancelado, 2 = outro motivo
			}
			results, _ := sig.Body[1].(map[string]dbus.Variant)
			uris, _ := results["uris"].Value().([]string)
			if len(uris) == 0 {
				return "", nil
			}
			u, err := url.Parse(uris[0])
			if err != nil || u.Scheme != "file" {
				return "", fmt.Errorf("resposta inesperada do portal: %q", uris[0])
			}
			return u.Path, nil
		case <-timeout:
			return "", errors.New("o seletor ficou aberto tempo demais")
		}
	}
}

var pickImageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}

// fileAttachment prepara um arquivo escolhido para o painel de envio: foto
// ganha prévia (PNG para o kitty); o resto mostra nome e tamanho.
func fileAttachment(path string) (attachment, error) {
	st, err := os.Stat(path)
	if err != nil {
		return attachment{}, err
	}
	a := attachment{path: path, name: filepath.Base(path), size: st.Size()}
	if !pickImageExts[strings.ToLower(filepath.Ext(path))] {
		return a, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return a, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return a, nil // vai como arquivo mesmo
	}
	base, err := paths.CacheDir()
	if err != nil {
		return a, nil
	}
	dir := filepath.Join(base, "paste")
	os.MkdirAll(dir, 0o700)
	prev := filepath.Join(dir, time.Now().Format("20060102-150405.000")+".preview.png")
	var buf bytes.Buffer
	if png.Encode(&buf, src) != nil || os.WriteFile(prev, buf.Bytes(), 0o600) != nil {
		return a, nil
	}
	a.image, a.preview = true, prev
	a.w, a.h = src.Bounds().Dx(), src.Bounds().Dy()
	return a, nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
