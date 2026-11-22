package tui

import (
	"strings"

	bt "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Tela "Eu" (alt+y ou clique no seu nome no topo da lista): nome de
// perfil, recado e privacidade da conta, num menu como o do chat.

type meMsg struct{ me *ipc.Me }

// privacyItems: as configurações na ordem do celular e os valores aceitos
// (os mesmos do daemon), com o rótulo de cada um.
var privacyItems = []struct {
	key, label string
	values     [][2]string
}{
	{"last", "Visto por último", [][2]string{{"all", "Todos"}, {"contacts", "Meus contatos"}, {"none", "Ninguém"}}},
	{"online", "Online", [][2]string{{"all", "Todos"}, {"match_last_seen", "Igual ao visto por último"}}},
	{"profile", "Foto do perfil", [][2]string{{"all", "Todos"}, {"contacts", "Meus contatos"}, {"none", "Ninguém"}}},
	{"status", "Status", [][2]string{{"contacts", "Meus contatos"}, {"none", "Ninguém"}}},
	{"readreceipts", "Confirmação de leitura", [][2]string{{"all", "Ligada"}, {"none", "Desligada"}}},
	{"groupadd", "Quem pode me pôr em grupos", [][2]string{{"all", "Todos"}, {"contacts", "Meus contatos"}}},
	{"calladd", "Chamadas de desconhecidos", [][2]string{{"all", "Tocam"}, {"known", "Silenciadas"}}},
}

// privacyLabel: o valor atual por extenso ("contact_blacklist" e outros
// que só o celular escolhe aparecem como "outro").
func privacyLabel(key, value string) string {
	for _, it := range privacyItems {
		if it.key != key {
			continue
		}
		for _, v := range it.values {
			if v[0] == value {
				return v[1]
			}
		}
	}
	switch value {
	case "contact_blacklist":
		return "Meus contatos, exceto…"
	case "":
		return "?"
	}
	return "outro"
}

func (m *model) openMe() bt.Cmd {
	var out ipc.Me
	m.setFlash("carregando seu perfil…")
	return m.call(ipc.Request{Op: ipc.OpMe}, &out, func() bt.Msg { return meMsg{&out} })
}

func (m *model) onMe(me *ipc.Me) {
	m.flash = ""
	about := me.About
	if about == "" {
		about = "(vazio)"
	}
	opts := []popOpt{
		{"Novo grupo…", m.startNewGroup},
		{"Novo canal…", m.newChannelForm},
		{"Explorar canais…", m.openChannelDir},
		{"Nome: " + me.Name, func() bt.Cmd { return m.openMeField("name", "Seu nome", me.Name) }},
		{"Recado: " + oneLine(about), func() bt.Cmd { return m.openMeField("about", "Recado", me.About) }},
	}
	for _, it := range privacyItems {
		it := it
		opts = append(opts, popOpt{it.label + ": " + privacyLabel(it.key, me.Privacy[it.key]), func() bt.Cmd {
			var sub []popOpt
			for _, v := range it.values {
				label := v[1]
				if v[0] == me.Privacy[it.key] {
					label += " ✓"
				}
				sub = append(sub, popOpt{label, func() bt.Cmd {
					return m.setMe("privacy:"+it.key, v[0], it.label+": "+strings.ToLower(v[1]))
				}})
			}
			m.popup = &popMenu{title: it.label, opts: sub}
			return nil
		}})
	}
	m.popup = &popMenu{title: "Eu · " + me.Phone, opts: opts}
}

// openMeField abre um formulário de um campo para o nome ou o recado.
func (m *model) openMeField(key, label, value string) bt.Cmd {
	f := m.newField(label, "")
	f.in.SetValue(value)
	f.in.CursorEnd()
	return m.openForm(&form{title: label, fields: []formField{f}, submit: func(f *form) bt.Cmd {
		v := strings.TrimSpace(f.fields[0].in.Value())
		return m.setMe(key, v, strings.ToLower(label)+" trocado")
	}})
}

func (m *model) setMe(key, value, done string) bt.Cmd {
	return m.call(ipc.Request{Op: ipc.OpSetMe, Text: key, Options: []string{value}}, nil,
		func() bt.Msg { return flashMsg(done) })
}

// viewMeForm: sem chat aberto não há rodapé da conversa, então o
// formulário do nome/recado flutua sobre o painel vazio.
func (m *model) viewMeForm(w int) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).
		Render(strings.TrimPrefix(m.viewForm(w-2), "\n"))
}
