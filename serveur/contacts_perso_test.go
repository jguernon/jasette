package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type trameContacts struct {
	Marques struct {
		Head    []string                `json:"head"`
		Perso   map[string]ContactPerso `json:"contacts_perso"`
		Version int64                   `json:"version"`
		Refus   string                  `json:"refus"`
	} `json:"marques"`
}

func lireContacts(t *testing.T, c *Client) trameContacts {
	t.Helper()
	var tr trameContacts
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

func TestNettoyerContactsPerso(t *testing.T) {
	m := nettoyerContactsPerso(map[string]ContactPerso{
		"123":  {Nom: "  Maman  ", Photo: "/m/contacts/ab.jpg"},
		"m:9":  {Nom: "", Photo: ""},                         // vide : retirée
		"":     {Nom: "x"},                                   // sans id
		"w:1":  {Nom: strings.Repeat("é", 200)},              // nom démesuré, sans photo : retirée
		"s:+1": {Nom: "a\nb", Photo: "/p.png", Google: true}, // nom à saut de ligne : vidé
	})
	if len(m) != 2 || m["123"].Nom != "Maman" || m["s:+1"].Nom != "" || m["s:+1"].Photo != "/p.png" || !m["s:+1"].Google {
		t.Fatalf("nettoyage : %+v", m)
	}
	if _, ok := lireContactsPerso(json.RawMessage(`null`)); ok {
		t.Fatal("null : rien ne change")
	}
	if _, ok := lireContactsPerso(json.RawMessage(`[1]`)); ok {
		t.Fatal("pas un objet : rien ne change")
	}
	l, ok := lireContactsPerso(json.RawMessage(`{"a":{"nom":"A","photo":3},"b":"x","c":{"nom":"C"}}`))
	if !ok || len(l) != 2 || l["a"].Nom != "A" || l["a"].Photo != "" || l["c"].Nom != "C" {
		t.Fatalf("lecture indulgente : %+v", l)
	}
}

func TestContactsPerso(t *testing.T) {
	s, c := serveurEssai(t)

	// Posé : gardé, diffusé, toujours un objet.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"contacts_perso":{"123":{"nom":"Maman","photo":"/m/contacts/ab.jpg","google":true}},"version":0}`))
	tr := lireContacts(t, c)
	if tr.Marques.Version != 1 || tr.Marques.Perso["123"].Nom != "Maman" || !tr.Marques.Perso["123"].Google {
		t.Fatalf("pose : %+v", tr.Marques)
	}

	// Un ancien client (sans le champ) ne les efface pas.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"version":1}`))
	tr = lireContacts(t, c)
	if tr.Marques.Version != 2 || len(tr.Marques.Perso) != 1 {
		t.Fatalf("ancien client : %+v", tr.Marques)
	}

	// Client en retard : ses entrées gagnent, celles du serveur restent.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"contacts_perso":{"m:9":{"nom":"Steph"}},"version":1}`))
	tr = lireContacts(t, c)
	if tr.Marques.Version != 3 || len(tr.Marques.Perso) != 2 || tr.Marques.Perso["m:9"].Nom != "Steph" {
		t.Fatalf("union : %+v", tr.Marques)
	}

	// Client en retard qui viderait tout : refusé.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"contacts_perso":{},"version":1}`))
	tr = lireContacts(t, c)
	if tr.Marques.Refus == "" || len(tr.Marques.Perso) != 2 {
		t.Fatalf("garde-fou : %+v", tr.Marques)
	}

	// « Rétablir » d'un client à jour : la clé disparaît.
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"contacts_perso":{"m:9":{"nom":"Steph"}},"version":3}`))
	tr = lireContacts(t, c)
	if tr.Marques.Version != 4 || len(tr.Marques.Perso) != 1 {
		t.Fatalf("rétablir : %+v", tr.Marques)
	}

	// Gardé sur disque, rejoué tel quel.
	m := chargerMarques()
	if m.Version != 4 || m.ContactsPerso["m:9"].Nom != "Steph" {
		t.Fatalf("relu : %+v", m)
	}
	var rejeu trameContacts
	_ = json.Unmarshal(trameMarques(m, nil, ""), &rejeu)
	if rejeu.Marques.Perso["m:9"].Nom != "Steph" {
		t.Fatalf("rejeu : %s", trameMarques(m, nil, ""))
	}
}

// Un ancien marques.json (sans contacts_perso) : un objet vide, jamais null.
func TestContactsPersoAncienFichier(t *testing.T) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := os.MkdirAll(serveurDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(marquesPath(), []byte(`{"head":["a"],"version":3}`), 0o600)
	m := chargerMarques()
	if m.ContactsPerso == nil || !strings.Contains(string(trameMarques(m, nil, "")), `"contacts_perso":{}`) {
		t.Fatalf("ancien fichier : %s", trameMarques(m, nil, ""))
	}
}

// `put` avec "garder":"contact" : la photo va sous <médias>/contacts, nommée
// par son contenu ; ni le dépôt ni une autre extension ne restent.
func TestPutPhotoContact(t *testing.T) {
	s, c := serveurEssai(t)
	t.Setenv("JASETTE_MEDIA", filepath.Join(t.TempDir(), "media"))
	s.depots = depotsDir()
	c.depots = map[int64]*depot{}
	c.reponses = make(chan envoi, 8)
	donnees := base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff une fausse photo"))
	lireDepose := func() map[string]any {
		t.Helper()
		var r struct {
			Depose map[string]any `json:"depose"`
		}
		select {
		case e := <-c.reponses:
			_ = json.Unmarshal(e.t, &r)
		case b := <-c.evs:
			_ = json.Unmarshal(b, &r)
		case <-time.After(time.Second):
			t.Fatal("aucune réponse")
		}
		return r.Depose
	}
	c.deposer(1, "visage.jpeg", donnees, true, "contact")
	r := lireDepose()
	chemin, _ := r["chemin"].(string)
	if !strings.HasPrefix(chemin, filepath.Join(mediaDir(), "contacts")+"/") || !strings.HasSuffix(chemin, ".jpg") {
		t.Fatalf("chemin : %+v", r)
	}
	if _, err := os.Stat(chemin); err != nil {
		t.Fatal(err)
	}
	if e, _ := os.ReadDir(s.depots); len(e) != 0 {
		t.Fatalf("dépôt resté : %v", e)
	}
	// Le même contenu : le même fichier.
	c.deposer(2, "autre.jpg", donnees, true, "contact")
	if r2 := lireDepose(); r2["chemin"] != chemin {
		t.Fatalf("même contenu : %+v", r2)
	}
	// Pas une image : refusé.
	c.deposer(3, "notes.txt", donnees, true, "contact")
	if r3 := lireDepose(); r3["erreur"] == nil {
		t.Fatalf("pas une image : %+v", r3)
	}

	// Le ménage : une photo que plus rien ne nomme, vieille d'un jour, part ;
	// celle qui est nommée reste.
	vieux := time.Now().Add(-48 * time.Hour)
	orpheline := filepath.Join(mediaDir(), "contacts", "0000.jpg")
	_ = os.WriteFile(orpheline, []byte("x"), 0o600)
	_ = os.Chtimes(orpheline, vieux, vieux)
	_ = os.Chtimes(chemin, vieux, vieux)
	menagePhotosContacts(map[string]ContactPerso{"123": {Photo: chemin}})
	if _, err := os.Stat(orpheline); err == nil {
		t.Fatal("orpheline restée")
	}
	if _, err := os.Stat(chemin); err != nil {
		t.Fatal("photo nommée effacée")
	}
}
