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

// Un grand espace (§5.13) n'est jamais préchargé, même marqué.
func TestPrechargeSansGrands(t *testing.T) {
	liste, _ := json.Marshal(map[string]any{"ev": "conversations", "items": []map[string]any{
		{"id": "1", "time": 10}, {"id": "g5:11", "time": 20, "large": true}, {"id": "g5:12", "time": 5, "large": true},
	}})
	c := &Cache{svc: "discord", listes: map[string]json.RawMessage{"inbox": liste}, convs: map[string]*convCache{}}
	r := c.candidats([]string{"g5:12"}, 8, 15)
	if len(r) != 1 || r[0].conv != "1" {
		t.Fatalf("candidats : %v", r)
	}
}

// Reddit : service facultatif, préfixe « r: », préchargement prudent comme Discord.
func TestServiceReddit(t *testing.T) {
	if prefixeService["reddit"] != "r:" {
		t.Fatalf("préfixe de Reddit : %q", prefixeService["reddit"])
	}
	if svc, id := svcDeId("r:!abc:reddit.com"); svc != "reddit" || id != "!abc:reddit.com" {
		t.Fatalf("svcDeId : %s %s", svc, id)
	}
	if f, ok := pontsFacultatifs["reddit"]; !ok || f.bin != "jasette-reddit" || f.dossier != "reddit" {
		t.Fatalf("pont facultatif : %+v", f)
	}
	if !contient(ordreServices, "reddit") {
		t.Fatalf("ordre : %v", ordreServices)
	}
	_, compte, total, pause := reglagesPrecharge("reddit")
	if compte > 30 || total == 0 || pause < 2e9 {
		t.Fatalf("préchargement de Reddit : %d, %d, %v", compte, total, pause)
	}
	if ids := idsDuService([]string{"r:!a:reddit.com", "d:1", "123"}, "google"); len(ids) != 1 || ids[0] != "123" {
		t.Fatalf("idsDuService : %v", ids)
	}
}

// LinkedIn : service facultatif, préfixe « l: », préchargement plus prudent encore.
func TestServiceLinkedIn(t *testing.T) {
	if prefixeService["linkedin"] != "l:" {
		t.Fatalf("préfixe de LinkedIn : %q", prefixeService["linkedin"])
	}
	if svc, id := svcDeId("l:2-ZDhk+/ab=="); svc != "linkedin" || id != "2-ZDhk+/ab==" {
		t.Fatalf("svcDeId : %s %s", svc, id)
	}
	if f, ok := pontsFacultatifs["linkedin"]; !ok || f.bin != "jasette-linkedin" || f.variable != "JASETTE_LINKEDIN" || f.dossier != "linkedin" {
		t.Fatalf("pont facultatif : %+v", f)
	}
	if ordreServices[len(ordreServices)-1] != "linkedin" {
		t.Fatalf("ordre : %v", ordreServices)
	}
	nombre, compte, total, pause := reglagesPrecharge("linkedin")
	if nombre > 5 || compte > 20 || total > 10 || pause < 5e9 {
		t.Fatalf("préchargement de LinkedIn : %d, %d, %d, %v", nombre, compte, total, pause)
	}
}
