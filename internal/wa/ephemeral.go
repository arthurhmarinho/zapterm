package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Mensagens temporárias. O prazo é da conversa: cada mensagem enviada
// precisa levar ContextInfo.Expiration, senão os outros aparelhos a guardam
// para sempre. O zapterm aprende o prazo pelo histórico
// (Conversation.EphemeralExpiration), pelos avisos de mudança
// (ProtocolMessage EPHEMERAL_SETTING), pelas informações do grupo e pelas
// próprias mensagens recebidas.

// onEphemeralSetting trata o aviso "Fulano ligou/desligou as mensagens
// temporárias". Devolve true se a mensagem era esse aviso.
func (d *Daemon) onEphemeralSetting(ctx context.Context, e *events.Message) bool {
	pm := e.Message.GetProtocolMessage()
	if pm == nil || pm.GetType() != waE2E.ProtocolMessage_EPHEMERAL_SETTING {
		return false
	}
	d.setChatEphemeral(d.normalize(ctx, e.Info.Chat).String(), int(pm.GetEphemeralExpiration()))
	return true
}

func (d *Daemon) setChatEphemeral(chat string, secs int) {
	changed, err := d.st.SetEphemeral(chat, secs)
	if err != nil {
		d.log.Warn("prazo de temporárias", "err", err)
		return
	}
	if changed {
		d.pushChat(chat)
	}
}

// expiresAt calcula quando uma mensagem recebida vence (unix ms; 0 = nunca).
func expiresAt(m *waE2E.Message, ts time.Time) int64 {
	secs := contextInfo(m).GetExpiration()
	if secs == 0 {
		return 0
	}
	return ts.Add(time.Duration(secs) * time.Second).UnixMilli()
}

// withExpiration põe o prazo da conversa na mensagem a enviar. Texto simples
// (Conversation) não tem ContextInfo; vira ExtendedTextMessage.
func withExpiration(msg *waE2E.Message, secs uint32) {
	if secs == 0 || msg == nil {
		return
	}
	if t := msg.GetConversation(); t != "" {
		msg.Conversation = nil
		msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{Text: proto.String(t)}
	}
	set := func(ci **waE2E.ContextInfo) {
		if *ci == nil {
			*ci = &waE2E.ContextInfo{}
		}
		(*ci).Expiration = proto.Uint32(secs)
	}
	switch {
	case msg.ExtendedTextMessage != nil:
		set(&msg.ExtendedTextMessage.ContextInfo)
	case msg.ImageMessage != nil:
		set(&msg.ImageMessage.ContextInfo)
	case msg.VideoMessage != nil:
		set(&msg.VideoMessage.ContextInfo)
	case msg.PtvMessage != nil:
		set(&msg.PtvMessage.ContextInfo)
	case msg.AudioMessage != nil:
		set(&msg.AudioMessage.ContextInfo)
	case msg.DocumentMessage != nil:
		set(&msg.DocumentMessage.ContextInfo)
	case msg.StickerMessage != nil:
		set(&msg.StickerMessage.ContextInfo)
	case msg.ContactMessage != nil:
		set(&msg.ContactMessage.ContextInfo)
	case msg.LocationMessage != nil:
		set(&msg.LocationMessage.ContextInfo)
	}
}

// backfillExpiry roda uma vez: as mensagens guardadas antes do zapterm
// conhecer as temporárias trazem o prazo no protobuf. Daí saem o vencimento
// de cada uma e o prazo atual de cada conversa (o da última recebida).
func (d *Daemon) backfillExpiry() {
	const key = "expiry_backfill"
	if d.st.Done(key) {
		return
	}
	exp := map[[2]string]int64{}
	last := map[string]uint32{}
	err := d.st.RawMessages(func(chat, id string, fromMe bool, ts int64, raw []byte) {
		var m waE2E.Message
		if proto.Unmarshal(raw, &m) != nil {
			return
		}
		secs := contextInfo(&m).GetExpiration()
		if !fromMe {
			last[chat] = secs
		}
		if secs > 0 {
			exp[[2]string{chat, id}] = ts + int64(secs)*1000
		}
	})
	if err == nil {
		err = d.st.SetExpiries(exp)
	}
	if err != nil {
		d.log.Warn("temporárias: lendo o banco", "err", err)
		return
	}
	chats := 0
	for chat, secs := range last {
		if secs > 0 && d.st.Ephemeral(chat) == 0 {
			if _, err := d.st.SetEphemeral(chat, int(secs)); err == nil {
				chats++
			}
		}
	}
	_ = d.st.MarkDone(key)
	d.log.Info("temporárias reconhecidas no banco", "mensagens", len(exp), "conversas", chats)
}

// purgeLoop apaga do banco as temporárias vencidas, como o celular faz, e
// manda as TUIs recarregarem quando sumiu alguma.
func (d *Daemon) purgeLoop(ctx context.Context) {
	d.backfillExpiry()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if n, err := d.st.PurgeExpired(); err != nil {
			d.log.Warn("apagando temporárias vencidas", "err", err)
		} else if n > 0 {
			d.log.Info("temporárias vencidas apagadas", "total", n)
			d.srv.Broadcast(ipc.EvReload, nil)
			d.scheduleUnreadPush()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// onLockChat trata a conversa trancada no celular (código secreto): ela
// some da lista, da busca, das notificações e do contador de não lidas.
func (d *Daemon) onLockChat(e *events.AppState) bool {
	if len(e.Index) < 2 || e.Index[0] != appstate.IndexLock {
		return false
	}
	jid, err := types.ParseJID(e.Index[1])
	if err != nil {
		return true
	}
	ctx := context.Background()
	chat := d.normalize(ctx, jid).String()
	locked := e.GetLockChatAction().GetLocked()
	if d.st.IsLocked(chat) == locked {
		return true
	}
	if err := d.st.SetLocked(chat, locked); err != nil {
		d.log.Warn("conversa trancada", "err", err)
		return true
	}
	d.srv.Broadcast(ipc.EvReload, nil)
	d.scheduleUnreadPush()
	return true
}
