package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type tramePersonnes struct {
	Marques struct {
		Head      []string   `json:"head"`
		Personnes []Personne `json:"personnes"`
		Refus     [][]string `json:"personnes_refus"`
		Version   int64      `json:"version"`
		RefusMsg  string     `json:"refus"`
	} `json:"marques"`
}

func lirePers(t *testing.T, c *Client) tramePersonnes {
	t.Helper()
	var tr tramePersonnes
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

func TestNettoyerPersonnes(t *testing.T) {
	l := nettoyerPersonnes([]Personne{
		{ID: "p1", Convs: []string{"123", "m:9", "123", ""}, Principal: "m:9"},
		{ID: "p1", Convs: []string{"w:1", "s:2"}},                 // id en double
		{ID: "p2", Convs: []string{"m:9", "i:4"}},                 // m:9 déjà à p1 : reste une seule conv
		{ID: "", Convs: []string{"a", "b"}},                       // sans id
		{ID: "p3", Convs: []string{"w:1", "t:5"}, Principal: "x"}, // principal étranger
	})
	if len(l) != 2 || l[0].ID != "p1" || len(l[0].Convs) != 2 || l[0].Principal != "m:9" {
		t.Fatalf("p1 : %+v", l)
	}
	if l[1].ID != "p3" || l[1].Principal != "" {
		t.Fatalf("p3 : %+v", l)
	}
	r := nettoyerRefus([][]string{{"m:9", "123"}, {"123", "m:9"}, {"a"}, {"a", "a"}, {"b", ""}})
	if len(r) != 1 || r[0][0] != "123" || r[0][1] != "m:9" {
		t.Fatalf("refus : %+v", r)
	}
}

func TestPersonnes(t *testing.T) {
	s, c := serveurEssai(t)

	// Un premier lien : gardé, diffusé, toujours des listes.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"personnes":[{"id":"p1","convs":["123","m:9"],"principal":"123"}],"personnes_refus":[["i:4","123"]],"version":0}`))
	tr := lirePers(t, c)
	if tr.Marques.Version != 1 || len(tr.Marques.Personnes) != 1 || tr.Marques.Personnes[0].Principal != "123" ||
		len(tr.Marques.Refus) != 1 || tr.Marques.Refus[0][0] != "123" {
		t.Fatalf("premier lien : %+v", tr.Marques)
	}

	// Un ancien client (sans `personnes`) ne les efface pas.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"version":1}`))
	tr = lirePers(t, c)
	if tr.Marques.Version != 2 || len(tr.Marques.Personnes) != 1 || len(tr.Marques.Refus) != 1 {
		t.Fatalf("ancien client : %+v", tr.Marques)
	}

	// Un élément mal formé est sauté, pas toute la trame.
	s.recevoirMarques(c, json.RawMessage(`{"personnes":[{"id":"p1","convs":["123","m:9","w:15145551234@s.whatsapp.net"]},{"id":5},"x"],"version":2}`))
	tr = lirePers(t, c)
	if tr.Marques.Version != 3 || len(tr.Marques.Personnes) != 1 || len(tr.Marques.Personnes[0].Convs) != 3 {
		t.Fatalf("indulgence : %+v", tr.Marques)
	}

	// Client en retard : ses personnes gagnent pour leurs ids ; celles du
	// serveur restent, sans les conversations qu'il relie déjà ailleurs.
	s.recevoirMarques(c, json.RawMessage(`{"personnes":[{"id":"p2","convs":["m:9","i:4"]}],"personnes_refus":[["s:1","t:2"]],"version":1}`))
	tr = lirePers(t, c)
	if tr.Marques.Version != 4 || len(tr.Marques.Personnes) != 2 {
		t.Fatalf("union : %+v", tr.Marques)
	}
	p2, p1 := tr.Marques.Personnes[0], tr.Marques.Personnes[1]
	if p2.ID != "p2" || p1.ID != "p1" || len(p1.Convs) != 2 || p1.Convs[0] != "123" || p1.Convs[1] != "w:15145551234@s.whatsapp.net" {
		t.Fatalf("union, détail : %+v", tr.Marques.Personnes)
	}
	if len(tr.Marques.Refus) != 2 {
		t.Fatalf("union des refus : %+v", tr.Marques.Refus)
	}

	// Client en retard qui viderait les personnes : refusé.
	s.recevoirMarques(c, json.RawMessage(`{"personnes":[],"version":2}`))
	tr = lirePers(t, c)
	if tr.Marques.RefusMsg == "" || tr.Marques.Version != 4 || len(tr.Marques.Personnes) != 2 {
		t.Fatalf("garde-fou personnes : %+v", tr.Marques)
	}
	// De même pour les refus.
	s.recevoirMarques(c, json.RawMessage(`{"personnes_refus":[],"version":2}`))
	tr = lirePers(t, c)
	if tr.Marques.RefusMsg == "" || len(tr.Marques.Refus) != 2 {
		t.Fatalf("garde-fou refus : %+v", tr.Marques)
	}

	// Client à jour : il délie tout (liste vide exprès).
	s.recevoirMarques(c, json.RawMessage(`{"personnes":[],"personnes_refus":[],"version":4}`))
	tr = lirePers(t, c)
	if tr.Marques.Version != 5 || len(tr.Marques.Personnes) != 0 || len(tr.Marques.Refus) != 0 || tr.Marques.Personnes == nil {
		t.Fatalf("vidées : %+v", tr.Marques)
	}

	// Gardées sur disque.
	s.recevoirMarques(c, json.RawMessage(`{"personnes":[{"id":"p9","convs":["a","b"]}],"version":5}`))
	_ = lirePers(t, c)
	m := chargerMarques()
	if len(m.Personnes) != 1 || m.Personnes[0].ID != "p9" || m.PersonnesRefus == nil {
		t.Fatalf("disque : %+v", m)
	}

	// Un message reçu qui désarchive ne perd pas les personnes.
	s.recevoirMarques(c, json.RawMessage(`{"archives":["m:77"],"version":6}`))
	_ = lirePers(t, c)
	s.marques.ArchivesDepuis["m:77"] = 1
	s.desarchiverSiRecu("messenger", []byte(`{"ev":"message","item":{"conv":"77","from_me":false,"time":5}}`))
	if len(s.marques.Archives) != 0 || len(s.marques.Personnes) != 1 {
		t.Fatalf("désarchiver : %+v", s.marques)
	}
}

func TestPersonnesAncienFichier(t *testing.T) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := os.MkdirAll(serveurDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marquesPath(), []byte(`{"head":["a"],"version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := chargerMarques()
	if m.Personnes == nil || m.PersonnesRefus == nil || m.Version != 3 {
		t.Fatalf("ancien fichier : %+v", m)
	}
	tr := string(trameMarques(m, nil, ""))
	if !json.Valid([]byte(tr)) || !strings.Contains(tr, `"personnes":[]`) || !strings.Contains(tr, `"personnes_refus":[]`) {
		t.Fatalf("trame : %s", tr)
	}
}
