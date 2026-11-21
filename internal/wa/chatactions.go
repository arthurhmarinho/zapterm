package wa

import (
	"context"
	"fmt"
	"go.mau.fi/whatsmeow/types/events"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
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
	if secs, ok := strings.CutPrefix(action, "ephemeral:"); ok {
		n, err := strconv.Atoi(secs)
		if err != nil {
			return fmt.Errorf("prazo inválido: %q", secs)
		}
		if err := d.cli.SetDisappearingTimer(ctx, jid, time.Duration(n)*time.Second, time.Now()); err != nil {
			return err
		}
		d.setChatEphemeral(chat, n)
		return nil
	}
	if jid.Server == types.NewsletterServer {
		// Canal: silenciar e deixar de seguir são chamadas próprias.
		switch action {
		case "mute8h", "mute1w", "muteforever", "unmute":
			if err := d.cli.NewsletterToggleMute(ctx, jid, action != "unmute"); err != nil {
				return err
			}
			d.setChannelMuted(chat, action != "unmute")
			d.pushChat(chat)
			return nil
		case "unfollow":
			if err := d.cli.UnfollowNewsletter(ctx, jid); err != nil {
				return err
			}
			if err := d.st.DeleteChat(chat); err != nil {
				return err
			}
			d.srv.Broadcast(ipc.EvReload, nil)
			return nil
		}
	}
	switch action {
	case "read":
		return d.markRead(ctx, chat)
	case "unread":
		ts, key := d.lastKey(chat, jid, target)
		if err := d.cli.SendAppState(ctx, appstate.BuildMarkChatAsRead(target, false, ts, key)); err != nil {
			return err
		}
		if err := d.st.SetUnread(chat, 1); err != nil {
			return err
		}
		d.pushChat(chat)
		d.scheduleUnreadPush()
		return nil
	case "block", "unblock":
		act := events.BlocklistChangeActionBlock
		if action == "unblock" {
			act = events.BlocklistChangeActionUnblock
		}
		list, err := d.cli.UpdateBlocklist(ctx, jid, act, d.blockHash())
		if err != nil {
			return err
		}
		d.setBlocklist(list)
		d.pushChat(chat)
		return nil
	case "delete":
		return d.deleteChat(ctx, chat)
	case "leave":
		if err := d.cli.LeaveGroup(ctx, jid); err != nil {
			return err
		}
		d.pushChat(chat)
		return nil
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
		ts, key := d.lastKey(chat, jid, target)
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

// checkNumber confere se um telefone tem WhatsApp e devolve o chat dele
// (existente ou novo), para abrir uma conversa nova pela busca.
func (d *Daemon) checkNumber(ctx context.Context, phone string) (*ipc.Chat, error) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if len(digits) < 10 {
		return nil, fmt.Errorf("número curto demais: use DDI e DDD (ex.: +55 84 9…)")
	}
	res, err := d.cli.IsOnWhatsApp(ctx, []string{"+" + digits})
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || !res[0].IsIn {
		return nil, fmt.Errorf("+%s não tem WhatsApp", digits)
	}
	jid := res[0].JID.ToNonAD()
	c, err := d.st.Chat(jid.String())
	if err != nil {
		c = ipc.Chat{JID: jid.String(), LastStatus: -1}
	}
	d.decorate(ctx, &c)
	return &c, nil
}

// lastKey: horário e chave da última mensagem do chat (arquivar e marcar
// como não lida levam isso).
func (d *Daemon) lastKey(chat string, jid, target types.JID) (time.Time, *waCommon.MessageKey) {
	last, err := d.st.Messages(chat, 1, 0)
	if err != nil || len(last) != 1 {
		return time.Time{}, nil
	}
	key := &waCommon.MessageKey{RemoteJID: proto.String(target.String()), FromMe: proto.Bool(last[0].FromMe),
		ID: proto.String(last[0].ID)}
	if jid.Server == types.GroupServer && !last[0].FromMe {
		key.Participant = proto.String(last[0].Sender)
	}
	return time.UnixMilli(last[0].TS), key
}

// ---------- bloqueados ----------

var (
	blockMu   sync.Mutex
	blocked   = map[string]bool{} // usuário (número ou LID, sem servidor)
	blockHash string
)

func (d *Daemon) loadBlocklist() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := d.cli.GetBlocklist(ctx, "")
	if err != nil {
		d.log.Warn("lista de bloqueados", "err", err)
		return
	}
	d.setBlocklist(list)
}

func (d *Daemon) setBlocklist(list *types.Blocklist) {
	if list == nil {
		return
	}
	blockMu.Lock()
	blocked = map[string]bool{}
	for _, it := range list.Items {
		// Cada bloqueado vem pelo LID e, quando se sabe, pelo número.
		for _, j := range []types.JID{it.LID, it.PN} {
			if j.User != "" {
				blocked[j.User] = true
			}
		}
	}
	blockHash = list.DHash
	blockMu.Unlock()
}

func (d *Daemon) blockHash() string {
	blockMu.Lock()
	defer blockMu.Unlock()
	return blockHash
}

// isBlocked olha pelo número e pelo LID do contato.
func (d *Daemon) isBlocked(chat string) bool {
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.DefaultUserServer {
		return false
	}
	blockMu.Lock()
	b := blocked[jid.User]
	blockMu.Unlock()
	if b {
		return true
	}
	if lid, err := d.cli.Store.LIDs.GetLIDForPN(context.Background(), jid); err == nil && !lid.IsEmpty() {
		blockMu.Lock()
		b = blocked[lid.User]
		blockMu.Unlock()
	}
	return b
}

// onBlocklist: bloqueou ou desbloqueou alguém no celular.
func (d *Daemon) onBlocklist(ctx context.Context, e *events.Blocklist) {
	go d.loadBlocklist() // mais simples e seguro que aplicar as mudanças uma a uma
	for _, c := range e.Changes {
		d.pushChat(d.normalize(ctx, c.JID).String())
	}
}

// Canais silenciados aqui (o estado do canal vem do servidor; guardamos o
// que mudamos para a lista refletir na hora).
var (
	chMuteMu sync.Mutex
	chMuted  = map[string]bool{}
)

func (d *Daemon) setChannelMuted(chat string, muted bool) {
	chMuteMu.Lock()
	chMuted[chat] = muted
	chMuteMu.Unlock()
}

func channelMuted(chat string) (muted, known bool) {
	chMuteMu.Lock()
	defer chMuteMu.Unlock()
	muted, known = chMuted[chat]
	return
}
