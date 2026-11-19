package wa

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Menções: no protocolo, o texto leva "@<usuário>" (número ou LID) e o
// ContextInfo.MentionedJID diz quem foi marcado; cada app troca o número
// pelo nome na hora de mostrar. Aqui o daemon faz essa troca e embrulha o
// nome em isolantes Unicode (U+2068 … U+2069), invisíveis, que a TUI usa
// para destacar a menção sem precisar adivinhar onde o nome termina.

const mentionOpen, mentionClose = "⁨", "⁩"

var mentionRe = regexp.MustCompile(`@(\d{5,})`)

// fillMentions troca "@5584…" pelo nome nas mensagens.
func (d *Daemon) fillMentions(ctx context.Context, msgs []ipc.Message) {
	cache := map[string]string{}
	for i := range msgs {
		if !strings.Contains(msgs[i].Text, "@") {
			continue
		}
		msgs[i].Text = mentionRe.ReplaceAllStringFunc(msgs[i].Text, func(m string) string {
			user := m[1:]
			name, ok := cache[user]
			if !ok {
				name = d.mentionName(ctx, user)
				cache[user] = name
			}
			if name == "" {
				return m
			}
			return "@" + mentionOpen + name + mentionClose
		})
	}
}

// mentionName resolve o usuário de uma menção, que pode ser número ou LID.
func (d *Daemon) mentionName(ctx context.Context, user string) string {
	if me := d.cli.Store.ID; me != nil && (user == me.User || (d.cli.Store.LID.User != "" && user == d.cli.Store.LID.User)) {
		return "Você"
	}
	pn := types.NewJID(user, types.DefaultUserServer)
	if n := d.knownName(ctx, pn); n != "" {
		return strings.TrimPrefix(n, "~")
	}
	lid := types.NewJID(user, types.HiddenUserServer)
	if p, err := d.cli.Store.LIDs.GetPNForLID(ctx, lid); err == nil && !p.IsEmpty() {
		if n := d.knownName(ctx, p); n != "" {
			return strings.TrimPrefix(n, "~")
		}
		return "+" + p.User
	}
	return ""
}

// mentionsMe diz se a mensagem marca você (pelo número ou pelo LID).
func (d *Daemon) mentionsMe(m *waE2E.Message) bool {
	me := d.cli.Store.ID
	if me == nil {
		return false
	}
	for _, j := range contextInfo(m).GetMentionedJID() {
		jid, err := types.ParseJID(j)
		if err != nil {
			continue
		}
		if jid.User == me.User || (d.cli.Store.LID.User != "" && jid.User == d.cli.Store.LID.User) {
			return true
		}
	}
	return false
}

// ---------- participantes, para o autocompletar ----------

type membersCache struct {
	at   time.Time
	list []ipc.Member
}

var (
	membersMu sync.Mutex
	members   = map[string]membersCache{}
)

// groupMembers lista os participantes de um grupo com nome, para o "@" da
// TUI. Fica em cache por 10 minutos.
func (d *Daemon) groupMembers(ctx context.Context, chat string) ([]ipc.Member, error) {
	membersMu.Lock()
	if c, ok := members[chat]; ok && time.Since(c.at) < 10*time.Minute {
		membersMu.Unlock()
		return c.list, nil
	}
	membersMu.Unlock()

	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server != types.GroupServer {
		return []ipc.Member{}, nil
	}
	info, err := d.cli.GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, err
	}
	me := d.cli.Store.ID
	list := []ipc.Member{}
	for _, p := range info.Participants {
		if me != nil && (p.JID.User == me.User || p.PhoneNumber.User == me.User) {
			continue // não se marca a si mesmo
		}
		named := p.PhoneNumber
		if named.IsEmpty() {
			named = d.normalize(ctx, p.JID)
		}
		name := strings.TrimPrefix(d.knownName(ctx, named), "~")
		if name == "" {
			name = p.DisplayName
		}
		if name == "" && named.Server == types.DefaultUserServer {
			name = "+" + named.User
		}
		if name == "" {
			continue
		}
		// O JID da menção é o que o grupo usa (LID nos grupos novos).
		list = append(list, ipc.Member{JID: p.JID.ToNonAD().String(), Name: name})
	}
	slices.SortFunc(list, func(a, b ipc.Member) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	membersMu.Lock()
	members[chat] = membersCache{at: time.Now(), list: list}
	membersMu.Unlock()
	return list, nil
}
