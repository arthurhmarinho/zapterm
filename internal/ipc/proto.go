// Package ipc define o protocolo entre o daemon e a TUI: JSON, uma mensagem
// por linha, num socket Unix. Dá para depurar com:
//
//	socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/zapterm.sock
//	{"id":1,"op":"chats"}
package ipc

import "encoding/json"

// Operações aceitas pelo daemon.
const (
	OpStatus      = "status"       // estado da conexão
	OpChats       = "chats"        // lista de chats, mais recente primeiro
	OpMessages    = "messages"     // mensagens de um chat
	OpSend        = "send"         // envia texto
	OpRead        = "read"         // marca o chat como lido
	OpViewing     = "viewing"      // chat aberto numa TUI em foco ("" = nenhum); suprime notificação
	OpMedia       = "media"        // baixa a mídia da mensagem Msg (ou devolve do cache)
	OpAvatar      = "avatar"       // foto de perfil de Chat (pessoa ou grupo); null = sem foto
	OpSendImage   = "send_image"   // envia a imagem em Path com legenda Text
	OpStickers    = "stickers"     // favoritas do celular (Text "recent": as das conversas), para o seletor
	OpSendSticker = "send_sticker" // reenvia a figurinha da mensagem SrcChat/Msg em Chat
	OpSendAudio   = "send_audio"   // envia o OGG/Opus em Path como mensagem de voz
	OpSendFile    = "send_file"    // envia o arquivo em Path (foto, vídeo, áudio ou documento)
	OpReact       = "react"        // reage à mensagem Msg com o emoji em Text ("" remove)
	OpTyping      = "typing"       // Text: "composing", "recording" ou "paused" em Chat
	OpRevoke      = "revoke"       // apaga para todos a sua mensagem Msg
	OpMsgInfo     = "msg_info"     // dados da mensagem Msg: quem recebeu, leu, ouviu e reagiu
	OpSendAlbum   = "send_album"   // fotos e vídeos (Paths) como um álbum; legenda (Text) no primeiro
	OpSendPoll    = "send_poll"    // enquete: Text pergunta, Options opções, Seconds 1 = uma escolha só
	OpSendEvent   = "send_event"   // evento: Text nome, Before início (unix ms), Options [descrição, local]
	OpSendContact = "send_contact" // contato: Msg o JID da pessoa, Text o nome
	OpAmAdmin     = "am_admin"     // você é admin do grupo Chat? (bool)
	OpGroupByLink = "group_link"   // dados do grupo de um link de convite (Text); GroupPreview
	OpJoinLink    = "join_link"    // entra no grupo do link (Text); devolve o JID
	OpGroupMember = "group_member" // Text "promote|demote|remove" no participante Msg (JID cru) do grupo Chat
	OpInviteLink  = "invite_link"  // link de convite do grupo Chat (Text "reset" gera outro)
	OpPin         = "pin"          // fixa a mensagem Msg por Seconds (0 = desafixa)
	OpPins        = "pins"         // mensagens fixadas do Chat
	OpKeep        = "keep"         // salva (Text "1") ou deixa de salvar ("0") a temporária Msg na conversa
	OpStar        = "star"         // favorita (Text "1") ou desfavorita ("0") a mensagem Msg
	OpStarred     = "starred"      // mensagens favoritas de todas as conversas ([]SearchHit)
	OpDeleteMe    = "delete_me"    // apaga só para você a mensagem Msg (sincroniza com o celular)
	OpEdit        = "edit"         // troca o texto da sua mensagem Msg por Text
	OpForward     = "forward"      // encaminha SrcChat/Msg para Chat
	OpMembers     = "members"      // participantes de um grupo, para o "@"
	OpChatAction  = "chat_action"  // Text: pin, unpin, archive, unarchive, mute8h, mute1w, muteforever, unmute, read
	OpCheckNumber = "check_number" // Text: telefone com DDI; devolve o chat se tiver WhatsApp
	OpSearch      = "search"       // procura Text no texto das mensagens (sem acento/maiúscula)
	OpProfile     = "profile"      // perfil do chat: foto grande, recado/descrição, participantes, mídia
	OpGallery     = "gallery"      // mídia, docs, links ou áudios do chat (Text = aba), em páginas
	OpPollVote    = "poll_vote"    // vota na enquete Msg do Chat com Options (vazio tira o voto)
	OpRefetch     = "refetch"      // pede ao celular o histórico anterior a Before (recupera mídia antiga)
	OpUnread      = "unread"       // resumo de não lidas (para a barra)
	OpShowUI      = "show_ui"      // traz a TUI para frente, ou abre uma
	OpRejectCall  = "reject_call"  // recusa a chamada tocando da linha Msg no Chat
	OpNewGroup    = "new_group"    // cria o grupo Text com os participantes Options (JIDs); devolve o JID
	OpGroupSet    = "group_set"    // muda Text (name, topic, photo, announce, locked, approval, addmode, approve, reject) do grupo Chat para Options[0]
	OpGroupReqs   = "group_reqs"   // pedidos para entrar no grupo Chat ([]ProfileMember)
	OpTranscribe  = "transcribe"   // transcreve o áudio Msg do Chat com o whisper.cpp; devolve o texto
	OpChannelDir  = "channel_dir"  // canais recomendados, filtrados por Text (ou o canal do link em Text) ([]Channel)
	OpFollow      = "follow"       // segue o canal Chat (Text = nome, para a lista)
	OpNewChannel  = "new_channel"  // cria o canal Text com a descrição Options[0]; devolve o JID
	OpStatuses    = "statuses"     // status das últimas 24 h ([]Message, do mais antigo ao mais novo)
	OpViewStatus  = "view_status"  // marca como vistos os status Options de Chat (quem postou)
	OpPostStatus  = "post_status"  // publica o status de texto Text
	OpEventRSVP   = "event_rsvp"   // responde ao evento Msg do Chat: Text going, maybe ou not_going
	OpSubGroups   = "sub_groups"   // grupos da comunidade do grupo Chat ([]SubGroup)
	OpLists       = "lists"        // listas do celular ([]List)
	OpMe          = "me"           // seu nome, número, recado e privacidade (Me)
	OpSetMe       = "set_me"       // Text "name", "about" ou "privacy:<tipo>"; Options[0] o valor novo
	OpPairPhone   = "pair_phone"   // sem sessão: pede o código de 8 letras para o número Text (só dígitos); devolve o código
)

// Eventos empurrados pelo daemon sem pedido.
const (
	EvMessage  = "message"  // mensagem nova (recebida ou enviada por outro aparelho)
	EvChat     = "chat"     // chat mudou (nome, não lidas, última mensagem)
	EvStatus   = "status"   // conectou, desconectou, sincronizando
	EvReload   = "reload"   // muita coisa mudou (history sync): recarregue a lista
	EvUpdate   = "update"   // mensagem existente mudou (editada, apagada, reação, recibo)
	EvPresence = "presence" // digitando, gravando, online, visto por último
	EvOpen     = "open"     // abra este chat (clique na notificação)
	EvPins     = "pins"     // mudaram as fixadas do chat (Data: o JID)
	EvCleared  = "cleared"  // conversa limpa (Data: o JID): recarregue as mensagens
)

// Request vai da TUI para o daemon.
type Request struct {
	ID     int64  `json:"id"`
	Op     string `json:"op"`
	Chat   string `json:"chat,omitempty"`
	Text   string `json:"text,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Before int64  `json:"before,omitempty"` // unix ms; 0 = mais recentes
	// After pede as mensagens posteriores (OpMessages), em vez das anteriores.
	After int64 `json:"after,omitempty"`
	// ReplyTo é o ID da mensagem respondida. "conversa|id" cita uma
	// mensagem de outra conversa (responder em privado a alguém do grupo).
	ReplyTo string `json:"reply_to,omitempty"`
	// Msg é o ID da mensagem alvo (OpMedia).
	Msg string `json:"msg,omitempty"`
	// Path é o arquivo local a enviar (OpSendImage).
	Path string `json:"path,omitempty"`
	// SrcChat é o chat da mensagem de origem (OpSendSticker).
	SrcChat string `json:"src_chat,omitempty"`
	// Mentions são os JIDs marcados com "@" no texto (OpSend).
	Mentions []string `json:"mentions,omitempty"`
	// Seconds é a duração do áudio gravado (OpSendAudio).
	Seconds int `json:"seconds,omitempty"`
	// Options são as opções escolhidas (OpPollVote) ou de envio (hd…).
	Options []string `json:"options,omitempty"`
	// Paths são os arquivos de um álbum (OpSendAlbum).
	Paths []string `json:"paths,omitempty"`
}

// Sticker é uma figurinha do seletor: a mensagem de onde ela veio e a
// prévia já baixada.
type Sticker struct {
	Chat    string `json:"chat"`
	ID      string `json:"id"`
	Preview string `json:"preview"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	// Quadros, se for animada (como em Media).
	Frames []string `json:"frames,omitempty"`
	Delays []int    `json:"delays,omitempty"`
}

// StickerPage é a resposta de OpStickers: uma página e de onde continuar
// (Next < 0: acabou).
type StickerPage struct {
	Stickers []Sticker `json:"stickers"`
	Next     int       `json:"next"`
}

// Media é um arquivo de mídia baixado. Preview (PNG reduzido) só existe
// para imagem e figurinha.
type Media struct {
	Path    string `json:"path"`
	Preview string `json:"preview,omitempty"`
	Width   int    `json:"width,omitempty"` // da prévia, em pixels
	Height  int    `json:"height,omitempty"`
	Mime    string `json:"mime,omitempty"`
	// Figurinha animada: quadros PNG (Preview é o primeiro) e a duração
	// de cada um em ms.
	Frames []string `json:"frames,omitempty"`
	Delays []int    `json:"delays,omitempty"`
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
	Kind     string `json:"kind"` // dm, group, channel
	Archived bool   `json:"archived"`
	Blocked  bool   `json:"blocked,omitempty"` // contato bloqueado
	// Restricted: o dono limitou aparelhos conectados nesta conversa.
	Restricted bool   `json:"restricted,omitempty"`
	Muted      bool   `json:"muted"`
	Pinned     bool   `json:"pinned"`
	IsGroup    bool   `json:"is_group"`
	LastTS     int64  `json:"last_ts"` // unix ms
	LastText   string `json:"last_text"`
	Unread     int    `json:"unread"`
	// LastStatus: situação da última mensagem, se for sua (ver Message.Status);
	// -1 se a última não é sua.
	LastStatus int `json:"last_status"`
	// Ephemeral: prazo das mensagens temporárias, em segundos (0 = desligado).
	Ephemeral int `json:"ephemeral,omitempty"`
	// Lists: IDs das listas do celular em que a conversa está (List.ID).
	Lists []string `json:"lists,omitempty"`
	// Community: comunidade do grupo (nome e JID); CommunityAnnounce: é o
	// grupo de avisos dela.
	Community         string `json:"community,omitempty"`
	CommunityJID      string `json:"community_jid,omitempty"`
	CommunityAnnounce bool   `json:"community_announce,omitempty"`
}

// Channel é um canal do diretório.
type Channel struct {
	JID         string `json:"jid"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Subscribers int    `json:"subscribers,omitempty"`
	Verified    bool   `json:"verified,omitempty"`
	Following   bool   `json:"following,omitempty"`
}

// SubGroup é um grupo de uma comunidade; Joined: você participa.
type SubGroup struct {
	JID      string `json:"jid"`
	Name     string `json:"name"`
	Announce bool   `json:"announce,omitempty"` // grupo de avisos
	Joined   bool   `json:"joined,omitempty"`
}

// List é uma lista do celular: Favoritas (Type 3), personalizada (5) ou
// etiqueta de empresa (0 e 4).
type List struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Count int    `json:"count"`
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
	Quote      *Quote `json:"quote,omitempty"` // mensagem citada, se for resposta
	Media      *Media `json:"media,omitempty"` // mídia já baixada, se houver
	// Thumb é a miniatura que vem dentro da mensagem (foto ou vídeo ainda
	// não baixados): borrada, mas aparece sem rede.
	Thumb *Media `json:"thumb,omitempty"`
	// HasRaw diz se a mensagem guarda o protobuf original, ou seja, se a
	// mídia dela pode ser baixada.
	HasRaw bool `json:"has_raw,omitempty"`
	// Edited: o autor editou depois de enviar. Kind "revoked" = apagada.
	Edited bool `json:"edited,omitempty"`
	// Status das suas mensagens: 0 desconhecido, 1 enviada (✓), 2 entregue
	// (✓✓), 3 lida, 4 ouvida (áudio).
	Status    int        `json:"status,omitempty"`
	Reactions []Reaction `json:"reactions,omitempty"`
	// MentionsMe: a mensagem marca você (o texto já vem com os nomes no
	// lugar dos números, entre U+2068 e U+2069).
	MentionsMe bool `json:"mentions_me,omitempty"`
	// Expires: quando a mensagem temporária vence (unix ms; 0 = não vence).
	Expires int64 `json:"expires,omitempty"`
	// Kept: temporária salva na conversa (não some).
	Kept bool `json:"kept,omitempty"`
	// Starred: favorita (estrela), sincronizada com o celular.
	Starred bool `json:"starred,omitempty"`
	// Forwarded: a mensagem foi encaminhada (selo "Encaminhada").
	Forwarded bool `json:"forwarded,omitempty"`
	// Transcript: transcrição local do áudio (whisper.cpp), se já feita.
	Transcript string `json:"transcript,omitempty"`
	// Card: contato, localização, evento, enquete ou prévia de link,
	// montado a partir do protobuf original.
	Card *Card `json:"card,omitempty"`
}

// Card é o conteúdo estruturado de uma mensagem que não é só texto ou
// mídia. Type: contact, location, event, poll, link.
type Card struct {
	Type  string   `json:"type"`
	Title string   `json:"title"`
	Lines []string `json:"lines,omitempty"`
	URL   string   `json:"url,omitempty"`  // ctrl+o abre (mapa, link, chamada)
	Chat  string   `json:"chat,omitempty"` // contato: conversa que ctrl+o abre
	Thumb *Media   `json:"thumb,omitempty"`
	// Enquete: opções com a contagem, se é de várias escolhas e quantas
	// pessoas votaram.
	Options []CardOption `json:"options,omitempty"`
	Multi   bool         `json:"multi,omitempty"`
	Voters  int          `json:"voters,omitempty"`
}

// MsgInfo são os dados de uma mensagem (menu → "Dados da mensagem").
type MsgInfo struct {
	Read      []Who `json:"read,omitempty"`
	Played    []Who `json:"played,omitempty"`
	Delivered []Who `json:"delivered,omitempty"` // recebeu e ainda não leu
	Reactions []Who `json:"reactions,omitempty"`
	FromMe    bool  `json:"from_me,omitempty"`
}

// Who: uma pessoa e quando (unix ms) ou com que emoji.
type Who struct {
	Name  string `json:"name"`
	TS    int64  `json:"ts,omitempty"`
	Emoji string `json:"emoji,omitempty"`
}

// GroupPreview: o grupo de um link de convite, antes de entrar.
type GroupPreview struct {
	Name    string `json:"name"`
	Topic   string `json:"topic,omitempty"`
	Members int    `json:"members"`
	JID     string `json:"jid"`
	Joined  bool   `json:"joined,omitempty"` // você já está nele
}

type CardOption struct {
	Name  string `json:"name"`
	Votes int    `json:"votes"`
	Mine  bool   `json:"mine,omitempty"`
}

// Presence é o estado de alguém num chat. Typing/Recording valem por
// alguns segundos; Online e LastSeen só vêm de conversas individuais.
type Presence struct {
	Chat      string `json:"chat"`
	Who       string `json:"who,omitempty"` // em grupo, quem está digitando
	Typing    bool   `json:"typing,omitempty"`
	Recording bool   `json:"recording,omitempty"`
	Online    *bool  `json:"online,omitempty"` // nil = sem informação nova
	LastSeen  int64  `json:"last_seen,omitempty"`
}

// Profile é a tela de perfil de um contato ou grupo.
type Profile struct {
	JID         string          `json:"jid"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Phone       string          `json:"phone,omitempty"`
	Business    string          `json:"business,omitempty"` // nome verificado de empresa
	About       string          `json:"about,omitempty"`    // recado (contato) ou descrição (grupo)
	Photo       *Media          `json:"photo,omitempty"`
	Created     int64           `json:"created,omitempty"`
	MemberCount int             `json:"member_count,omitempty"`
	Members     []ProfileMember `json:"members,omitempty"`
	Media       []Message       `json:"media"`
	Counts      map[string]int  `json:"counts,omitempty"` // itens em cada aba da galeria
	Note        string          `json:"note,omitempty"`   // aviso (ex.: saiu do grupo)
	IAmAdmin    bool            `json:"i_am_admin,omitempty"`
	Group       *GroupSettings  `json:"group,omitempty"`
}

// GroupSettings: as permissões do grupo, para o menu de admin.
type GroupSettings struct {
	Announce bool `json:"announce,omitempty"`  // só admins mandam mensagem
	Locked   bool `json:"locked,omitempty"`    // só admins mudam nome, descrição e foto
	Approval bool `json:"approval,omitempty"`  // admin aprova quem entra pelo link
	AdminAdd bool `json:"admin_add,omitempty"` // só admins adicionam participantes
}

// Abas da galeria (Request.Text em OpGallery).
var GalleryTabs = []string{"fotos", "videos", "docs", "links", "audios"}

// GalleryPage é uma página da galeria: itens do mais novo ao mais antigo,
// quantos há em cada aba e se ainda há mais (Before = TS do último).
type GalleryPage struct {
	Items  []Message      `json:"items"`
	Counts map[string]int `json:"counts"`
	More   bool           `json:"more,omitempty"`
}

type ProfileMember struct {
	Name  string `json:"name"`
	Admin bool   `json:"admin,omitempty"`
	JID   string `json:"jid,omitempty"` // conversa individual com a pessoa
	Raw   string `json:"raw,omitempty"` // como o grupo a conhece (para remover/promover)
}

// Unread é o resumo de não lidas mostrado na barra.
type Unread struct {
	Chats    int          `json:"chats"`    // conversas com não lidas (sem arquivadas e silenciadas)
	Messages int          `json:"messages"` // total de mensagens não lidas nelas
	Top      []UnreadChat `json:"top"`
}

type UnreadChat struct {
	Name   string `json:"name"`
	Unread int    `json:"unread"`
}

// Open pede à TUI que abra um chat.
type Open struct {
	Chat string `json:"chat"`
}

// SearchHit é um resultado da busca no texto das mensagens.
type SearchHit struct {
	Chat    Chat    `json:"chat"`
	Message Message `json:"message"`
}

// Member é um participante de grupo, para o autocompletar do "@".
type Member struct {
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// Reaction agrupa as reações de um emoji numa mensagem.
type Reaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine,omitempty"`
}

// Quote é o trecho citado de uma resposta.
type Quote struct {
	ID         string `json:"id"`
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name"`
	Text       string `json:"text"`
}

// Me é a tela "Eu": seu perfil e a privacidade da conta. Privacy: tipo
// (last, online, profile, status, readreceipts, groupadd, calladd) → valor
// do whatsmeow (all, contacts, none, match_last_seen, known).
type Me struct {
	JID     string            `json:"jid"`
	Phone   string            `json:"phone"`
	Name    string            `json:"name"`
	About   string            `json:"about"`
	Privacy map[string]string `json:"privacy"`
}

// Status descreve a conexão com o WhatsApp.
type Status struct {
	State string `json:"state"` // connecting, pairing, online, offline, logged_out
	Me    string `json:"me,omitempty"`
	Name  string `json:"name,omitempty"` // seu nome de perfil
	// Pair: o que a tela de login mostra (só com State "pairing").
	Pair *PairInfo `json:"pair,omitempty"`
}

// PairInfo é o estado do pareamento: o QR atual (muda a cada ~20 s), o
// código de 8 letras, se foi pedido pelo número, e o último erro ou aviso.
type PairInfo struct {
	QR   string `json:"qr,omitempty"`
	Code string `json:"code,omitempty"`
	Err  string `json:"err,omitempty"`
}
