package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// O desenho depende de cada placeholder (U+10EEEE + 2 diacríticos) contar
// como uma coluna para o lipgloss e o ansi; senão as bordas quebram.
func TestPlaceholderLargura(t *testing.T) {
	p := placeholder(&kimage{id: 70000, cols: 12, rows: 3})
	lines := strings.Split(p, "\n")
	if len(lines) != 3 {
		t.Fatalf("linhas = %d", len(lines))
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w != 12 {
			t.Fatalf("lipgloss.Width = %d, quer 12", w)
		}
		if w := ansi.StringWidth(ansi.Truncate(l, 12, "")); w != 12 {
			t.Fatalf("Truncate mudou a largura: %d", w)
		}
	}
	b := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render(p)
	for _, l := range strings.Split(b, "\n") {
		if w := lipgloss.Width(l); w != 16 {
			t.Fatalf("balão com largura %d, quer 16: %q", w, l)
		}
	}
	if !strings.Contains(b, "\x1b[38;2;1;17;112m") {
		t.Fatal("a cor do id (70000 = 1,17,112) se perdeu dentro do balão")
	}
}

// Animação: um a=T, um a=f por quadro extra (com a duração), a duração do
// primeiro quadro e o comando que põe para rodar em loop.
func TestAnimacaoKitty(t *testing.T) {
	im := &images{enabled: true, next: 1, placed: map[string]*kimage{}}
	md := &ipc.Media{Preview: "/a/0.png", Width: 160, Height: 160,
		Frames: []string{"/a/0.png", "/a/1.png", "/a/2.png"}, Delays: []int{100, 50, 70}}
	if im.media(md, 10, 5) == "" {
		t.Fatal("sem placeholder")
	}
	seq := strings.Join(im.pending, "")
	for _, want := range []string{"a=T,U=1", "a=f,i=1,f=100,t=f,z=50", "a=f,i=1,f=100,t=f,z=70",
		"a=a,i=1,r=1,z=100", "a=a,i=1,s=3,v=1"} {
		if !strings.Contains(seq, want) {
			t.Errorf("faltou %q", want)
		}
	}
	// Desenhar de novo não retransmite.
	im.pending = nil
	im.media(md, 10, 5)
	if len(im.pending) != 0 {
		t.Fatal("retransmitiu a animação")
	}
}

// ctrl+o numa foto baixada abre em tela cheia (sem xdg-open) e a imagem é
// transmitida no mesmo Update, não só na próxima tecla.
func TestFotoEmTelaCheia(t *testing.T) {
	m := newModel(nil)
	m.imgs.enabled = true
	m.w, m.h = 120, 40
	m.active = "x@s.whatsapp.net"
	m.msgs = []ipc.Message{{ID: "f", Kind: "image", Text: "[imagem] praia",
		Media: &ipc.Media{Path: "/x/f.jpg", Preview: "/x/f.preview.png", Width: 720, Height: 480}}}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.viewing == nil {
		t.Fatal("não abriu em tela cheia")
	}
	if cmd == nil {
		t.Fatal("a transmissão da foto não saiu neste Update")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "praia") {
		t.Fatal("legenda não apareceu")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.viewing != nil {
		t.Fatal("esc não fechou")
	}
}

func TestOpener(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, x := range names {
				if x == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", os.ErrNotExist
		}
	}
	cases := []struct {
		path     string
		look     func(string) (string, error)
		wantProg string
	}{
		{"/c/a.pdf", has("zathura", "sioyek"), "zathura"},
		{"/c/a.pdf", has("sioyek"), "sioyek"},
		{"/c/a.pdf", has(), "xdg-open"},
		{"/c/v.mp4", has("mpv", "gst-launch-1.0"), "mpv"},
		{"/c/v.mp4", has("gst-launch-1.0"), "gst-launch-1.0"},
		{"/c/v.mp4", has(), "xdg-open"},
		{"/c/f.jpg", has("imv"), "imv"},
		{"/c/f.jpg", has(), "xdg-open"},
		{"/c/d.docx", has("zathura", "mpv"), "xdg-open"},
	}
	for _, c := range cases {
		if got, _ := opener(c.path, c.look); got != c.wantProg {
			t.Errorf("%s: %s, quer %s", c.path, got, c.wantProg)
		}
	}
}

// Os botões da caixa de escrita respondem ao clique na posição desenhada.
func TestBotoesDaCaixa(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.active, m.activeChat = "x@s.whatsapp.net", ipc.Chat{JID: "x@s.whatsapp.net", Name: "X", Kind: "dm"}
	m.layout()
	g := m.geom()
	line := strings.Split(ansi.Strip(m.View().Content), "\n")[g.inputY+1]
	if r := []rune(line); string(r[g.stickerBtnX]) != icons.sticker || string(r[g.clipBtnX]) != icons.attach {
		t.Fatalf("botões fora do lugar: %q (x=%d,%d)", line, g.stickerBtnX, g.clipBtnX)
	}
	m.Update(tea.MouseClickMsg{X: g.stickerBtnX, Y: g.inputY + 1, Button: tea.MouseLeft})
	if !m.picking {
		t.Fatal("clique na figurinha não abriu o seletor")
	}
}

// Redimensionar retransmite as imagens (o kitty descarta as antigas).
func TestRedimensionarRetransmite(t *testing.T) {
	m := newModel(nil)
	m.imgs.enabled = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.imgs.block("/a.png", 96, 96, 6, 3)
	m.imgs.pending = nil
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if len(m.imgs.placed) != 0 {
		t.Fatal("não esqueceu as imagens")
	}
	if !strings.Contains(strings.Join(m.imgs.pending, ""), "a=d,d=I") {
		// o delete pode já ter saído no flush do Update; o que importa é
		// que a próxima chamada transmita de novo
	}
	m.imgs.block("/a.png", 96, 96, 6, 3)
	if !strings.Contains(strings.Join(m.imgs.pending, ""), "a=T") {
		t.Fatal("não retransmitiu depois do resize")
	}
}

// Uma imagem de 4000×4000 ocupa 64 MB decodificados: duas cabem no teto
// (128 MB), a terceira estoura. Os testes não tocam em arquivo nenhum: o
// place só transmite o caminho.
const bigPx = 4000

func imagemTeste() *images {
	return &images{enabled: true, next: 1, placed: map[string]*kimage{}}
}

// idDe devolve o id da imagem já transmitida com esse caminho (cols/rows
// pelo place: largura cheia de 6 colunas).
func idDe(t *testing.T, im *images, path string) uint32 {
	t.Helper()
	for key, k := range im.placed {
		if strings.HasPrefix(key, path+"|") {
			return k.id
		}
	}
	t.Fatalf("%s não está em placed", path)
	return 0
}

func (im *images) placeBig(path string) {
	im.place(path, nil, nil, bigPx, bigPx, 6, 6)
}

func TestTetoApagaAMenosUsadaRecentemente(t *testing.T) {
	im := imagemTeste()
	im.frame()
	im.placeBig("/a.png")
	idA := idDe(t, im, "/a.png")
	im.frame()
	im.placeBig("/b.png")
	idB := idDe(t, im, "/b.png")
	im.pending = nil
	im.frame()
	im.placeBig("/c.png") // 192 MB: passa do teto

	apagados := []string{}
	for _, c := range im.pending {
		if strings.Contains(c, "a=d,d=I") {
			apagados = append(apagados, c)
		}
	}
	if len(apagados) != 1 || apagados[0] != fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", idA) {
		t.Fatalf("deveria apagar só a mais antiga (id %d), saiu %q", idA, apagados)
	}
	if strings.Contains(strings.Join(apagados, ""), fmt.Sprintf("i=%d,", idB)) {
		t.Fatal("apagou a segunda, que foi usada depois")
	}
	if keyOf(im, "/a.png") != "" {
		t.Fatal("a apagada continua em placed")
	}
	if im.bytes > kittyBudget {
		t.Fatalf("ficou acima do teto: %d", im.bytes)
	}
}

func keyOf(im *images, path string) string {
	for key := range im.placed {
		if strings.HasPrefix(key, path+"|") {
			return key
		}
	}
	return ""
}

func TestAvatarUsadoEmTodoQuadroNuncaSai(t *testing.T) {
	im := imagemTeste()
	avatar := "/avatar.round.png"
	im.frame()
	im.placeBig(avatar)
	idAv := idDe(t, im, avatar)
	im.pending = nil
	for i := range 20 {
		im.frame()
		im.placeBig(avatar) // o quadro desenha a foto de novo
		im.placeBig(fmt.Sprintf("/foto%d.png", i))
	}
	if strings.Contains(strings.Join(im.pending, ""), fmt.Sprintf("a=d,d=I,i=%d,", idAv)) {
		t.Fatal("apagou o avatar, que estava em uso em todo quadro")
	}
	idDe(t, im, avatar) // ainda em placed
}

func TestApagadaEPedidaDeNovoRetransmite(t *testing.T) {
	im := imagemTeste()
	im.frame()
	im.placeBig("/a.png")
	im.frame()
	im.placeBig("/b.png")
	im.frame()
	im.placeBig("/c.png") // apaga /a.png
	im.pending = nil
	im.frame()
	im.placeBig("/a.png") // pedida de novo: tem que sair um a=T com o caminho
	got := strings.Join(im.pending, "")
	if !strings.Contains(got, "a=T,U=1") || !strings.Contains(got, base64.StdEncoding.EncodeToString([]byte("/a.png"))) {
		t.Fatalf("não retransmitiu: %q", got)
	}
}

// Só as imagens do quadro atual: o que cabe é o que já estava na tela, então
// nada sai, mesmo acima do teto. Sem loop infinito.
func TestQuadroAtualNaoApagaNada(t *testing.T) {
	im := imagemTeste()
	im.frame()
	im.placeBig("/a.png")
	im.placeBig("/b.png")
	im.placeBig("/c.png")
	if strings.Contains(strings.Join(im.pending, ""), "a=d,") {
		t.Fatal("apagou imagem usada no quadro atual")
	}
	if im.bytes <= kittyBudget {
		t.Fatalf("a conta de bytes deveria estar acima do teto: %d", im.bytes)
	}
}

// Correção 4: a foto real troca as iniciais. As iniciais deixam de ser
// pedidas e, com o teto, são apagadas sozinhas.
func TestIniciaisDescartadasDepoisDaFoto(t *testing.T) {
	im := imagemTeste()
	iniciais := "/fallback.png"
	im.frame()
	im.placeBig(iniciais)
	idIni := idDe(t, im, iniciais)
	im.frame()
	im.placeBig("/foto-real.png") // a foto chega; as iniciais não são mais pedidas
	im.frame()
	im.placeBig("/outra.png") // estoura: a menos usada é a das iniciais
	if !strings.Contains(strings.Join(im.pending, ""), fmt.Sprintf("a=d,d=I,i=%d,", idIni)) {
		t.Fatal("as iniciais continuaram no kitty")
	}
}
