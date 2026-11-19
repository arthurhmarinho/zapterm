package wa

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFileMime(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{"a.pdf": "application/pdf", "v.mp4": "video/mp4", "f.jpg": "image/jpeg", "m.mp3": "audio/mpeg"}
	for name, want := range cases {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o600)
		if got := fileMime(p); got != want {
			t.Errorf("%s: %s, quer %s", name, got, want)
		}
	}
	// Sem extensão: pelo conteúdo.
	p := filepath.Join(dir, "semext")
	os.WriteFile(p, []byte("%PDF-1.7\n"), 0o600)
	if got := fileMime(p); got != "application/pdf" {
		t.Errorf("sem extensão: %s", got)
	}
}

func TestMediaSeconds(t *testing.T) {
	if _, err := exec.LookPath("gst-discoverer-1.0"); err != nil {
		t.Skip("sem GStreamer")
	}
	p := filepath.Join(t.TempDir(), "a.ogg")
	// 150 buffers de 1024 amostras a 48 kHz ≈ 3,2 s
	gen := exec.Command("gst-launch-1.0", "-q", "-e", "audiotestsrc", "num-buffers=150", "samplesperbuffer=1024", "!",
		"audioconvert", "!", "audioresample", "!", "audio/x-raw,rate=48000", "!", "opusenc", "!", "oggmux", "!",
		"filesink", "location="+p)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if s := mediaSeconds(context.Background(), p); s != 3 {
		t.Fatalf("duração %d, quer 3", s)
	}
}
