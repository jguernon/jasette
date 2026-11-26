package main

import (
	"errors"
	"testing"
)

// Réponses de la forme lue par ws3-fca, fbchat-v2 et FB-Notes-Extended
// (données inventées).
func TestReponsesNoteFB(t *testing.T) {
	id, err := analyserNoteCreee([]byte(`for (;;);{"data":{"xfb_rich_status_create":{"status":{"id":"1234567890","description":"Salut","note_type":"TEXT_NOTE","expiration_time":1764246400}}}}`))
	if err != nil || id != "1234567890" {
		t.Fatalf("création : %q %v", id, err)
	}
	id, texte, err := analyserNoteActuelle([]byte(`{"data":{"viewer":{"actor":{"msgr_user_rich_status":{"id":"555","description":"Au chalet"}}}}}`))
	if err != nil || id != "555" || texte != "Au chalet" {
		t.Fatalf("lecture : %q %q %v", id, texte, err)
	}
	if id, _, err := analyserNoteActuelle([]byte(`{"data":{"viewer":{"actor":{"msgr_user_rich_status":null}}}}`)); err != nil || id != "" {
		t.Fatalf("aucune note : %q %v", id, err)
	}
	if err := analyserNoteEffacee([]byte(`{"data":{"xfb_rich_status_delete":{"success":true}}}`)); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{
		`{"errors":[{"message":"PersistedQueryNotFound"}]}`,
		`{"data":{"xfb_rich_status_create":null}}`,
		`{"data":{"xfb_rich_status_create":{}}}`,
		`pas du json`,
	} {
		if _, err := analyserNoteCreee([]byte(r)); !errors.Is(err, errPerimee) {
			t.Fatalf("%s : %v", r, err)
		}
	}
	if e := erreurFB("clear", errPerimee).Error(); e != "La note Messenger n'a pas pu être effacée (requête périmée)." {
		t.Fatal(e)
	}
}
