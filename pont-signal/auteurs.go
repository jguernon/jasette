package main

// La photo des auteurs dans les groupes et la mosaïque des groupes sans
// image (PROTOCOLE.md §5.13).
//
//   - Auteurs : chaque message d'un autre dans un groupe porte « sender_id »
//     (son ACI) ; sa photo est celle de contactAvatar (le carnet d'adresses
//     du téléphone si on l'a, sinon le profil Signal et sa photo chiffrée,
//     déchiffrée avec la clé de profil), avec les règles de
//     photos_auteurs.go (paresseux, une fois par personne et par jour, au
//     plus 10 par minute et 200 par jour, refus gardé jusqu'au lendemain).
//   - Mosaïque : un groupe sans image reçoit « avatars_groupe » avec ses
//     premiers membres et les photos déjà sur le disque, sans requête de plus.

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"time"
)

var errPasConnecte = errors.New("Signal n'est pas connecté")

func (p *Pont) nouveauxAuteurs() *photosAuteurs {
	return nouvellesPhotosAuteurs(filepath.Join(dataDir(), serviceName(), "auteurs.json"),
		func(ctx context.Context, id string) (string, error) {
			cli := p.client()
			if cli == nil {
				return "", errPasConnecte
			}
			ctx, annule := context.WithTimeout(ctx, 30*time.Second)
			defer annule()
			path, err := p.contactAvatar(ctx, cli, id)
			if errors.Is(err, errPasDImage) {
				return "", nil // pas de photo : les initiales jusqu'à demain
			}
			return path, err
		},
		refusSignal)
}

// refusSignal : le serveur refuse (limite de débit, accès, profil
// introuvable) : rien avant demain. Le reste (réseau, pas connecté) est passager.
func refusSignal(err error) bool {
	if errors.Is(err, errPasConnecte) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return codeRefus.MatchString(err.Error())
}

// Les codes HTTP d'un refus, en mots entiers (jamais un bout d'ACI).
var codeRefus = regexp.MustCompile(`\b(401|403|404|413|429)\b|(?i)rate.?limit`)

// photoConnue : la photo d'un ACI déjà sur le disque, quel que soit son âge.
func photoConnue(aci string) string {
	if path, ok := avatarFresh("contact:"+aci, 0); ok {
		return path
	}
	if path, ok := avatarFresh("profil:"+aci, 0); ok {
		return path
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
// d'image, avec les photos déjà connues seulement.
func (p *Pont) mosaiques(convs []string) {
	var envois []map[string]any
	p.mu.Lock()
	moi := p.me.String()
	for _, id := range convs {
		c := p.convs[id]
		if c == nil || !c.Group || c.Avatar != "" {
			continue
		}
		choisis := choisirMosaique(c.Members, func(a string) bool { return a == moi }, photoConnue)
		if len(choisis) < 2 {
			continue
		}
		liste := make([]map[string]any, 0, len(choisis))
		photos := 0
		for _, a := range choisis {
			path := photoConnue(a)
			if path != "" {
				photos++
			}
			liste = append(liste, map[string]any{"name": p.contactName(a), "color": "", "path": path})
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
