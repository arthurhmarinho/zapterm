package wa

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Resumo de não lidas para a barra (plugin do Noctalia em noctalia/): conta
// as conversas com mensagem não lida que não estão arquivadas nem
// silenciadas (grupo silenciado com 999+ só faria barulho, como no app).
// O daemon empurra o resumo ao plugin sempre que muda.

const noctaliaWidget = "arthurhmarinho/zapterm:unread"

func (d *Daemon) unreadSummary(ctx context.Context) ipc.Unread {
	u := ipc.Unread{Top: []ipc.UnreadChat{}}
	chats, err := d.st.Chats()
	if err != nil {
		return u
	}
	for i := range chats {
		c := &chats[i]
		if c.Unread == 0 {
			continue
		}
		d.decorate(ctx, c)
		if c.Archived || c.Muted || c.Kind == "channel" {
			continue
		}
		u.Chats++
		u.Messages += c.Unread
		u.Top = append(u.Top, ipc.UnreadChat{Name: c.Name, Unread: c.Unread})
	}
	slices.SortStableFunc(u.Top, func(a, b ipc.UnreadChat) int { return b.Unread - a.Unread })
	if len(u.Top) > 8 {
		u.Top = u.Top[:8]
	}
	return u
}

var (
	unreadMu    sync.Mutex
	unreadLast  string
	unreadTimer *time.Timer
)

// scheduleUnreadPush junta as mudanças de 1 s e empurra o resumo, se
// mudou, para o widget do Noctalia (se o noctalia estiver instalado).
func (d *Daemon) scheduleUnreadPush() {
	unreadMu.Lock()
	defer unreadMu.Unlock()
	if unreadTimer != nil {
		return
	}
	unreadTimer = time.AfterFunc(time.Second, func() {
		unreadMu.Lock()
		unreadTimer = nil
		unreadMu.Unlock()
		d.pushUnread(false)
	})
}

func (d *Daemon) pushUnread(force bool) {
	if _, err := exec.LookPath("noctalia"); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, _ := json.Marshal(d.unreadSummary(ctx))
	unreadMu.Lock()
	same := string(b) == unreadLast
	unreadLast = string(b)
	unreadMu.Unlock()
	if same && !force {
		return
	}
	// Sem o plugin instalado o comando só falha; não é erro do zapterm.
	_ = exec.CommandContext(ctx, "noctalia", "msg", "plugin", noctaliaWidget, "all", "set", string(b)).Run()
}

// showUI: o clique no widget. Com TUI aberta, ela vem para frente; sem
// nenhuma, abre um kitty com o zapterm.
func (d *Daemon) showUI() error {
	if d.srv.Clients() > 0 {
		d.srv.Broadcast(ipc.EvOpen, ipc.Open{})
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command("kitty", "--class", "zapterm", exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
