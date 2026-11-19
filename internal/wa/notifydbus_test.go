package wa

import (
	"os"
	"testing"
	"time"
)

// Mostra uma notificação de teste no servidor real e espera interação:
//
//	ZAPTERM_NOTIFY_TEST=1 go test ./internal/wa -run DesktopReal -v
func TestDesktopReal(t *testing.T) {
	if os.Getenv("ZAPTERM_NOTIFY_TEST") == "" {
		t.Skip()
	}
	got := make(chan string, 2)
	d, err := newDesktop(
		func(chat string) { got <- "abriu " + chat },
		func(chat, text string) { got <- "respondeu " + chat + ": " + text },
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := d.show("teste@s.whatsapp.net", "zapterm · teste",
		"Clique para testar abrir, ou responda aqui (nada é enviado)", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("notificação %d mostrada; resposta embutida: %v", id, d.reply)
	select {
	case ev := <-got:
		t.Log("evento: " + ev)
	case <-time.After(40 * time.Second):
		t.Log("sem interação em 40s")
	}
}
