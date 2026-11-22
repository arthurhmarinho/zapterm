package wa

import (
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// pollOf devolve a enquete da mensagem, seja qual for a versão.
func pollOf(m *waE2E.Message) *waE2E.PollCreationMessage {
	for _, p := range []*waE2E.PollCreationMessage{m.GetPollCreationMessage(), m.GetPollCreationMessageV2(),
		m.GetPollCreationMessageV3(), m.GetPollCreationMessageV5(), m.GetPollCreationMessageV6()} {
		if p != nil {
			return p
		}
	}
	return nil
}

// viewOnce: foto, vídeo ou áudio de visualização única. O WhatsApp não
// entrega o conteúdo a aparelhos conectados; só dá para avisar.
func viewOnce(m *waE2E.Message) bool {
	return m.GetImageMessage().GetViewOnce() || m.GetVideoMessage().GetViewOnce() || m.GetAudioMessage().GetViewOnce()
}

// fillCards completa as mensagens com o que só o protobuf tem: o cartão
// (contato, localização, evento, enquete, prévia de link) e o selo de
// encaminhada.
func (d *Daemon) fillCards(msgs []ipc.Message) {
	for i := range msgs {
		m := &msgs[i]
		if !m.HasRaw || m.Kind == "revoked" {
			continue
		}
		_, raw, err := d.st.Message(m.Chat, m.ID)
		if err != nil || len(raw) == 0 {
			continue
		}
		var pm waE2E.Message
		if proto.Unmarshal(raw, &pm) != nil {
			continue
		}
		m.Forwarded = contextInfo(&pm).GetIsForwarded()
		if m.Kind == "other" || m.Kind == "text" {
			m.Card = d.cardOf(m, &pm)
		}
	}
}

func (d *Daemon) cardOf(m *ipc.Message, pm *waE2E.Message) *ipc.Card {
	switch {
	case pm.GetContactMessage() != nil:
		return contactCard(pm.GetContactMessage())
	case pm.GetContactsArrayMessage() != nil:
		cs := pm.GetContactsArrayMessage().GetContacts()
		c := &ipc.Card{Type: "contact", Title: fmt.Sprintf("%d contatos", len(cs))}
		for _, x := range cs {
			one := contactCard(x)
			line := one.Title
			if len(one.Lines) > 0 {
				line += " · " + one.Lines[0]
			}
			c.Lines = append(c.Lines, line)
		}
		return c
	case pm.GetLocationMessage() != nil:
		l := pm.GetLocationMessage()
		c := &ipc.Card{Type: "location", Title: firstNonEmpty(l.GetName(), "Localização"),
			URL: mapURL(l.GetDegreesLatitude(), l.GetDegreesLongitude())}
		if a := strings.TrimSpace(l.GetAddress()); a != "" {
			c.Lines = append(c.Lines, a)
		} else {
			c.Lines = append(c.Lines, fmt.Sprintf("%.5f, %.5f", l.GetDegreesLatitude(), l.GetDegreesLongitude()))
		}
		return c
	case pm.GetLiveLocationMessage() != nil:
		l := pm.GetLiveLocationMessage()
		c := &ipc.Card{Type: "location", Title: "Localização em tempo real",
			URL: mapURL(l.GetDegreesLatitude(), l.GetDegreesLongitude())}
		c.Lines = append(c.Lines, "última posição enviada · acompanhe no celular")
		if txt := strings.TrimSpace(l.GetCaption()); txt != "" {
			c.Lines = append(c.Lines, txt)
		}
		return c
	case pm.GetEventMessage() != nil:
		c := eventCard(pm.GetEventMessage())
		d.fillRSVPs(c, m)
		return c
	case pollOf(pm) != nil:
		return d.pollCard(m, pollOf(pm))
	case pm.GetExtendedTextMessage().GetTitle() != "" || len(pm.GetExtendedTextMessage().GetJPEGThumbnail()) > 0:
		e := pm.GetExtendedTextMessage()
		u := strings.TrimSpace(e.GetMatchedText())
		if u == "" {
			return nil
		}
		if !strings.Contains(u, "://") {
			u = "https://" + u
		}
		c := &ipc.Card{Type: "link", Title: strings.TrimSpace(e.GetTitle()), URL: u}
		if desc := oneLineText(e.GetDescription()); desc != "" {
			c.Lines = append(c.Lines, desc)
		}
		c.Lines = append(c.Lines, hostOf(u))
		if jpg := e.GetJPEGThumbnail(); len(jpg) > 0 {
			if base, err := mediaBase(m.ID); err == nil {
				c.Thumb = pngThumb(base+".link.png", jpg, false)
			}
		}
		return c
	}
	return nil
}

func contactCard(x *waE2E.ContactMessage) *ipc.Card {
	c := &ipc.Card{Type: "contact", Title: strings.TrimSpace(x.GetDisplayName())}
	name, phone, waid := parseVCard(x.GetVcard())
	if c.Title == "" {
		c.Title = name
	}
	if phone != "" {
		c.Lines = append(c.Lines, phone)
	}
	if waid != "" {
		c.Chat = waid + "@s.whatsapp.net"
		c.Lines = append(c.Lines, "ctrl+o abre a conversa")
	} else if phone != "" {
		c.Lines = append(c.Lines, "sem WhatsApp")
	}
	return c
}

// parseVCard tira do vCard o nome, o primeiro telefone e, se houver, o
// número de WhatsApp dele (parâmetro waid).
func parseVCard(v string) (name, phone, waid string) {
	for _, l := range strings.Split(strings.ReplaceAll(v, "\r\n", "\n"), "\n") {
		key, val, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		up := strings.ToUpper(key)
		switch {
		case up == "FN" || strings.HasPrefix(up, "FN;"):
			if name == "" {
				name = strings.TrimSpace(val)
			}
		case strings.Contains(up, "TEL"):
			if phone != "" {
				continue
			}
			phone = strings.TrimSpace(val)
			for _, p := range strings.Split(key, ";") {
				if k, v, ok := strings.Cut(p, "="); ok && strings.EqualFold(k, "waid") {
					waid = strings.TrimSpace(v)
				}
			}
		}
	}
	return
}

var weekdays = [...]string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

func eventCard(e *waE2E.EventMessage) *ipc.Card {
	c := &ipc.Card{Type: "event", Title: strings.TrimSpace(e.GetName()), URL: e.GetJoinLink()}
	if e.GetIsCanceled() {
		c.Lines = append(c.Lines, "cancelado")
	}
	if ts := e.GetStartTime(); ts > 0 {
		t := time.Unix(ts, 0)
		when := fmt.Sprintf("%s, %s às %s", weekdays[t.Weekday()], t.Format("02/01"), t.Format("15:04"))
		if end := e.GetEndTime(); end > 0 {
			when += " até " + time.Unix(end, 0).Format("15:04")
		}
		c.Lines = append(c.Lines, "🕒 "+when)
	}
	if loc := strings.TrimSpace(e.GetLocation().GetName()); loc != "" {
		c.Lines = append(c.Lines, "📍 "+loc)
	}
	if desc := strings.TrimSpace(e.GetDescription()); desc != "" {
		c.Lines = append(c.Lines, desc)
	}
	if c.URL != "" {
		c.Lines = append(c.Lines, "🔗 "+c.URL)
	}
	return c
}

func mapURL(lat, lon float64) string {
	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%.6f&mlon=%.6f#map=17/%.6f/%.6f", lat, lon, lat, lon)
}

func hostOf(u string) string {
	h := u
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	return strings.TrimPrefix(h, "www.")
}

func oneLineText(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
