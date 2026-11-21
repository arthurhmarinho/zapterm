package wa

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Criar enquete, evento e mandar contato (clipe ou comandos /).

// keepSecret guarda o segredo de uma mensagem sua (enquete, evento): sem
// ele não dá para ler votos e respostas nem votar depois. O whatsmeow só
// guarda os das mensagens recebidas.
func (d *Daemon) keepSecret(ctx context.Context, chat types.JID, id string, secret []byte) {
	me := d.cli.Store.ID.ToNonAD()
	for _, sender := range []types.JID{me, d.cli.Store.GetLID().ToNonAD()} {
		if sender.IsEmpty() {
			continue
		}
		if err := d.cli.Store.MsgSecrets.PutMessageSecret(ctx, chat, sender, id, secret); err != nil {
			d.log.Warn("guardando segredo", "err", err)
		}
	}
}

func (d *Daemon) sendPoll(ctx context.Context, chat, question string, options []string, single bool) (any, error) {
	question = strings.TrimSpace(question)
	var opts []string
	seen := map[string]bool{}
	for _, o := range options {
		if o = strings.TrimSpace(o); o != "" && !seen[o] {
			seen[o] = true
			opts = append(opts, o)
		}
	}
	switch {
	case question == "":
		return nil, errors.New("a enquete precisa de uma pergunta")
	case len(opts) < 2:
		return nil, errors.New("a enquete precisa de pelo menos 2 opções diferentes")
	case len(opts) > 12:
		return nil, errors.New("no máximo 12 opções")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	count := 0
	if single {
		count = 1
	}
	msg := d.cli.BuildPollCreation(question, opts, count)
	secret := msg.GetMessageContextInfo().GetMessageSecret()
	return d.sendAndStore(ctx, jid, msg, ipc.Message{Kind: "other", Text: "[enquete: " + question + "]"},
		func(id string) *ipc.Media { d.keepSecret(ctx, jid, id, secret); return nil })
}

func (d *Daemon) sendEvent(ctx context.Context, chat, name string, start int64, desc, place string) (any, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("o evento precisa de um nome")
	}
	if start <= 0 {
		return nil, errors.New("o evento precisa de data e hora")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	ev := &waE2E.EventMessage{Name: proto.String(name), StartTime: proto.Int64(start / 1000), IsCanceled: proto.Bool(false)}
	if desc = strings.TrimSpace(desc); desc != "" {
		ev.Description = proto.String(desc)
	}
	if place = strings.TrimSpace(place); place != "" {
		ev.Location = &waE2E.LocationMessage{Name: proto.String(place)}
	}
	msg := &waE2E.Message{EventMessage: ev, MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: secret}}
	return d.sendAndStore(ctx, jid, msg, ipc.Message{Kind: "other", Text: eventText(ev)},
		func(id string) *ipc.Media { d.keepSecret(ctx, jid, id, secret); return nil })
}

// sendContact manda o cartão de contato (vCard) de alguém da sua lista.
func (d *Daemon) sendContact(ctx context.Context, chat, contact, name string) (any, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	who, err := types.ParseJID(contact)
	if err != nil || who.Server != types.DefaultUserServer {
		return nil, fmt.Errorf("contato inválido: %q", contact)
	}
	name = strings.TrimSpace(strings.TrimLeft(name, "~"))
	if name == "" {
		name = "+" + who.User
	}
	vcard := fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nN:;%s;;;\nFN:%s\nTEL;type=CELL;type=VOICE;waid=%s:+%s\nEND:VCARD",
		name, name, who.User, who.User)
	msg := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{DisplayName: proto.String(name), Vcard: proto.String(vcard)}}
	return d.sendAndStore(ctx, jid, msg, ipc.Message{Kind: "other", Text: "[contato: " + name + "]"}, nil)
}
