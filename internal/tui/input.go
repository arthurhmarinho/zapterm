package tui

import (
	"os/exec"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// ---------- teclado ----------

func (m *model) onKey(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	// [keys] do config: a tecla configurada vira a padrão da ação; a padrão
	// de uma ação trocada fica sem efeito (cai no texto).
	if def, ok := m.keyRemap[s]; ok {
		if def == "" {
			def = "\x00desligada"
		}
		s = def
	}
	if s != "ctrl+c" {
		m.quitArmed = false
	}
	if m.viewing != nil {
		switch s {
		case "o":
			if err := openExternal(m.viewing.media.Path); err != nil {
				m.setFlash("xdg-open: " + err.Error())
			}
		case "esc", "enter", "q", "ctrl+o", "ctrl+c", "space":
			m.viewing = nil
		}
		return nil
	}
	if m.help {
		m.help = false // qualquer tecla fecha a ajuda
		return nil
	}
	if m.menu != nil {
		return m.keyChatMenu(s)
	}
	if m.gallery != nil && m.profileOpen && !m.searching {
		return m.keyGallery(s)
	}
	if m.profileOpen && !m.searching {
		if s == "tab" || s == "shift+tab" || s == "ctrl+p" {
			m.closeProfile() // sair para a lista ou a busca fecha o perfil
		} else {
			return m.keyProfile(s)
		}
	}
	if m.searching {
		return m.keySearch(k)
	}
	if m.picking {
		return m.keyStickers(k)
	}
	if m.msgInfo != nil || m.qrView != "" {
		m.msgInfo, m.qrView = nil, "" // qualquer tecla fecha
		return nil
	}
	if m.form != nil {
		return m.keyForm(k)
	}
	if m.popup != nil {
		return m.keyPopup(s)
	}
	if m.voting != nil {
		return m.keyPoll(s)
	}
	if m.finding != nil {
		return m.keyFind(k)
	}
	if m.reacting {
		return m.keyReact(s)
	}
	if m.mention.open {
		if cmd, ok := m.keyMention(s); ok {
			return cmd
		}
	}
	if m.emoji.open {
		if cmd, ok := m.keyEmoji(s); ok {
			return cmd
		}
	}
	if m.attach != nil && m.keyAttach(s) {
		return nil
	}
	if m.slash.open {
		if cmd, ok := m.keySlash(s); ok {
			return cmd
		}
	}
	if m.recReview != nil {
		return m.keyReview(s)
	}
	if m.rec != nil {
		switch s {
		case "enter", "ctrl+t":
			return m.finishRec(true)
		case "esc", "ctrl+c":
			return m.finishRec(false)
		case "space":
			return m.reviewRec()
		}
		return nil // gravando: o resto das teclas fica de fora
	}

	// Áudio tocando e campo vazio: as setas para os lados andam 5 s.
	if m.play != nil && m.play.ctl != nil && m.input.Value() == "" && m.focus == paneChat {
		switch s {
		case "left":
			m.seekAudio(-5)
			return nil
		case "right":
			m.seekAudio(5)
			return nil
		}
	}
	switch s {
	case "alt+v":
		m.cycleSpeed()
		return nil
	case "alt+x":
		if m.play != nil {
			m.stopPlay()
			m.rerenderKeepingPlace()
		}
		return nil
	case "ctrl+c":
		// Como no Claude Code: limpa o que está escrito; com tudo vazio, o
		// segundo ctrl+c sai.
		if m.input.Value() != "" || m.replyTo != nil || m.attach != nil || m.editing != nil {
			m.input.Reset()
			m.replyTo, m.attach, m.editing = nil, nil, nil
			m.layout()
			return nil
		}
		if m.quitArmed {
			return tea.Quit
		}
		m.quitArmed = true
		m.setFlash("ctrl+c de novo para sair")
		return nil
	case "ctrl+f":
		if m.active != "" {
			return m.openFind()
		}
		m.startSearch()
		return nil
	case "ctrl+up":
		return m.editLast()
	case "alt+up":
		return m.switchChat(-1)
	case "alt+down":
		return m.switchChat(1)
	case "alt+n":
		return m.nextUnread()
	case "ctrl+p":
		m.startSearch()
		return nil
	case "ctrl+s":
		return m.openStickers()
	case "ctrl+t":
		return m.startRec()
	case "alt+i":
		return m.openProfile()
	case "alt+m":
		c, ok := m.selected()
		if m.focus == paneChat && m.active != "" {
			c, ok = m.activeChat, true
		}
		if ok {
			m.openChatMenu(c)
		}
		return nil
	case "f1", "alt+h":
		m.help = true
		return nil
	case "shift+up", "shift+down":
		if m.focus == paneChat && m.active != "" {
			d := 1
			if s == "shift+up" {
				d = -1
			}
			return m.extendSel(d)
		}
	case "alt+c":
		if _, _, ok := m.selRange(); ok {
			return m.copyRange()
		}
		return m.copySelected()
	case "alt+e":
		return m.startEdit()
	case "alt+d":
		if _, _, ok := m.selRange(); ok {
			return m.deleteRange()
		}
		return m.openDeleteMenu()
	case "alt+.":
		return m.openMsgMenu()
	case "alt+z":
		m.undoDelete()
		return nil
	case "alt+w":
		return m.startForward()
	case "ctrl+e":
		if m.active != "" && m.sel() >= 0 && m.msgs[m.sel()].Kind != "revoked" {
			m.reacting, m.picking = true, false
			m.layout()
		}
		return nil
	case "alt+f":
		return m.openAttachMenu()
	case "ctrl+v":
		if m.active == "" {
			m.setFlash("abra um chat antes de colar")
			return nil
		}
		return pasteClipboard()
	case "tab", "shift+tab":
		if m.focus == paneList && m.active != "" {
			m.focus = paneChat
		} else {
			m.focus = paneList
		}
		return nil
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5":
		return m.setTab(int(s[4] - '1'))
	case "alt+s":
		return m.toggleStar()
	case "alt+p":
		return m.jumpPin()
	case "alt+a":
		m.setArchived(!m.archived)
		return nil
	case "esc":
		switch {
		case m.editing != nil:
			m.editing = nil
			m.input.Reset()
			m.layout()
		case m.attach != nil:
			m.attach = nil
			m.layout()
		case m.replyTo != nil:
			m.replyTo = nil
			m.layout()
		case m.focus == paneChat && m.msgSel >= 0:
			m.setSel(len(m.msgs) - 1)
		case m.focus == paneChat:
			m.focus = paneList
		case m.archived:
			m.setArchived(false)
		case m.searchQuery() != "":
			m.stopSearch(true)
		}
		return nil
	case "ctrl+r":
		return m.startReply()
	case "ctrl+o":
		if m.active != "" {
			return m.openMedia(m.sel())
		}
		return nil
	case "enter":
		return m.onEnter()
	case "pgup":
		return m.scrollChat(-m.vp.Height() / 2)
	case "pgdown":
		return m.scrollChat(m.vp.Height() / 2)
	case "ctrl+home":
		if m.focus == paneList {
			m.moveCursor(-len(m.list))
			return nil
		}
		m.vp.GotoTop()
		m.selFromViewport(true)
		return m.maybeLoadOlder()
	case "ctrl+end":
		if m.focus == paneList {
			m.moveCursor(len(m.list))
			return nil
		}
		if m.gap {
			// Pulou para um resultado antigo: ir ao fim recarrega as últimas.
			m.gap, m.msgSel = false, -1
			return m.loadMessages(m.active, 0)
		}
		m.setSel(len(m.msgs) - 1)
		m.vp.GotoBottom()
		return nil
	case "up", "down":
		// Com texto de várias linhas, as setas andam dentro do texto.
		if !strings.Contains(m.input.Value(), "\n") {
			d := 1
			if s == "up" {
				d = -1
			}
			if m.focus == paneList {
				m.moveCursor(d)
				return nil
			}
			m.selAnchor = -1 // seta sem shift desfaz a seleção de várias
			m.setSel(m.sel() + d)
			return tea.Batch(m.maybeLoadOlder(), m.maybeLoadNewer())
		}
	}

	// Todo o resto é texto.
	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(k)
	m.layout() // a altura do campo acompanha o texto
	if m.input.Value() != before {
		m.updateSlash()
		return tea.Batch(cmd, m.noteTyping(), m.updateMention(), m.updateEmoji())
	}
	return cmd
}

func (m *model) keyStickers(k tea.KeyPressMsg) tea.Cmd {
	per := m.stickPerRow()
	n := len(m.stickers)
	switch k.String() {
	case "esc", "ctrl+s", "ctrl+c":
		m.closeStickers()
		return nil
	case "tab", "shift+tab":
		m.stickTab = 1 - m.stickTab
		return m.loadStickers(0)
	case "enter":
		return m.sendSticker(m.stickSel)
	case "left":
		m.stickSel--
	case "right":
		m.stickSel++
	case "up":
		m.stickSel -= per
	case "down":
		m.stickSel += per
	default:
		return nil
	}
	if n == 0 {
		m.stickSel = 0
		return nil
	}
	m.stickSel = max(0, min(m.stickSel, n-1))
	// Duas linhas visíveis: rola a grade para manter a escolhida à vista.
	row := m.stickSel / per
	if row < m.stickTop {
		m.stickTop = row
	} else if row > m.stickTop+1 {
		m.stickTop = row - 1
	}
	// Chegando na última linha carregada, busca a próxima página.
	if m.stickNext >= 0 && !m.stickLoad && m.stickSel >= len(m.stickers)-per {
		return m.loadStickers(m.stickNext)
	}
	return nil
}

func (m *model) keySearch(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		if m.forwarding != nil {
			m.forwarding, m.fwdTargets = nil, nil
			m.setFlash("encaminhamento cancelado")
		}
		m.pickingContact = false
		m.stopSearch(true)
		return nil
	case "alt+w":
		if m.forwarding != nil {
			return m.sendForward()
		}
	case "enter":
		c, ok := m.selected()
		if !ok {
			return nil
		}
		if m.forwarding != nil {
			if !strings.HasPrefix(c.JID, hitMarker) && !strings.HasPrefix(c.JID, newChatMarker) {
				m.toggleTarget(c)
			}
			return nil
		}
		if m.pickingContact {
			if c.Kind != "dm" || strings.HasPrefix(c.JID, hitMarker) || strings.HasPrefix(c.JID, newChatMarker) {
				m.setFlash("escolha uma pessoa (não grupo nem canal)")
				return nil
			}
			m.pickingContact = false
			m.stopSearch(true)
			m.focus = paneChat
			m.setFlash("mandando o contato de " + c.Name + "…")
			return m.send(ipc.Request{Op: ipc.OpSendContact, Chat: m.active, Msg: c.JID, Text: c.Name})
		}
		return m.open(c)
	case "up", "ctrl+k":
		m.moveCursor(-1)
		return nil
	case "down", "ctrl+j", "ctrl+n", "tab":
		m.moveCursor(1)
		return nil
	}
	var cmd tea.Cmd
	before := m.search.Value()
	m.search, cmd = m.search.Update(k)
	if m.search.Value() != before {
		m.cursor, m.offset = 0, 0
		m.applyFilter()
		return tea.Batch(cmd, m.scheduleSearch())
	}
	return cmd
}

func (m *model) onEnter() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if e := m.editing; e != nil {
		m.editing = nil
		m.input.Reset()
		m.layout()
		return m.call(ipc.Request{Op: ipc.OpEdit, Chat: e.Chat, Msg: e.ID, Text: text}, nil, nil)
	}
	if m.attach != nil {
		// send_file decide pelo tipo: foto, vídeo, áudio ou documento.
		return m.sendAttachments(text)
	}
	if text == "" {
		if m.focus == paneList {
			if c, ok := m.selected(); ok {
				return m.open(c)
			}
		}
		return nil
	}
	if m.active == "" {
		m.setFlash("abra um chat antes de enviar")
		return nil
	}
	text, mentions := m.resolveMentions(text)
	req := ipc.Request{Op: ipc.OpSend, Chat: m.active, Text: text, Mentions: mentions}
	if m.replyTo != nil {
		req.ReplyTo = m.replyRef()
	}
	m.replyTo, m.msgSel = nil, -1
	m.input.Reset()
	m.layout()
	// Mandar a mensagem já encerra o "digitando" do outro lado.
	m.typingChat, m.typingSentAt = "", time.Time{}
	return m.send(req)
}

func (m *model) startReply() tea.Cmd {
	i := m.sel()
	if m.active == "" || i < 0 {
		return nil
	}
	msg := m.msgs[i]
	m.replyTo = &msg
	m.layout()
	return nil
}

func (m *model) scrollChat(d int) tea.Cmd {
	if d < 0 {
		m.vp.ScrollUp(-d)
	} else {
		m.vp.ScrollDown(d)
	}
	if m.focus == paneChat {
		m.selFromViewport(d < 0)
	}
	return tea.Batch(m.maybeLoadOlder(), m.maybeLoadNewer())
}

type chatNames []ipc.Chat

func (c chatNames) String(i int) string { return c[i].Name }
func (c chatNames) Len() int            { return len(c) }

func fuzzyChats(q string, chats []ipc.Chat) []int {
	var out []int
	for _, r := range fuzzy.FindFrom(q, chatNames(chats)) {
		out = append(out, r.Index)
	}
	return out
}

// ---------- mouse ----------

func (m *model) onClick(c tea.MouseClickMsg) tea.Cmd {
	if m.menu != nil {
		m.menu = nil // clique fora fecha o menu
		return nil
	}
	if m.viewing != nil {
		m.viewing = nil
		return nil
	}
	g := m.geom()
	x, y := c.X, c.Y
	if x < g.listW {
		return m.clickList(g, x, y, c)
	}
	if m.searching {
		m.stopSearch(false)
	}
	if m.active == "" {
		return nil
	}
	if m.profileOpen {
		switch {
		case y <= 1 && m.gallery != nil:
			m.gallery = nil // seta de voltar da galeria: volta ao perfil
		case y <= 1:
			m.closeProfile() // clique na seta de voltar
		case m.gallery == nil && y >= m.profileMediaY:
			return m.openGallery(0) // clique em "Mídia, links e docs"
		}
		return nil
	}
	if y == headerH && m.pinBarH() > 0 {
		return m.jumpPin() // clique na faixa da fixada
	}
	if y < headerH {
		return m.openProfile() // clique no cabeçalho, como no app
	}
	if m.picking && y >= g.extraY && y < g.inputY {
		if i := m.stickerAt(g, x, y); i >= 0 {
			return m.sendSticker(i)
		}
		return nil
	}
	switch {
	case y >= g.vpY && y < g.vpY+g.vpH:
		wasChat := m.focus == paneChat
		m.focus = paneChat
		i := m.msgAtLine(y - g.vpY + m.vp.YOffset())
		if i < 0 {
			return nil
		}
		if c.Button == tea.MouseRight {
			m.setSel(i)
			return m.openMsgMenu() // botão direito: menu da mensagem
		}
		// Clique na mensagem já selecionada abre a mídia dela.
		if wasChat && i == m.sel() {
			return m.openMedia(i)
		}
		m.setSel(i)
	case y >= g.inputY:
		m.focus = paneChat
		// Botões dentro da caixa: figurinha e clipe.
		switch {
		case abs(x-g.stickerBtnX) <= 1:
			return m.openStickers()
		case abs(x-g.clipBtnX) <= 1:
			return m.openAttachMenu()
		}
	}
	return nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// keyReact: 1–6 escolhem a reação, 0 tira a sua, esc fecha.
func (m *model) keyReact(s string) tea.Cmd {
	close := func() {
		m.reacting, m.reactQuery = false, ""
		m.layout()
	}
	react := func(emoji string) tea.Cmd {
		i := m.sel()
		close()
		if i < 0 {
			return nil
		}
		if emoji != "" {
			m.useEmoji(emoji)
		}
		return m.call(ipc.Request{Op: ipc.OpReact, Chat: m.active, Msg: m.msgs[i].ID, Text: emoji}, nil, nil)
	}
	choices := m.reactChoices()
	switch {
	case s == "esc" || s == "ctrl+e" || s == "ctrl+c":
		if m.reactQuery != "" && s == "esc" {
			m.reactQuery = "" // esc com busca: limpa a busca primeiro
			return nil
		}
		close()
		return nil
	case s == "0" && m.reactQuery == "":
		return react("")
	case len(s) == 1 && s[0] >= '1' && s[0] <= '9':
		if i := int(s[0] - '1'); i < len(choices) {
			return react(choices[i])
		}
	case s == "enter":
		if len(choices) > 0 && m.reactQuery != "" {
			return react(choices[0])
		}
	case s == "backspace":
		if r := []rune(m.reactQuery); len(r) > 0 {
			m.reactQuery = string(r[:len(r)-1])
		}
	case s == "space":
		if m.reactQuery != "" {
			m.reactQuery += " "
		}
	case len([]rune(s)) == 1 && unicode.IsLetter([]rune(s)[0]):
		m.reactQuery += s // digitar busca entre todos os emojis
	}
	return nil
}

// editLast (ctrl+↑): edita a sua última mensagem de texto, como no app.
func (m *model) editLast() tea.Cmd {
	for i := len(m.msgs) - 1; i >= 0; i-- {
		x := m.msgs[i]
		if x.FromMe && x.Kind == "text" {
			m.msgSel = i
			m.refreshMessages(false)
			m.ensureSelVisible()
			return m.startEdit()
		}
	}
	m.setFlash("nenhuma mensagem sua para editar aqui")
	return nil
}

func (m *model) clickList(g geometry, x, y int, c tea.MouseClickMsg) tea.Cmd {
	m.focus = paneList
	switch {
	case y == g.titleY && m.archived:
		m.setArchived(false)
	case y >= g.searchY && y < g.searchY+3:
		m.startSearch()
	case y == g.chipsY && !m.archived:
		if t := m.chipAt(x); t >= 0 {
			if m.searching || m.searchQuery() != "" {
				m.stopSearch(true)
			}
			return m.setTab(t)
		}
	case g.archRowY >= 0 && y == g.archRowY:
		m.setArchived(true)
	case y >= g.itemsY && y < g.itemsY+g.listRows*itemH:
		i := m.offset + (y-g.itemsY)/itemH
		if i < len(m.list) {
			m.cursor = i
			if c.Button == tea.MouseRight {
				m.openChatMenu(m.list[i]) // botão direito: menu do chat
				return nil
			}
			return m.open(m.list[i])
		}
	}
	return nil
}

func (m *model) onWheel(w tea.MouseWheelMsg) tea.Cmd {
	if m.viewing != nil {
		return nil
	}
	g := m.geom()
	d := 3
	switch w.Button {
	case tea.MouseWheelUp:
		d = -3
	case tea.MouseWheelDown:
	default:
		return nil
	}
	if w.X < g.listW {
		// A roda rola a lista sem mexer na seleção.
		step := 1
		if d < 0 {
			step = -1
		}
		m.offset = max(0, min(m.offset+step, len(m.list)-m.listRows()))
		return nil
	}
	if d < 0 {
		m.vp.ScrollUp(-d)
	} else {
		m.vp.ScrollDown(d)
	}
	return tea.Batch(m.maybeLoadOlder(), m.maybeLoadNewer())
}

// ---------- ações na mensagem selecionada ----------

func (m *model) selectedMsg() (ipc.Message, bool) {
	i := m.sel()
	if m.active == "" || i < 0 {
		return ipc.Message{}, false
	}
	return m.msgs[i], true
}

// copySelected copia o texto da mensagem (com a formatação do WhatsApp,
// como o app copia) para a área de transferência.
func (m *model) copySelected() tea.Cmd {
	msg, ok := m.selectedMsg()
	if !ok || msg.Kind == "revoked" {
		return nil
	}
	text := msg.Text
	if msg.Kind != "text" {
		text = stripTag(text) // legenda, sem o marcador "[imagem]"
	}
	if text == "" {
		m.setFlash("essa mensagem não tem texto")
		return nil
	}
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		m.setFlash("wl-copy: " + err.Error())
		return nil
	}
	m.setFlash("copiado")
	return nil
}

func (m *model) startEdit() tea.Cmd {
	msg, ok := m.selectedMsg()
	switch {
	case !ok:
		return nil
	case !msg.FromMe || msg.Kind != "text":
		m.setFlash("só dá para editar mensagem de texto sua")
		return nil
	case time.Since(time.UnixMilli(msg.TS)) > 15*time.Minute:
		m.setFlash("o WhatsApp só deixa editar até 15 minutos depois")
		return nil
	}
	m.editing, m.replyTo, m.attach = &msg, nil, nil
	m.input.SetValue(msg.Text)
	m.layout()
	return nil
}
