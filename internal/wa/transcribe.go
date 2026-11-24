package wa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/arthurhmarinho/zapterm/internal/config"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Transcrição local de áudio: o GStreamer converte para WAV 16 kHz mono e
// o whisper.cpp (whisper-cli) transcreve na sua máquina. Opcional: sem o
// programa ou o modelo, a opção explica o que falta.

var transcribeMu sync.Mutex // um por vez: o whisper usa todos os núcleos

// whisperSetup acha o programa e o modelo do [transcription].
func whisperSetup(t config.Transcription) (cmd, model string, err error) {
	cmd = t.Command
	if cmd == "" {
		for _, c := range []string{"whisper-cli", "whisper-cpp"} {
			if p, e := exec.LookPath(c); e == nil {
				cmd = p
				break
			}
		}
	} else if p, e := exec.LookPath(expandHome(cmd)); e == nil {
		cmd = p
	} else {
		cmd = ""
	}
	if cmd == "" {
		return "", "", errors.New("whisper-cli não encontrado: instale o whisper.cpp (veja [transcription] no zapterm config)")
	}
	model = expandHome(t.Model)
	if model == "" {
		// Sem modelo configurado: o maior ggml-*.bin da pasta padrão.
		dir, _ := paths.DataDir()
		found, _ := filepath.Glob(filepath.Join(dir, "whisper", "ggml-*.bin"))
		sort.Slice(found, func(i, j int) bool { return fileSize(found[i]) > fileSize(found[j]) })
		if len(found) > 0 {
			model = found[0]
		}
	}
	if model == "" || fileSize(model) == 0 {
		return "", "", errors.New("modelo do whisper não encontrado: baixe um ggml-*.bin para ~/.local/share/zapterm/whisper/")
	}
	return cmd, model, nil
}

// WhisperSetup diz o que a transcrição vai usar (ou o que falta), para o doctor.
func WhisperSetup(t config.Transcription) (cmd, model string, err error) { return whisperSetup(t) }

func fileSize(p string) int64 {
	if st, err := os.Stat(p); err == nil {
		return st.Size()
	}
	return 0
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// transcribe baixa (se precisar) o áudio, transcreve e guarda o texto.
func (d *Daemon) transcribe(ctx context.Context, chat, id string) (string, error) {
	m, _, err := d.st.Message(chat, id)
	if err != nil {
		return "", err
	}
	if m.Kind != "audio" {
		return "", errors.New("só dá para transcrever áudio")
	}
	if t := d.st.Transcripts(chat, []string{id})[id]; t != "" {
		return t, nil
	}
	cmd, model, err := whisperSetup(d.cfg.Transcription)
	if err != nil {
		return "", err
	}
	md, err := d.downloadMedia(ctx, chat, id)
	if err != nil {
		return "", fmt.Errorf("baixando o áudio: %w", err)
	}
	transcribeMu.Lock()
	defer transcribeMu.Unlock()
	tmp, err := os.MkdirTemp("", "zapterm-whisper-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	wav := filepath.Join(tmp, "audio.wav")
	if err := toWAV(ctx, md.Path, wav); err != nil {
		return "", err
	}
	lang := d.cfg.Transcription.Language
	if lang == "" {
		lang = "pt"
	}
	var stdout, stderr bytes.Buffer
	run := exec.CommandContext(ctx, cmd, "-m", model, "-f", wav, "-l", lang, "-nt", "-np")
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		return "", fmt.Errorf("whisper: %v %s", err, lastLine(stderr.String()))
	}
	text := cleanTranscript(stdout.String())
	if text == "" {
		return "", errors.New("o whisper não entendeu nada neste áudio")
	}
	if err := d.st.SetTranscript(chat, id, text); err != nil {
		return "", err
	}
	d.pushUpdate(chat, id)
	return text, nil
}

// toWAV converte qualquer áudio para o WAV que o whisper quer (16 kHz,
// mono, 16 bits), pelo GStreamer que o zapterm já usa para tocar áudio.
func toWAV(ctx context.Context, src, dst string) error {
	conv := exec.CommandContext(ctx, "gst-launch-1.0", "-q", "filesrc", "location="+src, "!", "decodebin",
		"!", "audioconvert", "!", "audioresample", "!", "audio/x-raw,rate=16000,channels=1,format=S16LE",
		"!", "wavenc", "!", "filesink", "location="+dst)
	if out, err := conv.CombinedOutput(); err != nil {
		return fmt.Errorf("convertendo o áudio (gst-launch-1.0): %v %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// cleanTranscript junta as linhas do whisper e tira marcações como
// [música] e (risos) quando são a linha inteira.
func cleanTranscript(s string) string {
	var parts []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || (strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]")) || (strings.HasPrefix(l, "(") && strings.HasSuffix(l, ")")) {
			continue
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, " ")
}

func lastLine(s string) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	return ls[len(ls)-1]
}

// fillTranscripts põe nos áudios a transcrição já feita.
func (d *Daemon) fillTranscripts(msgs []ipc.Message) {
	byChat := map[string][]string{}
	for _, m := range msgs {
		if m.Kind == "audio" {
			byChat[m.Chat] = append(byChat[m.Chat], m.ID)
		}
	}
	for chat, ids := range byChat {
		ts := d.st.Transcripts(chat, ids)
		for i := range msgs {
			if t, ok := ts[msgs[i].ID]; ok && msgs[i].Chat == chat {
				msgs[i].Transcript = t
			}
		}
	}
}
