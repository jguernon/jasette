package main

import (
	"encoding/json"
	"testing"
)

func lireSons(t *testing.T, c *Client) string {
	t.Helper()
	var tr struct {
		Marques struct {
			Sons string `json:"sons_conversation"`
		} `json:"marques"`
	}
	select {
	case b := <-c.evs:
		if err := json.Unmarshal(b, &tr); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("aucune trame")
	}
	return tr.Marques.Sons
}

// sons_conversation : "bulle" par défaut ; une chaîne inconnue donne "bulle" ;
// un booléen (premiers clients) : vrai "bulle", faux "aucun" ; absent, rien
// ne change ; gardé sur disque.
func TestSonsConversation(t *testing.T) {
	s, c := serveurEssai(t)
	v := int64(0)
	envoyer := func(champ string) string {
		t.Helper()
		corps := `{"version":` + itoa(v)
		if champ != "" {
			corps += `,"sons_conversation":` + champ
		}
		s.recevoirMarques(c, json.RawMessage(corps+`}`))
		v = s.marques.Version
		return lireSons(t, c)
	}
	if j := envoyer(""); j != "bulle" {
		t.Fatalf("défaut : %q", j)
	}
	for _, cas := range []struct{ champ, attendu string }{
		{`"bois"`, "bois"},
		{``, "bois"}, // absent : rien ne change
		{`null`, "bois"},
		{`"verre"`, "verre"},
		{`false`, "aucun"},
		{`true`, "bulle"},
		{`"aucun"`, "aucun"},
		{`"klaxon"`, "bulle"},
		{`"bois"`, "bois"},
	} {
		if j := envoyer(cas.champ); j != cas.attendu {
			t.Fatalf("%s : %q, attendu %q", cas.champ, j, cas.attendu)
		}
	}
	if m := chargerMarques(); m.SonsConversation != "bois" {
		t.Fatalf("relu : %q", m.SonsConversation)
	}
	// Un fichier d'avant ce réglage : "bulle".
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":["a"],"version":3}`)); err != nil {
		t.Fatal(err)
	}
	if m := chargerMarques(); m.SonsConversation != "bulle" {
		t.Fatalf("ancien fichier : %q", m.SonsConversation)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
