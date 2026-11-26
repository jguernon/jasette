package main

import (
	"encoding/json"
	"strings"
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
		Privees  []string          `json:"privees"`
		Moods    map[string]string `json:"moods"`
		Emojis   map[string]string `json:"emojis"`
		Archives []string          `json:"archives"`
		Garder   *bool             `json:"garder_archives"`
		Cacher   *int              `json:"cacher_apres_jours"`
		Version  int64             `json:"version"`
		Refus    string            `json:"refus"`
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

// L'emoji rapide : posé, gardé par un ancien client, borné à 32 octets,
// retiré, protégé d'un client en retard, relu sur disque.
func TestEmojis(t *testing.T) {
	s, c := serveurEssai(t)

	long := `"w:2":"` + strings.Repeat("😀", 9) + `"` // 36 octets : refusé
	s.recevoirMarques(c, json.RawMessage(`{"emojis":{"m:9":"👍","":"x","s:1":"",`+long+`},"version":0}`))
	tr := lireTrame(t, c)
	if tr.Marques.Version != 1 || len(tr.Marques.Emojis) != 1 || tr.Marques.Emojis["m:9"] != "👍" {
		t.Fatalf("après pose : %+v", tr.Marques)
	}
	if tr.Marques.Moods == nil {
		t.Fatalf("moods nul : %+v", tr.Marques)
	}

	// Un ancien client (sans `emojis`) ne les efface pas.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"moods":{"m:9":"amour"},"version":1}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 2 || tr.Marques.Emojis["m:9"] != "👍" || tr.Marques.Moods["m:9"] != "amour" {
		t.Fatalf("ancien client : %+v", tr.Marques)
	}

	// Un emoji composé (famille, 25 octets) passe ; l'autre est retiré.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"emojis":{"i:5":"👨‍👩‍👧"},"version":2}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 3 || len(tr.Marques.Emojis) != 1 || tr.Marques.Emojis["i:5"] != "👨‍👩‍👧" {
		t.Fatalf("retrait : %+v", tr.Marques)
	}

	// Client en retard : union, ses emojis gagnent pour ses ids.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"emojis":{"i:5":"🔥","x:1":"🎉"},"version":1}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 4 || tr.Marques.Emojis["i:5"] != "🔥" || tr.Marques.Emojis["x:1"] != "🎉" {
		t.Fatalf("union : %+v", tr.Marques)
	}

	// Client en retard qui les viderait : refusé.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"emojis":{},"version":2}`))
	tr = lireTrame(t, c)
	if tr.Marques.Refus == "" || len(tr.Marques.Emojis) != 2 {
		t.Fatalf("garde-fou : %+v", tr.Marques)
	}

	// Gardés sur disque ; un fichier d'avant les emojis se lit aussi.
	if m := chargerMarques(); m.Version != 4 || m.Emojis["x:1"] != "🎉" {
		t.Fatalf("relu : %+v", m)
	}
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":["a"],"moods":{},"version":3}`)); err != nil {
		t.Fatal(err)
	}
	if m := chargerMarques(); m.Emojis == nil || len(m.Emojis) != 0 {
		t.Fatalf("ancien fichier : %+v", m)
	}
}

// garder_archives : faux par défaut (et pour un ancien fichier) ; vrai, un
// message reçu ne désarchive plus ; un ancien client (sans le champ) ne le
// remet pas à faux ; gardé sur disque.
func TestGarderArchives(t *testing.T) {
	s, c := serveurEssai(t)
	recu := []byte(`{"item":{"conv":"7","time":99999999999999}}`)

	s.recevoirMarques(c, json.RawMessage(`{"archives":["m:7","s:1"],"version":0}`))
	tr := lireTrame(t, c)
	if tr.Marques.Garder == nil || *tr.Marques.Garder {
		t.Fatalf("défaut : %+v", tr.Marques)
	}

	// Activé : la conversation reste dans les archives, sans nouvelle version.
	s.recevoirMarques(c, json.RawMessage(`{"archives":["m:7","s:1"],"garder_archives":true,"version":1}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 2 || tr.Marques.Garder == nil || !*tr.Marques.Garder {
		t.Fatalf("activé : %+v", tr.Marques)
	}
	s.desarchiverSiRecu("messenger", recu)
	if len(s.marques.Archives) != 2 || s.marques.Version != 2 {
		t.Fatalf("gardée : %+v", s.marques)
	}

	// Un ancien client (sans le champ) ne le désactive pas.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"archives":["m:7","s:1"],"version":2}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 3 || !*tr.Marques.Garder {
		t.Fatalf("ancien client : %+v", tr.Marques)
	}

	// Relu sur disque.
	if m := chargerMarques(); !m.GarderArchives || m.Version != 3 {
		t.Fatalf("relu : %+v", m)
	}

	// Désactivé : la règle d'avant revient.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"archives":["m:7","s:1"],"garder_archives":false,"version":3}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 4 || *tr.Marques.Garder {
		t.Fatalf("désactivé : %+v", tr.Marques)
	}
	s.desarchiverSiRecu("messenger", recu)
	if len(s.marques.Archives) != 1 || s.marques.Archives[0] != "s:1" || s.marques.GarderArchives {
		t.Fatalf("désarchivage : %+v", s.marques)
	}

	// Un fichier d'avant ce réglage : faux.
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":["a"],"archives":["m:7"],"version":3}`)); err != nil {
		t.Fatal(err)
	}
	if m := chargerMarques(); m.GarderArchives {
		t.Fatalf("ancien fichier : %+v", m)
	}
}

// cacher_apres_jours : 0 par défaut (et pour un ancien fichier) ; partagé ;
// un ancien client (sans le champ) ne le remet pas à 0 ; borné ; gardé sur
// disque. Le serveur ne filtre rien lui-même.
func TestCacherApresJours(t *testing.T) {
	s, c := serveurEssai(t)

	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"version":0}`))
	tr := lireTrame(t, c)
	if tr.Marques.Cacher == nil || *tr.Marques.Cacher != 0 {
		t.Fatalf("défaut : %+v", tr.Marques)
	}

	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"cacher_apres_jours":30,"version":1}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 2 || tr.Marques.Cacher == nil || *tr.Marques.Cacher != 30 {
		t.Fatalf("30 jours : %+v", tr.Marques)
	}

	// Un ancien client (sans le champ) ne le remet pas à 0.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"version":2}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 3 || *tr.Marques.Cacher != 30 {
		t.Fatalf("ancien client : %+v", tr.Marques)
	}

	// Relu sur disque.
	if m := chargerMarques(); m.CacherApresJours != 30 || m.Version != 3 {
		t.Fatalf("relu : %+v", m)
	}

	// Seul le réglage change : c'est un changement (nouvelle version).
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"cacher_apres_jours":365,"version":3}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 4 || *tr.Marques.Cacher != 365 {
		t.Fatalf("1 an : %+v", tr.Marques)
	}

	// Une valeur absurde : 0 (tout afficher).
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"cacher_apres_jours":-5,"version":4}`))
	tr = lireTrame(t, c)
	if tr.Marques.Version != 5 || *tr.Marques.Cacher != 0 {
		t.Fatalf("négatif : %+v", tr.Marques)
	}

	// Un fichier d'avant ce réglage : 0.
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":["a"],"garder_archives":true,"version":3}`)); err != nil {
		t.Fatal(err)
	}
	if m := chargerMarques(); m.CacherApresJours != 0 || !m.GarderArchives {
		t.Fatalf("ancien fichier : %+v", m)
	}
}

// groupes_inbox (une marque abandonnée) : ignorée sans erreur, d'un client
// comme d'un ancien marques.json.
func TestGroupesInboxIgnoree(t *testing.T) {
	s, c := serveurEssai(t)
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"groupes_inbox":["d:g5:11"],"version":0}`))
	tr := lireTrame(t, c)
	if tr.Marques.Version != 1 || len(s.marques.Head) != 1 {
		t.Fatalf("marques : %+v", s.marques)
	}
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":["a"],"groupes_inbox":["d:g5:11"],"version":3}`)); err != nil {
		t.Fatal(err)
	}
	if m := chargerMarques(); m.Version != 3 || len(m.Head) != 1 {
		t.Fatalf("ancien fichier : %+v", m)
	}
}
