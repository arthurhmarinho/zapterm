package tui

import (
	_ "embed"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/arthurhmarinho/zapterm/internal/paths"
)

// Emoji pelo nome. emoji.tsv vem dos nomes do CLDR (Unicode) em português
// e inglês, gerado por scripts/gen_emoji.py: "emoji<TAB>nome|palavras…".
// Digitar ":ri" no fim do texto abre a lista (😂 🤣 …); o painel de
// reações (ctrl+e) usa a mesma busca. Os usados por último vêm primeiro.

//go:embed emoji.tsv
var emojiTSV string

type emojiEntry struct {
	char  string
	name  string   // nome em português
	words []string // nome e palavras, sem acento e minúsculas
}

var emojiTable = sync.OnceValue(func() []emojiEntry {
	var out []emojiEntry
	for _, line := range strings.Split(emojiTSV, "\n") {
		e, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		words := strings.Split(rest, "|")
		ent := emojiEntry{char: e, name: words[0]}
		for _, w := range words {
			ent.words = append(ent.words, foldText(w))
		}
		out = append(out, ent)
	}
	return out
})

var foldT = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// foldText tira acentos e põe em minúsculas ("coração" → "coracao").
func foldText(s string) string {
	out, _, err := transform.String(foldT, strings.ToLower(s))
	if err != nil {
		return strings.ToLower(s)
	}
	return out
}

// popularEmoji: os mais usados no WhatsApp no Brasil; desempatam a busca
// ("kkk" é palavra de vários emojis de risada, mas o 😂 é o esperado).
var popularEmoji = func() map[string]int {
	list := []string{"😂", "❤️", "👍", "🙏", "😍", "🤣", "😭", "🔥", "😊", "🥰", "😘", "😅", "👏", "💪",
		"😉", "😎", "🤔", "🙄", "😢", "😡", "🎉", "✨", "💯", "😁", "🥺", "😆", "🤩", "😴", "🤝", "👀"}
	out := map[string]int{}
	for i, e := range list {
		out[e] = len(list) - i
	}
	return out
}()

// searchEmoji: palavra exata, depois começo de uma palavra (do nome ou
// das palavras-chave) e, por fim, trecho. No empate: usado recentemente, popular,
// termo mais cedo nas palavras do emoji, emoji simples antes do composto.
func (m *model) searchEmoji(q string, limit int) []emojiEntry {
	q = foldText(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	recent := map[string]int{}
	for i, e := range m.emojiRecent {
		recent[e] = len(m.emojiRecent) - i
	}
	type hit struct {
		e                    emojiEntry
		tier, rec, pop, word int
		runes, order         int
	}
	var hits []hit
	for i, e := range emojiTable() {
		tier, word := 99, 99
		for j, w := range e.words {
			t := 99
			switch {
			case w == q:
				t = 0
			case strings.HasPrefix(w, q) || strings.Contains(w, " "+q):
				t = 1 // começo do nome ou de uma palavra
			case len(q) >= 3 && strings.Contains(w, q):
				t = 2
			}
			if t < tier || t == tier && j < word {
				tier, word = t, j
			}
		}
		if tier < 99 {
			hits = append(hits, hit{e, tier, recent[e.char], popularEmoji[e.char], word,
				len([]rune(strings.ReplaceAll(e.char, "\ufe0f", ""))), i})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		x, y := hits[a], hits[b]
		switch {
		case x.tier != y.tier:
			return x.tier < y.tier
		case x.rec != y.rec:
			return x.rec > y.rec
		case x.pop != y.pop:
			return x.pop > y.pop
		case x.word != y.word:
			return x.word < y.word
		case x.runes != y.runes:
			return x.runes < y.runes
		}
		return x.order < y.order
	})
	out := make([]emojiEntry, 0, limit)
	for _, h := range hits {
		if len(out) == limit {
			break
		}
		out = append(out, h.e)
	}
	return out
}

// ---------- recentes ----------

func recentPath() string {
	dir, err := paths.CacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "emoji-recentes")
}

func loadRecentEmoji() []string {
	p := recentPath()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

// useEmoji põe o emoji no topo dos recentes (e grava).
func (m *model) useEmoji(e string) {
	out := []string{e}
	for _, x := range m.emojiRecent {
		if x != e && len(out) < 30 {
			out = append(out, x)
		}
	}
	m.emojiRecent = out
	if p := recentPath(); p != "" {
		_ = os.WriteFile(p, []byte(strings.Join(out, "\n")+"\n"), 0o600)
	}
}

// ---------- ":nome" no campo de escrita ----------

const emojiShown = 6

var emojiTailRe = regexp.MustCompile(`(?:^|\s):([\p{L}\p{N}_+-]{2,})$`)

type emojiState struct {
	open      bool
	query     string
	list      []emojiEntry
	sel       int
	dismissed string
}

func (m *model) updateEmoji() tea.Cmd {
	es := &m.emoji
	sub := emojiTailRe.FindStringSubmatch(m.input.Value())
	if sub == nil || sub[1] == es.dismissed {
		was := es.open
		es.open = false
		if sub == nil {
			es.dismissed = ""
		}
		if was {
			m.layout()
		}
		return nil
	}
	es.query = sub[1]
	es.list = m.searchEmoji(es.query, emojiShown)
	es.sel = min(es.sel, max(0, len(es.list)-1))
	es.open = len(es.list) > 0
	m.layout()
	return nil
}

func (m *model) keyEmoji(s string) (tea.Cmd, bool) {
	es := &m.emoji
	switch s {
	case "up":
		es.sel = max(0, es.sel-1)
	case "down":
		es.sel = min(len(es.list)-1, es.sel+1)
	case "tab", "enter":
		e := es.list[es.sel]
		v := m.input.Value()
		m.input.SetValue(v[:len(v)-len(es.query)-1] + e.char)
		m.useEmoji(e.char)
		es.open, es.sel = false, 0
		m.layout()
	case "esc":
		es.dismissed, es.open = es.query, false
		m.layout()
	default:
		return nil, false
	}
	return nil, true
}

func (m *model) viewEmoji(w int) string {
	var lines []string
	for i, e := range m.emoji.list {
		name := e.char + "  " + e.name
		if i == m.emoji.sel {
			lines = append(lines, "  "+sGreen.Render("▸ ")+sGreen.Bold(true).Render(name))
		} else {
			lines = append(lines, "    "+sText.Render(name))
		}
	}
	lines = append(lines, "  "+sText2.Render("↑/↓ escolhe · tab ou enter põe · esc fecha"))
	return strings.Join(lines, "\n")
}

// ---------- reagir com qualquer emoji ----------

// reactChoices: sem busca, as 6 rápidas; com busca, os achados (até 9).
func (m *model) reactChoices() []string {
	if m.reactQuery == "" {
		return quickReactions
	}
	var out []string
	for _, e := range m.searchEmoji(m.reactQuery, 9) {
		out = append(out, e.char)
	}
	return out
}
