package wa

import (
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
)

// desktop fala com o servidor de notificações (Noctalia, no Sway dele)
// direto pelo D-Bus, em vez do notify-send, para receber o clique e a
// resposta digitada na própria notificação:
//   - ação "default" (clicar na notificação) → abrir o chat;
//   - "inline-reply" (extensão que o Noctalia e o Plasma anunciam) → o
//     texto chega no sinal NotificationReplied e vira mensagem.
//
// https://specifications.freedesktop.org/notification-spec/latest/
type desktop struct {
	conn    *dbus.Conn
	obj     dbus.BusObject
	reply   bool // servidor aceita resposta embutida
	mu      sync.Mutex
	chatOf  map[uint32]string // id da notificação → chat
	onOpen  func(chat string)
	onReply func(chat, text string)
	// onAction recebe as outras ações (ex.: "reject" da chamada tocando).
	onAction func(chat, key string)
}

const notifIface = "org.freedesktop.Notifications"

func newDesktop(onOpen func(string), onReply func(string, string)) (*desktop, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	d := &desktop{
		conn: conn, obj: conn.Object(notifIface, "/org/freedesktop/Notifications"),
		chatOf: map[uint32]string{}, onOpen: onOpen, onReply: onReply,
	}
	var caps []string
	if err := d.obj.Call(notifIface+".GetCapabilities", 0).Store(&caps); err == nil {
		for _, c := range caps {
			if c == "inline-reply" {
				d.reply = true
			}
		}
	}
	for _, member := range []string{"ActionInvoked", "NotificationReplied", "NotificationClosed"} {
		if err := conn.AddMatchSignal(dbus.WithMatchInterface(notifIface), dbus.WithMatchMember(member)); err != nil {
			return nil, err
		}
	}
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	go d.listen(signals)
	return d, nil
}

func (d *desktop) listen(signals chan *dbus.Signal) {
	for sig := range signals {
		if len(sig.Body) < 2 {
			continue
		}
		id, _ := sig.Body[0].(uint32)
		d.mu.Lock()
		chat := d.chatOf[id]
		d.mu.Unlock()
		if chat == "" {
			continue // notificação de outro programa
		}
		switch sig.Name {
		case notifIface + ".ActionInvoked":
			switch key, _ := sig.Body[1].(string); {
			case key == "default":
				go d.onOpen(chat)
			case key != "" && d.onAction != nil:
				go d.onAction(chat, key)
			}
		case notifIface + ".NotificationReplied":
			if text, _ := sig.Body[1].(string); text != "" {
				go d.onReply(chat, text)
			}
		case notifIface + ".NotificationClosed":
			d.mu.Lock()
			delete(d.chatOf, id)
			d.mu.Unlock()
		}
	}
}

// show mostra (ou substitui, se replaces != 0) a notificação de um chat e
// devolve o id dela. extra são pares chave, rótulo de ações a mais; com
// elas não há resposta embutida (a chamada tocando tem "Recusar").
func (d *desktop) show(chat, title, body, icon string, replaces uint32, extra ...string) (uint32, error) {
	actions := append([]string{"default", "Abrir"}, extra...)
	if d.reply && len(extra) == 0 {
		actions = append(actions, "inline-reply", "Responder")
	}
	hints := map[string]dbus.Variant{
		"category":                     dbus.MakeVariant("im.received"),
		"x-kde-reply-placeholder-text": dbus.MakeVariant("Responder…"),
	}
	if icon != "" {
		hints["image-path"] = dbus.MakeVariant(icon)
	}
	var id uint32
	err := d.obj.Call(notifIface+".Notify", 0, "zapterm", replaces, icon, title, body, actions, hints, int32(-1)).Store(&id)
	if err != nil {
		return 0, fmt.Errorf("notificação: %w", err)
	}
	d.mu.Lock()
	d.chatOf[id] = chat
	d.mu.Unlock()
	return id, nil
}

// close fecha a notificação (a chamada foi atendida ou recusada).
func (d *desktop) close(id uint32) {
	if id == 0 {
		return
	}
	_ = d.obj.Call(notifIface+".CloseNotification", 0, id).Err
}
