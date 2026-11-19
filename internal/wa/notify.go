package wa

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// notifier mostra notificações pelo notify-send (o servidor no Sway é o do
// Noctalia). Cada chat tem uma única notificação, substituída a cada
// mensagem nova, com a contagem desde a última vez que você olhou.
type notifier struct {
	mu      sync.Mutex
	ids     map[string]string // chat -> id da notificação no servidor
	pending map[string]int    // chat -> mensagens desde que você olhou
	viewing map[uint64]string // conexão da TUI -> chat aberto com foco
}

func newNotifier() *notifier {
	return &notifier{ids: map[string]string{}, pending: map[string]int{}, viewing: map[uint64]string{}}
}

// setViewing registra o chat que uma TUI está mostrando com o terminal em
// foco ("" = nenhum). Olhar o chat zera a contagem dele.
func (n *notifier) setViewing(conn uint64, chat string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if chat == "" {
		delete(n.viewing, conn)
		return
	}
	n.viewing[conn] = chat
	delete(n.pending, chat)
}

// anyViewing: alguma TUI está aberta e em foco.
func (n *notifier) anyViewing() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.viewing) > 0
}

func (n *notifier) isViewing(chat string) bool {
	for _, c := range n.viewing {
		if c == chat {
			return true
		}
	}
	return false
}

func (d *Daemon) notify(m ipc.Message, chat types.JID, chatName string) {
	if chat.Server == types.NewsletterServer {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Silenciado ou arquivado no celular não notifica. Arquivado conta como
	// silenciado, como no app com "manter conversas arquivadas".
	if s := d.chatSettings(ctx, chat); s.MutedUntil.After(time.Now()) || s.Archived {
		return
	}

	n := d.notif
	n.mu.Lock()
	if n.isViewing(m.Chat) {
		n.mu.Unlock()
		return
	}
	n.pending[m.Chat]++
	count := n.pending[m.Chat]
	prevID := n.ids[m.Chat]
	n.mu.Unlock()

	title := chatName
	body := m.Text
	if chat.Server == types.GroupServer {
		body = m.SenderName + ": " + body
	}
	if count > 1 {
		title = fmt.Sprintf("%s · %d mensagens novas", chatName, count)
	}
	if r := []rune(body); len(r) > 200 {
		body = string(r[:199]) + "…"
	}

	args := []string{"-a", "zapterm", "-c", "im.received", "-p", "-h", "string:x-zapterm-chat:" + m.Chat}
	// Foto do chat (de quem mandou, ou do grupo) como ícone, se houver.
	if av, err := d.avatar(ctx, chat.String()); err == nil && av != nil && av.Preview != "" {
		args = append(args, "-i", av.Preview, "-h", "string:image-path:"+av.Preview)
	}
	if prevID != "" {
		args = append(args, "-r", prevID)
	}
	args = append(args, "--", title, body)
	out, err := exec.CommandContext(ctx, "notify-send", args...).Output()
	if err != nil {
		d.log.Warn("notify-send", "err", err)
		return
	}
	if id := strings.TrimSpace(string(out)); id != "" {
		n.mu.Lock()
		n.ids[m.Chat] = id
		n.mu.Unlock()
	}
}
