package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// La présence du flux arrive sur la conversation individuelle de la personne
// (id du fil, sans préfixe : l'interface ajoute « i: »).
func TestApplyPresence(t *testing.T) {
	var buf bytes.Buffer
	outMu.Lock()
	avant := out
	out = json.NewEncoder(&buf)
	outMu.Unlock()
	defer func() { outMu.Lock(); out = avant; outMu.Unlock() }()
	p := &Pont{
		me:         1,
		presenceOn: true,
		presence:   map[string]presenceState{},
		convs: map[string]*convState{
			"340282366841710300949128100000000001": {Key: 1, Members: []int64{1, 500}},
			"340282366841710300949128100000000002": {Key: 2, Group: true, Members: []int64{1, 500, 600}},
			"340282366841710300949128100000000003": {Key: 3, Members: []int64{600}},
		},
	}
	if n := p.applyPresence([]majPresence{{ID: 500, Actif: true}, {ID: 700, Actif: true}, {ID: 1, Actif: true}}, false); n != 1 {
		t.Errorf("rattachées = %d", n)
	}
	var ev struct {
		Ev   string `json:"ev"`
		Item struct {
			Conv   string `json:"conv"`
			User   string `json:"user"`
			Online bool   `json:"online"`
		} `json:"item"`
	}
	dec := json.NewDecoder(&buf)
	if err := dec.Decode(&ev); err != nil {
		t.Fatal(err)
	}
	if ev.Ev != "presence" || ev.Item.Conv != "340282366841710300949128100000000001" || ev.Item.User != "500" || !ev.Item.Online {
		t.Errorf("événement %+v", ev)
	}
	if dec.More() {
		t.Error("un seul événement attendu")
	}
	// Même état : rien de nouveau.
	p.applyPresence([]majPresence{{ID: 500, Actif: true}}, false)
	if buf.Len() != 0 {
		t.Errorf("répétition émise : %s", buf.String())
	}
	// Tout l'état sans la personne : hors ligne.
	p.applyPresence([]majPresence{{ID: 600, Derniere: 42}}, true)
	n := 0
	for dec = json.NewDecoder(&buf); dec.More(); n++ {
		if err := dec.Decode(&ev); err != nil {
			t.Fatal(err)
		}
		if ev.Item.Conv == "340282366841710300949128100000000001" && ev.Item.Online {
			t.Error("500 devrait passer hors ligne")
		}
	}
	if n != 2 {
		t.Errorf("%d événements, 2 attendus", n)
	}
}
