package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Votar em enquete: ctrl+o na enquete abre um painel acima da caixa com as
// opções; setas andam, espaço (ou o número) marca, enter vota, esc fecha.

type pollPanel struct {
	msg    ipc.Message
	cursor int
	chosen map[int]bool
}

func (m *model) openPoll(msg ipc.Message) tea.Cmd {
	c := msg.Card
	if c == nil || len(c.Options) == 0 {
		m.setFlash("enquete do histórico antigo: as opções não vieram (vote pelo celular)")
		return nil
	}
	p := &pollPanel{msg: msg, chosen: map[int]bool{}}
	for i, o := range c.Options {
		if o.Mine {
			p.chosen[i] = true
		}
	}
	m.voting = p
	m.layout()
	return nil
}

func (m *model) keyPoll(s string) tea.Cmd {
	p := m.voting
	opts := p.msg.Card.Options
	toggle := func(i int) {
		if i < 0 || i >= len(opts) {
			return
		}
		if !p.msg.Card.Multi {
			was := p.chosen[i]
			p.chosen = map[int]bool{}
			p.chosen[i] = !was
		} else {
			p.chosen[i] = !p.chosen[i]
		}
		p.cursor = i
	}
	switch s {
	case "esc", "ctrl+c":
		m.voting = nil
		m.layout()
	case "up":
		p.cursor = max(0, p.cursor-1)
	case "down":
		p.cursor = min(len(opts)-1, p.cursor+1)
	case "space":
		toggle(p.cursor)
	case "enter":
		var names []string
		for i, o := range opts {
			if p.chosen[i] {
				names = append(names, o.Name)
			}
		}
		m.voting = nil
		m.layout()
		if len(names) == 0 {
			m.setFlash("tirando o voto…")
		} else {
			m.setFlash("votando…")
		}
		return m.call(ipc.Request{Op: ipc.OpPollVote, Chat: p.msg.Chat, Msg: p.msg.ID, Options: names}, nil,
			func() tea.Msg { return flashMsg("voto enviado") })
	default:
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			toggle(int(s[0] - '1'))
		}
	}
	return nil
}

func (m *model) pollPanelHeight() int { return len(m.voting.msg.Card.Options) + 3 }

func (m *model) viewPoll(w int) string {
	p := m.voting
	c := p.msg.Card
	how := "escolha uma"
	if c.Multi {
		how = "escolha uma ou mais"
	}
	lines := []string{"    " + sBold.Render(ansi.Truncate(icons.poll+" "+c.Title, w-30, "…")) + "  " +
		sText2.Render(how+" · espaço marca · enter vota · esc fecha")}
	for i, o := range c.Options {
		box := "☐"
		if p.chosen[i] {
			box = sGreen.Render("☑")
		}
		mark := "  "
		if i == p.cursor {
			mark = sGreen.Render("▌ ")
		}
		lines = append(lines, "    "+mark+sText2.Render(fmt.Sprint(i+1))+" "+box+" "+sText.Render(ansi.Truncate(o.Name, w-14, "…")))
	}
	return "\n" + strings.Join(lines, "\n")
}

// pollLines: o placar dentro do cartão. Cada opção com ✓ se é a sua, e uma
// barra proporcional aos votos.
func pollLines(c *ipc.Card, w int) []string {
	total := max(1, c.Voters)
	barW := max(4, min(20, w-8))
	var out []string
	for _, o := range c.Options {
		mark, st := "○ ", sText
		if o.Mine {
			mark, st = "✓ ", sGreen
		}
		out = append(out, st.Render(ansi.Truncate(mark+o.Name, w, "…")))
		fill := o.Votes * barW / total
		if o.Votes > 0 && fill == 0 {
			fill = 1
		}
		out = append(out, "  "+fg(cGreen).Render(strings.Repeat("█", fill))+fg(cDivider).Render(strings.Repeat("░", barW-fill))+
			" "+sText2.Render(fmt.Sprint(o.Votes)))
	}
	n := "nenhum voto"
	if c.Voters == 1 {
		n = "1 voto"
	} else if c.Voters > 1 {
		n = fmt.Sprintf("%d votos", c.Voters)
	}
	how := "uma opção"
	if c.Multi {
		how = "várias opções"
	}
	out = append(out, sText2.Italic(true).Render(n+" · "+how+" · ctrl+o vota"))
	return out
}

// openRSVP: responder ao evento (Vou / Talvez / Não vou) e, se tiver,
// entrar na chamada.
func (m *model) openRSVP(msg ipc.Message) tea.Cmd {
	c := msg.Card
	if len(c.Lines) > 0 && c.Lines[0] == "cancelado" {
		m.setFlash("o evento foi cancelado")
		return nil
	}
	answer := func(key, done string) func() tea.Cmd {
		return func() tea.Cmd {
			return m.call(ipc.Request{Op: ipc.OpEventRSVP, Chat: msg.Chat, Msg: msg.ID, Text: key}, nil,
				func() tea.Msg { return flashMsg(done) })
		}
	}
	keys := [][3]string{{"going", "Vou", "você vai"}, {"maybe", "Talvez", "você talvez vá"}, {"not_going", "Não vou", "você não vai"}}
	var opts []popOpt
	for i, k := range keys {
		label := fmt.Sprintf("%s (%d)", k[1], c.Options[i].Votes)
		if c.Options[i].Mine {
			label += " ✓"
		}
		opts = append(opts, popOpt{label, answer(k[0], k[2])})
	}
	if c.URL != "" {
		u := c.URL
		opts = append(opts, popOpt{"Entrar na chamada", func() tea.Cmd {
			if err := openURL(u); err != nil {
				m.setFlash("xdg-open: " + err.Error())
			}
			return nil
		}})
	}
	m.popup = &popMenu{title: c.Title, opts: opts}
	return nil
}
