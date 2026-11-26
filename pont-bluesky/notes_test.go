package main

import "testing"

// La description change, tout le reste du profil (avatar…) reste.
func TestProfilAvecDescription(t *testing.T) {
	v := map[string]any{"$type": collectionProfil, "displayName": "Jo", "description": "ma bio",
		"avatar": map[string]any{"$type": "blob", "ref": map[string]any{"$link": "bafk"}}}
	n := profilAvecDescription(v, "🎵 Écoute Daft Punk")
	if n["description"] != "🎵 Écoute Daft Punk" || n["displayName"] != "Jo" || n["avatar"] == nil || v["description"] != "ma bio" {
		t.Fatalf("%v", n)
	}
	if _, ok := profilAvecDescription(v, "")["description"]; ok {
		t.Fatal("description vide gardée")
	}
	if profilAvecDescription(map[string]any{}, "x")["$type"] != collectionProfil {
		t.Fatal("$type manquant")
	}
	if descriptionDe(v) != "ma bio" {
		t.Fatal("description")
	}
}
