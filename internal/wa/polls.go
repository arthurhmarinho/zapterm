package wa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Enquetes. O voto chega cifrado com o segredo da enquete (o whatsmeow
// guarda o segredo das que recebe) e traz o SHA-256 do nome de cada opção
// escolhida. O último voto de cada pessoa vale.

func hexHashes(hs [][]byte) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, hex.EncodeToString(h))
	}
	return out
}

// onPollVote trata um voto recebido (de alguém ou seu, em outro aparelho).
// Devolve true se a mensagem era um voto.
func (d *Daemon) onPollVote(ctx context.Context, e *events.Message) bool {
	pu := e.Message.GetPollUpdateMessage()
	if pu == nil {
		return false
	}
	vote, err := d.cli.DecryptPollVote(ctx, e)
	if err != nil {
		d.log.Debug("voto de enquete", "err", err)
		return true
	}
	chat := d.normalize(ctx, e.Info.Chat).String()
	voter := d.normalize(ctx, e.Info.Sender.ToNonAD()).String()
	if e.Info.IsFromMe {
		voter = d.me()
	}
	pollID := pu.GetPollCreationMessageKey().GetID()
	if changed, err := d.st.SetPollVote(chat, pollID, voter, hexHashes(vote.GetSelectedOptions()), e.Info.Timestamp.UnixMilli()); err == nil && changed {
		d.pushUpdate(chat, pollID)
	}
	return true
}

// historyPolls grava os votos que vêm junto da enquete no histórico (já
// decifrados pelo celular).
func (d *Daemon) historyPolls(ctx context.Context, chat types.JID, wm *waWeb.WebMessageInfo) {
	pollID := wm.GetKey().GetID()
	for _, u := range wm.GetPollUpdates() {
		k := u.GetPollUpdateMessageKey()
		voter := chat.String()
		switch {
		case k.GetFromMe():
			voter = d.me()
		case k.GetParticipant() != "":
			if j, err := types.ParseJID(k.GetParticipant()); err == nil {
				voter = d.normalize(ctx, j.ToNonAD()).String()
			}
		}
		_, _ = d.st.SetPollVote(chat.String(), pollID, voter, hexHashes(u.GetVote().GetSelectedOptions()), u.GetSenderTimestampMS())
	}
}

// pollCard monta o cartão da enquete com o placar.
func (d *Daemon) pollCard(m *ipc.Message, p *waE2E.PollCreationMessage) *ipc.Card {
	c := &ipc.Card{Type: "poll", Title: strings.TrimSpace(p.GetName()), Multi: p.GetSelectableOptionsCount() != 1}
	votes, _ := d.st.PollVotes(m.Chat, m.ID)
	me := d.me()
	idx := map[string]int{}
	for i, o := range p.GetOptions() {
		h := sha256.Sum256([]byte(o.GetOptionName()))
		idx[hex.EncodeToString(h[:])] = i
		c.Options = append(c.Options, ipc.CardOption{Name: o.GetOptionName()})
	}
	for voter, hs := range votes {
		counted := false
		for _, h := range hs {
			if i, ok := idx[h]; ok {
				c.Options[i].Votes++
				if voter == me {
					c.Options[i].Mine = true
				}
				counted = true
			}
		}
		if counted {
			c.Voters++
		}
	}
	return c
}

// pollVote vota (ou tira o voto, com options vazio) numa enquete.
func (d *Daemon) pollVote(ctx context.Context, chat, msgID string, options []string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	orig, raw, err := d.st.Message(chat, msgID)
	if err != nil {
		return err
	}
	var pm waE2E.Message
	if len(raw) == 0 || proto.Unmarshal(raw, &pm) != nil || pollOf(&pm) == nil {
		return errors.New("enquete do histórico antigo: vote pelo celular")
	}
	sender, err := types.ParseJID(orig.Sender)
	if err != nil {
		return err
	}
	if orig.FromMe {
		sender = d.cli.Store.ID.ToNonAD()
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jid, Sender: sender, IsFromMe: orig.FromMe, IsGroup: jid.Server == types.GroupServer},
		ID:            orig.ID,
		Timestamp:     time.UnixMilli(orig.TS),
	}
	msg, err := d.cli.BuildPollVote(ctx, info, options)
	if err != nil {
		if errors.Is(err, whatsmeow.ErrOriginalMessageSecretNotFound) {
			return errors.New("o zapterm não tem a chave desta enquete: vote pelo celular")
		}
		return fmt.Errorf("montando o voto: %w", err)
	}
	if _, err := d.cli.SendMessage(ctx, jid, msg); err != nil {
		return err
	}
	if _, err := d.st.SetPollVote(chat, msgID, d.me(), hexHashes(whatsmeow.HashPollOptions(options)), time.Now().UnixMilli()); err != nil {
		return err
	}
	d.pushUpdate(chat, msgID)
	return nil
}
