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

const mediaCellW, mediaCellH = 14, 7

func (m *model) openProfile() tea.Cmd {
	if m.active == "" {
		return nil
	}
	m.profileOpen, m.profile, m.profileSel = true, nil, 0
	chat := m.active
	var out ipc.Profile
	return m.call(ipc.Request{Op: ipc.OpProfile, Chat: chat}, &out, func() tea.Msg { return profileMsg{&out} })
}

func (m *model) closeProfile() {
	m.profileOpen, m.profile = false, nil
	m.input.Focus()
}

func (m *model) keyProfile(s string) tea.Cmd {
	n := 0
	if m.profile != nil {
		n = len(m.profile.Media)
	}
	per := m.mediaPerRow()
	switch s {
	case "esc", "alt+i", "ctrl+c":
		m.closeProfile()
	case "left":
		m.profileSel = max(0, m.profileSel-1)
	case "right":
		m.profileSel = min(max(0, n-1), m.profileSel+1)
	case "up":
		m.profileSel = max(0, m.profileSel-per)
	case "down":
		m.profileSel = min(max(0, n-1), m.profileSel+per)
	case "enter", "ctrl+o":
		if n > 0 {
			return m.openProfileMedia(m.profile.Media[m.profileSel])
		}
	}
	return nil
}

func (m *model) mediaPerRow() int { return max(1, (m.geom0chatW()-4)/(mediaCellW+1)) }

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
	// O que foi baixado passa a aparecer na grade.
	if m.profile != nil {
		for i := range m.profile.Media {
			if m.profile.Media[i].ID == pm.msg.ID {
				m.profile.Media[i].Media = pm.md
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

	// Mídia: grade de miniaturas, a selecionada com borda de acento.
	lines = append(lines, "", "  "+sGreen.Bold(true).Render(fmt.Sprintf("Mídia (%d)", len(p.Media))))
	if len(p.Media) == 0 {
		lines = append(lines, "  "+sText2.Render("nenhuma foto ou vídeo nesta conversa"))
	} else {
		per := m.mediaPerRow()
		firstRow := max(0, m.profileSel/per-1) // mantém a selecionada à vista
		for r := firstRow; r < firstRow+2; r++ {
			var cells []string
			for c := 0; c < per; c++ {
				i := r*per + c
				if i >= len(p.Media) {
					break
				}
				cells = append(cells, m.mediaCell(p.Media[i], i == m.profileSel), " ")
			}
			if len(cells) > 0 {
				lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, append([]string{"  "}, cells...)...))
			}
		}
		lines = append(lines, "  "+sText2.Render("setas escolhem · enter abre"))
	}

	// Participantes (grupo), no espaço que sobrar.
	if len(p.Members) > 0 {
		lines = append(lines, "", "  "+sGreen.Bold(true).Render(fmt.Sprintf("Participantes (%d)", p.MemberCount)))
		used := strings.Count(strings.Join(lines, "\n"), "\n") + 1
		room := max(1, h-used-1)
		for i, mb := range p.Members {
			if i == room-1 && len(p.Members) > room {
				lines = append(lines, "  "+sText2.Render(fmt.Sprintf("… e mais %d", len(p.Members)-i)))
				break
			}
			l := "  " + sText.Render(mb.Name)
			if mb.Admin {
				l += "  " + sGreen.Render("admin")
			}
			lines = append(lines, l)
		}
	}
	return fitHeight(strings.Join(lines, "\n"), cDefault, w, h)
}

// mediaCell: miniatura (ou prévia baixada) numa célula com borda.
func (m *model) mediaCell(msg ipc.Message, selected bool) string {
	md := msg.Media
	if md == nil || md.Preview == "" {
		md = msg.Thumb
	}
	pic := ""
	if md != nil {
		pic = m.imgs.media(md, mediaCellW-2, mediaCellH-2)
	}
	if pic == "" {
		icon := icons.photo
		if msg.Kind == "video" {
			icon = icons.video
		}
		pic = sText2.Render(icon)
	}
	border := cText2
	if selected {
		border = cGreen
	}
	inner := lipgloss.Place(mediaCellW-2, mediaCellH-2, lipgloss.Center, lipgloss.Center, pic)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(inner)
}
