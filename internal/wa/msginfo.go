package wa

import (
	"context"
	"sort"

	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// historyReceipts guarda os recibos por pessoa que vêm no histórico.
func (d *Daemon) historyReceipts(ctx context.Context, chat, msgID string, rs []*waWeb.UserReceipt) {
	for _, r := range rs {
		j, err := types.ParseJID(r.GetUserJID())
		if err != nil {
			continue
		}
		user := d.normalize(ctx, j.ToNonAD()).String()
		_ = d.st.AddReceipt(chat, msgID, user, 2, r.GetReceiptTimestamp()*1000)
		_ = d.st.AddReceipt(chat, msgID, user, 3, r.GetReadTimestamp()*1000)
		_ = d.st.AddReceipt(chat, msgID, user, 4, r.GetPlayedTimestamp()*1000)
	}
}

// msgInfo monta "Dados da mensagem". Só vale o que o daemon viu chegar
// (ou veio no histórico): o WhatsApp não deixa consultar depois.
func (d *Daemon) msgInfo(ctx context.Context, chat, msgID string) (*ipc.MsgInfo, error) {
	m, _, err := d.st.Message(chat, msgID)
	if err != nil {
		return nil, err
	}
	info := &ipc.MsgInfo{FromMe: m.FromMe}
	names := map[string]string{}
	if m.FromMe {
		rs, err := d.st.Receipts(chat, msgID)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			name := d.senderLabel(ctx, r.User, names)
			switch {
			case r.Played > 0:
				info.Played = append(info.Played, ipc.Who{Name: name, TS: r.Played})
			case r.Read > 0:
				info.Read = append(info.Read, ipc.Who{Name: name, TS: r.Read})
			case r.Delivered > 0:
				info.Delivered = append(info.Delivered, ipc.Who{Name: name, TS: r.Delivered})
			}
		}
	}
	rx, err := d.st.ReactionsBy(chat, msgID)
	if err != nil {
		return nil, err
	}
	for _, r := range rx {
		info.Reactions = append(info.Reactions, ipc.Who{Name: d.senderLabel(ctx, r[0], names), Emoji: r[1]})
	}
	for _, l := range [][]ipc.Who{info.Read, info.Played, info.Delivered} {
		sort.Slice(l, func(a, b int) bool { return l[a].TS < l[b].TS })
	}
	return info, nil
}
