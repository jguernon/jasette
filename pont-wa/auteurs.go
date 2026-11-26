package main

// La photo des auteurs dans les groupes et la mosaïque des groupes sans
// image (PROTOCOLE.md §5.13).
//
//   - Auteurs : chaque message d'un autre dans un groupe porte « sender_id »
//     (son JID) ; sa photo vient de GetProfilePictureInfo, avec les règles de
//     photos_auteurs.go (paresseux, une fois par personne et par jour, au
//     plus 10 par minute et 200 par jour, refus gardé jusqu'au lendemain).
//     Les fichiers sont ceux des photos des fils à deux (avatars/) : une
//     photo déjà téléchargée pour un fil à deux sert sans requête.
//   - Mosaïque : un groupe sans image reçoit « avatars_groupe » avec ses
//     premiers membres et les photos déjà sur le disque, sans requête de plus.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
)

var errPasConnecte = errors.New("WhatsApp n'est pas connecté")

func (p *Pont) nouveauxAuteurs() *photosAuteurs {
	return nouvellesPhotosAuteurs(filepath.Join(waDir(), "auteurs.json"),
		func(ctx context.Context, id string) (string, error) {
			cli := p.client()
			if cli == nil {
				return "", errPasConnecte
			}
			path, err := p.fetchPic(cli, id)
			if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) || errors.Is(err, errPasDePhoto) {
				return "", nil // pas de photo : les initiales jusqu'à demain
			}
			return path, err
		},
		// Confidentialité, limite de débit : rien avant demain. Le reste
		// (réseau, pas connecté) est passager.
		func(err error) bool {
			return errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) || errors.Is(err, whatsmeow.ErrIQRateOverLimit) ||
				errors.Is(err, whatsmeow.ErrIQForbidden) || errors.Is(err, whatsmeow.ErrIQNotAuthorized)
		})
}

// avatarSurDisque : la photo d'un JID déjà téléchargée, quel que soit son âge.
func avatarSurDisque(id string) string {
	matches, _ := filepath.Glob(filepath.Join(mediaDir(), "avatars", avatarName(id)+".*"))
	for _, m := range matches {
		if strings.HasSuffix(m, ".tmp") {
			continue
		}
		if st, err := os.Stat(m); err == nil && st.Size() > 0 {
			return m
		}
	}
	return ""
}

// Le nombre de membres d'une mosaïque (comme les autres ponts).
const mosaiqueMax = 4

// choisirMosaique : les membres montrés dans la mosaïque d'un groupe : pas
// moi, pas de doublon, ceux qui ont une photo d'abord, quatre au plus.
func choisirMosaique(membres []string, moi func(string) bool, photo func(string) string) []string {
	var avec, sans []string
	vus := map[string]bool{}
	for _, m := range membres {
		if m == "" || vus[m] || moi(m) {
			continue
		}
		vus[m] = true
		if photo(m) != "" {
			avec = append(avec, m)
		} else {
			sans = append(sans, m)
		}
	}
	l := append(avec, sans...)
	if len(l) > mosaiqueMax {
		l = l[:mosaiqueMax]
	}
	return l
}

// mosaiques : « avatars_groupe » pour les groupes montrés qui n'ont pas
// d'image (après p.avatars), avec les photos déjà connues seulement.
func (p *Pont) mosaiques(convs []string) {
	type membre struct{ nom, path string }
	var envois []map[string]any
	p.mu.Lock()
	for _, id := range convs {
		c := p.convs[id]
		if c == nil || !c.Group || c.Community || avatarSurDisque(id) != "" {
			continue
		}
		photo := func(jid string) string {
			if pa := avatarSurDisque(jid); pa != "" {
				return pa
			}
			if pn := p.lidPN[jid]; pn != "" {
				return avatarSurDisque(pn)
			}
			return ""
		}
		choisis := choisirMosaique(c.Members, func(j string) bool { return p.isMe(p.cli, j) }, photo)
		if len(choisis) < 2 {
			continue
		}
		liste := make([]map[string]any, 0, len(choisis))
		photos := 0
		for _, j := range choisis {
			m := membre{nom: p.nameOf(j), path: photo(j)}
			if m.path != "" {
				photos++
			}
			liste = append(liste, map[string]any{"name": m.nom, "color": "", "path": m.path})
		}
		if photos > 0 {
			envois = append(envois, map[string]any{"conv": id, "members": liste})
		}
	}
	p.mu.Unlock()
	for _, e := range envois {
		emit("avatars_groupe", e)
	}
}
