package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Menu da mensagem (alt+. ou botão direito): só as ações que valem para
// aquela mensagem, numeradas. O mesmo menu flutuante pergunta "apagar
// para mim ou para todos" (alt+d).

type popOpt struct {
	label string
	run   func() tea.Cmd
}

type popMenu struct {
	title string
	opts  []popOpt
	sel   int
}

func (m *model) openMsgMenu() tea.Cmd {
	msg, ok := m.selectedMsg()
	if !ok || msg.Kind == "revoked" {
		return nil
	}
	var opts []popOpt
	add := func(label string, run func() tea.Cmd) { opts = append(opts, popOpt{label, run}) }
	add("Responder", m.startReply)
	if m.activeChat.Kind == "group" && !msg.FromMe {
		add("Responder em privado a "+firstName(msg.SenderName), func() tea.Cmd { return m.replyPrivately(msg) })
	}
	add("Reagir", func() tea.Cmd { m.reacting = true; m.layout(); return nil })
	if msg.Media != nil || msg.HasRaw && msg.Kind != "text" || firstURL(msg.Text) != "" || msg.Card != nil {
		add("Abrir", func() tea.Cmd { return m.openMedia(m.sel()) })
	}
	if strings.TrimSpace(msg.Text) != "" && msg.Kind != "audio" && msg.Kind != "sticker" {
		add("Copiar", m.copySelected)
	}
	if msg.FromMe && msg.Kind == "text" && time.Since(time.UnixMilli(msg.TS)) < 15*time.Minute {
		add("Editar", m.startEdit)
	}
	add("Encaminhar", m.startForward)
	if m.isPinned(msg.ID) {
		add("Desafixar", func() tea.Cmd { return m.openPinMenu(msg) })
	} else {
		add("Fixar…", func() tea.Cmd { return m.openPinMenu(msg) })
	}
	if msg.Starred {
		add("Desfavoritar", m.toggleStar)
	} else {
		add("Favoritar", m.toggleStar)
	}
	add("Dados da mensagem", func() tea.Cmd { return m.openMsgInfo(msg) })
	add("Apagar…", func() tea.Cmd { return m.openDeleteMenu() })
	m.popup = &popMenu{title: "Mensagem", opts: opts}
	return nil
}

// openDeleteMenu: alt+d pergunta para quem apagar. "Para todos" só nas
// suas e dentro do prazo do WhatsApp (~60 h).
func (m *model) openDeleteMenu() tea.Cmd {
	msg, ok := m.selectedMsg()
	if !ok || msg.Kind == "revoked" {
		return nil
	}
	opts := []popOpt{{"Apagar para mim", func() tea.Cmd { return m.deleteForMe(msg) }}}
	if msg.FromMe && time.Since(time.UnixMilli(msg.TS)) < 60*time.Hour {
		opts = append(opts, popOpt{"Apagar para todos", func() tea.Cmd {
			return m.call(ipc.Request{Op: ipc.OpRevoke, Chat: msg.Chat, Msg: msg.ID}, nil, nil)
		}})
	}
	opts = append(opts, popOpt{"Cancelar", func() tea.Cmd { return nil }})
	m.popup = &popMenu{title: "Apagar mensagem?", opts: opts}
	return nil
}

func (m *model) keyPopup(s string) tea.Cmd {
	p := m.popup
	run := func(i int) tea.Cmd {
		m.popup = nil
		return p.opts[i].run()
	}
	switch s {
	case "esc", "ctrl+c", "alt+.":
		m.popup = nil
	case "up":
		p.sel = max(0, p.sel-1)
	case "down":
		p.sel = min(len(p.opts)-1, p.sel+1)
	case "enter":
		return run(p.sel)
	default:
		if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(p.opts) {
			return run(int(s[0] - '1'))
		}
	}
	return nil
}

func (m *model) viewPopup() string {
	p := m.popup
	var lines []string
	for i, o := range p.opts {
		n := sText2.Render(fmt.Sprintf("%d", i+1))
		if i == p.sel {
			lines = append(lines, sGreen.Render("▸ ")+n+" "+sGreen.Bold(true).Render(o.label))
		} else {
			lines = append(lines, "  "+n+" "+sText.Render(o.label))
		}
	}
	lines = append(lines, "", sText2.Render("número ou enter · esc fecha"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).Padding(0, 1).
		Render(sBold.Render(ansi.Wrap(p.title, 44, " ")) + "\n\n" + strings.Join(lines, "\n"))
}

// popupY: o menu abre na altura da mensagem selecionada, sem sair da tela.
func (m *model) popupY(g geometry, h int) int {
	y := g.vpY
	if i := m.sel(); i >= 0 && i < len(m.spans) {
		y = g.vpY + max(0, m.spans[i][0]-m.vp.YOffset())
	}
	return max(1, min(y, m.h-h-2))
}

func firstName(s string) string {
	s = strings.TrimLeft(s, "~ ")
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return "o autor"
}

// ---------- responder em privado ----------

// replyPrivately abre a conversa individual com quem mandou e deixa a
// mensagem do grupo citada, como no app.
func (m *model) replyPrivately(msg ipc.Message) tea.Cmd {
	cmd := m.open(m.chatFor(msg.Sender, msg.SenderName))
	m.replyTo = &msg
	m.layout()
	return cmd
}

// replyRef: o ID da resposta para o daemon; de outra conversa vai como
// "conversa|id".
func (m *model) replyRef() string {
	if m.replyTo.Chat != "" && m.replyTo.Chat != m.active {
		return m.replyTo.Chat + "|" + m.replyTo.ID
	}
	return m.replyTo.ID
}

// ---------- apagar para mim, com desfazer ----------

type pendingDelete struct {
	msg ipc.Message
	idx int
}

type deleteCommitMsg struct{ id string }

// deleteForMe some com a mensagem na hora e só avisa o WhatsApp depois de
// 5 s; alt+z nesse meio-tempo desfaz.
func (m *model) deleteForMe(msg ipc.Message) tea.Cmd {
	commit := m.commitDelete() // um apagar pendente anterior vai agora
	for i, x := range m.msgs {
		if x.ID == msg.ID {
			m.msgs = append(m.msgs[:i:i], m.msgs[i+1:]...)
			m.pendingDel = &pendingDelete{msg: msg, idx: i}
			break
		}
	}
	m.msgSel = -1
	m.rerenderKeepingPlace()
	m.setFlash("mensagem apagada para você · alt+z desfaz")
	id := msg.ID
	return tea.Batch(commit, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return deleteCommitMsg{id} }))
}

func (m *model) commitDelete() tea.Cmd {
	p := m.pendingDel
	if p == nil {
		return nil
	}
	m.pendingDel = nil
	return m.call(ipc.Request{Op: ipc.OpDeleteMe, Chat: p.msg.Chat, Msg: p.msg.ID}, nil, nil)
}

func (m *model) onDeleteCommit(msg deleteCommitMsg) tea.Cmd {
	if m.pendingDel == nil || m.pendingDel.msg.ID != msg.id {
		return nil
	}
	return m.commitDelete()
}

// undoDelete (alt+z) devolve a mensagem ao lugar.
func (m *model) undoDelete() {
	p := m.pendingDel
	if p == nil {
		return
	}
	m.pendingDel = nil
	if p.msg.Chat == m.active {
		i := min(p.idx, len(m.msgs))
		m.msgs = append(m.msgs[:i], append([]ipc.Message{p.msg}, m.msgs[i:]...)...)
		m.rerenderKeepingPlace()
	}
	m.setFlash("apagar desfeito")
}

// ---------- favoritas ----------

type starredMsg struct{ hits []ipc.SearchHit }

func (m *model) loadStarred() tea.Cmd {
	var out []ipc.SearchHit
	return m.call(ipc.Request{Op: ipc.OpStarred}, &out, func() tea.Msg { return starredMsg{out} })
}

// toggleStar (alt+s) favorita ou desfavorita a mensagem selecionada; a
// estrela aparece quando o daemon confirma (EvUpdate).
func (m *model) toggleStar() tea.Cmd {
	msg, ok := m.selectedMsg()
	if !ok || msg.Kind == "revoked" {
		return nil
	}
	on, done := "1", "mensagem favoritada"
	if msg.Starred {
		on, done = "0", "mensagem desfavoritada"
	}
	return m.call(ipc.Request{Op: ipc.OpStar, Chat: msg.Chat, Msg: msg.ID, Text: on}, nil,
		func() tea.Msg { return flashMsg(done) })
}

// ---------- dados da mensagem ----------

type msgInfoMsg struct{ info *ipc.MsgInfo }

func (m *model) openMsgInfo(msg ipc.Message) tea.Cmd {
	var out ipc.MsgInfo
	return m.call(ipc.Request{Op: ipc.OpMsgInfo, Chat: msg.Chat, Msg: msg.ID}, &out,
		func() tea.Msg { return msgInfoMsg{&out} })
}

func whenLabel(ms int64) string {
	t := time.UnixMilli(ms)
	switch {
	case sameDay(t, time.Now()):
		return "hoje " + t.Format("15:04")
	case sameDay(t, time.Now().AddDate(0, 0, -1)):
		return "ontem " + t.Format("15:04")
	}
	return t.Format("02/01 15:04")
}

func (m *model) viewMsgInfo() string {
	in := m.msgInfo
	var lines []string
	section := func(title string, who []ipc.Who, emoji bool) {
		if len(who) == 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, sGreen.Bold(true).Render(fmt.Sprintf("%s (%d)", title, len(who))))
		for i, w := range who {
			if i == 12 {
				lines = append(lines, sText2.Render(fmt.Sprintf("… e mais %d", len(who)-i)))
				break
			}
			name := ansi.Truncate(w.Name, 26, "…")
			right := ""
			if emoji {
				right = w.Emoji
			} else if w.TS > 0 {
				right = sText2.Render(whenLabel(w.TS))
			}
			lines = append(lines, sText.Render(name)+strings.Repeat(" ", max(2, 28-ansi.StringWidth(name)))+right)
		}
	}
	section("Ouvida por", in.Played, false)
	section("Lida por", in.Read, false)
	section("Entregue a", in.Delivered, false)
	section("Reações", in.Reactions, true)
	if len(lines) == 0 {
		if in.FromMe {
			lines = append(lines, sText2.Render("ainda sem recibos vistos por aqui"))
		} else {
			lines = append(lines, sText2.Render("ninguém reagiu"))
		}
	}
	lines = append(lines, "", sText2.Render("qualquer tecla fecha"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).Padding(0, 1).
		Render(sBold.Render("Dados da mensagem") + "\n\n" + strings.Join(lines, "\n"))
}
