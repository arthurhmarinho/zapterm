// Package wa liga o whatsmeow ao banco do zapterm e ao socket da TUI.
package wa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/config"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
	"github.com/arthurhmarinho/zapterm/internal/store"
)

type Options struct {
	Log *slog.Logger
	// QROut recebe o QR code quando não há sessão. nil = sem sessão é erro
	// (o daemon em segundo plano não tem onde mostrar o QR).
	QROut io.Writer
}

type Daemon struct {
	log *slog.Logger
	cli *whatsmeow.Client
	st  *store.Store
	srv *ipc.Server

	mu     sync.Mutex
	status ipc.Status

	groupNameFetch sync.Map // jid -> struct{}: evita buscar o mesmo grupo duas vezes
	notif          *notifier
	avatars        *avatars
	desk           *desktop // servidor de notificações (D-Bus); nil sem sessão gráfica
	cfg            config.Config
}

// Run sobe o daemon e só volta quando ctx for cancelado ou algo fatal ocorrer.
func Run(ctx context.Context, opt Options) error {
	log := opt.Log

	sessPath, err := paths.SessionDB()
	if err != nil {
		return err
	}
	container, err := sqlstore.New(ctx, "sqlite3",
		"file:"+sessPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", waLogger{log.With("mod", "whatsmeow")})
	if err != nil {
		return fmt.Errorf("abrindo sessão: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return err
	}
	if device.ID == nil && opt.QROut == nil {
		return errors.New("sem sessão do WhatsApp: rode `zapterm login` num terminal primeiro")
	}

	msgPath, err := paths.MessagesDB()
	if err != nil {
		return err
	}
	st, err := store.Open(msgPath)
	if err != nil {
		return err
	}
	defer st.Close()

	d := &Daemon{log: log, st: st, status: ipc.Status{State: "connecting"}, notif: newNotifier(), avatars: newAvatars()}
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		log.Warn("config", "err", cfgErr) // vale o que deu para ler; o resto, padrão
	}
	d.cfg = cfg
	d.cli = whatsmeow.NewClient(device, waLogger{log.With("mod", "whatsmeow")})
	d.cli.AddEventHandler(d.onEvent)

	// O socket sobe antes de conectar: assim a TUI já lista o que está no
	// banco enquanto o WhatsApp ainda negocia.
	d.srv, err = ipc.Listen(paths.Socket(), d.handle, log)
	if err != nil {
		return err
	}
	defer d.srv.Close()
	if desk, err := newDesktop(d.openFromNotification, d.replyFromNotification); err != nil {
		log.Warn("notificações desligadas: sem D-Bus da sessão", "err", err)
	} else {
		d.desk = desk
	}
	d.srv.OnClose = func(conn uint64) {
		d.notif.setViewing(conn, "")
		d.syncAvailability()
	}
	go func() {
		if err := d.srv.Serve(); err != nil {
			log.Error("socket", "err", err)
		}
	}()

	if n, err := pruneMediaCache(); err != nil {
		log.Warn("limpando cache de mídia", "err", err)
	} else if n > 0 {
		log.Info("cache de mídia limpo", "arquivos", n)
	}

	if device.ID == nil {
		if err := d.pair(ctx, opt.QROut); err != nil {
			return err
		}
	} else if err := d.cli.Connect(); err != nil {
		return fmt.Errorf("conectando: %w", err)
	}
	defer d.cli.Disconnect()
	go d.purgeLoop(ctx)

	<-ctx.Done()
	return nil
}

func (d *Daemon) pair(ctx context.Context, out io.Writer) error {
	qrs, err := d.cli.GetQRChannel(ctx)
	if err != nil {
		return err
	}
	if err := d.cli.Connect(); err != nil {
		return fmt.Errorf("conectando: %w", err)
	}
	for item := range qrs {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			fmt.Fprintln(out, "\nNo celular: WhatsApp → Aparelhos conectados → Conectar um aparelho")
			qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, out)
		case whatsmeow.QRChannelSuccess.Event:
			fmt.Fprintln(out, "\nPareado. O histórico vai chegar nos próximos minutos;")
			fmt.Fprintln(out, "deixe este processo rodando até o log dizer que a sincronização terminou.")
			return nil
		default:
			return fmt.Errorf("pareamento falhou: %s", item.Event)
		}
	}
	return errors.New("pareamento cancelado")
}

func (d *Daemon) setStatus(state string) {
	d.mu.Lock()
	d.status.State = state
	if id := d.cli.Store.ID; id != nil {
		d.status.Me = id.ToNonAD().String()
		d.status.Name = d.cli.Store.PushName
	}
	s := d.status
	d.mu.Unlock()
	d.log.Info("status", "state", state)
	d.srv.Broadcast(ipc.EvStatus, s)
}

// ---------- eventos do WhatsApp ----------

func (d *Daemon) onEvent(evt any) {
	ctx := context.Background()
	switch e := evt.(type) {
	case *events.Connected:
		d.setStatus("online")
		go d.pushUnread(true) // o widget pode ter subido antes do daemon
		d.refreshAppState()
		// Reconectou: o servidor esqueceu a presença; manda de novo.
		availMu.Lock()
		availSent = nil
		availMu.Unlock()
		go d.syncAvailability()
	case *events.Disconnected:
		d.setStatus("offline")
	case *events.StreamReplaced:
		d.setStatus("offline")
		d.log.Warn("outra instância conectou com a mesma sessão")
	case *events.LoggedOut:
		d.setStatus("logged_out")
		d.log.Error("sessão encerrada pelo celular; rode `zapterm login` de novo", "reason", e.Reason)
	case *events.Message:
		d.onMessage(ctx, e, true)
	case *events.HistorySync:
		d.onHistory(ctx, e)
	case *events.GroupInfo:
		if e.Ephemeral != nil {
			secs := 0
			if e.Ephemeral.IsEphemeral {
				secs = int(e.Ephemeral.DisappearingTimer)
			}
			d.setChatEphemeral(e.JID.String(), secs)
		}
		if e.Name != nil {
			jid := e.JID.String()
			if err := d.st.SetChatName(jid, e.Name.Name); err == nil {
				d.pushChat(jid)
			}
		}
	case *events.Archive:
		// O whatsmeow já gravou no ChatSettings; só avisa as TUIs.
		d.pushChat(d.normalize(ctx, e.JID).String())
	case *events.Mute:
		d.pushChat(d.normalize(ctx, e.JID).String())
	case *events.Pin:
		d.pushChat(d.normalize(ctx, e.JID).String())
	case *events.AppState:
		if !d.onLockChat(e) {
			d.onAppState(e)
		}
	case *events.CallOffer:
		d.onCallOffer(e)
	case *events.Star:
		d.onStar(ctx, e)
	case *events.DeleteForMe:
		d.onDeleteForMe(ctx, e)
	case *events.Receipt:
		d.onReceipt(ctx, e)
	case *events.MarkChatAsRead:
		d.onMarkChatAsRead(ctx, e)
	case *events.ChatPresence:
		d.onChatPresence(ctx, e)
	case *events.Presence:
		d.onPresence(ctx, e)
	case *events.OfflineSyncCompleted:
		d.log.Info("mensagens offline recebidas", "count", e.Count)
	}
}

func (d *Daemon) onMessage(ctx context.Context, e *events.Message, live bool) {
	if e.Info.Chat == types.StatusBroadcastJID {
		return // status (stories) fica fora do zapterm
	}
	if d.onEphemeralSetting(ctx, e) || d.onPollVote(ctx, e) {
		return
	}
	if d.onChange(ctx, e, live) {
		return // edição, remoção ou reação: muda outra mensagem
	}
	kind, text := describe(e.Message)
	if kind == "" {
		return
	}
	chat := d.normalize(ctx, e.Info.Chat)
	sender := d.normalize(ctx, e.Info.Sender)
	m := ipc.Message{
		ID:         e.Info.ID,
		Chat:       chat.String(),
		Sender:     sender.String(),
		SenderName: d.contactName(ctx, sender, e.Info.PushName),
		FromMe:     e.Info.IsFromMe,
		TS:         e.Info.Timestamp.UnixMilli(),
		Kind:       kind,
		Text:       text,
		Quote:      quoteOf(e.Message),
		Expires:    expiresAt(e.Message, e.Info.Timestamp),
	}
	if m.Expires > 0 && m.Expires <= time.Now().UnixMilli() {
		return // temporária que já venceu (histórico antigo)
	}
	if m.Quote != nil && m.Quote.Sender != "" {
		if q, err := types.ParseJID(m.Quote.Sender); err == nil {
			m.Quote.Sender = d.normalize(ctx, q).String()
		}
	}
	raw, err := proto.Marshal(e.Message)
	if err != nil {
		raw = nil
	}
	if m.FromMe {
		// Enviada por você em outro aparelho: o contato "você" na agenda tem
		// o próprio número como nome.
		m.SenderName = "você"
	}
	isGroup := chat.Server == types.GroupServer
	isNew, err := d.st.SaveMessage(m, raw, isGroup, live && !m.FromMe)
	if err != nil {
		d.log.Error("gravando mensagem", "err", err)
		return
	}
	if !live || !isNew {
		return
	}
	// Quem manda numa conversa temporária manda com o prazo dela: serve para
	// aprender o prazo se o aviso de mudança se perdeu.
	if secs := int(contextInfo(e.Message).GetExpiration()); secs > 0 && !m.FromMe {
		d.setChatEphemeral(m.Chat, secs)
	}
	m.HasRaw = len(raw) > 0
	m.MentionsMe = d.mentionsMe(e.Message)
	one := []ipc.Message{m}
	d.fillSenders(ctx, one)
	d.fillMentions(ctx, one)
	d.fillThumbs(one)
	d.fillCards(one)
	d.fillReactions(one)
	m = one[0]
	d.srv.Broadcast(ipc.EvMessage, m)
	d.ensureName(chat)
	c := d.pushChat(m.Chat)
	if !m.FromMe {
		go d.notify(m, chat, c.Name)
	}
}

func (d *Daemon) onHistory(ctx context.Context, e *events.HistorySync) {
	convs := e.Data.GetConversations()
	n := 0
	onDemand := e.Data.GetSyncType() == waHistorySync.HistorySync_ON_DEMAND
	for _, c := range convs {
		jid, err := types.ParseJID(c.GetID())
		if err != nil {
			continue
		}
		jid = d.normalize(ctx, jid)
		isGroup := jid.Server == types.GroupServer
		ts := int64(c.GetConversationTimestamp()) * 1000
		if onDemand {
			// Resposta a um pedido nosso (refetch): só as mensagens
			// interessam; a conversa (não lidas, nome) fica como está.
			for _, hm := range c.GetMessages() {
				if evt, err := d.cli.ParseWebMessage(jid, hm.GetMessage()); err == nil {
					d.onMessage(ctx, evt, false)
					d.historyPolls(ctx, jid, hm.GetMessage())
					n++
				}
			}
			continue
		}
		if err := d.st.UpsertChat(jid.String(), c.GetName(), isGroup, ts, int(c.GetUnreadCount())); err != nil {
			d.log.Error("history: chat", "err", err)
			continue
		}
		// Só com o campo presente: pedaços parciais do histórico vêm sem ele
		// e não podem zerar um prazo conhecido.
		if c.GetEphemeralExpiration() > 0 || c.GetEphemeralSettingTimestamp() > 0 {
			if _, err := d.st.SetEphemeral(jid.String(), int(c.GetEphemeralExpiration())); err != nil {
				d.log.Warn("history: prazo de temporárias", "err", err)
			}
		}
		for _, hm := range c.GetMessages() {
			evt, err := d.cli.ParseWebMessage(jid, hm.GetMessage())
			if err != nil {
				continue
			}
			d.onMessage(ctx, evt, false)
			d.historyReactions(ctx, jid, hm.GetMessage())
			d.historyPolls(ctx, jid, hm.GetMessage())
			if hm.GetMessage().GetStarred() {
				_, _ = d.st.SetStarred(jid.String(), evt.Info.ID, true)
			}
			if evt.Info.IsFromMe {
				if st := historyStatus(hm.GetMessage().GetStatus()); st > 0 {
					_ = d.st.SetStatus(jid.String(), []string{evt.Info.ID}, st)
				}
			}
			n++
		}
	}
	d.log.Info("history sync", "tipo", e.Data.GetSyncType().String(),
		"progresso", e.Data.GetProgress(), "chats", len(convs), "mensagens", n)
	// A TUI recarrega a lista inteira em vez de receber milhares de eventos.
	d.srv.Broadcast(ipc.EvReload, nil)
	d.scheduleUnreadPush()
}

// pushChat avisa as TUIs de que um chat mudou.
func (d *Daemon) pushChat(jid string) ipc.Chat {
	c, err := d.st.Chat(jid)
	if err != nil {
		return ipc.Chat{JID: jid, Name: jid}
	}
	d.decorate(context.Background(), &c)
	d.srv.Broadcast(ipc.EvChat, c)
	d.scheduleUnreadPush()
	return c
}

// ---------- nomes e JIDs ----------

// normalize troca o JID de LID (identificador oculto que o WhatsApp vem
// adotando) pelo número de telefone quando o mapeamento é conhecido, para a
// mesma conversa não aparecer duas vezes na lista.
func (d *Daemon) normalize(ctx context.Context, jid types.JID) types.JID {
	jid = jid.ToNonAD()
	if jid.Server != types.HiddenUserServer {
		return jid
	}
	if pn, err := d.cli.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
		return pn.ToNonAD()
	}
	return jid
}

func (d *Daemon) contactName(ctx context.Context, jid types.JID, pushName string) string {
	if n := d.knownName(ctx, jid); n != "" {
		return n
	}
	if pushName != "" {
		return pushName
	}
	if jid.Server == types.DefaultUserServer {
		return "+" + jid.User
	}
	return jid.User
}

// knownName devolve o nome que o whatsmeow conhece (agenda, perfil
// comercial ou nome de perfil), ou "" se não houver nenhum.
func (d *Daemon) knownName(ctx context.Context, jid types.JID) string {
	c, err := d.cli.Store.Contacts.GetContact(ctx, jid)
	if err != nil || !c.Found {
		return ""
	}
	for _, n := range []string{c.FullName, c.FirstName, c.BusinessName} {
		if n != "" {
			return n
		}
	}
	if c.PushName != "" {
		return "~" + c.PushName // como o WhatsApp mostra quem não está na agenda
	}
	return ""
}

// fillSenders resolve o nome do remetente na hora da leitura. Gravar só o
// nome visto na chegada congelaria "+5584…": no history sync os nomes de
// perfil chegam depois das mensagens.
func (d *Daemon) fillSenders(ctx context.Context, msgs []ipc.Message) {
	cache := map[string]string{}
	for i := range msgs {
		m := &msgs[i]
		if m.FromMe {
			m.SenderName = "você"
			continue
		}
		name, ok := cache[m.Sender]
		if !ok {
			if jid, err := types.ParseJID(m.Sender); err == nil {
				name = d.knownName(ctx, jid)
			}
			cache[m.Sender] = name
		}
		if name != "" {
			m.SenderName = name
		}
		if q := m.Quote; q != nil {
			q.SenderName = d.senderLabel(ctx, q.Sender, cache)
		}
		if m.Kind != "text" && m.Kind != "other" {
			m.Media = cachedMedia(m.ID)
		}
	}
}

// senderLabel dá nome a um JID citado: "você", contato, ou o número.
func (d *Daemon) senderLabel(ctx context.Context, sender string, cache map[string]string) string {
	jid, err := types.ParseJID(sender)
	if err != nil || sender == "" {
		return ""
	}
	if id := d.cli.Store.ID; id != nil && jid.User == id.User {
		return "você"
	}
	if n, ok := cache[sender]; ok && n != "" {
		return n
	}
	return d.contactName(ctx, jid, "")
}

// decorate completa um chat do banco com o que vem do whatsmeow: nome,
// tipo e as configurações sincronizadas do celular (arquivado, mudo, fixado).
func (d *Daemon) decorate(ctx context.Context, c *ipc.Chat) {
	d.fillName(ctx, c)
	jid, err := types.ParseJID(c.JID)
	if err != nil {
		return
	}
	switch jid.Server {
	case types.GroupServer:
		c.Kind = "group"
	case types.NewsletterServer:
		c.Kind = "channel"
	default:
		c.Kind = "dm"
	}
	s := d.chatSettings(ctx, jid)
	c.Archived = s.Archived
	c.Pinned = s.Pinned
	c.Muted = s.MutedUntil.After(time.Now())
}

// chatSettings junta as configurações do celular gravadas pelo número e
// pelo LID: o WhatsApp grava a maioria pelo LID (314 de 435 na conta dele),
// e o zapterm guarda os chats pelo número. Procurar só pelo número perdia
// fixados, arquivados e silenciados.
func (d *Daemon) chatSettings(ctx context.Context, jid types.JID) types.LocalChatSettings {
	var out types.LocalChatSettings
	merge := func(j types.JID) {
		s, err := d.cli.Store.ChatSettings.GetChatSettings(ctx, j)
		if err != nil || !s.Found {
			return
		}
		out.Found = true
		out.Archived = out.Archived || s.Archived
		out.Pinned = out.Pinned || s.Pinned
		if s.MutedUntil.After(out.MutedUntil) {
			out.MutedUntil = s.MutedUntil
		}
	}
	merge(jid)
	switch jid.Server {
	case types.DefaultUserServer:
		if lid, err := d.cli.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
			merge(lid)
		}
	case types.HiddenUserServer:
		if pn, err := d.cli.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
			merge(pn)
		}
	}
	return out
}

func (d *Daemon) fillName(ctx context.Context, c *ipc.Chat) {
	if c.Name != "" {
		return
	}
	jid, err := types.ParseJID(c.JID)
	if err != nil {
		c.Name = c.JID
		return
	}
	switch jid.Server {
	case types.GroupServer:
		c.Name = "(grupo)"
		d.ensureName(jid)
		return
	case types.NewsletterServer:
		c.Name = "(canal)"
		d.ensureName(jid)
		return
	}
	c.Name = d.contactName(ctx, jid, "")
}

// ensureName busca no servidor o nome de um grupo ou canal que chegou sem
// nome (mensagem ao vivo, ou history sync incompleto), uma vez por execução.
func (d *Daemon) ensureName(jid types.JID) {
	if jid.Server != types.GroupServer && jid.Server != types.NewsletterServer {
		return
	}
	if _, loaded := d.groupNameFetch.LoadOrStore(jid.String(), struct{}{}); loaded {
		return
	}
	go func() {
		if c, err := d.st.Chat(jid.String()); err == nil && c.Name != "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var name string
		if jid.Server == types.GroupServer {
			info, err := d.cli.GetGroupInfo(ctx, jid)
			if err != nil {
				d.log.Warn("nome do grupo", "jid", jid, "err", err)
				return
			}
			name = info.Name
		} else {
			info, err := d.cli.GetNewsletterInfo(ctx, jid)
			if err != nil {
				d.log.Warn("nome do canal", "jid", jid, "err", err)
				return
			}
			name = info.ThreadMeta.Name.Text
		}
		if name == "" {
			return
		}
		if err := d.st.SetChatName(jid.String(), name); err == nil {
			d.pushChat(jid.String())
		}
	}()
}

// ---------- pedidos da TUI ----------

func (d *Daemon) handle(conn uint64, req ipc.Request) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	switch req.Op {
	case ipc.OpStatus:
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.status, nil
	case ipc.OpChats:
		chats, err := d.st.Chats()
		if err != nil {
			return nil, err
		}
		for i := range chats {
			d.decorate(ctx, &chats[i])
		}
		return chats, nil
	case ipc.OpUnread:
		return d.unreadSummary(ctx), nil
	case ipc.OpShowUI:
		return nil, d.showUI()
	case ipc.OpProfile:
		pctx, pcancel := context.WithTimeout(context.Background(), time.Minute)
		defer pcancel()
		return d.profile(pctx, req.Chat)
	case ipc.OpPollVote:
		return nil, d.pollVote(ctx, req.Chat, req.Msg, req.Options)
	case ipc.OpStar:
		return nil, d.star(ctx, req.Chat, req.Msg, req.Text == "1")
	case ipc.OpStarred:
		return d.starred(ctx)
	case ipc.OpDeleteMe:
		return nil, d.deleteForMe(ctx, req.Chat, req.Msg)
	case ipc.OpRefetch:
		return nil, d.refetch(ctx, req.Chat, req.Before)
	case ipc.OpGallery:
		return d.gallery(context.Background(), req)
	case ipc.OpSearch:
		return d.search(ctx, req.Chat, req.Text, req.Limit)
	case ipc.OpMessages:
		var msgs []ipc.Message
		var err error
		if req.After > 0 {
			msgs, err = d.st.MessagesAfter(req.Chat, req.Limit, req.After)
		} else {
			msgs, err = d.st.Messages(req.Chat, req.Limit, req.Before)
		}
		if err != nil {
			return nil, err
		}
		d.fillSenders(ctx, msgs)
		d.fillMentions(ctx, msgs)
		d.fillThumbs(msgs)
		d.fillCards(msgs)
		d.fillReactions(msgs)
		return msgs, nil
	case ipc.OpSend:
		return d.send(ctx, req.Chat, req.Text, req.ReplyTo, req.Mentions)
	case ipc.OpCheckNumber:
		return d.checkNumber(ctx, req.Text)
	case ipc.OpChatAction:
		return nil, d.chatAction(ctx, req.Chat, req.Text)
	case ipc.OpMembers:
		return d.groupMembers(ctx, req.Chat)
	case ipc.OpRead:
		return nil, d.markRead(ctx, req.Chat)
	case ipc.OpViewing:
		d.notif.setViewing(conn, req.Chat)
		d.syncAvailability()
		d.subscribePresence(req.Chat)
		return nil, nil
	case ipc.OpTyping:
		return nil, d.sendTyping(ctx, req.Chat, req.Text)
	case ipc.OpAvatar:
		// Prazo próprio: a fila espaça os pedidos ao servidor.
		actx, acancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer acancel()
		return d.avatar(actx, req.Chat)
	case ipc.OpStickers:
		sctx, scancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer scancel()
		if req.Text == "recent" {
			list, err := d.recentStickers(sctx)
			return ipc.StickerPage{Stickers: list, Next: -1}, err
		}
		return d.favoriteStickers(sctx, int(req.Before), req.Limit)
	case ipc.OpSendSticker:
		sctx, scancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer scancel()
		return d.sendSticker(sctx, req.Chat, req.SrcChat, req.Msg, req.ReplyTo)
	case ipc.OpRevoke:
		return nil, d.revoke(ctx, req.Chat, req.Msg)
	case ipc.OpEdit:
		return nil, d.edit(ctx, req.Chat, req.Msg, req.Text)
	case ipc.OpForward:
		fctx, fcancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer fcancel()
		return d.forward(fctx, req.Chat, req.SrcChat, req.Msg)
	case ipc.OpReact:
		return nil, d.react(ctx, req.Chat, req.Msg, req.Text)
	case ipc.OpSendFile:
		fctx, fcancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer fcancel()
		return d.sendFile(fctx, req.Chat, req.Path, req.Text, req.ReplyTo)
	case ipc.OpSendAudio:
		actx, acancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer acancel()
		return d.sendAudio(actx, req.Chat, req.Path, req.Seconds, req.ReplyTo)
	case ipc.OpSendImage:
		ictx, icancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer icancel()
		return d.sendImage(ictx, req.Chat, req.Path, req.Text, req.ReplyTo)
	case ipc.OpMedia:
		// Vídeo grande demora; o prazo de 25s dos outros pedidos é curto.
		mctx, mcancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer mcancel()
		return d.downloadMedia(mctx, req.Chat, req.Msg)
	}
	return nil, fmt.Errorf("operação desconhecida: %q", req.Op)
}

func (d *Daemon) send(ctx context.Context, chat, text, replyTo string, mentions []string) (any, error) {
	text = strings.TrimRight(text, " \n")
	if text == "" {
		return nil, errors.New("mensagem vazia")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	ci, quote, err := d.replyContext(chat, replyTo)
	if err != nil {
		return nil, err
	}
	if len(mentions) > 0 {
		if ci == nil {
			ci = &waE2E.ContextInfo{}
		}
		ci.MentionedJID = mentions
	}
	msg := &waE2E.Message{Conversation: proto.String(text)}
	if ci != nil {
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text), ContextInfo: ci,
		}}
	}
	return d.sendAndStore(ctx, jid, msg, ipc.Message{Kind: "text", Text: text, Quote: quote}, nil)
}

// replyContext monta a citação de uma resposta (nil, nil se replyTo for "").
func (d *Daemon) replyContext(chat, replyTo string) (*waE2E.ContextInfo, *ipc.Quote, error) {
	if replyTo == "" {
		return nil, nil, nil
	}
	// "grupo|id": responder em privado cita a mensagem do grupo.
	src := chat
	if c, id, ok := strings.Cut(replyTo, "|"); ok {
		src, replyTo = c, id
	}
	orig, origRaw, err := d.st.Message(src, replyTo)
	if err != nil {
		return nil, nil, fmt.Errorf("mensagem respondida não encontrada: %w", err)
	}
	// O celular mostra a citação a partir de QuotedMessage. Com o
	// protobuf original vai a mensagem exata (com miniatura, se for foto);
	// sem ele (history sync antigo), vai só o texto.
	quoted := &waE2E.Message{Conversation: proto.String(orig.Text)}
	if len(origRaw) > 0 {
		var q waE2E.Message
		if proto.Unmarshal(origRaw, &q) == nil {
			quoted = &q
		}
	}
	participant := orig.Sender
	if orig.FromMe {
		participant = d.cli.Store.ID.ToNonAD().String()
	}
	ci := &waE2E.ContextInfo{
		StanzaID:      proto.String(orig.ID),
		Participant:   proto.String(participant),
		QuotedMessage: quoted,
	}
	if src != chat {
		ci.RemoteJID = proto.String(src) // a citação vem de outra conversa
	}
	return ci, &ipc.Quote{ID: orig.ID, Sender: participant, Text: orig.Text}, nil
}

// sendAndStore envia, grava no banco e avisa as TUIs. m traz só Kind, Text,
// Quote e Media; o resto vem da resposta do servidor. afterSend roda com o ID
// definitivo antes do broadcast (a imagem usa para entrar no cache).
func (d *Daemon) sendAndStore(ctx context.Context, jid types.JID, msg *waE2E.Message, m ipc.Message, afterSend func(id string) *ipc.Media) (any, error) {
	// Conversa temporária: a mensagem leva o prazo, senão não some.
	secs := d.st.Ephemeral(jid.String())
	withExpiration(msg, uint32(secs))
	resp, err := d.cli.SendMessage(ctx, jid, msg)
	if err != nil {
		return nil, err
	}
	me := d.cli.Store.ID.ToNonAD()
	m.ID, m.Chat, m.Sender, m.SenderName = resp.ID, jid.String(), me.String(), "você"
	m.FromMe, m.TS = true, resp.Timestamp.UnixMilli()
	if secs > 0 {
		m.Expires = resp.Timestamp.Add(time.Duration(secs) * time.Second).UnixMilli()
	}
	raw, _ := proto.Marshal(msg)
	m.Status = 1 // o servidor aceitou: um tracinho
	if _, err := d.st.SaveMessage(m, raw, jid.Server == types.GroupServer, false); err != nil {
		d.log.Error("gravando enviada", "err", err)
	}
	_ = d.st.SetStatus(m.Chat, []string{m.ID}, 1)
	one := []ipc.Message{m}
	d.fillSenders(ctx, one)
	d.fillMentions(ctx, one)
	d.fillThumbs(one)
	d.fillCards(one)
	d.fillReactions(one)
	m = one[0]
	if afterSend != nil {
		m.Media = afterSend(m.ID)
	}
	m.HasRaw = true
	d.srv.Broadcast(ipc.EvMessage, m)
	d.pushChat(m.Chat)
	return m, nil
}

// markRead manda o recibo de leitura (os tracinhos azuis) das mensagens não
// lidas e zera o contador.
func (d *Daemon) markRead(ctx context.Context, chat string) error {
	c, err := d.st.Chat(chat)
	if err != nil {
		return err
	}
	if c.Unread == 0 {
		return nil
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	msgs, err := d.st.Messages(chat, c.Unread+20, 0)
	if err != nil {
		return err
	}
	bySender := map[string][]types.MessageID{}
	for _, m := range msgs {
		if !m.FromMe {
			bySender[m.Sender] = append(bySender[m.Sender], m.ID)
		}
	}
	for s, ids := range bySender {
		sender, _ := types.ParseJID(s)
		if jid.Server != types.GroupServer {
			sender = types.EmptyJID
		}
		if !d.cfg.Privacy.ReadReceipts {
			continue // [privacy] read_receipts = false: lê sem avisar
		}
		if err := d.cli.MarkRead(ctx, ids, time.Now(), jid, sender); err != nil {
			d.log.Warn("recibo de leitura", "err", err)
		}
	}
	if err := d.st.MarkRead(chat); err != nil {
		return err
	}
	d.pushChat(chat)
	return nil
}
