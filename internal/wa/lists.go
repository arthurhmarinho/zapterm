package wa

import (
	"context"
	"errors"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Listas do celular (Favoritas e personalizadas; numa conta comercial,
// as etiquetas): chegam como etiquetas pelo app state.

// shownListTypes: tipos que viram filtro. Os outros (não lidas, grupos,
// arquivadas…) o zapterm já tem, ou são internos do WhatsApp.
var shownListTypes = []int{
	int(waSyncAction.LabelEditAction_NONE),
	int(waSyncAction.LabelEditAction_FAVORITES),
	int(waSyncAction.LabelEditAction_PREDEFINED),
	int(waSyncAction.LabelEditAction_CUSTOM),
}

func (d *Daemon) onLabelEdit(e *events.LabelEdit) {
	a := e.Action
	if err := d.st.PutLabel(e.LabelID, a.GetName(), int(a.GetType()), int(a.GetOrderIndex()), a.GetDeleted()); err != nil {
		d.log.Warn("lista", "err", err)
		return
	}
	if !e.FromFullSync {
		d.srv.Broadcast(ipc.EvReload, nil)
	}
}

func (d *Daemon) onLabelChat(ctx context.Context, e *events.LabelAssociationChat) {
	chat := d.normalize(ctx, e.JID).String()
	if err := d.st.SetChatLabel(e.LabelID, chat, e.Action.GetLabeled()); err != nil {
		d.log.Warn("conversa na lista", "err", err)
		return
	}
	if !e.FromFullSync {
		d.pushChat(chat)
	}
}

// lists: as listas que viram filtro; Favoritas sem nome ganha um.
func (d *Daemon) lists() ([]ipc.List, error) {
	out, err := d.st.Labels(shownListTypes)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Name == "" && out[i].Type == int(waSyncAction.LabelEditAction_FAVORITES) {
			out[i].Name = "Favoritas"
		}
	}
	return out, nil
}

// setChatList põe ou tira a conversa da lista (sincroniza com o celular).
func (d *Daemon) setChatList(ctx context.Context, chat string, target types.JID, label string, on bool) error {
	if label == "" {
		return errors.New("lista vazia")
	}
	if err := d.cli.SendAppState(ctx, appstate.BuildLabelChat(target, label, on)); err != nil {
		return err
	}
	if err := d.st.SetChatLabel(label, chat, on); err != nil {
		return err
	}
	d.pushChat(chat)
	return nil
}
