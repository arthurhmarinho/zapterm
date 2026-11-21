package tui

import (
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mdp/qrterminal/v3"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Link de convite: copiar, mostrar o QR e redefinir (perfil do grupo,
// alt+c); e abrir um link chat.whatsapp.com recebido (ctrl+o), que mostra
// o grupo e oferece entrar.

type adminMsg struct {
	chat  string
	admin bool
}

func (m *model) loadAdmin(chat string) tea.Cmd {
	if !strings.HasSuffix(chat, "@g.us") {
		return nil
	}
	var out bool
	return m.call(ipc.Request{Op: ipc.OpAmAdmin, Chat: chat}, &out, func() tea.Msg { return adminMsg{chat, out} })
}

// openInviteMenu: o que fazer com o link de convite do grupo aberto.
func (m *model) openInviteMenu() tea.Cmd {
	chat := m.active
	get := func(reset bool, then func(link string) tea.Msg) func() tea.Cmd {
		return func() tea.Cmd {
			text := ""
			if reset {
				text = "reset"
			}
			var link string
			return m.call(ipc.Request{Op: ipc.OpInviteLink, Chat: chat, Text: text}, &link, func() tea.Msg { return then(link) })
		}
	}
	opts := []popOpt{
		{"Copiar link", get(false, func(l string) tea.Msg { return inviteMsg{l} })},
		{"Mostrar QR", get(false, func(l string) tea.Msg { return qrMsg{l} })},
	}
	if m.profile != nil && m.profile.IAmAdmin {
		opts = append(opts, popOpt{"Redefinir link…", func() tea.Cmd {
			m.popup = &popMenu{title: "Redefinir o link? O link atual para de funcionar.", opts: []popOpt{
				{"Redefinir", get(true, func(l string) tea.Msg { return inviteMsg{l} })},
				{"Cancelar", func() tea.Cmd { return nil }},
			}}
			return nil
		}})
	}
	opts = append(opts, popOpt{"Cancelar", func() tea.Cmd { return nil }})
	m.popup = &popMenu{title: "Link de convite", opts: opts}
	return nil
}

type qrMsg struct{ link string }

// qrBox desenha o QR em meio-bloco, preto no branco (fixo: QR invertido
// não é lido por todo celular, e o tema do terminal pode ser escuro).
func qrBox(link string) string {
	var buf bytes.Buffer
	qrterminal.GenerateWithConfig(link, qrterminal.Config{
		Level: qrterminal.L, Writer: &buf, HalfBlocks: true, QuietZone: 1,
		BlackChar: qrterminal.BLACK_BLACK, WhiteChar: qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE, WhiteBlackChar: qrterminal.WHITE_BLACK,
	})
	code := strings.TrimRight(buf.String(), "\n")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cGreen).Padding(0, 1).
		Render(sBold.Render("Link de convite") + "\n" + sText2.Render(link) + "\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#ffffff")).Render(code) +
			"\n\n" + sText2.Render("qualquer tecla fecha"))
}

// ---------- link recebido ----------

func isInviteLink(u string) bool {
	p, err := url.Parse(u)
	return err == nil && strings.HasSuffix(p.Host, "chat.whatsapp.com") && strings.Trim(p.Path, "/") != ""
}

type groupLinkMsg struct {
	link string
	g    *ipc.GroupPreview
}

func (m *model) openInviteLink(link string) tea.Cmd {
	m.setFlash("perguntando ao WhatsApp que grupo é esse…")
	var out ipc.GroupPreview
	return m.call(ipc.Request{Op: ipc.OpGroupByLink, Text: link}, &out, func() tea.Msg { return groupLinkMsg{link, &out} })
}

func (m *model) onGroupLink(msg groupLinkMsg) {
	m.flash = ""
	g := msg.g
	title := fmt.Sprintf("%s · %d participantes", g.Name, g.Members)
	if t := strings.TrimSpace(g.Topic); t != "" {
		title += "\n" + oneLine(t)
	}
	var opts []popOpt
	if g.Joined {
		title += "\nvocê já participa"
		opts = append(opts, popOpt{"Abrir a conversa", func() tea.Cmd { return m.open(m.chatFor(g.JID, g.Name)) }})
	} else {
		link := msg.link
		opts = append(opts, popOpt{"Entrar no grupo", func() tea.Cmd {
			var jid string
			return m.call(ipc.Request{Op: ipc.OpJoinLink, Text: link}, &jid, func() tea.Msg {
				return joinedMsg{ipc.Chat{JID: jid, Name: g.Name, Kind: "group", IsGroup: true, LastStatus: -1}}
			})
		}})
	}
	opts = append(opts, popOpt{"Cancelar", func() tea.Cmd { return nil }})
	m.popup = &popMenu{title: title, opts: opts}
}

type joinedMsg struct{ chat ipc.Chat }

func copyText(text string) error {
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
