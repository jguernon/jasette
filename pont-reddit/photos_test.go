package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestT2De(t *testing.T) {
	for in, want := range map[string]string{
		"@t2_abc123:reddit.com": "t2_abc123",
		"t2_x":                  "t2_x",
		"@t2_:reddit.com":       "",
		"@t5_abc:reddit.com":    "",
		"@t2_A?b:reddit.com":    "",
		"":                      "",
	} {
		if got := t2De(in); got != want {
			t.Errorf("t2De(%q) = %q, attendu %q", in, got, want)
		}
	}
}

// L'annuaire : une demande par personne et par jour, 10 requêtes par minute,
// une pause qui double après un refus, le cache sur le disque (0600).
func TestAnnuaire(t *testing.T) {
	t.Setenv("JASETTE_DATA", t.TempDir())
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.Local)
	a := nouvelAnnuaire()
	a.maintenant = func() time.Time { return now }

	if n := a.demander([]string{"@t2_a:reddit.com", "@t2_a:reddit.com", "@pas_t2:reddit.com", "sr:x"}, nil); n != 1 {
		t.Fatalf("demander : %d", n)
	}
	if n := a.demander([]string{"@t2_a:reddit.com", "sr:quebec"}, map[string]string{"sr:quebec": "Quebec"}); n != 1 {
		t.Fatalf("en file déjà, ou subreddit : %d", n)
	}
	// Les personnes d'abord, groupées ; puis le subreddit seul.
	if l := a.prochain(); len(l) != 1 || l[0] != "@t2_a:reddit.com" {
		t.Fatalf("lot : %v", l)
	}
	a.resultat([]string{"@t2_a:reddit.com"}, map[string]string{"@t2_a:reddit.com": "https://i.redd.it/a.png"}, nil)
	if l := a.prochain(); len(l) != 1 || l[0] != "sr:quebec" {
		t.Fatalf("lot du subreddit : %v", l)
	}
	if u, ajour := a.url("@t2_a:reddit.com"); u != "https://i.redd.it/a.png" || !ajour {
		t.Fatalf("url : %q %v", u, ajour)
	}
	// Déjà demandée aujourd'hui : rien de plus.
	if n := a.demander([]string{"@t2_a:reddit.com"}, nil); n != 0 {
		t.Fatal("redemandée le même jour")
	}
	// Le cache sur le disque, 0600, relu par un autre annuaire.
	st, err := os.Stat(photosPath())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("photos.json : %v %v", err, st)
	}
	b := nouvelAnnuaire()
	b.maintenant = a.maintenant
	if u, ajour := b.url("@t2_a:reddit.com"); u == "" || !ajour {
		t.Fatal("cache du disque non relu")
	}
	// Le lendemain : l'ancienne adresse sert encore, mais sera redemandée.
	now = now.Add(24 * time.Hour)
	if u, ajour := b.url("@t2_a:reddit.com"); u == "" || ajour {
		t.Fatal("photo d'hier prise pour à jour")
	}
	if n := b.demander([]string{"@t2_a:reddit.com"}, nil); n != 1 {
		t.Fatal("photo d'hier pas redemandée")
	}

	// 10 requêtes par minute au plus.
	for i := 0; i < photosParMinute; i++ {
		if d := a.attente(); d != 0 {
			t.Fatalf("requête %d retenue (%v)", i, d)
		}
	}
	if d := a.attente(); d <= 0 || d > time.Minute {
		t.Fatalf("11e requête de la minute permise (%v)", d)
	}
	now = now.Add(time.Minute)
	if d := a.attente(); d != 0 {
		t.Fatalf("une minute plus tard : %v", d)
	}
	// Un refus : le lot reste en file, pause de 5 min, puis 10.
	a.resultat([]string{"sr:quebec"}, nil, os.ErrPermission)
	if l := a.prochain(); len(l) != 1 {
		t.Fatal("lot refusé perdu")
	}
	if d := a.attente(); d != photosPauseMin {
		t.Fatalf("pause : %v", d)
	}
	a.resultat([]string{"sr:quebec"}, nil, os.ErrPermission)
	if a.pause != 2*photosPauseMin || a.refusees != 2 {
		t.Fatalf("pause doublée : %v, refusées %d", a.pause, a.refusees)
	}
	a.oublier()
	if _, err := os.Stat(photosPath()); err == nil {
		t.Fatal("photos.json gardé après oubli")
	}
}

// journal : un tampon sûr pour le journal du pont.
type journal struct {
	mu sync.Mutex
	b  strings.Builder
}

func (j *journal) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.b.Write(p)
}

func (j *journal) String() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.b.String()
}

func TestPhotosContreFauxServeur(t *testing.T) {
	sortie := capturer(t)
	jl := &journal{}
	log = zerolog.New(jl).Level(zerolog.InfoLevel)
	f := nouveauFaux(t)
	redditBase, matrixBase = f.srv.URL, f.srv.URL
	hoteEssai = f.srv.URL
	t.Cleanup(func() { hoteEssai = "" })
	ctx, annule := context.WithCancel(context.Background())
	defer annule()
	p := newPont(ctx)
	p.rest.gap = time.Millisecond
	p.cdn.gap = time.Millisecond

	go p.login(map[string]string{"reddit_session": "SECRETSESS", "csrf_token": "c5rf"})
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "state" && e["state"] == "ready" })

	// La conversation à deux (alice) prend la photo de Reddit ; carl (la
	// demande) n'en a pas ; moi : avatar_moi ; le subreddit : son icône.
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "avatar" && e["conv"] == "!dm:reddit.com" })
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "avatar_moi" && e["service"] == "reddit" })
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "avatar" && e["conv"] == "sr:quebecgames" })

	// Les auteurs d'un salon de subreddit : sender_avatar après la demande.
	p.messages(Cmd{Conv: "!sr:reddit.com"})
	sortie.attendre(t, func(e map[string]any) bool {
		return e["ev"] == "sender_avatar" && e["id"] == "@t2_b:reddit.com"
	})

	// Une liste redemandée ne redemande rien (cache du jour).
	p.listConversations(false)
	time.Sleep(300 * time.Millisecond)
	if p.photos.demander([]string{"@t2_a:reddit.com", "@t2_c:reddit.com"}, nil) != 0 {
		t.Fatal("photos redemandées le même jour")
	}
	f.mu.Lock()
	vus := map[string]int{}
	for _, lot := range f.graphql {
		for _, id := range lot {
			vus[id]++
		}
	}
	abouts := append([]string(nil), f.abouts...)
	f.mu.Unlock()
	for id, n := range vus {
		if n != 1 {
			t.Errorf("%s demandé %d fois", id, n)
		}
	}
	if vus["t2_a"] != 1 || vus["t2_moi"] != 1 || vus["t2_b"] != 1 || vus["t2_c"] != 1 {
		t.Fatalf("personnes demandées : %v", vus)
	}
	if len(abouts) != 1 || abouts[0] != "QuebecGames" {
		t.Fatalf("subreddits demandés : %v", abouts)
	}

	// Média : v3 refusé (404), puis la voie authentifiée v1.
	p.download(Cmd{MediaID: "mxc://reddit.com/img404"})
	md := sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "media" && e["media_id"] == "mxc://reddit.com/img404" })
	if md["path"] == nil {
		t.Fatalf("média v1 : %v", md)
	}
	f.mu.Lock()
	medias := strings.Join(f.medias, ",")
	f.mu.Unlock()
	if !strings.Contains(medias, "v3 img404,v1 img404") {
		t.Fatalf("voies du média : %s", medias)
	}

	// Le journal : un bilan en Info, sans adresse signée, cookie ni jeton.
	j := jl.String()
	if !strings.Contains(j, "photos Reddit : bilan") || !strings.Contains(j, "icône de subreddit : bilan") {
		t.Fatalf("bilan absent du journal : %s", j)
	}
	for _, secret := range []string{"SIGNATURE123", "SECRETSESS", "c5rf", ".signature", "/img/"} {
		if strings.Contains(j, secret) {
			t.Fatalf("journal : %q présent : %s", secret, j)
		}
	}
	for _, e := range sortie.evs() {
		for _, v := range e {
			if s, ok := v.(string); ok && strings.Contains(s, "SIGNATURE123") {
				t.Fatalf("adresse signée émise : %v", e)
			}
		}
	}
}

// Un salon de modération (reddit_modmail) : un groupe dans la boîte de
// réception, même si un subreddit y est nommé, et pas de « type peu connu ».
func TestModmail(t *testing.T) {
	jl := &journal{}
	log = zerolog.New(jl).Level(zerolog.InfoLevel)
	s := &salon{ID: "!mm", Type: "reddit_modmail", Subreddit: "QuebecGames", Name: "[join] I would like to join r/QuebecGames"}
	if s.sorte() != sorteGroupe {
		t.Fatalf("classe : %s", s.sorte())
	}
	c := s.convOut("@t2_moi:reddit.com", nil)
	if c.Large || c.Kind != "" || !c.Group {
		t.Fatalf("conversation : %+v", c)
	}
	p := newPont(context.Background())
	p.noterType(s)
	if strings.Contains(jl.String(), "peu connu") {
		t.Fatal("avertissement pour reddit_modmail")
	}
}
