package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func lirePresence(t *testing.T, c *Client) *bool {
	t.Helper()
	var tr struct {
		Marques struct {
			Presence *bool `json:"presence_en_ligne"`
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
	return tr.Marques.Presence
}

// presence_en_ligne : vrai par défaut (et pour un ancien fichier) ; un ancien
// client (sans le champ) ne le change pas ; gardé sur disque.
func TestPresenceEnLigneMarque(t *testing.T) {
	s, c := serveurEssai(t)
	if !s.marques.PresenceEnLigne {
		t.Fatal("défaut : faux")
	}
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"version":0}`))
	if p := lirePresence(t, c); p == nil || !*p {
		t.Fatalf("défaut envoyé : %v", p)
	}
	s.recevoirMarques(c, json.RawMessage(`{"head":["a"],"presence_en_ligne":false,"version":1}`))
	if p := lirePresence(t, c); p == nil || *p {
		t.Fatal("désactivé")
	}
	s.recevoirMarques(c, json.RawMessage(`{"head":["a","b"],"version":2}`))
	if p := lirePresence(t, c); *p || s.marques.Version != 3 {
		t.Fatal("un ancien client l'a réactivé")
	}
	if m := chargerMarques(); m.PresenceEnLigne {
		t.Fatal("relu : vrai")
	}
	// Un fichier d'avant ce réglage : vrai.
	t.Setenv("JASETTE_DATA", t.TempDir())
	if err := ecrireAtomique(marquesPath(), []byte(`{"head":["a"],"version":3}`)); err != nil {
		t.Fatal(err)
	}
	if m := chargerMarques(); !m.PresenceEnLigne {
		t.Fatal("ancien fichier : faux")
	}
}

// Les événements presence : relayés, gardés (le dernier par conversation),
// rejoués après les listes ; oubliés si le pont s'arrête ou si le réglage est
// désactivé, et alors plus relayés.
func TestPresenceRelais(t *testing.T) {
	s, c := serveurEssai(t)
	p := &Pont{svc: "whatsapp", srv: s, dernier: map[string][]byte{}, demandes: map[string]demande{},
		presence: map[string][]byte{}, cache: &Cache{}}
	s.ponts = map[string]*Pont{"whatsapp": p}

	p.recu([]byte(`{"ev":"presence","item":{"conv":"1@s.whatsapp.net","user":"1@s.whatsapp.net","online":true,"last_active":5}}`))
	p.recu([]byte(`{"ev":"presence","item":{"conv":"1@s.whatsapp.net","user":"1@s.whatsapp.net","online":false,"last_active":9}}`))
	if len(c.evs) != 2 {
		t.Fatalf("relayés : %d", len(c.evs))
	}
	<-c.evs
	<-c.evs
	s.mu.Lock()
	r := p.rejeu()
	s.mu.Unlock()
	if len(r) != 1 || !strings.Contains(string(r[0]), `"online":false`) || !strings.HasPrefix(string(r[0]), `{"svc":"whatsapp","ev":`) {
		t.Fatalf("rejeu : %q", r)
	}

	// Désactivé : oublié, et plus relayé.
	s.recevoirMarques(c, json.RawMessage(`{"presence_en_ligne":false,"version":0}`))
	<-c.evs
	if len(p.presence) != 0 {
		t.Fatal("pas oubliée")
	}
	p.recu([]byte(`{"ev":"presence","item":{"conv":"2@s.whatsapp.net","online":true}}`))
	if len(c.evs) != 0 || len(p.presence) != 0 {
		t.Fatal("relayée malgré le réglage")
	}
}
