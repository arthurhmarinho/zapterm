package wa

import (
	"testing"
	"time"
)

func TestFold(t *testing.T) {
	for in, want := range map[string]string{"Não": "nao", "JOÃO": "joao", "ação ÇA": "acao ca", "café ☕": "cafe ☕"} {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestParseQuery(t *testing.T) {
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, time.Local)
	day := func(y int, m time.Month, d int) int64 { return time.Date(y, m, d, 0, 0, 0, 0, time.Local).UnixMilli() }
	text, f, err := parseQuery("contrato tipo:PDF data:12/10", now)
	if err != nil || text != "contrato" || f.Ext != "pdf" || f.Type != "" || f.From != day(2026, 10, 12) || f.To != day(2026, 10, 13) {
		t.Fatalf("pdf+data: %q %+v %v", text, f, err)
	}
	// Dia/mês ainda por vir é do ano passado.
	if _, f, _ := parseQuery("data:25/12", now); f.From != day(2025, 12, 25) {
		t.Errorf("25/12 sem ano: %+v", f)
	}
	if _, f, _ := parseQuery("tipo:vídeo depois:ontem", now); f.Type != "videos" || f.From != day(2026, 11, 22) || f.To != 0 {
		t.Errorf("vídeo depois de ontem: %+v", f)
	}
	if _, f, _ := parseQuery("antes:1/1/25", now); f.To != day(2025, 1, 1) {
		t.Errorf("antes: %+v", f)
	}
	if text, f, _ := parseQuery("link: http://x.com", now); text != "link: http://x.com" || f.Active() {
		t.Errorf("texto comum virou filtro: %q %+v", text, f)
	}
	for _, bad := range []string{"tipo:qualquercoisa", "data:31/02", "data:amanha"} {
		if _, _, err := parseQuery(bad, now); err == nil {
			t.Errorf("%s deveria dar erro", bad)
		}
	}
}
