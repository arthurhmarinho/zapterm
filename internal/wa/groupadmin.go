package wa

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Coisas de admin de grupo, apagar conversa e links de convite.

var (
	adminMu    sync.Mutex
	adminCache = map[string]struct {
		at    time.Time
		admin bool
	}{}
)

// amAdmin diz se você é admin do grupo (guarda por 10 minutos).
func (d *Daemon) amAdmin(ctx context.Context, chat string) (bool, error) {
	adminMu.Lock()
	if c, ok := adminCache[chat]; ok && time.Since(c.at) < 10*time.Minute {
		adminMu.Unlock()
		return c.admin, nil
	}
	adminMu.Unlock()
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.GroupServer {
		return false, nil
	}
	info, err := d.cli.GetGroupInfo(ctx, jid)
	if err != nil {
		return false, err
	}
	me, myLID := d.cli.Store.ID, d.cli.Store.GetLID()
	admin := false
	for _, p := range info.Participants {
		if me != nil && (p.JID.User == me.User || p.PhoneNumber.User == me.User || p.JID.User == myLID.User) {
			admin = p.IsAdmin || p.IsSuperAdmin
		}
	}
	adminMu.Lock()
	adminCache[chat] = struct {
		at    time.Time
		admin bool
	}{time.Now(), admin}
	adminMu.Unlock()
	return admin, nil
}

// adminRevoke apaga para todos a mensagem de outra pessoa (só admin). Nos
// grupos que endereçam por LID, o autor vai pelo LID.
func (d *Daemon) adminRevoke(ctx context.Context, chat string, m ipc.Message) error {
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.GroupServer {
		return errors.New("só dá para apagar para todos as suas mensagens")
	}
	if ok, err := d.amAdmin(ctx, chat); err != nil {
		return err
	} else if !ok {
		return errors.New("só admin apaga mensagem dos outros")
	}
	sender, err := types.ParseJID(m.Sender)
	if err != nil {
		return err
	}
	if info, err := d.cli.GetGroupInfo(ctx, jid); err == nil && info.AddressingMode == types.AddressingModeLID &&
		sender.Server == types.DefaultUserServer {
		if lid, err := d.cli.Store.LIDs.GetLIDForPN(ctx, sender); err == nil && !lid.IsEmpty() {
			sender = lid
		}
	}
	if _, err := d.cli.SendMessage(ctx, jid, d.cli.BuildRevoke(jid, sender, m.ID)); err != nil {
		return err
	}
	d.applyUpdate(chat, m.ID, "revoked", "", false, true)
	return nil
}

// deleteChat apaga a conversa (no celular também) e tudo dela aqui.
func (d *Daemon) deleteChat(ctx context.Context, chat string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	target := jid
	if jid.Server == types.DefaultUserServer {
		if lid, err := d.cli.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
			target = lid
		}
	}
	ts, key := d.lastKey(chat, jid, target)
	if err := d.cli.SendAppState(ctx, appstate.BuildDeleteChat(target, ts, key, true)); err != nil {
		return fmt.Errorf("avisando o WhatsApp: %w", err)
	}
	if err := d.st.DeleteChat(chat); err != nil {
		return err
	}
	d.srv.Broadcast(ipc.EvReload, nil)
	d.scheduleUnreadPush()
	return nil
}

// inviteCode tira o código de "https://chat.whatsapp.com/CODIGO".
func inviteCode(link string) (string, error) {
	link = strings.TrimSpace(link)
	if !strings.Contains(link, "://") {
		link = "https://" + link
	}
	u, err := url.Parse(link)
	if err != nil || !strings.HasSuffix(u.Host, "chat.whatsapp.com") {
		return "", fmt.Errorf("não é um link de convite: %q", link)
	}
	code := strings.Trim(u.Path, "/")
	code = strings.TrimPrefix(code, "invite/")
	if code == "" || strings.Contains(code, "/") {
		return "", fmt.Errorf("link de convite sem código: %q", link)
	}
	return code, nil
}

func (d *Daemon) groupByLink(ctx context.Context, link string) (*ipc.GroupPreview, error) {
	code, err := inviteCode(link)
	if err != nil {
		return nil, err
	}
	info, err := d.cli.GetGroupInfoFromLink(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("o WhatsApp não mostrou o grupo (link vencido ou redefinido?): %w", err)
	}
	p := &ipc.GroupPreview{Name: info.Name, Topic: info.Topic, Members: len(info.Participants), JID: info.JID.String()}
	if info.ParticipantCount > p.Members {
		p.Members = info.ParticipantCount
	}
	_, err = d.st.Chat(p.JID)
	p.Joined = err == nil
	return p, nil
}

func (d *Daemon) joinLink(ctx context.Context, link string) (string, error) {
	code, err := inviteCode(link)
	if err != nil {
		return "", err
	}
	jid, err := d.cli.JoinGroupWithLink(ctx, code)
	if err != nil {
		return "", err
	}
	d.ensureName(jid)
	return jid.String(), nil
}
