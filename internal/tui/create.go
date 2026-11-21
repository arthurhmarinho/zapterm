package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbles/v2/textinput"
	bt "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Criar coisas: o clipe (alt+f) vira um menu e os comandos "/" no começo
// do campo fazem o mesmo. Foto/vídeo e documento abrem o seletor de
// arquivos; enquete e evento abrem um formulário acima da caixa; contato
// escolhe alguém na busca.

type command struct {
	name, desc string
	run        func(m *model) bt.Cmd
}

var commands = []command{
	{"foto", "foto ou vídeo", func(m *model) bt.Cmd { return m.chooseKind(true) }},
	{"arquivo", "documento ou qualquer arquivo", func(m *model) bt.Cmd { return m.chooseKind(false) }},
	{"figurinha", "suas figurinhas", func(m *model) bt.Cmd { return m.openStickers() }},
	{"enquete", "criar uma enquete", func(m *model) bt.Cmd { return m.openPollForm() }},
	{"evento", "criar um evento", func(m *model) bt.Cmd { return m.openEventForm() }},
	{"contato", "mandar um contato", func(m *model) bt.Cmd { return m.pickContact() }},
}

// openAttachMenu: o clipe (alt+f ou clique).
func (m *model) openAttachMenu() bt.Cmd {
	if m.active == "" {
		m.setFlash("abra um chat antes de anexar")
		return nil
	}
	var opts []popOpt
	for _, c := range commands {
		opts = append(opts, popOpt{strings.ToUpper(c.desc[:1]) + c.desc[1:], func() bt.Cmd { return c.run(m) }})
	}
	m.popup = &popMenu{title: "Anexar", opts: opts}
	return nil
}

func (m *model) chooseKind(media bool) bt.Cmd {
	m.picking = false
	m.layout()
	m.setFlash("escolha o arquivo na janela que abriu…")
	return chooseFile(media)
}

// ---------- comandos "/" ----------

type slashState struct {
	open      bool
	list      []command
	sel       int
	dismissed string
}

// updateSlash: "/en" sozinho no campo mostra os comandos que começam assim.
func (m *model) updateSlash() {
	ss := &m.slash
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \n") || v == ss.dismissed || m.active == "" {
		if !strings.HasPrefix(v, "/") {
			ss.dismissed = ""
		}
		if ss.open {
			ss.open = false
			m.layout()
		}
		return
	}
	q := strings.ToLower(strings.TrimPrefix(v, "/"))
	ss.list = ss.list[:0]
	for _, c := range commands {
		if strings.HasPrefix(c.name, q) {
			ss.list = append(ss.list, c)
		}
	}
	ss.sel = min(ss.sel, max(0, len(ss.list)-1))
	ss.open = len(ss.list) > 0
	m.layout()
}

func (m *model) keySlash(s string) (bt.Cmd, bool) {
	ss := &m.slash
	switch s {
	case "up":
		ss.sel = max(0, ss.sel-1)
	case "down":
		ss.sel = min(len(ss.list)-1, ss.sel+1)
	case "tab", "enter":
		c := ss.list[ss.sel]
		ss.open = false
		m.input.Reset()
		m.layout()
		return c.run(m), true
	case "esc":
		ss.dismissed, ss.open = m.input.Value(), false // manda como texto
		m.layout()
	default:
		return nil, false
	}
	return nil, true
}

func (m *model) viewSlash() string {
	var lines []string
	for i, c := range m.slash.list {
		name := "/" + c.name
		if i == m.slash.sel {
			lines = append(lines, "  "+sGreen.Render("▸ ")+sGreen.Bold(true).Render(name)+"  "+sText2.Render(c.desc))
		} else {
			lines = append(lines, "    "+sText.Render(name)+"  "+sText2.Render(c.desc))
		}
	}
	lines = append(lines, "  "+sText2.Render("↑/↓ escolhe · enter abre · esc manda como texto"))
	return strings.Join(lines, "\n")
}

// ---------- formulário (enquete, evento) ----------

type formField struct {
	label string
	in    tea.Model
}

type form struct {
	title  string
	fields []formField
	idx    int
	poll   bool // enquete: enter na última opção preenchida cria outra
	multi  bool // enquete: várias respostas (alt+t)
	submit func(f *form) bt.Cmd
}

func (m *model) newField(label, placeholder string) formField {
	in := tea.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.SetWidth(max(10, m.geom().chatW-24))
	return formField{label: label, in: in}
}

func (m *model) openPollForm() bt.Cmd {
	if m.active == "" {
		return nil
	}
	f := &form{title: "Nova enquete", poll: true, fields: []formField{
		m.newField("Pergunta", "o que você quer perguntar?"),
		m.newField("Opção 1", ""), m.newField("Opção 2", ""),
	}}
	chat := m.active
	f.submit = func(f *form) bt.Cmd {
		var opts []string
		for _, x := range f.fields[1:] {
			if v := strings.TrimSpace(x.in.Value()); v != "" {
				opts = append(opts, v)
			}
		}
		single := 1
		if f.multi {
			single = 0
		}
		return m.send(ipc.Request{Op: ipc.OpSendPoll, Chat: chat, Text: f.fields[0].in.Value(), Options: opts, Seconds: single})
	}
	return m.openForm(f)
}

func (m *model) openEventForm() bt.Cmd {
	if m.active == "" {
		return nil
	}
	now := time.Now().Add(24 * time.Hour)
	f := &form{title: "Novo evento", fields: []formField{
		m.newField("Nome", "nome do evento"),
		m.newField("Data", now.Format("02/01/2006")),
		m.newField("Hora", "19:00"),
		m.newField("Local", "opcional"),
		m.newField("Descrição", "opcional"),
	}}
	chat := m.active
	f.submit = func(f *form) bt.Cmd {
		start, err := parseWhen(f.fields[1].in.Value(), f.fields[2].in.Value())
		if err != nil {
			m.setFlash(err.Error())
			m.form = f // continua aberto para corrigir
			m.layout()
			return nil
		}
		return m.send(ipc.Request{Op: ipc.OpSendEvent, Chat: chat, Text: f.fields[0].in.Value(), Before: start.UnixMilli(),
			Options: []string{f.fields[4].in.Value(), f.fields[3].in.Value()}})
	}
	return m.openForm(f)
}

// parseWhen lê "dd/mm" ou "dd/mm/aaaa" e "hh:mm" (vazios: amanhã às 19h).
func parseWhen(date, hour string) (time.Time, error) {
	now := time.Now()
	date, hour = strings.TrimSpace(date), strings.TrimSpace(hour)
	if date == "" {
		date = now.Add(24 * time.Hour).Format("02/01/2006")
	}
	if hour == "" {
		hour = "19:00"
	}
	if strings.Count(date, "/") == 1 {
		date += "/" + fmt.Sprint(now.Year())
	}
	t, err := time.ParseInLocation("2/1/2006 15:04", date+" "+hour, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("data ou hora inválida (use 25/12 e 19:30)")
	}
	return t, nil
}

func (m *model) openForm(f *form) bt.Cmd {
	m.form = f
	m.input.Blur()
	m.layout()
	return f.fields[0].in.Focus()
}

func (m *model) closeForm() {
	m.form = nil
	m.input.Focus()
	m.layout()
}

func (m *model) focusField(i int) bt.Cmd {
	f := m.form
	f.fields[f.idx].in.Blur()
	f.idx = max(0, min(i, len(f.fields)-1))
	return f.fields[f.idx].in.Focus()
}

func (m *model) keyForm(k bt.KeyPressMsg) bt.Cmd {
	f := m.form
	switch k.String() {
	case "esc", "ctrl+c":
		m.closeForm()
		return nil
	case "alt+enter":
		m.closeForm()
		return f.submit(f)
	case "alt+t":
		if f.poll {
			f.multi = !f.multi
		}
		return nil
	case "tab", "down":
		return m.focusField(f.idx + 1)
	case "shift+tab", "up":
		return m.focusField(f.idx - 1)
	case "enter":
		last := f.idx == len(f.fields)-1
		if f.poll && last && strings.TrimSpace(f.fields[f.idx].in.Value()) != "" && len(f.fields) < 13 {
			f.fields = append(f.fields, m.newField(fmt.Sprintf("Opção %d", len(f.fields)), ""))
			m.layout()
			return m.focusField(len(f.fields) - 1)
		}
		if last {
			m.closeForm()
			return f.submit(f)
		}
		return m.focusField(f.idx + 1)
	}
	var cmd bt.Cmd
	f.fields[f.idx].in, cmd = f.fields[f.idx].in.Update(k)
	return cmd
}

func (m *model) formHeight() int { return len(m.form.fields) + 3 }

func (m *model) viewForm(w int) string {
	f := m.form
	hint := "enter próximo · alt+enter envia · esc cancela"
	if f.poll {
		kind := "uma resposta"
		if f.multi {
			kind = "várias respostas"
		}
		hint = "enter na última opção cria outra · alt+t " + kind + " · alt+enter envia · esc cancela"
	}
	lines := []string{"  " + sBold.Render(f.title) + "  " + sText2.Render(hint)}
	for i, x := range f.fields {
		mark := "  "
		label := sText2.Render(fmt.Sprintf("%-10s", x.label))
		if i == f.idx {
			mark, label = sGreen.Render("▌ "), sGreen.Render(fmt.Sprintf("%-10s", x.label))
		}
		lines = append(lines, "  "+mark+label+" "+x.in.View())
	}
	return "\n" + ansi.Truncate(strings.Join(lines, "\n"), w*len(lines), "")
}

// ---------- contato ----------

// pickContact: a busca escolhe a pessoa; enter manda o contato dela.
func (m *model) pickContact() bt.Cmd {
	m.pickingContact = true
	m.startSearch()
	m.setFlash("escolha o contato a mandar e aperte enter")
	return nil
}
