package wa

import (
	"testing"
	"time"
)

func TestDesfechoDaChamada(t *testing.T) {
	now := time.Unix(1000, 0)
	cases := []struct {
		lc    liveCall
		state string
		want  string
	}{
		{liveCall{}, "", "📞 Chamada de voz perdida"},
		{liveCall{video: true}, "", "📹 Chamada de vídeo perdida"},
		{liveCall{rejected: true}, "", "📞 Chamada de voz recusada"},
		{liveCall{accepted: now.Add(-133 * time.Second)}, "", "📞 Chamada de voz · atendida no celular · 2:13"},
		{liveCall{}, callRejected, "📞 Chamada de voz recusada"}, // recusada em outro aparelho
	}
	for _, c := range cases {
		state, dur := callOutcome(&c.lc, c.state, now)
		if got := callText(c.lc.video, state, dur); got != c.want {
			t.Errorf("%+v %q: %q, quer %q", c.lc, c.state, got, c.want)
		}
	}
	if got := callText(false, callRinging, 0); got != "📞 Chamada de voz recebida · atenda no celular" {
		t.Errorf("tocando: %q", got)
	}
	// Atendida, sem duração ainda (a chamada continua no celular).
	if got := callText(false, callAnswered, 0); got != "📞 Chamada de voz · atendida no celular" {
		t.Errorf("atendida: %q", got)
	}
}
