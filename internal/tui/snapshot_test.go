package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestSnapshotLive desenha as telas da TUI como texto usando o daemon real,
// para conferir layout sem terminal. Só lê: o chat é aberto sem open(), que
// mandaria recibo de leitura.
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
	m := &model{cli: cli, vp: newVP(), input: newInput(), jump: newJump(), msgSel: -1}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(m.loadChats()())
	m.Update(m.loadStatus()())
	dump := func(label string) {
		t.Logf("==== %s\n%s", label, ansi.Strip(m.View().Content))
	}
	dump("lista")
	// abre o chat indicado sem mandar recibo de leitura
	for _, c := range m.chats {
		if c.JID == os.Getenv("ZAPTERM_CHAT") {
			m.active, m.activeName, m.activeGroup, m.focus = c.JID, c.Name, strings.HasSuffix(c.JID, "@g.us"), paneChat
		}
	}
	m.Update(m.loadMessages(m.active, 0)())
	dump("chat")
	m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	dump("resposta (cursor duas acima, r)")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	for _, r := range "debat" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	dump("jump")
}
