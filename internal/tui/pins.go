package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Mensagens fixadas: uma faixa de uma linha entre o cabeçalho e a conversa.
// alt+p (ou clique na faixa) pula para a fixada e, com mais de uma, vai
// alternando entre elas.

type pinsMsg struct {
	chat string
	msgs []ipc.Message
}

func (m *model) loadPins(chat string) tea.Cmd {
	if chat == "" {
		return nil
	}
	var out []ipc.Message
	return m.call(ipc.Request{Op: ipc.OpPins, Chat: chat}, &out, func() tea.Msg { return pinsMsg{chat, out} })
}

func (m *model) onPins(msg pinsMsg) {
	if msg.chat != m.active {
		return
	}
	had := len(m.pins) > 0
	m.pins = msg.msgs
	m.pinIdx = min(m.pinIdx, max(0, len(m.pins)-1))
	if had != (len(m.pins) > 0) {
		m.layout() // a faixa entrou ou saiu: a conversa muda de altura
	}
}

// onPinsEvent: o daemon avisou que mudaram as fixadas de um chat.
func (m *model) onPinsEvent(data json.RawMessage) tea.Cmd {
	var chat string
	if json.Unmarshal(data, &chat) != nil || chat != m.active {
		return nil
	}
	return m.loadPins(chat)
}

func (m *model) pinBarH() int {
	if len(m.pins) == 0 || m.profileOpen {
		return 0
	}
	return 1
}

func (m *model) isPinned(id string) bool {
	for _, p := range m.pins {
		if p.ID == id {
			return true
		}
	}
	return false
}

func (m *model) viewPinBar(w int) string {
	p := m.pins[m.pinIdx]
	text := oneLine(plainText(mediaLabel(p.Text)))
	if p.Card != nil && p.Card.Title != "" {
		text = p.Card.Title
	}
	who := p.SenderName
	if p.FromMe {
		who = "você"
	}
	count := ""
	if len(m.pins) > 1 {
		count = fmt.Sprintf("%d/%d · ", m.pinIdx+1, len(m.pins))
	}
	right := sText2.Render(count + "alt+p")
	left := "  " + sGreen.Render(icons.pin) + " " + sText2.Render(who+": ")
	room := w - ansi.StringWidth(left) - ansi.StringWidth(right) - 3
	line := left + sText.Render(ansi.Truncate(text, max(4, room), "…"))
	gap := max(1, w-ansi.StringWidth(line)-ansi.StringWidth(right)-1)
	return paint(line+strings.Repeat(" ", gap)+right, cBgPanel, w)
}

// jumpPin pula para a fixada atual e prepara a próxima.
func (m *model) jumpPin() tea.Cmd {
	if len(m.pins) == 0 {
		m.setFlash("nenhuma mensagem fixada nesta conversa")
		return nil
	}
	p := m.pins[m.pinIdx]
	m.pinIdx = (m.pinIdx + 1) % len(m.pins)
	for i, x := range m.msgs {
		if x.ID == p.ID {
			m.msgSel = i
			m.refreshMessages(false)
			m.ensureSelVisible()
			return nil
		}
	}
	return m.loadAround(m.active, p.ID, p.TS)
}

// openPinMenu: fixar por quanto tempo (como no app) ou desafixar.
func (m *model) openPinMenu(msg ipc.Message) tea.Cmd {
	pin := func(secs int, done string) func() tea.Cmd {
		return func() tea.Cmd {
			return m.call(ipc.Request{Op: ipc.OpPin, Chat: msg.Chat, Msg: msg.ID, Seconds: secs}, nil,
				func() tea.Msg { return flashMsg(done) })
		}
	}
	if m.isPinned(msg.ID) {
		return pin(0, "mensagem desafixada")()
	}
	m.popup = &popMenu{title: "Fixar por quanto tempo?", opts: []popOpt{
		{"24 horas", pin(24*3600, "fixada por 24 horas")},
		{"7 dias", pin(7*24*3600, "fixada por 7 dias")},
		{"30 dias", pin(30*24*3600, "fixada por 30 dias")},
		{"Cancelar", func() tea.Cmd { return nil }},
	}}
	return nil
}
