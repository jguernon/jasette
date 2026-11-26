package main

// « Quoi de neuf ? » sur Discord (PROTOCOLE.md, §5.12) : le statut
// personnalisé (custom_status des réglages du compte, 128 caractères), sans
// échéance. Le pont garde l'ancien statut avant la première note (GET
// /users/@me/settings) et le remet à l'effacement. Une requête PATCH par
// publication, après l'espacement habituel du pont (p.rest).
//
// Le statut se change dans les réglages et non par la passerelle : le pont
// reste « invisible » (aucune présence envoyée).

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bwmarrin/discordgo"
)

func (p *Pont) estPret() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready && p.sess != nil && p.me != nil
}

// statutPerso : le custom_status des réglages, tel quel (JSON, "null" : aucun).
func analyserStatutPerso(corps []byte) (string, error) {
	var r struct {
		Custom json.RawMessage `json:"custom_status"`
	}
	if err := json.Unmarshal(corps, &r); err != nil {
		return "", err
	}
	if len(r.Custom) == 0 {
		return "null", nil
	}
	return string(r.Custom), nil
}

// corpsStatut : le PATCH des réglages pour un texte (vide : ancien statut,
// brut, ou rien).
func corpsStatut(texte string) map[string]any {
	return map[string]any{"custom_status": map[string]any{"text": texte, "expires_at": nil}}
}

func (p *Pont) patchStatut(ctx context.Context, corps any) error {
	s, err := p.api()
	if err != nil {
		return erreurNote("Discord n'est pas connecté.")
	}
	if err := p.rest.wait(ctx); err != nil {
		return err
	}
	_, err = s.RequestWithBucketID("PATCH", discordgo.EndpointUserSettings("@me"), corps, discordgo.EndpointUserSettings(""))
	return err
}

func erreurDiscord(verbe string, err error) error {
	var en erreurNote
	if errors.As(err, &en) {
		return en
	}
	return erreurNote("Le statut Discord n'a pas pu être " + verbe + " : " + err.Error())
}

func (p *Pont) publierNoteDiscord(ctx context.Context, texte string, _ int, e *etatNote) (string, error) {
	if e.Ancienne == nil && e.Text == "" {
		s, err := p.api()
		if err != nil {
			return "", erreurNote("Discord n'est pas connecté.")
		}
		if err := p.rest.wait(ctx); err != nil {
			return "", err
		}
		corps, err := s.RequestWithBucketID("GET", discordgo.EndpointUserSettings("@me"), nil, discordgo.EndpointUserSettings(""))
		ancien := ""
		if err == nil {
			ancien, err = analyserStatutPerso(corps)
		}
		if err != nil {
			return "", erreurNote("Le statut Discord actuel n'a pas pu être lu : rien n'a été changé.")
		}
		e.Ancienne = &ancien
	}
	if err := p.patchStatut(ctx, corpsStatut(texte)); err != nil {
		return "", erreurDiscord("publié", err)
	}
	return "", nil
}

func (p *Pont) effacerNoteDiscord(ctx context.Context, e *etatNote) error {
	var ancien json.RawMessage = []byte("null")
	if e.Ancienne != nil && *e.Ancienne != "" && json.Valid([]byte(*e.Ancienne)) {
		ancien = json.RawMessage(*e.Ancienne)
	}
	if err := p.patchStatut(ctx, map[string]any{"custom_status": ancien}); err != nil {
		return erreurDiscord("remis", err)
	}
	return nil
}

func nouvellesNotesDiscord(p *Pont) *notesPont {
	return nouvellesNotes(opsNote{
		nom: "Discord", limite: 128, parHeure: 6,
		publier: p.publierNoteDiscord, effacer: p.effacerNoteDiscord,
	})
}
