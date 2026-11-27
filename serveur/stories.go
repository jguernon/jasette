// Les stories des contacts (PROTOCOLE.md, §5.15) : un cercle autour de
// l'avatar d'un contact qui a une story Instagram ou Facebook active.
//
// Le réglage `stories` (marque partagée, faux par défaut) est transmis aux
// ponts Instagram et Messenger comme notes_contacts : JASETTE_STORIES=1|0 au
// lancement, puis la commande `stories`. Le serveur garde la dernière liste
// `stories` de chaque pont pour la rejouer, et l'oublie quand le réglage est
// désactivé. La marque `stories_ouvertes` retient, par conversation, l'heure
// de la story la plus récente ouverte depuis Jasette (le cercle disparaît
// alors, sur tous les appareils, jusqu'à une story plus récente) : jamais rien
// n'est marqué « vu » chez Meta.
package main

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
)

// Les ponts qui lisent les stories.
var servicesStories = []string{"instagram", "messenger"}

func envStories(on bool) string {
	if on {
		return "JASETTE_STORIES=1"
	}
	return "JASETTE_STORIES=0"
}

func lisStories(svc string) bool {
	for _, s := range servicesStories {
		if s == svc {
			return true
		}
	}
	return false
}

// storiesChange : la marque `stories` vient de changer. Les ponts reçoivent
// la commande (sans redémarrer) ; désactivée, les listes gardées sont
// oubliées. À appeler sous s.mu.
func (s *Serveur) storiesChange(on bool) {
	for _, svc := range servicesStories {
		p := s.ponts[svc]
		if p == nil {
			continue
		}
		if !on {
			p.stories = nil
		}
		cmd, _ := json.Marshal(map[string]any{"cmd": "stories", "on": on})
		go func() {
			if err := p.envoyer(cmd); err != nil && !errors.Is(err, errPontArrete) {
				logf("[%s] stories : %v", p.svc, err)
			}
		}()
	}
}

const (
	// Une story dure 24 h : au-delà de 48 h, l'entrée ne sert plus.
	storiesOuvertesDuree = 48 * time.Hour
	storiesOuvertesMax   = 2000
)

// nettoyerStoriesOuvertes : ids propres, heures positives, rien d'échu
// (purge à chaque écriture), borné (les plus récentes restent).
func nettoyerStoriesOuvertes(m map[string]int64) map[string]int64 {
	limite := time.Now().Add(-storiesOuvertesDuree).UnixMilli()
	r := make(map[string]int64, len(m))
	for id, t := range m {
		if id == "" || len(id) > tailleIdMax || !utf8.ValidString(id) || t <= limite {
			continue
		}
		r[id] = t
	}
	for len(r) > storiesOuvertesMax {
		var plusVieux string
		for id, t := range r {
			if plusVieux == "" || t < r[plusVieux] || (t == r[plusVieux] && id < plusVieux) {
				plusVieux = id
			}
		}
		delete(r, plusVieux)
	}
	return r
}

// unirStoriesOuvertes : l'heure la plus récente de chaque côté. Une entrée
// n'est jamais retirée par un client (seulement par la purge) : ouvrir une
// story sur un appareil ne peut pas être défait par un autre en retard.
func unirStoriesOuvertes(a, b map[string]int64) map[string]int64 {
	r := make(map[string]int64, len(a)+len(b))
	for id, t := range b {
		r[id] = t
	}
	for id, t := range a {
		if t > r[id] {
			r[id] = t
		}
	}
	return nettoyerStoriesOuvertes(r)
}

func egalesStoriesOuvertes(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for id, t := range a {
		if v, ok := b[id]; !ok || v != t {
			return false
		}
	}
	return true
}

// lireStoriesOuvertes : absent, null ou mal formé : rien (false).
func lireStoriesOuvertes(brute json.RawMessage) (map[string]int64, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return nil, false
	}
	var m map[string]int64
	if json.Unmarshal(brute, &m) != nil {
		return nil, false
	}
	return nettoyerStoriesOuvertes(m), true
}
