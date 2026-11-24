package wa

import (
	"errors"
	"os"
	"syscall"

	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// lockDaemon trava daemon.lock com flock; quem não conseguir é o segundo
// daemon. O arquivo precisa ficar aberto até o fim do Run.
func lockDaemon() (*os.File, error) {
	path, err := paths.Lock()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("o daemon já está rodando")
		}
		return nil, err
	}
	return f, nil
}
