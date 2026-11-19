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
