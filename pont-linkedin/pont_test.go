package main

// Les essais du pont LinkedIn : jamais contre linkedin.com. Un faux client
// (liAPI) note les envois, réactions et « lu » ; les pages et le direct sont
// fabriqués ici.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/jsontime"

	"go.mau.fi/mautrix-linkedin/pkg/linkedingo"
)

const moi = "ACoAAMOI"

// ---- Le faux client ----

type appel struct {
	quoi  string
	conv  string
	texte string
	emoji string
	etat  linkedingo.ConversationState
	rc    []linkedingo.SendRenderContent
}

type fauxAPI struct {
	mu       sync.Mutex
	appels   []appel
	pages    map[string]*pageMessage // URN de conversation → page
	fichiers map[string][]byte       // adresse → contenu
	refus    map[string]int          // adresse → code HTTP
}

func (f *fauxAPI) noter(a appel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appels = append(f.appels, a)
}

func (f *fauxAPI) liste(quoi string) []appel {
	f.mu.Lock()
	defer f.mu.Unlock()
	var l []appel
	for _, a := range f.appels {
		if quoi == "" || a.quoi == quoi {
			l = append(l, a)
		}
	}
	return l
}

func (f *fauxAPI) GetCurrentUserProfile(ctx context.Context) (*linkedingo.UserProfile, error) {
	return &linkedingo.UserProfile{MiniProfile: linkedingo.MiniProfile{FirstName: "Jo", LastName: "Moi", EntityURN: linkedingo.NewURN("urn:li:fs_miniProfile:" + moi)}}, nil
}

func (f *fauxAPI) GetConversationsBySyncToken(ctx context.Context) (*deltaConvs, error) {
	f.noter(appel{quoi: "delta"})
	return &deltaConvs{}, nil
}

func (f *fauxAPI) GetConversationsUpdatedBefore(ctx context.Context, before time.Time) (*pageConvs, error) {
	f.noter(appel{quoi: "page_convs"})
	return &pageConvs{}, nil
}

func (f *fauxAPI) GetMessagesBefore(ctx context.Context, c linkedingo.URN, before time.Time, count int) (*pageMessage, error) {
	f.noter(appel{quoi: "messages", conv: c.String()})
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.pages[c.String()]; p != nil {
		return p, nil
	}
	return &pageMessage{}, nil
}

func (f *fauxAPI) GetMessagesWithPrevCursor(ctx context.Context, c linkedingo.URN, cursor string, count int) (*pageMessage, error) {
	f.noter(appel{quoi: "curseur", conv: c.String(), texte: cursor})
	return &pageMessage{}, nil
}

func (f *fauxAPI) SendMessage(ctx context.Context, c linkedingo.URN, body linkedingo.SendMessageBody, rc []linkedingo.SendRenderContent, txn string, state linkedingo.ConversationState) (*linkedingo.MessageSentResponse, error) {
	f.noter(appel{quoi: "send", conv: c.String(), texte: body.Text, etat: state, rc: rc})
	return &linkedingo.MessageSentResponse{Data: linkedingo.Message{
		EntityURN:   linkedingo.NewURN(prefMsg + moi + ",2-NOUVEAU)"),
		Body:        linkedingo.AttributedText{Text: body.Text},
		DeliveredAt: jsontime.UM(time.UnixMilli(5000)),
		RenderContent: func() []linkedingo.RenderContent {
			var l []linkedingo.RenderContent
			for _, r := range rc {
				if r.File != nil {
					l = append(l, linkedingo.RenderContent{File: &linkedingo.FileAttachment{Name: r.File.Name, MediaType: r.File.MediaType, ByteSize: r.File.ByteSize, URL: "https://www.linkedin.com/dms/prv/fichier"}})
				}
			}
			return l
		}(),
	}}, nil
}

func (f *fauxAPI) SendReaction(ctx context.Context, msg linkedingo.URN, emoji string) error {
	f.noter(appel{quoi: "react", texte: msg.String(), emoji: emoji})
	return nil
}

func (f *fauxAPI) RemoveReaction(ctx context.Context, msg linkedingo.URN, emoji string) error {
	f.noter(appel{quoi: "unreact", texte: msg.String(), emoji: emoji})
	return nil
}

func (f *fauxAPI) MarkConversationRead(ctx context.Context, convs ...linkedingo.URN) (*linkedingo.MarkThreadReadResponse, error) {
	for _, c := range convs {
		f.noter(appel{quoi: "read", conv: c.String()})
	}
	return &linkedingo.MarkThreadReadResponse{}, nil
}

func (f *fauxAPI) UploadMedia(ctx context.Context, t linkedingo.MediaUploadType, filename, contentType string, size int, r io.Reader) (linkedingo.URN, error) {
	b, _ := io.ReadAll(r)
	f.noter(appel{quoi: "upload", texte: filename + "|" + contentType + "|" + string(t), emoji: string(b)})
	return linkedingo.NewURN("urn:li:digitalmediaAsset:ABC"), nil
}

func (f *fauxAPI) DownloadHTTP(ctx context.Context, u string) (*http.Response, error) {
	f.noter(appel{quoi: "get", texte: u})
	f.mu.Lock()
	defer f.mu.Unlock()
	if code := f.refus[u]; code != 0 {
		return nil, &linkedingo.ResponseError{StatusCode: code}
	}
	b, ok := f.fichiers[u]
	if !ok {
		return nil, &linkedingo.ResponseError{StatusCode: 404}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {http.DetectContentType(b)}}, Body: io.NopCloser(bytes.NewReader(b))}, nil
}

func (f *fauxAPI) RealtimeConnect(ctx context.Context) error { return nil }
func (f *fauxAPI) RealtimeDisconnect()                       {}
func (f *fauxAPI) IsLoggedIn() bool                          { return true }

// ---- Outils ----

type sortie struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *sortie) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(b)
}

func (s *sortie) evs(sorte string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var l []map[string]any
	for _, ligne := range strings.Split(s.buf.String(), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(ligne), &m) == nil && (sorte == "" || m["ev"] == sorte) {
			l = append(l, m)
		}
	}
	return l
}

func (s *sortie) brut() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// essai : un pont branché sur un faux client, données dans un dossier temporaire.
func essai(t *testing.T) (*Pont, *fauxAPI, *sortie) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("JASETTE_DATA", d)
	t.Setenv("JASETTE_MEDIA", filepath.Join(d, "media"))
	t.Setenv("JASETTE_LINKEDIN_LECTURE_SEULE", "")
	log = zerolog.Nop()
	s := &sortie{}
	outMu.Lock()
	out = json.NewEncoder(s)
	outMu.Unlock()
	ctx, annule := context.WithCancel(context.Background())
	t.Cleanup(annule)
	f := &fauxAPI{pages: map[string]*pageMessage{}, fichiers: map[string][]byte{}, refus: map[string]int{}}
	p := newPont(ctx)
	p.rest.gap, p.cdn.gap = 0, 0
	p.me, p.monNom = moi, "Jo Moi"
	p.sf = &sessionFile{Profile: moi, Nom: "Jo Moi"}
	p.api = &rythme{api: f, rest: &p.rest, cdn: &p.cdn}
	p.pret = true
	return p, f, s
}

func participant(id, prenom, nom string) linkedingo.MessagingParticipant {
	return linkedingo.MessagingParticipant{
		EntityURN: linkedingo.NewURN("urn:li:msg_messagingParticipant:urn:li:fsd_profile:" + id),
		ParticipantType: linkedingo.ParticipantType{Member: &linkedingo.MemberParticipantInfo{
			FirstName: linkedingo.AttributedText{Text: prenom}, LastName: linkedingo.AttributedText{Text: nom},
			ProfilePicture: &linkedingo.VectorImage{RootURL: "https://media.licdn.com/dms/image/v2/" + id + "/photo-shrink_", Artifacts: []linkedingo.VectorArtifact{
				{Width: 100, Height: 100, FileIdentifyingURLPathSegment: "100_100/0/1?e=1&t=a"},
				{Width: 200, Height: 200, FileIdentifyingURLPathSegment: "200_200/0/1?e=1&t=b"},
				{Width: 800, Height: 800, FileIdentifyingURLPathSegment: "800_800/0/1?e=1&t=c"},
			}},
		}},
	}
}

func msgLI(id, conv, auteur string, ts int64, texte string) linkedingo.Message {
	return linkedingo.Message{
		EntityURN:       linkedingo.NewURN(prefMsg + moi + "," + id + ")"),
		Body:            linkedingo.AttributedText{Text: texte},
		DeliveredAt:     jsontime.UM(time.UnixMilli(ts)),
		Sender:          participant(auteur, "Prénom"+auteur, "Nom"),
		ConversationURN: linkedingo.NewURN(prefConv + moi + "," + conv + ")"),
	}
}

func convLI(id string, groupe bool, autres ...string) linkedingo.Conversation {
	parts := []linkedingo.MessagingParticipant{participant(moi, "Jo", "Moi")}
	for _, a := range autres {
		parts = append(parts, participant(a, "Prénom"+a, "Nom"))
	}
	return linkedingo.Conversation{
		EntityURN:                linkedingo.NewURN(prefConv + moi + "," + id + ")"),
		LastActivityAt:           jsontime.UM(time.UnixMilli(1000)),
		GroupChat:                groupe,
		ConversationParticipants: parts,
		Categories:               []string{"INBOX", "PRIMARY_INBOX"},
		Read:                     true,
	}
}

func attendre(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("délai dépassé")
}

// ---- Les essais ----

func TestIdsCourts(t *testing.T) {
	u := linkedingo.NewURN(prefConv + moi + ",2-ZDhk+/ab==)")
	id := court(u, moi)
	if id != "2-ZDhk+/ab==" {
		t.Fatalf("id court : %q", id)
	}
	if got := urnConv(id, moi).String(); got != u.String() {
		t.Fatalf("aller-retour : %q", got)
	}
	m := linkedingo.NewURN(prefMsg + moi + ",2-MSG==)")
	if court(m, moi) != "2-MSG==" || urnMsg("2-MSG==", moi).String() != m.String() {
		t.Fatalf("message : %q", court(m, moi))
	}
	// La boîte d'un autre (ou un URN inconnu) : l'URN entier, qui revient tel quel.
	autre := linkedingo.NewURN(prefConv + "AUTRE,2-X)")
	if court(autre, moi) != autre.String() || urnConv(autre.String(), moi).String() != autre.String() {
		t.Fatalf("autre boîte : %q", court(autre, moi))
	}
}

func TestCookiesDeLaFenetre(t *testing.T) {
	c, ua := cookiesValides(map[string]string{
		"li_at": "AQEabc", "JSESSIONID": `"ajax:123456"`, "bcookie": `"v=2&x"`,
		"jasette_user_agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15", "jasette_autre": "x",
		"mauvais nom": "x", "vide": "",
	})
	if ua == "" || c["jasette_user_agent"] != "" || c["jasette_autre"] != "" || c["mauvais nom"] != "" || c["vide"] != "" {
		t.Fatalf("filtre : %v %q", c, ua)
	}
	jar, err := jarDe(c)
	if err != nil {
		t.Fatal(err)
	}
	if jar.GetCookie("li_at") != "AQEabc" || jar.GetCookie("JSESSIONID") != "ajax:123456" {
		t.Fatalf("bocal : li_at=%q JSESSIONID=%q", jar.GetCookie("li_at"), jar.GetCookie("JSESSIONID"))
	}
	if h := enTetesNavigateur("Mozilla/5.0 (Linux; Android 14; Pixel) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36"); h.Get("Sec-Ch-Ua-Mobile") != "?1" || h.Get("Sec-Ch-Ua-Platform") != `"Android"` {
		t.Fatalf("en-têtes Android : %v", h)
	}
	if h := enTetesNavigateur("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"); h.Get("Sec-Ch-Ua") != "" {
		t.Fatalf("WebKit n'envoie pas Sec-Ch-Ua : %v", h)
	}
}

func TestLoginSansCookies(t *testing.T) {
	p, f, s := essai(t)
	p.login(map[string]string{"bcookie": "x"})
	if len(f.liste("")) != 0 {
		t.Fatalf("aucune requête attendue : %v", f.liste(""))
	}
	if e := s.evs("error"); len(e) != 1 || e[0]["where"] != "login" {
		t.Fatalf("erreur : %v", e)
	}
}

func TestConvertirPiecesEtCleJamaisNulle(t *testing.T) {
	lm := msgLI("2-A", "2-C", "BOB", 1000, "")
	lm.Subject = "Offre d'emploi"
	lm.Body.Text = "Bonjour"
	lm.RenderContent = []linkedingo.RenderContent{
		{VectorImage: &linkedingo.VectorImage{RootURL: "https://media.licdn.com/dms/image/v2/X/messaging-image_", Artifacts: []linkedingo.VectorArtifact{
			{Width: 3000, Height: 2000, FileIdentifyingURLPathSegment: "3000/0/1?t=o"},
			{Width: 1280, Height: 853, FileIdentifyingURLPathSegment: "1280/0/1?t=v"},
			{Width: 640, Height: 426, FileIdentifyingURLPathSegment: "640/0/1?t=p"},
		}}},
		{Video: &linkedingo.VideoPlayMetadata{ProgressiveStreams: []linkedingo.ProgressiveDownloadMetadata{
			{BitRate: 1, Width: 320, Height: 240, MediaType: "video/mp4", StreamingLocations: []linkedingo.StreamingLocation{{URL: "https://dms.licdn.com/basse"}}},
			{BitRate: 9, Width: 1280, Height: 720, MediaType: "video/mp4", StreamingLocations: []linkedingo.StreamingLocation{{URL: "https://dms.licdn.com/haute"}}},
		}}},
		{Audio: &linkedingo.AudioMetadata{URL: "https://www.linkedin.com/dms/prv/audio"}},
		{File: &linkedingo.FileAttachment{Name: "cv.pdf", MediaType: "application/pdf", ByteSize: 1234, URL: "https://www.linkedin.com/dms/prv/cv"}},
		{ExternalMedia: &linkedingo.ExternalMedia{Title: "bravo", Media: linkedingo.ExternalProxyImage{URL: "https://media.licdn.com/gif"}}},
	}
	m := convertir(lm, "2-C", moi, func(string) string { return "" })
	if !strings.HasPrefix(m.Text, "Offre d'emploi\n\nBonjour") {
		t.Fatalf("InMail : %q", m.Text)
	}
	if len(m.Attachments) != 5 {
		t.Fatalf("pièces : %d", len(m.Attachments))
	}
	var k mediaKey
	_ = json.Unmarshal(m.Attachments[0].Key, &k)
	if !strings.HasSuffix(k.URL, "3000/0/1?t=o") || !strings.HasSuffix(k.Vign, "1280/0/1?t=v") || m.Attachments[0].W != 3000 {
		t.Fatalf("image : %+v %+v", k, m.Attachments[0])
	}
	_ = json.Unmarshal(m.Attachments[1].Key, &k)
	if k.URL != "https://dms.licdn.com/haute" || m.Attachments[1].Mime != "video/mp4" {
		t.Fatalf("vidéo : %+v", k)
	}
	if m.Attachments[2].Mime != "audio/mp4" || m.Attachments[3].Name != "cv.pdf" || m.Attachments[4].Mime != "image/gif" {
		t.Fatalf("autres : %+v", m.Attachments)
	}
	if m.Attachments[0].MediaID != "2-A#0" || m.Attachments[4].MediaID != "2-A#4" {
		t.Fatalf("ids : %q %q", m.Attachments[0].MediaID, m.Attachments[4].MediaID)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), `"key":null`) {
		t.Fatalf("clé nulle : %s", b)
	}
	if m.Sender != "PrénomBOB Nom" || m.SenderID != "BOB" || m.FromMe {
		t.Fatalf("auteur : %+v", m)
	}
}

func TestConvertirRetireReponseTransfert(t *testing.T) {
	lm := msgLI("2-B", "2-C", "BOB", 2000, "oui")
	lm.MessageBodyRenderFormat = linkedingo.MessageBodyRenderFormatRecalled
	if m := convertir(lm, "2-C", moi, func(string) string { return "" }); m.System != "deleted" || m.Text != "🚫 Message retiré" || m.Kind != "admin" {
		t.Fatalf("retiré : %+v", m)
	}
	lm = msgLI("2-B", "2-C", moi, 2000, "oui")
	orig := msgLI("2-A", "2-C", "BOB", 1000, "On se voit demain ?")
	fw := linkedingo.ForwardedMessage{ForwardedBody: linkedingo.AttributedText{Text: "texte transféré"}, OriginalSender: participant("ZOE", "Zoé", "Z")}
	lm.RenderContent = []linkedingo.RenderContent{{RepliedMessageContent: &linkedingo.RepliedMessage{OriginalMessage: orig}}, {ForwardedMessage: &fw},
		{HostURNData: &linkedingo.HostURNData{Type: "FEED_UPDATE", HostURN: linkedingo.NewURN("urn:li:fsd_update:(urn:li:activity:123,MESSAGING_RESHARE,EMPTY,DEFAULT,false)")}}}
	m := convertir(lm, "2-C", moi, func(string) string { return "" })
	if !m.FromMe || m.ReplyTo != "2-A" || m.ReplyText != "On se voit demain ?" {
		t.Fatalf("réponse : %+v", m)
	}
	if !strings.HasPrefix(m.Text, "↪ « On se voit demain ? »\noui\n↗️ Message transféré\nZoé Z :\ntexte transféré") {
		t.Fatalf("texte : %q", m.Text)
	}
	if !strings.Contains(m.Text, "https://www.linkedin.com/feed/update/urn:li:activity:123/") {
		t.Fatalf("publication : %q", m.Text)
	}
	if apercu(m) != "oui ↗️ Message transféré Zoé Z : texte transféré https://www.linkedin.com/feed/update/urn:li:activity:123/" {
		t.Fatalf("aperçu : %q", apercu(m))
	}
}

func TestListeEtDemande(t *testing.T) {
	p, _, s := essai(t)
	lc := convLI("2-C", false, "BOB")
	lc.State = linkedingo.ConversationStatePending
	lc.Read = false
	lc.Messages.Elements = []linkedingo.Message{msgLI("2-A", "2-C", "BOB", 1000, "Bonjour, une offre")}
	spam := convLI("2-S", false, "SPAM")
	spam.Categories = []string{"SPAM"}
	p.appliquerConvs([]linkedingo.Conversation{lc, spam}, moi, true)
	p.listConversations(false, 0)
	l := s.evs("conversations")
	items := l[len(l)-1]["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("liste : %v", items)
	}
	c := items[0].(map[string]any)
	if c["id"] != "2-C" || c["name"] != "PrénomBOB Nom" || c["request"] != true || c["unread"] != true || c["preview"] != "Demande de message · Bonjour, une offre" {
		t.Fatalf("conversation : %v", c)
	}
	// « Accepter » : dans la boîte, la réponse l'acceptera chez LinkedIn.
	p.acceptRequest(Cmd{Conv: "2-C"})
	if c := p.convs["2-C"].out(); c.Request || c.Preview != "Bonjour, une offre" {
		t.Fatalf("acceptée : %+v", c)
	}
	p.ignoreRequest(Cmd{Conv: "2-C"})
	if p.convs["2-C"].visible() || len(s.evs("deleted")) != 1 {
		t.Fatal("ignorée")
	}
}

func TestEnvoiAccepteLaDemande(t *testing.T) {
	p, f, s := essai(t)
	lc := convLI("2-C", false, "BOB")
	lc.State = linkedingo.ConversationStatePending
	p.appliquerConvs([]linkedingo.Conversation{lc}, moi, false)
	p.send(Cmd{Conv: "2-C", Text: "Merci !", TmpID: "tmp1"})
	envois := f.liste("send")
	if len(envois) != 1 || envois[0].conv != prefConv+moi+",2-C)" || envois[0].texte != "Merci !" || envois[0].etat != linkedingo.ConversationStateAccepted {
		t.Fatalf("envoi : %+v", envois)
	}
	if e := s.evs("sent"); len(e) != 1 || e[0]["ok"] != true || e[0]["tmp_id"] != "tmp1" {
		t.Fatalf("sent : %v", e)
	}
	ms := s.evs("message")
	item := ms[len(ms)-1]["item"].(map[string]any)
	if item["id"] != "2-NOUVEAU" || item["tmp_id"] != "tmp1" || item["from_me"] != true {
		t.Fatalf("message : %v", item)
	}
	if p.convs["2-C"].Pending {
		t.Fatal("la demande reste en attente")
	}
	// Le retour par le direct garde le même tmp_id, sans doublon.
	echo := msgLI("2-NOUVEAU", "2-C", moi, 5000, "Merci !")
	p.messageDirect(p.gen, echo)
	if n := len(p.msgs["2-C"]); n != 1 || p.msgs["2-C"][0].TmpID != "tmp1" {
		t.Fatalf("mémoire : %+v", p.msgs["2-C"])
	}
}

func TestEnvoiReponseEtFichier(t *testing.T) {
	p, f, _ := essai(t)
	p.appliquerConvs([]linkedingo.Conversation{convLI("2-C", false, "BOB")}, moi, false)
	p.mu.Lock()
	p.remember(convertir(msgLI("2-A", "2-C", "BOB", 1000, "Question ?"), "2-C", moi, func(string) string { return "" }))
	p.mu.Unlock()
	chemin := filepath.Join(t.TempDir(), "cv.pdf")
	_ = os.WriteFile(chemin, []byte("%PDF-1.4 essai"), 0o600)
	p.sendMedia(Cmd{Conv: "2-C", Text: "Voici", Path: chemin, ReplyTo: "2-A", TmpID: "t2"})
	up := f.liste("upload")
	if len(up) != 1 || !strings.HasPrefix(up[0].texte, "cv.pdf|application/pdf|"+string(linkedingo.MediaUploadTypeFileAttachment)) {
		t.Fatalf("téléversement : %+v", up)
	}
	e := f.liste("send")
	if len(e) != 1 || len(e[0].rc) != 2 || e[0].rc[0].RepliedMessageContent == nil || e[0].rc[1].File == nil {
		t.Fatalf("envoi : %+v", e)
	}
	r := e[0].rc[0].RepliedMessageContent
	if r.OriginalMessageURN.String() != prefMsg+moi+",2-A)" || r.MessageBody.Text != "Question ?" || !strings.HasSuffix(r.OriginalSenderURN.String(), "fsd_profile:BOB") {
		t.Fatalf("réponse : %+v", r)
	}
	if e[0].etat != "" {
		t.Fatalf("état : %q", e[0].etat)
	}
}

func TestReactionRemplaceLaMienne(t *testing.T) {
	p, f, _ := essai(t)
	p.appliquerConvs([]linkedingo.Conversation{convLI("2-C", false, "BOB")}, moi, false)
	lm := msgLI("2-A", "2-C", "BOB", 1000, "Salut")
	lm.ReactionSummaries = []linkedingo.ReactionSummary{{Emoji: "👍", Count: 2, ViewerReacted: true}}
	p.mu.Lock()
	p.remember(convertir(lm, "2-C", moi, func(string) string { return "" }))
	p.mu.Unlock()
	p.react(Cmd{Conv: "2-C", ID: "2-A", Emoji: "❤️"})
	if un := f.liste("unreact"); len(un) != 1 || un[0].emoji != "👍" || un[0].texte != prefMsg+moi+",2-A)" {
		t.Fatalf("retrait : %+v", un)
	}
	if r := f.liste("react"); len(r) != 1 || r[0].emoji != "❤️" {
		t.Fatalf("ajout : %+v", r)
	}
	m := p.trouver("2-C", "2-A")
	if len(m.Reactions) != 2 || m.Reactions[0].Emoji != "👍" || m.Reactions[0].FromMe || !m.Reactions[1].FromMe {
		t.Fatalf("réactions : %+v", m.Reactions)
	}
	p.react(Cmd{Conv: "2-C", ID: "2-A", Emoji: ""})
	if un := f.liste("unreact"); len(un) != 2 || un[1].emoji != "❤️" {
		t.Fatalf("retrait de la mienne : %+v", un)
	}
	if len(f.liste("react")) != 1 {
		t.Fatal("aucune nouvelle réaction attendue")
	}
}

func TestLectureSeule(t *testing.T) {
	p, f, s := essai(t)
	p.lectureSeule = true
	lc := convLI("2-C", false, "BOB")
	lc.Read = false
	p.appliquerConvs([]linkedingo.Conversation{lc}, moi, false)
	p.mu.Lock()
	p.remember(convertir(msgLI("2-A", "2-C", "BOB", 1000, "Salut"), "2-C", moi, func(string) string { return "" }))
	p.mu.Unlock()
	p.send(Cmd{Conv: "2-C", Text: "non", TmpID: "x"})
	p.react(Cmd{Conv: "2-C", ID: "2-A", Emoji: "👍"})
	p.markRead(Cmd{Conv: "2-C"})
	for _, q := range []string{"send", "react", "unreact", "read", "upload"} {
		if len(f.liste(q)) != 0 {
			t.Fatalf("%s envoyé en lecture seule", q)
		}
	}
	if e := s.evs("sent"); len(e) != 1 || e[0]["ok"] != false {
		t.Fatalf("sent : %v", e)
	}
	if !p.convs["2-C"].Unread {
		t.Fatal("marquée lue en lecture seule")
	}
}

func TestLuSeulementSiNonLu(t *testing.T) {
	p, f, _ := essai(t)
	p.appliquerConvs([]linkedingo.Conversation{convLI("2-L", false, "BOB")}, moi, false)
	nl := convLI("2-N", false, "ANN")
	nl.Read = false
	p.appliquerConvs([]linkedingo.Conversation{nl}, moi, false)
	p.markRead(Cmd{Conv: "2-L"})
	p.markRead(Cmd{Conv: "2-N"})
	p.markRead(Cmd{Conv: "2-N"})
	if r := f.liste("read"); len(r) != 1 || r[0].conv != prefConv+moi+",2-N)" {
		t.Fatalf("lu : %+v", r)
	}
}

func TestHistoriqueNeMarquePasLu(t *testing.T) {
	p, f, s := essai(t)
	lc := convLI("2-C", true, "BOB", "ANN")
	lc.Read = false
	p.appliquerConvs([]linkedingo.Conversation{lc}, moi, false)
	f.pages[prefConv+moi+",2-C)"] = &pageMessage{
		Metadata: linkedingo.MessageMetadata{PrevCursor: "CURSEUR"},
		Elements: []linkedingo.Message{msgLI("2-B", "2-C", "ANN", 2000, "deux"), msgLI("2-A", "2-C", "BOB", 1000, "un")},
	}
	p.messages(Cmd{Conv: "2-C", Count: 50})
	ev := s.evs("messages")
	if len(ev) != 1 || ev[0]["more"] != true {
		t.Fatalf("messages : %v", ev)
	}
	items := ev[0]["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != "2-A" || items[1].(map[string]any)["sender_id"] != "ANN" {
		t.Fatalf("ordre : %v", items)
	}
	p.messages(Cmd{Conv: "2-C", Before: "2-A"})
	if c := f.liste("curseur"); len(c) != 1 || c[0].texte != "CURSEUR" {
		t.Fatalf("page précédente : %+v", c)
	}
	if len(f.liste("read")) != 0 || !p.convs["2-C"].Unread {
		t.Fatal("l'historique a marqué la conversation lue")
	}
	// Les photos des auteurs du groupe (sender_avatar), téléchargées au CDN.
	f.mu.Lock()
	f.fichiers["https://media.licdn.com/dms/image/v2/ANN/photo-shrink_200_200/0/1?e=1&t=b"] = []byte("jpeg")
	f.mu.Unlock()
	p.auteursDe("2-C", []Message{{SenderID: "ANN"}})
	if a := s.evs("sender_avatar"); len(a) == 0 || a[len(a)-1]["id"] != "ANN" {
		t.Fatalf("sender_avatar : %v", a)
	}
}

func TestDirect(t *testing.T) {
	p, _, s := essai(t)
	p.appliquerConvs([]linkedingo.Conversation{convLI("2-C", false, "BOB")}, moi, false)
	mien := msgLI("2-M", "2-C", moi, 1000, "Bonjour Bob")
	p.messageDirect(p.gen, mien)
	recu := msgLI("2-R", "2-C", "BOB", 2000, "Salut !")
	p.evenement(p.gen, &linkedingo.DecoratedEvent{
		Topic:   linkedingo.NewURN("urn:li-realtime:messagesTopic:urn:li-realtime:myself"),
		Payload: linkedingo.DecoratedEventPayload{Data: linkedingo.DecoratedEventData{DecoratedMessage: &linkedingo.DecoratedMessage{Result: recu}}},
	})
	if !p.convs["2-C"].Unread || p.convs["2-C"].Preview != "Salut !" {
		t.Fatalf("conversation : %+v", p.convs["2-C"])
	}
	// Bob a lu mon message.
	p.vu(linkedingo.SeenReceipt{Message: linkedingo.Message{EntityURN: mien.EntityURN}, SeenByParticipant: participant("BOB", "Bob", "B")})
	if m := p.trouver("2-C", "2-M"); m.Status != "OUTGOING_READ" {
		t.Fatalf("lu : %+v", m)
	}
	// Bob réagit.
	p.reactionDirect(linkedingo.RealtimeReactionSummary{ReactionAdded: true, Actor: participant("BOB", "Bob", "B"), Message: linkedingo.Message{EntityURN: mien.EntityURN},
		ReactionSummary: linkedingo.ReactionSummary{Emoji: "👍", Count: 1}})
	if m := p.trouver("2-C", "2-M"); len(m.Reactions) != 1 || m.Reactions[0].Actor != "BOB" || m.Reactions[0].Sender != "Bob B" {
		t.Fatalf("réaction : %+v", m.Reactions)
	}
	p.reactionDirect(linkedingo.RealtimeReactionSummary{ReactionAdded: false, Actor: participant("BOB", "Bob", "B"), Message: linkedingo.Message{EntityURN: mien.EntityURN},
		ReactionSummary: linkedingo.ReactionSummary{Emoji: "👍", Count: 0}})
	if m := p.trouver("2-C", "2-M"); len(m.Reactions) != 0 {
		t.Fatalf("réaction retirée : %+v", m.Reactions)
	}
	// Bob écrit.
	p.ecrit(linkedingo.RealtimeTypingIndicator{TypingParticipant: participant("BOB", "Bob", "B"), Conversation: linkedingo.Conversation{EntityURN: convLI("2-C", false).EntityURN}})
	if e := s.evs("typing"); len(e) != 1 || e[0]["conv"] != "2-C" || e[0]["on"] != true {
		t.Fatalf("typing : %v", e)
	}
	// Lu ailleurs (moi).
	p.vu(linkedingo.SeenReceipt{Message: linkedingo.Message{EntityURN: recu.EntityURN}, SeenByParticipant: participant(moi, "Jo", "Moi")})
	if p.convs["2-C"].Unread {
		t.Fatal("lue ailleurs, toujours non lue")
	}
	// Retiré.
	retireEv := recu
	retireEv.MessageBodyRenderFormat = linkedingo.MessageBodyRenderFormatRecalled
	p.messageDirect(p.gen, retireEv)
	if m := p.trouver("2-C", "2-R"); m.System != "deleted" {
		t.Fatalf("retiré : %+v", m)
	}
}

func TestReactionsAnonymes(t *testing.T) {
	l := reactionsDe([]linkedingo.ReactionSummary{{Emoji: "👍", Count: 3, ViewerReacted: true}, {Emoji: "🎉", Count: 1}})
	if len(l) != 4 || !l[0].FromMe || l[3].Emoji != "🎉" {
		t.Fatalf("résumés : %+v", l)
	}
	// Une personne nommée remplace une anonyme, au même compte.
	l = appliquerReaction(l, "👍", "BOB", "Bob", false, true, 3)
	n := 0
	for _, r := range l {
		if r.Emoji == "👍" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("compte : %+v", l)
	}
	// Une page plus tard garde le nom connu.
	f := fusionReactions(l, reactionsDe([]linkedingo.ReactionSummary{{Emoji: "👍", Count: 3, ViewerReacted: true}}))
	nomme := false
	for _, r := range f {
		nomme = nomme || r.Actor == "BOB"
	}
	if len(f) != 3 || !nomme {
		t.Fatalf("fusion : %+v", f)
	}
}

func TestTelechargementVignetteEtOriginal(t *testing.T) {
	p, f, s := essai(t)
	k := mediaKey{URL: "https://media.licdn.com/orig", Vign: "https://media.licdn.com/vign", Conv: "2-C", Msg: "2-A", Time: 1000}
	f.fichiers["https://media.licdn.com/vign"] = []byte("petite")
	f.fichiers["https://media.licdn.com/orig"] = []byte("grande")
	p.download(Cmd{MediaID: "2-A#0", Key: k.bytes(), Mime: "image/jpeg"})
	e := s.evs("media")
	if len(e) != 1 || e[0]["full"] != false || !strings.HasSuffix(e[0]["path"].(string), "-v.jpg") {
		t.Fatalf("vignette : %v", e)
	}
	p.download(Cmd{MediaID: "2-A#0", Key: k.bytes(), Mime: "image/jpeg", Full: true})
	e = s.evs("media")
	if len(e) != 2 || e[1]["full"] != true {
		t.Fatalf("original : %v", e)
	}
	b, _ := os.ReadFile(e[1]["path"].(string))
	if string(b) != "grande" {
		t.Fatalf("contenu : %q", b)
	}
	if st, _ := os.Stat(e[1]["path"].(string)); st.Mode().Perm() != 0o600 {
		t.Fatalf("droits : %v", st.Mode())
	}
	// Une adresse étrangère est refusée sans requête.
	n := len(f.liste("get"))
	p.download(Cmd{MediaID: "x#0", Key: mediaKey{URL: "https://exemple.com/a"}.bytes()})
	if len(f.liste("get")) != n {
		t.Fatal("requête vers un hôte étranger")
	}
}

func TestTelechargementAdresseExpiree(t *testing.T) {
	p, f, s := essai(t)
	p.appliquerConvs([]linkedingo.Conversation{convLI("2-C", false, "BOB")}, moi, false)
	f.refus["https://www.linkedin.com/dms/prv/vieux"] = 403
	lm := msgLI("2-A", "2-C", "BOB", 1000, "")
	lm.RenderContent = []linkedingo.RenderContent{{File: &linkedingo.FileAttachment{Name: "a.pdf", MediaType: "application/pdf", URL: "https://www.linkedin.com/dms/prv/neuf"}}}
	f.pages[prefConv+moi+",2-C)"] = &pageMessage{Elements: []linkedingo.Message{lm}}
	f.fichiers["https://www.linkedin.com/dms/prv/neuf"] = []byte("%PDF-1.4 essai")
	k := mediaKey{URL: "https://www.linkedin.com/dms/prv/vieux", Conv: "2-C", Msg: "2-A", Time: 1000}
	p.download(Cmd{MediaID: "2-A#0", Key: k.bytes(), Mime: "application/pdf"})
	e := s.evs("media")
	if len(e) != 1 || e[0]["error"] != nil || !strings.HasSuffix(e[0]["path"].(string), ".pdf") {
		t.Fatalf("renouvelée : %v", e)
	}
}

func TestSessionEnPrive(t *testing.T) {
	_, _, _ = essai(t)
	jar, _ := jarDe(map[string]string{"li_at": "secret", "JSESSIONID": "ajax:1"})
	if err := saveSession(&sessionFile{Cookies: jar, Profile: moi}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(sessionPath())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("session.json : %v %v", st, err)
	}
	if st, _ := os.Stat(filepath.Dir(sessionPath())); st.Mode().Perm() != 0o700 {
		t.Fatalf("dossier : %v", st.Mode())
	}
	sf, err := loadSession()
	if err != nil || sf.Cookies.GetCookie("li_at") != "secret" || sf.Profile != moi {
		t.Fatalf("relue : %+v %v", sf, err)
	}
	removeSession()
	if _, err := loadSession(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oubliée : %v", err)
	}
}

func TestCommandesRefusees(t *testing.T) {
	p, f, s := essai(t)
	for _, c := range []string{"archive", "delete", "create_group", "edit", "pair"} {
		p.dispatch(Cmd{Cmd: c, Conv: "2-C"})
	}
	p.dispatch(Cmd{Cmd: "typing", Conv: "2-C"})
	if len(s.evs("error")) != 5 || len(f.liste("")) != 0 {
		t.Fatalf("refus : %v / %v", s.brut(), f.liste(""))
	}
}
