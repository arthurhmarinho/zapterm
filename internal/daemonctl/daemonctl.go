// Package daemonctl cuida do ciclo de vida do daemon do lado de quem o usa
// (TUI e comandos stop/restart): ligar, conferir a versão, reiniciar e parar.
package daemonctl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/buildinfo"
	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Action é o que fazer com o daemon que já está rodando.
type Action int

const (
	Keep      Action = iota // usa como está
	WarnStale               // é esta TUI que está velha: só avisa
	Shutdown                // pede OpShutdown
	Kill                    // não conhece OpShutdown (anterior a ela): SIGTERM
)

// IsLegacy diz se o erro veio de um daemon anterior à OpHello/OpShutdown:
// pareado, ele responde "operação desconhecida"; em pareamento, "não conectado".
func IsLegacy(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "operação desconhecida") || strings.Contains(s, "não conectado")
}

// Decide escolhe a ação a partir da resposta do OpHello (h, ou err se falhou),
// da versão desta TUI e de ela mesma ter sido apagada do disco (binário
// atualizado com a TUI aberta). Com "dev" dos dois lados nada é reiniciado,
// para o desenvolvimento local não ficar em laço.
func Decide(h ipc.Hello, err error, mine string, uiDeleted bool) Action {
	if err != nil {
		if !IsLegacy(err) {
			return Keep // timeout, conexão caindo: não mexe no que não entende
		}
		if uiDeleted {
			return WarnStale
		}
		return Kill
	}
	if h.Proto == ipc.ProtoVersion && h.Version == mine {
		return Keep
	}
	if uiDeleted {
		return WarnStale
	}
	return Shutdown
}

// SelfDeleted: o binário em execução foi substituído (o Linux marca o link
// de /proc/self/exe com " (deleted)").
func SelfDeleted() bool {
	p, err := os.Readlink("/proc/self/exe")
	return err == nil && strings.HasSuffix(p, " (deleted)")
}

// Connect devolve um cliente ligado ao daemon, subindo o daemon se preciso e
// trocando-o se for de outra versão. stale = true: esta TUI é que está velha
// (o binário mudou); o daemon foi mantido e ela deve pedir para reabrir.
func Connect() (cli *ipc.Client, stale bool, err error) {
	if c, err := ipc.Dial(paths.Socket()); err == nil {
		switch Reconcile(c) {
		case Keep:
			return c, false, nil
		case WarnStale:
			return c, true, nil
		}
		// Reconcile já derrubou o daemon velho; segue para subir o novo.
		c.Close()
		waitGone(5 * time.Second)
	}
	c, err := Start()
	return c, false, err
}

// Reconcile faz o OpHello e executa a ação escolhida por Decide (OpShutdown ou
// SIGTERM). Devolve Keep/WarnStale quando o daemon ficou e Shutdown/Kill
// quando foi derrubado.
func Reconcile(c *ipc.Client) Action {
	h, err := c.Hello()
	act := Decide(h, err, buildinfo.V(), SelfDeleted())
	switch act {
	case Shutdown:
		if err := c.Call(ipc.Request{Op: ipc.OpShutdown}, nil); err != nil {
			if !IsLegacy(err) {
				return Keep
			}
			act = Kill
		}
	}
	if act == Kill {
		if err := killPeer(c); err != nil {
			return Keep
		}
	}
	return act
}

// killPeer manda SIGTERM a quem atende o socket (o main do daemon já trata
// SIGTERM e encerra limpo).
func killPeer(c *ipc.Client) error {
	pid, err := c.PeerPID()
	if err != nil {
		return err
	}
	if pid <= 1 || pid == os.Getpid() {
		return fmt.Errorf("pid inválido: %d", pid)
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// lockHeld: há um daemon vivo com o daemon.lock travado.
func lockHeld() bool {
	path, err := paths.Lock()
	if err != nil {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// waitGone espera o socket parar de responder.
func waitGone(max time.Duration) bool {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		c, err := ipc.Dial(paths.Socket())
		if err != nil {
			return true
		}
		c.Close()
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// ErrNotRunning: não havia daemon para parar.
var ErrNotRunning = errors.New("o daemon não está rodando")

// Stop encerra o daemon e espera o socket sumir.
func Stop() error {
	c, err := ipc.Dial(paths.Socket())
	if err != nil {
		return ErrNotRunning
	}
	defer c.Close()
	if err := c.Call(ipc.Request{Op: ipc.OpShutdown}, nil); err != nil {
		if !IsLegacy(err) {
			return err
		}
		if err := killPeer(c); err != nil {
			return fmt.Errorf("daemon antigo, sem como pedir que saia: %w", err)
		}
	}
	if !waitGone(5 * time.Second) {
		return errors.New("o daemon não encerrou em 5s")
	}
	return nil
}

// Start sobe o daemon e devolve o cliente. O daemon nasce em sessão própria
// (setsid): fechar a TUI ou o terminal não derruba a conexão com o WhatsApp.
func Start() (*ipc.Client, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Binário trocado com este processo vivo: o caminho ganha " (deleted)", mas
	// o arquivo novo está no mesmo lugar.
	exe = strings.TrimSuffix(exe, " (deleted)")
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
			// Outro daemon pode ter ganhado a corrida (o lock é dele): se ele
			// já atende, é com ele que a TUI fica; se ainda está abrindo o
			// socket, espera por ele em vez de desistir.
			if c, derr := ipc.Dial(paths.Socket()); derr == nil {
				return c, nil
			}
			if lockHeld() {
				died = nil
				continue
			}
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
