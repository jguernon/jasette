package main

import (
	"encoding/json"
	"strings"
	"testing"
)

type trameOrdre struct {
	Marques struct {
		Ordre   []string `json:"ordre_services"`
		Favoris []string `json:"favoris"`
		Version int64    `json:"version"`
		Refus   string   `json:"refus"`
	} `json:"marques"`
}

func lireOrdre(t *testing.T, c *Client) trameOrdre {
	t.Helper()
	var tr trameOrdre
	select {
	case b := <-c.evs:
		if err := json.Unmarshal(b, &tr); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"ordre_services":[`) {
			t.Fatalf("ordre_services absent ou nul : %s", b)
		}
	default:
		t.Fatal("aucune trame")
	}
	return tr
}

// La marque ordre_services : toujours envoyée (liste, jamais null), nettoyée,
// prise telle quelle (sans union), absente ou mal formée = rien ne change,
// vide d'un client en retard = ignorée, gardée sur disque et diffusée.
func TestMarquesOrdreServices(t *testing.T) {
	s, c := serveurEssai(t)
	autre := &Client{srv: s, nom: "autre", evs: make(chan []byte, 16)}
	s.clients[autre] = struct{}{}

	s.recevoirMarques(c, json.RawMessage(`{"version":0}`))
	if m := lireOrdre(t, c).Marques; len(m.Ordre) != 0 || m.Version != 0 {
		t.Fatalf("défaut : %+v", m)
	}

	// Migration d'un client : l'ordre local part, nettoyé.
	s.recevoirMarques(c, json.RawMessage(`{"version":0,"ordre_services":["signal","","google","signal","`+strings.Repeat("x", 40)+`","x"]}`))
	m := lireOrdre(t, c).Marques
	if strings.Join(m.Ordre, ",") != "signal,google,x" || m.Version != 1 {
		t.Fatalf("migration : %+v", m)
	}
	if d := lireOrdre(t, autre).Marques; strings.Join(d.Ordre, ",") != "signal,google,x" {
		t.Fatalf("diffusion : %+v", d)
	}

	// Absent, null ou mal formé : rien ne change (le reste de la trame compte).
	s.recevoirMarques(c, json.RawMessage(`{"version":1,"favoris":["123"],"ordre_services":[1,2]}`))
	m = lireOrdre(t, c).Marques
	if strings.Join(m.Ordre, ",") != "signal,google,x" || strings.Join(m.Favoris, ",") != "123" || m.Version != 2 {
		t.Fatalf("mal formé : %+v", m)
	}
	lireOrdre(t, autre)
	s.recevoirMarques(c, json.RawMessage(`{"version":2,"ordre_services":null}`))
	if m = lireOrdre(t, c).Marques; strings.Join(m.Ordre, ",") != "signal,google,x" || m.Version != 2 {
		t.Fatalf("null : %+v", m)
	}

	// Client en retard : son ordre est pris tel quel (pas d'union) ; vide, ignoré.
	s.recevoirMarques(c, json.RawMessage(`{"version":1,"ordre_services":["x","messenger"]}`))
	if m = lireOrdre(t, c).Marques; strings.Join(m.Ordre, ",") != "x,messenger" || m.Version != 3 {
		t.Fatalf("en retard : %+v", m)
	}
	lireOrdre(t, autre)
	s.recevoirMarques(c, json.RawMessage(`{"version":1,"ordre_services":[]}`))
	if m = lireOrdre(t, c).Marques; strings.Join(m.Ordre, ",") != "x,messenger" || m.Version != 3 || m.Refus != "" {
		t.Fatalf("vide en retard : %+v", m)
	}

	// À jour, une liste vide est prise : retour à l'ordre par défaut.
	s.recevoirMarques(c, json.RawMessage(`{"version":3,"ordre_services":[]}`))
	if m = lireOrdre(t, c).Marques; len(m.Ordre) != 0 || m.Version != 4 {
		t.Fatalf("vide à jour : %+v", m)
	}

	// Plus de 32 noms : bornée.
	noms := make([]string, 40)
	for i := range noms {
		noms[i] = `"s` + itoa(int64(i)) + `"`
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":4,"ordre_services":[`+strings.Join(noms, ",")+`]}`))
	if m = lireOrdre(t, c).Marques; len(m.Ordre) != 32 || m.Ordre[0] != "s0" {
		t.Fatalf("bornée : %d", len(m.Ordre))
	}
	if d := chargerMarques(); len(d.OrdreServices) != 32 || d.Version != 5 {
		t.Fatalf("disque : %+v", d.OrdreServices)
	}
}

// Un ancien marques.json (sans ordre_services) se lit : ordre vide, envoyé `[]`.
func TestMarquesOrdreServicesAncienFichier(t *testing.T) {
	serveurEssai(t)
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":[],"favoris":["1"],"version":3}`)); err != nil {
		t.Fatal(err)
	}
	m := chargerMarques()
	if m.OrdreServices == nil || len(m.OrdreServices) != 0 || m.Version != 3 {
		t.Fatalf("ancien : %+v", m)
	}
	if !strings.Contains(string(trameMarques(m, nil, "")), `"ordre_services":[]`) {
		t.Fatal("ordre_services non envoyé")
	}
}
