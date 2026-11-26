package main

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestAuteurURL(t *testing.T) {
	u := &discordgo.User{ID: "7", Avatar: "abc"}
	dm := &discordgo.Message{Author: u, Member: &discordgo.Member{Avatar: "srv"}}
	if got := auteurURL(dm, "5"); !strings.Contains(got, "guilds/5/users/7/avatars/srv") {
		t.Fatalf("avatar du serveur d'abord : %s", got)
	}
	if got := auteurURL(dm, ""); !strings.Contains(got, "avatars/7/abc") {
		t.Fatalf("groupe privé : l'avatar du compte : %s", got)
	}
	if got := auteurURL(&discordgo.Message{Author: &discordgo.User{ID: "8"}}, "5"); got != "" {
		t.Fatalf("sans avatar, rien : %s", got)
	}
}

func TestNoteAuteurUneFoisParAdresse(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	p := &Pont{auteurs: map[string]string{}, fileAuteurs: make(chan demandeAuteur, 4)}
	salon := &chanState{guild: "5"}
	prive := &chanState{ch: &discordgo.Channel{Type: discordgo.ChannelTypeDM}}
	dm := &discordgo.Message{Author: &discordgo.User{ID: "7", Avatar: "abc"}}
	for range 3 {
		m := Message{}
		p.noteAuteur(&m, dm, salon)
		if m.SenderID != "7" {
			t.Fatal("sender_id manquant")
		}
	}
	if len(p.fileAuteurs) != 1 {
		t.Fatalf("une seule demande par adresse, pas %d", len(p.fileAuteurs))
	}
	m := Message{}
	p.noteAuteur(&m, &discordgo.Message{Author: &discordgo.User{ID: "9", Avatar: "x"}}, prive)
	mien := Message{FromMe: true}
	p.noteAuteur(&mien, dm, salon)
	if m.SenderID != "" || mien.SenderID != "" || len(p.fileAuteurs) != 1 {
		t.Fatal("ni les messages privés à deux, ni les miens")
	}
}
