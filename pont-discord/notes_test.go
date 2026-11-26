package main

import (
	"encoding/json"
	"testing"
)

func TestStatutPerso(t *testing.T) {
	a, err := analyserStatutPerso([]byte(`{"status":"invisible","custom_status":{"text":"En vacances","emoji_name":"🌴","expires_at":null}}`))
	if err != nil || a != `{"text":"En vacances","emoji_name":"🌴","expires_at":null}` {
		t.Fatalf("%q %v", a, err)
	}
	if a, _ := analyserStatutPerso([]byte(`{"status":"online"}`)); a != "null" {
		t.Fatal(a)
	}
	b, _ := json.Marshal(corpsStatut("🎵 Écoute Daft Punk"))
	if string(b) != `{"custom_status":{"expires_at":null,"text":"🎵 Écoute Daft Punk"}}` {
		t.Fatal(string(b))
	}
}
