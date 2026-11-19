package wa

import "testing"

func TestFold(t *testing.T) {
	for in, want := range map[string]string{"Não": "nao", "JOÃO": "joao", "ação ÇA": "acao ca", "café ☕": "cafe ☕"} {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, quer %q", in, got, want)
		}
	}
}
