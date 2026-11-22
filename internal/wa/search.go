package wa

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
	"github.com/arthurhmarinho/zapterm/internal/store"
)

// foldTable mapeia cada letra acentuada latina para a base minúscula
// ("Ã" → 'a', "ç" → 'c'), montada uma vez a partir da decomposição Unicode.
var foldTable = func() map[rune]rune {
	t := map[rune]rune{}
	for r := rune(0xC0); r <= 0x24F; r++ {
		for _, d := range norm.NFD.String(string(r)) {
			if !unicode.Is(unicode.Mn, d) {
				t[r] = unicode.ToLower(d)
				break
			}
		}
	}
	return t
}()

// fold deixa o texto comparável: minúsculo e sem acento ("Não" → "nao").
// Letra a letra pela tabela: é o laço quente da busca (148 mil textos).
func fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
		case r >= 0xC0 && r <= 0x24F:
			if f, ok := foldTable[r]; ok {
				r = f
			}
		default:
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// typeAliases: o que se escreve depois de tipo: → tipo do store.
var typeAliases = map[string]string{
	"foto": "fotos", "fotos": "fotos", "imagem": "fotos", "imagens": "fotos",
	"video": "videos", "videos": "videos",
	"gif": "gifs", "gifs": "gifs",
	"audio": "audios", "audios": "audios",
	"doc": "docs", "docs": "docs", "documento": "docs", "documentos": "docs", "arquivo": "docs", "arquivos": "docs",
	"link": "links", "links": "links",
	"figurinha": "figurinhas", "figurinhas": "figurinhas",
	"enquete": "enquetes", "enquetes": "enquetes",
	"evento": "eventos", "eventos": "eventos",
	"contato": "contatos", "contatos": "contatos",
	"localizacao": "localizacao", "local": "localizacao", "mapa": "localizacao",
}

var extRe = regexp.MustCompile(`^[a-z0-9]{1,6}$`)

// parseQuery separa os filtros (tipo:pdf, data:12/10, antes:, depois:) do
// texto procurado. Datas no fuso local; dia/mês sem ano é o mais recente
// que já passou.
func parseQuery(q string, now time.Time) (text string, f store.SearchFilter, err error) {
	var words []string
	for _, w := range strings.Fields(q) {
		key, val, ok := strings.Cut(w, ":")
		key, val = fold(key), fold(val)
		if !ok || val == "" {
			words = append(words, w)
			continue
		}
		switch key {
		case "tipo":
			if t, ok := typeAliases[val]; ok {
				f.Type = t
			} else if extRe.MatchString(strings.TrimPrefix(val, ".")) {
				f.Ext = strings.TrimPrefix(val, ".")
			} else {
				return "", f, fmt.Errorf("tipo desconhecido: %s (foto, vídeo, áudio, doc, pdf, link, gif, figurinha, enquete, evento, contato, local)", val)
			}
		case "data", "antes", "depois":
			day, err := parseDay(val, now)
			if err != nil {
				return "", f, err
			}
			next := day.AddDate(0, 0, 1)
			switch key {
			case "data":
				f.From, f.To = day.UnixMilli(), next.UnixMilli()
			case "antes":
				f.To = day.UnixMilli()
			case "depois":
				f.From = next.UnixMilli()
			}
		default:
			words = append(words, w)
		}
	}
	return strings.Join(words, " "), f, nil
}

// parseDay entende hoje, ontem, 12/10, 12/10/25 e 12/10/2025.
func parseDay(v string, now time.Time) (time.Time, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch v {
	case "hoje":
		return today, nil
	case "ontem":
		return today.AddDate(0, 0, -1), nil
	}
	bad := fmt.Errorf("data inválida: %s (use dd/mm, dd/mm/aaaa, hoje ou ontem)", v)
	parts := strings.Split(v, "/")
	if len(parts) < 2 || len(parts) > 3 {
		return time.Time{}, bad
	}
	n := make([]int, len(parts))
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil {
			return time.Time{}, bad
		}
		n[i] = x
	}
	year := now.Year()
	if len(n) == 3 {
		year = n[2]
		if year < 100 {
			year += 2000
		}
	}
	day := time.Date(year, time.Month(n[1]), n[0], 0, 0, 0, 0, now.Location())
	if n[0] < 1 || n[1] < 1 || n[1] > 12 || day.Day() != n[0] {
		return time.Time{}, bad
	}
	if len(n) == 2 && day.After(today) {
		day = day.AddDate(-1, 0, 0)
	}
	return day, nil
}

// search procura q no texto das mensagens; com chat, só naquela conversa
// (a busca do ctrl+f), que aceita termos de 2 letras e até 500 achados.
// Filtros (tipo:, data:, antes:, depois:) dispensam o texto.
func (d *Daemon) search(ctx context.Context, chat, q string, limit int) ([]ipc.SearchHit, error) {
	text, f, err := parseQuery(q, time.Now())
	if err != nil {
		return nil, err
	}
	f.Chat = chat
	text = fold(strings.TrimSpace(text))
	minLen, maxLimit := 3, 100
	if chat != "" {
		minLen, maxLimit = 2, 500
	}
	if f.Active() {
		minLen = 0
	}
	if len([]rune(text)) < minLen {
		return []ipc.SearchHit{}, nil
	}
	if limit <= 0 || limit > maxLimit {
		limit = 30
	}
	msgs, err := d.st.SearchMessages(f, func(t string) bool { return strings.Contains(fold(t), text) }, limit)
	if err != nil {
		return nil, err
	}
	return d.hitsFor(ctx, msgs), nil
}

// hitsFor junta a cada mensagem a conversa dela (busca, favoritas).
func (d *Daemon) hitsFor(ctx context.Context, msgs []ipc.Message) []ipc.SearchHit {
	d.fillSenders(ctx, msgs)
	d.fillMentions(ctx, msgs)
	chats := map[string]ipc.Chat{}
	out := make([]ipc.SearchHit, 0, len(msgs))
	for _, m := range msgs {
		c, ok := chats[m.Chat]
		if !ok {
			var err error
			if c, err = d.st.Chat(m.Chat); err != nil {
				c = ipc.Chat{JID: m.Chat}
			}
			d.decorate(ctx, &c)
			chats[m.Chat] = c
		}
		out = append(out, ipc.SearchHit{Chat: c, Message: m})
	}
	return out
}
