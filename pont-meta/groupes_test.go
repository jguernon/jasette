package main

import (
	"testing"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

// Le classement de l'onglet Groupes (§5.13).
func TestKindOf(t *testing.T) {
	cas := map[table.ThreadType]string{
		table.ONE_TO_ONE:                         "",
		table.GROUP_THREAD:                       "",
		table.ENCRYPTED_OVER_WA_GROUP:            "",
		table.COMMUNITY_FOLDER:                   "",
		table.COMMUNITY_GROUP:                    "community_chat",
		table.COMMUNITY_SUB_THREAD:               "community_chat",
		table.DISCOVERABLE_PUBLIC_CHAT:           "community_chat",
		table.COMMUNITY_BROADCAST_JOINED_THREAD:  "broadcast",
		table.DISCOVERABLE_PUBLIC_BROADCAST_CHAT: "broadcast",
	}
	for tt, attendu := range cas {
		if k := kindOf(tt); k != attendu {
			t.Errorf("type %d : %q, attendu %q", tt, k, attendu)
		}
	}
}

// Une discussion de communauté : grand espace, jamais archivée par son
// dossier « community », avec le nom du Groupe Facebook parent.
func TestConvOutCommunaute(t *testing.T) {
	p := &Pont{convs: map[string]*convState{}, contacts: map[int64]*contact{}}
	p.convs["10"] = &convState{Key: 10, Type: table.COMMUNITY_FOLDER, Name: "Groupe vélo"}
	c := &convState{Key: 11, Type: table.COMMUNITY_GROUP, Name: "Sorties", Folder: "community", Parent: 10, LastActivity: 1}
	co := p.convOut("11", c)
	if !co.Large || co.Kind != "community_chat" || co.Archived || co.Parent != "10" || co.ParentName != "Groupe vélo" {
		t.Errorf("discussion de communauté : %+v", co)
	}
	g := &convState{Key: 12, Type: table.GROUP_THREAD, Name: "Famille", Folder: "inbox", Parent: 10, LastActivity: 1}
	co = p.convOut("12", g)
	if co.Large || co.Kind != "" || co.Parent != "" {
		t.Errorf("groupe ordinaire changé : %+v", co)
	}
}

func TestMentionneMoi(t *testing.T) {
	if !mentionneMoi("111,222, 333", 333) {
		t.Error("mention de moi non vue")
	}
	if mentionneMoi("1111,2222", 111) || mentionneMoi("", 111) || mentionneMoi("111", 0) {
		t.Error("mention prise pour moi à tort")
	}
}
