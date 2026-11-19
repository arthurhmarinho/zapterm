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
	done, ok := m.finishRec(true)().(recDoneMsg)
	if !ok || done.err != nil || !done.send {
		t.Fatalf("finishRec: %+v", done)
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
