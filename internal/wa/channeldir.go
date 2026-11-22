package wa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Canais: diretório (recomendados), busca, seguir e criar. O diretório só
// existe pela API interna (consulta "mex" sem formato documentado), então a
// resposta é lida de forma tolerante: qualquer objeto com id de canal e
// thread_metadata vira uma entrada.

const queryChannelDirectory = "6190824427689257" // mesma do whatsmeow (queryNewslettersDirectory)

// ddiCountry: país do diretório pelo DDI do seu número (padrão: BR).
var ddiCountry = map[string]string{"55": "BR", "351": "PT", "1": "US", "44": "GB", "34": "ES", "54": "AR", "52": "MX"}

func (d *Daemon) myCountry() string {
	if id := d.cli.Store.ID; id != nil {
		for _, n := range []int{3, 2, 1} {
			if len(id.User) > n {
				if c, ok := ddiCountry[id.User[:n]]; ok {
					return c
				}
			}
		}
	}
	return "BR"
}

func channelOf(md *types.NewsletterMetadata) ipc.Channel {
	return ipc.Channel{
		JID: md.ID.String(), Name: md.ThreadMeta.Name.Text, Description: md.ThreadMeta.Description.Text,
		Subscribers: md.ThreadMeta.SubscriberCount, Verified: md.ThreadMeta.VerificationState == types.NewsletterVerificationStateVerified,
	}
}

// findChannels procura, em qualquer profundidade, objetos que parecem um
// canal (id @newsletter e thread_metadata).
func findChannels(v any, out *[]types.NewsletterMetadata) {
	switch x := v.(type) {
	case map[string]any:
		if id, ok := x["id"].(string); ok && strings.HasSuffix(id, "@newsletter") && x["thread_metadata"] != nil {
			b, _ := json.Marshal(x)
			var md types.NewsletterMetadata
			if json.Unmarshal(b, &md) == nil {
				*out = append(*out, md)
				return
			}
		}
		for _, y := range x {
			findChannels(y, out)
		}
	case []any:
		for _, y := range x {
			findChannels(y, out)
		}
	}
}

// channelDirectory: canais recomendados; q filtra pelo nome (sem acento).
// Um link whatsapp.com/channel/… em q mostra aquele canal.
func (d *Daemon) channelDirectory(ctx context.Context, q string) ([]ipc.Channel, error) {
	q = strings.TrimSpace(q)
	if strings.Contains(q, "whatsapp.com/channel/") {
		md, err := d.cli.GetNewsletterInfoWithInvite(ctx, q)
		if err != nil {
			return nil, err
		}
		return []ipc.Channel{channelOf(md)}, nil
	}
	data, err := d.cli.DangerousInternals().SendMexIQ(ctx, queryChannelDirectory, map[string]any{
		"input": map[string]any{"view": "RECOMMENDED", "limit": 50,
			"filters": map[string]any{"country_codes": []string{d.myCountry()}}},
	})
	if err != nil && data == nil {
		return nil, fmt.Errorf("o diretório de canais não respondeu (API interna do WhatsApp): %w", err)
	}
	var tree any
	if json.Unmarshal(data, &tree) != nil {
		return nil, errors.New("o diretório de canais mudou de formato")
	}
	var mds []types.NewsletterMetadata
	findChannels(tree, &mds)
	fq := fold(q)
	out := []ipc.Channel{}
	for i := range mds {
		c := channelOf(&mds[i])
		if ch, err := d.st.Chat(c.JID); err == nil && ch.LastTS > 0 {
			c.Following = true // já está na sua lista
		}
		if fq == "" || strings.Contains(fold(c.Name), fq) || strings.Contains(fold(c.Description), fq) {
			out = append(out, c)
		}
	}
	return out, nil
}

// followChannel segue o canal e o põe na lista.
func (d *Daemon) followChannel(ctx context.Context, chat, name string) error {
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.NewsletterServer {
		return errors.New("não é um canal")
	}
	if err := d.cli.FollowNewsletter(ctx, jid); err != nil {
		return err
	}
	return d.addChannelChat(jid.String(), name)
}

// createChannel cria um canal seu e devolve o JID.
func (d *Daemon) createChannel(ctx context.Context, name, desc string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("o canal precisa de um nome")
	}
	md, err := d.cli.CreateNewsletter(ctx, whatsmeow.CreateNewsletterParams{Name: name, Description: strings.TrimSpace(desc)})
	if err != nil {
		return "", fmt.Errorf("criar canal (o celular pode pedir para aceitar os termos de canais antes): %w", err)
	}
	if md == nil {
		return "", errors.New("o WhatsApp não devolveu o canal criado")
	}
	return md.ID.String(), d.addChannelChat(md.ID.String(), name)
}

func (d *Daemon) addChannelChat(jid, name string) error {
	if err := d.st.UpsertChat(jid, name, false, time.Now().UnixMilli(), 0); err != nil {
		return err
	}
	d.srv.Broadcast(ipc.EvReload, nil)
	return nil
}
