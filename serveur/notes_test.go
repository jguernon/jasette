package main

import (
	"encoding/json"
	"strings"
	"testing"
)

type trameNotes struct {
	Marques struct {
		Contacts *bool    `json:"notes_contacts"`
		Musique  *string  `json:"notes_musique"`
		Format   string   `json:"notes_musique_format"`
		Services []string `json:"notes_services"`
		Version  int64    `json:"version"`
	} `json:"marques"`
}

func lireNotes(t *testing.T, c *Client) trameNotes {
	t.Helper()
	var tr trameNotes
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

// Les marques de « Quoi de neuf ? » : défauts, absent = rien ne change,
// notes_services normalisé (vide ou « tout » : ["tout"]), gardées sur disque.
func TestMarquesNotes(t *testing.T) {
	s, c := serveurEssai(t)
	s.recevoirMarques(c, json.RawMessage(`{"version":0}`))
	m := lireNotes(t, c).Marques
	if m.Contacts == nil || !*m.Contacts || m.Musique == nil || *m.Musique != "" || m.Format != "artiste" ||
		strings.Join(m.Services, ",") != "tout" {
		t.Fatalf("défauts : %+v", m)
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":0,"notes_contacts":false,"notes_musique":"Pixel 9","notes_musique_format":"genre","notes_services":["instagram","whatsapp","instagram",""]}`))
	m = lireNotes(t, c).Marques
	if *m.Contacts || *m.Musique != "Pixel 9" || m.Format != "genre" || strings.Join(m.Services, ",") != "instagram,whatsapp" {
		t.Fatalf("changées : %+v", m)
	}
	v := m.Version
	// Absents : rien ne change, même version.
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(v)+`}`))
	if m2 := lireNotes(t, c).Marques; m2.Version != v || *m2.Musique != "Pixel 9" || m2.Format != "genre" {
		t.Fatalf("absents : %+v", m2)
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(v)+`,"notes_services":["discord","tout"],"notes_musique_format":"bidon"}`))
	if m3 := lireNotes(t, c).Marques; strings.Join(m3.Services, ",") != "tout" || m3.Format != "artiste" {
		t.Fatalf("tout : %+v", m3)
	}
	s.recevoirMarques(c, json.RawMessage(`{"version":`+itoa(s.marques.Version)+`,"notes_services":[]}`))
	if m4 := lireNotes(t, c).Marques; strings.Join(m4.Services, ",") != "tout" {
		t.Fatalf("vide : %+v", m4)
	}
	if d := chargerMarques(); d.NotesContacts || d.NotesMusique != "Pixel 9" {
		t.Fatalf("disque : %+v", d)
	}
}

// note_moi est gardé et rejoué ; note_result est relayé sans être gardé ;
// notes (Instagram) est gardé, rejoué, puis oublié et plus relayé quand le
// réglage est désactivé.
func TestNotesRelais(t *testing.T) {
	s, c := serveurEssai(t)
	p := &Pont{svc: "instagram", srv: s, dernier: map[string][]byte{}, demandes: map[string]demande{},
		presence: map[string][]byte{}, cache: &Cache{}}
	s.ponts = map[string]*Pont{"instagram": p}

	p.recu([]byte(`{"ev":"note_moi","text":"Salut","time":5,"expires":9,"source":"main","audience":0}`))
	p.recu([]byte(`{"ev":"note_result","op":"set","ok":true,"req":"n1"}`))
	p.recu([]byte(`{"ev":"notes","items":[{"conv":"3001","user":"1","text":"🌴","time":3}],"time":7}`))
	if len(c.evs) != 3 {
		t.Fatalf("relayés : %d", len(c.evs))
	}
	for len(c.evs) > 0 {
		<-c.evs
	}
	s.mu.Lock()
	r := p.rejeu()
	s.mu.Unlock()
	tout := ""
	for _, t := range r {
		tout += string(t) + "\n"
	}
	if !strings.Contains(tout, `"ev":"note_moi"`) || !strings.Contains(tout, `"ev":"notes"`) || strings.Contains(tout, "note_result") {
		t.Fatalf("rejeu : %s", tout)
	}

	s.recevoirMarques(c, json.RawMessage(`{"notes_contacts":false,"version":0}`))
	<-c.evs
	if p.notes != nil {
		t.Fatal("notes pas oubliées")
	}
	p.recu([]byte(`{"ev":"notes","items":[],"time":8}`))
	if len(c.evs) != 0 || p.notes != nil {
		t.Fatal("notes relayées malgré le réglage")
	}
}
