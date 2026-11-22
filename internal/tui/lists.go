package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
)

// Listas do celular (Favoritas e personalizadas): o filtro ☰ (alt+6)
// escolhe qual; o menu do chat põe e tira a conversa delas.

func (m *model) listByID(id string) *struct{ ID, Name string } {
	for _, l := range m.lists {
		if l.ID == id {
			return &struct{ ID, Name string }{l.ID, l.Name}
		}
	}
	return nil
}

// openListPicker: escolha da lista do filtro.
func (m *model) openListPicker() tea.Cmd {
	if len(m.lists) == 0 {
		m.setFlash("nenhuma lista: crie no celular (Conversas → filtros → +)")
		return nil
	}
	var opts []popOpt
	for _, l := range m.lists {
		label := fmt.Sprintf("%s (%d)", l.Name, l.Count)
		if l.ID == m.listID && m.tab == tabLists {
			label += " ✓"
		}
		opts = append(opts, popOpt{label, func() tea.Cmd {
			m.focus, m.archived = paneList, false
			m.tab, m.listID = tabLists, l.ID
			m.cursor, m.offset = 0, 0
			m.applyFilter()
			return nil
		}})
	}
	m.popup = &popMenu{title: "Listas", opts: opts}
	return nil
}

// openChatLists: no menu do chat, cada lista com ✓ se a conversa está nela.
func (m *model) openChatLists(chat string) tea.Cmd {
	c, _ := m.chatByJID(chat)
	var opts []popOpt
	for _, l := range m.lists {
		in := slices.Contains(c.Lists, l.ID)
		label, action, done := l.Name, "list:"+l.ID+":1", "adicionada a "+l.Name
		if in {
			label, action, done = l.Name+" ✓", "list:"+l.ID+":0", "tirada de "+l.Name
		}
		opts = append(opts, popOpt{label, func() tea.Cmd { return m.chatAction(c, menuOpt{l.Name, action, done}) }})
	}
	m.popup = &popMenu{title: "Listas de " + c.Name, opts: opts}
	return nil
}
