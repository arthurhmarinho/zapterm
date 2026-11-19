package wa

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// sendAudio envia um OGG/Opus gravado na TUI como mensagem de voz (PTT),
// com a ondinha que o celular desenha.
func (d *Daemon) sendAudio(ctx context.Context, chat, path string, seconds int, replyTo string) (any, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	up, err := d.cli.Upload(ctx, data, whatsmeow.MediaAudio)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	ci, quote, err := d.replyContext(chat, replyTo)
	if err != nil {
		return nil, err
	}
	wave, err := waveform(ctx, path)
	if err != nil {
		d.log.Warn("ondinha do áudio", "err", err) // vai sem; o celular mostra reta
	}
	am := &waE2E.AudioMessage{
		Mimetype:      proto.String("audio/ogg; codecs=opus"),
		URL:           proto.String(up.URL),
		DirectPath:    proto.String(up.DirectPath),
		MediaKey:      up.MediaKey,
		FileEncSHA256: up.FileEncSHA256,
		FileSHA256:    up.FileSHA256,
		FileLength:    proto.Uint64(up.FileLength),
		Seconds:       proto.Uint32(uint32(max(1, seconds))),
		PTT:           proto.Bool(true),
		Waveform:      wave,
		ContextInfo:   ci,
	}
	text := fmt.Sprintf("[áudio %d:%02d]", seconds/60, seconds%60)
	m := ipc.Message{Kind: "audio", Text: text, Quote: quote}
	return d.sendAndStore(ctx, jid, &waE2E.Message{AudioMessage: am}, m, func(id string) *ipc.Media {
		// Já entra no cache para tocar sem baixar de novo.
		base, err := mediaBase(id)
		if err != nil {
			return nil
		}
		md := &ipc.Media{Mime: "audio/ogg", Path: base + ".ogg"}
		if os.WriteFile(md.Path, data, 0o600) != nil {
			return nil
		}
		return md
	})
}

// waveform decodifica o áudio com o GStreamer para PCM mono de 8 kHz e
// reduz a 64 barras de 0 a 100 (o formato que o WhatsApp espera),
// normalizadas pelo pico para a fala não sair achatada.
func waveform(ctx context.Context, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gst-launch-1.0", "-q",
		"filesrc", "location="+path, "!", "oggdemux", "!", "opusdec", "!",
		"audioconvert", "!", "audioresample", "!",
		"audio/x-raw,format=S16LE,channels=1,rate=8000", "!", "fdsink", "fd=1")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	n := len(raw) / 2
	if n < 64 {
		return nil, fmt.Errorf("áudio curto demais")
	}
	rms := make([]float64, 64)
	peak := 0.0
	for b := range 64 {
		lo, hi := b*n/64, (b+1)*n/64
		sum := 0.0
		for i := lo; i < hi; i++ {
			v := float64(int16(binary.LittleEndian.Uint16(raw[2*i:])))
			sum += v * v
		}
		rms[b] = math.Sqrt(sum / float64(max(1, hi-lo)))
		peak = max(peak, rms[b])
	}
	out := make([]byte, 64)
	for i, v := range rms {
		if peak > 0 {
			out[i] = byte(math.Round(v / peak * 100))
		}
	}
	return out, nil
}
