package main

// Les serveurs Discord, sans réseau : READY inventé, puis des événements.

import (
	"context"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

const voir = discordgo.PermissionViewChannel | discordgo.PermissionSendMessages

func TestChannelPerms(t *testing.T) {
	gs := &guildState{id: "5", ownerID: "99", myRoles: []string{"r1"}, roles: []*discordgo.Role{
		{ID: "5", Permissions: voir},
		{ID: "r1", Permissions: 0},
		{ID: "r2", Permissions: discordgo.PermissionAdministrator},
	}}
	open := &discordgo.Channel{ID: "1"}
	cache := &discordgo.Channel{ID: "2", PermissionOverwrites: []*discordgo.PermissionOverwrite{
		{ID: "5", Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel},
	}}
	pourR1 := &discordgo.Channel{ID: "3", PermissionOverwrites: []*discordgo.PermissionOverwrite{
		{ID: "5", Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel},
		{ID: "r1", Type: discordgo.PermissionOverwriteTypeRole, Allow: discordgo.PermissionViewChannel},
	}}
	lecture := &discordgo.Channel{ID: "4", PermissionOverwrites: []*discordgo.PermissionOverwrite{
		{ID: "1", Type: discordgo.PermissionOverwriteTypeMember, Deny: discordgo.PermissionSendMessages},
	}}
	if channelPerms(gs, open, "1")&discordgo.PermissionViewChannel == 0 {
		t.Error("salon ouvert invisible")
	}
	if channelPerms(gs, cache, "1")&discordgo.PermissionViewChannel != 0 {
		t.Error("salon caché visible")
	}
	if channelPerms(gs, pourR1, "1")&discordgo.PermissionViewChannel == 0 {
		t.Error("salon permis à mon rôle invisible")
	}
	if p := channelPerms(gs, lecture, "1"); p&discordgo.PermissionViewChannel == 0 || p&discordgo.PermissionSendMessages != 0 {
		t.Error("exception de membre ignorée")
	}
	gs.myRoles = []string{"r2"}
	if channelPerms(gs, cache, "1")&discordgo.PermissionViewChannel == 0 {
		t.Error("administrateur sans accès")
	}
	if g, c := splitConv("g5:77"); g != "5" || c != "77" {
		t.Errorf("splitConv : %q %q", g, c)
	}
	if g, c := splitConv("1234"); g != "" || c != "1234" {
		t.Errorf("splitConv (privé) : %q %q", g, c)
	}
}

func TestGuildEvents(t *testing.T) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	t.Setenv("JASETTE_MEDIA", t.TempDir())
	_, lire := capture(t)
	ctx, annule := context.WithCancel(context.Background())
	defer annule()
	p := newPont(ctx)
	moi := &discordgo.User{ID: "1", Username: "jo"}
	emma := &discordgo.User{ID: "2", Username: "emma", GlobalName: "Emma"}
	guild := &discordgo.Guild{
		ID:         "5",
		Properties: &discordgo.Guild{Name: "Pixel Nord", OwnerID: "99"},
		Roles:      []*discordgo.Role{{ID: "5", Permissions: voir}, {ID: "r1"}},
		Channels: []*discordgo.Channel{
			{ID: "10", Type: discordgo.ChannelTypeGuildCategory, Name: "Général", Position: 0},
			{ID: "11", Type: discordgo.ChannelTypeGuildCategory, Name: "Jeux", Position: 1},
			{ID: "1100000000000000020", Type: discordgo.ChannelTypeGuildText, Name: "bienvenue", ParentID: "10", Position: 0, LastMessageID: "1200000000000000000"},
			{ID: "1100000000000000021", Type: discordgo.ChannelTypeGuildNews, Name: "annonces", ParentID: "10", Position: 1, LastMessageID: "1250000000000000000"},
			{ID: "1100000000000000022", Type: discordgo.ChannelTypeGuildText, Name: "démos", ParentID: "11", Position: 0, LastMessageID: "1260000000000000000"},
			{ID: "1100000000000000023", Type: discordgo.ChannelTypeGuildText, Name: "modos", ParentID: "11", Position: 1,
				PermissionOverwrites: []*discordgo.PermissionOverwrite{{ID: "5", Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel}}},
			{ID: "1100000000000000024", Type: discordgo.ChannelTypeGuildVoice, Name: "vocal"},
		},
	}
	p.handle(genEvent{evt: &discordgo.Ready{
		User:          moi,
		Users:         []*discordgo.User{emma},
		Guilds:        []*discordgo.Guild{guild},
		MergedMembers: [][]*discordgo.Member{{{UserID: "1", Roles: []string{"r1"}}}},
		ReadState: &discordgo.ReadStateList{Entries: []*discordgo.ReadState{
			{ID: "1100000000000000022", LastMessageID: "1250000000000000000", MentionCount: 2},
		}},
	}, sf: &sessionFile{Token: "x"}})
	lire()

	p.listConversations(false)
	items := find(lire(), "conversations")["items"].([]any)
	got := map[string]map[string]any{}
	for _, it := range items {
		c := it.(map[string]any)
		got[c["id"].(string)] = c
	}
	if len(got) != 3 {
		t.Fatalf("salons visibles : %v", items)
	}
	demos := got["g5:1100000000000000022"]
	if demos == nil || demos["large"] != true || demos["kind"] != "server" || demos["parent"] != "g5" ||
		demos["parent_name"] != "Pixel Nord" || demos["section"] != "Jeux" || demos["section_order"] != 1.0 ||
		demos["unread"] != true || demos["mentions"] != 2.0 || demos["group"] != true {
		t.Fatalf("salon démos : %v", demos)
	}
	if a := got["g5:1100000000000000021"]; a["announce"] != true || a["unread"] != false {
		t.Fatalf("salon d'annonces : %v", a)
	}

	// Un message qui me mentionne, puis une réponse à un de mes messages.
	salon := "1100000000000000020"
	p.handle(genEvent{evt: &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1300000000000000001", ChannelID: salon, GuildID: "5", Content: "<@1> regarde", Author: emma,
		Mentions: []*discordgo.User{moi}, Timestamp: time.Now(),
	}}})
	evs := lire()
	m := find(evs, "message")["item"].(map[string]any)
	if m["conv"] != "g5:"+salon || m["mentions_me"] != true || m["reply_to_me"] != nil {
		t.Fatalf("mention : %v", m)
	}
	if c := find(evs, "conversation")["item"].(map[string]any); c["mentions"] != 1.0 || c["unread"] != true {
		t.Fatalf("salon après mention : %v", c)
	}
	p.handle(genEvent{evt: &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1300000000000000002", ChannelID: salon, GuildID: "5", Content: "oui", Author: emma, Type: discordgo.MessageTypeReply,
		MessageReference:  &discordgo.MessageReference{MessageID: "1299999999999999999"},
		ReferencedMessage: &discordgo.Message{ID: "1299999999999999999", Author: moi, Content: "allo"},
		Timestamp:         time.Now(),
	}}})
	if m := find(lire(), "message")["item"].(map[string]any); m["reply_to_me"] != true || m["mentions_me"] != nil {
		t.Fatalf("réponse : %v", m)
	}
	// @everyone.
	p.handle(genEvent{evt: &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1300000000000000003", ChannelID: salon, GuildID: "5", Content: "@everyone", Author: emma, MentionEveryone: true, Timestamp: time.Now(),
	}}})
	if m := find(lire(), "message")["item"].(map[string]any); m["mentions_me"] != true {
		t.Fatalf("@everyone : %v", m)
	}

	// Lu ailleurs : plus de non-lu ni de mention.
	p.handle(genEvent{evt: &discordgo.MessageAck{ChannelID: salon, MessageID: "1300000000000000003"}})
	if c := find(lire(), "conversation")["item"].(map[string]any); c["unread"] != false || c["mentions"] != nil {
		t.Fatalf("salon lu : %v", c)
	}

	// Un salon caché, ou d'un serveur inconnu : rien.
	p.handle(genEvent{evt: &discordgo.MessageCreate{Message: &discordgo.Message{ID: "1300000000000000004", ChannelID: "1100000000000000023", GuildID: "5", Content: "x", Author: emma}}})
	p.handle(genEvent{evt: &discordgo.MessageCreate{Message: &discordgo.Message{ID: "1300000000000000005", ChannelID: "42", GuildID: "6", Content: "x", Author: emma}}})
	if evs := lire(); len(evs) != 0 {
		t.Fatalf("salons invisibles : %v", evs)
	}

	// Un salon renommé, un salon retiré, puis le serveur quitté.
	p.handle(genEvent{evt: &discordgo.ChannelUpdate{Channel: &discordgo.Channel{ID: salon, GuildID: "5", Type: discordgo.ChannelTypeGuildText, Name: "accueil", ParentID: "10"}}})
	if c := find(lire(), "conversation")["item"].(map[string]any); c["name"] != "accueil" || c["id"] != "g5:"+salon {
		t.Fatalf("renommé : %v", c)
	}
	p.handle(genEvent{evt: &discordgo.ChannelDelete{Channel: &discordgo.Channel{ID: "1100000000000000021", GuildID: "5", Type: discordgo.ChannelTypeGuildNews}}})
	if d := find(lire(), "deleted"); d == nil || d["conv"] != "g5:1100000000000000021" {
		t.Fatal("salon retiré non annoncé")
	}
	p.handle(genEvent{evt: &discordgo.GuildDelete{Guild: &discordgo.Guild{ID: "5"}}})
	if n := len(lire()); n != 2 {
		t.Fatalf("serveur quitté : %d retraits", n)
	}
	if rawChannel("g5:"+salon) != salon {
		t.Fatal("rawChannel")
	}
}
