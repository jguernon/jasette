package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Un message retiré porte le code neutre « deleted » (PROTOCOLE.md §5.14) :
// le client le montre dans sa langue.
func TestRetiredSystem(t *testing.T) {
	m := retired(Message{ID: "1", Conv: "c", Text: "salut"})
	if m.System != "deleted" || m.Text != "🚫 Message retiré" {
		t.Fatalf("retired : %+v", m)
	}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"system":"deleted"`) {
		t.Fatalf("JSON sans system : %s", b)
	}
	b, _ = json.Marshal(Message{ID: "2", Text: "salut"})
	if strings.Contains(string(b), `"system"`) {
		t.Fatalf("system vide envoyé : %s", b)
	}
}
