package wa

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// Gera um OGG/Opus sintético (sem microfone) e confere a ondinha: 64
// barras, pico em 100, e silêncio no começo mais baixo que o tom.
func TestWaveform(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("sem GStreamer")
	}
	path := filepath.Join(t.TempDir(), "a.ogg")
	// 1s de silêncio seguido de 1s de tom.
	gen := exec.Command("gst-launch-1.0", "-q", "-e", "concat", "name=c", "!", "audioconvert", "!",
		"audioresample", "!", "audio/x-raw,rate=48000,channels=1", "!", "opusenc", "!", "oggmux", "!",
		"filesink", "location="+path,
		"audiotestsrc", "wave=silence", "num-buffers=50", "!", "c.",
		"audiotestsrc", "num-buffers=50", "!", "c.")
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("gerando: %v %s", err, out)
	}
	w, err := waveform(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 64 {
		t.Fatalf("%d barras", len(w))
	}
	top := byte(0)
	for _, v := range w {
		top = max(top, v)
	}
	if top != 100 || w[5] >= w[60] {
		t.Fatalf("ondinha inesperada: %v", w)
	}
}
