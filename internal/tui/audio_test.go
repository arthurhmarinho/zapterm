package tui

import (
	"os"
	"os/exec"
	"path/filepath"
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

// Grava com uma fonte sintética (sem microfone), pausa, retoma e para pelo
// SIGINT como na TUI; confere que a pausa não entra na duração e que o
// OGG/Opus emendado saiu fechado e decodificável.
func TestGravacaoComPausa(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("sem GStreamer")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ZAPTERM_REC_SRC", "audiotestsrc is-live=true")
	m := &model{active: "x@s.whatsapp.net"}
	if m.startRec() == nil || m.rec == nil {
		t.Fatalf("não começou: %s", m.flash)
	}
	rec := m.rec
	time.Sleep(1200 * time.Millisecond)
	m.togglePauseRec()
	if rec.cur != nil {
		t.Fatal("não pausou")
	}
	time.Sleep(1500 * time.Millisecond) // pausada: não pode contar
	m.togglePauseRec()
	if rec.cur == nil {
		t.Fatal("não retomou")
	}
	time.Sleep(1200 * time.Millisecond)
	cmd := rec.finish(true, false)
	m.rec = nil
	done, ok := cmd().(recDoneMsg)
	if !ok || done.err != nil || !done.send {
		t.Fatalf("finish: %+v (%v)", done, done.err)
	}
	if done.seconds < 2 || done.seconds > 3 {
		t.Fatalf("duração %d s, quer ~2 (sem a pausa)", done.seconds)
	}
	dec := exec.Command("gst-launch-1.0", "-q", "filesrc", "location="+done.path, "!", "oggdemux", "!",
		"opusdec", "!", "fakesink")
	if out, err := dec.CombinedOutput(); err != nil {
		t.Fatalf("arquivo não decodifica: %v %s", err, out)
	}
	if left, _ := filepath.Glob(rec.base + "-*.pcm"); len(left) != 0 {
		t.Fatalf("trechos ficaram no disco: %v", left)
	}
	os.Remove(done.path)
}

// Descartar com a gravação pausada não deixa nada no disco.
func TestGravacaoDescartada(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("sem GStreamer")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ZAPTERM_REC_SRC", "audiotestsrc is-live=true")
	m := &model{active: "x@s.whatsapp.net"}
	m.startRec()
	rec := m.rec
	time.Sleep(500 * time.Millisecond)
	m.togglePauseRec()
	if done := rec.finish(false, false)().(recDoneMsg); done.path != "" || done.err != nil {
		t.Fatalf("descartar: %+v", done)
	}
	if left, _ := filepath.Glob(rec.base + "*"); len(left) != 0 {
		t.Fatalf("sobrou: %v", left)
	}
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

// Toca um áudio sintético no mpv (sem som: --ao=null) e confere que
// velocidade, pausa e avançar funcionam pelo socket.
func TestPlayerMPV(t *testing.T) {
	if !hasMPV() {
		t.Skip("sem mpv")
	}
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("sem GStreamer")
	}
	t.Setenv("ZAPTERM_AUDIO_NULL", "1")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	path := t.TempDir() + "/tom.ogg"
	gen := exec.Command("gst-launch-1.0", "-q", "audiotestsrc", "num-buffers=600", "!", "audioconvert", "!",
		"opusenc", "!", "oggmux", "!", "filesink", "location="+path)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("gerando áudio: %v %s", err, out)
	}
	m := &model{audioSpeed: 2}
	m.startPlay(path, "a", 13)
	defer m.stopPlay()
	if m.play == nil || m.play.ctl == nil {
		t.Fatal("mpv não respondeu no socket")
	}
	time.Sleep(time.Second)
	pos, paused := m.play.ctl.state()
	if paused || pos < 1.4 {
		t.Fatalf("em 2× deveria ter andado ~2 s: %.2f (pausado %v)", pos, paused)
	}
	m.togglePause()
	p1, _ := m.play.ctl.state()
	time.Sleep(400 * time.Millisecond)
	p2, paused := m.play.ctl.state()
	if !paused || p2-p1 > 0.05 {
		t.Fatalf("pausa não segurou: %.2f → %.2f", p1, p2)
	}
	m.seekAudio(5)
	time.Sleep(100 * time.Millisecond)
	if p3, _ := m.play.ctl.state(); p3 < p2+4.5 {
		t.Fatalf("avançar 5 s: %.2f → %.2f", p2, p3)
	}
}

func TestRecadoDeVideo(t *testing.T) {
	m := ipc.Message{Kind: "video", Text: "[recado de vídeo 0:12]"}
	if !isVideoNote(m) || isVideoNote(ipc.Message{Kind: "video", Text: "[vídeo 0:12]"}) {
		t.Fatal("isVideoNote")
	}
	if got := videoLabel(m.Text); got != icons.play+" Recado de vídeo 0:12" {
		t.Fatalf("rótulo: %q", got)
	}
}

func TestChamadaTocando(t *testing.T) {
	m := ipc.Message{ID: "call-X", Kind: "other", Text: "📞 Chamada de voz recebida · atenda no celular", TS: time.Now().UnixMilli()}
	if !isRingingCall(m) {
		t.Fatal("chamada recente devia estar tocando")
	}
	old := m
	old.TS = time.Now().Add(-5 * time.Minute).UnixMilli()
	missed := m
	missed.Text = "📞 Chamada de voz perdida"
	if isRingingCall(old) || isRingingCall(missed) {
		t.Fatal("antiga ou perdida não toca")
	}
	r := msgRenderer{w: 80, sel: -2}
	out, _ := r.render([]ipc.Message{m})
	if plain := ansi.Strip(out); !strings.Contains(plain, "ctrl+o") || !strings.Contains(plain, "recusa") {
		t.Fatalf("sem a dica de recusar:\n%s", ansi.Strip(out))
	}
}
