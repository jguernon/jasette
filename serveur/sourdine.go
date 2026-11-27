// La sourdine par conversation (marque `sourdine`, PROTOCOLE.md §5.5) :
// {id de conversation : échéance en ms depuis l'epoch, 0 = jusqu'à ce que
// l'utilisateur la réactive}. Elle ne coupe que les notifications système
// des clients (aucune bulle, aucun son de notification, aucune vibration) ;
// le serveur lui-même n'envoie aucune notification : il ne fait que la
// garder, l'élaguer et la partager.
package main

import (
	"encoding/json"
	"sort"
	"time"
	"unicode/utf8"
)

// sourdineMax : au plus tant de conversations en sourdine (les échéances
// les plus lointaines restent, « jusqu'à réactivation » d'abord).
const sourdineMax = 5000

// nettoyerSourdine : ids propres, échéances positives ou nulles, rien
// d'échu (élagué à chaque écriture, au chargement et à chaque envoi),
// borné. Jamais nul.
func nettoyerSourdine(m map[string]int64) map[string]int64 {
	return nettoyerSourdineA(m, time.Now().UnixMilli())
}

func nettoyerSourdineA(m map[string]int64, maintenant int64) map[string]int64 {
	r := make(map[string]int64, len(m))
	for id, t := range m {
		if id == "" || len(id) > tailleIdMax || !utf8.ValidString(id) || t < 0 || (t > 0 && t <= maintenant) {
			continue
		}
		r[id] = t
	}
	if len(r) > sourdineMax {
		ids := make([]string, 0, len(r))
		for id := range r {
			ids = append(ids, id)
		}
		// « Jusqu'à réactivation » (0) d'abord, puis les plus lointaines.
		cle := func(t int64) int64 {
			if t == 0 {
				return 1<<63 - 1
			}
			return t
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := cle(r[ids[i]]), cle(r[ids[j]])
			if a != b {
				return a > b
			}
			return ids[i] < ids[j]
		})
		for _, id := range ids[sourdineMax:] {
			delete(r, id)
		}
	}
	return r
}

// unirSourdine : les entrées de `dabord` gagnent, `ensuite` ajoute les ids
// qui manquent (comme les moods). Sert seulement pour un client en retard.
func unirSourdine(dabord, ensuite map[string]int64) map[string]int64 {
	r := make(map[string]int64, len(dabord)+len(ensuite))
	for id, t := range ensuite {
		r[id] = t
	}
	for id, t := range dabord {
		r[id] = t
	}
	return nettoyerSourdine(r)
}

func egalesSourdine(a, b map[string]int64) bool {
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

// lireSourdine : absent, null ou mal formé : rien (false).
func lireSourdine(brute json.RawMessage) (map[string]int64, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return nil, false
	}
	var m map[string]int64
	if json.Unmarshal(brute, &m) != nil {
		return nil, false
	}
	return nettoyerSourdine(m), true
}

// enSourdine : la conversation `id` est-elle en sourdine à `maintenant` (ms) ?
func enSourdine(m map[string]int64, id string, maintenant int64) bool {
	t, ok := m[id]
	return ok && (t == 0 || t > maintenant)
}
