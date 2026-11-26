package main

// La photo des auteurs dans les groupes (PROTOCOLE.md §5.13), comme le pont
// Discord : chaque message d'un autre dans un groupe porte « sender_id »,
// et {"ev":"sender_avatar","service":…,"id":<sender_id>,"path":…} donne sa
// photo. Paresseux : seulement pour les messages réellement chargés ou
// reçus, une fois par personne et par session (le serveur rejoue ensuite
// l'événement aux clients qui arrivent), au plus auteursParMinute
// téléchargements par minute. Les photos viennent du même cache que celles
// des fils à deux (« contact:<id> ») : déjà sur le disque, elles partent
// sans réseau.

import (
	"os"
	"path/filepath"
	"time"
)

const auteursParMinute = 20

// noteAuteur : pose sender_id sur un message d'un autre dans un groupe et
// demande sa photo la première fois. À appeler avec p.mu.
func (p *Pont) noteAuteur(r *msgRow, m *Message) {
	if m.FromMe || r.SenderID == 0 || r.SenderID == p.me {
		return
	}
	c := p.convs[r.Conv]
	if c == nil || !isGroupType(c.Type) {
		return
	}
	m.Auteur = idStr(r.SenderID)
	if p.auteurs == nil || p.auteurs[r.SenderID] {
		return
	}
	ct := p.contacts[r.SenderID]
	if ct == nil || ct.Avatar == "" {
		return // pas d'adresse (encore) : on réessaiera au prochain message
	}
	p.auteurs[r.SenderID] = true
	select {
	case p.fileAuteurs <- r.SenderID:
	default:
		delete(p.auteurs, r.SenderID) // file pleine : plus tard
	}
}

// Une photo de contact déjà sur le disque, quel que soit son âge.
func avatarSurDisque(id string) string {
	matches, _ := filepath.Glob(filepath.Join(mediaDir(), "avatars", hashName(id)+".*"))
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && st.Size() > 0 && filepath.Ext(m) != ".tmp" {
			return m
		}
	}
	return ""
}

// boucleAuteurs : une demande à la fois ; une photo sur le disque part tout
// de suite, un téléchargement attend son tour (auteursParMinute).
func (p *Pont) boucleAuteurs() {
	var dernier time.Time
	ecart := time.Minute / auteursParMinute
	for {
		var id int64
		select {
		case <-p.ctx.Done():
			return
		case id = <-p.fileAuteurs:
		}
		cle := "contact:" + idStr(id)
		path := avatarSurDisque(cle)
		if path == "" {
			if attente := ecart - time.Since(dernier); attente > 0 {
				select {
				case <-time.After(attente):
				case <-p.ctx.Done():
					return
				}
			}
			dernier = time.Now()
			p.mu.Lock()
			url := ""
			if ct := p.contacts[id]; ct != nil {
				url = ct.Avatar
			}
			p.mu.Unlock()
			if pa, err := p.fetchAvatar(cle, url); err == nil {
				path = pa
			} else {
				log.Debug().Err(err).Msg("photo d'un auteur")
			}
		}
		if path == "" {
			continue // gardé comme demandé : pas de nouvel essai dans cette session
		}
		emit("sender_avatar", map[string]any{"service": serviceName(), "id": idStr(id), "path": path})
	}
}
