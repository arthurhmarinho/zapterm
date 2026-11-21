package tui

import (
	"strings"
	"testing"
)

func TestBuscaDeEmoji(t *testing.T) {
	m := &model{}
	first := func(q string) string {
		r := m.searchEmoji(q, 5)
		if len(r) == 0 {
			return ""
		}
		return r[0].char
	}
	for q, want := range map[string]string{
		"kkk": "😂", "coracao vermelho": "❤️", "polegar": "👍", "joy": "😂", "fogo": "🔥",
	} {
		if got := first(q); got != want {
			t.Errorf("%q → %q, queria %q", q, got, want)
		}
	}
	// Recente sobe no empate.
	before := m.searchEmoji("rosto", 50)
	last := before[len(before)-1].char
	m.useEmoji(last)
	if got := m.searchEmoji("rosto", 50); got[0].char != last {
		t.Errorf("recente %q não subiu: %q", last, got[0].char)
	}
}

func TestDoisPontosNoCampo(t *testing.T) {
	m := newModel(nil)
	m.w, m.h = 120, 40
	m.input.SetValue("que engraçado :kk")
	m.updateEmoji()
	if !m.emoji.open || m.emoji.list[0].char != "😂" {
		t.Fatalf("lista: %+v", m.emoji)
	}
	m.keyEmoji("enter")
	if v := m.input.Value(); v != "que engraçado 😂" {
		t.Fatalf("campo: %q", v)
	}
	// Hora não abre a lista.
	m.input.SetValue("às 12:30")
	m.updateEmoji()
	if m.emoji.open {
		t.Error("12:30 abriu a lista de emoji")
	}
}

func TestReagirComQualquerEmoji(t *testing.T) {
	m := newModel(nil)
	m.reacting = true
	for _, k := range []string{"f", "o", "g", "o"} {
		m.keyReact(k)
	}
	if c := m.reactChoices(); len(c) == 0 || c[0] != "🔥" {
		t.Fatalf("busca no painel: %v", c)
	}
	if !strings.Contains(m.reactQuery, "fogo") {
		t.Fatal("busca não acumulou")
	}
	m.keyReact("esc")
	if m.reactQuery != "" || !m.reacting {
		t.Error("esc deveria limpar a busca e manter o painel")
	}
}

func TestAmostraEmoji(t *testing.T) {
	m := &model{}
	for _, q := range []string{"ri", "cora", "triste", "ok", "obrigado", "festa", "pensando"} {
		var out []string
		for _, e := range m.searchEmoji(q, 6) {
			out = append(out, e.char)
		}
		t.Logf(":%-9s %s", q, strings.Join(out, " "))
	}
}
