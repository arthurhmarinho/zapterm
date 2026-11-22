package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Status (alt+r): quem postou nas últimas 24 h, os não vistos primeiro.
// enter abre os status da pessoa (←/→ andam, r responde no privado), n
// publica um status de texto.

type statusGroup struct {
	sender, name string
	items        []ipc.Message // do mais antigo ao mais novo
	unseen       int
	mine         bool
}

type stories struct {
	groups  []statusGroup
	sel     int
	open    bool // vendo os status de groups[sel]
	idx     int
	loading bool
}

type statusesMsg []ipc.Message

func (m *model) openStatus() tea.Cmd {
	if m.searching {
		m.stopSearch(false)
	}
	m.stories = &stories{loading: true}
	return m.loadStatuses()
}

func (m *model) loadStatuses() tea.Cmd {
	var out []ipc.Message
	return m.call(ipc.Request{Op: ipc.OpStatuses}, &out, func() tea.Msg { return statusesMsg(out) })
}

// groupStatuses junta por pessoa: os seus primeiro, depois quem tem não
// vistos e, dentro de cada parte, o mais recente antes.
func groupStatuses(msgs []ipc.Message) []statusGroup {
	idx := map[string]int{}
	var gs []statusGroup
	for _, x := range msgs {
		key := x.Sender
		if x.FromMe {
			key = "eu"
		}
		i, ok := idx[key]
		if !ok {
			i = len(gs)
			idx[key] = i
			name := x.SenderName
			if x.FromMe {
				name = "Meu status"
			}
			gs = append(gs, statusGroup{sender: x.Sender, name: name, mine: x.FromMe})
		}
		gs[i].items = append(gs[i].items, x)
		if !x.FromMe && x.Status < 3 {
			gs[i].unseen++
		}
	}
	last := func(g statusGroup) int64 { return g.items[len(g.items)-1].TS }
	slices.SortStableFunc(gs, func(a, b statusGroup) int {
		switch {
		case a.mine != b.mine:
			if a.mine {
				return -1
			}
			return 1
		case (a.unseen > 0) != (b.unseen > 0):
			if a.unseen > 0 {
				return -1
			}
			return 1
		case last(a) > last(b):
			return -1
		case last(a) < last(b):
			return 1
		}
		return 0
	})
	return gs
}

func (m *model) onStatuses(msgs []ipc.Message) {
	s := m.stories
	if s == nil {
		return
	}
	s.loading = false
	sel := ""
	if s.sel < len(s.groups) {
		sel = s.groups[s.sel].sender
	}
	s.groups = groupStatuses(msgs)
	s.sel = max(0, slices.IndexFunc(s.groups, func(g statusGroup) bool { return g.sender == sel }))
	if s.open && (s.sel >= len(s.groups) || s.idx >= len(s.groups[s.sel].items)) {
		s.open = false
	}
}

// showStatus abre o status idx da pessoa selecionada: marca visto e baixa a
// foto se precisar.
func (m *model) showStatus(idx int) tea.Cmd {
	s := m.stories
	g := &s.groups[s.sel]
	s.open, s.idx = true, max(0, min(idx, len(g.items)-1))
	it := &g.items[s.idx]
	var cmds []tea.Cmd
	if !it.FromMe && it.Status < 3 {
		it.Status = 3
		g.unseen = max(0, g.unseen-1)
		cmds = append(cmds, m.call(ipc.Request{Op: ipc.OpViewStatus, Chat: it.Sender, Options: []string{it.ID}}, nil, nil))
	}
	if (it.Kind == "image" || it.Kind == "video") && it.Media == nil && it.HasRaw {
		msg := *it
		var md ipc.Media
		cmds = append(cmds, m.call(ipc.Request{Op: ipc.OpMedia, Chat: msg.Chat, Msg: msg.ID}, &md,
			func() tea.Msg { return statusMediaMsg{id: msg.ID, md: &md} }))
	}
	return tea.Batch(cmds...)
}

type statusMediaMsg struct {
	id string
	md *ipc.Media
}

func (m *model) onStatusMedia(msg statusMediaMsg) {
	if m.stories == nil {
		return
	}
	for gi := range m.stories.groups {
		for i := range m.stories.groups[gi].items {
			if it := &m.stories.groups[gi].items[i]; it.ID == msg.id {
				it.Media = msg.md
			}
		}
	}
}

func (m *model) keyStatus(s string) tea.Cmd {
	st := m.stories
	n := len(st.groups)
	if !st.open {
		switch s {
		case "esc", "ctrl+c", "alt+r":
			m.stories = nil
		case "up":
			st.sel = max(0, st.sel-1)
		case "down":
			st.sel = min(max(0, n-1), st.sel+1)
		case "enter", "right":
			if n > 0 {
				g := st.groups[st.sel]
				first := slices.IndexFunc(g.items, func(x ipc.Message) bool { return !x.FromMe && x.Status < 3 })
				return m.showStatus(max(0, first))
			}
		case "n":
			return m.newStatusForm()
		}
		return nil
	}
	g := st.groups[st.sel]
	switch s {
	case "esc", "ctrl+c", "backspace":
		st.open = false
	case "right", "space", "enter":
		if st.idx+1 < len(g.items) {
			return m.showStatus(st.idx + 1)
		}
		// Acabaram os desta pessoa: vai para a próxima, ou volta à lista.
		if st.sel+1 < n {
			st.sel++
			return m.showStatus(0)
		}
		st.open = false
	case "left":
		if st.idx > 0 {
			return m.showStatus(st.idx - 1)
		}
	case "r":
		if !g.mine {
			return m.replyStatusForm(g.items[st.idx])
		}
	case "ctrl+o":
		if it := g.items[st.idx]; it.Media != nil {
			if err := openExternal(it.Media.Path); err != nil {
				m.setFlash("abrir: " + err.Error())
			}
		}
	}
	return nil
}

func (m *model) newStatusForm() tea.Cmd {
	f := m.newField("Texto", "o que você quer contar?")
	return m.openForm(&form{title: "Novo status", fields: []formField{f}, submit: func(f *form) tea.Cmd {
		text := strings.TrimSpace(f.fields[0].in.Value())
		return m.call(ipc.Request{Op: ipc.OpPostStatus, Text: text}, nil, func() tea.Msg {
			return statusPostedMsg{}
		})
	}})
}

type statusPostedMsg struct{}

// replyStatusForm: a resposta vai para a conversa com a pessoa, citando o
// status, como no app.
func (m *model) replyStatusForm(it ipc.Message) tea.Cmd {
	f := m.newField("Resposta", "")
	name := firstName(it.SenderName)
	return m.openForm(&form{title: "Responder a " + name, fields: []formField{f}, submit: func(f *form) tea.Cmd {
		text := strings.TrimSpace(f.fields[0].in.Value())
		if text == "" {
			return nil
		}
		m.setFlash("resposta enviada para " + name)
		return m.send(ipc.Request{Op: ipc.OpSend, Chat: it.Sender, Text: text, ReplyTo: it.Chat + "|" + it.ID})
	}})
}

func (m *model) viewStatus(gm geometry) string {
	w, h := gm.chatW, gm.footerY
	st := m.stories
	if st.open && st.sel < len(st.groups) {
		return m.viewStatusItem(gm)
	}
	lines := []string{"  " + sText.Render(icons.back) + "  " + sBold.Render("Status") + "   " +
		sText2.Render("esc volta · enter vê · n novo status de texto"), ""}
	if len(st.groups) == 0 {
		msg := "ninguém postou nas últimas 24 horas"
		if st.loading {
			msg = "carregando…"
		}
		lines = append(lines, "", lipgloss.PlaceHorizontal(w, lipgloss.Center, sText2.Render(msg)))
		return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
	}
	seenHead := false
	for i, g := range st.groups {
		if !g.mine && g.unseen == 0 && !seenHead {
			lines = append(lines, "", "  "+sText2.Render("Vistos"))
			seenHead = true
		} else if i == 0 || (st.groups[i-1].mine && !g.mine) {
			if !g.mine {
				lines = append(lines, "", "  "+sText2.Render("Recentes"))
			}
		}
		mark, nameSt := "  ", sText
		if i == st.sel {
			mark, nameSt = fg(cGreen).Render("▌")+" ", sGreen.Bold(true)
		}
		ring := sText2.Render("○")
		if g.unseen > 0 {
			ring = sGreen.Render("●")
		}
		last := g.items[len(g.items)-1]
		sub := fmt.Sprintf("%d · %s", len(g.items), shortTime(last.TS))
		if g.unseen > 0 {
			sub = fmt.Sprintf("%d novo(s) · %s", g.unseen, shortTime(last.TS))
		}
		lines = append(lines, mark+ring+" "+nameSt.Render(ansi.Truncate(g.name, w-30, "…"))+"  "+sText2.Render(sub))
	}
	return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
}

func (m *model) viewStatusItem(gm geometry) string {
	w, h := gm.chatW, gm.footerY
	st := m.stories
	g := st.groups[st.sel]
	it := g.items[st.idx]
	// Barrinhas de progresso no topo, uma por status.
	var bars []string
	bw := max(1, (w-4)/len(g.items)-1)
	for i := range g.items {
		c := cDivider
		if i <= st.idx {
			c = cGreen
		}
		bars = append(bars, fg(c).Render(strings.Repeat("━", bw)))
	}
	head := []string{"  " + strings.Join(bars, " "),
		"  " + sBold.Render(g.name) + "  " + sText2.Render(time.UnixMilli(it.TS).Format("15:04")), ""}
	room := h - len(head) - 2
	var body string
	switch {
	case it.Kind == "image" || it.Kind == "video":
		md := it.Media
		if md == nil || md.Preview == "" {
			md = it.Thumb
		}
		if md != nil && md.Preview != "" {
			body = m.imgs.media(md, w-4, room-2)
		}
		if body == "" {
			body = sText2.Render(mediaLabel(it.Text))
		}
		if c := stripTag(it.Text); c != "" {
			body += "\n\n" + sText.Render(ansi.Wrap(c, w-8, " "))
		}
	default:
		text := it.Text
		if strings.HasPrefix(text, "[") {
			text = otherLabel(text)
		}
		body = lipgloss.NewStyle().Foreground(cDefault).Bold(true).Render(ansi.Wrap(plainText(text), w-12, " "))
	}
	box := lipgloss.Place(w, room, lipgloss.Center, lipgloss.Center, body)
	hint := "←/→ anda · r responde · ctrl+o abre · esc volta"
	if g.mine {
		hint = "←/→ anda · esc volta"
	}
	out := append(head, strings.Split(box, "\n")...)
	out = append(out, "", "  "+sText2.Render(hint))
	return fitHeight(strings.Join(out, "\n"), cDefault, w, h)
}
