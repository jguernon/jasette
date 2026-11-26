package main

import (
	"errors"
	"slices"
	"testing"
)

// La mosaïque d'un groupe sans image : pas moi, pas de doublon, ceux qui ont
// une photo d'abord, quatre au plus.
func TestChoisirMosaique(t *testing.T) {
	photos := map[string]string{"c": "/c.jpg", "d": "/d.jpg", "f": "/f.jpg"}
	moi := func(j string) bool { return j == "moi" }
	photo := func(j string) string { return photos[j] }
	got := choisirMosaique([]string{"moi", "b", "c", "c", "d", "e", "f", ""}, moi, photo)
	if want := []string{"c", "d", "f", "b"}; !slices.Equal(got, want) {
		t.Errorf("mosaïque %v, attendu %v", got, want)
	}
	if got := choisirMosaique([]string{"moi", "b"}, moi, photo); !slices.Equal(got, []string{"b"}) {
		t.Errorf("petit groupe : %v", got)
	}
}

func TestRefusSignal(t *testing.T) {
	if !refusSignal(errors.New("unexpected status code 429")) || refusSignal(errPasConnecte) || refusSignal(errors.New("connexion coupée")) || refusSignal(errors.New("contact 8f0c4013-4290-a401-b429-0a2b3c4d5e6f : EOF")) || !refusSignal(errors.New("Rate limited")) {
		t.Error("refusSignal")
	}
}
