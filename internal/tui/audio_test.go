package tui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

func TestAudioSeconds(t *testing.T) {
	for in, want := range map[string]int{"[áudio 0:12]": 12, "[arquivo de áudio 2:05]": 125, "[áudio]": 0} {
		if got := audioSeconds(in); got != want {
			t.Errorf("%q = %d, quer %d", in, got, want)
		}
	}
}

func TestAudioLine(t *testing.T) {
	r := msgRenderer{playID: "a", elapsed: 6}
	m := ipc.Message{ID: "a", Kind: "audio", Text: "[áudio 0:12]", HasRaw: true}
	if got := ansi.Strip(r.audioLine(m, 40)); !strings.Contains(got, "0:06 / 0:12") || !strings.Contains(got, "●") {
		t.Fatalf("tocando: %q", got)
	}
	m.ID = "b"
	if got := ansi.Strip(r.audioLine(m, 40)); !strings.HasSuffix(got, "0:12") {
		t.Fatalf("parado: %q", got)
	}
}

// Grava com uma fonte sintética (sem microfone), para pelo SIGINT como na
// TUI e confere que o OGG/Opus saiu fechado e decodificável.
func TestGravacaoFechaArquivo(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("sem GStreamer")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ZAPTERM_REC_SRC", "audiotestsrc")
	m := &model{active: "x@s.whatsapp.net"}
	if m.startRec() == nil || m.rec == nil {
		t.Fatalf("não começou: %s", m.flash)
	}
	time.Sleep(1500 * time.Millisecond)
	rec := m.rec
	m.finishRec(true)
	done, ok := rec.stop(true, int(time.Since(rec.started).Round(time.Second).Seconds()))().(recDoneMsg)
	if !ok || done.err != nil || !done.send {
		t.Fatalf("finishRec: %+v (%v)", done, done.err)
	}
	if done.seconds < 1 {
		t.Fatalf("duração %d", done.seconds)
	}
	dec := exec.Command("gst-launch-1.0", "-q", "filesrc", "location="+done.path, "!", "oggdemux", "!",
		"opusdec", "!", "fakesink")
	if out, err := dec.CombinedOutput(); err != nil {
		t.Fatalf("arquivo não decodifica: %v %s", err, out)
	}
	os.Remove(done.path)
}

func TestMediaLabelComDuracao(t *testing.T) {
	cases := map[string]string{
		"[vídeo 0:32] olha isso": icons.video + " olha isso",
		"[vídeo 0:32]":           icons.video + " Vídeo 0:32",
		"[áudio 1:05]":           icons.audio + " Áudio 1:05",
		"[imagem]":               icons.photo + " Foto",
	}
	for in, want := range cases {
		if got := mediaLabel(in); got != want {
			t.Errorf("%q = %q, quer %q", in, got, want)
		}
	}
	if got := videoLabel("[vídeo 0:32] olha isso"); got != icons.play+" Vídeo 0:32" {
		t.Errorf("videoLabel: %q", got)
	}
}

func TestApagadaEditadaReacoes(t *testing.T) {
	r := msgRenderer{w: 80, sel: -2}
	content, _ := r.render([]ipc.Message{
		{ID: "a", Kind: "revoked", TS: 1},
		{ID: "b", Kind: "text", Text: "oi de novo", Edited: true, TS: 2,
			Reactions: []ipc.Reaction{{Emoji: "👍", Count: 2, Mine: true}, {Emoji: "😂", Count: 1}}},
	})
	out := ansi.Strip(content)
	for _, want := range []string{"Mensagem apagada", "editada", "oi de novo", "👍 2", "😂"} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou %q em:\n%s", want, out)
		}
	}
}

func TestDigitandoETracinhos(t *testing.T) {
	m := newModel(nil)
	m.onPresence(ipc.Frame{Data: []byte(`{"chat":"g@g.us","who":"Ana","typing":true}`)})
	if got := m.typingText("g@g.us", true); got != "Ana está digitando…" {
		t.Fatalf("cabeçalho: %q", got)
	}
	if got := m.typingText("g@g.us", false); got != "Ana: digitando…" {
		t.Fatalf("lista: %q", got)
	}
	m.presence["g@g.us"].until = time.Now().Add(-time.Second)
	if m.typingText("g@g.us", true) != "" {
		t.Fatal("aviso vencido continuou")
	}
	for st, want := range map[int]string{0: "", 1: "✓", 2: "✓✓", 3: "✓✓"} {
		if got := ansi.Strip(ticks(st)); got != want {
			t.Errorf("ticks(%d) = %q", st, got)
		}
	}
}
