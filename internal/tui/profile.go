package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Tela de perfil (alt+i ou clique no cabeçalho da conversa): foto grande,
// nome, número e recado, ou descrição e participantes do grupo, e a grade
// com as fotos e vídeos trocados na conversa.

type (
	profileMsg      struct{ p *ipc.Profile }
	profileMediaMsg struct {
		msg ipc.Message
		md  *ipc.Media
	}
)

func (m *model) openProfile() tea.Cmd {
	if m.active == "" {
		return nil
	}
	m.profileOpen, m.profile, m.gallery, m.memberSel = true, nil, nil, -1
	chat := m.active
	var out ipc.Profile
	return m.call(ipc.Request{Op: ipc.OpProfile, Chat: chat}, &out, func() tea.Msg { return profileMsg{&out} })
}

func (m *model) closeProfile() {
	m.profileOpen, m.profile, m.gallery = false, nil, nil
	m.input.Focus()
}

// keyProfile: enter abre a galeria; num grupo, ↓/↑ andam pelos
// participantes e enter num deles abre o menu dele; alt+c copia o link de
// convite.
func (m *model) keyProfile(s string) tea.Cmd {
	var members []ipc.ProfileMember
	if m.profile != nil {
		members = m.profile.Members
	}
	switch s {
	case "esc", "alt+i", "ctrl+c":
		if m.memberSel >= 0 && s == "esc" {
			m.memberSel = -1
			return nil
		}
		m.closeProfile()
	case "down":
		if len(members) == 0 {
			return m.openGallery(0)
		}
		m.memberSel = min(len(members)-1, m.memberSel+1)
	case "up":
		m.memberSel = max(-1, m.memberSel-1)
	case "right":
		return m.openGallery(0)
	case "enter", "ctrl+o":
		if m.memberSel >= 0 && m.memberSel < len(members) {
			return m.openMemberMenu(members[m.memberSel])
		}
		return m.openGallery(0)
	case "alt+c":
		if m.profile != nil && m.profile.Kind == "group" {
			return m.openInviteMenu()
		}
	}
	return nil
}

type inviteMsg struct{ link string }

func (m *model) onInvite(msg inviteMsg) {
	if err := copyText(msg.link); err != nil {
		m.setFlash("wl-copy: " + err.Error())
		return
	}
	m.setFlash("link de convite copiado: " + msg.link)
}

// openMemberMenu: conversar e, se você é admin, promover, rebaixar ou
// remover (com confirmação).
func (m *model) openMemberMenu(mb ipc.ProfileMember) tea.Cmd {
	chat := m.active
	act := func(action, done string) func() tea.Cmd {
		return func() tea.Cmd {
			return m.call(ipc.Request{Op: ipc.OpGroupMember, Chat: chat, Msg: mb.Raw, Text: action}, nil,
				func() tea.Msg { return flashMsg(done) })
		}
	}
	opts := []popOpt{{"Conversar com " + firstName(mb.Name), func() tea.Cmd {
		m.closeProfile()
		return m.open(m.chatFor(mb.JID, mb.Name))
	}}}
	if m.profile.IAmAdmin {
		if mb.Admin {
			opts = append(opts, popOpt{"Tirar de admin", act("demote", mb.Name+" não é mais admin")})
		} else {
			opts = append(opts, popOpt{"Tornar admin", act("promote", mb.Name+" agora é admin")})
		}
		opts = append(opts, popOpt{"Remover do grupo…", func() tea.Cmd {
			m.popup = &popMenu{title: "Remover " + mb.Name + " do grupo?", opts: []popOpt{
				{"Remover", act("remove", mb.Name+" removido do grupo")},
				{"Cancelar", func() tea.Cmd { return nil }},
			}}
			return nil
		}})
	}
	opts = append(opts, popOpt{"Cancelar", func() tea.Cmd { return nil }})
	m.popup = &popMenu{title: mb.Name, opts: opts}
	return nil
}

// openProfileMedia: foto vai para a tela cheia; vídeo para o player. O que
// ainda não foi baixado é baixado antes.
func (m *model) openProfileMedia(msg ipc.Message) tea.Cmd {
	if msg.Media != nil {
		return func() tea.Msg { return profileMediaMsg{msg: msg, md: msg.Media} }
	}
	if !msg.HasRaw {
		m.setFlash("mídia de mensagem antiga (do pareamento) não pode ser baixada")
		return nil
	}
	m.setFlash("baixando…")
	var md ipc.Media
	return m.call(ipc.Request{Op: ipc.OpMedia, Chat: msg.Chat, Msg: msg.ID}, &md,
		func() tea.Msg { return profileMediaMsg{msg: msg, md: &md} })
}

func (m *model) onProfileMedia(pm profileMediaMsg) {
	m.flash = ""
	if pm.msg.Kind == "image" && pm.md.Preview != "" && m.imgs.enabled {
		m.viewing = &viewer{media: pm.md, caption: stripTag(pm.msg.Text)}
		return
	}
	if err := openExternal(pm.md.Path); err != nil {
		m.setFlash("abrir: " + err.Error())
	}
	// O que foi baixado passa a aparecer na prévia e na galeria.
	if m.profile != nil {
		for i := range m.profile.Media {
			if m.profile.Media[i].ID == pm.msg.ID {
				m.profile.Media[i].Media = pm.md
			}
		}
	}
	if g := m.gallery; g != nil {
		for i := range g.items {
			if g.items[i].ID == pm.msg.ID {
				g.items[i].Media = pm.md
			}
		}
	}
}

func (m *model) viewProfile(g geometry) string {
	w, h := g.chatW, g.footerY
	center := func(s string) string { return lipgloss.PlaceHorizontal(w, lipgloss.Center, s) }
	top := "  " + sText.Render(icons.back) + "  " + sBold.Render("Dados do contato")
	if m.activeChat.Kind == "group" {
		top = "  " + sText.Render(icons.back) + "  " + sBold.Render("Dados do grupo")
	}
	lines := []string{top + "   " + sText2.Render("esc fecha"), ""}
	p := m.profile
	if p == nil {
		lines = append(lines, "", center(sText2.Render("carregando perfil…")))
		return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
	}

	// Foto grande (ou o círculo com iniciais, em tamanho grande).
	pic := ""
	if p.Photo != nil {
		pic = m.imgs.media(p.Photo, 24, 12)
	}
	if pic == "" {
		pic = m.avatar(p.JID, p.Name, 16, 8)
	}
	for _, l := range strings.Split(pic, "\n") {
		lines = append(lines, center(l))
	}
	lines = append(lines, "", center(sBold.Render(ansi.Truncate(p.Name, w-4, "…"))))

	var sub []string
	switch {
	case p.Kind == "group":
		sub = append(sub, fmt.Sprintf("Grupo · %d participantes", p.MemberCount))
		if p.Created > 0 {
			sub = append(sub, "criado em "+time.UnixMilli(p.Created).Format("02/01/2006"))
		}
	case p.Phone != "":
		sub = append(sub, p.Phone)
	}
	if p.Business != "" {
		sub = append(sub, "empresa verificada: "+p.Business)
	}
	if len(sub) > 0 {
		lines = append(lines, center(sText2.Render(strings.Join(sub, " · "))))
	}
	if p.Note != "" {
		lines = append(lines, "", center(sText2.Italic(true).Render(ansi.Truncate(p.Note, w-4, "…"))))
	}
	if p.About != "" {
		lines = append(lines, "")
		for _, l := range strings.Split(formatText(p.About, min(w-8, 70), sText), "\n") {
			lines = append(lines, center(l))
		}
	}

	// Mídia, links e docs: resumo e uma fileira de prévia; enter (ou
	// clique) abre a galeria completa.
	total := 0
	var parts []string
	for _, t := range ipc.GalleryTabs {
		if n := p.Counts[t]; n > 0 {
			total += n
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(galleryLabels[t])))
		}
	}
	m.profileMediaY = len(strings.Split(strings.Join(lines, "\n"), "\n")) + 1
	lines = append(lines, "", "  "+sGreen.Bold(true).Render("Mídia, links e docs")+"  "+sText2.Render(fmt.Sprint(total))+
		"   "+sText2.Render("enter abre"+" "+icons.forward))
	if total == 0 {
		lines = append(lines, "  "+sText2.Render("nada trocado nesta conversa ainda"))
	} else {
		lines = append(lines, "  "+sText2.Render(ansi.Truncate(strings.Join(parts, " · "), w-4, "…")))
		if len(p.Media) > 0 {
			per, cw, ch := m.galleryGrid()
			var cells []string
			for i := 0; i < min(per, len(p.Media)); i++ {
				cells = append(cells, m.galleryCell(p.Media[i], false, cw, ch))
				if i < min(per, len(p.Media))-1 {
					cells = append(cells, " ")
				}
			}
			row := lipgloss.JoinHorizontal(lipgloss.Top, append([]string{"  "}, cells...)...)
			lines = append(lines, strings.Split(row, "\n")...)
		}
	}

	// Participantes (grupo), no espaço que sobrar.
	if len(p.Members) > 0 {
		hint := "↓ escolhe · enter: conversar"
		if p.IAmAdmin {
			hint += ", admin, remover"
		}
		hint += " · alt+c link de convite"
		lines = append(lines, "", "  "+sGreen.Bold(true).Render(fmt.Sprintf("Participantes (%d)", p.MemberCount))+"   "+sText2.Render(hint))
		used := strings.Count(strings.Join(lines, "\n"), "\n") + 1
		room := max(1, h-used-1)
		// Janela que acompanha o selecionado.
		first := max(0, min(m.memberSel-room/2, len(p.Members)-room))
		for i := first; i < len(p.Members) && i < first+room; i++ {
			mb := p.Members[i]
			mark, st := "  ", sText
			if i == m.memberSel {
				mark, st = sGreen.Render("▌ "), sGreen.Bold(true)
			}
			l := mark + st.Render(mb.Name)
			if mb.Admin {
				l += "  " + sGreen.Render("admin")
			}
			lines = append(lines, l)
		}
	}
	return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
}
