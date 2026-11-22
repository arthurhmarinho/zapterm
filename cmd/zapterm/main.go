// zapterm: cliente de WhatsApp para o terminal.
//
//	zapterm                  abre a TUI (sobe o daemon se preciso)
//	zapterm login            pareia por QR code e fica rodando como daemon
//	zapterm login --telefone 5584999999999   pareia pelo código de 8 letras
//	zapterm daemon           daemon em segundo plano (exige sessão)
//	zapterm chats            lista os chats (depuração do IPC)
//	zapterm msgs <jid> [n]   últimas n mensagens de um chat
//	zapterm send <jid> <txt> envia texto
//	zapterm tail             mostra os eventos ao vivo
//	zapterm media <jid> <id> baixa a mídia de uma mensagem e mostra o caminho
//	zapterm demo             abre a TUI com conversas fictícias (para prints)
//	zapterm config           cria ~/.config/zapterm/config.toml comentado e mostra o caminho
//	zapterm unread [--json]  não lidas (o widget da barra usa o --json)
//	zapterm open             traz a TUI para frente, ou abre uma (clique no widget)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/config"
	"github.com/arthurhmarinho/zapterm/internal/demo"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
	"github.com/arthurhmarinho/zapterm/internal/tui"
	"github.com/arthurhmarinho/zapterm/internal/wa"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "zapterm:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "login":
		phone := ""
		for i, a := range args {
			if (a == "--telefone" || a == "--phone") && i+1 < len(args) {
				phone = strings.Map(func(r rune) rune {
					if r >= '0' && r <= '9' {
						return r
					}
					return -1
				}, args[i+1])
			}
		}
		return daemon(true, phone)
	case "daemon":
		return daemon(false, "")
	case "chats":
		return chats()
	case "msgs":
		return msgs(args)
	case "send":
		return send(args)
	case "tail":
		return tail()
	case "media":
		return media(args)
	case "demo":
		return demoRun()
	case "config":
		return configCmd()
	case "unread":
		return unread(args)
	case "open":
		return openUI()
	case "":
		return tui.Run()
	case "help", "-h", "--help":
		fmt.Println("uso: zapterm [login | daemon | chats | msgs <jid> [n] | send <jid> <texto> | tail | media <jid> <id> | demo]")
		return nil
	}
	return fmt.Errorf("comando desconhecido: %s", cmd)
}

func daemon(interactive bool, phone string) error {
	logPath, err := paths.Log()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	var out io.Writer = f
	var qr io.Writer
	if interactive {
		// No login o log vai também para a tela, pra acompanhar o history sync.
		out = io.MultiWriter(f, os.Stderr)
		qr = os.Stdout
	}
	log := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return wa.Run(ctx, wa.Options{Log: log, QROut: qr, Phone: phone})
}

func dial() (*ipc.Client, error) {
	c, err := ipc.Dial(paths.Socket())
	if err != nil {
		return nil, fmt.Errorf("daemon não está rodando (%w)", err)
	}
	return c, nil
}

func chats() error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	var list []ipc.Chat
	if err := c.Call(ipc.Request{Op: ipc.OpChats}, &list); err != nil {
		return err
	}
	for _, ch := range list {
		unread := ""
		if ch.Unread > 0 {
			unread = fmt.Sprintf(" (%d)", ch.Unread)
		}
		fmt.Printf("%-16s %-28s %s%s\n    %s\n",
			when(ch.LastTS), trunc(ch.Name, 28), ch.JID, unread, trunc(ch.LastText, 70))
	}
	return nil
}

func msgs(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: zapterm msgs <jid> [n]")
	}
	n := 30
	if len(args) > 1 {
		n, _ = strconv.Atoi(args[1])
	}
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	var list []ipc.Message
	if err := c.Call(ipc.Request{Op: ipc.OpMessages, Chat: args[0], Limit: n}, &list); err != nil {
		return err
	}
	for _, m := range list {
		fmt.Printf("%s  %s: %s\n", when(m.TS), m.SenderName, m.Text)
	}
	return nil
}

func send(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: zapterm send <jid> <texto>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	var m ipc.Message
	if err := c.Call(ipc.Request{Op: ipc.OpSend, Chat: args[0], Text: strings.Join(args[1:], " ")}, &m); err != nil {
		return err
	}
	fmt.Println("enviada:", m.ID)
	return nil
}

func tail() error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	var st ipc.Status
	if err := c.Call(ipc.Request{Op: ipc.OpStatus}, &st); err != nil {
		return err
	}
	fmt.Printf("status: %s %s\n", st.State, st.Me)
	for f := range c.Events() {
		fmt.Printf("%s %s\n", f.Event, compact(f.Data))
	}
	return ipc.ErrClosed
}

func when(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format("02/01 15:04")
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func compact(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

func media(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: zapterm media <jid> <id>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	var md ipc.Media
	if err := c.Call(ipc.Request{Op: ipc.OpMedia, Chat: args[0], Msg: args[1]}, &md); err != nil {
		return err
	}
	fmt.Printf("arquivo: %s\nprévia:  %s (%dx%d)\ntipo:    %s\n", md.Path, md.Preview, md.Width, md.Height, md.Mime)
	return nil
}

// demoRun sobe o daemon de mentira num socket próprio e abre a TUI nele;
// o daemon de verdade e as suas conversas não são tocados.
func demoRun() error {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("zapterm-demo-%d.sock", os.Getpid()))
	os.Setenv("ZAPTERM_SOCKET", sock)
	os.Setenv("ZAPTERM_DEMO", "1") // a TUI abre a primeira conversa sozinha
	srv, err := demo.Serve(sock)
	if err != nil {
		return err
	}
	defer func() { srv.Close(); os.Remove(sock) }()
	return tui.Run()
}

func unread(args []string) error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	var u ipc.Unread
	if err := c.Call(ipc.Request{Op: ipc.OpUnread}, &u); err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "--json" {
		return json.NewEncoder(os.Stdout).Encode(u)
	}
	fmt.Printf("%d conversas, %d mensagens não lidas\n", u.Chats, u.Messages)
	for _, t := range u.Top {
		fmt.Printf("  %4d  %s\n", t.Unread, t.Name)
	}
	return nil
}

func openUI() error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(ipc.Request{Op: ipc.OpShowUI}, nil)
}

func configCmd() error {
	created, err := config.WriteDefault()
	if err != nil {
		return err
	}
	if created {
		fmt.Println("criado (tudo comentado, com os padrões):", config.Path())
	} else {
		fmt.Println("já existe:", config.Path())
	}
	if _, err := config.Load(); err != nil {
		fmt.Println("atenção:", err)
	}
	return nil
}
