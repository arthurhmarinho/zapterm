package wa

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// onChange trata as mensagens que mudam outra mensagem em vez de aparecer
// sozinhas: remoção ("apagar para todos"), edição e reação. Devolve true se
// a mensagem era uma delas (e não deve ser gravada como nova).
func (d *Daemon) onChange(ctx context.Context, e *events.Message, live bool) bool {
	chat := d.normalize(ctx, e.Info.Chat).String()

	if pm := e.Message.GetProtocolMessage(); pm != nil {
		target := pm.GetKey().GetID()
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			d.applyUpdate(chat, target, "revoked", "", false, live)
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if kind, text := describe(pm.GetEditedMessage()); kind != "" {
				d.applyUpdate(chat, target, kind, text, true, live)
			}
		}
		return true // mensagem de protocolo nunca aparece sozinha
	}

	if r := e.Message.GetReactionMessage(); r != nil {
		sender := d.normalize(ctx, e.Info.Sender).String()
		if e.Info.IsFromMe {
			sender = d.me()
		}
		ts := r.GetSenderTimestampMS()
		if ts == 0 {
			ts = e.Info.Timestamp.UnixMilli()
		}
		target := r.GetKey().GetID()
		if err := d.st.SetReaction(chat, target, sender, r.GetText(), ts); err != nil {
			d.log.Error("gravando reação", "err", err)
		} else if live {
			d.pushUpdate(chat, target)
		}
		return true
	}
	return false
}

func (d *Daemon) applyUpdate(chat, id, kind, text string, edited, live bool) {
	ok, err := d.st.UpdateMessage(chat, id, kind, text, edited)
	if err != nil {
		d.log.Error("atualizando mensagem", "err", err)
		return
	}
	if ok && live {
		d.pushUpdate(chat, id)
		d.pushChat(chat)
	}
}

// historyReactions grava as reações que vêm junto de cada mensagem no
// history sync.
func (d *Daemon) historyReactions(ctx context.Context, chat types.JID, wm *waWeb.WebMessageInfo) {
	msgID := wm.GetKey().GetID()
	for _, r := range wm.GetReactions() {
		sender := chat.String()
		switch {
		case r.GetKey().GetFromMe():
			sender = d.me()
		case r.GetKey().GetParticipant() != "":
			if j, err := types.ParseJID(r.GetKey().GetParticipant()); err == nil {
				sender = d.normalize(ctx, j).String()
			}
		}
		_ = d.st.SetReaction(chat.String(), msgID, sender, r.GetText(), r.GetSenderTimestampMS())
	}
}

// pushUpdate manda às TUIs a versão atual de uma mensagem.
func (d *Daemon) pushUpdate(chat, id string) {
	m, _, err := d.st.Message(chat, id)
	if err != nil {
		return
	}
	one := []ipc.Message{m}
	ctx := context.Background()
	d.fillSenders(ctx, one)
	d.fillMentions(ctx, one)
	d.fillThumbs(one)
	d.fillReactions(one)
	d.srv.Broadcast(ipc.EvUpdate, one[0])
}

func (d *Daemon) fillReactions(msgs []ipc.Message) {
	if len(msgs) == 0 {
		return
	}
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	rs, err := d.st.Reactions(msgs[0].Chat, ids, d.me())
	if err != nil {
		d.log.Warn("lendo reações", "err", err)
		return
	}
	for i := range msgs {
		msgs[i].Reactions = rs[msgs[i].ID]
	}
}

func (d *Daemon) me() string {
	if id := d.cli.Store.ID; id != nil {
		return id.ToNonAD().String()
	}
	return ""
}

// react reage (ou, com emoji vazio, tira a reação) a uma mensagem.
func (d *Daemon) react(ctx context.Context, chat, msgID, emoji string) error {
	chatJID, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("chat inválido: %w", err)
	}
	orig, _, err := d.st.Message(chat, msgID)
	if err != nil {
		return fmt.Errorf("mensagem não encontrada: %w", err)
	}
	sender := d.cli.Store.ID.ToNonAD()
	if !orig.FromMe {
		if sender, err = types.ParseJID(orig.Sender); err != nil {
			return err
		}
	}
	if _, err := d.cli.SendMessage(ctx, chatJID, d.cli.BuildReaction(chatJID, sender, msgID, emoji)); err != nil {
		return err
	}
	if err := d.st.SetReaction(chat, msgID, d.me(), emoji, time.Now().UnixMilli()); err != nil {
		return err
	}
	d.pushUpdate(chat, msgID)
	return nil
}
