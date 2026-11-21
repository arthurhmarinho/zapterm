package wa

import (
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestPrazoNoEnvio(t *testing.T) {
	msg := &waE2E.Message{Conversation: proto.String("oi")}
	withExpiration(msg, 604800)
	if msg.GetConversation() != "" || msg.GetExtendedTextMessage().GetText() != "oi" {
		t.Fatal("texto simples não virou ExtendedTextMessage")
	}
	if contextInfo(msg).GetExpiration() != 604800 {
		t.Fatal("sem prazo no texto")
	}
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("x")}}}
	withExpiration(img, 86400)
	if ci := contextInfo(img); ci.GetExpiration() != 86400 || ci.GetStanzaID() != "x" {
		t.Fatal("prazo na foto perdeu a citação ou não entrou")
	}
	plain := &waE2E.Message{Conversation: proto.String("oi")}
	withExpiration(plain, 0)
	if plain.GetConversation() != "oi" {
		t.Fatal("sem prazo, a mensagem não deve mudar")
	}
	ts := time.Unix(1000, 0)
	if got := expiresAt(msg, ts); got != ts.Add(7*24*time.Hour).UnixMilli() {
		t.Fatalf("vencimento: %d", got)
	}
}

func TestEventoERecadoDeVideo(t *testing.T) {
	ev := &waE2E.Message{EventMessage: &waE2E.EventMessage{
		Name: proto.String("Trilha"), StartTime: proto.Int64(time.Date(2026, 11, 22, 7, 0, 0, 0, time.Local).Unix()),
		Location: &waE2E.LocationMessage{Name: proto.String("Pico")},
	}}
	kind, text := describe(ev)
	if kind == "" || !strings.Contains(text, "Trilha · 22/11 07:00 · Pico") {
		t.Fatalf("evento: %q %q", kind, text)
	}
	ptv := &waE2E.Message{PtvMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(12)}}
	if kind, text := describe(ptv); kind != "video" || text != "[recado de vídeo 0:12]" {
		t.Fatalf("recado de vídeo: %q %q", kind, text)
	}
}
