package wa

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Pareamento pela TUI. Sem sessão e sem terminal para o QR, o daemon sobe
// no estado "pairing": o socket funciona (as TUIs leem o banco), o QR atual
// e o código de 8 letras vão em Status.Pair e a TUI desenha a tela de login.

// pairRetry: espera entre uma rodada de QRs e a seguinte.
const pairRetry = 2 * time.Second

// paired diz se há sessão (o aparelho tem JID). Sem ela os repositórios do
// whatsmeow (contatos, LIDs, configurações de chat) nem existem: nada que
// passe por d.cli.Store.Contacts e afins pode rodar.
func (d *Daemon) paired() bool { return d.cli.Store.ID != nil && d.cli.Store.Initialized }

// claimOwner registra a conta da sessão como dona do banco de mensagens.
// Se o banco era de outra conta, as conversas dela vão para um arquivo à
// parte e este recomeça vazio, para duas contas não se misturarem.
func (d *Daemon) claimOwner(user string) {
	moved, err := d.st.SwitchOwner(user)
	switch {
	case err != nil:
		d.log.Error("dono do banco de mensagens", "err", err)
	case moved != "":
		d.log.Warn("conta diferente da anterior: conversas antigas guardadas à parte", "arquivo", moved)
		d.srv.Broadcast(ipc.EvReload, nil)
		d.scheduleUnreadPush()
	}
}

// setPair publica o estado do login. Troca o ponteiro (nunca muda o que já
// foi publicado): quem leu o status antes pode estar serializando a cópia.
func (d *Daemon) setPair(qr, code, errText string) {
	d.mu.Lock()
	d.status.State = "pairing"
	d.status.Pair = &ipc.PairInfo{QR: qr, Code: code, Err: errText}
	s := d.status
	d.mu.Unlock()
	d.srv.Broadcast(ipc.EvStatus, s)
}

func (d *Daemon) pairInfo() ipc.PairInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.Pair == nil {
		return ipc.PairInfo{}
	}
	return *d.status.Pair
}

// pairLoop roda QRs até parear: quando uma rodada expira, recomeça. Só
// para no sucesso ou quando ctx acaba.
func (d *Daemon) pairLoop(ctx context.Context) {
	for ctx.Err() == nil {
		ok, err := d.pairRound(ctx)
		if ok {
			return
		}
		note := d.pairNote
		if err != nil {
			d.log.Warn("pareamento", "err", err)
			note = err.Error()
		}
		d.cli.Disconnect() // GetQRChannel só aceita cliente desconectado
		d.setPair("", "", note)
		select {
		case <-ctx.Done():
			return
		case <-time.After(pairRetry):
		}
	}
}

// pairRound conecta e acompanha os QRs de uma rodada (~3 min). Devolve
// true se pareou.
func (d *Daemon) pairRound(ctx context.Context) (bool, error) {
	qrs, err := d.cli.GetQRChannel(ctx)
	if err != nil {
		return false, err
	}
	if err := d.cli.Connect(); err != nil {
		return false, fmt.Errorf("sem conexão com o WhatsApp: %w", err)
	}
	for item := range qrs {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			// O código de 8 letras vale enquanto dura a conexão: segue na tela.
			d.setPair(item.Code, d.pairInfo().Code, d.pairNote)
		case whatsmeow.QRChannelSuccess.Event:
			d.log.Info("pareado pela TUI")
			d.setStatus("connecting") // o Connected vem em seguida
			return true, nil
		case whatsmeow.QRChannelTimeout.Event:
			return false, nil
		default:
			msg := item.Event
			if item.Error != nil {
				msg = item.Error.Error()
			}
			return false, fmt.Errorf("o pareamento falhou: %s", msg)
		}
	}
	return false, nil
}

// pairPhone pede o código de 8 letras para o número (só dígitos, com DDI).
// O WhatsApp só aceita depois do primeiro QR, com a conexão aberta.
func (d *Daemon) pairPhone(ctx context.Context, phone string) (string, error) {
	if d.paired() {
		return "", errors.New("este computador já está conectado")
	}
	phone = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if cur := d.pairInfo(); cur.QR == "" || !d.cli.IsConnected() {
		return "", errors.New("ainda conectando ao WhatsApp; tente de novo em alguns segundos")
	}
	code, err := d.cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	switch {
	case errors.Is(err, whatsmeow.ErrPhoneNumberTooShort):
		return "", errors.New("número curto demais: use o DDI e o DDD, só dígitos")
	case errors.Is(err, whatsmeow.ErrPhoneNumberIsNotInternational):
		return "", errors.New("use o número completo com o DDI (55 no Brasil), sem zero na frente")
	case err != nil:
		return "", fmt.Errorf("pedindo o código: %w", err)
	}
	cur := d.pairInfo()
	d.setPair(cur.QR, code, "")
	return code, nil
}

// handleUnpaired atende o que dá para fazer sem sessão: ler o banco e
// parear. handled=false: a operação precisa da conexão ("não conectado").
func (d *Daemon) handleUnpaired(ctx context.Context, req ipc.Request) (data any, handled bool, err error) {
	switch req.Op {
	case ipc.OpStatus:
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.status, true, nil
	case ipc.OpPairPhone:
		pctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		code, err := d.pairPhone(pctx, req.Text)
		return code, true, err
	case ipc.OpChats:
		chats, err := d.st.Chats()
		if err != nil {
			return nil, true, err
		}
		for i := range chats {
			c := &chats[i]
			if c.Name == "" {
				c.Name = c.JID
			}
			c.Kind = "dm"
			switch {
			case strings.HasSuffix(c.JID, "@g.us"):
				c.Kind = "group"
			case strings.HasSuffix(c.JID, "@newsletter"):
				c.Kind = "channel"
			}
		}
		return chats, true, nil
	case ipc.OpMessages:
		var msgs []ipc.Message
		var err error
		if req.After > 0 {
			msgs, err = d.st.MessagesAfter(req.Chat, req.Limit, req.After)
		} else {
			msgs, err = d.st.Messages(req.Chat, req.Limit, req.Before)
		}
		return msgs, true, err
	case ipc.OpLists:
		return []ipc.List{}, true, nil
	case ipc.OpUnread:
		return ipc.Unread{Top: []ipc.UnreadChat{}}, true, nil
	case ipc.OpViewing, ipc.OpAvatar:
		return nil, true, nil
	case ipc.OpShowUI:
		return nil, true, d.showUI()
	}
	return nil, false, errors.New("não conectado")
}
