package main

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

// sender_id seulement pour les messages des autres dans un groupe ; la
// photo est demandée une fois par personne, et seulement avec une adresse.
func TestNoteAuteur(t *testing.T) {
	p := &Pont{convs: map[string]*convState{}, contacts: map[int64]*contact{}, me: 1,
		auteurs: map[int64]bool{}, fileAuteurs: make(chan int64, 8)}
	p.convs["9"] = &convState{Key: 9, Type: table.GROUP_THREAD}
	p.convs["5"] = &convState{Key: 5, Type: table.ONE_TO_ONE}
	p.convs["8"] = &convState{Key: 8, Type: table.ENCRYPTED_OVER_WA_GROUP}
	p.contacts[5] = &contact{Name: "Cinq", Avatar: "https://exemple/5.jpg"}
	p.contacts[6] = &contact{Name: "Six"}
	out := func(conv string, sender int64, moi bool) Message {
		r := &msgRow{Message: Message{ID: "x", Conv: conv, FromMe: moi}, SenderID: sender}
		return p.msgOut(r)
	}
	if m := out("9", 5, false); m.Auteur != "5" || m.Sender != "Cinq" {
		t.Errorf("groupe : %+v", m)
	}
	out("8", 5, false)
	if m := out("8", 6, false); m.Auteur != "6" {
		t.Errorf("groupe chiffré : %+v", m)
	}
	if m := out("5", 5, false); m.Auteur != "" {
		t.Errorf("fil à deux : %+v", m)
	}
	if m := out("9", 1, true); m.Auteur != "" {
		t.Errorf("mon message : %+v", m)
	}
	if len(p.fileAuteurs) != 1 || <-p.fileAuteurs != 5 {
		t.Errorf("demandes : %d", len(p.fileAuteurs))
	}
	b, _ := json.Marshal(out("9", 5, false))
	if !strings.Contains(string(b), `"sender_id":"5"`) {
		t.Errorf("JSON : %s", b)
	}
	// Le cache garde l'id numérique de msgRow, pas la chaîne.
	r := &msgRow{Message: Message{ID: "y", Conv: "9", Auteur: "5"}, SenderID: 5}
	b, _ = json.Marshal(r)
	var r2 msgRow
	if json.Unmarshal(b, &r2) != nil || r2.SenderID != 5 {
		t.Errorf("cache : %s", b)
	}
}
