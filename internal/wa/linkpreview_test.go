package wa

import (
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/arthurhmarinho/zapterm/internal/config"
)

func TestPreviaDeLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/capa.png" {
			w.Header().Set("Content-Type", "image/png")
			png.Encode(w, image.NewRGBA(image.Rect(0, 0, 400, 200)))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Título da aba</title>
			<meta property="og:title" content="Feira de domingo &amp; música">
			<meta content='Começa às 8h na praça' property='og:description'>
			<meta property="og:image" content="/capa.png"></head><body>oi</body></html>`))
	}))
	defer srv.Close()

	p := fetchPreview(context.Background(), srv.URL+"/agenda")
	if p == nil || p.title != "Feira de domingo & música" || p.desc != "Começa às 8h na praça" || len(p.thumb) == 0 {
		t.Fatalf("prévia: %+v", p)
	}

	d := &Daemon{cfg: config.Config{Privacy: config.Privacy{LinkPreviews: true}}}
	msg := &waE2E.Message{Conversation: proto.String("olha " + srv.URL + "/agenda.")}
	d.withLinkPreview(context.Background(), msg, msg.GetConversation())
	et := msg.GetExtendedTextMessage()
	if et.GetTitle() != "Feira de domingo & música" || et.GetMatchedText() != srv.URL+"/agenda" || et.GetText() == "" {
		t.Fatalf("mensagem: %+v", et)
	}

	d.cfg.Privacy.LinkPreviews = false
	off := &waE2E.Message{Conversation: proto.String(srv.URL)}
	d.withLinkPreview(context.Background(), off, off.GetConversation())
	if off.GetExtendedTextMessage() != nil {
		t.Error("link_previews = false e mesmo assim buscou")
	}
}
