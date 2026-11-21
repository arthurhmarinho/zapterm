package tui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Anexos: vários de uma vez (seletor ou arrastar para o kitty) e as opções
// de envio da foto: HD (alt+q), como figurinha (alt+t) e visualização
// única (alt+o).

// setAttachments prepara os arquivos para o painel de envio.
func (m *model) setAttachments(paths []string) {
	var list []attachment
	for _, p := range paths {
		a, err := fileAttachment(p)
		if err != nil {
			m.setFlash(err.Error())
			continue
		}
		list = append(list, a)
	}
	if len(list) == 0 {
		return
	}
	first := list[0]
	first.more = list[1:]
	m.attach, m.picking = &first, false
	m.layout()
}

// droppedFiles: o kitty cola o caminho do arquivo solto na janela (às vezes
// como file://). Se todo o texto colado for de arquivos que existem, vira
// anexo; senão é texto comum.
func droppedFiles(text string) []string {
	if files := filesIn(strings.Split(strings.TrimSpace(text), "\n")); files != nil {
		return files
	}
	// Vários numa linha só, com aspas ou barra invertida, como no shell.
	return filesIn(shellWords(strings.TrimSpace(text)))
}

// shellWords separa palavras como o shell: espaço separa, 'aspas',
// "aspas" e \ escapam.
func shellWords(s string) []string {
	var out []string
	var cur strings.Builder
	quote, has := rune(0), false
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc, has = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

func filesIn(items []string) []string {
	var out []string
	for _, l := range items {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "file://") {
			u, err := url.Parse(l)
			if err != nil {
				return nil
			}
			l = u.Path
		}
		l = strings.Trim(l, `'"`)
		if !filepath.IsAbs(l) {
			return nil
		}
		if st, err := os.Stat(l); err != nil || st.IsDir() {
			return nil
		}
		out = append(out, l)
	}
	return out
}

func isVideoName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mkv", ".webm", ".mov":
		return true
	}
	return false
}

// keyAttach: as opções do painel de envio; false = não era delas.
func (m *model) keyAttach(s string) bool {
	a := m.attach
	single := len(a.more) == 0
	switch s {
	case "alt+q":
		if a.image {
			a.hd = !a.hd
			for i := range a.more {
				a.more[i].hd = a.hd
			}
		}
	case "alt+t":
		if a.image && single {
			a.sticker = !a.sticker
			a.once = false
		}
	case "alt+o":
		if single && (a.image || isVideoName(a.name)) {
			a.once = !a.once
			a.sticker = false
		}
	default:
		return false
	}
	return true
}

// attachOptions: o que vai em Request.Options.
func (a attachment) options() []string {
	var o []string
	if a.hd {
		o = append(o, "hd")
	}
	if a.sticker {
		o = append(o, "sticker")
	}
	if a.once {
		o = append(o, "once")
	}
	return o
}

// sendAttachments manda todos em sequência; a legenda vai no primeiro.
func (m *model) sendAttachments(caption string) tea.Cmd {
	a := m.attach
	all := append([]attachment{*a}, a.more...)
	reply := ""
	if m.replyTo != nil {
		reply = m.replyRef()
	}
	chat, cli := m.active, m.cli
	m.attach, m.replyTo, m.msgSel = nil, nil, -1
	m.input.Reset()
	m.layout()
	if len(all) == 1 {
		m.setFlash("enviando " + a.name + "…")
	} else {
		m.setFlash(fmt.Sprintf("enviando %d arquivos…", len(all)))
	}
	return func() tea.Msg {
		for i, x := range all {
			req := ipc.Request{Op: ipc.OpSendFile, Chat: chat, Path: x.path, Options: x.options()}
			if i == 0 {
				req.Text, req.ReplyTo = caption, reply
			}
			if err := cli.Call(req, nil); err != nil {
				return errMsg{fmt.Errorf("não enviou %s: %w", x.name, err)}
			}
		}
		return nil
	}
}

// attachHint: as opções que valem para o anexo, a ligada em destaque.
func (m *model) attachHint() string {
	a := m.attach
	opt := func(key, label string, on bool) string {
		if on {
			return sGreen.Bold(true).Render(key + " " + label + " ✓")
		}
		return sText2.Render(key + " " + label)
	}
	var parts []string
	if a.image {
		parts = append(parts, opt("alt+q", "HD", a.hd))
	}
	if a.image && len(a.more) == 0 {
		parts = append(parts, opt("alt+t", "figurinha", a.sticker))
	}
	if len(a.more) == 0 && (a.image || isVideoName(a.name)) {
		parts = append(parts, opt("alt+o", "visualização única", a.once))
	}
	return strings.Join(parts, sText2.Render(" · "))
}
