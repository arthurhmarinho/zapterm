package doctor

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/arthurhmarinho/zapterm/internal/buildinfo"
	"github.com/arthurhmarinho/zapterm/internal/config"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// tudo: um sistema completo; cada teste tira o que quer.
func tudo() Env {
	return Env{
		Cfg: config.Config{Interface: config.Interface{Images: true}},
		Getenv: func(k string) string {
			if k == "XDG_RUNTIME_DIR" {
				return "/run/user/1000"
			}
			return ""
		},
		LookPath:   func(n string) (string, error) { return "/usr/bin/" + n, nil },
		GstElement: func(string) bool { return true },
		DBusName:   func(string) (bool, bool, error) { return true, false, nil },
		Graphics:   func() bool { return true },
		Terminal:   func() ([]string, string, error) { return []string{"kitty", "--class", "zapterm"}, "detectado", nil },
		Whisper:    func() (string, string, error) { return "/usr/bin/whisper-cli", "/m/ggml-base.bin", nil },
		Daemon: func() (ipc.Hello, bool, error) {
			return ipc.Hello{Version: buildinfo.V(), Proto: ipc.ProtoVersion, PID: 7}, true, nil
		},
	}
}

func contar(ls []line) (okN, faltaN, dicaN int) {
	for _, l := range ls {
		switch l.st {
		case ok:
			okN++
		case falta:
			faltaN++
		case dica:
			dicaN++
		}
	}
	return
}

func TestSistemaCompletoNaoFalta(t *testing.T) {
	ls := Check(tudo())
	if _, f, _ := contar(ls); f != 0 {
		for _, l := range ls {
			if l.st == falta {
				t.Errorf("faltou algo num sistema completo: %+v", l)
			}
		}
	}
}

func TestFaltaTrazOPacoteDasTresDistros(t *testing.T) {
	e := tudo()
	e.LookPath = func(n string) (string, error) { return "", errors.New("não achei") }
	e.GstElement = func(string) bool { return false }
	e.DBusName = func(string) (bool, bool, error) { return false, false, nil }
	e.Graphics = func() bool { return false }
	e.Terminal = func() ([]string, string, error) { return nil, "", errors.New("nenhum terminal") }
	e.Daemon = func() (ipc.Hello, bool, error) { return ipc.Hello{}, false, nil }
	ls := Check(e)
	if _, f, _ := contar(ls); f < 10 {
		t.Fatalf("esperava muitas faltas, vieram %d", f)
	}
	for _, l := range ls {
		if l.st != falta || l.what == "XDG_RUNTIME_DIR" {
			continue
		}
		if l.hint == "" {
			t.Errorf("%s: falta sem dica", l.what)
		}
	}
	// Quem tem pacote: as três distros aparecem.
	for _, l := range ls {
		if strings.Contains(l.hint, "Arch:") && (!strings.Contains(l.hint, "Debian/Ubuntu:") || !strings.Contains(l.hint, "Fedora:")) {
			t.Errorf("%s: dica sem as três distros: %s", l.what, l.hint)
		}
	}
}

func TestElementoFaltandoApareceNoGrupo(t *testing.T) {
	e := tudo()
	e.GstElement = func(n string) bool { return n != "webpenc" && n != "oggmux" }
	var webp, voz *line
	ls := Check(e)
	for i := range ls {
		switch ls[i].what {
		case "GStreamer: criar figurinhas (webp)":
			webp = &ls[i]
		case "GStreamer: voz (Opus/Ogg)":
			voz = &ls[i]
		}
	}
	if webp == nil || webp.st != falta || !strings.Contains(webp.detail, "webpenc") || !strings.Contains(webp.hint, "gst-plugins-bad") {
		t.Errorf("webp: %+v", webp)
	}
	if voz == nil || voz.st != falta || voz.detail != "faltam: oggmux" {
		t.Errorf("voz: %+v", voz)
	}
}

func TestDaemon(t *testing.T) {
	casos := []struct {
		nome    string
		h       ipc.Hello
		running bool
		err     error
		st      state
		hint    string
	}{
		{"parado", ipc.Hello{}, false, nil, dica, ""},
		{"mesma versão", ipc.Hello{Version: buildinfo.V(), Proto: ipc.ProtoVersion, PID: 1}, true, nil, ok, ""},
		{"outra versão", ipc.Hello{Version: "v9.9.9", Proto: ipc.ProtoVersion}, true, nil, falta, "zapterm restart"},
		{"anterior ao hello", ipc.Hello{}, true, errors.New(`operação desconhecida: "hello"`), falta, "zapterm restart"},
	}
	for _, c := range casos {
		e := tudo()
		e.Daemon = func() (ipc.Hello, bool, error) { return c.h, c.running, c.err }
		l := daemonLine(e)
		if l.st != c.st || (c.hint != "" && l.hint != c.hint) {
			t.Errorf("%s: %+v", c.nome, l)
		}
	}
}

func TestDBusAtivavelContaComoOk(t *testing.T) {
	e := tudo()
	e.DBusName = func(string) (bool, bool, error) { return false, true, nil }
	if l := dbusLine(e, "x", "n", "porque", "dica"); l.st != ok {
		t.Errorf("ativável deveria ser ok: %+v", l)
	}
	e.DBusName = func(string) (bool, bool, error) { return false, false, errors.New("sem sessão") }
	if l := dbusLine(e, "x", "n", "porque", "dica"); l.st != falta {
		t.Errorf("sem barramento deveria faltar: %+v", l)
	}
}

func TestImagensDesligadasNoConfig(t *testing.T) {
	e := tudo()
	e.Cfg.Interface.Images = false
	e.Graphics = func() bool { return false }
	if ls := Check(e); ls[0].st != dica || !strings.Contains(ls[0].detail, "desligadas") {
		t.Errorf("imagens desligadas: %+v", ls[0])
	}
}

func TestRunEscreveSemCorQuandoPedido(t *testing.T) {
	var b bytes.Buffer
	Run(&b, tudo())
	if strings.Contains(b.String(), "\x1b[") || !strings.Contains(b.String(), "ok") {
		t.Errorf("saída:\n%s", b.String())
	}
}
