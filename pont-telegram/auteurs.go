package main

// La photo des auteurs dans les groupes et la mosaïque des groupes sans
// image (PROTOCOLE.md §5.13).
//
//   - Auteurs : chaque message d'un autre dans un groupe ou un supergroupe
//     porte « sender_id » (« u<id> ») ; sa photo de profil (la petite
//     version) vient de peerAvatar, avec les règles de photos_auteurs.go
//     (paresseux, une fois par personne et par jour, au plus 10 par minute
//     et 200 par jour, refus gardé jusqu'au lendemain). Une photo cachée par
//     la confidentialité n'a pas d'identifiant : les initiales, sans requête.
//   - Mosaïque : un groupe sans image reçoit « avatars_groupe » avec ses
//     derniers auteurs (Telegram ne donne pas la liste des membres sans
//     requête) et les photos déjà sur le disque, sans requête de plus.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/tgerr"
)

func (p *Pont) nouveauxAuteurs() *photosAuteurs {
	return nouvellesPhotosAuteurs(filepath.Join(dataDir(), serviceName(), "auteurs.json"),
		func(ctx context.Context, id string) (string, error) {
			ctx, annule := context.WithTimeout(ctx, 30*time.Second)
			defer annule()
			path, err := p.peerAvatar(ctx, id)
			if errors.Is(err, errPasDImage) {
				return "", nil // pas de photo (ou cachée) : les initiales jusqu'à demain
			}
			return path, err
		},
		refusTelegram)
}

// refusTelegram : une erreur de Telegram lui-même (FLOOD_WAIT, accès,
// pair inconnu…) : rien avant demain. Le reste (réseau, hors ligne) est passager.
func refusTelegram(err error) bool {
	if _, ok := tgerr.As(err); ok {
		return true
	}
	return false
}

// photoConnue : la photo d'un pair déjà sur le disque, sans requête. À
// appeler sans p.mu (lecture de la base).
func (p *Pont) photoConnue(k string) string {
	var path string
	if p.loadKV("avatar:"+k, &path) && path != "" {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return path
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
// d'image, avec leurs derniers auteurs gardés et les photos déjà connues.
func (p *Pont) mosaiques(convs []string) {
	p.mu.Lock()
	moi := keyOf('u', p.me)
	var groupes []string
	for _, k := range convs {
		if pe := p.peers[k]; pe != nil && pe.Photo == 0 && (pe.Kind == 'g' || (pe.Kind == 'c' && !pe.Broadcast)) {
			groupes = append(groupes, k)
		}
	}
	p.mu.Unlock()
	for _, k := range groupes {
		if p.ctx.Err() != nil {
			return
		}
		var auteurs []string
		for _, r := range p.queryMsgs(k, 0, "", 60) {
			if !r.FromMe && len(r.SenderID) > 1 && r.SenderID[0] == 'u' {
				auteurs = append(auteurs, r.SenderID)
			}
		}
		photos := map[string]string{}
		for _, a := range auteurs {
			if _, ok := photos[a]; !ok {
				photos[a] = p.photoConnue(a)
			}
		}
		choisis := choisirMosaique(auteurs, func(a string) bool { return a == moi }, func(a string) string { return photos[a] })
		if len(choisis) < 2 {
			continue
		}
		liste := make([]map[string]any, 0, len(choisis))
		n := 0
		p.mu.Lock()
		for _, a := range choisis {
			if photos[a] != "" {
				n++
			}
			liste = append(liste, map[string]any{"name": p.peerName(a), "color": "", "path": photos[a]})
		}
		p.mu.Unlock()
		if n > 0 {
			emit("avatars_groupe", map[string]any{"conv": k, "members": liste})
		}
	}
}
