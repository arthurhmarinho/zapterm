package wa

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/arthurhmarinho/zapterm/internal/config"
)

func TestLimparTranscricao(t *testing.T) {
	in := "\n [Música]\n Oi, tudo bem?\n (risos)\n Te vejo amanhã.\n"
	if got := cleanTranscript(in); got != "Oi, tudo bem? Te vejo amanhã." {
		t.Errorf("limpo: %q", got)
	}
}

func TestWhisperSemProgramaOuModelo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, _, err := whisperSetup(config.Transcription{}); err == nil {
		t.Error("sem whisper-cli deveria explicar o que falta")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "whisper-cli")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, _, err := whisperSetup(config.Transcription{Command: bin}); err == nil {
		t.Error("sem modelo deveria explicar o que falta")
	}
	model := filepath.Join(dir, "ggml-base.bin")
	os.WriteFile(model, []byte("x"), 0o600)
	cmd, m, err := whisperSetup(config.Transcription{Command: bin, Model: model})
	if err != nil || cmd != bin || m != model {
		t.Errorf("com tudo: %q %q %v", cmd, m, err)
	}
}

func TestConverterParaWAV(t *testing.T) {
	if _, err := exec.LookPath("gst-launch-1.0"); err != nil {
		t.Skip("sem GStreamer")
	}
	dir := t.TempDir()
	ogg := filepath.Join(dir, "voz.ogg")
	// Meio segundo de tom em Opus, como uma mensagem de voz.
	gen := exec.Command("gst-launch-1.0", "-q", "audiotestsrc", "num-buffers=25", "!", "audioconvert", "!",
		"opusenc", "!", "oggmux", "!", "filesink", "location="+ogg)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("sem o plugin de Opus: %v %s", err, out)
	}
	wav := filepath.Join(dir, "voz.wav")
	if err := toWAV(context.Background(), ogg, wav); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(wav)
	// Cabeçalho WAV: 16 kHz (bytes 24–27), 1 canal (22–23), 16 bits (34–35).
	if len(data) < 44 || string(data[:4]) != "RIFF" || binary.LittleEndian.Uint32(data[24:]) != 16000 ||
		binary.LittleEndian.Uint16(data[22:]) != 1 || binary.LittleEndian.Uint16(data[34:]) != 16 {
		t.Fatalf("WAV inesperado: %d bytes, %x", len(data), data[:min(44, len(data))])
	}
}
