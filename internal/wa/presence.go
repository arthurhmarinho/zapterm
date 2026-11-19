package wa

import (
	"context"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// ---------- recibos (tracinhos) ----------

func (d *Daemon) onReceipt(ctx context.Context, e *events.Receipt) {
	chat := d.normalize(ctx, e.Chat).String()
	switch e.Type {
	case types.ReceiptTypeReadSelf:
		// Você leu este chat em outro aparelho (o celular): zera aqui também.
		if err := d.st.MarkRead(chat); err == nil {
			d.pushChat(chat)
		}
		return
	case types.ReceiptTypeDelivered:
		d.setMsgStatus(chat, e.MessageIDs, 2)
	case types.ReceiptTypeRead:
		d.setMsgStatus(chat, e.MessageIDs, 3)
	case types.ReceiptTypePlayed:
		d.setMsgStatus(chat, e.MessageIDs, 4)
	}
}

// setMsgStatus grava e avisa as TUIs. Em grupo basta um participante para
// subir (o app espera todos; contar participantes fica para depois).
func (d *Daemon) setMsgStatus(chat string, ids []string, status int) {
	if err := d.st.SetStatus(chat, ids, status); err != nil {
		d.log.Warn("recibo", "err", err)
		return
	}
	for _, id := range ids {
		d.pushUpdate(chat, id)
	}
	d.pushChat(chat)
}

// historyStatus traduz a situação que vem no history sync.
func historyStatus(s waWeb.WebMessageInfo_Status) int {
	switch s {
	case waWeb.WebMessageInfo_SERVER_ACK:
		return 1
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return 2
	case waWeb.WebMessageInfo_READ:
		return 3
	case waWeb.WebMessageInfo_PLAYED:
		return 4
	}
	return 0
}

// ---------- presença ----------

func (d *Daemon) onChatPresence(ctx context.Context, e *events.ChatPresence) {
	p := ipc.Presence{
		Chat:      d.normalize(ctx, e.Chat).String(),
		Typing:    e.State == types.ChatPresenceComposing && e.Media != types.ChatPresenceMediaAudio,
		Recording: e.State == types.ChatPresenceComposing && e.Media == types.ChatPresenceMediaAudio,
	}
	if e.IsGroup {
		p.Who = d.contactName(ctx, d.normalize(ctx, e.Sender), "")
	}
	d.srv.Broadcast(ipc.EvPresence, p)
}

func (d *Daemon) onPresence(ctx context.Context, e *events.Presence) {
	online := !e.Unavailable
	p := ipc.Presence{Chat: d.normalize(ctx, e.From).String(), Online: &online}
	if !e.LastSeen.IsZero() {
		p.LastSeen = e.LastSeen.UnixMilli()
	}
	d.srv.Broadcast(ipc.EvPresence, p)
}

var availMu sync.Mutex
var availSent *bool

// syncAvailability: você aparece online só enquanto alguma TUI está aberta
// e em foco num chat, como o WhatsApp Web. O WhatsApp só manda "digitando"
// e "online" dos outros a quem está online; ficar online sempre (o daemon
// vive conectado) mostraria você online 24 horas.
func (d *Daemon) syncAvailability() {
	want := d.notif.anyViewing()
	availMu.Lock()
	defer availMu.Unlock()
	if availSent != nil && *availSent == want {
		return
	}
	state := types.PresenceUnavailable
	if want {
		state = types.PresenceAvailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.cli.SendPresence(ctx, state); err != nil {
		d.log.Warn("presença", "err", err)
		return
	}
	availSent = &want
}

// subscribePresence pede "online"/"visto por último" de uma conversa
// individual quando ela é aberta.
func (d *Daemon) subscribePresence(chat string) {
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.DefaultUserServer {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.cli.SubscribePresence(ctx, jid); err != nil {
			d.log.Debug("assinando presença", "err", err)
		}
	}()
}

// sendTyping mostra aos outros que você está digitando ou gravando.
func (d *Daemon) sendTyping(ctx context.Context, chat, state string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	cp, media := types.ChatPresencePaused, types.ChatPresenceMediaText
	switch state {
	case "composing":
		cp = types.ChatPresenceComposing
	case "recording":
		cp, media = types.ChatPresenceComposing, types.ChatPresenceMediaAudio
	}
	return d.cli.SendChatPresence(ctx, jid, cp, media)
}
