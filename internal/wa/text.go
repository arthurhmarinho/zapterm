package wa

import (
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// describe reduz uma mensagem do WhatsApp a (tipo, texto). Mídia vira um
// marcador com a legenda, já que o MVP não baixa arquivos. Mensagens de
// protocolo (reação, edição, apagar) devolvem kind "" e são ignoradas.
func describe(m *waE2E.Message) (kind, text string) {
	if m == nil {
		return "", ""
	}
	switch {
	case m.GetConversation() != "":
		return "text", m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		return "text", m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		return "image", tag("imagem", m.GetImageMessage().GetCaption())
	case m.GetVideoMessage() != nil:
		return "video", tag("vídeo", m.GetVideoMessage().GetCaption())
	case m.GetAudioMessage() != nil:
		if m.GetAudioMessage().GetPTT() {
			return "audio", "[áudio]"
		}
		return "audio", "[arquivo de áudio]"
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		return "document", tag("documento: "+d.GetFileName(), d.GetCaption())
	case m.GetStickerMessage() != nil:
		return "sticker", "[figurinha]"
	case m.GetContactMessage() != nil:
		return "other", "[contato: " + m.GetContactMessage().GetDisplayName() + "]"
	case m.GetLocationMessage() != nil:
		return "other", "[localização]"
	case m.GetPollCreationMessage() != nil:
		return "other", "[enquete: " + m.GetPollCreationMessage().GetName() + "]"
	case m.GetPollCreationMessageV3() != nil:
		return "other", "[enquete: " + m.GetPollCreationMessageV3().GetName() + "]"
	}
	return "", ""
}

func tag(label, caption string) string {
	caption = strings.TrimSpace(caption)
	if caption == "" {
		return "[" + label + "]"
	}
	return "[" + label + "] " + caption
}
