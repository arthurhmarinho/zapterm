package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// O desenho depende de cada placeholder (U+10EEEE + 2 diacríticos) contar
// como uma coluna para o lipgloss e o ansi; senão as bordas quebram.
func TestPlaceholderLargura(t *testing.T) {
	p := placeholder(&kimage{id: 70000, cols: 12, rows: 3})
	lines := strings.Split(p, "\n")
	if len(lines) != 3 {
		t.Fatalf("linhas = %d", len(lines))
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w != 12 {
			t.Fatalf("lipgloss.Width = %d, quer 12", w)
		}
		if w := ansi.StringWidth(ansi.Truncate(l, 12, "")); w != 12 {
			t.Fatalf("Truncate mudou a largura: %d", w)
		}
	}
	b := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(p)
	for _, l := range strings.Split(b, "\n") {
		if w := lipgloss.Width(l); w != 16 {
			t.Fatalf("balão com largura %d, quer 16: %q", w, l)
		}
	}
	if !strings.Contains(b, "\x1b[38;2;1;17;112m") {
		t.Fatal("a cor do id (70000 = 1,17,112) se perdeu dentro do balão")
	}
}
