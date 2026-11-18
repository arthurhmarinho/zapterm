package wa

import (
	"context"
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// waLogger adapta o slog para a interface de log do whatsmeow. Debug do
// whatsmeow é barulhento demais e fica de fora.
type waLogger struct{ l *slog.Logger }

func (w waLogger) Errorf(msg string, args ...any) { w.l.Error(fmt.Sprintf(msg, args...)) }
func (w waLogger) Warnf(msg string, args ...any)  { w.l.Warn(fmt.Sprintf(msg, args...)) }
func (w waLogger) Infof(msg string, args ...any)  { w.l.Info(fmt.Sprintf(msg, args...)) }
func (w waLogger) Debugf(msg string, args ...any) {
	if w.l.Enabled(context.Background(), slog.LevelDebug) {
		w.l.Debug(fmt.Sprintf(msg, args...))
	}
}
func (w waLogger) Sub(module string) waLog.Logger { return waLogger{w.l.With("sub", module)} }
