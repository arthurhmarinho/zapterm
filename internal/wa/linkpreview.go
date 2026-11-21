package wa

import (
	"bytes"
	"context"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	_ "golang.org/x/image/webp"
	"google.golang.org/protobuf/proto"
)

// Prévia de link ao enviar: abre a página, lê as meta tags og:/twitter:
// (título, descrição, imagem) e manda junto, como o app. Liga e desliga em
// [privacy] link_previews.

var (
	urlInText = regexp.MustCompile(`https?://[^\s<>"]+`)
	metaTag   = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	metaAttr  = regexp.MustCompile(`(?is)(property|name|content)\s*=\s*("([^"]*)"|'([^']*)')`)
	titleTag  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

type linkPreview struct {
	url, title, desc string
	thumb            []byte
}

// fetchPreview baixa até 512 KB da página e até 1 MB da imagem, em 5 s.
func fetchPreview(ctx context.Context, raw string) *linkPreview {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body := get(ctx, raw, 512<<10, "text/html")
	if body == nil {
		return nil
	}
	meta := map[string]string{}
	for _, tag := range metaTag.FindAllString(string(body), -1) {
		var key, val string
		for _, a := range metaAttr.FindAllStringSubmatch(tag, -1) {
			v := a[3] + a[4]
			if strings.EqualFold(a[1], "content") {
				val = v
			} else {
				key = strings.ToLower(v)
			}
		}
		if key != "" && val != "" {
			if _, ok := meta[key]; !ok {
				meta[key] = html.UnescapeString(strings.TrimSpace(val))
			}
		}
	}
	p := &linkPreview{url: raw}
	p.title = first(meta["og:title"], meta["twitter:title"])
	if p.title == "" {
		if t := titleTag.FindStringSubmatch(string(body)); t != nil {
			p.title = html.UnescapeString(strings.Join(strings.Fields(t[1]), " "))
		}
	}
	p.desc = first(meta["og:description"], meta["twitter:description"], meta["description"])
	if p.title == "" && p.desc == "" {
		return nil
	}
	if img := first(meta["og:image"], meta["twitter:image"]); img != "" {
		if iu, err := u.Parse(img); err == nil {
			if data := get(ctx, iu.String(), 1<<20, "image/"); data != nil {
				if src, _, err := image.Decode(bytes.NewReader(data)); err == nil {
					p.thumb, _, _ = scaleJPEG(src, 160, 75)
				}
			}
		}
	}
	return p
}

func first(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// get baixa no máximo limit bytes, só se o tipo começar com want.
func get(ctx context.Context, u string, limit int64, want string) []byte {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	// Agente genérico de navegador: muitos sites só mandam as meta tags assim.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), want) {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil
	}
	return data
}

// withLinkPreview põe a prévia na mensagem de texto (se o texto tiver link
// e a página tiver o que mostrar).
func (d *Daemon) withLinkPreview(ctx context.Context, msg *waE2E.Message, text string) {
	if !d.cfg.Privacy.LinkPreviews {
		return
	}
	u := urlInText.FindString(text)
	if u == "" {
		return
	}
	u = strings.TrimRight(u, ".,;:!?)]}'")
	p := fetchPreview(ctx, u)
	if p == nil {
		return
	}
	if msg.ExtendedTextMessage == nil {
		msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{Text: proto.String(msg.GetConversation())}
		msg.Conversation = nil
	}
	et := msg.ExtendedTextMessage
	et.MatchedText = proto.String(p.url)
	et.Title = proto.String(p.title)
	if p.desc != "" {
		et.Description = proto.String(p.desc)
	}
	if len(p.thumb) > 0 {
		et.JPEGThumbnail = p.thumb
	}
	et.PreviewType = waE2E.ExtendedTextMessage_NONE.Enum()
}
