package main

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"unicode"
)

// Les contacts du téléphone (ListContacts), gardés 10 minutes en mémoire,
// pour la recherche du « Nouveau message ».
var (
	contactsMu    sync.Mutex
	contactsCache []contactTrouve
	contactsLus   time.Time
)

type contactTrouve struct {
	ID     string `json:"id"`     // le numéro : ce que `new` attend
	Name   string `json:"name"`
	Number string `json:"number"` // le numéro tel qu'affiché
}

// Sans accents ni majuscules.
func plierTexte(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	r, _, err := transform.String(t, strings.ToLower(s))
	if err != nil {
		return strings.ToLower(s)
	}
	return r
}

func (p *Pont) contacts() []contactTrouve {
	contactsMu.Lock()
	defer contactsMu.Unlock()
	if contactsCache != nil && time.Since(contactsLus) < 10*time.Minute {
		return contactsCache
	}
	cli := p.client()
	if cli == nil {
		return contactsCache
	}
	ctx, annule := context.WithTimeout(context.Background(), 20*time.Second)
	defer annule()
	resp, err := cli.ListContacts(ctx)
	if err != nil {
		emitErr("search", err)
		return contactsCache
	}
	liste := []contactTrouve{}
	vus := map[string]bool{}
	for _, c := range resp.GetContacts() {
		n := c.GetNumber()
		if n == nil {
			continue
		}
		num := n.GetNumber()
		if num == "" {
			num = n.GetNumber2()
		}
		if num == "" || vus[num+"|"+c.GetName()] {
			continue
		}
		vus[num+"|"+c.GetName()] = true
		affiche := n.GetFormattedNumber()
		if affiche == "" {
			affiche = num
		}
		liste = append(liste, contactTrouve{ID: num, Name: c.GetName(), Number: affiche})
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].Name < liste[j].Name })
	contactsCache, contactsLus = liste, time.Now()
	return liste
}

// searchContacts : les contacts du téléphone dont le nom ou le numéro contient q.
func (p *Pont) searchContacts(q string) {
	items := []contactTrouve{}
	pq := plierTexte(q)
	chiffres := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q)
	if len([]rune(pq)) >= 2 {
		for _, c := range p.contacts() {
			if strings.Contains(plierTexte(c.Name), pq) ||
				(len(chiffres) >= 3 && strings.Contains(strings.Map(func(r rune) rune {
					if r >= '0' && r <= '9' {
						return r
					}
					return -1
				}, c.Number), chiffres)) {
				items = append(items, c)
				if len(items) >= 20 {
					break
				}
			}
		}
	}
	emit("search", map[string]any{"query": q, "items": items})
}
