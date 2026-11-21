package wa

import (
	"context"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// onCallOffer registra a chamada recebida na conversa e notifica. O
// whatsmeow só tem a sinalização da chamada, não o áudio: atender fica
// para o celular.
func (d *Daemon) onCallOffer(e *events.CallOffer) {
	ctx := context.Background()
	if e.GroupJID.User != "" {
		return // chamada em grupo: o celular avisa
	}
	from := d.normalize(ctx, e.From.ToNonAD())
	label := "📞 Chamada de voz recebida"
	if e.Data != nil {
		if _, ok := e.Data.GetOptionalChildByTag("video"); ok {
			label = "📹 Chamada de vídeo recebida"
		}
	}
	m := ipc.Message{
		ID:         "call-" + e.CallID,
		Chat:       from.String(),
		Sender:     from.String(),
		SenderName: d.contactName(ctx, from, ""),
		TS:         e.Timestamp.UnixMilli(),
		Kind:       "other",
		Text:       label + " · atenda no celular",
	}
	isNew, err := d.st.SaveMessage(m, nil, from.Server == types.GroupServer, true)
	if err != nil || !isNew {
		return
	}
	d.srv.Broadcast(ipc.EvMessage, m)
	d.ensureName(from)
	c := d.pushChat(m.Chat)
	go d.notify(m, from, c.Name)
}
