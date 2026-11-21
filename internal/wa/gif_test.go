package wa

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Um MP4 de 3 s (como o "GIF" do WhatsApp) vira ~30 quadros de 320 px.
func TestQuadrosDoGIF(t *testing.T) {
	if exec.Command("gst-inspect-1.0", "openh264enc").Run() != nil {
		t.Skip("sem codificador H.264 no GStreamer")
	}
	dir := t.TempDir()
	mp4 := filepath.Join(dir, "gif.mp4")
	gen := exec.Command("gst-launch-1.0", "-q", "-e", "videotestsrc", "num-buffers=90", "!",
		"video/x-raw,width=640,height=360,framerate=30/1", "!", "videoconvert", "!", "openh264enc", "!",
		"h264parse", "!", "mp4mux", "!", "filesink", "location="+mp4)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("gerando mp4: %v %s", err, out)
	}
	var md ipc.Media
	if err := gifFrames(mp4, filepath.Join(dir, "x"), &md); err != nil {
		t.Fatal(err)
	}
	if n := len(md.Frames); n < 25 || n > 35 || len(md.Delays) != n {
		t.Fatalf("%d quadros", n)
	}
	if md.Width != 320 || md.Height != 180 {
		t.Fatalf("tamanho %dx%d", md.Width, md.Height)
	}
	var again ipc.Media
	loadAnim(filepath.Join(dir, "x"), &again)
	if len(again.Frames) != len(md.Frames) {
		t.Fatal("manifesto não recarrega")
	}
}
