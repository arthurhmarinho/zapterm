package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Criar grupo (Eu → Novo grupo, ou /grupo) e administrar (alt+e no perfil
// do grupo): nome, descrição, foto, participantes, permissões e pedidos.

// ---------- novo grupo ----------

// startNewGroup: a busca escolhe as pessoas (enter marca), alt+enter
// pede o nome e cria.
func (m *model) startNewGroup() tea.Cmd {
	m.newGroup = &[]ipc.Chat{}
	m.startSearch() // a linha de ajuda diz o que fazer e quem já foi marcado
	return nil
}

func (m *model) toggleGroupMember(c ipc.Chat) {
	list := m.newGroup
	for i, t := range *list {
		if t.JID == c.JID {
			*list = append((*list)[:i], (*list)[i+1:]...)
			return
		}
	}
	*list = append(*list, c)
}

func (m *model) newGroupHelp() string {
	h := "novo grupo: enter marca · alt+enter segue · esc cancela"
	if n := len(*m.newGroup); n > 0 {
		var names []string
		for _, c := range *m.newGroup {
			names = append(names, firstName(c.Name))
		}
		h = fmt.Sprintf("com: %s (%d) · ", strings.Join(names, ", "), n) + h
	}
	return h
}

// askGroupName: depois de escolher as pessoas, o nome.
func (m *model) askGroupName() tea.Cmd {
	people := *m.newGroup
	if len(people) == 0 {
		m.setFlash("marque pelo menos uma pessoa com enter")
		return nil
	}
	m.newGroup = nil
	m.stopSearch(true)
	f := m.newField("Nome", "nome do grupo")
	return m.openForm(&form{title: "Novo grupo", fields: []formField{f}, submit: func(f *form) tea.Cmd {
		name := strings.TrimSpace(f.fields[0].in.Value())
		var jids []string
		for _, c := range people {
			jids = append(jids, c.JID)
		}
		var jid string
		m.setFlash("criando o grupo…")
		return m.call(ipc.Request{Op: ipc.OpNewGroup, Text: name, Options: jids}, &jid, func() tea.Msg {
			return joinedMsg{ipc.Chat{JID: jid, Name: name, Kind: "group", IsGroup: true, LastStatus: -1}}
		})
	}})
}

// ---------- administrar ----------

// openGroupAdmin: o que dá para mudar no grupo aberto no perfil. Nome,
// descrição e foto: admin, ou todos se o grupo não está trancado.
func (m *model) openGroupAdmin() tea.Cmd {
	p := m.profile
	if p == nil || p.Kind != "group" || p.Group == nil {
		return nil
	}
	chat, gs := p.JID, *p.Group
	set := func(key, value, done string) tea.Cmd {
		return m.call(ipc.Request{Op: ipc.OpGroupSet, Chat: chat, Text: key, Options: []string{value}}, nil,
			func() tea.Msg { return groupChangedMsg{chat: chat, done: done} })
	}
	field := func(key, label, value string) func() tea.Cmd {
		return func() tea.Cmd {
			f := m.newField(label, "")
			f.in.SetValue(value)
			f.in.CursorEnd()
			return m.openForm(&form{title: label + " do grupo", fields: []formField{f}, submit: func(f *form) tea.Cmd {
				return set(key, strings.TrimSpace(f.fields[0].in.Value()), strings.ToLower(label)+" do grupo trocado")
			}})
		}
	}
	toggle := func(label, key string, on bool) popOpt {
		state := "não"
		if on {
			state = "sim"
		}
		v, done := "1", label+": sim"
		if on {
			v, done = "0", label+": não"
		}
		return popOpt{label + ": " + state, func() tea.Cmd { return set(key, v, strings.ToLower(done[:1])+done[1:]) }}
	}
	var opts []popOpt
	if p.IAmAdmin || !gs.Locked {
		opts = append(opts,
			popOpt{"Mudar nome…", field("name", "Nome", p.Name)},
			popOpt{"Mudar descrição…", field("topic", "Descrição", p.About)},
			popOpt{"Trocar foto…", func() tea.Cmd {
				m.groupPhotoFor = chat
				m.setFlash("escolha a foto na janela que abriu…")
				return chooseFile(true)
			}})
		if p.Photo != nil {
			opts = append(opts, popOpt{"Tirar a foto", func() tea.Cmd { return set("photo", "", "foto do grupo removida") }})
		}
	}
	if p.IAmAdmin || !gs.AdminAdd {
		opts = append(opts, popOpt{"Adicionar participante…", func() tea.Cmd {
			f := m.newField("Telefone", "+55 84 9…")
			return m.openForm(&form{title: "Adicionar ao grupo", fields: []formField{f}, submit: func(f *form) tea.Cmd {
				phone := strings.TrimSpace(f.fields[0].in.Value())
				return m.call(ipc.Request{Op: ipc.OpGroupMember, Chat: chat, Msg: phone, Text: "add"}, nil,
					func() tea.Msg { return groupChangedMsg{chat: chat, done: phone + " adicionado ao grupo"} })
			}})
		}})
	}
	if p.IAmAdmin {
		opts = append(opts,
			toggle("Só admins mandam mensagens", "announce", gs.Announce),
			toggle("Só admins mudam os dados", "locked", gs.Locked),
			toggle("Só admins adicionam pessoas", "addmode", gs.AdminAdd),
			toggle("Aprovar quem entra pelo link", "approval", gs.Approval),
			popOpt{"Pedidos para entrar…", func() tea.Cmd {
				var out []ipc.ProfileMember
				return m.call(ipc.Request{Op: ipc.OpGroupReqs, Chat: chat}, &out,
					func() tea.Msg { return groupReqsMsg{chat: chat, reqs: out} })
			}})
		// addmode quer "admin"/"all", não 1/0.
		for i := range opts {
			if strings.HasPrefix(opts[i].label, "Só admins adicionam") {
				v, done := "admin", "só admins adicionam pessoas"
				if gs.AdminAdd {
					v, done = "all", "todos podem adicionar pessoas"
				}
				opts[i].run = func() tea.Cmd { return set("addmode", v, done) }
			}
		}
	}
	if len(opts) == 0 {
		m.setFlash("só os admins mudam este grupo")
		return nil
	}
	m.popup = &popMenu{title: "Administrar " + p.Name, opts: opts}
	return nil
}

type groupChangedMsg struct{ chat, done string }

type groupReqsMsg struct {
	chat string
	reqs []ipc.ProfileMember
}

// onGroupChanged: avisa e recarrega o perfil para mostrar o novo estado.
func (m *model) onGroupChanged(msg groupChangedMsg) tea.Cmd {
	m.setFlash(msg.done)
	if m.profileOpen && m.active == msg.chat {
		chat := msg.chat
		var out ipc.Profile
		return m.call(ipc.Request{Op: ipc.OpProfile, Chat: chat}, &out, func() tea.Msg { return profileMsg{&out} })
	}
	return nil
}

// onGroupReqs: cada pedido vira uma opção que pergunta aprovar ou recusar.
func (m *model) onGroupReqs(msg groupReqsMsg) {
	if len(msg.reqs) == 0 {
		m.setFlash("nenhum pedido para entrar")
		return
	}
	chat := msg.chat
	var opts []popOpt
	for _, r := range msg.reqs {
		opts = append(opts, popOpt{r.Name, func() tea.Cmd {
			act := func(key, done string) func() tea.Cmd {
				return func() tea.Cmd {
					return m.call(ipc.Request{Op: ipc.OpGroupSet, Chat: chat, Text: key, Options: []string{r.Raw}}, nil,
						func() tea.Msg { return groupChangedMsg{chat: chat, done: done} })
				}
			}
			m.popup = &popMenu{title: r.Name + " quer entrar", opts: []popOpt{
				{"Aprovar", act("approve", r.Name+" entrou no grupo")},
				{"Recusar", act("reject", "pedido de "+r.Name+" recusado")},
				{"Cancelar", func() tea.Cmd { return nil }},
			}}
			return nil
		}})
	}
	m.popup = &popMenu{title: fmt.Sprintf("Pedidos para entrar (%d)", len(msg.reqs)), opts: opts}
}
