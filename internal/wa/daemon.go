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
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

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

	d := &Daemon{log: log, st: st, status: ipc.Status{State: "connecting"}}
	d.cli = whatsmeow.NewClient(device, waLogger{log.With("mod", "whatsmeow")})
	d.cli.AddEventHandler(d.onEvent)

	// O socket sobe antes de conectar: assim a TUI já lista o que está no
	// banco enquanto o WhatsApp ainda negocia.
	d.srv, err = ipc.Listen(paths.Socket(), d.handle, log)
	if err != nil {
		return err
	}
	defer d.srv.Close()
	go func() {
		if err := d.srv.Serve(); err != nil {
			log.Error("socket", "err", err)
		}
	}()

	if device.ID == nil {
		if err := d.pair(ctx, opt.QROut); err != nil {
			return err
		}
	} else if err := d.cli.Connect(); err != nil {
		return fmt.Errorf("conectando: %w", err)
	}
	defer d.cli.Disconnect()

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
		if e.Name != nil {
			jid := e.JID.String()
			if err := d.st.SetChatName(jid, e.Name.Name); err == nil {
				d.pushChat(jid)
			}
		}
	case *events.OfflineSyncCompleted:
		d.log.Info("mensagens offline recebidas", "count", e.Count)
	}
}

func (d *Daemon) onMessage(ctx context.Context, e *events.Message, live bool) {
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
	}
	isGroup := chat.Server == types.GroupServer
	isNew, err := d.st.SaveMessage(m, isGroup, live && !m.FromMe)
	if err != nil {
		d.log.Error("gravando mensagem", "err", err)
		return
	}
	if !live || !isNew {
		return
	}
	d.srv.Broadcast(ipc.EvMessage, m)
	d.ensureGroupName(chat)
	d.pushChat(m.Chat)
}

func (d *Daemon) onHistory(ctx context.Context, e *events.HistorySync) {
	convs := e.Data.GetConversations()
	n := 0
	for _, c := range convs {
		jid, err := types.ParseJID(c.GetID())
		if err != nil {
			continue
		}
		jid = d.normalize(ctx, jid)
		isGroup := jid.Server == types.GroupServer
		ts := int64(c.GetConversationTimestamp()) * 1000
		if err := d.st.UpsertChat(jid.String(), c.GetName(), isGroup, ts, int(c.GetUnreadCount())); err != nil {
			d.log.Error("history: chat", "err", err)
			continue
		}
		for _, hm := range c.GetMessages() {
			evt, err := d.cli.ParseWebMessage(jid, hm.GetMessage())
			if err != nil {
				continue
			}
			d.onMessage(ctx, evt, false)
			n++
		}
	}
	d.log.Info("history sync", "tipo", e.Data.GetSyncType().String(),
		"progresso", e.Data.GetProgress(), "chats", len(convs), "mensagens", n)
	// A TUI recarrega a lista inteira em vez de receber milhares de eventos.
	d.srv.Broadcast(ipc.EvReload, nil)
}

// pushChat avisa as TUIs de que um chat mudou.
func (d *Daemon) pushChat(jid string) {
	c, err := d.st.Chat(jid)
	if err != nil {
		return
	}
	d.fillName(context.Background(), &c)
	d.srv.Broadcast(ipc.EvChat, c)
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
	if c, err := d.cli.Store.Contacts.GetContact(ctx, jid); err == nil && c.Found {
		for _, n := range []string{c.FullName, c.FirstName, c.BusinessName, c.PushName} {
			if n != "" {
				return n
			}
		}
	}
	if pushName != "" {
		return pushName
	}
	if jid.Server == types.DefaultUserServer {
		return "+" + jid.User
	}
	return jid.User
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
	if jid.Server == types.GroupServer {
		c.Name = "(grupo)"
		return
	}
	c.Name = d.contactName(ctx, jid, "")
}

// ensureGroupName busca o nome de um grupo que apareceu só por mensagem ao
// vivo (sem history sync), uma vez por execução.
func (d *Daemon) ensureGroupName(jid types.JID) {
	if jid.Server != types.GroupServer {
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
		info, err := d.cli.GetGroupInfo(ctx, jid)
		if err != nil {
			d.log.Warn("nome do grupo", "jid", jid, "err", err)
			return
		}
		if err := d.st.SetChatName(jid.String(), info.Name); err == nil {
			d.pushChat(jid.String())
		}
	}()
}

// ---------- pedidos da TUI ----------

func (d *Daemon) handle(req ipc.Request) (any, error) {
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
			d.fillName(ctx, &chats[i])
		}
		return chats, nil
	case ipc.OpMessages:
		return d.st.Messages(req.Chat, req.Limit, req.Before)
	case ipc.OpSend:
		return d.send(ctx, req.Chat, req.Text)
	case ipc.OpRead:
		return nil, d.markRead(ctx, req.Chat)
	}
	return nil, fmt.Errorf("operação desconhecida: %q", req.Op)
}

func (d *Daemon) send(ctx context.Context, chat, text string) (any, error) {
	text = strings.TrimRight(text, " \n")
	if text == "" {
		return nil, errors.New("mensagem vazia")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	resp, err := d.cli.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(text)})
	if err != nil {
		return nil, err
	}
	me := d.cli.Store.ID.ToNonAD()
	m := ipc.Message{
		ID: resp.ID, Chat: jid.String(), Sender: me.String(), SenderName: "você",
		FromMe: true, TS: resp.Timestamp.UnixMilli(), Kind: "text", Text: text,
	}
	if _, err := d.st.SaveMessage(m, jid.Server == types.GroupServer, false); err != nil {
		d.log.Error("gravando enviada", "err", err)
	}
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
