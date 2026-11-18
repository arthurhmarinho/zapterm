package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// connect devolve um cliente ligado ao daemon, subindo o daemon se preciso.
// O daemon nasce em sessão própria (setsid): fechar a TUI ou o terminal não
// derruba a conexão com o WhatsApp.
func connect() (*ipc.Client, error) {
	if c, err := ipc.Dial(paths.Socket()); err == nil {
		return c, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("subindo o daemon: %w", err)
	}
	died := make(chan error, 1)
	go func() { died <- cmd.Wait() }()

	deadline := time.After(8 * time.Second)
	for {
		select {
		case err := <-died:
			logPath, _ := paths.Log()
			return nil, fmt.Errorf("o daemon saiu logo ao subir (%v); veja %s ou rode `zapterm login`", err, logPath)
		case <-deadline:
			return nil, errors.New("o daemon não abriu o socket em 8s")
		case <-time.After(100 * time.Millisecond):
			if c, err := ipc.Dial(paths.Socket()); err == nil {
				return c, nil
			}
		}
	}
}
