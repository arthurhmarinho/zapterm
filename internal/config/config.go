// Package config lê ~/.config/zapterm/config.toml. Tudo é opcional: sem o
// arquivo (ou sem uma chave), vale o padrão, que é o comportamento de antes
// de existir configuração.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// Default é o arquivo que `zapterm config` cria: tudo comentado, com os
// valores padrão, para servir de documentação.
//
//go:embed default.toml
var Default string

type Config struct {
	Keys      map[string]string `toml:"keys"`
	Colors    Colors            `toml:"colors"`
	Apps      Apps              `toml:"apps"`
	Privacy   Privacy           `toml:"privacy"`
	Interface Interface         `toml:"interface"`
	// Transcription: transcrição local de áudio (whisper.cpp, opcional).
	Transcription Transcription `toml:"transcription"`
}

type Transcription struct {
	Command  string `toml:"command"`  // "" = whisper-cli (ou whisper-cpp) do PATH
	Model    string `toml:"model"`    // arquivo ggml-*.bin; "" = procura em ~/.local/share/zapterm/whisper
	Language string `toml:"language"` // "pt"; "auto" detecta
}

type Colors struct {
	Accent      string `toml:"accent"`
	Dim         string `toml:"dim"`
	OwnBubble   string `toml:"own_bubble"`
	OtherBubble string `toml:"other_bubble"`
	Panel       string `toml:"panel"`
	ReadTick    string `toml:"read_tick"`
	Danger      string `toml:"danger"`
}

type Apps struct {
	Video   string `toml:"video"`
	PDF     string `toml:"pdf"`
	Image   string `toml:"image"`
	Browser string `toml:"browser"`
}

type Privacy struct {
	ReadReceipts   bool `toml:"read_receipts"`
	ShowOnline     bool `toml:"show_online"`
	NotifyPreview  bool `toml:"notify_preview"`
	Notifications  bool `toml:"notifications"`
	NotifyMentions bool `toml:"notify_muted_mentions"`
	LinkPreviews   bool `toml:"link_previews"`
}

type Interface struct {
	Images    bool `toml:"images"`
	NerdFont  bool `toml:"nerd_font"`
	ListWidth int  `toml:"list_width"` // 0 = automático
	// EnterSends: enter envia (padrão). false: enter quebra a linha e
	// alt+enter envia.
	EnterSends bool `toml:"enter_sends"`
	// NotifySound: arquivo de som tocado a cada notificação ("" = sem som).
	NotifySound string `toml:"notify_sound"`
	// Terminal: comando do terminal que o clique na notificação ou no widget
	// abre (o zapterm vai no fim). Vazio = $TERMINAL, depois o primeiro
	// instalado (kitty, foot, alacritty, wezterm, ghostty).
	Terminal []string `toml:"terminal"`
}

// Actions são os atalhos que dá para trocar em [keys], com o padrão.
var Actions = map[string]string{
	"reply":     "ctrl+r",
	"react":     "ctrl+e",
	"open":      "ctrl+o",
	"copy":      "alt+c",
	"edit":      "alt+e",
	"delete":    "alt+d",
	"forward":   "alt+w",
	"chat_menu": "alt+m",
	"profile":   "alt+i",
	"stickers":  "ctrl+s",
	"record":    "ctrl+t",
	"attach":    "alt+f",
	"paste":     "ctrl+v",
	"search":    "ctrl+p",
	"help":      "f1",
	"archived":  "alt+a",
	"me":        "alt+y",
	"status":    "alt+r",
}

// Defaults são os valores sem arquivo de config.
func Defaults() Config { return defaults() }

func defaults() Config {
	return Config{
		Keys: map[string]string{},
		Privacy: Privacy{
			ReadReceipts: true, ShowOnline: true, NotifyPreview: true,
			Notifications: true, NotifyMentions: true, LinkPreviews: true,
		},
		Interface: Interface{Images: true, NerdFont: true, EnterSends: true},
	}
}

// Path devolve onde o arquivo fica (respeitando XDG_CONFIG_HOME).
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "zapterm", "config.toml")
}

// Load lê o arquivo. Sem arquivo: padrões e nenhum erro. Com erro de
// sintaxe ou chave desconhecida: padrões (o que der para aproveitar) e o
// erro, para quem chamou avisar.
func Load() (Config, error) {
	c := defaults()
	md, err := toml.DecodeFile(Path(), &c)
	if errors.Is(err, fs.ErrNotExist) {
		return defaults(), nil
	}
	if err != nil {
		return defaults(), fmt.Errorf("%s: %w", Path(), err)
	}
	if c.Keys == nil {
		c.Keys = map[string]string{}
	}
	if und := md.Undecoded(); len(und) > 0 {
		return c, fmt.Errorf("%s: chave desconhecida %q", Path(), und[0].String())
	}
	for action := range c.Keys {
		if _, ok := Actions[action]; !ok {
			return c, fmt.Errorf("%s: atalho desconhecido em [keys]: %q", Path(), action)
		}
	}
	return c, nil
}

// KeyRemap traduz as teclas configuradas para as teclas padrão que o
// código entende. A tecla padrão de uma ação trocada fica desligada (vira
// ""), a não ser que outra ação a tenha pegado.
func (c Config) KeyRemap() map[string]string {
	remap := map[string]string{}
	names := make([]string, 0, len(c.Keys))
	for a := range c.Keys {
		names = append(names, a)
	}
	sort.Strings(names)
	for _, a := range names {
		if def := Actions[a]; def != c.Keys[a] {
			remap[def] = ""
		}
	}
	for _, a := range names {
		if def := Actions[a]; def != c.Keys[a] {
			remap[c.Keys[a]] = def
		}
	}
	return remap
}

// WriteDefault cria o arquivo padrão se ainda não existir.
func WriteDefault() (created bool, err error) {
	p := Path()
	if _, err := os.Stat(p); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return false, err
	}
	return true, os.WriteFile(p, []byte(Default), 0o600)
}
