package wa

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Criar grupo e administrar: nome, descrição, foto, permissões e pedidos
// para entrar.

// createGroup cria o grupo com você e os participantes (JIDs) e devolve o
// JID dele.
func (d *Daemon) createGroup(ctx context.Context, name string, members []string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return "", errors.New("o nome do grupo precisa ter de 1 a 100 letras")
	}
	if len(members) == 0 {
		return "", errors.New("escolha pelo menos uma pessoa")
	}
	var jids []types.JID
	for _, m := range members {
		j, err := types.ParseJID(m)
		if err != nil {
			return "", err
		}
		jids = append(jids, j)
	}
	info, err := d.cli.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: jids})
	if err != nil {
		return "", err
	}
	jid := info.JID.String()
	if err := d.st.UpsertChat(jid, info.Name, true, time.Now().UnixMilli(), 0); err != nil {
		d.log.Warn("guardar grupo novo", "err", err)
	}
	d.srv.Broadcast(ipc.EvReload, nil)
	return jid, nil
}

// groupSetting muda uma configuração do grupo (só admin, ou qualquer um se
// o grupo não está trancado, para nome, descrição e foto). key: name,
// topic, photo (value = caminho; "" tira), announce, locked, approval
// ("1"/"0"), addmode ("admin"/"all"), approve, reject (value = JID).
func (d *Daemon) groupSetting(ctx context.Context, chat, key, value string) error {
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.GroupServer {
		return errors.New("não é um grupo")
	}
	on := value == "1"
	switch key {
	case "name":
		value = strings.TrimSpace(value)
		if value == "" {
			return errors.New("o nome não pode ficar vazio")
		}
		if err := d.cli.SetGroupName(ctx, jid, value); err != nil {
			return err
		}
		return d.st.SetChatName(chat, value)
	case "topic":
		return d.cli.SetGroupTopic(ctx, jid, "", "", strings.TrimSpace(value))
	case "photo":
		var jpg []byte
		if value != "" {
			if jpg, err = squareJPEG(value); err != nil {
				return err
			}
		}
		_, err := d.cli.SetGroupPhoto(ctx, jid, jpg)
		if err == nil {
			d.forgetAvatar(chat)
		}
		return err
	case "announce":
		return d.cli.SetGroupAnnounce(ctx, jid, on)
	case "locked":
		return d.cli.SetGroupLocked(ctx, jid, on)
	case "approval":
		return d.cli.SetGroupJoinApprovalMode(ctx, jid, on)
	case "addmode":
		mode := types.GroupMemberAddModeAllMember
		if value == "admin" {
			mode = types.GroupMemberAddModeAdmin
		}
		return d.cli.SetGroupMemberAddMode(ctx, jid, mode)
	case "approve", "reject":
		who, err := types.ParseJID(value)
		if err != nil {
			return err
		}
		change := whatsmeow.ParticipantChangeApprove
		if key == "reject" {
			change = whatsmeow.ParticipantChangeReject
		}
		_, err = d.cli.UpdateGroupRequestParticipants(ctx, jid, []types.JID{who}, change)
		if err == nil {
			membersMu.Lock()
			delete(members, chat)
			membersMu.Unlock()
		}
		return err
	}
	return fmt.Errorf("configuração desconhecida: %s", key)
}

// groupRequests: quem pediu para entrar no grupo (só admin).
func (d *Daemon) groupRequests(ctx context.Context, chat string) ([]ipc.ProfileMember, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, err
	}
	reqs, err := d.cli.GetGroupRequestParticipants(ctx, jid)
	if err != nil {
		return nil, err
	}
	out := []ipc.ProfileMember{}
	for _, r := range reqs {
		named := d.normalize(ctx, r.JID)
		out = append(out, ipc.ProfileMember{Name: d.contactName(ctx, named, ""), JID: named.ToNonAD().String(), Raw: r.JID.ToNonAD().String()})
	}
	return out, nil
}

// squareJPEG lê a imagem, corta o quadrado do centro e reduz a 640 px,
// como o WhatsApp quer a foto de grupo.
func squareJPEG(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("imagem: %w", err)
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	sq := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			sq.Set(x, y, img.At(x0+x, y0+y))
		}
	}
	jpg, _, _ := scaleJPEG(sq, 640, 85)
	return jpg, nil
}

// forgetAvatar faz a foto do chat ser buscada de novo na próxima vez.
func (d *Daemon) forgetAvatar(chat string) {
	if p, err := avatarMetaPath(chat); err == nil {
		os.Remove(p)
	}
}
