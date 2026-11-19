package wa

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
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
		v := m.GetVideoMessage()
		label := "vídeo"
		if s := v.GetSeconds(); s > 0 {
			label += fmt.Sprintf(" %d:%02d", s/60, s%60)
		}
		return "video", tag(label, v.GetCaption())
	case m.GetAudioMessage() != nil:
		a := m.GetAudioMessage()
		dur := ""
		if s := a.GetSeconds(); s > 0 {
			dur = fmt.Sprintf(" %d:%02d", s/60, s%60)
		}
		if a.GetPTT() {
			return "audio", "[áudio" + dur + "]"
		}
		return "audio", "[arquivo de áudio" + dur + "]"
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

// quoteOf extrai a citação de uma resposta. Toda mensagem com conteúdo pode
// carregar ContextInfo; o WhatsApp só preenche StanzaID quando é resposta.
func quoteOf(m *waE2E.Message) *ipc.Quote {
	ci := contextInfo(m)
	if ci.GetStanzaID() == "" {
		return nil
	}
	_, text := describe(ci.GetQuotedMessage())
	return &ipc.Quote{ID: ci.GetStanzaID(), Sender: ci.GetParticipant(), Text: text}
}

// contextInfo devolve o ContextInfo do conteúdo da mensagem (citação,
// menções, encaminhada), seja qual for o tipo.
func contextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	var ci *waE2E.ContextInfo
	switch {
	case m.GetExtendedTextMessage() != nil:
		ci = m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		ci = m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		ci = m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		ci = m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		ci = m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		ci = m.GetStickerMessage().GetContextInfo()
	}
	return ci
}

func tag(label, caption string) string {
	caption = strings.TrimSpace(caption)
	if caption == "" {
		return "[" + label + "]"
	}
	return "[" + label + "] " + caption
}
