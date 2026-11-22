package wa

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Comunidades: ao conectar, os grupos de que você participa dizem de qual
// comunidade são; a lista mostra "Comunidade › Grupo" e junta os grupos
// da mesma comunidade.

var communitiesOnce sync.Once

func (d *Daemon) syncCommunities() {
	communitiesOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			groups, err := d.cli.GetJoinedGroups(ctx)
			if err != nil {
				d.log.Warn("comunidades", "err", err)
				communitiesOnce = sync.Once{} // tenta de novo na próxima conexão
				return
			}
			n := 0
			for _, g := range groups {
				jid := g.JID.String()
				switch {
				case g.IsParent:
					d.st.PutCommunity(jid, g.Name)
				case !g.LinkedParentJID.IsEmpty():
					d.st.SetGroupParent(jid, g.LinkedParentJID.String(), g.IsDefaultSubGroup)
					n++
				default:
					d.st.SetGroupParent(jid, "", false)
				}
			}
			d.log.Info("comunidades sincronizadas", "grupos", n)
			if n > 0 {
				d.srv.Broadcast(ipc.EvReload, nil)
			}
		}()
	})
}

// fillCommunity põe no grupo a comunidade dele.
func (d *Daemon) fillCommunity(c *ipc.Chat) {
	if c.Kind != "group" {
		return
	}
	jid, name, def := d.st.GroupCommunity(c.JID)
	if jid == "" {
		return
	}
	if name == "" {
		name = "Comunidade"
	}
	c.CommunityJID, c.Community, c.CommunityAnnounce = jid, name, def
}

// subGroups: os grupos da comunidade do grupo chat, dizendo de quais você
// participa.
func (d *Daemon) subGroups(ctx context.Context, chat string) ([]ipc.SubGroup, error) {
	parent, _, _ := d.st.GroupCommunity(chat)
	if parent == "" {
		return nil, errors.New("este grupo não é de uma comunidade")
	}
	pj, err := types.ParseJID(parent)
	if err != nil {
		return nil, err
	}
	targets, err := d.cli.GetSubGroups(ctx, pj)
	if err != nil {
		return nil, err
	}
	out := []ipc.SubGroup{}
	for _, t := range targets {
		jid := t.JID.String()
		p, _, _ := d.st.GroupCommunity(jid)
		out = append(out, ipc.SubGroup{JID: jid, Name: t.Name, Announce: t.IsDefaultSubGroup, Joined: p == parent})
	}
	return out, nil
}
