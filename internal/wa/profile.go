package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// profile monta a tela de perfil de um chat: foto grande, nome, número e
// recado (contato) ou descrição e participantes (grupo), e a mídia trocada
// na conversa.
func (d *Daemon) profile(ctx context.Context, chat string) (*ipc.Profile, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return nil, fmt.Errorf("chat inválido: %w", err)
	}
	c, err := d.st.Chat(chat)
	if err != nil {
		c = ipc.Chat{JID: chat}
	}
	d.decorate(ctx, &c)
	p := &ipc.Profile{JID: chat, Name: c.Name, Kind: c.Kind}

	// As consultas ao servidor (dados, foto) e ao banco (mídia) correm em
	// paralelo: em série somavam 4–7 s; assim é o tempo da mais lenta.
	var wg sync.WaitGroup
	var photo *ipc.Media
	wg.Add(2)
	go func() {
		defer wg.Done()
		if ph, err := d.fullPhoto(ctx, jid); err == nil {
			photo = ph
		} else if !errors.Is(err, whatsmeow.ErrProfilePictureNotSet) && !errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) {
			d.log.Debug("foto do perfil", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		if media, err := d.st.MediaMessages(chat, 12); err == nil {
			d.fillSenders(ctx, media)
			d.fillThumbs(media)
			p.Media = media
		}
		if counts, err := d.st.GalleryCounts(chat); err == nil {
			p.Counts = counts
		}
	}()

	switch jid.Server {
	case types.GroupServer:
		info, err := d.cli.GetGroupInfo(ctx, jid)
		if errors.Is(err, whatsmeow.ErrNotInGroup) || errors.Is(err, whatsmeow.ErrGroupNotFound) {
			p.Note = "Você não participa mais deste grupo: o WhatsApp não mostra foto, descrição nem participantes."
		} else if err != nil {
			p.Note = "Não deu para carregar os dados do grupo agora."
			d.log.Warn("perfil do grupo", "err", err)
		}
		if err == nil {
			p.Name = info.Name
			p.About = info.Topic
			p.Created = info.GroupCreated.UnixMilli()
			p.MemberCount = len(info.Participants)
			p.Group = &ipc.GroupSettings{Announce: info.IsAnnounce, Locked: info.IsLocked,
				Approval: info.IsJoinApprovalRequired, AdminAdd: info.MemberAddMode == types.GroupMemberAddModeAdmin}
			names := map[string]string{}
			if list, err := d.groupMembers(ctx, chat); err == nil {
				for _, mb := range list {
					names[mb.JID] = mb.Name
				}
			}
			me := d.cli.Store.ID
			for _, pt := range info.Participants {
				admin := pt.IsAdmin || pt.IsSuperAdmin
				if me != nil && (pt.JID.User == me.User || pt.PhoneNumber.User == me.User) {
					p.IAmAdmin = admin
					continue
				}
				named := pt.PhoneNumber
				if named.IsEmpty() {
					named = d.normalize(ctx, pt.JID)
				}
				raw := pt.JID.ToNonAD().String()
				name, ok := names[raw]
				if !ok {
					continue
				}
				p.Members = append(p.Members, ipc.ProfileMember{Name: name, Admin: admin, JID: named.ToNonAD().String(), Raw: raw})
			}
			// Admins primeiro, como no app; dentro de cada grupo, por nome.
			sort.SliceStable(p.Members, func(a, b int) bool {
				if p.Members[a].Admin != p.Members[b].Admin {
					return p.Members[a].Admin
				}
				return strings.ToLower(p.Members[a].Name) < strings.ToLower(p.Members[b].Name)
			})
		}
	case types.DefaultUserServer:
		p.Phone = "+" + jid.User
		if info, err := d.cli.GetUserInfo(ctx, []types.JID{jid}); err == nil {
			if ui, ok := info[jid]; ok {
				p.About = ui.Status
				if ui.VerifiedName != nil && ui.VerifiedName.Details != nil {
					p.Business = ui.VerifiedName.Details.GetVerifiedName()
				}
			}
		}
	}
	wg.Wait()
	p.Photo = photo
	if p.Media == nil {
		p.Media = []ipc.Message{}
	}
	return p, nil
}

// fullPhoto baixa a foto de perfil em tamanho cheio (a da lista é a
// miniatura) e gera a prévia PNG; fica em cache pelo ID da foto.
func (d *Daemon) fullPhoto(ctx context.Context, jid types.JID) (*ipc.Media, error) {
	if jid.Server == types.NewsletterServer {
		return nil, whatsmeow.ErrProfilePictureNotSet
	}
	info, err := d.cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{})
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, whatsmeow.ErrProfilePictureNotSet
	}
	dir, err := paths.AvatarDir()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(dir, safeName.ReplaceAllString(jid.ToNonAD().String()+"_"+info.ID, "_")+".full")
	md := &ipc.Media{Path: base + ".jpg", Preview: base + ".preview.png", Mime: "image/jpeg"}
	if _, err := os.Stat(md.Preview); err == nil {
		md.Width, md.Height = pngSize(md.Preview)
		return md, nil
	}
	if err := d.downloadAvatarRaw(ctx, info.URL, md.Path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(md.Path)
	if err != nil {
		return nil, err
	}
	if md.Width, md.Height, err = writePreview(data, md.Preview); err != nil {
		return nil, err
	}
	return md, nil
}

// gallery devolve uma página de uma aba da galeria (fotos, vídeos, docs,
// links, áudios) com as contagens de todas as abas.
func (d *Daemon) gallery(ctx context.Context, req ipc.Request) (*ipc.GalleryPage, error) {
	limit := req.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	items, more, err := d.st.Gallery(req.Chat, req.Text, req.Before, limit)
	if err != nil {
		return nil, err
	}
	counts, err := d.st.GalleryCounts(req.Chat)
	if err != nil {
		return nil, err
	}
	d.fillSenders(ctx, items)
	d.fillThumbs(items)
	return &ipc.GalleryPage{Items: items, Counts: counts, More: more}, nil
}

// refetch pede ao celular as mensagens anteriores à primeira mensagem
// depois de before. As que já estão no banco sem o protobuf (histórico do
// pareamento) o recebem de volta: miniatura e download voltam a funcionar.
// O pedido vai só para o seu próprio celular, que precisa estar online.
func (d *Daemon) refetch(ctx context.Context, chat string, before int64) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	anchor, err := d.st.NewerThan(chat, before)
	if err != nil {
		return fmt.Errorf("sem mensagem de referência: %w", err)
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: jid, IsFromMe: anchor.FromMe},
		ID:            anchor.ID,
		Timestamp:     time.UnixMilli(anchor.TS),
	}
	_, err = d.cli.SendPeerMessage(ctx, d.cli.BuildHistorySyncRequest(info, 50))
	return err
}

// groupMember promove, rebaixa ou remove um participante (só admin).
func (d *Daemon) groupMember(ctx context.Context, chat, member, action string) error {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return err
	}
	var who types.JID
	if action == "add" && !strings.Contains(member, "@") {
		c, err := d.checkNumber(ctx, member) // telefone digitado
		if err != nil {
			return err
		}
		member = c.JID
	}
	if who, err = types.ParseJID(member); err != nil {
		return err
	}
	change := map[string]whatsmeow.ParticipantChange{
		"add":     whatsmeow.ParticipantChangeAdd,
		"promote": whatsmeow.ParticipantChangePromote,
		"demote":  whatsmeow.ParticipantChangeDemote,
		"remove":  whatsmeow.ParticipantChangeRemove,
	}[action]
	if change == "" {
		return fmt.Errorf("ação desconhecida: %q", action)
	}
	res, err := d.cli.UpdateGroupParticipants(ctx, jid, []types.JID{who}, change)
	if err != nil {
		return err
	}
	if len(res) == 1 && res[0].Error != 0 {
		switch res[0].Error {
		case 403:
			return errors.New("a privacidade da pessoa não deixa adicionar: mande o link de convite")
		case 409:
			return errors.New("a pessoa já está no grupo")
		}
		return fmt.Errorf("o WhatsApp recusou (código %d)", res[0].Error)
	}
	membersMu.Lock()
	delete(members, chat) // a lista de participantes mudou
	membersMu.Unlock()
	return nil
}

func (d *Daemon) inviteLink(ctx context.Context, chat string, reset bool) (string, error) {
	jid, err := types.ParseJID(chat)
	if err != nil {
		return "", err
	}
	return d.cli.GetGroupInviteLink(ctx, jid, reset)
}
