package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

type trameStories struct {
	Marques struct {
		Stories  *bool            `json:"stories"`
		Ouvertes map[string]int64 `json:"stories_ouvertes"`
		Version  int64            `json:"version"`
	} `json:"marques"`
}

func lireStories(t *testing.T, c *Client) trameStories {
	t.Helper()
	var tr trameStories
	select {
	case b := <-c.evs:
		if err := json.Unmarshal(b, &tr); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("aucune trame")
	}
	return tr
}

// stories : faux par défaut ; stories_ouvertes s'unit (l'heure la plus
// récente gagne, même d'un client en retard) et se purge après 48 h.
func TestMarquesStories(t *testing.T) {
	s, c := serveurEssai(t)
	s.recevoirMarques(c, json.RawMessage(`{"version":0}`))
	m := lireStories(t, c).Marques
	if m.Stories == nil || *m.Stories || m.Ouvertes == nil || len(m.Ouvertes) != 0 {
		t.Fatalf("défauts : %+v", m)
	}
	maint := time.Now().UnixMilli()
	vieux := time.Now().Add(-72 * time.Hour).UnixMilli()
	n := strconv.FormatInt(maint, 10)
	s.recevoirMarques(c, json.RawMessage(`{"version":0,"stories":true,"stories_ouvertes":{"i:1":`+n+`,"i:2":`+strconv.FormatInt(vieux, 10)+`,"":5}}`))
	m = lireStories(t, c).Marques
	if !*m.Stories || len(m.Ouvertes) != 1 || m.Ouvertes["i:1"] != maint {
		t.Fatalf("changées : %+v", m)
	}
	// Un client en retard, avec une heure plus ancienne pour i:1 et une autre
	// conversation : union, l'heure la plus récente reste.
	s.recevoirMarques(c, json.RawMessage(`{"version":0,"stories_ouvertes":{"i:1":`+strconv.FormatInt(maint-1000, 10)+`,"m:3":`+n+`}}`))
	m = lireStories(t, c).Marques
	if len(m.Ouvertes) != 2 || m.Ouvertes["i:1"] != maint || m.Ouvertes["m:3"] != maint {
		t.Fatalf("union : %+v", m)
	}
	// Vide : rien n'est retiré.
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(m.Version)+`,"stories_ouvertes":{}}`))
	if m2 := lireStories(t, c).Marques; len(m2.Ouvertes) != 2 || m2.Version != m.Version {
		t.Fatalf("vide : %+v", m2)
	}
	if d := chargerMarques(); !d.Stories || len(d.StoriesOuvertes) != 2 {
		t.Fatalf("disque : %+v", d)
	}
}

// stories est gardé par pont, rejoué, puis oublié et plus relayé quand le
// réglage est désactivé ; jamais relayé avant qu'il soit activé.
func TestStoriesRelais(t *testing.T) {
	s, c := serveurEssai(t)
	p := &Pont{svc: "instagram", srv: s, dernier: map[string][]byte{}, demandes: map[string]demande{},
		presence: map[string][]byte{}, cache: &Cache{}}
	s.ponts = map[string]*Pont{"instagram": p}

	p.recu([]byte(`{"ev":"stories","items":[{"conv":"3001","user":"1","latest":3,"until":9,"seen":false,"link":"https://www.instagram.com/stories/a/","service":"instagram"}],"time":7}`))
	if len(c.evs) != 0 || p.stories != nil {
		t.Fatal("stories relayées sans le réglage")
	}
	s.recevoirMarques(c, json.RawMessage(`{"stories":true,"version":0}`))
	<-c.evs
	p.recu([]byte(`{"ev":"stories","items":[{"conv":"3001","user":"1","latest":3,"until":9,"seen":false,"link":"https://www.instagram.com/stories/a/","service":"instagram"}],"time":7}`))
	if len(c.evs) != 1 {
		t.Fatalf("relayées : %d", len(c.evs))
	}
	<-c.evs
	s.mu.Lock()
	r := p.rejeu()
	s.mu.Unlock()
	tout := ""
	for _, t := range r {
		tout += string(t) + "\n"
	}
	if !strings.Contains(tout, `"ev":"stories"`) {
		t.Fatalf("rejeu : %s", tout)
	}
	s.recevoirMarques(c, json.RawMessage(`{"stories":false,"version":`+itoa(s.marques.Version)+`}`))
	<-c.evs
	if p.stories != nil {
		t.Fatal("stories pas oubliées")
	}
}
