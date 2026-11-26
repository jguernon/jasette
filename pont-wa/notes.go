package main

// « Quoi de neuf ? » sur WhatsApp (PROTOCOLE.md, §5.12) : le texte « Infos »
// du profil (SetStatusMessage de whatsmeow), éphémère 24 h comme une note.
// Avant la première note, le pont lit et garde l'ancien texte (GetUserInfo
// de mon propre compte) ; l'effacement le remet.

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/util/jsontime"
	"go.mau.fi/whatsmeow/types"
)

const dureeNoteWA = 24 * time.Hour

func (p *Pont) estPret() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready && p.cli != nil
}

// infosActuelles : mon texte « Infos » actuel.
func (p *Pont) infosActuelles(ctx context.Context) (string, error) {
	cli := p.client()
	if cli == nil || cli.Store.ID == nil {
		return "", erreurNote("WhatsApp n'est pas connecté.")
	}
	moi := cli.Store.ID.ToNonAD()
	infos, err := cli.GetUserInfo(ctx, []types.JID{moi})
	if err != nil {
		return "", err
	}
	return infos[moi].Status, nil
}

func (p *Pont) ecrireInfos(ctx context.Context, texte string, duree time.Duration) error {
	cli := p.client()
	if cli == nil {
		return erreurNote("WhatsApp n'est pas connecté.")
	}
	return cli.SetStatusMessage(ctx, types.SetStatusInput{Text: &texte, Duration: jsontime.S(duree)})
}

func (p *Pont) publierNoteWA(ctx context.Context, texte string, _ int, e *etatNote) (string, error) {
	if e.Ancienne == nil && e.Text == "" {
		// La bio d'avant : si on ne peut pas la lire, on ne l'écrase pas.
		ancienne, err := p.infosActuelles(ctx)
		if err != nil {
			return "", erreurNote("Le texte « Infos » de WhatsApp n'a pas pu être lu : rien n'a été changé.")
		}
		e.Ancienne = &ancienne
	}
	if err := p.ecrireInfos(ctx, texte, dureeNoteWA); err != nil {
		var en erreurNote
		if errors.As(err, &en) {
			return "", en
		}
		return "", erreurNote("Le statut WhatsApp n'a pas pu être publié : " + err.Error())
	}
	return "", nil
}

func (p *Pont) effacerNoteWA(ctx context.Context, e *etatNote) error {
	cli := p.client()
	if cli == nil {
		return erreurNote("WhatsApp n'est pas connecté.")
	}
	// Le statut de 24 h n'est pas le texte « Infos » classique (GetUserInfo) :
	// on l'efface (texte nul) au lieu d'y réécrire l'ancien texte avec une
	// durée de 0, que WhatsApp refuse (400). Le texte « Infos » classique
	// n'a jamais été touché.
	err := cli.SetStatusMessage(ctx, types.SetStatusInput{Text: nil})
	if err != nil {
		log.Warn().Err(err).Msg("note WhatsApp : effacement (texte nul) refusé, essai avec un texte vide")
		vide := ""
		err = cli.SetStatusMessage(ctx, types.SetStatusInput{Text: &vide, Duration: jsontime.S(dureeNoteWA)})
	}
	if err != nil {
		log.Warn().Err(err).Msg("note WhatsApp : effacement refusé")
		return erreurNote("Le statut WhatsApp n'a pas pu être effacé : " + err.Error())
	}
	return nil
}

func nouvellesNotesWA(p *Pont) *notesPont {
	return nouvellesNotes(opsNote{
		nom: "WhatsApp", limite: 139, parHeure: 20, duree: dureeNoteWA,
		publier: p.publierNoteWA, effacer: p.effacerNoteWA,
	})
}
