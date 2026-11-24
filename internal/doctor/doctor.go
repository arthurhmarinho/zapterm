// Package doctor confere o que o zapterm precisa do sistema (programas,
// plugins do GStreamer, serviços do D-Bus) e diz, para cada falta, o pacote
// em cada distro. Só informa: nunca falha.
package doctor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/arthurhmarinho/zapterm/internal/buildinfo"
	"github.com/arthurhmarinho/zapterm/internal/config"
	"github.com/arthurhmarinho/zapterm/internal/daemonctl"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
	"github.com/arthurhmarinho/zapterm/internal/tui"
	"github.com/arthurhmarinho/zapterm/internal/wa"
)

// Env é tudo o que o doctor pergunta ao sistema; os testes trocam por
// respostas fixas.
type Env struct {
	Cfg      config.Config
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// GstElement diz se o elemento existe (gst-inspect-1.0 --exists).
	GstElement func(name string) bool
	// DBusName: o nome tem dono na sessão (running) ou o D-Bus sabe subir quem
	// o atende (activatable).
	DBusName func(name string) (running, activatable bool, err error)
	// Graphics: este terminal desenha imagens.
	Graphics func() bool
	// Terminal: o que o clique na notificação abre.
	Terminal func() (argv []string, source string, err error)
	// Whisper: programa e modelo da transcrição.
	Whisper func() (cmd, model string, err error)
	// Daemon: running = há algo atendendo o socket; h/err é a resposta ao
	// OpHello (err de operação desconhecida: daemon anterior a ela).
	Daemon func() (h ipc.Hello, running bool, err error)
	// Color liga as cores ANSI.
	Color bool
}

// System monta o Env com o sistema de verdade.
func System(cfg config.Config, color bool) Env {
	return Env{
		Cfg:      cfg,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		GstElement: func(name string) bool {
			return exec.Command("gst-inspect-1.0", "--exists", name).Run() == nil
		},
		DBusName: sessionBusName,
		Graphics: tui.GraphicsTerminal,
		Terminal: func() ([]string, string, error) { return wa.TerminalCommand(cfg) },
		Whisper:  func() (string, string, error) { return wa.WhisperSetup(cfg.Transcription) },
		Daemon: func() (ipc.Hello, bool, error) {
			c, err := ipc.Dial(paths.Socket())
			if err != nil {
				return ipc.Hello{}, false, nil
			}
			defer c.Close()
			h, err := c.Hello()
			return h, true, err
		},
		Color: color,
	}
}

func sessionBusName(name string) (running, activatable bool, err error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false, false, err
	}
	defer conn.Close()
	bus := conn.BusObject()
	if err := bus.Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&running); err != nil {
		return false, false, err
	}
	var act []string
	if err := bus.Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&act); err == nil {
		for _, n := range act {
			if n == name {
				activatable = true
			}
		}
	}
	return running, activatable, nil
}

type state int

const (
	ok    state = iota
	falta       // algo que o zapterm usa não está disponível
	dica        // só informação
)

type line struct {
	st     state
	what   string // o que foi conferido
	detail string // o que achou, ou para que serve
	hint   string // o que fazer, se faltou
}

// Pacotes por distro. Vazio = não existe pacote com esse nome lá.
type pkgs struct{ arch, deb, fedora string }

func (p pkgs) String() string {
	return fmt.Sprintf("Arch: %s · Debian/Ubuntu: %s · Fedora: %s", orDash(p.arch), orDash(p.deb), orDash(p.fedora))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Grupos de elementos do GStreamer, cada um com o pacote que os traz.
var gstGroups = []struct {
	what     string
	elements []string
	pkg      pkgs
}{
	{"GStreamer: voz (Opus/Ogg)", []string{"opusenc", "opusdec", "oggmux", "oggdemux", "rawaudioparse", "audioconvert", "audioresample"},
		pkgs{"gst-plugins-base", "gstreamer1.0-plugins-base", "gstreamer1-plugins-base"}},
	{"GStreamer: decodificar mídia", []string{"decodebin", "playbin", "videoconvert", "videorate", "videoscale"},
		pkgs{"gst-plugins-base", "gstreamer1.0-plugins-base", "gstreamer1-plugins-base"}},
	{"GStreamer: microfone", []string{"pulsesrc"},
		pkgs{"gst-plugins-good (e pipewire-pulse ou pulseaudio)", "gstreamer1.0-pulseaudio", "gstreamer1-plugins-good (e pipewire-pulseaudio)"}},
	{"GStreamer: imagens e WAV", []string{"pngenc", "pngdec", "multifilesink", "wavenc"},
		pkgs{"gst-plugins-good", "gstreamer1.0-plugins-good", "gstreamer1-plugins-good"}},
	{"GStreamer: criar figurinhas (webp)", []string{"webpenc"},
		pkgs{"gst-plugins-bad", "gstreamer1.0-plugins-bad", "gstreamer1-plugins-bad-free"}},
	{"GStreamer: vídeo H.264", []string{"avdec_h264"},
		pkgs{"gst-libav", "gstreamer1.0-libav", "gstreamer1-libav (RPM Fusion)"}},
}

// Run confere tudo e escreve o relatório em w.
func Run(w io.Writer, e Env) {
	v, c, _ := buildinfo.Info()
	head := "zapterm " + v
	if c != "" {
		head += " (" + c + ")"
	}
	fmt.Fprintf(w, "%s: o que o sistema tem e o que falta\n\n", head)
	for _, l := range Check(e) {
		fmt.Fprintln(w, l.render(e.Color))
	}
}

// Check devolve as linhas do relatório, na ordem.
func Check(e Env) []line {
	var out []line
	add := func(l line) { out = append(out, l) }

	// Terminal e fonte.
	switch {
	case e.Getenv("ZAPTERM_IMAGES") == "0" || !e.Cfg.Interface.Images && e.Getenv("ZAPTERM_IMAGES") != "1":
		add(line{dica, "imagens no terminal", "desligadas ([interface] images = false ou ZAPTERM_IMAGES=0)", ""})
	case e.Getenv("ZAPTERM_IMAGES") == "1" || e.Graphics():
		add(line{ok, "terminal com gráficos", "fotos e figurinhas aparecem no chat", ""})
	default:
		add(line{falta, "terminal com gráficos", "fotos e figurinhas viram texto",
			"use o kitty (TERM=xterm-kitty); em outro terminal ZAPTERM_IMAGES=1 força, se ele aceitar o protocolo do kitty"})
	}
	add(line{dica, "Nerd Font", "ícones da lista e dos balões",
		"não dá para conferir daqui: instale uma em nerdfonts.com e use no terminal; sem ela, ZAPTERM_NERDFONT=0 ou nerd_font = false no config troca por texto"})

	// GStreamer.
	for _, t := range []struct {
		bin, what string
		pkg       pkgs
	}{
		{"gst-launch-1.0", "tocar, gravar e converter áudio, vídeo e figurinhas", pkgs{"gstreamer", "gstreamer1.0-tools", "gstreamer1"}},
		{"gst-discoverer-1.0", "tamanho e duração de vídeo e áudio ao anexar", pkgs{"gstreamer", "gstreamer1.0-tools", "gstreamer1"}},
	} {
		out = append(out, binLine(e, t.bin, t.what, t.pkg))
	}
	if _, err := e.LookPath("gst-inspect-1.0"); err != nil {
		add(line{falta, "gst-inspect-1.0", "conferir os plugins do GStreamer", pkgs{"gstreamer", "gstreamer1.0-tools", "gstreamer1"}.String()})
	} else {
		out = append(out, gstLines(e)...)
	}

	// Área de transferência: wl-clipboard no Wayland, xclip no X11.
	if e.Getenv("WAYLAND_DISPLAY") != "" {
		for _, b := range []struct{ bin, what string }{
			{"wl-copy", "copiar mensagem e link"},
			{"wl-paste", "colar print e texto com ctrl+v"},
		} {
			out = append(out, binLine(e, b.bin, b.what, pkgs{"wl-clipboard", "wl-clipboard", "wl-clipboard"}))
		}
	} else {
		out = append(out, binLine(e, "xclip", "copiar mensagem e link, colar print e texto com ctrl+v (X11)", pkgs{"xclip", "xclip", "xclip"}))
	}

	// D-Bus.
	out = append(out, dbusLine(e, "servidor de notificação (D-Bus)", "org.freedesktop.Notifications",
		"notificações com clique e resposta; o zapterm fala direto com o D-Bus, não usa notify-send",
		"rode um: mako, dunst, swaync ou o do seu ambiente. Arch: mako · Debian/Ubuntu: mako-notifier · Fedora: mako"))
	out = append(out, dbusLine(e, "portal de arquivos (D-Bus)", "org.freedesktop.portal.Desktop",
		"seletor de arquivos ao anexar (FileChooser)",
		"xdg-desktop-portal com um backend (gtk, gnome, kde). Arch/Debian/Ubuntu/Fedora: xdg-desktop-portal e xdg-desktop-portal-gtk"))

	// Som, vídeo, links.
	if _, err := e.LookPath("pw-play"); err == nil {
		add(line{ok, "pw-play", "som da notificação", ""})
	} else if _, err := e.LookPath("paplay"); err == nil {
		add(line{ok, "paplay", "som da notificação", ""})
	} else {
		add(line{falta, "pw-play ou paplay", "som da notificação ([interface] notify_sound)",
			"Arch: pipewire ou libpulse · Debian/Ubuntu: pipewire-bin ou pulseaudio-utils · Fedora: pipewire-utils ou pulseaudio-utils"})
	}
	out = append(out, binLine(e, "mpv", "vídeos e áudio com velocidade (sem ele usa o GStreamer, sem controles)", pkgs{"mpv", "mpv", "mpv"}))
	out = append(out, binLine(e, "xdg-open", "abrir links e arquivos sem programa próprio", pkgs{"xdg-utils", "xdg-utils", "xdg-utils"}))

	// Transcrição (opcional).
	if cmd, model, err := e.Whisper(); err != nil {
		add(line{dica, "transcrição de áudio (opcional)", "whisper.cpp e um modelo ggml",
			err.Error() + ". whisper-cli: Arch: whisper.cpp (AUR) · Debian/Ubuntu: compile github.com/ggml-org/whisper.cpp · Fedora: whisper-cpp"})
	} else {
		add(line{ok, "transcrição de áudio", cmd + " com " + model, ""})
	}

	// Terminal do clique.
	if argv, src, err := e.Terminal(); err != nil {
		add(line{falta, "terminal do clique", "abrir o zapterm pela notificação e pelo widget",
			err.Error() + ". Instale kitty, foot, alacritty, wezterm ou ghostty (Arch/Debian/Ubuntu/Fedora: o nome do programa)"})
	} else {
		add(line{ok, "terminal do clique", strings.Join(argv, " ") + " (" + src + ")", ""})
	}
	if t := e.Cfg.Interface.Terminal; len(t) > 0 {
		if _, err := e.LookPath(t[0]); err != nil {
			add(line{falta, "[interface] terminal", t[0] + " do config não está instalado; usando outro",
				"corrija ou apague terminal = [...] no config"})
		}
	}

	// Ambiente e daemon.
	if e.Getenv("XDG_RUNTIME_DIR") != "" {
		add(line{ok, "XDG_RUNTIME_DIR", e.Getenv("XDG_RUNTIME_DIR"), ""})
	} else {
		add(line{falta, "XDG_RUNTIME_DIR", "o socket do daemon cai para " + paths.Socket(),
			"é definida pelo login (systemd-logind ou elogind/seatd); em sessão sem ela o zapterm ainda funciona"})
	}
	out = append(out, daemonLine(e))
	return out
}

func binLine(e Env, bin, what string, p pkgs) line {
	if _, err := e.LookPath(bin); err == nil {
		return line{ok, bin, what, ""}
	}
	return line{falta, bin, what, p.String()}
}

func dbusLine(e Env, what, name, why, hint string) line {
	running, act, err := e.DBusName(name)
	switch {
	case err != nil:
		return line{falta, what, "sem barramento de sessão do D-Bus: " + err.Error(), "confira DBUS_SESSION_BUS_ADDRESS"}
	case running:
		return line{ok, what, why, ""}
	case act:
		return line{ok, what, why + " (o D-Bus sobe sob demanda)", ""}
	}
	return line{falta, what, why, hint}
}

func gstLines(e Env) []line {
	type res struct{ missing []string }
	results := make([]res, len(gstGroups))
	var wg sync.WaitGroup
	for i, g := range gstGroups {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var mu sync.Mutex
			var inner sync.WaitGroup
			for _, el := range g.elements {
				inner.Add(1)
				go func() {
					defer inner.Done()
					if !e.GstElement(el) {
						mu.Lock()
						results[i].missing = append(results[i].missing, el)
						mu.Unlock()
					}
				}()
			}
			inner.Wait()
		}()
	}
	wg.Wait()
	var out []line
	for i, g := range gstGroups {
		if m := results[i].missing; len(m) > 0 {
			// Ordem estável (a da lista), não a de quem terminou primeiro.
			slices.SortFunc(m, func(a, b string) int { return slices.Index(g.elements, a) - slices.Index(g.elements, b) })
			out = append(out, line{falta, g.what, "faltam: " + strings.Join(m, ", "), g.pkg.String()})
		} else {
			out = append(out, line{ok, g.what, strings.Join(g.elements, " "), ""})
		}
	}
	return out
}

func daemonLine(e Env) line {
	h, running, err := e.Daemon()
	mine := buildinfo.V()
	switch {
	case !running:
		return line{dica, "daemon", "não está rodando; sobe sozinho ao abrir o zapterm", "para subir com a sessão: exec zapterm daemon (veja o README)"}
	case daemonctl.IsLegacy(err):
		return line{falta, "daemon", "rodando numa versão anterior à atual (" + mine + ")", "zapterm restart"}
	case err != nil:
		return line{dica, "daemon", "rodando, mas não respondeu à versão: " + err.Error(), "zapterm restart"}
	case h.Proto != ipc.ProtoVersion || h.Version != mine:
		return line{falta, "daemon", fmt.Sprintf("rodando em outra versão (%s, este binário é %s)", h.Version, mine), "zapterm restart"}
	}
	return line{ok, "daemon", fmt.Sprintf("rodando, mesma versão (%s, pid %d)", h.Version, h.PID), ""}
}

func (l line) render(color bool) string {
	tag, code := "ok   ", "32"
	switch l.st {
	case falta:
		tag, code = "falta", "31"
	case dica:
		tag, code = "dica ", "33"
	}
	if color {
		tag = "\x1b[" + code + "m" + tag + "\x1b[0m"
	}
	s := "  " + tag + "  " + l.what
	if l.detail != "" {
		s += ": " + l.detail
	}
	if l.hint != "" {
		s += "\n" + strings.Repeat(" ", 9) + "→ " + l.hint
	}
	return s
}
