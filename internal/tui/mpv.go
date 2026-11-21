package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// mpvCtl conversa com um mpv pelo socket de IPC (--input-ipc-server):
// pausa, velocidade e posição mudam com o áudio tocando, e a posição vem
// exata (não estimada pelo relógio).
type mpvCtl struct {
	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
	next int
}

var hasMPV = sync.OnceValue(func() bool { _, err := exec.LookPath("mpv"); return err == nil })

func mpvSocket() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, fmt.Sprintf("zapterm-mpv-%d.sock", os.Getpid()))
}

func mpvCommand(path, sock string, speed float64) *exec.Cmd {
	args := []string{"--no-video", "--no-terminal", "--really-quiet", "--no-config",
		fmt.Sprintf("--speed=%g", speed), "--input-ipc-server=" + sock}
	if os.Getenv("ZAPTERM_AUDIO_NULL") != "" {
		args = append(args, "--ao=null") // testes: sem som nas caixas
	}
	return exec.Command("mpv", append(args, "--", path)...)
}

// dialMPV espera o socket aparecer (o mpv cria logo ao abrir).
func dialMPV(sock string) (*mpvCtl, error) {
	var last error
	for i := 0; i < 40; i++ {
		c, err := net.Dial("unix", sock)
		if err == nil {
			return &mpvCtl{conn: c, rd: bufio.NewReader(c)}, nil
		}
		last = err
		time.Sleep(25 * time.Millisecond)
	}
	return nil, last
}

// call manda um comando e espera a resposta dele (os eventos que o mpv
// manda no meio são ignorados).
func (c *mpvCtl) call(args ...any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	b, _ := json.Marshal(map[string]any{"command": args, "request_id": id})
	c.conn.SetDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	for {
		line, err := c.rd.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var r struct {
			Data  json.RawMessage `json:"data"`
			Error string          `json:"error"`
			ID    int             `json:"request_id"`
		}
		if json.Unmarshal(line, &r) != nil || r.ID != id {
			continue
		}
		if r.Error != "success" {
			return nil, fmt.Errorf("mpv: %s", r.Error)
		}
		return r.Data, nil
	}
}

func (c *mpvCtl) state() (pos float64, paused bool) {
	if d, err := c.call("get_property", "time-pos"); err == nil {
		json.Unmarshal(d, &pos)
	}
	if d, err := c.call("get_property", "pause"); err == nil {
		json.Unmarshal(d, &paused)
	}
	return
}

func (c *mpvCtl) close() {
	if c != nil {
		c.conn.Close()
	}
}
