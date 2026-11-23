package wa

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"github.com/arthurhmarinho/zapterm/internal/ipc"
)

// Tela "Eu": seu nome de perfil, recado e a privacidade da conta, como em
// Configurações → Privacidade no celular.

// privacyValues: valores aceitos por cada configuração, na ordem do menu.
var privacyValues = map[string][]types.PrivacySetting{
	"last":         {types.PrivacySettingAll, types.PrivacySettingContacts, types.PrivacySettingNone},
	"online":       {types.PrivacySettingAll, types.PrivacySettingMatchLastSeen},
	"profile":      {types.PrivacySettingAll, types.PrivacySettingContacts, types.PrivacySettingNone},
	"status":       {types.PrivacySettingContacts, types.PrivacySettingNone},
	"readreceipts": {types.PrivacySettingAll, types.PrivacySettingNone},
	"groupadd":     {types.PrivacySettingAll, types.PrivacySettingContacts},
	"calladd":      {types.PrivacySettingAll, types.PrivacySettingKnown},
}

func privacyMap(p types.PrivacySettings) map[string]string {
	return map[string]string{
		"last": string(p.LastSeen), "online": string(p.Online), "profile": string(p.Profile),
		"status": string(p.Status), "readreceipts": string(p.ReadReceipts),
		"groupadd": string(p.GroupAdd), "calladd": string(p.CallAdd),
	}
}

// meScreen devolve seu nome, número, recado e privacidade.
func (d *Daemon) meScreen(ctx context.Context) (*ipc.Me, error) {
	id := d.cli.Store.ID
	if id == nil {
		return nil, errors.New("não conectado")
	}
	own := id.ToNonAD()
	me := &ipc.Me{JID: own.String(), Phone: "+" + own.User, Name: d.cli.Store.PushName}
	// Recado, foto e privacidade são três pedidos independentes ao servidor.
	var wg sync.WaitGroup
	var privErr error
	wg.Add(3)
	go func() {
		defer wg.Done()
		if info, err := d.cli.GetUserInfo(ctx, []types.JID{own}); err == nil {
			me.About = info[own].Status
		}
	}()
	go func() {
		defer wg.Done()
		photo, err := d.fullPhoto(ctx, own)
		switch {
		case err == nil:
			me.Photo = photo
		case errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
			// sem foto: a tela desenha as iniciais
		default:
			d.log.Warn("sua foto de perfil", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		var p *types.PrivacySettings
		if p, privErr = d.cli.TryFetchPrivacySettings(ctx, true); privErr == nil {
			me.Privacy = privacyMap(*p)
		}
	}()
	wg.Wait()
	if privErr != nil {
		return nil, fmt.Errorf("privacidade: %w", privErr)
	}
	return me, nil
}

// setMe troca o nome ("name"), o recado ("about") ou uma configuração de
// privacidade ("privacy:last" etc.).
func (d *Daemon) setMe(ctx context.Context, key, value string) error {
	switch {
	case key == "name":
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > 25 {
			return errors.New("o nome precisa ter de 1 a 25 letras")
		}
		if err := d.cli.SendAppState(ctx, appstate.BuildSettingPushName(value)); err != nil {
			return err
		}
		d.cli.Store.PushName = value
		if err := d.cli.Store.Save(ctx); err != nil {
			d.log.Warn("guardar nome", "err", err)
		}
		d.mu.Lock()
		state := d.status.State
		d.mu.Unlock()
		d.setStatus(state) // avisa as TUIs do nome novo
		return nil
	case key == "about":
		value = strings.TrimSpace(value)
		if len([]rune(value)) > 139 {
			return errors.New("o recado aceita até 139 letras")
		}
		return d.cli.SetStatusMessage(ctx, types.SetStatusInput{Text: &value})
	case strings.HasPrefix(key, "privacy:"):
		name := strings.TrimPrefix(key, "privacy:")
		vals, ok := privacyValues[name]
		if !ok {
			return fmt.Errorf("configuração desconhecida: %s", name)
		}
		v := types.PrivacySetting(value)
		if !slices.Contains(vals, v) {
			return fmt.Errorf("valor inválido para %s: %s", name, value)
		}
		_, err := d.cli.SetPrivacySetting(ctx, types.PrivacySettingType(name), v)
		return err
	}
	return fmt.Errorf("campo desconhecido: %s", key)
}
