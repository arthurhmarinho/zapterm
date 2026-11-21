// Package tui é a interface de terminal do zapterm, em Bubble Tea v2.
// Ela não fala com o WhatsApp: tudo passa pelo daemon via ipc.
//
// O visual imita o WhatsApp Web (tema escuro) e a navegação segue o Claude
// Code: o campo de escrita está sempre ativo, setas/PgUp/PgDn/Tab e o
// mouse navegam, atalhos usam ctrl/alt.
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

type pane int

const (
	paneList pane = iota
	paneChat
)

// Filtros ("pílulas") acima da lista. Arquivadas não é filtro: é uma
// linha no topo da lista que abre outra vista, como no app.
const (
	tabAll = iota
	tabUnread
	tabGroups
	tabChannels
	tabStarred // mensagens favoritas (não é filtro de chat: lista mensagens)
	numTabs
)

var tabNames = [numTabs]string{"Tudo", "Não lidas", "Grupos", "Canais", "★"}

const (
	pageSize        = 100
	itemH           = 3 // linhas por chat na lista
	avatarCols      = 6 // avatar da lista e do cabeçalho: 6×3 células
	avatarRows      = 3
	avatarSmallCols = 4 // avatar ao lado das mensagens em grupo: 4×2
	avatarSmallRows = 2
)

type attachment struct {
	path, preview string // original e PNG para o kitty (só foto)
	w, h          int
	image         bool // foto (prévia e legenda) ou arquivo qualquer
	name          string
	size          int64
}

type model struct {
	cli *ipc.Client

	w, h  int
	focus pane

	chats    []ipc.Chat // todos, como vieram do daemon
	list     []ipc.Chat // o que a lista mostra agora
	tab      int
	archived bool // vista "Arquivadas"
	cursor   int  // em list
	offset   int

	searching bool
	search    textinput.Model

	active     string // jid do chat aberto
	activeChat ipc.Chat
	msgs       []ipc.Message
	spans      [][2]int // linhas de cada balão no viewport
	msgSel     int      // mensagem sob o cursor; -1 = seguir a última
	replyTo    *ipc.Message
	attach     *attachment
	drafts     map[string]draft // o que ficou escrito em cada conversa

	// Seletor de figurinhas (ctrl+s)
	picking   bool
	stickers  []ipc.Sticker
	stickLoad bool
	stickSel  int
	stickTop  int             // primeira linha visível da grade
	stickTab  int             // favoritas ou recentes
	stickNext int             // de onde continuar a carregar (-1: acabou)
	autoAsked map[string]bool // figurinhas já pedidas para baixar sozinhas

	viewing  *viewer    // foto aberta em tela cheia
	reacting bool       // barra de reações (ctrl+e) aberta
	voting   *pollPanel // painel de votar na enquete (ctrl+o)

	audioSpeed float64 // velocidade dos áudios (alt+v), vale para os próximos

	unreadCount int       // não lidas ao abrir a conversa (divisor)
	unreadID    string    // primeira não lida: o divisor vai antes dela
	finding     *chatFind // busca na conversa (ctrl+f)

	emoji       emojiState // ":nome" no campo de escrita
	emojiRecent []string   // usados por último (primeiro = mais recente)
	reactQuery  string     // busca de emoji no painel de reações

	starred    []ipc.SearchHit // mensagens favoritas (filtro ★)
	pins       []ipc.Message   // fixadas da conversa aberta (faixa no topo)
	pinIdx     int
	popup      *popMenu       // menu da mensagem (alt+.) ou "apagar para quem?"
	msgInfo    *ipc.MsgInfo   // "Dados da mensagem" aberto
	pendingDel *pendingDelete // apagar para mim esperando os 5 s de desfazer
	recReview  *recReview     // gravação parada esperando você ouvir e decidir

	editing    *ipc.Message  // mensagem sua sendo editada (alt+e)
	forwarding []ipc.Message // a encaminhar (alt+w): a busca escolhe os destinos
	fwdTargets []ipc.Chat    // destinos marcados (até 5)
	selAnchor  int           // começo da seleção de várias (shift+↑/↓); -1 = sem
	help       bool          // tela de ajuda (F1)

	menu          *chatMenu         // menu do chat (alt+m)
	keyRemap      map[string]string // teclas do config → teclas padrão
	profileOpen   bool
	gallery       *gallery // "Mídia, links e docs" aberta sobre o perfil
	profileMediaY int      // linha da seção de mídia no perfil (clique abre a galeria)
	memberSel     int      // participante escolhido no perfil do grupo (-1 = nenhum)
	profile       *ipc.Profile
	pendingOpen   ipc.Chat // chat a abrir assim que possível (notificação, ZAPTERM_OPEN)

	hits       []ipc.SearchHit // resultados da busca no texto
	hitsQuery  string
	gap        bool // há mensagens mais novas não carregadas (pulou para um resultado)
	loadingNew bool
	mention    mentionState
	members    map[string][]ipc.Member // participantes por grupo (nil = pedido)
	mentioned  map[string]string       // \"@Nome\" escolhido → JID

	presence     map[string]*presenceState // digitando/online por chat
	typingSentAt time.Time                 // último "digitando" seu enviado
	typingChat   string                    // chat que recebeu esse aviso
	play         *playback                 // áudio tocando
	rec          *recording                // gravação em andamento
	loadingOld   bool
	noMoreOld    bool
	vp           viewport.Model
	input        textarea.Model

	imgs      *images
	avatars   map[string]*ipc.Media // jid -> foto (nil = sem foto ou buscando)
	avAsked   map[string]bool
	avQueue   []string
	fallbacks map[string]string // jid -> PNG com iniciais

	status  string
	me      string // seu JID e nome de perfil, para a foto no topo
	myName  string
	flash   string
	flashAt time.Time

	termFocused bool   // o kitty avisa quando a janela ganha/perde foco
	sentViewing string // último valor de "viewing" mandado ao daemon
	quitArmed   bool   // ctrl+c com campo vazio pede confirmação
	autoOpen    bool   // abrir o primeiro chat ao carregar (modo demonstração)
}

// ---------- mensagens internas ----------

type (
	chatsMsg []ipc.Chat
	msgsMsg  struct {
		chat  string
		msgs  []ipc.Message
		older bool
	}
	evMsg struct {
		cli *ipc.Client
		f   ipc.Frame
	}
	goneMsg      struct{ cli *ipc.Client }
	reconnectMsg struct {
		cli *ipc.Client
		err error
	}
	errMsg    struct{ err error }
	statusMsg ipc.Status
	avatarMsg struct {
		jid   string
		media *ipc.Media
	}
	mediaMsg struct {
		chat, id string
		media    *ipc.Media
		open     bool // abrir no programa externo quando chegar
		auto     bool // download automático (figurinha), sem mexer no aviso
		play     bool // tocar quando chegar (áudio)
	}
	attachMsg    attachment
	pasteTextMsg string
	stickersMsg  struct {
		page   ipc.StickerPage
		tab    int
		offset int
	}
)

func Run() error {
	cfgErr := applyConfig() // antes de tudo: cores e ícones valem desde o 1º quadro
	cli, err := connect()
	if err != nil {
		return err
	}
	m := newModel(cli)
	if cfgErr != nil {
		m.setFlash("config: " + cfgErr.Error())
	}
	m.autoOpen = os.Getenv("ZAPTERM_DEMO") != ""
	if jid := os.Getenv("ZAPTERM_OPEN"); jid != "" {
		m.pendingOpen = ipc.Chat{JID: jid, Name: jid, LastStatus: -1}
	}
	defer func() { m.cli.Close() }()
	_, err = tea.NewProgram(m).Run()
	m.cleanupAudio()
	m.imgs.cleanup()
	return err
}

func newModel(cli *ipc.Client) *model {
	m := &model{cli: cli, input: newInput(), search: newSearch(), vp: viewport.New(), emojiRecent: loadRecentEmoji(),
		status: "conectando", termFocused: true, msgSel: -1, selAnchor: -1, imgs: newImages(),
		avatars: map[string]*ipc.Media{}, avAsked: map[string]bool{}, fallbacks: map[string]string{},
		autoAsked: map[string]bool{}, presence: map[string]*presenceState{},
		members: map[string][]ipc.Member{}, mentioned: map[string]string{}}
	m.vp.MouseWheelEnabled = false // a roda é tratada em input.go
	m.keyRemap = cfg.KeyRemap()
	m.input.Focus()
	return m
}

func newInput() textarea.Model {
	in := textarea.New()
	in.Placeholder = "Digite uma mensagem · ctrl+v cola um print"
	in.ShowLineNumbers = false
	in.Prompt = ""
	in.CharLimit = 0
	in.DynamicHeight = true
	in.MinHeight = 1
	in.MaxHeight = 6
	// enter envia; shift+enter / alt+enter / ctrl+j quebram linha.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	in.SetHeight(1)
	st := in.Styles()
	for _, s := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		s.Base = s.Base.UnsetBackground()
		s.CursorLine = s.CursorLine.UnsetBackground().Foreground(cText)
		s.Text = s.Text.Foreground(cText)
		s.Placeholder = s.Placeholder.Foreground(cText2)
		s.EndOfBuffer = s.EndOfBuffer.Foreground(cBgHover)
	}
	in.SetStyles(st)
	return in
}

func newSearch() textinput.Model {
	t := textinput.New()
	t.Prompt = ""
	t.Placeholder = "Pesquisar conversa"
	return t
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.loadChats(), m.loadStatus(), waitEvent(m.cli))
}

// ---------- comandos (rodam fora do loop de UI) ----------

func waitEvent(cli *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-cli.Events()
		if !ok {
			return goneMsg{cli}
		}
		return evMsg{cli, f}
	}
}

// call faz um pedido ao daemon fora do loop de UI; wrap transforma a
// resposta (já decodificada em out) na mensagem que volta para o Update.
func (m *model) call(req ipc.Request, out any, wrap func() tea.Msg) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		if err := cli.Call(req, out); err != nil {
			return errMsg{err}
		}
		if wrap == nil {
			return nil
		}
		return wrap()
	}
}

func (m *model) loadChats() tea.Cmd {
	var out []ipc.Chat
	return m.call(ipc.Request{Op: ipc.OpChats}, &out, func() tea.Msg { return chatsMsg(out) })
}

func (m *model) loadStatus() tea.Cmd {
	var st ipc.Status
	return m.call(ipc.Request{Op: ipc.OpStatus}, &st, func() tea.Msg { return statusMsg(st) })
}

func (m *model) loadMessages(chat string, before int64) tea.Cmd {
	var out []ipc.Message
	return m.call(ipc.Request{Op: ipc.OpMessages, Chat: chat, Limit: pageSize, Before: before}, &out,
		func() tea.Msg { return msgsMsg{chat: chat, msgs: out, older: before > 0} })
}

func (m *model) markRead(chat string) tea.Cmd {
	return m.call(ipc.Request{Op: ipc.OpRead, Chat: chat}, nil, nil)
}

func (m *model) send(req ipc.Request) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		// A mensagem enviada volta como evento; aqui só interessa o erro.
		if err := cli.Call(req, nil); err != nil {
			return errMsg{fmt.Errorf("não enviou: %w", err)}
		}
		return nil
	}
}

// syncViewing conta ao daemon qual chat está na tela, para ele não notificar
// mensagem que você já está vendo.
func (m *model) syncViewing() tea.Cmd {
	want := ""
	if m.termFocused {
		want = m.active
	}
	if want == m.sentViewing {
		return nil
	}
	m.sentViewing = want
	return m.call(ipc.Request{Op: ipc.OpViewing, Chat: want}, nil, nil)
}

func reconnect() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		cli, err := connect()
		return reconnectMsg{cli, err}
	})
}

// ---------- update ----------

// Update envolve update: depois de cada mensagem, busca os avatares que o
// próximo quadro vai mostrar e manda ao kitty as imagens novas.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	m.prepareAvatars()
	if m.profileOpen && m.w > 0 {
		// Dispara as transmissões das fotos neste Update.
		if m.gallery != nil {
			m.viewGallery(m.geom())
		} else {
			m.viewProfile(m.geom())
		}
	}
	if v := m.viewing; v != nil && m.w > 0 {
		m.imgs.media(v.media, m.w-4, m.h-4) // mesma chamada do viewPhoto
	}
	if q := m.avQueue; len(q) > 0 {
		m.avQueue = nil
		cmds := []tea.Cmd{cmd}
		for _, jid := range q {
			cmds = append(cmds, m.loadAvatar(jid))
		}
		cmd = tea.Batch(cmds...)
	}
	if f := m.imgs.flush(); f != nil {
		// A transmissão vai antes do quadro que usa os placeholders.
		return m, tea.Sequence(f, cmd)
	}
	return m, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.onAudioMsg(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		resized := m.w != 0 && (m.w != msg.Width || m.h != msg.Height)
		m.w, m.h = msg.Width, msg.Height
		if resized {
			m.imgs.reset()
		}
		m.layout()
		m.refreshMessages(true)
		return m, nil

	case tea.KeyPressMsg:
		prev := m.focus
		cmd := m.onKey(msg)
		m.afterNav(prev)
		return m, tea.Batch(cmd, m.syncViewing())

	case tea.MouseClickMsg:
		prev := m.focus
		cmd := m.onClick(msg)
		m.afterNav(prev)
		return m, tea.Batch(cmd, m.syncViewing())

	case tea.MouseWheelMsg:
		return m, m.onWheel(msg)

	case tea.PasteMsg:
		if m.searching {
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.applyFilter()
			return m, cmd
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.layout()
		return m, cmd

	case pasteTextMsg:
		m.input.InsertString(string(msg))
		m.layout()
		return m, nil

	case attachMsg:
		a := attachment(msg)
		m.attach = &a
		m.flash = ""
		m.layout()
		return m, nil

	case tea.FocusMsg:
		m.termFocused = true
		cmds := []tea.Cmd{m.syncViewing()}
		if m.activeChat.Unread > 0 {
			// Voltou para a janela com mensagem nova no chat aberto.
			cmds = append(cmds, m.markRead(m.active))
		}
		return m, tea.Batch(cmds...)

	case tea.BlurMsg:
		m.termFocused = false
		return m, m.syncViewing()

	case chatsMsg:
		m.chats = msg
		m.sortChats()
		m.applyFilter()
		if m.pendingOpen.JID != "" {
			return m, m.flushPendingOpen()
		}
		if m.autoOpen && m.active == "" && len(m.list) > 0 {
			m.autoOpen = false
			return m, m.open(m.list[0])
		}
		if c, ok := m.chatByJID(m.active); ok {
			m.activeChat = c
		}
		return m, nil

	case inviteMsg:
		m.onInvite(msg)
		return m, nil
	case msgInfoMsg:
		m.msgInfo = msg.info
		return m, nil
	case pinsMsg:
		m.onPins(msg)
		return m, nil
	case starredMsg:
		m.starred = msg.hits
		if m.tab == tabStarred {
			m.applyFilter()
		}
		return m, nil
	case deleteCommitMsg:
		return m, m.onDeleteCommit(msg)
	case findMsg:
		return m, m.onFind(msg)
	case galleryMsg:
		return m, m.onGallery(msg)
	case refetchDoneMsg:
		m.onRefetchDone(msg)
		return m, nil
	case profileMsg:
		if m.profileOpen && msg.p.JID == m.active {
			m.profile = msg.p
		}
		return m, nil

	case profileMediaMsg:
		m.onProfileMedia(msg)
		return m, nil

	case searchTickMsg:
		return m, m.runSearch(msg.query)

	case hitsMsg:
		if msg.query == m.searchQuery() {
			m.hits, m.hitsQuery = msg.hits, msg.query
			m.applyFilter()
		}
		return m, nil

	case aroundMsg:
		return m, m.onAround(msg)

	case newerMsg:
		return m, m.onNewer(msg)

	case checkedMsg:
		c := *msg.chat
		if c.Name == "" {
			c.Name = c.JID
		}
		if m.chatIndex(c.JID) < 0 {
			m.chats = append(m.chats, c)
		}
		m.flash = ""
		return m, m.open(c)

	case flashMsg:
		m.setFlash(string(msg))
		return m, nil

	case membersMsg:
		m.members[msg.chat] = msg.list
		return m, tea.Batch(m.updateMention(), m.updateEmoji())

	case presenceTickMsg:
		return m, nil // só redesenhar: o aviso de digitando venceu

	case statusMsg:
		m.status, m.me, m.myName = msg.State, msg.Me, msg.Name
		return m, nil

	case chosenMsg:
		a, err := fileAttachment(string(msg))
		if err != nil {
			m.setFlash(err.Error())
			return m, nil
		}
		m.attach, m.picking = &a, false
		m.layout()
		return m, nil

	case msgsMsg:
		return m, m.onMessages(msg)

	case evMsg:
		if msg.cli != m.cli {
			return m, nil
		}
		return m, tea.Batch(m.onEvent(msg.f), waitEvent(m.cli))

	case goneMsg:
		if msg.cli != m.cli {
			return m, nil
		}
		m.status = "daemon caiu, reconectando"
		return m, reconnect()

	case reconnectMsg:
		if msg.err != nil {
			m.setFlash(msg.err.Error())
			return m, reconnect()
		}
		m.cli.Close()
		m.cli = msg.cli
		m.sentViewing = ""
		cmds := []tea.Cmd{m.loadChats(), m.loadStatus(), waitEvent(m.cli), m.syncViewing()}
		if m.active != "" {
			cmds = append(cmds, m.loadMessages(m.active, 0))
		}
		return m, tea.Batch(cmds...)

	case mediaMsg:
		return m, m.onMedia(msg)

	case stickersMsg:
		if msg.tab != m.stickTab {
			return m, nil // resposta da outra aba, chegou tarde
		}
		m.stickLoad = false
		m.stickNext = msg.page.Next
		if msg.offset == 0 {
			m.stickers, m.stickSel, m.stickTop = msg.page.Stickers, 0, 0
		} else {
			m.stickers = append(m.stickers, msg.page.Stickers...)
		}
		m.layout()
		return m, nil

	case avatarMsg:
		m.avatars[msg.jid] = msg.media
		if msg.media != nil && m.activeChat.Kind == "group" {
			m.rerenderKeepingPlace()
		}
		return m, nil

	case errMsg:
		m.setFlash(msg.err.Error())
		return m, nil
	}
	return m, nil
}

// afterNav redesenha a conversa quando o foco de painel muda: o destaque da
// mensagem só existe com o painel da conversa em foco.
func (m *model) afterNav(prev pane) {
	if m.focus != prev {
		m.rerenderKeepingPlace()
	}
}

func (m *model) rerenderKeepingPlace() {
	off := m.vp.YOffset()
	atBottom := m.vp.AtBottom()
	m.refreshMessages(atBottom)
	if !atBottom {
		m.vp.SetYOffset(off)
	}
}

func (m *model) onEvent(f ipc.Frame) tea.Cmd {
	switch f.Event {
	case ipc.EvStatus:
		var st ipc.Status
		if json.Unmarshal(f.Data, &st) == nil {
			m.status = st.State
			if st.Me != "" {
				m.me, m.myName = st.Me, st.Name
			}
		}
	case ipc.EvReload:
		return tea.Batch(m.loadChats(), m.reloadGallery())
	case ipc.EvChat:
		var c ipc.Chat
		if json.Unmarshal(f.Data, &c) != nil {
			return nil
		}
		if i := m.chatIndex(c.JID); i >= 0 {
			m.chats[i] = c
		} else {
			m.chats = append(m.chats, c)
		}
		m.sortChats()
		m.applyFilter()
		if c.JID == m.active {
			m.activeChat = c
		}
	case ipc.EvPins:
		return m.onPinsEvent(f.Data)
	case ipc.EvOpen:
		var o ipc.Open
		if json.Unmarshal(f.Data, &o) == nil {
			m.openRequested(o.Chat)
			return m.flushPendingOpen()
		}
	case ipc.EvPresence:
		return m.onPresence(f)
	case ipc.EvUpdate:
		// Mensagem editada, apagada ou com reação nova: troca no lugar.
		var msg ipc.Message
		if json.Unmarshal(f.Data, &msg) != nil {
			return nil
		}
		var reload tea.Cmd
		if m.tab == tabStarred {
			reload = m.loadStarred() // a estrela pode ter mudado
		}
		if msg.Chat != m.active {
			return reload
		}
		for i := range m.msgs {
			if m.msgs[i].ID == msg.ID && msg.Kind == "deleted" {
				// Apagada para você (aqui ou em outro aparelho): some.
				m.msgs = append(m.msgs[:i], m.msgs[i+1:]...)
				m.msgSel = -1
				m.rerenderKeepingPlace()
				break
			}
			if m.msgs[i].ID == msg.ID {
				m.msgs[i] = msg
				m.rerenderKeepingPlace()
				break
			}
		}
		return reload
	case ipc.EvMessage:
		var msg ipc.Message
		if json.Unmarshal(f.Data, &msg) != nil || msg.Chat != m.active {
			return nil
		}
		if slices.ContainsFunc(m.msgs, func(x ipc.Message) bool { return x.ID == msg.ID }) {
			return nil
		}
		if m.gap {
			return nil // há um buraco antes dela; chega ao descer até o fim
		}
		following := m.msgSel < 0 || m.msgSel == len(m.msgs)-1
		m.msgs = append(m.msgs, msg)
		if following {
			m.msgSel = -1
		}
		m.refreshMessages(m.vp.AtBottom() || msg.FromMe)
		cmds := []tea.Cmd{m.autoStickers([]ipc.Message{msg})}
		if !msg.FromMe && m.termFocused {
			// Chat aberto na tela = lido, como no app.
			cmds = append(cmds, m.markRead(m.active))
		}
		return tea.Batch(cmds...)
	}
	return nil
}

func (m *model) onMessages(msg msgsMsg) tea.Cmd {
	if msg.chat != m.active {
		return nil
	}
	if !msg.older {
		m.msgs = msg.msgs
		m.noMoreOld = len(msg.msgs) < pageSize
		m.refreshMessages(true)
		m.markUnread()
		return m.autoStickers(msg.msgs)
	}
	m.loadingOld = false
	if len(msg.msgs) == 0 {
		m.noMoreOld = true
		return nil
	}
	// Mantém o mesmo trecho na tela depois de inserir as antigas em cima.
	before := m.vp.TotalLineCount()
	off := m.vp.YOffset()
	if m.msgSel >= 0 {
		m.msgSel += len(msg.msgs)
	}
	m.msgs = append(msg.msgs, m.msgs...)
	m.refreshMessages(false)
	m.vp.SetYOffset(off + m.vp.TotalLineCount() - before)
	return m.autoStickers(msg.msgs)
}

// ---------- ações ----------

// newChatMarker prefixa o JID da entrada "Nova conversa com…" da busca.
const newChatMarker = "novo:"

var phoneRe = regexp.MustCompile(`^\+?[\d\s().-]{10,}$`)

type checkedMsg struct{ chat *ipc.Chat }

func (m *model) open(c ipc.Chat) tea.Cmd {
	if strings.HasPrefix(c.JID, hitMarker) {
		return m.openHit(c)
	}
	if strings.HasPrefix(c.JID, newChatMarker) {
		// Confere o número antes; a resposta abre o chat (checkedMsg).
		var out ipc.Chat
		m.setFlash("conferindo o número…")
		return m.call(ipc.Request{Op: ipc.OpCheckNumber, Text: strings.TrimPrefix(c.JID, newChatMarker)}, &out,
			func() tea.Msg { return checkedMsg{&out} })
	}
	m.focus = paneChat
	stop := m.stopTyping()
	if m.searching {
		m.stopSearch(true)
	}
	if c.JID == m.active {
		if c.Unread > 0 {
			return m.markRead(c.JID)
		}
		return nil
	}
	m.profileOpen, m.profile, m.gallery = false, nil, nil
	m.saveDraft()
	m.active, m.activeChat = c.JID, c
	m.msgs = nil
	m.msgSel, m.selAnchor = -1, -1
	m.mention, m.voting, m.finding = mentionState{}, nil, nil
	m.unreadCount, m.unreadID = c.Unread, ""
	m.restoreDraft()
	m.loadingOld, m.noMoreOld, m.gap = false, false, false
	m.layout()
	m.refreshMessages(true)
	m.pins, m.pinIdx = nil, 0
	cmds := []tea.Cmd{m.loadMessages(c.JID, 0), stop, m.loadPins(c.JID)}
	if c.Unread > 0 {
		cmds = append(cmds, m.markRead(c.JID))
	}
	return tea.Batch(cmds...)
}

func (m *model) maybeLoadOlder() tea.Cmd {
	if !(m.vp.AtTop() || m.sel() == 0) || m.loadingOld || m.noMoreOld || len(m.msgs) == 0 {
		return nil
	}
	m.loadingOld = true
	return m.loadMessages(m.active, m.msgs[0].TS)
}

func (m *model) setFlash(s string) {
	m.flash, m.flashAt = s, time.Now()
}

// ---------- mídia ----------

// openMedia: mídia já baixada abre no programa externo (foto em tela
// cheia, vídeo, PDF); a não baixada é baixada e, se for imagem no kitty,
// aparece no balão.
func (m *model) openMedia(i int) tea.Cmd {
	if i < 0 || i >= len(m.msgs) {
		return nil
	}
	msg := m.msgs[i]
	if c := msg.Card; c != nil && c.Type == "poll" {
		return m.openPoll(msg)
	}
	if c := msg.Card; c != nil && c.Chat != "" {
		// Contato com WhatsApp: abre a conversa com ele.
		return m.open(m.chatFor(c.Chat, c.Title))
	}
	if msg.Kind == "text" || msg.Kind == "other" {
		// Mensagem com link: ctrl+o abre o primeiro no navegador.
		u := firstURL(msg.Text)
		if c := msg.Card; c != nil && c.URL != "" {
			u = c.URL
		}
		if u != "" {
			if err := openURL(u); err != nil {
				m.setFlash("xdg-open: " + err.Error())
			} else {
				m.setFlash("abrindo " + u)
			}
		}
		return nil
	}
	if msg.Kind == "audio" {
		return m.toggleAudio(i)
	}
	isPic := msg.Kind == "image" || msg.Kind == "sticker"
	if msg.Media != nil {
		// Foto no kitty abre em tela cheia dentro do zapterm; o resto (e
		// fora do kitty) vai para o programa padrão.
		if isPic && m.imgs.enabled && msg.Media.Preview != "" {
			m.viewing = &viewer{media: msg.Media, caption: stripTag(msg.Text)}
			return nil
		}
		if err := openExternal(msg.Media.Path); err != nil {
			m.setFlash("xdg-open: " + err.Error())
		}
		return nil
	}
	if !msg.HasRaw {
		m.setFlash("mídia de mensagem antiga (do pareamento) não pode ser baixada")
		return nil
	}
	m.setFlash("baixando…")
	chat, id := m.active, msg.ID
	open := !(isPic && m.imgs.enabled)
	var md ipc.Media
	return m.call(ipc.Request{Op: ipc.OpMedia, Chat: chat, Msg: id}, &md,
		func() tea.Msg { return mediaMsg{chat: chat, id: id, media: &md, open: open} })
}

// autoStickers baixa sozinho as figurinhas das mensagens (são pequenas),
// sem esperar o ctrl+o.
func (m *model) autoStickers(msgs []ipc.Message) tea.Cmd {
	var cmds []tea.Cmd
	for _, msg := range msgs {
		hasPic := msg.Media != nil && msg.Media.Preview != ""
		isGIF := msg.Kind == "video" && strings.HasPrefix(msg.Text, "[GIF]")
		if (msg.Kind != "sticker" && !isGIF) || !msg.HasRaw || hasPic || m.autoAsked[msg.ID] {
			continue
		}
		m.autoAsked[msg.ID] = true
		chat, id := msg.Chat, msg.ID
		cli := m.cli
		cmds = append(cmds, func() tea.Msg {
			var md ipc.Media
			if err := cli.Call(ipc.Request{Op: ipc.OpMedia, Chat: chat, Msg: id}, &md); err != nil {
				return nil // figurinha expirada: fica como texto, sem alarde
			}
			return mediaMsg{chat: chat, id: id, media: &md, auto: true}
		})
	}
	return tea.Batch(cmds...)
}

// ---------- figurinhas ----------

func (m *model) openStickers() tea.Cmd {
	if m.active == "" {
		m.setFlash("abra um chat antes de escolher uma figurinha")
		return nil
	}
	m.picking, m.attach = true, nil
	m.stickTab = stickFav
	return m.loadStickers(0)
}

// Abas do seletor: a biblioteca do celular e as que passaram nas conversas.
const (
	stickFav = iota
	stickRecent
)

// Favoritas vêm em páginas (são centenas e cada uma é baixada na hora).
const stickPage = 28

// loadStickers pede uma página da aba atual; offset 0 recomeça a lista.
func (m *model) loadStickers(offset int) tea.Cmd {
	m.stickLoad = true
	if offset == 0 {
		m.stickers = nil
	}
	m.layout()
	tab := m.stickTab
	req := ipc.Request{Op: ipc.OpStickers, Limit: stickPage, Before: int64(offset)}
	if tab == stickRecent {
		req.Text = "recent"
	}
	var out ipc.StickerPage
	return m.call(req, &out, func() tea.Msg { return stickersMsg{page: out, tab: tab, offset: offset} })
}

func (m *model) closeStickers() {
	m.picking = false
	m.layout()
}

func (m *model) sendSticker(i int) tea.Cmd {
	if i < 0 || i >= len(m.stickers) {
		return nil
	}
	st := m.stickers[i]
	req := ipc.Request{Op: ipc.OpSendSticker, Chat: m.active, SrcChat: st.Chat, Msg: st.ID}
	if m.replyTo != nil {
		req.ReplyTo = m.replyRef()
	}
	m.replyTo, m.msgSel = nil, -1
	m.closeStickers()
	return m.send(req)
}

func (m *model) onMedia(msg mediaMsg) tea.Cmd {
	if !msg.auto {
		m.flash = ""
	}
	if msg.open {
		if err := openExternal(msg.media.Path); err != nil {
			m.setFlash("xdg-open: " + err.Error())
		}
	}
	if msg.chat != m.active {
		return nil
	}
	var cmd tea.Cmd
	for i := range m.msgs {
		if m.msgs[i].ID == msg.id {
			m.msgs[i].Media = msg.media
			if msg.play {
				cmd = m.startPlay(msg.media.Path, msg.id, audioSeconds(m.msgs[i].Text))
			}
			m.rerenderKeepingPlace()
			m.ensureSelVisible()
		}
	}
	return cmd
}

// ---------- fotos de perfil ----------

// avatar devolve a foto de perfil como bloco cols×rows: a foto redonda, o
// círculo com iniciais (kitty) ou um bloco de texto. Na primeira vez que vê
// um JID, enfileira a busca no daemon.
func (m *model) avatar(jid, name string, cols, rows int) string {
	if !m.imgs.enabled {
		return textAvatar(jid, name, cols, rows)
	}
	if !m.avAsked[jid] {
		m.avAsked[jid] = true
		m.avQueue = append(m.avQueue, jid)
	}
	if md := m.avatars[jid]; md != nil {
		if b := m.imgs.block(md.Preview, md.Width, md.Height, cols, rows); b != "" {
			return b
		}
	}
	f, ok := m.fallbacks[jid]
	if !ok {
		f = fallbackAvatarFile(jid, name)
		m.fallbacks[jid] = f
	}
	if b := m.imgs.block(f, fallbackPx, fallbackPx, cols, rows); b != "" {
		return b
	}
	return textAvatar(jid, name, cols, rows)
}

func (m *model) bigAvatar(jid, name string) string {
	return m.avatar(jid, name, avatarCols, avatarRows)
}

func (m *model) smallAvatar(jid, name string) string {
	return m.avatar(jid, name, avatarSmallCols, avatarSmallRows)
}

// prepareAvatars toca os avatares que o próximo quadro vai desenhar (lista
// visível e cabeçalho), para que buscas e transmissões ao kitty saiam neste
// Update e não fiquem esperando a próxima tecla.
func (m *model) prepareAvatars() {
	if m.w == 0 {
		return
	}
	for i := m.offset; i < min(len(m.list), m.offset+m.listRows()); i++ {
		m.bigAvatar(m.list[i].JID, m.list[i].Name)
	}
	if m.active != "" {
		m.bigAvatar(m.active, m.activeChat.Name)
	}
	if m.me != "" {
		m.smallAvatar(m.me, m.myName)
	}
}

func (m *model) loadAvatar(jid string) tea.Cmd {
	cli := m.cli
	return func() tea.Msg {
		var md *ipc.Media
		if err := cli.Call(ipc.Request{Op: ipc.OpAvatar, Chat: jid}, &md); err != nil {
			return nil // sem foto não é erro que valha incomodar
		}
		return avatarMsg{jid: jid, media: md}
	}
}

// ---------- lista, filtros e busca ----------

// sortChats: fixados primeiro, depois o mais recente, como no app.
func (m *model) sortChats() {
	slices.SortStableFunc(m.chats, func(a, b ipc.Chat) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		switch {
		case a.LastTS > b.LastTS:
			return -1
		case a.LastTS < b.LastTS:
			return 1
		}
		return 0
	})
}

func inTab(c ipc.Chat, tab int) bool {
	if c.Archived {
		return false
	}
	switch tab {
	case tabStarred:
		return false
	case tabUnread:
		return c.Unread > 0 && c.Kind != "channel"
	case tabGroups:
		return c.Kind == "group"
	case tabChannels:
		return c.Kind == "channel"
	}
	return c.Kind != "channel"
}

// tabCount conta os chats não lidos de um filtro.
func (m *model) tabCount(tab int) int {
	n := 0
	for _, c := range m.chats {
		if inTab(c, tab) && c.Unread > 0 {
			n++
		}
	}
	return n
}

func (m *model) archivedUnread() (total, unread int) {
	for _, c := range m.chats {
		if c.Archived {
			total++
			if c.Unread > 0 {
				unread++
			}
		}
	}
	return
}

// showArchivedRow: a linha "Arquivadas" aparece no topo de "Tudo".
func (m *model) showArchivedRow() bool {
	if m.archived || m.tab != tabAll || m.searchQuery() != "" {
		return false
	}
	n, _ := m.archivedUnread()
	return n > 0
}

func (m *model) searchQuery() string { return strings.TrimSpace(m.search.Value()) }

// applyFilter refaz a lista visível mantendo o cursor no mesmo chat. Com
// texto na busca, procura em todos os chats (inclusive arquivados e canais).
func (m *model) applyFilter() {
	sel := m.selectedJID()
	var list []ipc.Chat
	if q := m.searchQuery(); q != "" {
		// Telefone com DDI: primeira opção é abrir conversa nova com ele.
		if phoneRe.MatchString(q) {
			list = append(list, ipc.Chat{JID: newChatMarker + q, Name: "Nova conversa com " + q, Kind: "dm",
				LastText: "enter confere se o número tem WhatsApp e abre", LastStatus: -1})
		}
		for _, i := range fuzzyChats(q, m.chats) {
			list = append(list, m.chats[i])
		}
		if q == m.hitsQuery {
			list = append(list, m.hitEntries()...)
		}
	} else if m.tab == tabStarred && !m.archived {
		// Favoritas: as entradas são mensagens; enter pula até elas.
		m.hits, m.hitsQuery = m.starred, ""
		list = m.hitEntries()
	} else {
		for _, c := range m.chats {
			if (m.archived && c.Archived) || (!m.archived && inTab(c, m.tab)) {
				list = append(list, c)
			}
		}
	}
	m.list = list
	if i := slices.IndexFunc(m.list, func(c ipc.Chat) bool { return c.JID == sel }); i >= 0 {
		m.cursor = i
	}
	m.cursor = max(0, min(m.cursor, len(m.list)-1))
	m.scrollList()
}

func (m *model) setTab(t int) tea.Cmd {
	m.focus = paneList
	m.archived = false
	if t == m.tab {
		m.applyFilter()
		if t == tabStarred {
			return m.loadStarred()
		}
		return nil
	}
	m.tab = t
	m.cursor, m.offset = 0, 0
	m.applyFilter()
	if t == tabStarred {
		return m.loadStarred()
	}
	return nil
}

func (m *model) setArchived(on bool) {
	m.archived = on
	m.focus = paneList
	m.cursor, m.offset = 0, 0
	m.applyFilter()
}

func (m *model) startSearch() {
	m.searching = true
	m.focus = paneList
	m.input.Blur()
	m.search.Focus()
}

// stopSearch sai da busca; clear também apaga o texto e volta à lista.
func (m *model) stopSearch(clear bool) {
	m.searching = false
	m.search.Blur()
	m.input.Focus()
	if clear {
		m.hits, m.hitsQuery = nil, ""
		m.search.Reset()
		m.cursor, m.offset = 0, 0
		m.applyFilter()
	}
}

func (m *model) selected() (ipc.Chat, bool) {
	if m.cursor < 0 || m.cursor >= len(m.list) {
		return ipc.Chat{}, false
	}
	return m.list[m.cursor], true
}

func (m *model) selectedJID() string {
	c, _ := m.selected()
	return c.JID
}

func (m *model) chatIndex(jid string) int {
	return slices.IndexFunc(m.chats, func(c ipc.Chat) bool { return c.JID == jid })
}

func (m *model) chatByJID(jid string) (ipc.Chat, bool) {
	if i := m.chatIndex(jid); i >= 0 {
		return m.chats[i], true
	}
	return ipc.Chat{}, false
}

func (m *model) moveCursor(d int) {
	m.cursor = max(0, min(m.cursor+d, len(m.list)-1))
	m.scrollList()
}

func (m *model) scrollList() {
	rows := m.listRows()
	if rows <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, min(m.offset, len(m.list)-rows))
}

// ---------- mensagens: cursor ----------

// sel devolve o índice real da mensagem sob o cursor.
func (m *model) sel() int {
	if len(m.msgs) == 0 {
		return -1
	}
	if m.msgSel < 0 || m.msgSel >= len(m.msgs) {
		return len(m.msgs) - 1
	}
	return m.msgSel
}

func (m *model) setSel(i int) {
	if len(m.msgs) == 0 {
		return
	}
	i = max(0, min(i, len(m.msgs)-1))
	m.msgSel = i
	if i == len(m.msgs)-1 {
		m.msgSel = -1
	}
	off := m.vp.YOffset()
	m.refreshMessages(false)
	m.vp.SetYOffset(off)
	m.ensureSelVisible()
}

// selFromViewport põe o cursor na primeira (ou última) mensagem visível
// depois de rolar por página.
func (m *model) selFromViewport(top bool) {
	y0, y1 := m.vp.YOffset(), m.vp.YOffset()+m.vp.Height()-1
	pick := -1
	for i, sp := range m.spans {
		if sp[1] >= y0 && sp[0] <= y1 {
			pick = i
			if top {
				break
			}
		}
	}
	if pick >= 0 {
		m.msgSel = pick
		off := m.vp.YOffset()
		m.refreshMessages(false)
		m.vp.SetYOffset(off)
	}
}

func (m *model) ensureSelVisible() {
	i := m.sel()
	if i < 0 || i >= len(m.spans) {
		return
	}
	sp := m.spans[i]
	h := m.vp.Height()
	switch {
	case sp[1]-sp[0]+1 > h, sp[0] < m.vp.YOffset():
		// balão acima da tela, ou maior que ela: mostra o começo dele
		m.vp.SetYOffset(sp[0])
	case sp[1] > m.vp.YOffset()+h-1:
		m.vp.SetYOffset(sp[1] - h + 1)
	}
}

func (m *model) msgAtLine(line int) int {
	for i, sp := range m.spans {
		if line >= sp[0] && line <= sp[1] {
			return i
		}
	}
	return -1
}

func (m *model) refreshMessages(toBottom bool) {
	w := m.vp.Width()
	if w <= 0 {
		return
	}
	var content string
	switch {
	case m.active == "":
		content, m.spans = "", nil
	case len(m.msgs) == 0:
		content = paint(sText2.Render("\n  Carregando…"), cBgChat, w)
		m.spans = nil
	default:
		sel := -2 // sem destaque fora do painel da conversa
		if m.focus == paneChat {
			sel = m.sel()
		}
		playID, elapsed := m.playState()
		r := msgRenderer{w: w, isGroup: m.activeChat.Kind == "group", sel: sel, imgs: m.imgs, avatar: m.smallAvatar,
			playID: playID, elapsed: elapsed, selFrom: -1, selTo: -1, paused: m.play != nil && m.play.paused, speed: m.audioSpeed,
			unreadID: m.unreadID, unreadN: m.unreadCount}
		if a, b, ok := m.selRange(); ok {
			r.selFrom, r.selTo = a, b
		}
		content, m.spans = r.render(m.msgs)
	}
	m.vp.SetContent(content)
	if toBottom {
		m.vp.GotoBottom()
	}
}

// flushPendingOpen abre o chat pendente, com o nome da lista se ela já
// tiver chegado.
func (m *model) flushPendingOpen() tea.Cmd {
	c := m.pendingOpen
	if c.JID == "" {
		return nil
	}
	m.pendingOpen = ipc.Chat{}
	if cur, ok := m.chatByJID(c.JID); ok {
		c = cur
	}
	if m.searching {
		m.stopSearch(true)
	}
	m.active = "" // força recarregar mesmo se for o chat já aberto
	return m.open(c)
}

// draft é o que ficou na caixa de escrita de uma conversa ao trocar de
// chat: texto, resposta, anexo e as menções escolhidas.
type draft struct {
	text      string
	replyTo   *ipc.Message
	attach    *attachment
	mentioned map[string]string
}

// saveDraft guarda a caixa de escrita da conversa aberta. Uma edição em
// andamento não vira rascunho: é cancelada.
func (m *model) saveDraft() {
	if m.active == "" {
		return
	}
	if m.drafts == nil {
		m.drafts = map[string]draft{}
	}
	if m.editing != nil {
		m.editing = nil
		m.input.Reset()
	}
	d := draft{text: m.input.Value(), replyTo: m.replyTo, attach: m.attach, mentioned: m.mentioned}
	if strings.TrimSpace(d.text) == "" && d.replyTo == nil && d.attach == nil {
		delete(m.drafts, m.active)
		return
	}
	m.drafts[m.active] = d
}

// restoreDraft põe de volta o rascunho da conversa recém-aberta (ou limpa a
// caixa). Enquanto a conversa está aberta, o rascunho mora na própria caixa.
func (m *model) restoreDraft() {
	d, ok := m.drafts[m.active]
	delete(m.drafts, m.active)
	m.input.Reset()
	m.replyTo, m.attach, m.mentioned = nil, nil, map[string]string{}
	if !ok {
		return
	}
	m.input.SetValue(d.text)
	m.replyTo, m.attach = d.replyTo, d.attach
	if d.mentioned != nil {
		m.mentioned = d.mentioned
	}
}

// draftText: prévia do rascunho de uma conversa fechada ("" se não houver).
func (m *model) draftText(jid string) string {
	d, ok := m.drafts[jid]
	if !ok || jid == m.active {
		return ""
	}
	if t := strings.TrimSpace(d.text); t != "" {
		return t
	}
	if d.attach != nil {
		return "anexo"
	}
	return "resposta"
}

// chatFor acha a conversa na lista ou monta uma nova para abrir.
func (m *model) chatFor(jid, name string) ipc.Chat {
	for _, c := range m.chats {
		if c.JID == jid {
			return c
		}
	}
	return ipc.Chat{JID: jid, Name: name, Kind: "dm", LastStatus: -1}
}
