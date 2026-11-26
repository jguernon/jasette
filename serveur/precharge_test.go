package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Discord précharge moins que les autres services (anti « self-bot »).
func TestPrechargeDiscord(t *testing.T) {
	items := []map[string]any{}
	for i := 0; i < 40; i++ {
		items = append(items, map[string]any{"id": fmt.Sprint(1000 + i), "time": 1000 + i})
	}
	liste, _ := json.Marshal(map[string]any{"ev": "conversations", "items": items})
	c := &Cache{svc: "discord", listes: map[string]json.RawMessage{"inbox": liste}, convs: map[string]*convCache{}}
	marquees := []string{"1000", "1001", "1002", "1003", "1004", "1005", "1006", "1007", "1008", "1009"}

	nombre, compte, total, pause := reglagesPrecharge("discord")
	if compte > 30 || pause < 2e9 {
		t.Fatalf("réglages de Discord : %d messages, pause %v", compte, pause)
	}
	if r := c.candidats(marquees, nombre, total); len(r) != total || r[0].conv != "1039" {
		t.Fatalf("Discord : %d candidats (%v)", len(r), r)
	}
	n, _, tot, _ := reglagesPrecharge("google")
	if r := c.candidats(marquees, n, tot); len(r) != 40 {
		t.Fatalf("Google : %d candidats", len(r))
	}
}
