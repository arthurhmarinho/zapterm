package wa

import (
	"os"
	"path/filepath"
	"testing"
)

// Roda contra figurinhas animadas reais, se houver uma pasta com elas:
//
//	ZAPTERM_ANIM_DIR=/caminho go test ./internal/wa -run Animated -v
func TestDecodeAnimatedWebPReal(t *testing.T) {
	dir := os.Getenv("ZAPTERM_ANIM_DIR")
	if dir == "" {
		t.Skip()
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.webp"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		frames, err := decodeAnimatedWebP(data)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		total := 0
		for _, fr := range frames {
			total += fr.delay
		}
		b := frames[0].img.Bounds()
		t.Logf("%s: %d quadros, %dms, %dx%d", filepath.Base(f), len(frames), total, b.Dx(), b.Dy())
	}
}

func TestThinFramesMantemDuracao(t *testing.T) {
	var in []animFrame
	for range 150 {
		in = append(in, animFrame{delay: 40})
	}
	out := thinFrames(in)
	total := 0
	for _, f := range out {
		total += f.delay
	}
	if len(out) > animMaxFrames || total != 150*40 {
		t.Fatalf("%d quadros, %dms", len(out), total)
	}
}
