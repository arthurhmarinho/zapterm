package tui

import (
	"fmt"
	"strings"

	bt "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Tela "Eu" (alt+y ou clique na sua foto no topo da lista): ocupa o painel
// da direita, como o perfil, com a sua foto, nome, número e recado e a lista
// de opções: nome, recado, privacidade, novo grupo e canal, explorar canais
// e desconectar.

type meMsg struct {
	me   *ipc.Me
	open bool // abrir a tela (e não só atualizar os dados dela)
}

type meDoneMsg string

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

// openMe (alt+y ou clique no seu nome no topo) abre a tela "Eu".
func (m *model) openMe() bt.Cmd {
	var out ipc.Me
	m.setFlash("carregando seu perfil…")
	return m.call(ipc.Request{Op: ipc.OpMe}, &out, func() bt.Msg { return meMsg{&out, true} })
}

// refreshMe recarrega os dados da tela aberta (depois de trocar o nome, o
// recado ou uma configuração).
func (m *model) refreshMe() bt.Cmd {
	var out ipc.Me
	return m.call(ipc.Request{Op: ipc.OpMe}, &out, func() bt.Msg { return meMsg{&out, false} })
}

func (m *model) onMe(msg meMsg) {
	if msg.open {
		m.flash = ""
		m.stories, m.chans, m.gallery = nil, nil, nil
		if m.searching {
			m.stopSearch(false)
		}
		if m.profileOpen {
			m.closeProfile()
		}
		if !m.meOpen {
			m.meSel = 0
		}
		m.meOpen = true
	} else if !m.meOpen {
		return // chegou depois de fechar a tela
	}
	m.meData = msg.me
	if msg.me.Photo == nil && m.meSel < 0 {
		m.meSel = 0
	}
}

func (m *model) closeMe() { m.meOpen, m.meData = false, nil }

type meItem struct {
	label, value string
	danger       bool
	run          func() bt.Cmd
}

func (m *model) meItems() []meItem {
	me := m.meData
	if me == nil {
		return nil
	}
	about := me.About
	if about == "" {
		about = "(vazio)"
	}
	return []meItem{
		{label: "Nome", value: me.Name, run: func() bt.Cmd { return m.openMeField("name", "Seu nome", me.Name) }},
		{label: "Recado", value: oneLine(about), run: func() bt.Cmd { return m.openMeField("about", "Recado", me.About) }},
		{label: "Privacidade", value: "visto por último, online, foto, status…", run: func() bt.Cmd { return m.openPrivacy(me) }},
		{label: "Novo grupo", run: func() bt.Cmd { m.closeMe(); return m.startNewGroup() }},
		{label: "Novo canal", run: m.newChannelForm},
		{label: "Explorar canais", run: func() bt.Cmd { m.closeMe(); return m.openChannelDir() }},
		{label: "Desconectar", danger: true, run: m.confirmLogout},
	}
}

// openPrivacy: as sete configurações da conta, cada uma com seus valores.
func (m *model) openPrivacy(me *ipc.Me) bt.Cmd {
	var opts []popOpt
	for _, it := range privacyItems {
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
	m.popup = &popMenu{title: "Privacidade · " + me.Phone, opts: opts}
	return nil
}

// confirmLogout pergunta antes de desconectar; o padrão é Cancelar.
func (m *model) confirmLogout() bt.Cmd {
	m.popup = &popMenu{
		title: "Desconectar este computador? O zapterm sai de Aparelhos conectados no celular. As mensagens ficam guardadas aqui.",
		opts: []popOpt{
			{"Desconectar", m.logout},
			{"Cancelar", func() bt.Cmd { return nil }},
		},
		sel: 1,
	}
	return nil
}

type loggedOutMsg struct{}

func (m *model) logout() bt.Cmd {
	m.setFlash("desconectando…")
	return m.call(ipc.Request{Op: ipc.OpLogout}, nil, func() bt.Msg { return loggedOutMsg{} })
}

func (m *model) keyMe(s string) bt.Cmd {
	items := m.meItems()
	first := -1
	if m.meData == nil || m.meData.Photo == nil {
		first = 0
	}
	switch s {
	case "esc", "ctrl+c", "alt+y":
		m.closeMe()
	case "up":
		m.meSel = max(first, m.meSel-1)
	case "down":
		m.meSel = min(len(items)-1, m.meSel+1)
	case "enter":
		return m.activateMe()
	}
	return nil
}

// activateMe: enter ou clique no item selecionado (-1 = a foto).
func (m *model) activateMe() bt.Cmd {
	items := m.meItems()
	switch {
	case m.meSel < 0 && m.meData != nil && m.meData.Photo != nil:
		m.viewing = &viewer{media: m.meData.Photo, caption: m.meData.Name}
	case m.meSel >= 0 && m.meSel < len(items):
		return items[m.meSel].run()
	}
	return nil
}

func (m *model) clickMe(y int) bt.Cmd {
	items := m.meItems()
	switch {
	case y <= 1:
		m.closeMe() // seta de voltar
	case m.meData != nil && m.meData.Photo != nil && y >= m.mePhotoY[0] && y < m.mePhotoY[1]:
		m.meSel = -1
		return m.activateMe()
	case y >= m.meItemsY && y < m.meItemsY+len(items):
		m.meSel = y - m.meItemsY
		return m.activateMe()
	}
	return nil
}

// viewMe desenha a tela no painel da direita, como o perfil: foto grande,
// nome, número, recado e a lista de opções.
func (m *model) viewMe(g geometry) string {
	w, h := g.chatW, g.footerY
	center := func(s string) string { return lipgloss.PlaceHorizontal(w, lipgloss.Center, s) }
	lines := []string{"  " + sText.Render(icons.back) + "  " + sBold.Render("Eu") + "   " + sText2.Render("esc fecha"), ""}
	me := m.meData
	if me == nil {
		lines = append(lines, "", center(sText2.Render("carregando…")))
		return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
	}

	items := m.meItems()
	var info []string
	info = append(info, center(sBold.Render(ansi.Truncate(me.Name, w-4, "…"))), center(sText2.Render(me.Phone)))
	if me.About != "" {
		for _, l := range strings.Split(formatText(me.About, min(w-8, 70), sText), "\n") {
			info = append(info, center(l))
		}
	}
	// A foto ocupa o que sobrar: cabeça (2), foto, linha da foto, informações,
	// vazio, lista.
	room := h - len(lines) - 1 - len(info) - 1 - len(items) - 1
	ph := min(12, room)
	m.mePhotoY = [2]int{}
	if ph >= 4 {
		var pic string
		if me.Photo != nil {
			pic = m.imgs.media(me.Photo, 2*ph, ph)
		}
		if pic == "" {
			pic = m.avatar(me.JID, me.Name, 4*ph/3, 2*ph/3)
		}
		m.mePhotoY[0] = len(lines)
		for _, l := range strings.Split(pic, "\n") {
			lines = append(lines, center(l))
		}
		m.mePhotoY[1] = len(lines)
		if me.Photo != nil && m.meSel < 0 {
			lines = append(lines, center(sGreen.Render("▌ enter abre a foto em tamanho cheio")))
		} else {
			lines = append(lines, "")
		}
	}
	lines = append(lines, info...)
	lines = append(lines, "")
	m.meItemsY = len(lines)
	labelW := 17
	for i, it := range items {
		mark, st := "  ", sText
		if it.danger {
			st = fg(cDanger)
		}
		if i == m.meSel {
			mark, st = sGreen.Render("▌ "), st.Bold(true)
			if !it.danger {
				st = sGreen.Bold(true)
			}
		}
		l := "  " + mark + st.Render(fmt.Sprintf("%-*s", labelW, it.label))
		if it.value != "" {
			l += sText2.Render(ansi.Truncate(it.value, max(4, w-labelW-8), "…"))
		}
		lines = append(lines, l)
	}
	return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
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
		func() bt.Msg { return meDoneMsg(done) })
}

// viewMeForm: sem chat aberto não há rodapé da conversa, então o
// formulário do nome/recado flutua sobre o painel vazio.
func (m *model) viewMeForm(w int) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).
		Render(strings.TrimPrefix(m.viewForm(w-2), "\n"))
}
