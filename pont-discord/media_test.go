package main

import (
	"net/url"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// La vignette d'affichage : proxy media.discordapp.net, WebP, 1024 px de
// large au plus, signature de l'adresse gardée.
func TestVignetteURL(t *testing.T) {
	brute := "https://cdn.discordapp.com/attachments/50/900/vitrine.png?ex=6b09d48e&is=6b0882fe&hm=abc123&"
	v := vignetteURL(mediaKey{URL: brute, W: 4096, H: 2048, Size: 9 << 20}, "image/png")
	u, err := url.Parse(v)
	if err != nil || u.Host != "media.discordapp.net" || u.Path != "/attachments/50/900/vitrine.png" {
		t.Fatalf("vignette : %q", v)
	}
	q := u.Query()
	if q.Get("format") != "webp" || q.Get("width") != "1024" || q.Get("height") != "512" {
		t.Fatalf("taille : %v", q)
	}
	if q.Get("ex") != "6b09d48e" || q.Get("is") != "6b0882fe" || q.Get("hm") != "abc123" {
		t.Fatalf("signature perdue : %v", q)
	}
	if !trustedCDN(v) {
		t.Fatal("la vignette n'est pas sur un hôte de confiance")
	}
	// Très haute : la hauteur est bornée aussi.
	q = mustQuery(t, vignetteURL(mediaKey{URL: brute, W: 1000, H: 8000, Size: 4 << 20}, "image/jpeg"))
	if q.Get("height") != "2048" || q.Get("width") != "256" {
		t.Fatalf("haute : %v", q)
	}
	// Petite et légère : l'original suffit.
	if v := vignetteURL(mediaKey{URL: brute, W: 640, H: 480, Size: 80 << 10}, "image/jpeg"); v != "" {
		t.Fatalf("petite image : %q", v)
	}
	// Petite mais lourde (PNG) : vignette, à sa taille.
	q = mustQuery(t, vignetteURL(mediaKey{URL: brute, W: 800, H: 600, Size: 2 << 20}, "image/png"))
	if q.Get("width") != "800" || q.Get("height") != "600" {
		t.Fatalf("petite lourde : %v", q)
	}
	// Pas de vignette : GIF animé, vidéo, dimensions inconnues, autre hôte.
	for _, c := range []struct {
		k    mediaKey
		mime string
	}{
		{mediaKey{URL: brute, W: 2000, H: 2000, Size: 5 << 20}, "image/gif"},
		{mediaKey{URL: brute, W: 2000, H: 2000, Size: 5 << 20}, "video/mp4"},
		{mediaKey{URL: brute, Size: 5 << 20}, "image/png"},
		{mediaKey{URL: "https://images-ext-1.discordapp.net/external/x.png", W: 2000, H: 2000, Size: 5 << 20}, "image/png"},
		{mediaKey{URL: "http://cdn.discordapp.com/attachments/1/2/x.png", W: 2000, H: 2000}, "image/png"},
	} {
		if v := vignetteURL(c.k, c.mime); v != "" {
			t.Errorf("%s %s : %q", c.k.URL, c.mime, v)
		}
	}
}

func mustQuery(t *testing.T, v string) url.Values {
	t.Helper()
	u, err := url.Parse(v)
	if err != nil || v == "" {
		t.Fatalf("adresse : %q", v)
	}
	return u.Query()
}

// La vignette ne se confond pas avec l'original (« <hash>.* »).
func TestNomVignette(t *testing.T) {
	dir := t.TempDir()
	if err := writeWhole(nomVignette(dir, "dc:1"), []byte("RIFF")); err != nil {
		t.Fatal(err)
	}
	if o := original(dir, "dc:1"); o != "" {
		t.Fatalf("la vignette passe pour l'original : %s", o)
	}
	if err := writeWhole(dir+"/"+hashName("dc:1")+".png", []byte("png")); err != nil {
		t.Fatal(err)
	}
	if o := original(dir, "dc:1"); !strings.HasSuffix(o, ".png") {
		t.Fatalf("original : %q", o)
	}
}

// La clé d'une pièce jointe porte ses dimensions et sa taille.
func TestCleDimensions(t *testing.T) {
	m := convertMessage(testMessageImage(), fakeResolver{})
	if len(m.Attachments) != 1 || !strings.Contains(string(m.Attachments[0].Key), `"w":640`) || !strings.Contains(string(m.Attachments[0].Key), `"s":1234`) {
		t.Fatalf("clé : %s", m.Attachments[0].Key)
	}
}

func testMessageImage() *discordgo.Message {
	return &discordgo.Message{ID: "11", ChannelID: "50", Author: &discordgo.User{ID: "2", Username: "emma"},
		Attachments: []*discordgo.MessageAttachment{{
			ID: "900", URL: "https://cdn.discordapp.com/attachments/50/900/lac.jpg", Filename: "lac.jpg", ContentType: "image/jpeg", Size: 1234, Width: 640, Height: 480,
		}}}
}
