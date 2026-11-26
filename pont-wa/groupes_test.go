package main

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

// Le classement de l'onglet Groupes (§5.13).
func TestClasseEspace(t *testing.T) {
	cas := []struct {
		nom string
		c   convState
		e   espace
	}{
		{"personne", convState{}, espace{}},
		{"groupe ordinaire", convState{Group: true, Announce: true}, espace{}},
		{"communauté", convState{Group: true, Community: true}, espace{Large: true, Kind: "community"}},
		{"groupe rattaché", convState{Group: true, Parent: "1@g.us"},
			espace{Large: true, Kind: "community_group", Parent: "1@g.us", ParentName: "Quartier"}},
		{"annonces", convState{Group: true, Parent: "1@g.us", Announce: true},
			espace{Large: true, Kind: "community_group", Announce: true, Parent: "1@g.us", ParentName: "Quartier"}},
	}
	for _, c := range cas {
		if e := classeEspace(&c.c, "Quartier"); e != c.e {
			t.Errorf("%s : %+v, attendu %+v", c.nom, e, c.e)
		}
	}
}

func TestMentionneMoi(t *testing.T) {
	moi := types.NewADJID("15145550000", 0, 3) // avec un appareil
	lid := types.NewJID("987654321", types.HiddenUserServer)
	if !mentionneMoi([]string{"111@s.whatsapp.net", "15145550000@s.whatsapp.net"}, moi, lid) {
		t.Error("mon numéro mentionné non vu")
	}
	if !mentionneMoi([]string{"987654321@lid"}, moi, lid) {
		t.Error("mon LID mentionné non vu")
	}
	if mentionneMoi([]string{"111@s.whatsapp.net", "pas un jid@@", ""}, moi, lid) {
		t.Error("mention d'un autre prise pour moi")
	}
	if mentionneMoi([]string{"987654321@lid"}, moi, types.EmptyJID) {
		t.Error("LID inconnu : aucune mention possible")
	}
}

// Un groupe ordinaire ne change pas : aucun des nouveaux champs dans le JSON.
func TestConversationOrdinaire(t *testing.T) {
	b, _ := json.Marshal(Conversation{ID: "1@g.us", Group: true})
	for _, champ := range []string{"large", "kind", "announce", "parent"} {
		if strings.Contains(string(b), `"`+champ) {
			t.Errorf("%s émis à tort : %s", champ, b)
		}
	}
}
