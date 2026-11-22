package wa

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Chamadas recebidas. O whatsmeow só tem a sinalização, não o áudio:
// atender fica para o celular, mas dá para recusar daqui e saber como a
// chamada acabou. Cada uma vira uma linha na conversa que muda de estado:
// tocando → atendida no celular (com a duração), recusada ou perdida.

type liveCall struct {
	chat     string // conversa (JID normalizado)
	msgID    string // linha da chamada na conversa
	creator  types.JID
	name     string
	video    bool
	notif    uint32    // notificação "tocando" (0 = não mostrou)
	accepted time.Time // atendida em algum aparelho
	rejected bool
}

var (
	callsMu sync.Mutex
	calls   = map[string]*liveCall{} // call-id → chamada tocando ou em andamento
)

// Estados da linha da chamada.
const (
	callRinging  = "ringing"
	callAnswered = "answered"
	callRejected = "rejected"
	callMissed   = "missed"
)

// callText é o texto da linha da chamada na conversa.
func callText(video bool, state string, dur time.Duration) string {
	icon, kind := "📞", "voz"
	if video {
		icon, kind = "📹", "vídeo"
	}
	switch state {
	case callAnswered:
		t := fmt.Sprintf("%s Chamada de %s · atendida no celular", icon, kind)
		if s := int(dur.Round(time.Second).Seconds()); s > 0 {
			t += fmt.Sprintf(" · %d:%02d", s/60, s%60)
		}
		return t
	case callRejected:
		return fmt.Sprintf("%s Chamada de %s recusada", icon, kind)
	case callMissed:
		return fmt.Sprintf("%s Chamada de %s perdida", icon, kind)
	}
	return fmt.Sprintf("%s Chamada de %s recebida · atenda no celular", icon, kind)
}

// onCallOffer registra a chamada recebida na conversa e notifica, com
// "Recusar" na notificação.
func (d *Daemon) onCallOffer(e *events.CallOffer) {
	ctx := context.Background()
	if e.GroupJID.User != "" {
		return // chamada em grupo: o celular avisa
	}
	from := d.normalize(ctx, e.From.ToNonAD())
	video := false
	if e.Data != nil {
		_, video = e.Data.GetOptionalChildByTag("video")
	}
	m := ipc.Message{
		ID:         "call-" + e.CallID,
		Chat:       from.String(),
		Sender:     from.String(),
		SenderName: d.contactName(ctx, from, ""),
		TS:         e.Timestamp.UnixMilli(),
		Kind:       "other",
		Text:       callText(video, callRinging, 0),
	}
	isNew, err := d.st.SaveMessage(m, nil, from.Server == types.GroupServer, true)
	if err != nil || !isNew {
		return
	}
	creator := e.CallCreator
	if creator.IsEmpty() {
		creator = e.From
	}
	lc := &liveCall{chat: m.Chat, msgID: m.ID, creator: creator.ToNonAD(), name: m.SenderName, video: video}
	callsMu.Lock()
	calls[e.CallID] = lc
	callsMu.Unlock()

	d.srv.Broadcast(ipc.EvMessage, m)
	d.ensureName(from)
	c := d.pushChat(m.Chat)
	go func() {
		id := d.notify(m, from, c.Name, "reject", "Recusar")
		callsMu.Lock()
		_, live := calls[e.CallID]
		done := !live || !lc.accepted.IsZero() || lc.rejected
		if !done {
			lc.notif = id
		}
		callsMu.Unlock()
		// Atendida ou terminada enquanto a notificação aparecia: fecha.
		if done && d.desk != nil {
			d.desk.close(id)
		}
	}()
}

// onCallAccept: atendida em algum aparelho seu (o celular).
func (d *Daemon) onCallAccept(e *events.CallAccept) {
	d.log.Info("chamada atendida", "call", e.CallID, "from", e.From)
	callsMu.Lock()
	lc := calls[e.CallID]
	if lc != nil && lc.accepted.IsZero() {
		lc.accepted = time.Now()
	}
	callsMu.Unlock()
	if lc == nil {
		return
	}
	d.closeCallNotif(lc)
	d.applyUpdate(lc.chat, lc.msgID, "other", callText(lc.video, callAnswered, 0), false, true)
}

// onCallReject: recusada em outro aparelho seu.
func (d *Daemon) onCallReject(e *events.CallReject) {
	d.log.Info("chamada recusada", "call", e.CallID, "from", e.From)
	d.finishCall(e.CallID, callRejected)
}

// onCallTerminate: a chamada acabou. Sem ninguém atender, foi perdida.
func (d *Daemon) onCallTerminate(e *events.CallTerminate) {
	d.log.Info("chamada terminou", "call", e.CallID, "from", e.From, "reason", e.Reason)
	d.finishCall(e.CallID, "")
}

// finishCall tira a chamada da lista e grava como ela acabou (state ""
// decide pelo que aconteceu: atendida, recusada ou perdida).
func (d *Daemon) finishCall(callID, state string) {
	callsMu.Lock()
	lc := calls[callID]
	delete(calls, callID)
	callsMu.Unlock()
	if lc == nil {
		return
	}
	state, dur := callOutcome(lc, state, time.Now())
	text := callText(lc.video, state, dur)
	d.applyUpdate(lc.chat, lc.msgID, "other", text, false, true)
	if state != callMissed {
		d.closeCallNotif(lc)
		return
	}
	// Perdida: a notificação "tocando" vira "perdida" (sem o Recusar).
	callsMu.Lock()
	id := lc.notif
	callsMu.Unlock()
	if id != 0 && d.desk != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		icon := ""
		if av, err := d.avatar(ctx, lc.chat); err == nil && av != nil {
			icon = av.Preview
		}
		if _, err := d.desk.show(lc.chat, lc.name, text, icon, id); err != nil {
			d.log.Warn("notificação da chamada perdida", "err", err)
		}
	}
}

// callOutcome decide como a chamada acabou: o estado dado, ou recusada,
// atendida (com a duração até now) ou perdida.
func callOutcome(lc *liveCall, state string, now time.Time) (string, time.Duration) {
	switch {
	case state != "":
		return state, 0
	case lc.rejected:
		return callRejected, 0
	case !lc.accepted.IsZero():
		return callAnswered, now.Sub(lc.accepted)
	}
	return callMissed, 0
}

func (d *Daemon) closeCallNotif(lc *liveCall) {
	callsMu.Lock()
	id := lc.notif
	lc.notif = 0
	callsMu.Unlock()
	if d.desk != nil {
		d.desk.close(id)
	}
}

var errNoCall = errors.New("essa chamada já terminou")

// rejectCall recusa a chamada que está tocando: a da mensagem msgID, ou
// (msgID vazio) a que estiver tocando na conversa.
func (d *Daemon) rejectCall(ctx context.Context, chat, msgID string) error {
	callsMu.Lock()
	var id string
	var lc *liveCall
	for k, c := range calls {
		if c.chat == chat && (msgID == "" || c.msgID == msgID) && c.accepted.IsZero() && !c.rejected {
			id, lc = k, c
			break
		}
	}
	callsMu.Unlock()
	if lc == nil {
		return errNoCall
	}
	if err := d.cli.RejectCall(ctx, lc.creator, id); err != nil {
		return err
	}
	callsMu.Lock()
	lc.rejected = true
	callsMu.Unlock()
	// O fim da chamada (CallTerminate) pode não vir para quem recusou.
	d.finishCall(id, callRejected)
	return nil
}

// actionFromNotification: botão da notificação ("reject" na chamada).
func (d *Daemon) actionFromNotification(chat, key string) {
	if key != "reject" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.rejectCall(ctx, chat, ""); err != nil && !errors.Is(err, errNoCall) {
		d.log.Warn("recusando chamada", "err", err)
	}
}
