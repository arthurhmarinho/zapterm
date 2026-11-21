package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Navegação dentro e entre conversas: o divisor "N mensagens não lidas",
// a busca na conversa (ctrl+f) e a troca de chat sem passar pela lista
// (alt+↑/↓, alt+n).

// ---------- divisor de não lidas ----------

// markUnread acha, nas mensagens recém-carregadas, a primeira das n não
// lidas (contando de trás para frente só as dos outros) e rola até ela.
func (m *model) markUnread() {
	if m.unreadCount <= 0 || m.unreadID != "" || len(m.msgs) == 0 {
		return
	}
	n := 0
	m.unreadID = m.msgs[0].ID // mais não lidas do que o carregado: no topo
	for i := len(m.msgs) - 1; i >= 0; i-- {
		if m.msgs[i].FromMe {
			continue
		}
		n++
		if n == m.unreadCount {
			m.unreadID = m.msgs[i].ID
			break
		}
	}
	m.refreshMessages(false)
	for i, x := range m.msgs {
		if x.ID == m.unreadID && i < len(m.spans) {
			// O divisor (uma linha acima do balão) no alto da tela.
			off := max(0, m.spans[i][0]-2)
			if off < m.vp.TotalLineCount()-m.vp.Height() {
				m.vp.SetYOffset(off)
			}
		}
	}
}

func unreadLabel(n int) string {
	if n == 1 {
		return " 1 mensagem não lida "
	}
	return fmt.Sprintf(" %d mensagens não lidas ", n)
}

// ---------- busca na conversa (ctrl+f) ----------

type chatFind struct {
	input   textinput.Model
	query   string // termo da última busca feita
	hits    []ipc.Message
	idx     int // achado mostrado (0 = o mais novo)
	loading bool
}

type findMsg struct {
	chat, query string
	hits        []ipc.SearchHit
}

func (m *model) openFind() tea.Cmd {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "procurar nesta conversa"
	in.SetWidth(max(10, m.geom().fieldW-30))
	f := &chatFind{input: in}
	m.finding = f
	m.layout()
	return f.input.Focus()
}

func (m *model) closeFind() {
	m.finding = nil
	m.layout()
	m.input.Focus()
}

func (m *model) runFind() tea.Cmd {
	f := m.finding
	q := strings.TrimSpace(f.input.Value())
	if len([]rune(q)) < 2 {
		m.setFlash("digite pelo menos 2 letras")
		return nil
	}
	f.query, f.loading, f.hits, f.idx = q, true, nil, 0
	chat := m.active
	var out []ipc.SearchHit
	return m.call(ipc.Request{Op: ipc.OpSearch, Chat: chat, Text: q, Limit: 500}, &out,
		func() tea.Msg { return findMsg{chat: chat, query: q, hits: out} })
}

func (m *model) onFind(msg findMsg) tea.Cmd {
	f := m.finding
	if f == nil || msg.chat != m.active || msg.query != f.query {
		return nil
	}
	f.loading = false
	for _, h := range msg.hits {
		f.hits = append(f.hits, h.Message)
	}
	if len(f.hits) == 0 {
		return nil
	}
	return m.showFound()
}

// showFound seleciona o achado atual: se já está carregado, só rola até
// ele; senão carrega as mensagens em volta.
func (m *model) showFound() tea.Cmd {
	f := m.finding
	h := f.hits[f.idx]
	for i, x := range m.msgs {
		if x.ID == h.ID {
			m.msgSel = i
			m.refreshMessages(false)
			m.ensureSelVisible()
			return nil
		}
	}
	return m.loadAround(m.active, h.ID, h.TS)
}

func (m *model) keyFind(msg tea.KeyPressMsg) tea.Cmd {
	f := m.finding
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+f":
		m.closeFind()
		return nil
	case "enter", "up":
		// enter com termo novo busca; com o mesmo termo, vai para o
		// achado mais antigo seguinte (como o ↑).
		if strings.TrimSpace(f.input.Value()) != f.query || msg.String() == "enter" && len(f.hits) == 0 {
			return m.runFind()
		}
		if f.idx < len(f.hits)-1 {
			f.idx++
			return m.showFound()
		}
		m.setFlash("esse é o mais antigo")
		return nil
	case "shift+enter", "down":
		if f.idx > 0 {
			f.idx--
			return m.showFound()
		}
		m.setFlash("esse é o mais recente")
		return nil
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return cmd
}

func (m *model) viewFind(w int) string {
	f := m.finding
	status := ""
	switch {
	case f.loading:
		status = "procurando…"
	case f.query != "" && len(f.hits) == 0:
		status = "nada encontrado"
	case len(f.hits) > 0:
		status = fmt.Sprintf("%d de %d", f.idx+1, len(f.hits))
		if len(f.hits) == 500 {
			status += "+"
		}
	}
	line := "  " + sGreen.Render(icons.search) + " " + f.input.View() + "  " + sText.Render(status) + "   " +
		sText2.Render("enter/↑ mais antigo · ↓ mais novo · esc fecha")
	return "\n" + ansi.Truncate(line, w, "…")
}

// ---------- trocar de conversa ----------

// switchChat abre a conversa vizinha na lista (d = -1 acima, +1 abaixo).
func (m *model) switchChat(d int) tea.Cmd {
	if len(m.list) == 0 {
		return nil
	}
	i := -1
	for j, c := range m.list {
		if c.JID == m.active {
			i = j
		}
	}
	j := i + d
	if i < 0 {
		j = 0
	}
	if j < 0 || j >= len(m.list) || strings.HasPrefix(m.list[j].JID, hitMarker) || strings.HasPrefix(m.list[j].JID, newChatMarker) {
		return nil
	}
	m.cursor = j
	return m.open(m.list[j])
}

// nextUnread abre a próxima conversa com mensagem não lida (depois da
// atual, dando a volta), pulando as silenciadas.
func (m *model) nextUnread() tea.Cmd {
	start := 0
	for j, c := range m.list {
		if c.JID == m.active {
			start = j + 1
		}
	}
	for k := 0; k < len(m.list); k++ {
		j := (start + k) % len(m.list)
		c := m.list[j]
		if c.Unread > 0 && !c.Muted && c.JID != m.active {
			m.cursor = j
			return m.open(c)
		}
	}
	m.setFlash("nenhuma conversa com mensagem não lida")
	return nil
}
