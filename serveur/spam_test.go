package main

import (
	"encoding/json"
	"testing"
)

type trameSpam struct {
	Marques struct {
		Spam     map[string]int64  `json:"spam"`
		Sourdine map[string]int64  `json:"sourdine"`
		Moods    map[string]string `json:"moods"`
		Version  int64             `json:"version"`
	} `json:"marques"`
}

func lireTrameSpam(t *testing.T, c *Client) trameSpam {
	t.Helper()
	var tr trameSpam
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

func TestNettoyerSpam(t *testing.T) {
	r := nettoyerSpam(map[string]int64{"a": 5, "b": 0, "c": -1, "": 3})
	if len(r) != 2 || r["a"] != 5 || r["b"] != 0 {
		t.Fatalf("nettoyage : %v", r)
	}
	if nettoyerSpam(nil) == nil {
		t.Fatal("jamais nul")
	}
	u := unirSpam(map[string]int64{"a": 9}, map[string]int64{"a": 1, "z": 2})
	if len(u) != 2 || u["a"] != 9 || u["z"] != 2 {
		t.Fatalf("union : %v", u)
	}
}

// La marque `spam` : vide par défaut ; présente, c'est l'objet complet (une
// clé absente : « Ce n'est pas du spam ») ; absente, rien ne change (un
// ancien client ne l'efface pas) ; d'un client en retard, unie sans refus ;
// gardée sur disque ; aucune échéance (rien n'est élagué avec le temps).
func TestMarquesSpam(t *testing.T) {
	s, c := serveurEssai(t)
	s.recevoirMarques(c, json.RawMessage(`{"version":0}`))
	m := lireTrameSpam(t, c).Marques
	if m.Spam == nil || len(m.Spam) != 0 {
		t.Fatalf("défaut : %+v", m)
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(m.Version)+`,"spam":{"15145551234":1764370800000,"m:1":1,"":4},"sourdine":{"15145551234":0}}`))
	m = lireTrameSpam(t, c).Marques
	if len(m.Spam) != 2 || m.Spam["15145551234"] != 1764370800000 || m.Sourdine["15145551234"] != 0 {
		t.Fatalf("posé : %+v", m)
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(m.Version)+`,"moods":{"m:1":"amour"}}`))
	m = lireTrameSpam(t, c).Marques
	if len(m.Spam) != 2 || m.Moods["m:1"] != "amour" {
		t.Fatalf("ancien client : %+v", m)
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(m.Version)+`,"spam":{"m:1":1}}`))
	m = lireTrameSpam(t, c).Marques
	if len(m.Spam) != 1 || m.Spam["m:1"] != 1 {
		t.Fatalf("pas du spam : %+v", m)
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":0,"spam":{"t:9":7}}`))
	m = lireTrameSpam(t, c).Marques
	if len(m.Spam) != 2 || m.Spam["t:9"] != 7 || m.Spam["m:1"] != 1 {
		t.Fatalf("union : %+v", m)
	}
	if d := chargerMarques(); len(d.Spam) != 2 {
		t.Fatalf("disque : %+v", d.Spam)
	}
}
