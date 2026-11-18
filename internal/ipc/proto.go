// Package ipc define o protocolo entre o daemon e a TUI: JSON, uma mensagem
// por linha, num socket Unix. Dá para depurar com:
//
//	socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/zapterm.sock
//	{"id":1,"op":"chats"}
package ipc

import "encoding/json"

// Operações aceitas pelo daemon.
const (
	OpStatus   = "status"   // estado da conexão
	OpChats    = "chats"    // lista de chats, mais recente primeiro
	OpMessages = "messages" // mensagens de um chat
	OpSend     = "send"     // envia texto
	OpRead     = "read"     // marca o chat como lido
	OpViewing  = "viewing"  // chat aberto numa TUI em foco ("" = nenhum); suprime notificação
)

// Eventos empurrados pelo daemon sem pedido.
const (
	EvMessage = "message" // mensagem nova (recebida ou enviada por outro aparelho)
	EvChat    = "chat"    // chat mudou (nome, não lidas, última mensagem)
	EvStatus  = "status"  // conectou, desconectou, sincronizando
	EvReload  = "reload"  // muita coisa mudou (history sync): recarregue a lista
)

// Request vai da TUI para o daemon.
type Request struct {
	ID     int64  `json:"id"`
	Op     string `json:"op"`
	Chat   string `json:"chat,omitempty"`
	Text   string `json:"text,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Before int64  `json:"before,omitempty"` // unix ms; 0 = mais recentes
}

// Frame vai do daemon para a TUI. Resposta tem ID; evento tem Event.
type Frame struct {
	ID    int64           `json:"id,omitempty"`
	Event string          `json:"event,omitempty"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Chat é uma linha da lista de conversas.
type Chat struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	IsGroup  bool   `json:"is_group"`
	LastTS   int64  `json:"last_ts"` // unix ms
	LastText string `json:"last_text"`
	Unread   int    `json:"unread"`
}

// Message é uma mensagem já reduzida ao que a TUI mostra.
type Message struct {
	ID         string `json:"id"`
	Chat       string `json:"chat"`
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name"`
	FromMe     bool   `json:"from_me"`
	TS         int64  `json:"ts"`   // unix ms
	Kind       string `json:"kind"` // text, image, audio, video, document, sticker, other
	Text       string `json:"text"`
}

// Status descreve a conexão com o WhatsApp.
type Status struct {
	State string `json:"state"` // connecting, online, offline, logged_out
	Me    string `json:"me,omitempty"`
}
