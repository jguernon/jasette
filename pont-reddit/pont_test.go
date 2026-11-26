package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// ---- Outils ----

func jwtDe(claims map[string]any) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	b, _ := json.Marshal(claims)
	return h + "." + base64.RawURLEncoding.EncodeToString(b) + ".signature"
}

func ev(typ, sender, id string, ts int64, content string) mxEvent {
	return mxEvent{Type: typ, Sender: sender, ID: id, TS: ts, Content: json.RawMessage(content)}
}

func etat(typ, cle, sender, content string) mxEvent {
	k := cle
	return mxEvent{Type: typ, StateKey: &k, Sender: sender, Content: json.RawMessage(content)}
}

// sortie : capture les événements émis par le pont.
type sortie struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *sortie) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(b)
}

func (s *sortie) evs() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var l []map[string]any
	for _, ligne := range strings.Split(s.buf.String(), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(ligne), &m) == nil {
			l = append(l, m)
		}
	}
	return l
}

func (s *sortie) attendre(t *testing.T, quoi func(map[string]any) bool) map[string]any {
	t.Helper()
	fin := time.Now().Add(10 * time.Second)
	for time.Now().Before(fin) {
		for _, e := range s.evs() {
			if quoi(e) {
				return e
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("événement attendu jamais venu ; reçus : %v", s.evs())
	return nil
}

func capturer(t *testing.T) *sortie {
	s := &sortie{}
	outMu.Lock()
	out = json.NewEncoder(s)
	outMu.Unlock()
	log = zerolog.New(io.Discard)
	t.Setenv("JASETTE_DATA", t.TempDir())
	t.Setenv("JASETTE_MEDIA", t.TempDir())
	return s
}

// ---- JWT ----

func TestDecodeJWT(t *testing.T) {
	exp := time.Now().Add(24 * time.Hour).Unix()
	info, err := decodeJWT(jwtDe(map[string]any{"lid": "t2_abc", "aid": "t2_zzz", "exp": float64(exp) + 0.5}))
	if err != nil || info.Account != "t2_abc" || info.Expires.Unix() != exp {
		t.Fatalf("lid : %+v %v", info, err)
	}
	if info, _ := decodeJWT(jwtDe(map[string]any{"aid": "t2_def", "exp": exp})); info.Account != "t2_def" {
		t.Fatalf("aid : %+v", info)
	}
	if matrixUserID("t2_abc") != "@t2_abc:reddit.com" {
		t.Fatal(matrixUserID("t2_abc"))
	}
	// Un jeton de visiteur (loid seulement), ou pas un JWT.
	if _, err := decodeJWT(jwtDe(map[string]any{"loid": "000", "exp": exp})); err == nil {
		t.Fatal("jeton sans compte accepté")
	}
	if _, err := decodeJWT("abc"); err == nil {
		t.Fatal("pas un JWT accepté")
	}
	now := time.Now()
	if !aRenouveler(now.Add(30*time.Minute), now) || aRenouveler(now.Add(3*time.Hour), now) || !aRenouveler(time.Time{}, now) {
		t.Fatal("aRenouveler")
	}
}

func TestTokenDePage(t *testing.T) {
	tok := jwtDe(map[string]any{"lid": "t2_page", "exp": time.Now().Add(time.Hour).Unix()})
	inner := fmt.Sprintf(`{"token":"%s","expires":%d}`, tok, time.Now().Add(time.Hour).UnixMilli())
	page := `<html><rs-app class="x" token="` + strings.ReplaceAll(inner, `"`, "&quot;") + `"></rs-app>`
	r, err := tokenDePage([]byte(page), "")
	if err != nil || r.Info.Account != "t2_page" || r.Token != tok {
		t.Fatalf("rs-app : %+v %v", r, err)
	}
	if _, err := tokenDePage([]byte("<html></html>"), ""); err != errSession {
		t.Fatalf("page sans jeton : %v", err)
	}
	if e := parseExpires(json.RawMessage("1764160000000")); e.Unix() != 1764160000 {
		t.Fatalf("expires ms : %v", e)
	}
}

// ---- Conversion ----

func TestConvertir(t *testing.T) {
	me := "@t2_moi:reddit.com"
	nom := func(u string) string { return map[string]string{"@t2_a:reddit.com": "alice"}[u] }
	m, ok := convertir(ev("m.room.message", "@t2_a:reddit.com", "$1", 1000, `{"msgtype":"m.text","body":"salut"}`), "!r", me, nom)
	if !ok || m.Text != "salut" || m.Sender != "alice" || m.FromMe || m.SenderID != "@t2_a:reddit.com" || m.Kind != "reddit" {
		t.Fatalf("texte : %+v", m)
	}
	m, _ = convertir(ev("m.room.message", me, "$2", 1000, `{"msgtype":"m.image","body":"Image","url":"mxc://reddit.com/abc","info":{"mimetype":"image/png","size":12,"w":3,"h":4}}`), "!r", me, nom)
	if !m.FromMe || m.Text != "" || len(m.Attachments) != 1 || m.Attachments[0].MediaID != "mxc://reddit.com/abc" || m.Attachments[0].W != 3 {
		t.Fatalf("image : %+v", m)
	}
	// Réponse : fil m.thread, avec repli vers la racine, puis réponse dans le fil.
	m, _ = convertir(ev("m.room.message", "@t2_a:reddit.com", "$3", 1000, `{"msgtype":"m.text","body":"oui","m.relates_to":{"rel_type":"m.thread","event_id":"$1","is_falling_back":true,"m.in_reply_to":{"event_id":"$1"}}}`), "!r", me, nom)
	if m.ReplyTo != "$1" || m.racine != "$1" {
		t.Fatalf("réponse : %+v", m)
	}
	m, _ = convertir(ev("m.room.message", "@t2_a:reddit.com", "$4", 1000, `{"msgtype":"m.text","body":"> cité\n\nnon","m.relates_to":{"rel_type":"m.thread","event_id":"$1","m.in_reply_to":{"event_id":"$3"}}}`), "!r", me, nom)
	if m.ReplyTo != "$3" || m.racine != "$1" || m.Text != "non" {
		t.Fatalf("réponse dans le fil : %+v", m)
	}
	// Modification : ignorée ; message rédigé : retiré.
	if _, ok := convertir(ev("m.room.message", me, "$5", 1, `{"msgtype":"m.text","body":"* x","m.relates_to":{"rel_type":"m.replace","event_id":"$2"}}`), "!r", me, nom); ok {
		t.Fatal("modification montrée")
	}
	r := ev("m.room.message", me, "$6", 1, `{}`)
	r.Unsigned.RedactedBecause = json.RawMessage(`{"type":"m.room.redaction"}`)
	if m, _ := convertir(r, "!r", me, nom); m.Kind != "admin" || m.Text != "Message retiré" {
		t.Fatalf("rédaction : %+v", m)
	}
}

func TestReactionsEtRedaction(t *testing.T) {
	capturer(t)
	p := newPont(context.Background())
	p.me = "@t2_moi:reddit.com"
	s := p.salonDe("!r")
	s.Type = "direct"
	p.evenement(s, ev("m.room.message", "@t2_a:reddit.com", "$1", 1000, `{"msgtype":"m.text","body":"hey"}`))
	m, ok, _ := p.evenement(s, ev("m.reaction", "@t2_a:reddit.com", "$r1", 1001, `{"m.relates_to":{"rel_type":"m.annotation","event_id":"$1","key":"jvuspmbga7081.gif"}}`))
	if !ok || len(m.Reactions) != 1 || m.Reactions[0].Emoji != "😂" {
		t.Fatalf("réaction : %+v", m)
	}
	// Forme « à plat » du client de Reddit.
	m, ok, _ = p.evenement(s, ev("m.reaction", p.me, "$r2", 1002, `{"event_id":"$1","key":"7url39xga7081.gif","rel_type":"m.annotation"}`))
	if !ok || len(m.Reactions) != 2 || !m.Reactions[1].FromMe || m.Reactions[1].Emoji != "👍" {
		t.Fatalf("réaction à plat : %+v", m)
	}
	m, ok, _ = p.evenement(s, mxEvent{Type: "m.room.redaction", Sender: "@t2_a:reddit.com", ID: "$x", Redacts: "$r1", Content: json.RawMessage(`{}`)})
	if !ok || len(m.Reactions) != 1 || !m.Reactions[0].FromMe {
		t.Fatalf("réaction retirée : %+v", m)
	}
	m, ok, ch := p.evenement(s, mxEvent{Type: "m.room.redaction", Sender: "@t2_a:reddit.com", ID: "$y", Content: json.RawMessage(`{"redacts":"$1"}`)})
	if !ok || m.Kind != "admin" || !ch || s.Preview != "Message retiré" {
		t.Fatalf("message retiré : %+v", m)
	}
	// La palette : émojis connus, clés, noms ; un émoji sans équivalent est refusé.
	if cleReaction("👍🏽") != "7url39xga7081.gif" || cleReaction("❤️") == "" || cleReaction(":snoo:") != "fsg2a1oga7081.gif" || cleReaction("🦄") != "" {
		t.Fatal("palette")
	}
}

func TestProfilEtMention(t *testing.T) {
	capturer(t)
	p := newPont(context.Background())
	p.me, p.monNom = "@t2_moi:reddit.com", "jo"
	s := p.salonDe("!g")
	s.Type = "private_group"
	m, _, _ := p.evenement(s, ev("m.room.message", "@t2_a:reddit.com", "$1", 1000, `{"msgtype":"m.text","body":"salut u/jo !"}`))
	if !m.MentionsMe || m.Sender != "t2_a" {
		t.Fatalf("mention : %+v", m)
	}
	m, ok, _ := p.evenement(s, ev("com.reddit.profile", "@t2_systeme:reddit.com", "$p", 1001, `{"username":"alice","icon_url":"https://styles.redditmedia.com/a.png?s=sig","m.relates_to":{"event_id":"$1"}}`))
	if !ok || m.Sender != "alice" || p.icones["@t2_a:reddit.com"] != "https://styles.redditmedia.com/a.png" {
		t.Fatalf("profil : %+v %v", m, p.icones)
	}
	if mentionne("bonjour u/jonathan", "jo") || !mentionne("@Jo, viens", "jo") {
		t.Fatal("mentionne")
	}
}

// ---- Plafonds ----

func TestPlafonds(t *testing.T) {
	if plafondImage("image/png") != 20<<20 || plafondImage("image/gif") != 100<<20 || plafondImage("video/mp4") != 0 || plafondImage("application/pdf") != 0 {
		t.Fatal("plafondImage")
	}
	if texteTropLong(strings.Repeat("é", 10000)) || !texteTropLong(strings.Repeat("a", 10001)) {
		t.Fatal("texteTropLong")
	}
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	_ = os.WriteFile(filepath.Join(dir, "a.png"), png, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "a.mp4"), []byte("\x00\x00\x00\x18ftypmp42"), 0o600)
	if _, mime, err := lireImage(filepath.Join(dir, "a.png")); err != nil || mime != "image/png" {
		t.Fatalf("png : %s %v", mime, err)
	}
	if _, _, err := lireImage(filepath.Join(dir, "a.mp4")); err == nil {
		t.Fatal("vidéo acceptée")
	}
	if _, _, err := urlMedia("https://evil.example/x.png"); err == nil {
		t.Fatal("hôte refusé accepté")
	}
	if u, jeton, _ := urlMedia("mxc://reddit.com/abc"); !jeton || !strings.HasSuffix(u, "/_matrix/media/v3/download/reddit.com/abc") {
		t.Fatalf("mxc : %s", u)
	}
	if _, _, err := urlMedia("mxc://reddit.com/../x"); err == nil {
		t.Fatal("mxc douteux accepté")
	}
}

// ---- Classement ----

func TestClassement(t *testing.T) {
	me := "@t2_moi:reddit.com"
	direct := &salon{ID: "!d", Type: "direct", Members: map[string]*membre{me: {Membership: "join"}, "@t2_a:reddit.com": {Name: "alice", Membership: "join"}}}
	if c := direct.convOut(me, nil); c.Group || c.Large || c.Name != "alice" {
		t.Fatalf("à deux : %+v", c)
	}
	titre := &salon{ID: "!t", Type: "reddit_titled_direct", Name: "Vente de vélo"}
	if titre.sorte() != sorteDirect {
		t.Fatal("reddit_titled_direct")
	}
	g := &salon{ID: "!g", Type: "private_group", Members: map[string]*membre{me: {Membership: "join"}, "@t2_a:reddit.com": {Name: "alice", Membership: "join"}, "@t2_b:reddit.com": {Name: "bob", Membership: "join"}}}
	if c := g.convOut(me, nil); !c.Group || c.Large || c.Name != "alice, bob" {
		t.Fatalf("groupe : %+v", c)
	}
	// Un salon de subreddit : grand espace, parent le subreddit.
	sr := &salon{ID: "!s", Name: "général"}
	sr.noteState(etat("com.reddit.chat.type", "", "", `{"type":"subreddit_channel","subreddit":{"name":"r/QuebecGames","icon":"https://styles.redditmedia.com/i.png"}}`), me)
	c := sr.convOut(me, nil)
	if !c.Large || c.Kind != "subreddit" || c.Parent != "sr:quebecgames" || c.ParentName != "r/QuebecGames" || sr.SubredditIcon == "" {
		t.Fatalf("communauté : %+v", c)
	}
	// Un type inconnu sans subreddit reste dans la boîte de réception.
	if (&salon{Type: "mystere"}).sorte() != sorteGroupe {
		t.Fatal("type inconnu")
	}
	// Invitation sans type : l'indice is_direct.
	inv := &salon{ID: "!i", Invite: true}
	inv.noteState(etat("m.room.member", me, "@t2_a:reddit.com", `{"membership":"invite","is_direct":true}`), me)
	if c := inv.convOut(me, map[string]string{"@t2_a:reddit.com": "alice"}); !c.Request || c.Group || c.Name != "alice" || c.Preview != "Demande de message" {
		t.Fatalf("demande : %+v", c)
	}
	// Liste : communautés après le reste ; salons cachés absents.
	l := trierConvs(map[string]*salon{
		"!d": direct, "!s": sr, "!h": {ID: "!h", Type: "direct", Hidden: true, LastTS: 9e12},
	}, me, nil)
	if len(l) != 2 || l[0].ID != "!d" || l[1].ID != "!s" {
		t.Fatalf("liste : %+v", l)
	}
}

// ---- Faux serveur Matrix (et faux reddit.com) ----

type fauxServeur struct {
	mu       sync.Mutex
	syncs    int
	envoyes  []string
	recus    []string
	jetons   int
	srv      *httptest.Server
	cookieOK bool
}

func nouveauFaux(t *testing.T) *fauxServeur {
	f := &fauxServeur{}
	moi := "@t2_moi:reddit.com"
	tok := jwtDe(map[string]any{"lid": "t2_moi", "exp": time.Now().Add(24 * time.Hour).Unix()})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /svc/shreddit/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		ck, _ := r.Cookie("reddit_session")
		if r.FormValue("csrf_token") != "c5rf" || ck == nil || r.Header.Get("X-Reddit-Client-Version") == "" || !strings.Contains(r.UserAgent(), "Chrome") {
			w.WriteHeader(403)
			return
		}
		f.mu.Lock()
		f.jetons++
		f.mu.Unlock()
		fmt.Fprintf(w, `{"token":%q,"expires":%d}`, tok, time.Now().Add(24*time.Hour).UnixMilli())
	})
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+tok {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"errcode":"M_UNKNOWN_TOKEN","error":"?"}`)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /_matrix/client/v3/sync", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "" || r.URL.Query().Get("set_presence") != "offline" {
			t.Errorf("sync : filtre ou présence inattendus : %s", r.URL.RawQuery)
		}
		f.mu.Lock()
		f.syncs++
		n := f.syncs
		f.mu.Unlock()
		if r.URL.Query().Get("since") == "" {
			fmt.Fprint(w, `{"next_batch":"s1","rooms":{
			  "join":{
			    "!dm:reddit.com":{"state":{"events":[
			        {"type":"com.reddit.chat.type","state_key":"","content":{"type":"direct"}},
			        {"type":"m.room.member","state_key":"@t2_moi:reddit.com","content":{"membership":"join","displayname":"jo"}},
			        {"type":"m.room.member","state_key":"@t2_a:reddit.com","content":{"membership":"join","displayname":"alice"}}]},
			      "timeline":{"events":[{"type":"m.room.message","sender":"@t2_a:reddit.com","event_id":"$m1","origin_server_ts":1000,"content":{"msgtype":"m.text","body":"Allo !"}}],"prev_batch":"p1"},
			      "unread_notifications":{"notification_count":1}},
			    "!sr:reddit.com":{"state":{"events":[
			        {"type":"com.reddit.chat.type","state_key":"","content":{"type":"subreddit_channel","subreddit_name":"QuebecGames"}},
			        {"type":"m.room.name","state_key":"","content":{"name":"général"}}]},
			      "timeline":{"events":[{"type":"m.room.message","sender":"@t2_b:reddit.com","event_id":"$s1","origin_server_ts":2000,"content":{"msgtype":"m.text","body":"Bienvenue"}}]}}},
			  "peek":{
			    "!req:reddit.com":{"state":{"events":[{"type":"com.reddit.chat.type","state_key":"","content":{"type":"direct"}},
			        {"type":"m.room.member","state_key":"@t2_c:reddit.com","content":{"membership":"join","displayname":"carl"}}]},
			      "timeline":{"events":[{"type":"m.room.message","sender":"@t2_c:reddit.com","event_id":"$q1","origin_server_ts":3000,"content":{"msgtype":"m.text","body":"Bonjour, une question"}}]}}}}}`)
			return
		}
		if n == 2 {
			fmt.Fprint(w, `{"next_batch":"s2","rooms":{"join":{"!dm:reddit.com":{
			  "timeline":{"events":[
			    {"type":"m.room.message","sender":"@t2_a:reddit.com","event_id":"$m2","origin_server_ts":5000,"content":{"msgtype":"m.image","body":"Image","url":"mxc://reddit.com/img1","info":{"mimetype":"image/jpeg","w":10,"h":10}}},
			    {"type":"m.reaction","sender":"@t2_a:reddit.com","event_id":"$r1","origin_server_ts":5001,"content":{"m.relates_to":{"rel_type":"m.annotation","event_id":"$m2","key":"jvuspmbga7081.gif"}}}]},
			  "ephemeral":{"events":[{"type":"m.typing","content":{"user_ids":["@t2_a:reddit.com"]}}]}}}}}`)
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
		fmt.Fprintf(w, `{"next_batch":"s%d"}`, n)
	}))
	mux.HandleFunc("GET /_matrix/client/v3/joined_rooms", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"joined_rooms":["!dm:reddit.com","!sr:reddit.com","!vieux:reddit.com"]}`)
	}))
	mux.HandleFunc("GET /_matrix/client/v3/rooms/{room}/state/{typ}/{cle...}", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("room") != "!vieux:reddit.com" {
			w.WriteHeader(404)
			return
		}
		switch r.PathValue("typ") {
		case "com.reddit.chat.type":
			fmt.Fprint(w, `{"type":"private_group"}`)
		default:
			w.WriteHeader(404)
			fmt.Fprint(w, `{"errcode":"M_NOT_FOUND","error":"?"}`)
		}
	}))
	mux.HandleFunc("GET /_matrix/client/v3/rooms/{room}/members", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"chunk":[{"type":"m.room.member","state_key":"@t2_d:reddit.com","content":{"membership":"join","displayname":"dany"}},
		  {"type":"m.room.member","state_key":"@t2_e:reddit.com","content":{"membership":"join","displayname":"eve"}}]}`)
	}))
	mux.HandleFunc("GET /_matrix/client/v3/rooms/{room}/messages", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dir") != "b" {
			t.Errorf("messages : dir %q", r.URL.Query().Get("dir"))
		}
		switch r.PathValue("room") {
		case "!vieux:reddit.com":
			fmt.Fprint(w, `{"chunk":[{"type":"m.room.message","sender":"@t2_d:reddit.com","event_id":"$v1","origin_server_ts":500,"content":{"msgtype":"m.text","body":"vieux groupe"}}],"end":"t0_0"}`)
		case "!dm:reddit.com":
			if r.URL.Query().Get("from") == "e1" {
				fmt.Fprint(w, `{"chunk":[{"type":"m.room.message","sender":"@t2_moi:reddit.com","event_id":"$m0","origin_server_ts":900,"content":{"msgtype":"m.text","body":"premier"}}],"end":"t0_0"}`)
				return
			}
			fmt.Fprint(w, `{"chunk":[{"type":"m.room.message","sender":"@t2_a:reddit.com","event_id":"$m1","origin_server_ts":1000,"content":{"msgtype":"m.text","body":"Allo !"}}],"end":"e1",
			  "state":[{"type":"m.room.member","state_key":"@t2_a:reddit.com","content":{"membership":"join","displayname":"alice"}}]}`)
		default:
			fmt.Fprint(w, `{"chunk":[]}`)
		}
	}))
	mux.HandleFunc("GET /_matrix/client/v3/profile/{user}", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"displayname":"jo"}`)
	}))
	mux.HandleFunc("PUT /_matrix/client/v3/rooms/{room}/send/{typ}/{txn}", auth(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.envoyes = append(f.envoyes, r.PathValue("typ")+" "+string(b))
		f.mu.Unlock()
		fmt.Fprintf(w, `{"event_id":"$env%s"}`, r.PathValue("txn"))
	}))
	mux.HandleFunc("POST /_matrix/client/v3/rooms/{room}/receipt/{typ}/{evt}", auth(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.recus = append(f.recus, r.PathValue("room")+" "+r.PathValue("evt"))
		f.mu.Unlock()
		fmt.Fprint(w, `{}`)
	}))
	mux.HandleFunc("POST /_matrix/client/v3/join/{room}", auth(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"room_id":%q}`, r.PathValue("room"))
	}))
	mux.HandleFunc("GET /_matrix/media/v3/download/{srv}/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0 jpeg"))
	}))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	_ = moi
	return f
}

func TestPontContreFauxServeur(t *testing.T) {
	sortie := capturer(t)
	f := nouveauFaux(t)
	redditBase, matrixBase = f.srv.URL, f.srv.URL
	ctx, annule := context.WithCancel(context.Background())
	defer annule()
	p := newPont(ctx)
	p.rest.gap = time.Millisecond
	p.cdn.gap = time.Millisecond

	// Sans reddit_session : refus clair, sans réseau.
	p.login(map[string]string{"csrf_token": "c5rf"})
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "error" && e["where"] == "login" })

	go p.login(map[string]string{"reddit_session": "SECRETSESS", "csrf_token": "c5rf", "inutile": "x"})
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "paired" })
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "account" && e["name"] == "jo" })
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "state" && e["state"] == "ready" })

	// La liste : la conversation, la demande, le salon de subreddit (Groupes).
	liste := sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "conversations" })
	items := liste["items"].([]any)
	par := map[string]map[string]any{}
	for _, x := range items {
		c := x.(map[string]any)
		par[c["id"].(string)] = c
	}
	if c := par["!dm:reddit.com"]; c == nil || c["name"] != "alice" || c["unread"] != true || c["preview"] != "Allo !" {
		t.Fatalf("conversation : %v", c)
	}
	if c := par["!req:reddit.com"]; c == nil || c["request"] != true || c["name"] != "carl" {
		t.Fatalf("demande : %v", c)
	}
	if c := par["!sr:reddit.com"]; c == nil || c["large"] != true || c["kind"] != "subreddit" || c["parent"] != "sr:quebecgames" || c["parent_name"] != "r/QuebecGames" {
		t.Fatalf("salon de subreddit : %v", c)
	}

	// Le direct : une image, une réaction, « écrit… ».
	sortie.attendre(t, func(e map[string]any) bool {
		it, _ := e["item"].(map[string]any)
		r, _ := it["reactions"].([]any)
		return e["ev"] == "message" && it["id"] == "$m2" && len(r) == 1
	})
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "typing" && e["on"] == true && e["conv"] == "!dm:reddit.com" })

	// Le salon absent de /sync, retrouvé par /joined_rooms.
	sortie.attendre(t, func(e map[string]any) bool {
		if e["ev"] != "conversations" {
			return false
		}
		for _, x := range e["items"].([]any) {
			c := x.(map[string]any)
			if c["id"] == "!vieux:reddit.com" && c["group"] == true && c["name"] == "dany, eve" && c["preview"] == "vieux groupe" {
				return true
			}
		}
		return false
	})

	// L'historique : une page, puis la suivante (fin : t0_0). Rien n'est lu.
	p.messages(Cmd{Conv: "!dm:reddit.com", Count: 30})
	pg := sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "messages" && e["older"] == false })
	if pg["more"] != true {
		t.Fatalf("page : %v", pg)
	}
	p.messages(Cmd{Conv: "!dm:reddit.com", Count: 30, Before: "$m1"})
	pg = sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "messages" && e["older"] == true })
	if pg["more"] != false || len(pg["items"].([]any)) != 1 {
		t.Fatalf("page plus ancienne : %v", pg)
	}
	f.mu.Lock()
	if len(f.recus) != 0 {
		t.Fatalf("lu sans action : %v", f.recus)
	}
	f.mu.Unlock()

	// « Lu » (sur action), envoi d'une réponse, réaction refusée hors palette.
	p.markRead(Cmd{Conv: "!dm:reddit.com"})
	p.send(Cmd{Conv: "!dm:reddit.com", Text: "Salut", TmpID: "t1", ReplyTo: "$m1"})
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "sent" && e["tmp_id"] == "t1" && e["ok"] == true })
	p.react(Cmd{Conv: "!dm:reddit.com", ID: "$m1", Emoji: "🦄"})
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "error" && e["where"] == "react" })
	p.react(Cmd{Conv: "!dm:reddit.com", ID: "$m1", Emoji: "👍"})
	p.markRead(Cmd{Conv: "!dm:reddit.com"}) // déjà lu : rien ne part
	f.mu.Lock()
	envoyes, recus := strings.Join(f.envoyes, "\n"), f.recus
	f.mu.Unlock()
	if !strings.Contains(envoyes, `"m.thread"`) || !strings.Contains(envoyes, `"$m1"`) || !strings.Contains(envoyes, "7url39xga7081.gif") || strings.Contains(envoyes, "🦄") {
		t.Fatalf("envoyés : %s", envoyes)
	}
	if len(recus) != 1 || !strings.HasPrefix(recus[0], "!dm:reddit.com ") {
		t.Fatalf("accusés : %v", recus)
	}

	// Une vidéo est refusée avant tout envoi.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "v.mp4"), []byte("\x00\x00\x00\x18ftypmp42"), 0o600)
	p.sendMedia(Cmd{Conv: "!dm:reddit.com", Path: filepath.Join(dir, "v.mp4"), TmpID: "t2"})
	sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "sent" && e["tmp_id"] == "t2" && e["ok"] == false })

	// Téléchargement d'une image du serveur Matrix.
	p.download(Cmd{MediaID: "mxc://reddit.com/img1"})
	md := sortie.attendre(t, func(e map[string]any) bool { return e["ev"] == "media" })
	if md["path"] == nil {
		t.Fatalf("média : %v", md)
	}

	// La session : 0600 dans un dossier 0700, sans le cookie inutile.
	st, _ := os.Stat(sessionPath())
	dst, _ := os.Stat(rdDir())
	if st.Mode().Perm() != 0o600 || dst.Mode().Perm() != 0o700 {
		t.Fatalf("permissions : %v %v", st.Mode(), dst.Mode())
	}
	b, _ := os.ReadFile(sessionPath())
	if strings.Contains(string(b), "inutile") {
		t.Fatal("cookie inutile gardé")
	}
	// Aucune ligne émise ne contient le jeton ni les cookies.
	for _, e := range sortie.evs() {
		l, _ := json.Marshal(e)
		if strings.Contains(string(l), "SECRETSESS") || strings.Contains(string(l), "signature") {
			t.Fatalf("secret émis : %s", l)
		}
	}
	p.unpair()
	if _, err := os.Stat(sessionPath()); err == nil {
		t.Fatal("session gardée après unpair")
	}
}
