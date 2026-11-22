package wa

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// notifier decide quando notificar; quem mostra é o desktop (D-Bus). Cada
// chat tem uma única notificação, substituída a cada mensagem nova, com a
// contagem desde a última vez que você olhou.
type notifier struct {
	mu      sync.Mutex
	ids     map[string]uint32 // chat -> id da notificação no servidor
	pending map[string]int    // chat -> mensagens desde que você olhou
	viewing map[uint64]string // conexão da TUI -> chat aberto com foco
}

func newNotifier() *notifier {
	return &notifier{ids: map[string]uint32{}, pending: map[string]int{}, viewing: map[uint64]string{}}
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

// notify devolve o id da notificação mostrada (0 = nenhuma). extra são
// ações a mais (pares chave, rótulo).
func (d *Daemon) notify(m ipc.Message, chat types.JID, chatName string, extra ...string) uint32 {
	if chat.Server == types.NewsletterServer {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Silenciado ou arquivado no celular não notifica. Arquivado conta como
	// silenciado, como no app com "manter conversas arquivadas".
	// Silenciado não notifica, a não ser que te marquem (como no app).
	if !d.cfg.Privacy.Notifications || d.st.IsLocked(m.Chat) {
		return 0 // trancada no celular: nem o nome aparece
	}
	mentionOK := m.MentionsMe && d.cfg.Privacy.NotifyMentions
	if s := d.chatSettings(ctx, chat); s.Archived || (s.MutedUntil.After(time.Now()) && !mentionOK) {
		return 0
	}

	n := d.notif
	n.mu.Lock()
	if n.isViewing(m.Chat) {
		n.mu.Unlock()
		return 0
	}
	n.pending[m.Chat]++
	if snd := d.cfg.Interface.NotifySound; snd != "" {
		go playSound(snd)
	}
	count := n.pending[m.Chat]
	prevID := n.ids[m.Chat]
	n.mu.Unlock()

	title := chatName
	body := m.Text
	if chat.Server == types.GroupServer {
		body = m.SenderName + ": " + body
	}
	if !d.cfg.Privacy.NotifyPreview {
		body = "Nova mensagem" // [privacy] notify_preview = false
	}
	if count > 1 {
		title = fmt.Sprintf("%s · %d mensagens novas", chatName, count)
	}
	body = strings.NewReplacer(mentionOpen, "", mentionClose, "").Replace(body)
	if r := []rune(body); len(r) > 200 {
		body = string(r[:199]) + "…"
	}

	// Foto do chat (de quem mandou, ou do grupo) como ícone, se houver.
	icon := ""
	if av, err := d.avatar(ctx, chat.String()); err == nil && av != nil && av.Preview != "" {
		icon = av.Preview
	}
	if d.desk == nil {
		d.log.Warn("sem servidor de notificações no D-Bus")
		return 0
	}
	id, err := d.desk.show(m.Chat, title, body, icon, prevID, extra...)
	if err != nil {
		d.log.Warn("notificação", "err", err)
		return 0
	}
	n.mu.Lock()
	n.ids[m.Chat] = id
	n.mu.Unlock()
	return id
}

// openFromNotification: clicou na notificação. Com uma TUI aberta, ela
// abre o chat e põe a janela em foco; sem nenhuma, abre um kitty novo já
// nesse chat.
func (d *Daemon) openFromNotification(chat string) {
	if d.srv.Clients() > 0 {
		d.srv.Broadcast(ipc.EvOpen, ipc.Open{Chat: chat})
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command("kitty", "--class", "zapterm", exe)
	cmd.Env = append(os.Environ(), "ZAPTERM_OPEN="+chat)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		d.log.Warn("abrindo o zapterm pela notificação", "err", err)
	}
}

// replyFromNotification envia o texto digitado na própria notificação e
// marca o chat como lido (você respondeu, então viu).
func (d *Daemon) replyFromNotification(chat, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := d.send(ctx, chat, text, "", nil); err != nil {
		d.log.Warn("resposta pela notificação", "err", err)
		return
	}
	d.notif.mu.Lock()
	delete(d.notif.pending, chat)
	d.notif.mu.Unlock()
	_ = d.markRead(ctx, chat)
}

// playSound toca o som da notificação ([interface] notify_sound).
func playSound(path string) {
	for _, player := range []string{"pw-play", "paplay"} {
		if _, err := exec.LookPath(player); err == nil {
			_ = exec.Command(player, path).Run()
			return
		}
	}
}
