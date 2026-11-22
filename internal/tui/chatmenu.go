package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Menu de um chat (alt+m ou botão direito na lista): fixar, arquivar,
// silenciar e marcar como lida, com as opções que fazem sentido para o
// estado atual dele.

type menuOpt struct {
	label, action, done string
}

type chatMenu struct {
	chat ipc.Chat
	opts []menuOpt
	sel  int
}

func (m *model) openChatMenu(c ipc.Chat) {
	var opts []menuOpt
	if c.Pinned {
		opts = append(opts, menuOpt{"Desafixar", "unpin", "desafixado"})
	} else {
		opts = append(opts, menuOpt{"Fixar no topo", "pin", "fixado"})
	}
	if c.Archived {
		opts = append(opts, menuOpt{"Desarquivar", "unarchive", "desarquivado"})
	} else {
		opts = append(opts, menuOpt{"Arquivar", "archive", "arquivado"})
	}
	if c.Muted {
		opts = append(opts, menuOpt{"Reativar notificações", "unmute", "notificações reativadas"})
	} else {
		opts = append(opts,
			menuOpt{"Silenciar por 8 horas", "mute8h", "silenciado por 8 horas"},
			menuOpt{"Silenciar por 1 semana", "mute1w", "silenciado por 1 semana"},
			menuOpt{"Silenciar sempre", "muteforever", "silenciado"})
	}
	if c.Unread > 0 {
		opts = append(opts, menuOpt{"Marcar como lida", "read", "marcado como lido"})
	} else if c.Kind != "channel" {
		opts = append(opts, menuOpt{"Marcar como não lida", "unread", "marcado como não lido"})
	}
	if c.Kind != "channel" {
		opts = append(opts, menuOpt{"Mensagens temporárias…", "ephemeral", ""})
	}
	if len(m.lists) > 0 && c.Kind != "channel" {
		opts = append(opts, menuOpt{"Listas…", "lists", ""})
	}
	switch {
	case c.Kind == "dm" && c.Blocked:
		opts = append(opts, menuOpt{"Desbloquear", "unblock", "desbloqueado"})
	case c.Kind == "dm":
		opts = append(opts, menuOpt{"Bloquear…", "block", "bloqueado"})
	case c.Kind == "group":
		opts = append(opts, menuOpt{"Sair do grupo…", "leave", "você saiu do grupo"})
	}
	if c.Kind != "channel" {
		opts = append(opts,
			menuOpt{"Limpar conversa…", "clear", "conversa limpa"},
			menuOpt{"Apagar conversa…", "delete", "conversa apagada"})
	} else {
		opts = append(opts, menuOpt{"Deixar de seguir…", "unfollow", "você deixou de seguir o canal"})
	}
	m.menu = &chatMenu{chat: c, opts: opts}
}

func (m *model) keyChatMenu(s string) tea.Cmd {
	mn := m.menu
	switch s {
	case "esc", "alt+m", "ctrl+c":
		m.menu = nil
	case "up":
		mn.sel = max(0, mn.sel-1)
	case "down":
		mn.sel = min(len(mn.opts)-1, mn.sel+1)
	case "enter":
		return m.runMenu(mn.sel)
	default:
		if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(mn.opts) {
			return m.runMenu(int(s[0] - '1'))
		}
		if s == "0" && len(mn.opts) >= 10 { // a décima opção
			return m.runMenu(9)
		}
	}
	return nil
}

func (m *model) runMenu(i int) tea.Cmd {
	mn := m.menu
	m.menu = nil
	o := mn.opts[i]
	chat := mn.chat
	switch o.action {
	case "lists":
		return m.openChatLists(chat.JID)
	case "ephemeral":
		opt := func(label, secs, done string) popOpt {
			return popOpt{label, func() tea.Cmd { return m.chatAction(chat, menuOpt{label, "ephemeral:" + secs, done}) }}
		}
		m.popup = &popMenu{title: "Mensagens temporárias", opts: []popOpt{
			opt("24 horas", "86400", "mensagens temporárias: 24 horas"),
			opt("7 dias", "604800", "mensagens temporárias: 7 dias"),
			opt("90 dias", "7776000", "mensagens temporárias: 90 dias"),
			opt("Desligar", "0", "mensagens temporárias desligadas"),
		}}
		return nil
	case "block", "leave", "clear", "delete", "unfollow":
		q := "Bloquear " + chat.Name + "? Ele não vai mais conseguir te mandar mensagem nem ligar."
		switch o.action {
		case "leave":
			q = "Sair de " + chat.Name + "?"
		case "clear":
			q = "Limpar a conversa com " + chat.Name + "? As mensagens somem aqui e no celular; as favoritas ficam."
		case "delete":
			q = "Apagar a conversa com " + chat.Name + "? Some aqui e no celular, com as mídias."
		case "unfollow":
			q = "Deixar de seguir " + chat.Name + "?"
		}
		m.popup = &popMenu{title: q, opts: []popOpt{
			{strings.TrimSuffix(o.label, "…"), func() tea.Cmd { return m.chatAction(chat, o) }},
			{"Cancelar", func() tea.Cmd { return nil }},
		}}
		return nil
	}
	return m.chatAction(chat, o)
}

// chatAction manda a ação do menu do chat ao daemon.
func (m *model) chatAction(chat ipc.Chat, o menuOpt) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		if err := cli.Call(ipc.Request{Op: ipc.OpChatAction, Chat: chat.JID, Text: o.action}, nil); err != nil {
			return errMsg{fmt.Errorf("%s: %w", strings.ToLower(o.label), err)}
		}
		return flashMsg(chat.Name + ": " + o.done)
	}
}

type flashMsg string

func (m *model) viewChatMenu() string {
	mn := m.menu
	var lines []string
	for i, o := range mn.opts {
		n := sText2.Render(fmt.Sprintf("%d", (i+1)%10))
		if i == mn.sel {
			lines = append(lines, sGreen.Render("▸ ")+n+" "+sGreen.Bold(true).Render(o.label))
		} else {
			lines = append(lines, "  "+n+" "+sText.Render(o.label))
		}
	}
	lines = append(lines, "", sText2.Render("número ou enter · esc fecha"))
	title := sBold.Render(ansi.Truncate(mn.chat.Name, 30, "…"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).Padding(0, 1).
		Render(title + "\n\n" + strings.Join(lines, "\n"))
}
