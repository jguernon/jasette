package main

// « Quoi de neuf ? » sur Telegram (PROTOCOLE.md, §5.12) : la bio du profil
// (account.updateProfile, champ about, 70 caractères). Avant la première
// note, le pont lit et garde l'ancienne bio (users.getFullUser de moi-même) ;
// l'effacement la remet.

import (
	"context"
	"errors"
	"strings"

	"github.com/gotd/td/tg"
)

func (p *Pont) estPret() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready && p.authed
}

func (p *Pont) ecrireBio(ctx context.Context, texte string) error {
	api := p.tgAPI()
	if api == nil {
		return erreurNote("Telegram n'est pas connecté.")
	}
	req := &tg.AccountUpdateProfileRequest{}
	req.SetAbout(texte)
	_, err := api.AccountUpdateProfile(ctx, req)
	return err
}

func erreurTG(verbe string, err error) error {
	var en erreurNote
	if errors.As(err, &en) {
		return en
	}
	if strings.Contains(err.Error(), "ABOUT_TOO_LONG") {
		return erreurNote("La bio Telegram est trop longue (70 caractères au plus).")
	}
	return erreurNote("La bio Telegram n'a pas pu être " + verbe + " : " + err.Error())
}

func (p *Pont) publierNoteTG(ctx context.Context, texte string, _ int, e *etatNote) (string, error) {
	if e.Ancienne == nil && e.Text == "" {
		api := p.tgAPI()
		if api == nil {
			return "", erreurNote("Telegram n'est pas connecté.")
		}
		plein, err := api.UsersGetFullUser(ctx, &tg.InputUserSelf{})
		if err != nil {
			return "", erreurNote("La bio Telegram n'a pas pu être lue : rien n'a été changé.")
		}
		ancienne := plein.FullUser.About
		e.Ancienne = &ancienne
	}
	if err := p.ecrireBio(ctx, texte); err != nil {
		return "", erreurTG("publiée", err)
	}
	return "", nil
}

func (p *Pont) effacerNoteTG(ctx context.Context, e *etatNote) error {
	ancienne := ""
	if e.Ancienne != nil {
		ancienne = *e.Ancienne
	}
	if err := p.ecrireBio(ctx, ancienne); err != nil {
		return erreurTG("remise", err)
	}
	return nil
}

func nouvellesNotesTG(p *Pont) *notesPont {
	return nouvellesNotes(opsNote{
		nom: "Telegram", limite: 70, parHeure: 20,
		publier: p.publierNoteTG, effacer: p.effacerNoteTG,
	})
}
