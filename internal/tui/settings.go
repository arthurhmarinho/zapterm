package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/arthurhmarinho/zapterm/internal/config"
)

// cfg é a configuração lida ao abrir a TUI (~/.config/zapterm/config.toml).
var cfg = config.Config{Interface: config.Interface{Images: true, NerdFont: true}}

// Cores que não estão na paleta base e passaram a ser configuráveis.
var (
	cOwnBorder   = lipgloss.Color("6")
	cOtherBorder = lipgloss.Color("8")
	cReadTick    = lipgloss.Color("4")
)

// applyConfig põe a configuração em vigor: cores, ícones e programas. O
// erro (arquivo inválido) volta para a TUI avisar; os padrões continuam.
func applyConfig() error {
	c, err := config.Load()
	cfg = c
	if v := c.Colors.Accent; v != "" {
		cGreen, cBadge, cChipOnBg = lipgloss.Color(v), lipgloss.Color(v), lipgloss.Color(v)
	}
	if v := c.Colors.Dim; v != "" {
		cText2, cDivider, cOtherBorder = lipgloss.Color(v), lipgloss.Color(v), lipgloss.Color(v)
	}
	if v := c.Colors.OtherBubble; v != "" {
		cOtherBorder = lipgloss.Color(v)
	}
	if v := c.Colors.OwnBubble; v != "" {
		cOwnBorder = lipgloss.Color(v)
	}
	if v := c.Colors.Panel; v != "" {
		cBgPanel, cBgHover, cDayPill = lipgloss.Color(v), lipgloss.Color(v), lipgloss.Color(v)
	}
	if v := c.Colors.ReadTick; v != "" {
		cReadTick = lipgloss.Color(v)
	}
	if v := c.Colors.Danger; v != "" {
		cDanger = lipgloss.Color(v)
	}
	sText2, sGreen = fg(cText2), fg(cGreen)
	if !c.Interface.NerdFont {
		icons = plainIcons
	}
	return err
}

// keyFor diz a tecla configurada para uma ação (para a ajuda e o rodapé).
func keyFor(action string) string {
	if k, ok := cfg.Keys[action]; ok && k != "" {
		return k
	}
	return config.Actions[action]
}

// configuredApp devolve o comando de [apps] para um tipo, com o arquivo no
// lugar de {} (ou no fim); nil se não configurado.
func configuredApp(kind, path string) []string {
	var cmd string
	switch kind {
	case "video":
		cmd = cfg.Apps.Video
	case "pdf":
		cmd = cfg.Apps.PDF
	case "image":
		cmd = cfg.Apps.Image
	case "browser":
		cmd = cfg.Apps.Browser
	}
	f := strings.Fields(cmd)
	if len(f) == 0 {
		return nil
	}
	placed := false
	for i, a := range f {
		if a == "{}" {
			f[i], placed = path, true
		}
	}
	if !placed {
		f = append(f, path)
	}
	return f
}

// imagesAllowed: [interface] images = false desliga, mas ZAPTERM_IMAGES
// (usado em testes e para forçar) tem a palavra final.
func imagesAllowed() bool {
	if v := os.Getenv("ZAPTERM_IMAGES"); v != "" {
		return v != "0"
	}
	return cfg.Interface.Images
}
