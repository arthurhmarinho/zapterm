package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Prazos do WhatsApp para mexer numa mensagem já enviada.
const (
	editWindow   = 15 * time.Minute
	revokeWindow = 60 * time.Hour
)

// ownMessage carrega uma mensagem sua e confere o prazo.
func (d *Daemon) ownMessage(chat, id string, window time.Duration, what string) (ipc.Message, types.JID, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return ipc.Message{}, jid, fmt.Errorf("chat inválido: %w", err)
	}
	m, _, err := d.st.Message(chat, id)
	if err != nil {
		return m, jid, fmt.Errorf("mensagem não encontrada: %w", err)
	}
	if !m.FromMe {
		return m, jid, fmt.Errorf("só dá para %s as suas mensagens", what)
	}
	if time.Since(time.UnixMilli(m.TS)) > window {
		return m, jid, fmt.Errorf("passou do prazo do WhatsApp para %s", what)
	}
	return m, jid, nil
}

// revoke apaga uma mensagem sua para todos.
func (d *Daemon) revoke(ctx context.Context, chat, id string) error {
	if orig, _, err := d.st.Message(chat, id); err == nil && !orig.FromMe {
		return d.adminRevoke(ctx, chat, orig)
	}
	m, jid, err := d.ownMessage(chat, id, revokeWindow, "apagar")
	if err != nil {
		return err
	}
	if m.Kind == "revoked" {
		return nil
	}
	if _, err := d.cli.SendMessage(ctx, jid, d.cli.BuildRevoke(jid, types.EmptyJID, id)); err != nil {
		return err
	}
	d.applyUpdate(chat, id, "revoked", "", false, true)
	return nil
}

// edit troca o texto de uma mensagem sua.
func (d *Daemon) edit(ctx context.Context, chat, id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("texto vazio: para tirar a mensagem, apague (alt+d)")
	}
	m, jid, err := d.ownMessage(chat, id, editWindow, "editar")
	if err != nil {
		return err
	}
	if m.Kind != "text" {
		return errors.New("só dá para editar mensagem de texto")
	}
	if text == m.Text {
		return nil
	}
	if _, err := d.cli.SendMessage(ctx, jid, d.cli.BuildEdit(jid, id, &waE2E.Message{Conversation: proto.String(text)})); err != nil {
		return err
	}
	d.applyUpdate(chat, id, "text", text, true, true)
	return nil
}

// forward encaminha uma mensagem para outro chat, como "Encaminhada".
// Mídia reaproveita o arquivo que já está no servidor (como o app faz);
// se ele expirou, o envio falha e o erro volta para a TUI.
func (d *Daemon) forward(ctx context.Context, dest, srcChat, srcID string) (any, error) {
	jid, err := types.ParseJID(dest)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	orig, raw, err := d.st.Message(srcChat, srcID)
	if err != nil {
		return nil, fmt.Errorf("mensagem não encontrada: %w", err)
	}
	if orig.Kind == "revoked" {
		return nil, errors.New("mensagem apagada não dá para encaminhar")
	}
	var msg *waE2E.Message
	if len(raw) > 0 {
		msg = &waE2E.Message{}
		if err := proto.Unmarshal(raw, msg); err != nil {
			return nil, err
		}
	} else if orig.Kind == "text" {
		msg = &waE2E.Message{Conversation: proto.String(orig.Text)}
	} else {
		return nil, errors.New("mídia de mensagem antiga (do pareamento) não dá para encaminhar")
	}
	if err := markForwarded(msg); err != nil {
		return nil, err
	}
	m := ipc.Message{Kind: orig.Kind, Text: orig.Text}
	return d.sendAndStore(ctx, jid, msg, m, func(id string) *ipc.Media {
		return copyCachedMedia(srcID, id)
	})
}

// markForwarded põe a etiqueta de encaminhada e tira citação e menções do
// original (que não fazem sentido no outro chat).
func markForwarded(msg *waE2E.Message) error {
	ci := &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(1)}
	switch {
	case msg.Conversation != nil:
		msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{Text: msg.Conversation, ContextInfo: ci}
		msg.Conversation = nil
	case msg.ExtendedTextMessage != nil:
		msg.ExtendedTextMessage.ContextInfo = ci
	case msg.ImageMessage != nil:
		msg.ImageMessage.ContextInfo = ci
	case msg.VideoMessage != nil:
		msg.VideoMessage.ContextInfo = ci
	case msg.AudioMessage != nil:
		msg.AudioMessage.ContextInfo = ci
	case msg.DocumentMessage != nil:
		msg.DocumentMessage.ContextInfo = ci
	case msg.StickerMessage != nil:
		msg.StickerMessage.ContextInfo = ci
	default:
		return errors.New("esse tipo de mensagem não dá para encaminhar")
	}
	msg.MessageContextInfo = nil
	return nil
}

// copyCachedMedia duplica no cache a mídia já baixada da mensagem de
// origem com o ID da nova, para ela aparecer sem baixar de novo.
func copyCachedMedia(srcID, dstID string) *ipc.Media {
	src := cachedMedia(srcID)
	if src == nil {
		return nil
	}
	dstBase, err := mediaBase(dstID)
	if err != nil {
		return nil
	}
	cp := func(from string) string {
		if from == "" {
			return ""
		}
		suffix := filepath.Ext(from) // ".webp", ".jpg"…
		if strings.HasSuffix(from, ".preview.png") {
			suffix = ".preview.png"
		}
		to := dstBase + suffix
		data, err := os.ReadFile(from)
		if err != nil || os.WriteFile(to, data, 0o600) != nil {
			return ""
		}
		return to
	}
	out := &ipc.Media{Mime: src.Mime, Path: cp(src.Path), Width: src.Width, Height: src.Height}
	if len(src.Frames) == 0 {
		out.Preview = cp(src.Preview)
	}
	if out.Path == "" {
		return nil
	}
	return out
}

// deleteForMe apaga a mensagem só para você: manda a mudança pelo app state
// (o celular e os outros aparelhos apagam também) e tira do banco.
func (d *Daemon) deleteForMe(ctx context.Context, chat, msgID string) error {
	m, _, err := d.st.Message(chat, msgID)
	if err != nil {
		return err
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	fromMe, sender := "0", "0"
	if m.FromMe {
		fromMe = "1"
	} else if jid.Server == types.GroupServer {
		sender = m.Sender
	}
	del := true
	patch := appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexDeleteMessageForMe, chat, msgID, fromMe, sender},
			Version: 3,
			Value: &waSyncAction.SyncActionValue{DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
				DeleteMedia: &del, MessageTimestamp: proto.Int64(m.TS / 1000),
			}},
		}},
	}
	if err := d.cli.SendAppState(ctx, patch); err != nil {
		return fmt.Errorf("avisando o WhatsApp: %w", err)
	}
	d.removeLocal(chat, msgID)
	return nil
}

// onDeleteForMe: você apagou para si em outro aparelho.
func (d *Daemon) onDeleteForMe(ctx context.Context, e *events.DeleteForMe) {
	d.removeLocal(d.normalize(ctx, e.ChatJID).String(), e.MessageID)
}

// removeLocal tira a mensagem do banco e avisa as TUIs (Kind "deleted").
func (d *Daemon) removeLocal(chat, msgID string) {
	if _, _, err := d.st.Message(chat, msgID); err != nil {
		return
	}
	if err := d.st.DeleteMessage(chat, msgID); err != nil {
		d.log.Warn("apagando para mim", "err", err)
		return
	}
	d.srv.Broadcast(ipc.EvUpdate, ipc.Message{ID: msgID, Chat: chat, Kind: "deleted"})
	d.pushChat(chat)
}

// star favorita ou desfavorita: patch de app state (vale no celular
// também) e marca no banco.
func (d *Daemon) star(ctx context.Context, chat, msgID string, starred bool) error {
	m, _, err := d.st.Message(chat, msgID)
	if err != nil {
		return err
	}
	target, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	sender := target
	switch {
	case m.FromMe:
		sender = d.cli.Store.ID.ToNonAD()
	case target.Server == types.GroupServer:
		if sender, err = types.ParseJID(m.Sender); err != nil {
			return err
		}
	}
	if err := d.cli.SendAppState(ctx, appstate.BuildStar(target, sender, msgID, m.FromMe, starred)); err != nil {
		return fmt.Errorf("avisando o WhatsApp: %w", err)
	}
	if changed, _ := d.st.SetStarred(chat, msgID, starred); changed {
		d.pushUpdate(chat, msgID)
	}
	return nil
}

// onStar: estrela marcada ou tirada no celular (ou na carga completa das
// configurações, que traz as que já existiam).
func (d *Daemon) onStar(ctx context.Context, e *events.Star) {
	chat := d.normalize(ctx, e.ChatJID).String()
	if changed, err := d.st.SetStarred(chat, e.MessageID, e.Action.GetStarred()); err == nil && changed && !e.FromFullSync {
		d.pushUpdate(chat, e.MessageID)
	}
}

// starred: as favoritas, como resultados de busca (conversa + mensagem).
func (d *Daemon) starred(ctx context.Context) ([]ipc.SearchHit, error) {
	msgs, err := d.st.StarredMessages(300)
	if err != nil {
		return nil, err
	}
	return d.hitsFor(ctx, msgs), nil
}
