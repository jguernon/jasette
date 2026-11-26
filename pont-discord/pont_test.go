package main

// Essais sans réseau : conversions, et la session nourrie d'événements
// inventés (aucune connexion à Discord).

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

type fakeResolver struct{}

func (fakeResolver) userName(id string) string {
	return map[string]string{"1": "Moi", "2": "Emma"}[id]
}
func (fakeResolver) channelName(id string) string { return "" }
func (fakeResolver) meID() string                 { return "1" }

func TestSnowflake(t *testing.T) {
	// 175928847299117063 : l'exemple de la documentation de Discord (2016-04-30 11:18:25.796 UTC).
	if got := snowflakeTime("175928847299117063"); got != 1462015105796 {
		t.Fatalf("snowflakeTime = %d", got)
	}
	if compareIDs("99", "100") != -1 || compareIDs("100", "99") != 1 || compareIDs("", "1") != -1 || compareIDs("5", "5") != 0 {
		t.Fatal("compareIDs")
	}
	n := generateNonce()
	if d := time.Now().UnixMilli() - snowflakeTime(n); d < 0 || d > 5000 {
		t.Fatalf("nonce %s : %d ms", n, d)
	}
}

func TestRenderContent(t *testing.T) {
	in := "Salut <@2> et <@!9>, va dans <#77> <:pepe:123> <a:danse:456> </aide:1> à <t:172800:d>"
	got := renderContent(in, fakeResolver{})
	for _, want := range []string{"@Emma", "@quelqu'un", "#salon", ":pepe:", ":danse:", "/aide", "1970-01-0"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q ne contient pas %q", got, want)
		}
	}
}

func TestCleanToken(t *testing.T) {
	good := strings.Repeat("A", 24) + "." + strings.Repeat("b", 6) + "." + strings.Repeat("c", 38)
	if tok, err := cleanToken(" \"" + good + "\" "); err != nil || tok != good {
		t.Fatalf("cleanToken : %q %v", tok, err)
	}
	if _, err := cleanToken("Bot " + good); err == nil {
		t.Fatal("jeton de robot accepté")
	}
	if _, err := cleanToken("bonjour"); err == nil {
		t.Fatal("texte accepté")
	}
}

func TestConvertMessage(t *testing.T) {
	ts := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	ref := &discordgo.Message{ID: "10", ChannelID: "50", Content: "La démo est prête", Author: &discordgo.User{ID: "1", Username: "jo"}}
	m := convertMessage(&discordgo.Message{
		ID: "11", ChannelID: "50", Content: "Bravo <@1> !", Timestamp: ts, Type: discordgo.MessageTypeReply,
		Author:            &discordgo.User{ID: "2", Username: "emma"},
		MessageReference:  &discordgo.MessageReference{MessageID: "10", ChannelID: "50"},
		ReferencedMessage: ref,
		Attachments: []*discordgo.MessageAttachment{{
			ID: "900", URL: "https://cdn.discordapp.com/attachments/50/900/lac.jpg", Filename: "lac.jpg", ContentType: "image/jpeg", Size: 1234, Width: 640, Height: 480,
		}},
		Reactions: []*discordgo.MessageReactions{{Count: 3, Me: true, Emoji: &discordgo.Emoji{Name: "🎉"}}},
		Embeds:    []*discordgo.MessageEmbed{{Type: discordgo.EmbedTypeLink, URL: "https://exemple.com", Title: "Exemple", Thumbnail: &discordgo.MessageEmbedThumbnail{URL: "https://images-ext-1.discordapp.net/x.png"}}},
	}, fakeResolver{})
	if m.FromMe || m.Sender != "Emma" || m.Time != ts.UnixMilli() || m.Kind != "discord" {
		t.Fatalf("en-tête : %+v", m)
	}
	if m.ReplyTo != "10" || m.ReplyText != "Vous : La démo est prête" || !strings.HasPrefix(m.Text, "↪ Vous : La démo est prête\nBravo @Moi !") {
		t.Fatalf("citation : %q / %q", m.ReplyText, m.Text)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].MediaID != "dc:900" || m.Attachments[0].W != 640 {
		t.Fatalf("pièce jointe : %+v", m.Attachments)
	}
	var k mediaKey
	if json.Unmarshal(m.Attachments[0].Key, &k) != nil || k.Channel != "50" || k.Message != "11" || k.ID != "900" {
		t.Fatalf("clé : %s", m.Attachments[0].Key)
	}
	if len(m.Reactions) != 3 || !m.Reactions[0].FromMe || m.Reactions[1].FromMe {
		t.Fatalf("réactions : %+v", m.Reactions)
	}
	if m.LinkPreview == nil || m.LinkPreview.Site != "exemple.com" || m.LinkPreview.ImageURL == "" {
		t.Fatalf("aperçu : %+v", m.LinkPreview)
	}
	sys := convertMessage(&discordgo.Message{ID: "12", ChannelID: "50", Type: discordgo.MessageTypeRecipientAdd, Timestamp: ts,
		Author: &discordgo.User{ID: "1"}, Mentions: []*discordgo.User{{ID: "2"}}}, fakeResolver{})
	if sys.Kind != "admin" || sys.Text != "Moi a ajouté Emma" {
		t.Fatalf("message de service : %+v", sys)
	}
}

// capture : ce que le pont écrit, une ligne JSON par événement.
func capture(t *testing.T) (*bytes.Buffer, func() []map[string]any) {
	var buf bytes.Buffer
	outMu.Lock()
	out = json.NewEncoder(&buf)
	outMu.Unlock()
	return &buf, func() []map[string]any {
		outMu.Lock()
		defer outMu.Unlock()
		var evs []map[string]any
		for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			var v map[string]any
			if json.Unmarshal([]byte(l), &v) == nil {
				evs = append(evs, v)
			}
		}
		buf.Reset()
		return evs
	}
}

func find(evs []map[string]any, ev string) map[string]any {
	for _, e := range evs {
		if e["ev"] == ev {
			return e
		}
	}
	return nil
}

func TestSessionEvents(t *testing.T) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	t.Setenv("JASETTE_MEDIA", t.TempDir())
	t.Setenv("JASETTE_PRESENCE", "1")
	_, lire := capture(t)
	ctx, annule := context.WithCancel(context.Background())
	defer annule()
	p := newPont(ctx)
	p.login = true
	sf := &sessionFile{Token: "x"}
	moi := &discordgo.User{ID: "1", Username: "jo", GlobalName: "Jo"}
	emma := &discordgo.User{ID: "2", Username: "emma", GlobalName: "Emma Caron"}
	sam := &discordgo.User{ID: "3", Username: "sam"}
	p.handle(genEvent{evt: &discordgo.Ready{
		User:  moi,
		Users: []*discordgo.User{emma, sam},
		PrivateChannels: []*discordgo.Channel{
			{ID: "1000000000000000050", Type: discordgo.ChannelTypeDM, RecipientIDs: []string{"2"}, LastMessageID: "1300000000000000000"},
			{ID: "1000000000000000060", Type: discordgo.ChannelTypeGroupDM, RecipientIDs: []string{"2", "3"}, LastMessageID: "1200000000000000000"},
			{ID: "900", GuildID: "5", Type: discordgo.ChannelTypeGuildText}, // salon d'un serveur : ignoré
		},
		Relationships: []*discordgo.Relationship{{ID: "3", Type: discordgo.RelationshipFriend, Nickname: "Samuel"}},
		ReadState:     &discordgo.ReadStateList{Entries: []*discordgo.ReadState{{ID: "1000000000000000060", LastMessageID: "1200000000000000000"}}},
		Presences:     []*discordgo.Presence{{User: &discordgo.User{ID: "2"}, Status: discordgo.StatusOnline}},
	}, sf: sf})
	evs := lire()
	if find(evs, "paired") == nil || find(evs, "account") == nil || find(evs, "account")["name"] != "Jo" {
		t.Fatalf("connexion : %v", evs)
	}
	if pr := find(evs, "presence"); pr == nil || pr["item"].(map[string]any)["conv"] != "1000000000000000050" {
		t.Fatalf("présence : %v", evs)
	}
	if s, err := loadSession(); err != nil || s.UserID != "1" || s.Token != "x" {
		t.Fatalf("session.json : %+v %v", s, err)
	}

	p.listConversations(false)
	evs = lire()
	l := find(evs, "conversations")["items"].([]any)
	if len(l) != 2 {
		t.Fatalf("liste : %v", l)
	}
	dm, grp := l[0].(map[string]any), l[1].(map[string]any)
	if dm["name"] != "Emma Caron" || dm["unread"] != true || dm["group"] != false {
		t.Fatalf("message privé : %v", dm)
	}
	if grp["name"] != "Emma Caron, Samuel" || grp["unread"] != false || grp["group"] != true {
		t.Fatalf("groupe : %v", grp)
	}

	// Un message reçu, puis une réaction, puis un retrait.
	p.handle(genEvent{evt: &discordgo.TypingStart{UserID: "2", ChannelID: "1000000000000000050"}})
	p.handle(genEvent{evt: &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "1300000000000000001", ChannelID: "1000000000000000050", Content: "Allo !", Author: emma, Timestamp: time.Now(),
	}}})
	evs = lire()
	if ty := find(evs, "typing"); ty == nil || ty["name"] != "Emma Caron" {
		t.Fatalf("typing : %v", evs)
	}
	if m := find(evs, "message"); m == nil || m["item"].(map[string]any)["text"] != "Allo !" {
		t.Fatalf("message : %v", evs)
	}
	if c := find(evs, "conversation"); c == nil || c["item"].(map[string]any)["preview"] != "Allo !" {
		t.Fatalf("conversation : %v", evs)
	}
	p.handle(genEvent{evt: &discordgo.MessageReactionAdd{MessageReaction: &discordgo.MessageReaction{
		UserID: "2", MessageID: "1300000000000000001", ChannelID: "1000000000000000050", Emoji: discordgo.Emoji{Name: "❤️"},
	}}})
	evs = lire()
	r := find(evs, "message")["item"].(map[string]any)["reactions"].([]any)
	if len(r) != 1 || r[0].(map[string]any)["sender"] != "Emma Caron" {
		t.Fatalf("réaction : %v", r)
	}
	p.handle(genEvent{evt: &discordgo.MessageDelete{Message: &discordgo.Message{ID: "1300000000000000001", ChannelID: "1000000000000000050"}}})
	evs = lire()
	if m := find(evs, "message")["item"].(map[string]any); m["text"] != "🚫 Message retiré" || m["sender"] != "Emma Caron" {
		t.Fatalf("retrait : %v", m)
	}

	// Lu ailleurs (un autre appareil) : plus de non-lu.
	p.handle(genEvent{evt: &discordgo.MessageAck{ChannelID: "1000000000000000050", MessageID: "1300000000000000001"}})
	evs = lire()
	if c := find(evs, "conversation"); c == nil || c["item"].(map[string]any)["unread"] != false {
		t.Fatalf("lu : %v", evs)
	}

	// Un message d'un salon de serveur : ignoré.
	p.handle(genEvent{evt: &discordgo.MessageCreate{Message: &discordgo.Message{ID: "1", ChannelID: "900", GuildID: "5", Content: "x", Author: sam}}})
	if evs = lire(); len(evs) != 0 {
		t.Fatalf("salon de serveur : %v", evs)
	}

	// Recherche de personnes et de messages (en mémoire).
	p.search("sam")
	if it := find(lire(), "search")["items"].([]any); len(it) != 1 || it[0].(map[string]any)["id"] != "3" {
		t.Fatalf("search : %v", it)
	}
	p.searchMessages("retire", 10)
	if it := find(lire(), "search_messages")["items"].([]any); len(it) != 1 {
		t.Fatalf("search_messages : %v", it)
	}

	// Jeton refusé : tout est oublié.
	p.handle(genEvent{evt: &discordgo.InvalidAuth{}})
	evs = lire()
	if st := find(evs, "state"); st == nil || st["state"] != "unpaired" {
		t.Fatalf("jeton refusé : %v", evs)
	}
	if _, err := loadSession(); err == nil {
		t.Fatal("session.json gardé après un refus")
	}
}

// create_group : les demandes impossibles sont refusées avant tout appel à Discord.
func TestCreateGroupRefus(t *testing.T) {
	_, lire := capture(t)
	ctx, annule := context.WithCancel(context.Background())
	defer annule()
	p := newPont(ctx)
	for _, membres := range [][]string{
		{"2"},         // une seule personne
		{"2", "2"},    // la même deux fois
		{"2", "emma"}, // pas un id Discord
		{"2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}, // plus de 9
	} {
		p.createGroup(Cmd{Cmd: "create_group", Members: membres})
		e := find(lire(), "error")
		if e == nil || e["where"] != "create_group" {
			t.Fatalf("%v : pas d'erreur create_group (%v)", membres, e)
		}
	}
	// Valide, mais pas de session : une erreur aussi, sans réseau.
	p.createGroup(Cmd{Cmd: "create_group", Members: []string{"d:2", "3"}, Name: "Les amis"})
	if e := find(lire(), "error"); e == nil || e["where"] != "create_group" {
		t.Fatalf("sans session : %v", e)
	}
}
