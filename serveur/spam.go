// Le spam par conversation (marque `spam`, PROTOCOLE.md §5.5) : {id de
// conversation : heure (ms depuis l'epoch) du marquage}. Une conversation en
// spam quitte la boîte de réception des clients et ne notifie plus jamais ;
// elle se range dans le dossier Spam (Réglages). Le serveur ne fait que la
// garder et la partager : il n'envoie aucune notification et ne touche à
// aucune conversation.
package main

import (
	"encoding/json"
	"sort"
	"unicode/utf8"
)

// spamMax : au plus tant de conversations en spam (les plus récemment
// marquées restent).
const spamMax = 5000

// nettoyerSpam : ids propres, heures positives ou nulles, borné. Jamais nul.
func nettoyerSpam(m map[string]int64) map[string]int64 {
	r := make(map[string]int64, len(m))
	for id, t := range m {
		if id == "" || len(id) > tailleIdMax || !utf8.ValidString(id) || t < 0 {
			continue
		}
		r[id] = t
	}
	if len(r) > spamMax {
		ids := make([]string, 0, len(r))
		for id := range r {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if r[ids[i]] != r[ids[j]] {
				return r[ids[i]] > r[ids[j]]
			}
			return ids[i] < ids[j]
		})
		for _, id := range ids[spamMax:] {
			delete(r, id)
		}
	}
	return r
}

// unirSpam : les entrées de `dabord` gagnent, `ensuite` ajoute les ids qui
// manquent. Sert seulement pour un client en retard.
func unirSpam(dabord, ensuite map[string]int64) map[string]int64 {
	r := make(map[string]int64, len(dabord)+len(ensuite))
	for id, t := range ensuite {
		r[id] = t
	}
	for id, t := range dabord {
		r[id] = t
	}
	return nettoyerSpam(r)
}

func egalesSpam(a, b map[string]int64) bool {
	return egalesSourdine(a, b)
}

// lireSpam : absent, null ou mal formé : rien (false).
func lireSpam(brute json.RawMessage) (map[string]int64, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return nil, false
	}
	var m map[string]int64
	if json.Unmarshal(brute, &m) != nil {
		return nil, false
	}
	return nettoyerSpam(m), true
}
