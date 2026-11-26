package main

import (
	"errors"

	"github.com/gotd/td/tgerr"
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

func TestRefusTelegram(t *testing.T) {
	if !refusTelegram(tgerr.New(420, "FLOOD_WAIT_30")) || refusTelegram(errHorsLigne) || refusTelegram(errors.New("EOF")) {
		t.Error("refusTelegram")
	}
}
