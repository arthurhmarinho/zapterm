package wa

import (
	"context"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// chatAction fixa, arquiva, silencia ou marca como lida uma conversa, pelo
// app state: o celular e os outros aparelhos recebem a mudança também.
func (d *Daemon) chatAction(ctx context.Context, chat, action string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("chat inválido: %w", err)
	}
	// O WhatsApp grava a maioria dessas configurações pelo LID (ver
	// chatSettings); mandamos a mudança pelo mesmo endereço.
	target := jid
	if jid.Server == types.DefaultUserServer {
		if lid, err := d.cli.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
			target = lid
		}
	}

	var patch appstate.PatchInfo
	switch action {
	case "read":
		return d.markRead(ctx, chat)
	case "pin", "unpin":
		patch = appstate.BuildPin(target, action == "pin")
	case "mute8h":
		patch = appstate.BuildMute(target, true, 8*time.Hour)
	case "mute1w":
		patch = appstate.BuildMute(target, true, 7*24*time.Hour)
	case "muteforever":
		patch = appstate.BuildMute(target, true, 0)
	case "unmute":
		patch = appstate.BuildMute(target, false, 0)
	case "archive", "unarchive":
		// Arquivar leva a última mensagem do chat (o app usa para saber o
		// que estava arquivado quando chegar mensagem nova).
		var ts time.Time
		var key *waCommon.MessageKey
		if last, err := d.st.Messages(chat, 1, 0); err == nil && len(last) == 1 {
			ts = time.UnixMilli(last[0].TS)
			key = &waCommon.MessageKey{RemoteJID: proto.String(target.String()), FromMe: proto.Bool(last[0].FromMe),
				ID: proto.String(last[0].ID)}
			if jid.Server == types.GroupServer && !last[0].FromMe {
				key.Participant = proto.String(last[0].Sender)
			}
		}
		patch = appstate.BuildArchive(target, action == "archive", ts, key)
	default:
		return fmt.Errorf("ação desconhecida: %q", action)
	}
	if err := d.cli.SendAppState(ctx, patch); err != nil {
		return err
	}
	d.pushChat(chat)
	return nil
}
