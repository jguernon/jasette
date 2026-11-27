package main

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

type trameSourdine struct {
	Marques struct {
		Sourdine map[string]int64  `json:"sourdine"`
		Moods    map[string]string `json:"moods"`
		Version  int64             `json:"version"`
	} `json:"marques"`
}

func lireTrameSourdine(t *testing.T, c *Client) trameSourdine {
	t.Helper()
	var tr trameSourdine
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

// L'élagage : ids vides ou démesurés, échéances négatives ou passées
// retirés ; 0 (« jusqu'à réactivation ») et les échéances à venir restent.
func TestNettoyerSourdine(t *testing.T) {
	m := map[string]int64{"a": 0, "b": 2000, "c": 1000, "d": 999, "e": -5, "": 0}
	r := nettoyerSourdineA(m, 1000)
	if len(r) != 2 || r["a"] != 0 || r["b"] != 2000 {
		t.Fatalf("élagage : %v", r)
	}
	if !enSourdine(r, "a", 1<<62) || !enSourdine(r, "b", 1999) || enSourdine(r, "b", 2000) || enSourdine(r, "z", 0) {
		t.Fatal("enSourdine")
	}
	if nettoyerSourdine(nil) == nil {
		t.Fatal("jamais nul")
	}
}

// La marque `sourdine` : vide par défaut ; présente, c'est l'objet complet
// (une clé absente réactive) ; absente, rien ne change ; échue, élaguée ;
// d'un client en retard, unie sans refus.
func TestMarquesSourdine(t *testing.T) {
	s, c := serveurEssai(t)
	s.recevoirMarques(c, json.RawMessage(`{"version":0}`))
	m := lireTrameSourdine(t, c).Marques
	if m.Sourdine == nil || len(m.Sourdine) != 0 {
		t.Fatalf("défaut : %+v", m)
	}
	plusTard := time.Now().Add(time.Hour).UnixMilli()
	passe := time.Now().Add(-time.Minute).UnixMilli()
	pt := strconv.FormatInt(plusTard, 10)
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(m.Version)+`,"sourdine":{"m:1":0,"w:2":`+pt+`,"i:3":`+strconv.FormatInt(passe, 10)+`,"":0}}`))
	m = lireTrameSourdine(t, c).Marques
	if len(m.Sourdine) != 2 || m.Sourdine["m:1"] != 0 || m.Sourdine["w:2"] != plusTard {
		t.Fatalf("posée : %+v", m)
	}
	// Un ancien client (sans `sourdine`) ne l'efface pas.
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(m.Version)+`,"moods":{"m:1":"amour"}}`))
	m = lireTrameSourdine(t, c).Marques
	if len(m.Sourdine) != 2 || m.Moods["m:1"] != "amour" {
		t.Fatalf("ancien client : %+v", m)
	}
	// « Réactiver les notifications » : la clé disparaît.
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(m.Version)+`,"sourdine":{"w:2":`+pt+`}}`))
	m = lireTrameSourdine(t, c).Marques
	if len(m.Sourdine) != 1 || m.Sourdine["w:2"] != plusTard {
		t.Fatalf("réactivée : %+v", m)
	}
	// Un client en retard qui ne l'a pas : union, rien n'est refusé ni perdu.
	s.recevoirMarques(c, json.RawMessage(`{"version":0,"sourdine":{"t:9":0}}`))
	m = lireTrameSourdine(t, c).Marques
	if len(m.Sourdine) != 2 || m.Sourdine["t:9"] != 0 || m.Sourdine["w:2"] != plusTard {
		t.Fatalf("union : %+v", m)
	}
	if d := chargerMarques(); len(d.Sourdine) != 2 {
		t.Fatalf("disque : %+v", d.Sourdine)
	}
	// Échue dans marques.json : élaguée au chargement et à l'envoi.
	s.mu.Lock()
	s.marques.Sourdine["w:2"] = passe
	tr := trameMarques(s.marques, nil, "")
	s.mu.Unlock()
	var lue trameSourdine
	if err := json.Unmarshal(tr, &lue); err != nil {
		t.Fatal(err)
	}
	if len(lue.Marques.Sourdine) != 1 || lue.Marques.Sourdine["t:9"] != 0 {
		t.Fatalf("envoi élagué : %+v", lue.Marques.Sourdine)
	}
}
