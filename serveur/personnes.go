// La fiche de la personne : les marques `personnes` et `personnes_refus`
// (PROTOCOLE.md, §5.5, « Règle des personnes »).
//
// Une personne relie à la main des conversations à deux de services
// différents (« Mon Amour » sur Google, « Stephanie Legendre » sur
// Messenger). La correspondance automatique (même numéro, nom proche) se fait
// chez les clients, sans rien demander aux services : le serveur ne garde
// que les liens choisis à la main et les refus (« Pas la même personne »).
package main

import (
	"encoding/json"
	"unicode/utf8"
)

const (
	personnesMax      = 2000 // personnes au plus
	convsParPersonne  = 32   // conversations au plus par personne
	tailleIdPersonne  = 64   // octets au plus par id de personne
	personnesRefusMax = 5000 // refus au plus
)

// Personne : des conversations (ids préfixés, comme les autres marques) qui
// sont la même personne. Principal (facultatif) : la conversation dont le nom
// est celui de la personne ; une des Convs, sinon ignoré.
type Personne struct {
	ID        string   `json:"id"`
	Convs     []string `json:"convs"`
	Principal string   `json:"principal,omitempty"`
}

// idValide : un id de conversation propre (non vide, UTF-8, borné).
func idValide(id string) bool {
	return id != "" && len(id) <= tailleIdMax && utf8.ValidString(id)
}

// nettoyerPersonnes : jamais nul ; ids de personne uniques et bornés ;
// chaque conversation dans une seule personne (la première qui la nomme) ;
// au plus 32 conversations par personne ; une personne de moins de deux
// conversations ne relie rien et disparaît ; Principal seulement s'il est
// une de ses conversations.
func nettoyerPersonnes(l []Personne) []Personne {
	r := make([]Personne, 0, len(l))
	idsVus := map[string]bool{}
	convsVues := map[string]bool{}
	for _, p := range l {
		if p.ID == "" || len(p.ID) > tailleIdPersonne || !utf8.ValidString(p.ID) || idsVus[p.ID] {
			continue
		}
		convs := make([]string, 0, len(p.Convs))
		ici := map[string]bool{}
		for _, id := range p.Convs {
			if !idValide(id) || convsVues[id] || ici[id] {
				continue
			}
			ici[id] = true
			if len(convs) >= convsParPersonne {
				break
			}
			convs = append(convs, id)
		}
		if len(convs) < 2 {
			continue
		}
		for _, id := range convs {
			convsVues[id] = true
		}
		idsVus[p.ID] = true
		principal := ""
		for _, id := range convs {
			if id == p.Principal {
				principal = id
			}
		}
		r = append(r, Personne{ID: p.ID, Convs: convs, Principal: principal})
		if len(r) >= personnesMax {
			break
		}
	}
	return r
}

// nettoyerRefus : des paires de deux conversations différentes, rangées
// (la plus petite d'abord), sans doublon, bornées. Jamais nul.
func nettoyerRefus(l [][]string) [][]string {
	r := make([][]string, 0, len(l))
	vus := map[[2]string]bool{}
	for _, p := range l {
		if len(p) != 2 || !idValide(p[0]) || !idValide(p[1]) || p[0] == p[1] {
			continue
		}
		a, b := p[0], p[1]
		if b < a {
			a, b = b, a
		}
		if vus[[2]string{a, b}] {
			continue
		}
		vus[[2]string{a, b}] = true
		r = append(r, []string{a, b})
		if len(r) >= personnesRefusMax {
			break
		}
	}
	return r
}

// lirePersonnes : la valeur de `personnes` d'une trame, avec indulgence (un
// élément mal formé est sauté, pas toute la trame). Faux si elle est absente
// ou nulle (rien ne change) ou n'est pas une liste.
func lirePersonnes(brute json.RawMessage) ([]Personne, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return nil, false
	}
	var elems []json.RawMessage
	if json.Unmarshal(brute, &elems) != nil {
		return nil, false
	}
	r := make([]Personne, 0, len(elems))
	for _, e := range elems {
		var p struct {
			ID        string            `json:"id"`
			Convs     []json.RawMessage `json:"convs"`
			Principal json.RawMessage   `json:"principal"`
		}
		if json.Unmarshal(e, &p) != nil {
			continue
		}
		var convs []string
		for _, c := range p.Convs {
			var s string
			if json.Unmarshal(c, &s) == nil {
				convs = append(convs, s)
			}
		}
		var principal string
		_ = json.Unmarshal(p.Principal, &principal)
		r = append(r, Personne{ID: p.ID, Convs: convs, Principal: principal})
	}
	return nettoyerPersonnes(r), true
}

// lireRefus : la valeur de `personnes_refus` d'une trame, avec la même
// indulgence. Faux si absente, nulle ou pas une liste.
func lireRefus(brute json.RawMessage) ([][]string, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return nil, false
	}
	var elems []json.RawMessage
	if json.Unmarshal(brute, &elems) != nil {
		return nil, false
	}
	r := make([][]string, 0, len(elems))
	for _, e := range elems {
		var p []string
		if json.Unmarshal(e, &p) == nil {
			r = append(r, p)
		}
	}
	return nettoyerRefus(r), true
}

func egalesPersonnes(a, b []Personne) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Principal != b[i].Principal || !egales(a[i].Convs, b[i].Convs) {
			return false
		}
	}
	return true
}

func egalesRefus(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !egales(a[i], b[i]) {
			return false
		}
	}
	return true
}

// unirPersonnes : les personnes de `dabord` telles quelles (même id : celle
// de `dabord` gagne, entière), puis celles de `ensuite` qu'il n'a pas, moins
// les conversations que `dabord` relie déjà ailleurs (une conversation n'est
// qu'à une personne). Une personne qui n'a plus deux conversations tombe.
func unirPersonnes(dabord, ensuite []Personne) []Personne {
	r := append([]Personne{}, dabord...)
	ids := map[string]bool{}
	convs := map[string]bool{}
	for _, p := range dabord {
		ids[p.ID] = true
		for _, c := range p.Convs {
			convs[c] = true
		}
	}
	for _, p := range ensuite {
		if ids[p.ID] {
			continue
		}
		reste := make([]string, 0, len(p.Convs))
		for _, c := range p.Convs {
			if !convs[c] {
				reste = append(reste, c)
			}
		}
		r = append(r, Personne{ID: p.ID, Convs: reste, Principal: p.Principal})
	}
	return nettoyerPersonnes(r)
}

// unirRefus : les refus de `dabord`, puis ceux de `ensuite` qu'il n'a pas.
func unirRefus(dabord, ensuite [][]string) [][]string {
	return nettoyerRefus(append(append([][]string{}, dabord...), ensuite...))
}
