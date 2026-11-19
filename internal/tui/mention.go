package tui

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Autocompletar de menções: num grupo, "@" seguido de letras no fim do
// texto abre a lista de participantes filtrada; tab ou enter escolhe e
// insere "@Nome ". Na hora de enviar, cada "@Nome" escolhido vira
// "@<usuário>" e o JID vai na lista de marcados, que é como o WhatsApp
// marca (o celular troca de volta pelo nome).

const mentionShown = 6

var mentionTailRe = regexp.MustCompile(`(?:^|\s)@([^\s@]*)$`)

type mentionState struct {
	open      bool
	query     string
	list      []ipc.Member
	sel       int
	dismissed string // consulta fechada com esc (não reabre até mudar)
}

type membersMsg struct {
	chat string
	list []ipc.Member
}

// updateMention reavalia o "@" depois de cada mudança no texto.
func (m *model) updateMention() tea.Cmd {
	ms := &m.mention
	if m.activeChat.Kind != "group" {
		ms.open = false
		return nil
	}
	sub := mentionTailRe.FindStringSubmatch(m.input.Value())
	if sub == nil || sub[1] == ms.dismissed && ms.dismissed != "" {
		ms.open = false
		if sub == nil {
			ms.dismissed = ""
		}
		m.layout()
		return nil
	}
	ms.query = sub[1]
	members, loaded := m.members[m.active]
	if !loaded {
		chat := m.active
		var out []ipc.Member
		m.members[chat] = nil // marca como pedido
		return m.call(ipc.Request{Op: ipc.OpMembers, Chat: chat}, &out,
			func() tea.Msg { return membersMsg{chat: chat, list: out} })
	}
	ms.list = ms.list[:0]
	if ms.query == "" {
		ms.list = append(ms.list, members[:min(len(members), mentionShown)]...)
	} else {
		for _, r := range fuzzy.FindFrom(ms.query, memberNames(members)) {
			ms.list = append(ms.list, members[r.Index])
			if len(ms.list) == mentionShown {
				break
			}
		}
	}
	ms.sel = min(ms.sel, max(0, len(ms.list)-1))
	ms.open = len(ms.list) > 0
	m.layout()
	return nil
}

type memberNames []ipc.Member

func (n memberNames) String(i int) string { return n[i].Name }
func (n memberNames) Len() int            { return len(n) }

// keyMention trata as teclas com a lista aberta; false = não era dela.
func (m *model) keyMention(s string) (tea.Cmd, bool) {
	ms := &m.mention
	switch s {
	case "up":
		ms.sel = max(0, ms.sel-1)
	case "down":
		ms.sel = min(len(ms.list)-1, ms.sel+1)
	case "tab", "enter":
		m.insertMention(ms.list[ms.sel])
	case "esc":
		ms.dismissed, ms.open = ms.query, false
		m.layout()
	default:
		return nil, false
	}
	return nil, true
}

func (m *model) insertMention(p ipc.Member) {
	v := m.input.Value()
	v = v[:len(v)-len(m.mention.query)-1] // tira "@consulta"
	display := "@" + p.Name
	m.input.SetValue(v + display + " ")
	m.mentioned[display] = p.JID
	m.mention.open = false
	m.mention.sel = 0
	m.layout()
}

// resolveMentions troca cada "@Nome" escolhido por "@<usuário>" e devolve
// os JIDs marcados.
func (m *model) resolveMentions(text string) (string, []string) {
	var jids []string
	for display, jid := range m.mentioned {
		if !strings.Contains(text, display) {
			continue
		}
		user, _, _ := strings.Cut(jid, "@")
		text = strings.ReplaceAll(text, display, "@"+user)
		jids = append(jids, jid)
	}
	m.mentioned = map[string]string{}
	return text, jids
}

func (m *model) viewMentions(w int) string {
	var lines []string
	for i, p := range m.mention.list {
		name := "@" + p.Name
		if i == m.mention.sel {
			lines = append(lines, "  "+sGreen.Render("▸ ")+sGreen.Bold(true).Render(name))
		} else {
			lines = append(lines, "    "+sText.Render(name))
		}
	}
	lines = append(lines, "  "+sText2.Render("↑/↓ escolhe · tab ou enter marca · esc fecha"))
	return strings.Join(lines, "\n")
}
