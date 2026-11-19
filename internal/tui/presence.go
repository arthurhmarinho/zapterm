package tui

import (
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// O WhatsApp manda "digitando" e repete enquanto a pessoa digita; se o
// "parou" se perder, o aviso some sozinho depois disso.
const typingTTL = 8 * time.Second

type presenceState struct {
	who       string
	typing    bool
	recording bool
	until     time.Time
	online    *bool
	lastSeen  int64
}

type presenceTickMsg struct{}

func (m *model) onPresence(f ipc.Frame) tea.Cmd {
	var p ipc.Presence
	if json.Unmarshal(f.Data, &p) != nil {
		return nil
	}
	st := m.presence[p.Chat]
	if st == nil {
		st = &presenceState{}
		m.presence[p.Chat] = st
	}
	if p.Online != nil {
		st.online = p.Online
		if p.LastSeen > 0 {
			st.lastSeen = p.LastSeen
		}
		if *p.Online {
			st.lastSeen = 0
		}
		return nil
	}
	st.who, st.typing, st.recording = p.Who, p.Typing, p.Recording
	st.until = time.Now().Add(typingTTL)
	// Redesenha quando o aviso vencer.
	return tea.Tick(typingTTL+100*time.Millisecond, func(time.Time) tea.Msg { return presenceTickMsg{} })
}

// typingText: "digitando…", "Fulano está digitando…" (cabeçalho de grupo) ou
// "gravando áudio…"; "" se ninguém está.
func (m *model) typingText(chat string, header bool) string {
	st := m.presence[chat]
	if st == nil || time.Now().After(st.until) || (!st.typing && !st.recording) {
		return ""
	}
	what := "digitando…"
	if st.recording {
		what = "gravando áudio…"
	}
	if st.who != "" {
		if header {
			return st.who + " está " + what
		}
		return st.who + ": " + what
	}
	return what
}

// ---------- o seu "digitando" ----------

// noteTyping avisa o chat que você está digitando, no máximo a cada 4s
// (o aviso do outro lado dura uns segundos), e "parou" quando o campo esvazia.
func (m *model) noteTyping() tea.Cmd {
	if m.active == "" {
		return nil
	}
	if m.input.Value() == "" {
		return m.stopTyping()
	}
	if time.Since(m.typingSentAt) < 4*time.Second {
		return nil
	}
	m.typingSentAt, m.typingChat = time.Now(), m.active
	return m.call(ipc.Request{Op: ipc.OpTyping, Chat: m.active, Text: "composing"}, nil, nil)
}

func (m *model) stopTyping() tea.Cmd {
	if m.typingChat == "" {
		return nil
	}
	chat := m.typingChat
	m.typingChat, m.typingSentAt = "", time.Time{}
	return m.call(ipc.Request{Op: ipc.OpTyping, Chat: chat, Text: "paused"}, nil, nil)
}
