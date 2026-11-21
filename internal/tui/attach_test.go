package tui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/demo"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

func TestArrastarArquivo(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.pdf"), filepath.Join(dir, "foto com espaço.png")
	os.WriteFile(a, []byte("%PDF"), 0o600)
	f, _ := os.Create(b)
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	f.Close()
	if got := droppedFiles(a + "\nfile://" + strings.ReplaceAll(b, " ", "%20") + "\n"); len(got) != 2 || got[1] != b {
		t.Fatalf("arrastados: %v", got)
	}
	if got := droppedFiles("'" + b + "' " + a); len(got) != 2 || got[0] != b {
		t.Fatalf("na mesma linha, com aspas: %v", got)
	}
	if got := droppedFiles(strings.ReplaceAll(b, " ", `\ `)); len(got) != 1 || got[0] != b {
		t.Fatalf("com barra invertida: %v", got)
	}
	for _, txt := range []string{"olha isso", a + " e mais", "/nao/existe.pdf", dir} {
		if got := droppedFiles(txt); got != nil {
			t.Errorf("%q virou anexo: %v", txt, got)
		}
	}
}

func TestVariosAnexosEOpcoes(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, n := range []string{"1.png", "2.png"} {
		p := filepath.Join(dir, n)
		f, _ := os.Create(p)
		png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8)))
		f.Close()
		paths = append(paths, p)
	}
	sock := filepath.Join(t.TempDir(), "demo.sock")
	srv, err := demo.Serve(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli, err := ipc.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	m := newModel(cli)
	m.imgs.enabled = false
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	runCmd(m, m.open(m.chatFor("5511900000001@s.whatsapp.net", "Marina")))

	// Um arquivo: figurinha e visualização única se excluem.
	m.setAttachments(paths[:1])
	m.keyAttach("alt+t")
	m.keyAttach("alt+o")
	if m.attach.sticker || !m.attach.once {
		t.Fatalf("opções: %+v", *m.attach)
	}
	m.attach = nil

	// Dois: só HD vale, a legenda vai no primeiro.
	m.Update(tea.PasteMsg{Content: strings.Join(paths, "\n")})
	if m.attach == nil || len(m.attach.more) != 1 {
		t.Fatal("colar dois caminhos não anexou os dois")
	}
	if m.keyAttach("alt+t"); m.attach.sticker {
		t.Error("figurinha com dois arquivos")
	}
	m.keyAttach("alt+q")
	runCmd(m, m.sendAttachments("legenda"))
	got := demo.LastSent(srv)
	if got.Op != ipc.OpSendAlbum || strings.Join(got.Paths, ",") != strings.Join(paths, ",") || got.Text != "legenda" ||
		strings.Join(got.Options, ",") != "hd" {
		t.Fatalf("álbum: %+v", got)
	}

	// Com um PDF no meio não é álbum: vão um a um, a legenda no primeiro.
	pdf := filepath.Join(dir, "x.pdf")
	os.WriteFile(pdf, []byte("%PDF"), 0o600)
	m.setAttachments([]string{paths[0], pdf})
	runCmd(m, m.sendAttachments("oi"))
	if got := demo.LastSent(srv); got.Op != ipc.OpSendFile || got.Path != pdf || got.Text != "" {
		t.Fatalf("sequência: %+v", got)
	}
}
