package wa

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Mensagem fixada no topo da conversa. Fixar e desafixar são mensagens
// (PinInChatMessage) que apontam para outra; o prazo vai em
// MessageContextInfo.MessageAddOnDurationInSecs.

// onPinMessage trata o aviso "fulano fixou/desafixou". true se era um.
func (d *Daemon) onPinMessage(ctx context.Context, e *events.Message) bool {
	p := e.Message.GetPinInChatMessage()
	if p == nil {
		return false
	}
	chat := d.normalize(ctx, e.Info.Chat).String()
	ts := e.Info.Timestamp.UnixMilli()
	pinned := p.GetType() == waE2E.PinInChatMessage_PIN_FOR_ALL
	var expires int64
	if secs := e.Message.GetMessageContextInfo().GetMessageAddOnDurationInSecs(); secs > 0 {
		expires = ts + int64(secs)*1000
	}
	if err := d.st.SetPin(chat, p.GetKey().GetID(), pinned, ts, expires); err != nil {
		d.log.Warn("mensagem fixada", "err", err)
		return true
	}
	d.srv.Broadcast(ipc.EvPins, chat)
	return true
}

// pin fixa a mensagem por secs segundos (0 desafixa).
func (d *Daemon) pin(ctx context.Context, chat, msgID string, secs int) error {
	orig, _, err := d.st.Message(chat, msgID)
	if err != nil {
		return err
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	key := &waCommon.MessageKey{RemoteJID: proto.String(chat), FromMe: proto.Bool(orig.FromMe), ID: proto.String(msgID)}
	if jid.Server == types.GroupServer && !orig.FromMe {
		key.Participant = proto.String(orig.Sender)
	}
	typ := waE2E.PinInChatMessage_PIN_FOR_ALL
	if secs == 0 {
		typ = waE2E.PinInChatMessage_UNPIN_FOR_ALL
	}
	now := time.Now()
	msg := &waE2E.Message{PinInChatMessage: &waE2E.PinInChatMessage{
		Key: key, Type: typ.Enum(), SenderTimestampMS: proto.Int64(now.UnixMilli()),
	}}
	if secs > 0 {
		msg.MessageContextInfo = &waE2E.MessageContextInfo{MessageAddOnDurationInSecs: proto.Uint32(uint32(secs))}
	}
	if _, err := d.cli.SendMessage(ctx, jid, msg); err != nil {
		return fmt.Errorf("fixando: %w", err)
	}
	var expires int64
	if secs > 0 {
		expires = now.Add(time.Duration(secs) * time.Second).UnixMilli()
	}
	if err := d.st.SetPin(chat, msgID, secs > 0, now.UnixMilli(), expires); err != nil {
		return err
	}
	d.srv.Broadcast(ipc.EvPins, chat)
	return nil
}

func (d *Daemon) pins(ctx context.Context, chat string) ([]ipc.Message, error) {
	msgs, err := d.st.Pins(chat)
	if err != nil {
		return nil, err
	}
	d.fillSenders(ctx, msgs)
	d.fillMentions(ctx, msgs)
	return msgs, nil
}

// onKeepInChat: alguém salvou (ou deixou de salvar) uma temporária.
func (d *Daemon) onKeepInChat(ctx context.Context, e *events.Message) bool {
	k := e.Message.GetKeepInChatMessage()
	if k == nil {
		return false
	}
	chat := d.normalize(ctx, e.Info.Chat).String()
	keep := k.GetKeepType() == waE2E.KeepType_KEEP_FOR_ALL
	if changed, err := d.st.SetKept(chat, k.GetKey().GetID(), keep); err == nil && changed {
		d.pushUpdate(chat, k.GetKey().GetID())
	}
	return true
}

// keepInChat salva a temporária na conversa (vale para todos).
func (d *Daemon) keepInChat(ctx context.Context, chat, msgID string, keep bool) error {
	orig, _, err := d.st.Message(chat, msgID)
	if err != nil {
		return err
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	key := &waCommon.MessageKey{RemoteJID: proto.String(chat), FromMe: proto.Bool(orig.FromMe), ID: proto.String(msgID)}
	if jid.Server == types.GroupServer && !orig.FromMe {
		key.Participant = proto.String(orig.Sender)
	}
	typ := waE2E.KeepType_KEEP_FOR_ALL
	if !keep {
		typ = waE2E.KeepType_UNDO_KEEP_FOR_ALL
	}
	msg := &waE2E.Message{KeepInChatMessage: &waE2E.KeepInChatMessage{
		Key: key, KeepType: typ.Enum(), TimestampMS: proto.Int64(time.Now().UnixMilli()),
	}}
	if _, err := d.cli.SendMessage(ctx, jid, msg); err != nil {
		return err
	}
	if changed, err := d.st.SetKept(chat, msgID, keep); err == nil && changed {
		d.pushUpdate(chat, msgID)
	}
	return nil
}
