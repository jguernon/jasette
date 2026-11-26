package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

type evPresence struct {
	Ev   string `json:"ev"`
	Item struct {
		Conv   string `json:"conv"`
		User   string `json:"user"`
		Online bool   `json:"online"`
		Last   int64  `json:"last_active"`
	} `json:"item"`
}

func capturer(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	outMu.Lock()
	avant := out
	out = json.NewEncoder(&buf)
	outMu.Unlock()
	t.Cleanup(func() { outMu.Lock(); out = avant; outMu.Unlock() })
	return &buf
}

func lire(t *testing.T, buf *bytes.Buffer) []evPresence {
	var l []evPresence
	for dec := json.NewDecoder(buf); dec.More(); {
		var e evPresence
		if err := dec.Decode(&e); err != nil {
			t.Fatal(err)
		}
		l = append(l, e)
	}
	return l
}

// Le flux : l'id du contact donne la conversation individuelle (fil chiffré
// compris), id sans préfixe (l'interface ajoute « m: »).
func TestApplyPresenceFlux(t *testing.T) {
	buf := capturer(t)
	p := &Pont{
		me:           1,
		presenceOn:   true,
		presence:     map[int64]presenceRow{},
		presenceHors: map[int64]int64{},
		threadMap:    map[int64]int64{300: 9300},
		openThread:   map[int64]int64{},
		convs: map[string]*convState{
			"200":  {Key: 200, Type: table.ONE_TO_ONE, Members: []int64{1, 200}},
			"9300": {Key: 9300, Type: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, Members: []int64{1, 300}},
			"777":  {Key: 777, Type: table.GROUP_THREAD, Members: []int64{1, 200, 300, 400}},
		},
	}
	n := p.applyPresenceFlux([]majPresence{{ID: 200, Actif: true}, {ID: 300, Derniere: 5000}, {ID: 400, Actif: true}, {ID: 1, Actif: true}}, false)
	if n != 2 {
		t.Errorf("rattachées = %d", n)
	}
	evs := lire(t, buf)
	if len(evs) != 2 || evs[0].Item.Conv != "200" || !evs[0].Item.Online || evs[1].Item.Conv != "9300" || evs[1].Item.Online || evs[1].Item.Last != 5000 || evs[1].Item.User != "300" {
		t.Fatalf("événements %+v", evs)
	}
	// Rien de neuf : rien d'émis.
	p.applyPresenceFlux([]majPresence{{ID: 200, Actif: true}, {ID: 300, Derniere: 5000}}, false)
	if evs := lire(t, buf); len(evs) != 0 {
		t.Errorf("répétitions %+v", evs)
	}
	// Tout l'état, sans 200 : hors ligne.
	p.applyPresenceFlux(nil, true)
	if evs := lire(t, buf); len(evs) != 1 || evs[0].Item.Conv != "200" || evs[0].Item.Online {
		t.Errorf("événements %+v", evs)
	}
}
