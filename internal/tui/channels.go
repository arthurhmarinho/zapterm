package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Explorar canais (Eu → Explorar canais): os recomendados do diretório,
// filtrados enquanto você digita; um link whatsapp.com/channel/… + enter
// mostra aquele canal. enter num canal oferece seguir. Novo canal: Eu →
// Novo canal.

type chanDir struct {
	in      textinput.Model
	all     []ipc.Channel
	shown   []ipc.Channel
	sel     int
	loading bool
}

type chanDirMsg struct {
	query string
	list  []ipc.Channel
}

func (m *model) openChannelDir() tea.Cmd {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "filtrar pelo nome, ou cole um link de canal"
	m.chans = &chanDir{in: in, loading: true}
	return tea.Batch(m.chans.in.Focus(), m.loadChannelDir(""))
}

func (m *model) loadChannelDir(q string) tea.Cmd {
	var out []ipc.Channel
	return m.call(ipc.Request{Op: ipc.OpChannelDir, Text: q}, &out, func() tea.Msg { return chanDirMsg{q, out} })
}

func (m *model) onChannelDir(msg chanDirMsg) {
	c := m.chans
	if c == nil {
		return
	}
	c.loading = false
	if msg.query != "" {
		c.shown, c.sel = msg.list, 0 // canal do link
		return
	}
	c.all = msg.list
	c.filter()
}

func (c *chanDir) filter() {
	q := strings.ToLower(strings.TrimSpace(c.in.Value()))
	c.shown = nil
	for _, ch := range c.all {
		if q == "" || strings.Contains(strings.ToLower(ch.Name+" "+ch.Description), q) {
			c.shown = append(c.shown, ch)
		}
	}
	c.sel = max(0, min(c.sel, len(c.shown)-1))
}

func (m *model) keyChannelDir(k tea.KeyPressMsg) tea.Cmd {
	c := m.chans
	switch k.String() {
	case "esc", "ctrl+c":
		m.chans = nil
		return nil
	case "up":
		c.sel = max(0, c.sel-1)
		return nil
	case "down":
		c.sel = min(max(0, len(c.shown)-1), c.sel+1)
		return nil
	case "enter":
		if q := strings.TrimSpace(c.in.Value()); strings.Contains(q, "whatsapp.com/channel/") {
			c.loading = true
			return m.loadChannelDir(q)
		}
		if c.sel >= len(c.shown) {
			return nil
		}
		ch := c.shown[c.sel]
		if ch.Following {
			m.chans = nil
			return m.open(m.chatFor(ch.JID, ch.Name))
		}
		m.popup = &popMenu{title: ch.Name, opts: []popOpt{
			{"Seguir", func() tea.Cmd {
				return m.call(ipc.Request{Op: ipc.OpFollow, Chat: ch.JID, Text: ch.Name}, nil,
					func() tea.Msg { return followedMsg{ch} })
			}},
			{"Cancelar", func() tea.Cmd { return nil }},
		}}
		return nil
	}
	var cmd tea.Cmd
	c.in, cmd = c.in.Update(k)
	c.filter()
	return cmd
}

type followedMsg struct{ ch ipc.Channel }

func (m *model) onFollowed(msg followedMsg) tea.Cmd {
	m.chans = nil
	m.setFlash("seguindo " + msg.ch.Name)
	return m.open(ipc.Chat{JID: msg.ch.JID, Name: msg.ch.Name, Kind: "channel", LastStatus: -1})
}

// newChannelForm: nome e descrição; o canal novo abre.
func (m *model) newChannelForm() tea.Cmd {
	f := &form{title: "Novo canal", fields: []formField{
		m.newField("Nome", "nome do canal"), m.newField("Descrição", "opcional"),
	}}
	f.submit = func(f *form) tea.Cmd {
		name := strings.TrimSpace(f.fields[0].in.Value())
		var jid string
		m.setFlash("criando o canal…")
		return m.call(ipc.Request{Op: ipc.OpNewChannel, Text: name, Options: []string{f.fields[1].in.Value()}}, &jid,
			func() tea.Msg {
				return joinedMsg{ipc.Chat{JID: jid, Name: name, Kind: "channel", LastStatus: -1}}
			})
	}
	return m.openForm(f)
}

func humanCount(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.Replace(fmt.Sprintf("%.1f mi", float64(n)/1e6), ".", ",", 1)
	case n >= 1000:
		return strings.Replace(fmt.Sprintf("%.1f mil", float64(n)/1e3), ".", ",", 1)
	}
	return fmt.Sprint(n)
}

func (m *model) viewChannelDir(gm geometry) string {
	w, h := gm.chatW, gm.footerY
	c := m.chans
	lines := []string{"  " + sText.Render(icons.back) + "  " + sBold.Render("Explorar canais") + "   " +
		sText2.Render("esc volta · ↑/↓ escolhem · enter segue ou abre"), "",
		"  " + sText2.Render(icons.search) + "  " + c.in.View(), ""}
	if len(c.shown) == 0 {
		msg := "nenhum canal"
		if c.loading {
			msg = "carregando…"
		}
		lines = append(lines, lipgloss.PlaceHorizontal(w, lipgloss.Center, sText2.Render(msg)))
		return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
	}
	room := max(1, (h-len(lines)-1)/3)
	first := max(0, min(c.sel-room/2, len(c.shown)-room))
	for i := first; i < len(c.shown) && i < first+room; i++ {
		ch := c.shown[i]
		mark, st := "  ", sText
		if i == c.sel {
			mark, st = fg(cGreen).Render("▌")+" ", sGreen.Bold(true)
		}
		title := ch.Name
		if ch.Verified {
			title += " ✓"
		}
		sub := humanCount(ch.Subscribers) + " seguidores"
		if ch.Following {
			sub += " · seguindo"
		}
		if ch.Description != "" {
			sub += " · " + oneLine(ch.Description)
		}
		lines = append(lines, mark+st.Render(ansi.Truncate(title, w-6, "…")),
			mark+sText2.Render(ansi.Truncate(sub, w-6, "…")), "")
	}
	return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
}
