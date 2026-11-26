package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Le classement de l'onglet Groupes (§5.13).
func TestGrandEspace(t *testing.T) {
	cas := []struct {
		nom   string
		pe    *peer
		large bool
		kind  string
	}{
		{"inconnu", nil, false, ""},
		{"personne", &peer{Kind: 'u'}, false, ""},
		{"groupe simple", &peer{Kind: 'g'}, false, ""},
		{"canal", &peer{Kind: 'c', Broadcast: true}, true, "channel"},
		{"supergroupe", &peer{Kind: 'c'}, true, "supergroup"},
	}
	for _, c := range cas {
		large, kind := grandEspace(c.pe)
		if large != c.large || kind != c.kind {
			t.Errorf("%s : %v %q, attendu %v %q", c.nom, large, kind, c.large, c.kind)
		}
	}
}

// Un groupe ordinaire ne change pas : aucun des nouveaux champs dans le JSON.
func TestConversationSansGrandEspace(t *testing.T) {
	b, _ := json.Marshal(Conversation{ID: "g1", Group: true})
	for _, champ := range []string{`"large"`, `"kind"`, `"mentions"`} {
		if strings.Contains(string(b), champ) {
			t.Errorf("%s ne devrait pas être émis : %s", champ, b)
		}
	}
	b, _ = json.Marshal(Message{ID: "g1/1"})
	if strings.Contains(string(b), "mentions_me") || strings.Contains(string(b), "reply_to_me") {
		t.Errorf("champs de mention émis à tort : %s", b)
	}
}
