package tui

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestSnapshotLive desenha as telas da TUI como texto usando o daemon real,
// para conferir layout sem terminal. Só lê: o chat é aberto sem open(), que
// mandaria recibo de leitura, e nada é enviado.
//
//	ZAPTERM_LIVE=1 ZAPTERM_CHAT=<jid> go test ./internal/tui -run Snapshot -v
func TestSnapshotLive(t *testing.T) {
	if os.Getenv("ZAPTERM_LIVE") == "" {
		t.Skip()
	}
	cli, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	m := newModel(cli)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	m.Update(m.loadStatus()())
	dump := func(label string) {
		t.Logf("==== %s\n%s", label, ansi.Strip(m.View().Content))
	}
	dump("lista (Todos)")

	g := m.geom()
	click := func(x, y int) { m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) }
	click(m.tabStartX(tabArchived), g.tabsY)
	dump("aba Arquivados (clique)")
	m.Update(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})

	if c, ok := m.chatByJID(os.Getenv("ZAPTERM_CHAT")); ok {
		m.active, m.activeChat, m.focus = c.JID, c, paneChat
		m.layout()
		m.Update(m.loadMessages(m.active, 0)())
	}
	for _, r := range "oi, isso não vai ser enviado" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	dump("conversa: texto digitado, ↑↑, ctrl+r")
}
