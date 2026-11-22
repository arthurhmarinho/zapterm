package wa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Respostas a eventos (Vou / Talvez / Não vou). Vão cifradas com o segredo
// do evento, como o voto de enquete; o whatsmeow só expõe a cifra pela API
// interna. As respostas ficam na tabela de votos (poll_votes), com a
// resposta no lugar das opções.

var rsvpNames = map[waE2E.EventResponseMessage_EventResponseType]string{
	waE2E.EventResponseMessage_GOING:     "going",
	waE2E.EventResponseMessage_MAYBE:     "maybe",
	waE2E.EventResponseMessage_NOT_GOING: "not_going",
}

// rsvpLabels: a ordem e o rótulo das respostas no cartão.
var rsvpLabels = [][2]string{{"going", "Vou"}, {"maybe", "Talvez"}, {"not_going", "Não vou"}}

func (d *Daemon) saveRSVP(chat, eventID, voter string, r *waE2E.EventResponseMessage, ts int64) bool {
	name, ok := rsvpNames[r.GetResponse()]
	var opts []string
	if ok {
		opts = []string{name}
	}
	changed, err := d.st.SetPollVote(chat, eventID, voter, opts, ts)
	return err == nil && changed
}

// onEventResponse trata uma resposta recebida (de alguém, ou sua em outro
// aparelho). Devolve true se a mensagem era uma resposta.
func (d *Daemon) onEventResponse(ctx context.Context, e *events.Message) bool {
	enc := e.Message.GetEncEventResponseMessage()
	if enc == nil {
		return false
	}
	plain, err := d.cli.DangerousInternals().DecryptMsgSecret(ctx, e, whatsmeow.EncSecretEventResponse, enc, enc.GetEventCreationMessageKey())
	if err != nil {
		d.log.Debug("resposta de evento", "err", err)
		return true
	}
	var r waE2E.EventResponseMessage
	if proto.Unmarshal(plain, &r) != nil {
		return true
	}
	chat := d.normalize(ctx, e.Info.Chat).String()
	voter := d.normalize(ctx, e.Info.Sender.ToNonAD()).String()
	if e.Info.IsFromMe {
		voter = d.me()
	}
	eventID := enc.GetEventCreationMessageKey().GetID()
	if d.saveRSVP(chat, eventID, voter, &r, e.Info.Timestamp.UnixMilli()) {
		d.pushUpdate(chat, eventID)
	}
	return true
}

// historyRSVPs grava as respostas que vêm junto do evento no histórico.
func (d *Daemon) historyRSVPs(ctx context.Context, chat types.JID, wm *waWeb.WebMessageInfo) {
	eventID := wm.GetKey().GetID()
	for _, u := range wm.GetEventResponses() {
		k := u.GetEventResponseMessageKey()
		voter := chat.String()
		switch {
		case k.GetFromMe():
			voter = d.me()
		case k.GetParticipant() != "":
			if j, err := types.ParseJID(k.GetParticipant()); err == nil {
				voter = d.normalize(ctx, j.ToNonAD()).String()
			}
		}
		d.saveRSVP(chat.String(), eventID, voter, u.GetEventResponseMessage(), u.GetTimestampMS())
	}
}

// fillRSVPs põe no cartão do evento quantos vão, talvez e não vão.
func (d *Daemon) fillRSVPs(c *ipc.Card, m *ipc.Message) {
	votes, _ := d.st.PollVotes(m.Chat, m.ID)
	me := d.me()
	for _, l := range rsvpLabels {
		c.Options = append(c.Options, ipc.CardOption{Name: l[1]})
	}
	for voter, rs := range votes {
		for _, r := range rs {
			for i, l := range rsvpLabels {
				if r == l[0] {
					c.Options[i].Votes++
					c.Options[i].Mine = c.Options[i].Mine || voter == me
					c.Voters++
				}
			}
		}
	}
}

// eventRSVP responde ao evento msgID: going, maybe ou not_going.
func (d *Daemon) eventRSVP(ctx context.Context, chat, msgID, answer string) error {
	var resp waE2E.EventResponseMessage_EventResponseType
	for k, v := range rsvpNames {
		if v == answer {
			resp = k
		}
	}
	if resp == waE2E.EventResponseMessage_UNKNOWN {
		return fmt.Errorf("resposta desconhecida: %q", answer)
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	orig, raw, err := d.st.Message(chat, msgID)
	if err != nil {
		return err
	}
	var pm waE2E.Message
	if len(raw) == 0 || proto.Unmarshal(raw, &pm) != nil || pm.GetEventMessage() == nil {
		return errors.New("evento do histórico antigo: responda pelo celular")
	}
	if pm.GetEventMessage().GetIsCanceled() {
		return errors.New("o evento foi cancelado")
	}
	sender, err := types.ParseJID(orig.Sender)
	if err != nil {
		return err
	}
	if orig.FromMe {
		sender = d.cli.Store.ID.ToNonAD()
	}
	in := d.cli.DangerousInternals()
	ownID := in.GetOwnLID()
	if sender.Server == types.DefaultUserServer {
		ownID = in.GetOwnID()
	}
	r := &waE2E.EventResponseMessage{Response: resp.Enum(), TimestampMS: proto.Int64(time.Now().UnixMilli())}
	plain, err := proto.Marshal(r)
	if err != nil {
		return err
	}
	ct, iv, err := in.EncryptMsgSecret(ctx, ownID, jid, sender, msgID, whatsmeow.EncSecretEventResponse, plain)
	if err != nil {
		if errors.Is(err, whatsmeow.ErrOriginalMessageSecretNotFound) {
			return errors.New("o zapterm não tem a chave deste evento: responda pelo celular")
		}
		return fmt.Errorf("cifrando a resposta: %w", err)
	}
	key := &waCommon.MessageKey{RemoteJID: proto.String(jid.String()), FromMe: proto.Bool(orig.FromMe), ID: proto.String(msgID)}
	if jid.Server == types.GroupServer && !orig.FromMe {
		key.Participant = proto.String(sender.String())
	}
	msg := &waE2E.Message{EncEventResponseMessage: &waE2E.EncEventResponseMessage{
		EventCreationMessageKey: key, EncPayload: ct, EncIV: iv,
	}}
	if _, err := d.cli.SendMessage(ctx, jid, msg); err != nil {
		return err
	}
	if d.saveRSVP(chat, msgID, d.me(), r, time.Now().UnixMilli()) {
		d.pushUpdate(chat, msgID)
	}
	return nil
}
