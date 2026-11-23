package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Tela de login: o daemon sem sessão fica no estado "pairing" e manda o QR
// (e, se pedido, o código de 8 letras) no status. A tela ocupa o lugar da
// lista e do chat; pareou, o daemon sai do estado e a lista volta.

type pairCodeMsg string

// onStatus aplica o status do daemon (resposta de OpStatus ou evento).
func (m *model) onStatus(st ipc.Status) tea.Cmd {
	was := m.status
	m.status, m.pair = st.State, st.Pair
	if st.Me != "" {
		m.me, m.myName = st.Me, st.Name
	} else if st.State == "pairing" {
		m.me, m.myName = "", ""
	}
	if st.State == "logged_out" {
		m.closeMe()
	}
	switch {
	case st.State == "pairing" && was != "pairing":
		// Entrou na tela de login: nada aberto por cima dela.
		m.popup, m.menu, m.form, m.viewing, m.help = nil, nil, nil, nil, false
		m.stories, m.chans = nil, nil
		m.closeMe()
		m.loginQR = false
		m.input.Blur()
	case was == "pairing" && st.State != "pairing":
		// Pareou: a lista que veio durante o login era só o banco, sem os
		// nomes dos contatos. Carrega de novo, já com a sessão.
		m.form = nil
		m.input.Focus()
		m.pair = nil
		return tea.Batch(m.loadChats(), m.loadStatus())
	}
	return nil
}

func (m *model) onPairCode(code string) {
	m.flash = ""
	if m.pair == nil {
		m.pair = &ipc.PairInfo{}
	}
	m.pair.Code = code
	m.loginQR = false
}

func (m *model) keyLogin(s string) tea.Cmd {
	switch s {
	case "ctrl+c":
		return tea.Quit
	case "tab":
		return m.phoneForm()
	case "esc":
		if m.pair != nil && m.pair.Code != "" {
			m.loginQR = true // volta a mostrar o QR
		}
	}
	return nil
}

// phoneForm pede o número e depois o código de 8 letras ao WhatsApp.
func (m *model) phoneForm() tea.Cmd {
	f := &form{title: "Entrar com número de telefone", fields: []formField{
		m.newField("Número", "55 11 99999-9999 (com DDI e DDD)"),
	}}
	f.submit = func(f *form) tea.Cmd {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, f.fields[0].in.Value())
		if len(digits) < 8 {
			m.setFlash("digite o número completo, com o DDI e o DDD")
			return nil
		}
		m.setFlash("pedindo o código ao WhatsApp…")
		var code string
		return m.call(ipc.Request{Op: ipc.OpPairPhone, Text: digits}, &code, func() tea.Msg { return pairCodeMsg(code) })
	}
	return m.openForm(f)
}

// spaced: "ABCD-EFGH" → "A B C D - E F G H", para o código ler de longe.
func spaced(code string) string {
	return strings.Join(strings.Split(code, ""), " ")
}

func (m *model) viewLogin() string {
	var p ipc.PairInfo
	if m.pair != nil {
		p = *m.pair
	}
	steps := "No celular: WhatsApp → Aparelhos conectados → Conectar um aparelho"
	lines := []string{sBold.Render("zapterm · entrar no WhatsApp"), ""}
	hint := "tab: entrar com número de telefone · ctrl+c sai"
	switch {
	case p.Code != "" && !m.loginQR:
		lines = append(lines, sText.Render(steps+" →"),
			sText.Render("\"Conectar com número de telefone\" e digite o código:"), "",
			lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).Padding(1, 4).
				Render(sGreen.Bold(true).Render(spaced(p.Code))), "",
			sText2.Render("o celular vai mostrar uma notificação para confirmar"))
		hint = "tab: outro número · esc: mostrar o QR · ctrl+c sai"
	case p.QR == "":
		lines = append(lines, sText2.Render("conectando ao WhatsApp…"))
	default:
		lines = append(lines, sText.Render(steps), "")
		qr := qrBlocks(p.QR)
		w, h := lipgloss.Width(qr), lipgloss.Height(qr)
		if m.w < w+4 || m.h < h+9 {
			lines = append(lines, fg(cDanger).Render(fmt.Sprintf("a janela é pequena para o QR: aumente para pelo menos %d×%d (agora %d×%d)",
				w+4, h+9, m.w, m.h)), sText2.Render("ou entre pelo número de telefone"))
		} else {
			lines = append(lines, qr)
		}
	}
	if p.Err != "" {
		lines = append(lines, "", fg(cDanger).Render(ansi.Truncate(p.Err, m.w-4, "…")))
	}
	if m.flash != "" {
		lines = append(lines, "", fg(cDanger).Render(ansi.Truncate(m.flash, m.w-4, "…")))
	}
	lines = append(lines, "", sText2.Render(hint))
	if m.form != nil {
		lines = append(lines, m.viewMeForm(min(m.w-4, 70)))
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, lines...))
}
