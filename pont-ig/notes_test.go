package main

import (
	"errors"
	"testing"
)

// Un plateau de la forme observée par dumpsta-gram (données inventées) : deux
// notes, un élément « ambient_data » sans note, et ma propre note.
const plateauEssai = `for (;;);{"data":{"response":{"inbox_tray_items":[
 {"inbox_tray_item_id":"90000000000000001","inbox_tray_item_type":"note",
  "pog_info":{"pog_style":"user","pog_users":[{"id":"1001","username":"maxime.roy","full_name":"Maxime","interop_messaging_user_fbid":"17841"}]},
  "note_dict":{"note_style":0,"text":"Enfin en vacances 🌴","is_emoji_only":false,"audience":0,"author_id":"1001","created_at":1764150000}},
 {"inbox_tray_item_id":"90000000000000002","inbox_tray_item_type":"ambient_data","pog_info":{"pog_users":[{"id":"1002"}]},"note_dict":null},
 {"inbox_tray_item_id":"90000000000000003","inbox_tray_item_type":"note",
  "pog_info":{"pog_users":[{"id":"1003","username":"noemie.p"}]},
  "note_dict":{"note_style":0,"text":"Spectacle samedi ?","audience":1,"author_id":1003,"created_at":1764151000}}
]}},"extensions":{"is_final":true}}`

func TestAnalyserPlateau(t *testing.T) {
	n, err := analyserPlateau([]byte(plateauEssai))
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 2 || n[0].AuthorID != "1001" || n[0].Username != "maxime.roy" || n[0].FBID != 17841 ||
		n[0].Text != "Enfin en vacances 🌴" || n[0].Time != 1764150000000 || n[1].AuthorID != "1003" || n[1].ItemID != "90000000000000003" {
		t.Fatalf("%+v", n)
	}
}

func TestReponsesNoteIG(t *testing.T) {
	id, err := analyserNoteCreee([]byte(`{"data":{"xdt_create_inbox_tray_item":{"inbox_tray_item":{"inbox_tray_item_id":"1799","inbox_tray_item_type":"note","note_dict":{"text":"Salut"}}}}}`))
	if err != nil || id != "1799" {
		t.Fatalf("création : %q %v", id, err)
	}
	if err := analyserNoteEffacee([]byte(`{"data":{"xdt_delete_inbox_tray_item":null},"extensions":{}}`)); err != nil {
		t.Fatalf("effacement : %v", err)
	}
	// doc_id périmé ou réponse d'une autre forme : « requête périmée ».
	for _, r := range []string{
		`{"errors":[{"message":"PersistedQueryNotFound"}]}`,
		`{"data":{"autre_chose":{}}}`,
		`<html>oups</html>`,
		`{"data":{"xdt_create_inbox_tray_item":{"inbox_tray_item":null}}}`,
	} {
		if _, err := analyserNoteCreee([]byte(r)); !errors.Is(err, errPerimee) {
			t.Fatalf("%s : %v", r, err)
		}
	}
	if _, err := analyserNoteCreee([]byte(`{"errors":[{"message":"Note trop longue"}],"data":{"xdt_create_inbox_tray_item":null}}`)); err == nil || err.Error() != "Note trop longue" {
		t.Fatalf("erreur du service : %v", err)
	}
	if _, err := analyserNoteCreee([]byte(`{"error":1357004,"errorSummary":"Désolé, un problème est survenu"}`)); err == nil {
		t.Fatal("erreur de session ignorée")
	}
	if e := erreurIG("set", errPerimee).Error(); e != "La note Instagram n'a pas pu être publiée (requête périmée)." {
		t.Fatal(e)
	}
}

func TestDocsNotesFichier(t *testing.T) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	if docNote(docTrayIG) != "29231580869776032" {
		t.Fatal("défaut")
	}
	if err := writeWhole(igDir()+"/notes-docs.json", []byte(`{"IGDInboxTrayQuery":" 123 "}`)); err != nil {
		t.Fatal(err)
	}
	if docNote(docTrayIG) != "123" || docNote(docCreateIG) != "28592645767037889" {
		t.Fatal("fichier ignoré")
	}
}
