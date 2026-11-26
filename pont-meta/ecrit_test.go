package main

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/util/exsync"
	waTypes "go.mau.fi/whatsmeow/types"
)

// La disponibilité de l'appareil chiffré : seulement à l'ouverture d'un fil
// chiffré par l'utilisateur, réglage activé ; indisponible après la fenêtre.
func TestDisponibiliteChiffree(t *testing.T) {
	var envois []waTypes.Presence
	p := &Pont{
		ctx:        context.Background(),
		convs:      map[string]*convState{},
		e2eeReady:  exsync.NewEvent(),
		presenceOn: true,
		waPresenceTest: func(pr waTypes.Presence) error {
			envois = append(envois, pr)
			return nil
		},
	}
	p.e2eeReady.Set()
	p.convs["111"] = &convState{Key: 111, Type: table.ENCRYPTED_OVER_WA_ONE_TO_ONE}
	p.convs["222"] = &convState{Key: 222, Type: table.ONE_TO_ONE}

	p.vueChiffree("222", false) // fil non chiffré
	p.vueChiffree("111", true)  // préchargement du serveur
	p.vueChiffree("999", false) // fil inconnu
	if len(envois) != 0 {
		t.Fatalf("rien attendu : %v", envois)
	}
	p.vueChiffree("111", false)
	p.vueChiffree("111", false)
	if len(envois) != 1 || envois[0] != waTypes.PresenceAvailable {
		t.Fatalf("une seule disponibilité attendue : %v", envois)
	}
	p.ecritExpire() // encore dans la fenêtre
	if len(envois) != 1 {
		t.Fatalf("trop tôt : %v", envois)
	}
	p.e2eeVue = time.Now().Add(-ecritFenetre - time.Second)
	p.ecritExpire()
	if len(envois) != 2 || envois[1] != waTypes.PresenceUnavailable {
		t.Fatalf("indisponible attendu : %v", envois)
	}

	// Reconnexion pendant la fenêtre : la disponibilité revient.
	p.vueChiffree("111", false)
	p.ecritConnecte()
	if len(envois) != 4 || envois[3] != waTypes.PresenceAvailable {
		t.Fatalf("rétablie attendue : %v", envois)
	}

	// Réglage désactivé : indisponible, puis plus rien.
	p.setPresence(false)
	p.ecritExpire()
	p.vueChiffree("111", false)
	p.ecritConnecte()
	if len(envois) != 5 || envois[4] != waTypes.PresenceUnavailable {
		t.Fatalf("réglage désactivé : %v", envois)
	}
}
