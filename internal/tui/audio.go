package tui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Áudio pelo GStreamer (gst-launch-1.0): o playbin toca qualquer formato
// (a voz do WhatsApp é OGG/Opus) pelo PipeWire, e a gravação usa o
// microfone padrão (pulsesrc, via pipewire-pulse) codificando em Opus,
// que é o que o app espera numa mensagem de voz.

type playback struct {
	id      string
	chat    string // conversa do áudio (o mini player mostra fora dela)
	name    string // quem mandou
	cmd     *exec.Cmd
	started time.Time
	total   int     // segundos
	ctl     *mpvCtl // nil: tocando pelo gst-launch (sem pausa nem velocidade)
	pos     float64
	paused  bool
	stopped bool // parado por nós: não emenda o próximo
}

// recReview: gravação terminada esperando você ouvir e decidir.
type recReview struct {
	path    string
	seconds int
}

type recording struct {
	cmd     *exec.Cmd
	path    string
	started time.Time
	done    chan error // processo terminou (por conta própria ou por nós)
}

type (
	playTickMsg struct{}
	playPosMsg  struct {
		id     string
		pos    float64
		paused bool
	}
	playDoneMsg struct{ id string }
	recTickMsg  struct{}
	recDoneMsg  struct {
		path    string
		seconds int
		send    bool
		review  bool
		err     error
	}
)

var durRe = regexp.MustCompile(`(\d+):(\d\d)\]?$`)

// audioSeconds tira a duração do texto "[áudio 0:12]".
func audioSeconds(text string) int {
	m := durRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	mm, _ := strconv.Atoi(m[1])
	ss, _ := strconv.Atoi(m[2])
	return mm*60 + ss
}

func fmtDur(s int) string { return fmt.Sprintf("%d:%02d", s/60, s%60) }

// ---------- tocar ----------

// toggleAudio toca o áudio da mensagem (baixando antes, se preciso) ou
// para, se ele já estiver tocando.
func (m *model) toggleAudio(i int) tea.Cmd {
	msg := m.msgs[i]
	if m.play != nil && m.play.id == msg.ID {
		if m.play.ctl != nil {
			m.togglePause()
			return nil
		}
		m.stopPlay()
		m.rerenderKeepingPlace()
		return nil
	}
	if msg.Media == nil {
		if !msg.HasRaw {
			m.setFlash("áudio de mensagem antiga (do pareamento) não pode ser baixado")
			return nil
		}
		m.setFlash("baixando áudio…")
		chat, id := m.active, msg.ID
		var md ipc.Media
		return m.call(ipc.Request{Op: ipc.OpMedia, Chat: chat, Msg: id}, &md,
			func() tea.Msg { return mediaMsg{chat: chat, id: id, media: &md, play: true} })
	}
	return m.startPlay(msg.Media.Path, msg.ID, audioSeconds(msg.Text))
}

// speeds: alt+v alterna entre elas; a escolhida vale para os próximos.
var speeds = []float64{1, 1.5, 2}

func (m *model) cycleSpeed() {
	i := 0
	for j, s := range speeds {
		if s == m.audioSpeed {
			i = j
		}
	}
	m.audioSpeed = speeds[(i+1)%len(speeds)]
	if p := m.play; p != nil && p.ctl != nil {
		p.ctl.call("set_property", "speed", m.audioSpeed)
	}
	m.setFlash("velocidade do áudio: " + speedLabel(m.audioSpeed))
	m.rerenderKeepingPlace()
}

func speedLabel(s float64) string {
	return strings.Replace(strconv.FormatFloat(s, 'f', -1, 64), ".", ",", 1) + "×"
}

func (m *model) togglePause() {
	p := m.play
	if p == nil || p.ctl == nil {
		return
	}
	p.paused = !p.paused
	p.ctl.call("set_property", "pause", p.paused)
	m.rerenderKeepingPlace()
}

// seekAudio anda secs segundos (negativo volta) no áudio tocando.
func (m *model) seekAudio(secs float64) {
	p := m.play
	if p == nil || p.ctl == nil {
		return
	}
	p.ctl.call("seek", secs, "relative")
	p.pos = max(0, p.pos+secs)
	if p.total > 0 {
		p.pos = min(p.pos, float64(p.total))
	}
	m.rerenderKeepingPlace()
}

func (m *model) startPlay(path, id string, total int) tea.Cmd {
	m.stopPlay()
	if m.audioSpeed == 0 {
		m.audioSpeed = 1
	}
	// mpv, se houver: pausa, velocidade e avançar/voltar. Sem ele, o
	// gst-launch só toca do começo ao fim.
	var cmd *exec.Cmd
	sock := ""
	if hasMPV() {
		sock = mpvSocket()
		os.Remove(sock)
		cmd = mpvCommand(path, sock, m.audioSpeed)
	} else {
		u := url.URL{Scheme: "file", Path: path}
		cmd = exec.Command("gst-launch-1.0", "-q", "playbin", "uri="+u.String())
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		m.setFlash("não deu para tocar: " + err.Error())
		return nil
	}
	p := &playback{id: id, chat: m.active, name: m.audioSender(id), cmd: cmd, started: time.Now(), total: total}
	if sock != "" {
		if ctl, err := dialMPV(sock); err == nil {
			p.ctl = ctl
		}
	}
	m.play = p
	m.flash = ""
	m.rerenderKeepingPlace()
	return tea.Batch(
		func() tea.Msg { cmd.Wait(); return playDoneMsg{id} },
		playTick(),
	)
}

func (m *model) stopPlay() {
	if m.play == nil {
		return
	}
	m.play.stopped = true
	m.play.ctl.close()
	syscall.Kill(-m.play.cmd.Process.Pid, syscall.SIGTERM)
	m.play = nil
}

// audioSender: quem mandou o áudio (para o mini player).
func (m *model) audioSender(id string) string {
	for _, x := range m.msgs {
		if x.ID == id {
			if x.FromMe {
				return "você"
			}
			if m.activeChat.Kind == "group" {
				return x.SenderName
			}
			return m.activeChat.Name
		}
	}
	return "áudio"
}

func playTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return playTickMsg{} })
}

// playState devolve, para a mensagem tocando, o tempo decorrido.
func (m *model) playState() (id string, elapsed int) {
	if m.play == nil {
		return "", 0
	}
	e := int(time.Since(m.play.started).Seconds())
	if m.play.ctl != nil {
		e = int(m.play.pos)
	}
	if m.play.total > 0 {
		e = min(e, m.play.total)
	}
	return m.play.id, e
}

// ---------- gravar ----------

func (m *model) startRec() tea.Cmd {
	if m.active == "" {
		m.setFlash("abra um chat antes de gravar")
		return nil
	}
	base, err := paths.CacheDir()
	if err != nil {
		m.setFlash(err.Error())
		return nil
	}
	dir := filepath.Join(base, "rec")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.setFlash(err.Error())
		return nil
	}
	path := filepath.Join(dir, time.Now().Format("20060102-150405")+".ogg")
	// -e: no SIGINT o GStreamer fecha o arquivo direito (manda EOS).
	// ZAPTERM_REC_SRC troca o microfone por outra fonte (os testes usam
	// audiotestsrc para não gravar ninguém).
	src := "pulsesrc"
	if v := os.Getenv("ZAPTERM_REC_SRC"); v != "" {
		src = v
	}
	cmd := exec.Command("gst-launch-1.0", "-q", "-e",
		src, "!", "audioconvert", "!", "audioresample", "!",
		"audio/x-raw,rate=48000,channels=1", "!", "opusenc", "bitrate=32000", "!",
		"oggmux", "!", "filesink", "location="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		m.setFlash("não deu para gravar: " + err.Error())
		return nil
	}
	m.stopPlay()
	r := &recording{cmd: cmd, path: path, started: time.Now(), done: make(chan error, 1)}
	go func() { r.done <- cmd.Wait() }()
	m.rec = r
	m.picking, m.attach = false, nil
	m.layout()
	// Do outro lado aparece "gravando áudio…", como no app.
	m.typingChat, m.typingSentAt = m.active, time.Now()
	return tea.Batch(recTick(), m.call(ipc.Request{Op: ipc.OpTyping, Chat: m.active, Text: "recording"}, nil, nil))
}

// finishRec para a gravação; send decide se ela vai ou é descartada.
func (m *model) finishRec(send bool) tea.Cmd {
	return m.endRec(send, false)
}

// reviewRec para a gravação e toca o que foi gravado antes de enviar.
func (m *model) reviewRec() tea.Cmd { return m.endRec(false, true) }

func (m *model) endRec(send, review bool) tea.Cmd {
	r := m.rec
	if r == nil {
		return nil
	}
	m.rec = nil
	m.layout()
	secs := int(time.Since(r.started).Round(time.Second).Seconds())
	return tea.Batch(m.stopTyping(), r.stop(send || review, secs, review))
}

// keyReview: a gravação parada, esperando decisão.
func (m *model) keyReview(s string) tea.Cmd {
	rv := m.recReview
	switch s {
	case "enter", "ctrl+t":
		m.stopPlay()
		m.recReview = nil
		m.layout()
		return func() tea.Msg { return recDoneMsg{path: rv.path, seconds: rv.seconds, send: true} }
	case "esc", "ctrl+c":
		m.stopPlay()
		os.Remove(rv.path)
		m.recReview = nil
		m.layout()
		m.setFlash("áudio descartado")
	case "space", "ctrl+o":
		if m.play != nil && m.play.id == "rec-review" && m.play.ctl != nil {
			m.togglePause()
			return nil
		}
		return m.startPlay(rv.path, "rec-review", rv.seconds)
	}
	return nil
}

// stop para o gravador fora do loop de UI e diz o que fazer com o arquivo.
func (r *recording) stop(send bool, secs int, review bool) tea.Cmd {
	return func() tea.Msg {
		// O gst-launch só trata o SIGINT (fechando o arquivo) depois que o
		// pipeline está rodando; antes disso o sinal mata o processo e a
		// gravação some. Espera o cabeçalho do OGG aparecer no disco (sinal
		// de que está rodando), por até 3s, e só então para.
		for wait := 0; wait < 60; wait++ {
			if st, err := os.Stat(r.path); err == nil && st.Size() > 0 {
				break
			}
			select {
			case <-r.done:
				return recDoneMsg{err: fmt.Errorf("o gravador parou sozinho (microfone?)")}
			case <-time.After(50 * time.Millisecond):
			}
		}
		syscall.Kill(-r.cmd.Process.Pid, syscall.SIGINT)
		var err error
		select {
		case err = <-r.done:
		case <-time.After(3 * time.Second):
			syscall.Kill(-r.cmd.Process.Pid, syscall.SIGKILL)
			err = fmt.Errorf("o gravador não fechou o arquivo")
		}
		if !send {
			os.Remove(r.path)
			return recDoneMsg{}
		}
		if st, serr := os.Stat(r.path); serr != nil || st.Size() < 200 {
			return recDoneMsg{err: fmt.Errorf("gravação vazia (microfone?): %v", err)}
		}
		return recDoneMsg{path: r.path, seconds: secs, send: !review, review: review}
	}
}

func recTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return recTickMsg{} })
}

func (m *model) onAudioMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case playTickMsg:
		p := m.play
		if p == nil {
			return nil, true
		}
		if p.ctl != nil {
			// A posição vem do mpv, fora do loop de UI.
			id, ctl := p.id, p.ctl
			return tea.Batch(playTick(), func() tea.Msg {
				pos, paused := ctl.state()
				return playPosMsg{id: id, pos: pos, paused: paused}
			}), true
		}
		m.rerenderKeepingPlace()
		return playTick(), true
	case playPosMsg:
		if p := m.play; p != nil && p.id == msg.id {
			p.pos, p.paused = msg.pos, msg.paused
			m.rerenderKeepingPlace()
		}
		return nil, true
	case playDoneMsg:
		p := m.play
		if p == nil || p.id != msg.id {
			return nil, true
		}
		p.ctl.close()
		m.play = nil
		m.rerenderKeepingPlace()
		// Acabou sozinho: emenda o áudio seguinte da conversa, como no app.
		if p.chat == m.active {
			for i, x := range m.msgs {
				if x.ID == p.id && i+1 < len(m.msgs) && m.msgs[i+1].Kind == "audio" {
					return m.toggleAudio(i + 1), true
				}
			}
		}
		return nil, true
	case recTickMsg:
		if m.rec == nil {
			return nil, true
		}
		// O gravador morreu sozinho (sem microfone, por exemplo)?
		select {
		case err := <-m.rec.done:
			os.Remove(m.rec.path)
			m.rec = nil
			m.layout()
			m.setFlash(fmt.Sprintf("a gravação parou: %v", err))
			return nil, true
		default:
		}
		return recTick(), true
	case recDoneMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error())
			return nil, true
		}
		if msg.review {
			m.recReview = &recReview{path: msg.path, seconds: msg.seconds}
			m.layout()
			return m.startPlay(msg.path, "rec-review", msg.seconds), true
		}
		if !msg.send {
			return nil, true
		}
		req := ipc.Request{Op: ipc.OpSendAudio, Chat: m.active, Path: msg.path, Seconds: msg.seconds}
		if m.replyTo != nil {
			req.ReplyTo = m.replyRef()
			m.replyTo = nil
			m.layout()
		}
		m.msgSel = -1
		m.setFlash("enviando áudio…")
		return m.send(req), true
	}
	return nil, false
}

// cleanupAudio encerra player e gravador ao sair da TUI.
func (m *model) cleanupAudio() {
	m.stopPlay()
	if m.recReview != nil {
		os.Remove(m.recReview.path)
	}
	if m.rec != nil {
		syscall.Kill(-m.rec.cmd.Process.Pid, syscall.SIGKILL)
		os.Remove(m.rec.path)
	}
}
