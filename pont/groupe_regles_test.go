package main

import (
	"fmt"
	"testing"
)

func TestMembresGroupe(t *testing.T) {
	m, err := membresGroupe([]string{" a ", "b", "a", "", "c"})
	if err != nil || len(m) != 3 || m[0] != "a" || m[1] != "b" || m[2] != "c" {
		t.Fatalf("membres : %v %v", m, err)
	}
	if _, err := membresGroupe([]string{"a", " a "}); err == nil {
		t.Fatal("une seule personne doit être refusée")
	}
	trop := make([]string, maxMembresGroupe+1)
	for i := range trop {
		trop[i] = fmt.Sprint(i)
	}
	if _, err := membresGroupe(trop); err == nil {
		t.Fatal("trop de personnes doit être refusé")
	}
}

func TestNomGroupe(t *testing.T) {
	if n := nomGroupe("  Les   amis ", []string{"Ana"}, 0); n != "Les amis" {
		t.Fatalf("nom choisi : %q", n)
	}
	if n := nomGroupe("", []string{"Ana", " ", "Bob"}, 0); n != "Ana, Bob" {
		t.Fatalf("nom par défaut : %q", n)
	}
	if n := nomGroupe("", []string{"Anabelle", "Bob"}, 5); n != "Anab…" || len([]rune(n)) != 5 {
		t.Fatalf("nom coupé : %q", n)
	}
}
