package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
		if media, err := d.st.MediaMessages(chat, 30); err == nil {
			d.fillSenders(ctx, media)
			d.fillThumbs(media)
			p.Media = media
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
			admins := map[string]bool{}
			for _, pt := range info.Participants {
				if pt.IsAdmin || pt.IsSuperAdmin {
					admins[pt.JID.ToNonAD().String()] = true
				}
			}
			if list, err := d.groupMembers(ctx, chat); err == nil {
				for _, mb := range list {
					p.Members = append(p.Members, ipc.ProfileMember{Name: mb.Name, Admin: admins[mb.JID]})
				}
			}
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
