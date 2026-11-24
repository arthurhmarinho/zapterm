package tui

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

func TestAtrasoDaNovaTentativaDobraAte30Min(t *testing.T) {
	casos := map[int]time.Duration{
		1:  time.Minute,
		2:  2 * time.Minute,
		3:  4 * time.Minute,
		4:  8 * time.Minute,
		5:  16 * time.Minute,
		6:  30 * time.Minute,
		7:  30 * time.Minute,
		50: 30 * time.Minute,
	}
	for n, want := range casos {
		if got := avatarRetryDelay(n); got != want {
			t.Errorf("tentativa %d: %v, quer %v", n, got, want)
		}
	}
}

// Falha de pedido: o JID fica marcado enquanto a espera corre e é liberado
// depois, para o próximo quadro pedir de novo.
func TestFalhaLiberaOJIDDepoisDoAtraso(t *testing.T) {
	m := newModel(nil)
	m.imgs.enabled = true
	const jid = "5584@s.whatsapp.net"
	m.avAsked[jid] = true

	_, cmd := m.Update(avatarFailedMsg{jid: jid})
	if cmd == nil {
		t.Fatal("falha não agendou a nova tentativa")
	}
	if !m.avAsked[jid] {
		t.Fatal("liberou antes do atraso")
	}
	if m.avTries[jid] != 1 {
		t.Fatalf("tentativas: %d", m.avTries[jid])
	}
	m.Update(avatarRetryMsg{jid: jid})
	if m.avAsked[jid] {
		t.Fatal("o retry não liberou o JID")
	}
	// e o próximo quadro que mostra a foto enfileira o pedido
	m.avatar(jid, "Ana", 4, 2)
	if len(m.avQueue) != 1 || m.avQueue[0] != jid {
		t.Fatalf("fila: %v", m.avQueue)
	}
}

// Resposta sem erro e sem foto (contato sem foto): nada de nova tentativa, e
// a contagem de falhas zera quando uma resposta chega.
func TestSemFotoNaoTentaDeNovo(t *testing.T) {
	m := newModel(nil)
	const jid = "5584@s.whatsapp.net"
	m.avAsked[jid] = true
	m.avTries[jid] = 3
	_, cmd := m.Update(avatarMsg{jid: jid, media: nil})
	if cmd != nil {
		t.Fatal("resposta vazia agendou nova tentativa")
	}
	if !m.avAsked[jid] {
		t.Fatal("resposta vazia liberou o JID")
	}
	if _, ok := m.avTries[jid]; ok {
		t.Fatal("a contagem de falhas não zerou")
	}
}

// loadAvatar de verdade contra um daemon de mentira: erro vira avatarFailedMsg,
// e resposta vazia vira avatarMsg com media nil.
func TestLoadAvatarErroEVazio(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sock := filepath.Join(t.TempDir(), "a.sock")
	srv, err := ipc.Listen(sock, func(_ uint64, req ipc.Request) (any, error) {
		if req.Chat == "sem-erro" {
			return nil, nil
		}
		return nil, errors.New("daemon fora")
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	defer srv.Close()
	cli, err := ipc.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	m := newModel(cli)
	if got, ok := m.loadAvatar("com-erro")().(avatarFailedMsg); !ok || got.jid != "com-erro" {
		t.Fatalf("erro: %#v", got)
	}
	got, ok := m.loadAvatar("sem-erro")().(avatarMsg)
	if !ok || got.media != nil {
		t.Fatalf("vazio: %#v", got)
	}
}
