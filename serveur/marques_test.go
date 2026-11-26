package main

import (
	"encoding/json"
	"testing"
)

// Un serveur d'essai, ses données dans un dossier temporaire du test, et
// un client dont on lit les trames.
func serveurEssai(t *testing.T) (*Serveur, *Client) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	s := &Serveur{clients: map[*Client]struct{}{}}
	s.marques = chargerMarques()
	c := &Client{srv: s, nom: "essai", evs: make(chan []byte, 16)}
	s.clients[c] = struct{}{}
	return s, c
}

type trameEssai struct {
	Marques struct {
		Privees []string          `json:"privees"`
		Moods   map[string]string `json:"moods"`
		Version int64             `json:"version"`
		Refus   string            `json:"refus"`
	} `json:"marques"`
}

func lireTrame(t *testing.T, c *Client) trameEssai {
	t.Helper()
	var tr trameEssai
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

func TestMoods(t *testing.T) {
	s, c := serveurEssai(t)

	// Un mood posé : gardé, diffusé, toujours un objet.
	s.recevoirMarques(c, json.RawMessage(`{"privees":["w:1"],"moods":{"m:9":"amour","":"x","w:1":""},"version":0}`))
	tr := lireTrame(t, c)
	if tr.Marques.Version != 1 || len(tr.Marques.Moods) != 1 || tr.Marques.Moods["m:9"] != "amour" {
		t.Fatalf("après pose : %+v", tr.Marques)
	}

	// Un ancien client (sans `moods`) ne les efface pas.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"privees":["w:1"],"version":1}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 2 || tr.Marques.Moods["m:9"] != "amour" {
		t.Fatalf("ancien client : %+v", tr.Marques)
	}

	// Un client à jour change un mood et en retire un autre (objet sans la clé).
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"privees":["w:1"],"moods":{"m:9":"ocean","s:+1":"soleil"},"version":2}`))
	tr = lireTrame(t, c)
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"privees":["w:1"],"moods":{"s:+1":"soleil"},"version":3}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 4 || len(tr.Marques.Moods) != 1 || tr.Marques.Moods["s:+1"] != "soleil" {
		t.Fatalf("retrait : %+v", tr.Marques)
	}

	// Client en retard : ses moods gagnent pour ses ids, ceux du serveur restent.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"privees":["w:1"],"moods":{"s:+1":"menthe","i:5":"aurore"},"version":1}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 5 || tr.Marques.Moods["s:+1"] != "menthe" || tr.Marques.Moods["i:5"] != "aurore" {
		t.Fatalf("union : %+v", tr.Marques)
	}

	// Client en retard qui viderait les moods : refusé.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"privees":["w:1"],"moods":{},"version":2}`))
	tr = lireTrame(t, c)
	if tr.Marques.Refus == "" || len(tr.Marques.Moods) != 2 {
		t.Fatalf("garde-fou : %+v", tr.Marques)
	}

	// Gardés sur disque, relus au redémarrage.
	m := chargerMarques()
	if m.Version != 5 || m.Moods["i:5"] != "aurore" || len(m.Privees) != 1 {
		t.Fatalf("relu : %+v", m)
	}

	// Un message reçu qui désarchive garde les moods.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"archives":["m:7"],"privees":["w:1"],"moods":{"s:+1":"menthe","i:5":"aurore"},"version":5}`))
	lireTrame(t, c)
	s.desarchiverSiRecu("messenger", []byte(`{"item":{"conv":"7","time":99999999999999}}`))
	if len(s.marques.Archives) != 0 || len(s.marques.Moods) != 2 {
		t.Fatalf("désarchivage : %+v", s.marques)
	}
}

// Un fichier d'avant les moods se lit (moods vide, jamais nul).
func TestMoodsAncienFichier(t *testing.T) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":["a"],"favoris":[],"epingles":[],"archives":[],"privees":[],"version":3}`)); err != nil {
		t.Fatal(err)
	}
	m := chargerMarques()
	if m.Version != 3 || m.Moods == nil || len(m.Moods) != 0 {
		t.Fatalf("%+v", m)
	}
	b, _ := json.Marshal(trameMarques(m, nil, ""))
	if len(b) == 0 {
		t.Fatal("trame vide")
	}
}
