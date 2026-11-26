package main

// « Quoi de neuf ? » sur Bluesky (PROTOCOLE.md, §5.12) : la description du
// profil (app.bsky.actor.profile, rkey « self »), qui est une bio : le pont
// garde l'ancienne avant la première note et la remet à l'effacement.
// Lecture : com.atproto.repo.getRecord ; écriture :
// com.atproto.repo.putRecord avec swapRecord (le CID lu), en gardant tous
// les autres champs du profil (nom, avatar, bannière…) tels quels.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

const collectionProfil = "app.bsky.actor.profile"

func (p *Pont) estPret() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready && p.me != ""
}

type enregistrementProfil struct {
	CID   string         `json:"cid"`
	Value map[string]any `json:"value"`
}

// lireProfil : mon enregistrement de profil (absent : un profil vide).
func (p *Pont) lireProfil(ctx context.Context) (string, enregistrementProfil, error) {
	p.mu.Lock()
	moi := p.me
	p.mu.Unlock()
	var r enregistrementProfil
	if moi == "" {
		return "", r, erreurNote("Bluesky n'est pas connecté.")
	}
	err := p.a.call(ctx, "com.atproto.repo.getRecord", url.Values{
		"repo": {moi}, "collection": {collectionProfil}, "rkey": {"self"},
	}, nil, "", &r)
	if isXErr(err, "RecordNotFound") {
		return moi, enregistrementProfil{Value: map[string]any{"$type": collectionProfil}}, nil
	}
	if r.Value == nil && err == nil {
		r.Value = map[string]any{"$type": collectionProfil}
	}
	return moi, r, err
}

// profilAvecDescription : le profil avec une nouvelle description (vide : retirée).
func profilAvecDescription(v map[string]any, texte string) map[string]any {
	n := make(map[string]any, len(v)+1)
	for k, x := range v {
		n[k] = x
	}
	if _, ok := n["$type"]; !ok {
		n["$type"] = collectionProfil
	}
	if texte == "" {
		delete(n, "description")
	} else {
		n["description"] = texte
	}
	return n
}

func descriptionDe(v map[string]any) string {
	s, _ := v["description"].(string)
	return s
}

func (p *Pont) ecrireDescription(ctx context.Context, moi string, r enregistrementProfil, texte string) error {
	corps := map[string]any{
		"repo": moi, "collection": collectionProfil, "rkey": "self",
		"record": profilAvecDescription(r.Value, texte),
	}
	if r.CID != "" {
		corps["swapRecord"] = r.CID
	}
	var rep json.RawMessage
	return p.a.call(ctx, "com.atproto.repo.putRecord", nil, corps, "", &rep)
}

func erreurBS(verbe string, err error) error {
	var en erreurNote
	if errors.As(err, &en) {
		return en
	}
	if isXErr(err, "InvalidSwap") {
		return erreurNote("Le profil Bluesky a changé entre-temps : réessaie.")
	}
	return erreurNote("La description Bluesky n'a pas pu être " + verbe + " : " + err.Error())
}

func (p *Pont) publierNoteBS(ctx context.Context, texte string, _ int, e *etatNote) (string, error) {
	moi, r, err := p.lireProfil(ctx)
	if err != nil {
		return "", erreurBS("publiée", err)
	}
	if e.Ancienne == nil && e.Text == "" {
		ancienne := descriptionDe(r.Value)
		e.Ancienne = &ancienne
	}
	if err := p.ecrireDescription(ctx, moi, r, texte); err != nil {
		return "", erreurBS("publiée", err)
	}
	return "", nil
}

func (p *Pont) effacerNoteBS(ctx context.Context, e *etatNote) error {
	moi, r, err := p.lireProfil(ctx)
	if err != nil {
		return erreurBS("remise", err)
	}
	ancienne := ""
	if e.Ancienne != nil {
		ancienne = *e.Ancienne
	}
	if err := p.ecrireDescription(ctx, moi, r, ancienne); err != nil {
		return erreurBS("remise", err)
	}
	return nil
}

func nouvellesNotesBS(p *Pont) *notesPont {
	return nouvellesNotes(opsNote{
		nom: "Bluesky", limite: 256, parHeure: 20,
		publier: p.publierNoteBS, effacer: p.effacerNoteBS,
	})
}
