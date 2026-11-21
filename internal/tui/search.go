package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Busca no texto das mensagens: com 3+ letras na busca, depois de uma
// pausa na digitação, o daemon procura (sem acento/maiúscula) e os
// resultados entram na lista depois dos chats. Abrir um resultado pula para
// a mensagem: a conversa carrega em volta dela e as mais novas vêm ao
// descer.

const (
	hitMarker   = "busca:" // prefixo do JID das entradas de resultado
	searchDelay = 350 * time.Millisecond
	aroundPage  = 50
)

type (
	searchTickMsg struct{ query string }
	hitsMsg       struct {
		query string
		hits  []ipc.SearchHit
	}
	// aroundMsg: mensagens em volta de um resultado da busca.
	aroundMsg struct {
		chat, target string
		msgs         []ipc.Message
		moreNewer    bool
	}
	newerMsg struct {
		chat string
		msgs []ipc.Message
	}
)

// scheduleSearch espera a pausa na digitação antes de procurar.
func (m *model) scheduleSearch() tea.Cmd {
	q := m.searchQuery()
	if len([]rune(q)) < 3 || phoneRe.MatchString(q) {
		m.hits, m.hitsQuery = nil, ""
		return nil
	}
	return tea.Tick(searchDelay, func(time.Time) tea.Msg { return searchTickMsg{q} })
}

func (m *model) runSearch(q string) tea.Cmd {
	if q != m.searchQuery() || q == m.hitsQuery {
		return nil // o texto mudou de novo, ou já procurado
	}
	var out []ipc.SearchHit
	return m.call(ipc.Request{Op: ipc.OpSearch, Text: q, Limit: 30}, &out,
		func() tea.Msg { return hitsMsg{query: q, hits: out} })
}

// hitEntries vira os resultados em entradas da lista.
func (m *model) hitEntries() []ipc.Chat {
	var out []ipc.Chat
	icon := icons.search
	if m.searchQuery() == "" && m.tab == tabStarred {
		icon = "★"
	}
	for i, h := range m.hits {
		text := plainText(h.Message.Text)
		if h.Message.FromMe {
			text = "você: " + text
		} else if h.Chat.Kind == "group" && h.Message.SenderName != "" {
			text = h.Message.SenderName + ": " + text
		}
		out = append(out, ipc.Chat{
			JID: fmt.Sprintf("%s%d", hitMarker, i), Name: icon + " " + h.Chat.Name, Kind: h.Chat.Kind,
			LastText: text, LastTS: h.Message.TS, LastStatus: -1,
		})
	}
	return out
}

// openHit abre o chat do resultado já posicionado na mensagem.
func (m *model) openHit(entry ipc.Chat) tea.Cmd {
	var i int
	fmt.Sscanf(strings.TrimPrefix(entry.JID, hitMarker), "%d", &i)
	if i < 0 || i >= len(m.hits) {
		return nil
	}
	h := m.hits[i]
	m.focus = paneChat
	if m.searching {
		m.stopSearch(true)
	}
	c := h.Chat
	if cur, ok := m.chatByJID(c.JID); ok {
		c = cur
	}
	stop := m.stopTyping()
	m.active, m.activeChat = c.JID, c
	m.msgs, m.msgSel, m.replyTo, m.attach = nil, -1, nil, nil
	m.mention, m.mentioned = mentionState{}, map[string]string{}
	m.loadingOld, m.noMoreOld, m.gap = false, false, false
	m.layout()
	m.refreshMessages(true)
	return tea.Batch(m.loadAround(c.JID, h.Message.ID, h.Message.TS), stop, m.syncViewing())
}

// loadAround carrega as mensagens em volta de target (para pular até ela).
func (m *model) loadAround(chat, target string, ts int64) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		var before, after []ipc.Message
		if err := cli.Call(ipc.Request{Op: ipc.OpMessages, Chat: chat, Limit: aroundPage, Before: ts + 1}, &before); err != nil {
			return errMsg{err}
		}
		if err := cli.Call(ipc.Request{Op: ipc.OpMessages, Chat: chat, Limit: aroundPage, After: ts}, &after); err != nil {
			return errMsg{err}
		}
		return aroundMsg{chat: chat, target: target, msgs: append(before, after...), moreNewer: len(after) == aroundPage}
	}
}

func (m *model) onAround(msg aroundMsg) tea.Cmd {
	if msg.chat != m.active {
		return nil
	}
	m.msgs, m.gap, m.noMoreOld = msg.msgs, msg.moreNewer, false
	for i, x := range m.msgs {
		if x.ID == msg.target {
			m.msgSel = i
		}
	}
	m.refreshMessages(false)
	m.ensureSelVisible()
	return m.autoStickers(msg.msgs)
}

// maybeLoadNewer: depois de pular para um resultado, descer até o fim do
// que está carregado busca as mais novas.
func (m *model) maybeLoadNewer() tea.Cmd {
	if !m.gap || m.loadingNew || len(m.msgs) == 0 || !(m.vp.AtBottom() || m.sel() == len(m.msgs)-1) {
		return nil
	}
	m.loadingNew = true
	chat, after := m.active, m.msgs[len(m.msgs)-1].TS
	var out []ipc.Message
	return m.call(ipc.Request{Op: ipc.OpMessages, Chat: chat, Limit: pageSize, After: after}, &out,
		func() tea.Msg { return newerMsg{chat: chat, msgs: out} })
}

func (m *model) onNewer(msg newerMsg) tea.Cmd {
	m.loadingNew = false
	if msg.chat != m.active {
		return nil
	}
	m.gap = len(msg.msgs) == pageSize
	sel := m.sel()
	m.msgs = append(m.msgs, msg.msgs...)
	m.msgSel = sel
	m.rerenderKeepingPlace()
	return m.autoStickers(msg.msgs)
}
