package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Seleção de várias mensagens: shift+↑/↓ estende a partir da atual, como
// num editor; seta sem shift desfaz. Com seleção, alt+c copia todas
// ("[14:03] Fulano: texto"), alt+w encaminha todas e alt+d apaga para você.

// extendSel move a ponta da seleção (d = -1 sobe, +1 desce).
func (m *model) extendSel(d int) tea.Cmd {
	if len(m.msgs) == 0 {
		return nil
	}
	if m.selAnchor < 0 {
		m.selAnchor = m.sel()
	}
	m.setSel(m.sel() + d)
	return m.maybeLoadOlder()
}

// selRange devolve o trecho selecionado (de, até), ok=false sem seleção.
func (m *model) selRange() (int, int, bool) {
	if m.selAnchor < 0 || m.selAnchor >= len(m.msgs) {
		return 0, 0, false
	}
	a, b := m.selAnchor, m.sel()
	if a > b {
		a, b = b, a
	}
	return a, b, true
}

func (m *model) clearSelRange() {
	if m.selAnchor >= 0 {
		m.selAnchor = -1
		m.rerenderKeepingPlace()
	}
}

// selectedMsgs: as mensagens da seleção, ou só a do cursor.
func (m *model) selectedMsgs() []ipc.Message {
	if a, b, ok := m.selRange(); ok {
		var out []ipc.Message
		for _, x := range m.msgs[a : b+1] {
			if x.Kind != "revoked" {
				out = append(out, x)
			}
		}
		return out
	}
	if msg, ok := m.selectedMsg(); ok && msg.Kind != "revoked" {
		return []ipc.Message{msg}
	}
	return nil
}

// copyRange copia a seleção como texto corrido, bom para colar em notas.
func (m *model) copyRange() tea.Cmd {
	var lines []string
	for _, x := range m.selectedMsgs() {
		who := x.SenderName
		if x.FromMe {
			who = "você"
		}
		text := x.Text
		if x.Kind != "text" {
			text = mediaLabel(text)
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", time.UnixMilli(x.TS).Format("02/01 15:04"), who, plainText(text)))
	}
	if err := copyText(strings.Join(lines, "\n")); err != nil {
		m.setFlash(err.Error())
		return nil
	}
	m.clearSelRange()
	m.setFlash(fmt.Sprintf("%d mensagens copiadas", len(lines)))
	return nil
}

// deleteRange: apagar várias só para você (com confirmação).
func (m *model) deleteRange() tea.Cmd {
	msgs := m.selectedMsgs()
	m.popup = &popMenu{title: fmt.Sprintf("Apagar %d mensagens?", len(msgs)), opts: []popOpt{
		{"Apagar para mim", func() tea.Cmd {
			ids := map[string]bool{}
			var cmds []tea.Cmd
			for _, x := range msgs {
				ids[x.ID] = true
				cmds = append(cmds, m.call(ipc.Request{Op: ipc.OpDeleteMe, Chat: x.Chat, Msg: x.ID}, nil, nil))
			}
			var keep []ipc.Message
			for _, x := range m.msgs {
				if !ids[x.ID] {
					keep = append(keep, x)
				}
			}
			m.msgs, m.selAnchor, m.msgSel = keep, -1, -1
			m.rerenderKeepingPlace()
			m.setFlash(fmt.Sprintf("%d mensagens apagadas para você", len(msgs)))
			return tea.Batch(cmds...)
		}},
		{"Cancelar", func() tea.Cmd { return nil }},
	}}
	return nil
}

// ---------- encaminhar para várias conversas ----------

const maxForwardTargets = 5

func (m *model) startForward() tea.Cmd {
	msgs := m.selectedMsgs()
	if len(msgs) == 0 {
		return nil
	}
	m.forwarding, m.fwdTargets = msgs, nil
	m.startSearch()
	m.setFlash("enter marca até 5 conversas · alt+w envia")
	return nil
}

// toggleTarget marca ou desmarca a conversa como destino.
func (m *model) toggleTarget(c ipc.Chat) {
	for i, t := range m.fwdTargets {
		if t.JID == c.JID {
			m.fwdTargets = append(m.fwdTargets[:i], m.fwdTargets[i+1:]...)
			return
		}
	}
	if len(m.fwdTargets) >= maxForwardTargets {
		m.setFlash("no máximo 5 conversas, como no app")
		return
	}
	m.fwdTargets = append(m.fwdTargets, c)
}

// sendForward encaminha tudo para os destinos marcados (ou, sem marcados,
// para a conversa sob o cursor).
func (m *model) sendForward() tea.Cmd {
	targets := m.fwdTargets
	if len(targets) == 0 {
		if c, ok := m.selected(); ok && !strings.HasPrefix(c.JID, hitMarker) && !strings.HasPrefix(c.JID, newChatMarker) {
			targets = []ipc.Chat{c}
		}
	}
	msgs := m.forwarding
	if len(targets) == 0 || len(msgs) == 0 {
		return nil
	}
	m.forwarding, m.fwdTargets = nil, nil
	m.stopSearch(true)
	m.clearSelRange()
	var names []string
	for _, t := range targets {
		names = append(names, t.Name)
	}
	m.setFlash("encaminhando para " + strings.Join(names, ", ") + "…")
	cli := m.cli
	return func() tea.Msg {
		for _, t := range targets {
			for _, x := range msgs {
				if err := cli.Call(ipc.Request{Op: ipc.OpForward, Chat: t.JID, SrcChat: x.Chat, Msg: x.ID}, nil); err != nil {
					return errMsg{fmt.Errorf("encaminhando para %s: %w", t.Name, err)}
				}
			}
		}
		n := "mensagem encaminhada"
		if len(msgs) > 1 {
			n = fmt.Sprintf("%d mensagens encaminhadas", len(msgs))
		}
		return flashMsg(fmt.Sprintf("%s para %d conversa(s)", n, len(targets)))
	}
}

func (m *model) forwardHelp() string {
	what := "1 mensagem"
	if n := len(m.forwarding); n > 1 {
		what = fmt.Sprintf("%d mensagens", n)
	}
	h := "encaminhar " + what + ": enter marca (até 5) · alt+w envia · esc cancela"
	if len(m.fwdTargets) > 0 {
		var names []string
		for _, t := range m.fwdTargets {
			names = append(names, t.Name)
		}
		h = fmt.Sprintf("para: %s (%d/5) · ", strings.Join(names, ", "), len(m.fwdTargets)) + h
	}
	return h
}
