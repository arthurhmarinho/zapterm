package wa

import (
	"context"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
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

// search procura nas mensagens, da mais nova para a mais antiga.
// search procura q no texto das mensagens; com chat, só naquela conversa
// (a busca do ctrl+f), que aceita termos de 2 letras e até 500 achados.
func (d *Daemon) search(ctx context.Context, chat, q string, limit int) ([]ipc.SearchHit, error) {
	q = fold(strings.TrimSpace(q))
	minLen, maxLimit := 3, 100
	if chat != "" {
		minLen, maxLimit = 2, 500
	}
	if len([]rune(q)) < minLen {
		return []ipc.SearchHit{}, nil
	}
	if limit <= 0 || limit > maxLimit {
		limit = 30
	}
	msgs, err := d.st.SearchMessages(chat, func(text string) bool { return strings.Contains(fold(text), q) }, limit)
	if err != nil {
		return nil, err
	}
	d.fillSenders(ctx, msgs)
	d.fillMentions(ctx, msgs)
	chats := map[string]ipc.Chat{}
	out := make([]ipc.SearchHit, 0, len(msgs))
	for _, m := range msgs {
		c, ok := chats[m.Chat]
		if !ok {
			if c, err = d.st.Chat(m.Chat); err != nil {
				c = ipc.Chat{JID: m.Chat}
			}
			d.decorate(ctx, &c)
			chats[m.Chat] = c
		}
		out = append(out, ipc.SearchHit{Chat: c, Message: m})
	}
	return out, nil
}
