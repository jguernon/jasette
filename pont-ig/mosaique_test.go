package main

import (
	"slices"
	"testing"
)

// La mosaïque d'un groupe sans image : pas moi, pas de doublon, ceux qui ont
// une photo d'abord, quatre au plus.
func TestMembresMosaique(t *testing.T) {
	p := &Pont{contacts: map[int64]*contact{}, me: 1}
	p.contacts[2] = &contact{Name: "Deux"}
	p.contacts[3] = &contact{Name: "Trois", Avatar: "https://exemple/3.jpg"}
	p.contacts[4] = &contact{Name: "Quatre", Avatar: "https://exemple/4.jpg"}
	p.contacts[6] = &contact{Name: "Six", Avatar: "https://exemple/6.jpg"}
	c := &convState{Key: 99, Group: true, Members: []int64{1, 2, 3, 3, 4, 5, 6, 0}}
	got := p.membresMosaique(c)
	if want := []int64{3, 4, 6, 2}; !slices.Equal(got, want) {
		t.Errorf("mosaïque %v, attendu %v", got, want)
	}
	c.Members = []int64{1, 2}
	if got := p.membresMosaique(c); !slices.Equal(got, []int64{2}) {
		t.Errorf("petit groupe : %v", got)
	}
}
