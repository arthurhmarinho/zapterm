package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// As cores vêm do tema do terminal: fundo padrão (49) e ANSI 0–15, nunca
// RGB fixo.
func TestPaintUsaCoresDoTerminal(t *testing.T) {
	cases := map[string]string{
		bgSeq(cBgList):             "\x1b[49m",
		bgSeq(cBgPanel):            "\x1b[40m",
		bgSeq(cBubbleOut):          "\x1b[100m",
		bgSeq(lipgloss.Color("5")): "\x1b[45m",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("bgSeq = %q, quer %q", got, want)
		}
	}
	out := paint(sText2.Render("oi")+" mundo", cBgPanel, 12)
	if strings.Contains(out, "48;2;") {
		t.Fatalf("paint gerou fundo RGB: %q", out)
	}
	if !strings.HasPrefix(out, "\x1b[40m") || lipgloss.Width(out) != 12 {
		t.Fatalf("paint inesperado: %q", out)
	}
}
