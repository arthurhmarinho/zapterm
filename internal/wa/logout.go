package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Desconectar. As mensagens e as mídias locais ficam; só a sessão sai.
// O daemon encerra e a TUI, ao ver o socket cair, sobe um novo, que sem
// sessão entra no modo de pareamento (a tela de login).

// exitDelay: o tempo de a resposta (ou o evento) chegar às TUIs antes de o
// daemon encerrar.
const exitDelay = 400 * time.Millisecond

// logoutWait: quanto esperar o whatsmeow apagar a sessão depois do LoggedOut.
var logoutWait = 3 * time.Second

const loggedOutNote = "Este computador foi desconectado pelo celular."

// noteFile guarda o motivo da desconexão para a tela de login do próximo
// daemon, que roda em outro processo.
func noteFile() (string, error) {
	dir, err := paths.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logged_out.note"), nil
}

func saveLoggedOutNote(text string) {
	if f, err := noteFile(); err == nil {
		_ = os.WriteFile(f, []byte(text), 0o600)
	}
}

// takeLoggedOutNote lê e apaga o aviso: ele vale uma vez só.
func takeLoggedOutNote() string {
	f, err := noteFile()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return ""
	}
	_ = os.Remove(f)
	return strings.TrimSpace(string(b))
}

// logout avisa o servidor (o aparelho some de Aparelhos conectados) e apaga a
// sessão do session.db. Se o servidor não confirmar, não apaga nada: o
// aparelho continuaria na lista do celular.
func (d *Daemon) logout(ctx context.Context) error {
	if !d.paired() {
		return errors.New("não conectado")
	}
	if !d.cli.IsConnected() {
		return errors.New("sem conexão com o WhatsApp; tente de novo")
	}
	if err := d.cli.Logout(ctx); err != nil {
		if d.cli.Store.ID != nil && strings.Contains(err.Error(), "deleting data from store") {
			// O servidor já tirou o aparelho; só faltou limpar o arquivo daqui.
			d.log.Warn("logout: sessão local", "err", err)
			if err := d.cli.Store.Delete(ctx); err != nil {
				return fmt.Errorf("o celular já desconectou, mas não deu para apagar a sessão daqui: %w", err)
			}
		} else {
			d.log.Warn("logout", "err", err)
			return errors.New("sem conexão com o WhatsApp; tente de novo")
		}
	}
	d.log.Info("desconectado a pedido da TUI")
	d.afterLogout()
	return nil
}

// afterLogout: estado, barra, presença e saída do daemon.
func (d *Daemon) afterLogout() {
	d.setStatus("logged_out")
	d.clearUnreadWidget()
	availMu.Lock()
	availSent = nil
	availMu.Unlock()
	d.notif.reset()
	d.exitSoon()
}

// exitSoon cancela o Run depois de exitDelay.
func (d *Daemon) exitSoon() {
	if d.stop == nil {
		return
	}
	time.AfterFunc(exitDelay, d.stop)
}

// onLoggedOut: o celular tirou este aparelho. O whatsmeow apaga a sessão
// (numa goroutine, depois de emitir o evento): espera e, se não apagou,
// apaga. Depois guarda o aviso para a tela de login e sai.
func (d *Daemon) onLoggedOut(e *events.LoggedOut) {
	d.log.Warn("sessão encerrada pelo celular", "reason", e.Reason)
	deadline := time.Now().Add(logoutWait)
	for d.cli.Store.ID != nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if d.cli.Store.ID != nil {
		if err := d.cli.Store.Delete(context.Background()); err != nil {
			d.log.Error("apagando a sessão depois do logout", "err", err)
		}
	}
	saveLoggedOutNote(loggedOutNote)
	d.afterLogout()
}

// clearUnreadWidget zera o resumo de não lidas da barra (Noctalia): com a
// sessão fora, o que ficou no banco não é mais para mostrar lá.
func (d *Daemon) clearUnreadWidget() {
	d.pushUnreadValue(ipc.Unread{Top: []ipc.UnreadChat{}}, true)
}
