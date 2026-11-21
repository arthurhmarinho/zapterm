package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPlainText(t *testing.T) {
	cases := map[string]string{
		"*negrito* e _itálico_":                "negrito e itálico",
		"~riscado~ ok":                         "riscado ok",
		"conta 2*3*4 = 24":                     "conta 2*3*4 = 24", // * no meio de palavra não formata
		"* não é negrito *":                    "* não é negrito *",
		"veja https://ex.com/a_b_c_d e _isso_": "veja https://ex.com/a_b_c_d e isso",
		"```código *aqui*```":                  "código *aqui*",
		"`x_y`":                                "x_y",
		"*abre\nfecha*":                        "*abre\nfecha*", // não atravessa linha
		"snake_case_name":                      "snake_case_name",
		"_*os dois*_":                          "os dois",
	}
	for in, want := range cases {
		if got := plainText(in); got != want {
			t.Errorf("%q → %q, quer %q", in, got, want)
		}
	}
}

func TestFormatQuebraELarguras(t *testing.T) {
	out := formatText("*um texto bem comprido* com emoji ❤️ e link https://exemplo.com/caminho", 20, lipgloss.NewStyle())
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 20 {
			t.Errorf("linha com %d colunas: %q", w, ansi.Strip(l))
		}
	}
	plain := ansi.Strip(out)
	if strings.Contains(plain, "*") {
		t.Errorf("marcador sobrou: %q", plain)
	}
	if !strings.Contains(out, "\x1b]8;") {
		t.Error("link sem hyperlink (OSC 8)")
	}
	if firstURL("oi www.site.com.br/x.") != "https://www.site.com.br/x" {
		t.Errorf("firstURL: %q", firstURL("oi www.site.com.br/x."))
	}
}

func TestMencaoDestacada(t *testing.T) {
	in := "oi @\u2068Ana Souza\u2069, tudo bem?"
	if got := plainText(in); got != "oi @Ana Souza, tudo bem?" {
		t.Fatalf("plainText: %q", got)
	}
	out := formatText(in, 40, lipgloss.NewStyle())
	if strings.ContainsAny(out, "\u2068\u2069") {
		t.Fatal("isolante apareceu")
	}
	if ansi.Strip(out) != "oi @Ana Souza, tudo bem?" {
		t.Fatalf("texto: %q", ansi.Strip(out))
	}
}

func TestListasECitacao(t *testing.T) {
	got := ansi.Strip(formatText("compras:\n* pão\n- leite com *muito* chocolate quente\n2. ovos\n> disse ele", 20, sText))
	want := []string{"compras:", "• pão", "• leite com muito", "  chocolate quente", "2. ovos", "▎ disse ele"}
	lines := strings.Split(got, "\n")
	if len(lines) != len(want) {
		t.Fatalf("linhas:\n%s", got)
	}
	for i := range want {
		if strings.TrimRight(lines[i], " ") != want[i] {
			t.Errorf("linha %d: %q, queria %q", i, lines[i], want[i])
		}
	}
	if plain := ansi.Strip(formatText("*negrito* normal", 40, sText)); plain != "negrito normal" {
		t.Errorf("negrito no começo virou lista: %q", plain)
	}
}

func TestRotulosAntigos(t *testing.T) {
	for in, want := range map[string]string{
		"[contato: Zé]":     icons.contact + " Zé",
		"[enquete: Pizza?]": icons.poll + " Enquete: Pizza?",
		"[evento: Trilha · 22/11 06:00] levem água": icons.event + " Trilha · 22/11 06:00",
		"[localização]": icons.location + " Localização",
	} {
		if got := previewLabel(in); got != want {
			t.Errorf("%q → %q, queria %q", in, got, want)
		}
	}
}
