package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func write(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "zapterm"), 0o700)
	os.WriteFile(filepath.Join(dir, "zapterm", "config.toml"), []byte(body), 0o600)
}

func TestSemArquivoUsaPadroes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := Load()
	if err != nil || !c.Privacy.ReadReceipts || !c.Interface.Images || len(c.KeyRemap()) != 0 {
		t.Fatalf("padrões: %+v %v", c, err)
	}
}

// O arquivo padrão, todo comentado, tem de ser TOML válido e dar os padrões.
func TestArquivoPadrao(t *testing.T) {
	var c Config
	if _, err := toml.Decode(Default, &c); err != nil {
		t.Fatal(err)
	}
	// e cada ação de [keys] tem de estar documentada nele
	for a := range Actions {
		if !strings.Contains(Default, "# "+a+" = ") {
			t.Errorf("ação %q não documentada no default.toml", a)
		}
	}
}

func TestValoresEAtalhos(t *testing.T) {
	write(t, `
[privacy]
read_receipts = false
[colors]
accent = "#ff79c6"
[keys]
reply = "alt+r"
react = "ctrl+r"
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Privacy.ReadReceipts || !c.Privacy.ShowOnline || c.Colors.Accent != "#ff79c6" {
		t.Fatalf("valores: %+v", c)
	}
	r := c.KeyRemap()
	// alt+r responde; ctrl+r (que era responder) agora reage; ctrl+e
	// (que era reagir) fica desligado.
	if r["alt+r"] != "ctrl+r" || r["ctrl+r"] != "ctrl+e" || r["ctrl+e"] != "" {
		t.Fatalf("remapeamento: %v", r)
	}
	if _, ok := r["ctrl+e"]; !ok {
		t.Fatal("ctrl+e devia estar desligado")
	}
}

func TestErros(t *testing.T) {
	write(t, "[keys]\nresponder = \"alt+r\"\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "responder") {
		t.Fatalf("ação inválida passou: %v", err)
	}
	write(t, "[privacy]\nread_recipts = false\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "read_recipts") {
		t.Fatalf("chave com erro de digitação passou: %v", err)
	}
	write(t, "isso não é toml")
	if c, err := Load(); err == nil || !c.Privacy.ReadReceipts {
		t.Fatalf("sintaxe inválida: %v", err)
	}
}

func TestTerminalDoClique(t *testing.T) {
	write(t, "[interface]\nterminal = [\"foot\", \"-a\", \"zapterm\"]\n")
	c, err := Load()
	if err != nil || len(c.Interface.Terminal) != 3 || c.Interface.Terminal[0] != "foot" {
		t.Fatalf("terminal: %+v %v", c.Interface.Terminal, err)
	}
	if !strings.Contains(Default, "# terminal = [") {
		t.Error("terminal não documentado no default.toml")
	}
}
