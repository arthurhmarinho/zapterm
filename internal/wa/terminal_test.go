package wa

import (
	"errors"
	"reflect"
	"testing"
)

// instalados devolve um LookPath que só "acha" os programas da lista.
func instalados(nomes ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, x := range nomes {
			if x == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("não achei")
	}
}

func TestPickTerminal(t *testing.T) {
	casos := []struct {
		nome     string
		cfg      []string
		env      string
		instal   []string
		wantArgv []string
		wantSrc  string
		wantErr  bool
	}{
		{"config vence tudo", []string{"foot", "-a", "x"}, "kitty", []string{"foot", "kitty"}, []string{"foot", "-a", "x"}, TermConfig, false},
		{"config com programa que falta cai no TERMINAL", []string{"sumiu"}, "foot", []string{"foot"}, []string{"foot", "-e"}, TermEnv, false},
		{"TERMINAL leva -e", nil, "alacritty", []string{"alacritty", "kitty"}, []string{"alacritty", "-e"}, TermEnv, false},
		{"TERMINAL com argumentos", nil, "wezterm start", []string{"wezterm"}, []string{"wezterm", "start", "-e"}, TermEnv, false},
		{"TERMINAL que falta é ignorado", nil, "sumiu", []string{"foot"}, []string{"foot", "-a", "zapterm"}, TermAuto, false},
		{"kitty primeiro", nil, "", []string{"foot", "kitty", "alacritty"}, []string{"kitty", "--class", "zapterm"}, TermAuto, false},
		{"foot", nil, "", []string{"foot", "alacritty"}, []string{"foot", "-a", "zapterm"}, TermAuto, false},
		{"alacritty", nil, "", []string{"alacritty", "wezterm"}, []string{"alacritty", "--class", "zapterm", "-e"}, TermAuto, false},
		{"wezterm", nil, "", []string{"wezterm", "ghostty"}, []string{"wezterm", "start", "--class", "zapterm", "--"}, TermAuto, false},
		{"ghostty", nil, "", []string{"ghostty", "xdg-terminal-exec"}, []string{"ghostty", "--class=zapterm", "-e"}, TermAuto, false},
		{"xdg-terminal-exec por último", nil, "", []string{"xdg-terminal-exec"}, []string{"xdg-terminal-exec"}, TermAuto, false},
		{"nenhum", nil, "", nil, nil, "", true},
	}
	for _, c := range casos {
		argv, src, err := pickTerminal(c.cfg, c.env, instalados(c.instal...))
		if (err != nil) != c.wantErr || !reflect.DeepEqual(argv, c.wantArgv) || src != c.wantSrc {
			t.Errorf("%s: got %v %q %v", c.nome, argv, src, err)
		}
	}
}

// O resultado não pode apontar para a tabela global: quem o altera (append do
// zapterm no fim) estragaria a próxima escolha.
func TestPickTerminalNaoCompartilhaTabela(t *testing.T) {
	a, _, _ := pickTerminal(nil, "", instalados("kitty"))
	a = append(a[:1], "lixo")
	b, _, _ := pickTerminal(nil, "", instalados("kitty"))
	if !reflect.DeepEqual(b, []string{"kitty", "--class", "zapterm"}) {
		t.Fatalf("tabela global alterada: %v", b)
	}
}

func TestSelfExe(t *testing.T) {
	// O binário de teste existe, então é ele mesmo.
	if exe, err := selfExe(instalados()); err != nil || exe == "" {
		t.Fatalf("selfExe com o binário presente: %q %v", exe, err)
	}
}
