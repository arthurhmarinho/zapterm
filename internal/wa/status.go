package wa

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Status (stories): ficam no banco como mensagens da conversa
// status@broadcast, que não aparece na lista, e vencem em 24 h como as
// temporárias. Visto = Status 3 na mensagem.

const (
	statusChat = "status@broadcast"
	statusTTL  = 24 * time.Hour
)

// statuses: os status das últimas 24 h, do mais antigo ao mais novo.
func (d *Daemon) statuses(ctx context.Context) ([]ipc.Message, error) {
	msgs, err := d.st.Messages(statusChat, 500, 0)
	if err != nil {
		return nil, err
	}
	out := msgs[:0]
	for _, m := range msgs {
		if m.Kind != "revoked" {
			out = append(out, m)
		}
	}
	d.fillSenders(ctx, out)
	d.fillThumbs(out)
	return out, nil
}

// viewStatus marca como vistos os status ids de sender (manda o "visto"
// se [privacy] read_receipts deixa).
func (d *Daemon) viewStatus(ctx context.Context, sender string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	who, err := types.ParseJID(sender)
	if err != nil {
		return err
	}
	if d.cfg.Privacy.ReadReceipts {
		mids := make([]types.MessageID, len(ids))
		for i, id := range ids {
			mids[i] = id
		}
		if err := d.cli.MarkRead(ctx, mids, time.Now(), types.StatusBroadcastJID, who); err != nil {
			d.log.Warn("status visto", "err", err)
		}
	}
	return d.st.SetStatus(statusChat, ids, 3)
}

// postStatus publica um status de texto, com fundo verde.
func (d *Daemon) postStatus(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("status vazio")
	}
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:           proto.String(text),
		BackgroundArgb: proto.Uint32(0xFF128C7E),
		TextArgb:       proto.Uint32(0xFFFFFFFF),
		Font:           waE2E.ExtendedTextMessage_SYSTEM.Enum(),
	}}
	resp, err := d.cli.SendMessage(ctx, types.StatusBroadcastJID, msg)
	if err != nil {
		return err
	}
	raw, _ := proto.Marshal(msg)
	m := ipc.Message{ID: resp.ID, Chat: statusChat, Sender: d.me(), SenderName: "você", FromMe: true,
		TS: resp.Timestamp.UnixMilli(), Kind: "text", Text: text, Expires: resp.Timestamp.Add(statusTTL).UnixMilli()}
	_, err = d.st.SaveMessage(m, raw, true, false)
	return err
}
