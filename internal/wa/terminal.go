package wa

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/arthurhmarinho/zapterm/internal/config"
)

// knownTerminals: o que tentar, na ordem, quando nada foi configurado. Cada
// um já leva a classe "zapterm" (para o compositor achar a janela) e o que o
// terminal exige antes do comando.
var knownTerminals = [][]string{
	{"kitty", "--class", "zapterm"},
	{"foot", "-a", "zapterm"},
	{"alacritty", "--class", "zapterm", "-e"},
	{"wezterm", "start", "--class", "zapterm", "--"},
	{"ghostty", "--class=zapterm", "-e"},
	{"xdg-terminal-exec"},
}

// Origem do terminal escolhido, para o doctor dizer de onde veio.
const (
	TermConfig = "[interface] terminal"
	TermEnv    = "$TERMINAL"
	TermAuto   = "detectado"
)

// pickTerminal devolve o comando do terminal (sem o zapterm no fim) e de
// onde veio: 1. [interface] terminal do config; 2. $TERMINAL -e; 3. o
// primeiro instalado de knownTerminals. lookPath é parâmetro para o teste não
// depender do que está instalado.
func pickTerminal(cfg []string, termEnv string, lookPath func(string) (string, error)) (argv []string, source string, err error) {
	found := func(name string) bool { _, e := lookPath(name); return e == nil }
	if len(cfg) > 0 && found(cfg[0]) {
		return append([]string(nil), cfg...), TermConfig, nil
	}
	if f := strings.Fields(termEnv); len(f) > 0 && found(f[0]) {
		return append(f, "-e"), TermEnv, nil
	}
	for _, t := range knownTerminals {
		if found(t[0]) {
			return append([]string(nil), t...), TermAuto, nil
		}
	}
	return nil, "", errors.New("nenhum terminal encontrado (defina [interface] terminal no config ou $TERMINAL)")
}

// TerminalCommand é o terminal que o clique (notificação, widget) abre, com o
// que está instalado agora.
func TerminalCommand(cfg config.Config) (argv []string, source string, err error) {
	return pickTerminal(cfg.Interface.Terminal, os.Getenv("TERMINAL"), exec.LookPath)
}

// selfExe é o caminho do zapterm para pôr no fim do comando do terminal. Se o
// binário foi trocado com o daemon vivo, o caminho do processo ganha
// " (deleted)" e não existe mais: cai para o que o PATH achar.
func selfExe(lookPath func(string) (string, error)) (string, error) {
	exe, err := os.Executable()
	if err == nil {
		exe = strings.TrimSuffix(exe, " (deleted)")
		if _, serr := os.Stat(exe); serr == nil {
			return exe, nil
		}
	}
	return lookPath("zapterm")
}

// openTerminal abre o zapterm numa janela nova de terminal, desligada do
// daemon (setsid). env entra no ambiente do processo (ex.: ZAPTERM_OPEN=jid).
func (d *Daemon) openTerminal(env ...string) error {
	argv, _, err := TerminalCommand(d.cfg)
	if err != nil {
		return err
	}
	exe, err := selfExe(exec.LookPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(argv[0], append(argv[1:], exe)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
