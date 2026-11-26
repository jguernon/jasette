package main

// La session LinkedIn : la connexion (cookies), la liste des conversations
// (jeton de synchronisation, petites pages), le direct (flux « realtime »)
// et l'état en mémoire.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/mautrix-linkedin/pkg/linkedingo"
)

// Les rythmes : l'API (au moins une seconde entre deux requêtes, plus du
// hasard) et les fichiers (photos, pièces jointes).
const (
	gapREST = 1100 * time.Millisecond
	gapCDN  = 300 * time.Millisecond
	// Pages de la liste lues au premier lancement (20 conversations chacune),
	// en plus de la première ; puis une page de plus à la demande, pas plus
	// d'une fois toutes les deux minutes.
	pagesDepart  = 2
	pauseSuite   = 2 * time.Minute
	pauseDelta   = 20 * time.Second
	dlSimultanes = 3
)

// envoi : un message parti de Jasette, pas encore revenu par le direct.
type envoi struct {
	tmp, texte string
	quand      time.Time
}

type Pont struct {
	ctx context.Context

	mu           sync.Mutex
	sf           *sessionFile
	api          liAPI
	gen          int // la session en cours ; les rappels d'une ancienne sont ignorés
	stop         context.CancelFunc
	pret         bool
	coupe        bool // « reconnecting » annoncé
	me           string
	monNom       string
	convs        map[string]*conv
	msgs         map[string][]Message // par conversation, du plus vieux au plus récent
	prev         map[string]string    // conversation → curseur de la page plus ancienne
	avatars      map[string]string    // conversation ou personne → photo déjà annoncée
	envois       map[string][]envoi
	dlEnvol      map[string]bool
	finListe     bool      // plus de conversations plus vieilles
	plusVieux    int64     // la plus vieille activité lue (ms)
	derniereLue  time.Time // dernière page de liste demandée
	dernierDelta time.Time
	deltaPrevu   bool
	sale         bool // convs.json à réécrire
	lectureSeule bool

	rest throttle
	cdn  throttle
	dl   chan struct{}

	// nouveauClient : le client linkedingo d'une session (les essais le remplacent).
	nouveauClient func(ctx context.Context, sf *sessionFile, h linkedingo.Handlers) liAPI
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:          ctx,
		convs:        map[string]*conv{},
		msgs:         map[string][]Message{},
		prev:         map[string]string{},
		avatars:      map[string]string{},
		envois:       map[string][]envoi{},
		dlEnvol:      map[string]bool{},
		lectureSeule: lectureSeule(),
		rest:         throttle{gap: gapREST},
		cdn:          throttle{gap: gapCDN},
		dl:           make(chan struct{}, dlSimultanes),
	}
	p.nouveauClient = func(ctx context.Context, sf *sessionFile, h linkedingo.Handlers) liAPI {
		c := linkedingo.NewClient(ctx, linkedingo.NewURN(sf.Profile), sf.Cookies, sf.PageInstance, sf.XLITrack, sf.SyncToken, h)
		if hd := enTetesNavigateur(sf.UserAgent); hd != nil {
			c.SetBrowserHeaders(hd)
		}
		return c
	}
	return p
}

// ---- Démarrage et connexion ----

func (p *Pont) start() {
	sf, err := loadSession()
	if err != nil {
		emitState("unpaired")
		return
	}
	p.mu.Lock()
	p.sf = sf
	p.me, p.monNom = sf.Profile, sf.Nom
	p.chargerConvs()
	p.mu.Unlock()
	emitState("connecting")
	p.emitAccount()
	p.connect(false)
}

// login : les cookies de la fenêtre de connexion (linkedin.com).
func (p *Pont) login(raw map[string]string) {
	cookies, ua := cookiesValides(raw)
	if cookies["li_at"] == "" || cookies["JSESSIONID"] == "" {
		emitErr("login", errors.New("il manque les cookies li_at et JSESSIONID : connectez-vous sur linkedin.com dans la fenêtre, puis réessayez"))
		emitState("unpaired")
		return
	}
	jar, err := jarDe(cookies)
	if err != nil {
		emitErr("login", fmt.Errorf("cookies de LinkedIn illisibles : %w", err))
		emitState("unpaired")
		return
	}
	p.arreter()
	emitState("pairing")
	sf := &sessionFile{Cookies: jar, UserAgent: ua, PageInstance: pageInstanceNeuve()}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	essai := &rythme{api: p.nouveauClient(p.ctx, sf, linkedingo.Handlers{}), rest: &p.rest, cdn: &p.cdn}
	prof, err := essai.GetCurrentUserProfile(ctx)
	if err == nil && prof.MiniProfile.EntityURN.ID() == "" {
		err = errors.New("profil vide")
	}
	if err != nil {
		log.Warn().Str("err", erreurCourte(err)).Msg("connexion : profil illisible")
		if estErrAuth(err) {
			err = errors.New("LinkedIn refuse ces cookies : reconnectez-vous dans la fenêtre")
		} else {
			err = fmt.Errorf("LinkedIn : %s", erreurCourte(err))
		}
		emitErr("login", err)
		emitState("unpaired")
		return
	}
	mp := prof.MiniProfile
	sf.Profile = mp.EntityURN.ID()
	sf.Nom = strings.TrimSpace(mp.FirstName + " " + mp.LastName)
	if mp.Picture.VectorImage != nil {
		sf.Photo, _, _ = adresseImage(mp.Picture.VectorImage, tailleAvat)
	}
	p.mu.Lock()
	if p.sf == nil || p.sf.Profile != sf.Profile {
		// Un autre compte : on oublie tout de l'ancien.
		p.convs = map[string]*conv{}
		p.msgs = map[string][]Message{}
		p.prev = map[string]string{}
		p.avatars = map[string]string{}
		p.finListe, p.plusVieux = false, 0
		_ = os.Remove(convsPath())
	} else {
		sf.SyncToken = p.sf.SyncToken
	}
	p.sf = sf
	p.me, p.monNom = sf.Profile, sf.Nom
	p.mu.Unlock()
	if err := saveSession(sf); err != nil {
		log.Warn().Err(err).Msg("session.json")
	}
	log.Info().Msg("connexion : compte LinkedIn reconnu")
	emit("paired", nil)
	p.emitAccount()
	p.connect(true)
}

// connect : le client linkedingo de la session, puis la première synchronisation.
func (p *Pont) connect(fromLogin bool) {
	p.mu.Lock()
	sf := p.sf
	if sf == nil {
		p.mu.Unlock()
		emitState("unpaired")
		return
	}
	if sf.PageInstance == "" {
		sf.PageInstance = pageInstanceNeuve()
	}
	p.gen++
	gen := p.gen
	ctx, annule := context.WithCancel(p.ctx)
	p.stop = annule
	p.mu.Unlock()
	cli := p.nouveauClient(ctx, sf, p.handlers(gen))
	p.mu.Lock()
	p.api = &rythme{api: cli, rest: &p.rest, cdn: &p.cdn}
	p.mu.Unlock()
	go p.premiereSync(ctx, gen, fromLogin)
	go p.boucleSauvegarde(ctx, gen)
}

// courant : cette génération de session est-elle encore la bonne ?
func (p *Pont) courant(gen int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen == gen
}

func (p *Pont) handlers(gen int) linkedingo.Handlers {
	return linkedingo.Handlers{
		ClientConnection: func(ctx context.Context, _ *linkedingo.ClientConnection) {
			if !p.courant(gen) {
				return
			}
			p.mu.Lock()
			etaitCoupe := p.coupe
			revient := etaitCoupe && p.pret
			p.coupe = false
			p.mu.Unlock()
			if revient {
				emitState("ready")
			}
			// Après une coupure : ce qui a changé entre-temps (une requête).
			// À la première connexion, la synchronisation vient d'être faite.
			if etaitCoupe {
				p.planDelta(gen)
			}
		},
		TransientDisconnect: func(ctx context.Context, err error) {
			if !p.courant(gen) {
				return
			}
			log.Info().Str("raison", erreurCourte(err)).Msg("direct : coupé, reprise")
			p.mu.Lock()
			annonce := p.pret && !p.coupe
			p.coupe = true
			p.mu.Unlock()
			if annonce {
				emitState("reconnecting")
			}
		},
		BadCredentials: func(ctx context.Context, err error) {
			if !p.courant(gen) {
				return
			}
			// Pas d'attente ici : c'est la boucle du direct qui appelle.
			go p.sessionPerdue(err)
		},
		DecoratedEvent: func(ctx context.Context, de *linkedingo.DecoratedEvent) {
			if p.courant(gen) && de != nil {
				p.evenement(gen, de)
			}
		},
		ConversationsSyncToken: func(ctx context.Context, tok string) {
			p.mu.Lock()
			if p.gen == gen && p.sf != nil && p.sf.SyncToken != tok {
				p.sf.SyncToken = tok
				sf := *p.sf
				p.mu.Unlock()
				_ = saveSession(&sf)
				return
			}
			p.mu.Unlock()
		},
	}
}

// arreter : la session en cours s'arrête (direct compris).
func (p *Pont) arreter() {
	p.mu.Lock()
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	api := p.api
	p.api = nil
	p.gen++
	p.pret, p.coupe = false, false
	p.mu.Unlock()
	if api != nil {
		api.RealtimeDisconnect()
	}
}

// estErrAuth : LinkedIn refuse la session (cookies effacés ou rejetés).
func estErrAuth(err error) bool {
	if errors.Is(err, linkedingo.ErrTokenInvalidated) {
		return true
	}
	var re *linkedingo.ResponseError
	return errors.As(err, &re) && re.StatusCode == http.StatusUnauthorized
}

// sessionPerdue : LinkedIn a fermé la session → unpaired. Les cookies
// effacés par LinkedIn sont oubliés ; un simple refus garde la session
// (le prochain lancement réessaie une fois).
func (p *Pont) sessionPerdue(err error) {
	log.Warn().Str("err", erreurCourte(err)).Msg("session LinkedIn perdue")
	emitErr("login", errors.New("LinkedIn a fermé la session : reconnectez-vous dans la fenêtre de linkedin.com"))
	p.arreter()
	if errors.Is(err, linkedingo.ErrTokenInvalidated) {
		p.forget()
	}
	emitState("unpaired")
}

// erreurCourte : une erreur sans adresse (les adresses de fichiers sont signées).
func erreurCourte(err error) string {
	if err == nil {
		return ""
	}
	var re *linkedingo.ResponseError
	if errors.As(err, &re) {
		return re.Error()
	}
	s := err.Error()
	if i := strings.Index(s, "https://"); i >= 0 {
		s = s[:i] + "…"
	}
	return shorten(s, 200)
}

// ---- Compte ----

func (p *Pont) forget() {
	removeSession()
	p.mu.Lock()
	p.sf, p.api, p.me, p.monNom = nil, nil, "", ""
	p.convs = map[string]*conv{}
	p.msgs = map[string][]Message{}
	p.prev = map[string]string{}
	p.avatars = map[string]string{}
	p.envois = map[string][]envoi{}
	p.finListe, p.plusVieux, p.sale = false, 0, false
	p.mu.Unlock()
}

// unpair : on oublie la session sur cette machine. Pas de /uas/logout : ce
// serait fermer aussi la session web de l'utilisateur.
func (p *Pont) unpair() {
	p.arreter()
	p.forget()
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.mu.Lock()
	if p.sale && p.sf != nil {
		p.sauverConvs()
	}
	sf := p.sf
	p.mu.Unlock()
	if sf != nil {
		_ = saveSession(sf) // les cookies que LinkedIn a renouvelés
	}
	p.arreter()
}

func (p *Pont) emitAccount() {
	p.mu.Lock()
	me, nom := p.me, p.monNom
	p.mu.Unlock()
	if me == "" {
		return
	}
	emit("account", map[string]any{"service": serviceName(), "name": firstOf(nom, "LinkedIn"), "id": me, "email": ""})
}

// client : l'accès à LinkedIn, ou une erreur lisible.
func (p *Pont) client() (liAPI, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.api == nil || p.me == "" {
		return nil, "", errors.New("LinkedIn n'est pas connecté")
	}
	return p.api, p.me, nil
}

// ---- convs.json ----

type fichierConvs struct {
	Convs     map[string]*conv `json:"convs"`
	Fin       bool             `json:"fin,omitempty"`
	PlusVieux int64            `json:"plus_vieux,omitempty"`
}

func convsPath() string { return filepath.Join(liDir(), "convs.json") }

// chargerConvs : à appeler avec p.mu.
func (p *Pont) chargerConvs() {
	b, err := os.ReadFile(convsPath())
	if err != nil {
		return
	}
	var f fichierConvs
	if json.Unmarshal(b, &f) != nil || f.Convs == nil {
		return
	}
	p.convs = f.Convs
	p.finListe, p.plusVieux = f.Fin, f.PlusVieux
}

// sauverConvs : à appeler avec p.mu.
func (p *Pont) sauverConvs() {
	b, err := json.Marshal(fichierConvs{Convs: p.convs, Fin: p.finListe, PlusVieux: p.plusVieux})
	if err == nil && writeWhole(convsPath(), b) == nil {
		p.sale = false
	}
}

// boucleSauvegarde : convs.json et les cookies renouvelés, de temps en temps.
func (p *Pont) boucleSauvegarde(ctx context.Context, gen int) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		n++
		p.mu.Lock()
		if p.gen != gen || p.sf == nil {
			p.mu.Unlock()
			return
		}
		if p.sale {
			p.sauverConvs()
		}
		sf := *p.sf
		p.mu.Unlock()
		if n%20 == 0 { // toutes les dix minutes
			_ = saveSession(&sf)
		}
	}
}

// ---- La liste des conversations ----

// premiereSync : ce qui a changé depuis la dernière fois (jeton de
// synchronisation), quelques pages au premier lancement, puis le direct.
func (p *Pont) premiereSync(ctx context.Context, gen int, fromLogin bool) {
	p.mu.Lock()
	connues := len(p.convs)
	p.mu.Unlock()
	if connues > 0 {
		// Ce qu'on sait déjà, tout de suite.
		p.listConversations(false, 0)
	}
	err := p.delta(ctx, gen)
	if err != nil {
		if estErrAuth(err) {
			p.sessionPerdue(err)
			return
		}
		if ctx.Err() != nil {
			return
		}
		log.Warn().Str("err", erreurCourte(err)).Msg("liste : synchronisation ratée (le direct réessaiera)")
	}
	p.mu.Lock()
	peu := len(p.convs) < 15 && !p.finListe
	p.mu.Unlock()
	if err == nil && (fromLogin || peu) {
		for i := 0; i < pagesDepart && ctx.Err() == nil; i++ {
			if !p.pageSuivante(ctx, gen) {
				break
			}
		}
	}
	if !p.courant(gen) {
		return
	}
	p.mu.Lock()
	p.pret = true
	if p.sale {
		p.sauverConvs()
	}
	p.mu.Unlock()
	emitState("ready")
	p.listConversations(false, 0)
	go p.photoMoi()
	cli, _, errc := p.client()
	if errc == nil {
		if err := cli.RealtimeConnect(ctx); err != nil {
			log.Warn().Str("err", erreurCourte(err)).Msg("direct : connexion ratée")
		}
	}
}

// delta : GetConversationsBySyncToken (les conversations changées, ou la
// première page sans jeton).
func (p *Pont) delta(ctx context.Context, gen int) error {
	cli, me, err := p.client()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.dernierDelta = time.Now()
	p.mu.Unlock()
	r, err := cli.GetConversationsBySyncToken(ctx)
	if err != nil {
		return err
	}
	if !p.courant(gen) || r == nil {
		return nil
	}
	p.appliquerConvs(r.Elements, me, false)
	for _, d := range r.Metadata.DeletedURNs {
		if id := court(d.Conversation.EntityURN, me); id != "" {
			p.supprimee(id)
		}
	}
	return nil
}

// planDelta : un delta bientôt (plusieurs événements → une seule requête).
func (p *Pont) planDelta(gen int) {
	p.mu.Lock()
	if p.deltaPrevu || p.gen != gen {
		p.mu.Unlock()
		return
	}
	p.deltaPrevu = true
	attente := pauseDelta - time.Since(p.dernierDelta)
	attente = max(attente, 3*time.Second)
	ctx := p.ctx
	p.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(attente):
		}
		p.mu.Lock()
		p.deltaPrevu = false
		p.mu.Unlock()
		if !p.courant(gen) {
			return
		}
		cctx, annule := context.WithTimeout(ctx, time.Minute)
		defer annule()
		if err := p.delta(cctx, gen); err != nil {
			if estErrAuth(err) {
				p.sessionPerdue(err)
				return
			}
			log.Info().Str("err", erreurCourte(err)).Msg("liste : mise à jour ratée")
		}
	}()
}

// pageSuivante : une page de 20 conversations plus vieilles. Faux : plus rien.
func (p *Pont) pageSuivante(ctx context.Context, gen int) bool {
	cli, me, err := p.client()
	if err != nil {
		return false
	}
	p.mu.Lock()
	avant := p.plusVieux
	if avant == 0 {
		avant = time.Now().UnixMilli()
	}
	p.derniereLue = time.Now()
	p.mu.Unlock()
	r, err := cli.GetConversationsUpdatedBefore(ctx, time.UnixMilli(avant))
	if err != nil {
		if estErrAuth(err) {
			go p.sessionPerdue(err)
		} else {
			log.Info().Str("err", erreurCourte(err)).Msg("liste : page ratée")
		}
		return false
	}
	if !p.courant(gen) || r == nil {
		return false
	}
	p.appliquerConvs(r.Elements, me, true)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(r.Elements) == 0 || p.plusVieux >= avant {
		p.finListe = true
		p.sale = true
		return false
	}
	return true
}

// suiteDemandee : la liste demandée est plus longue que ce qu'on a : une
// page de plus, au plus toutes les deux minutes.
func (p *Pont) suiteDemandee(voulu int) {
	p.mu.Lock()
	n := 0
	for _, c := range p.convs {
		if c.visible() && !c.Archivee {
			n++
		}
	}
	gen := p.gen
	ok := p.pret && !p.finListe && voulu > n && time.Since(p.derniereLue) > pauseSuite
	if ok {
		p.derniereLue = time.Now()
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	p.pageSuivante(ctx, gen)
	if p.courant(gen) {
		p.listConversations(false, voulu)
	}
}

// appliquerConvs : des conversations de LinkedIn. page : elles viennent
// d'une page de la liste (la plus vieille compte pour la suivante).
func (p *Pont) appliquerConvs(l []linkedingo.Conversation, me string, page bool) {
	var aEmettre []string
	p.mu.Lock()
	for _, lc := range l {
		id := court(lc.EntityURN, me)
		if id == "" {
			continue
		}
		c := p.convs[id]
		neuve := c == nil
		if neuve {
			c = &conv{ID: id}
			p.convs[id] = c
		}
		avant := c.out()
		// Ma photo, si je suis dans la liste des participants.
		for _, part := range lc.ConversationParticipants {
			if part.EntityURN.ID() == me && p.sf != nil {
				if ph := personneDe(part).Photo; ph != "" && cleImage(ph) != cleImage(p.sf.Photo) {
					p.sf.Photo = ph
				}
			}
		}
		if dernier := appliquerConv(c, lc, me); dernier != nil {
			m := convertir(*dernier, id, me, p.nomDe(c))
			p.remember(m)
			p.noteLast(c, m)
		}
		if page && lc.LastActivityAt.UnixMilli() > 0 && (p.plusVieux == 0 || lc.LastActivityAt.UnixMilli() < p.plusVieux) {
			p.plusVieux = lc.LastActivityAt.UnixMilli()
		}
		p.sale = true
		if p.pret && c.visible() && (neuve || !memeConv(avant, c.out())) {
			aEmettre = append(aEmettre, id)
		}
	}
	p.mu.Unlock()
	for _, id := range aEmettre {
		p.emitConv(id)
	}
}

func memeConv(a, b Conversation) bool {
	return a.Name == b.Name && a.Preview == b.Preview && a.Time == b.Time && a.Unread == b.Unread &&
		a.FromMe == b.FromMe && a.Request == b.Request && a.Archived == b.Archived && strings.Join(a.Members, "\x00") == strings.Join(b.Members, "\x00")
}

// supprimee : la conversation n'existe plus chez LinkedIn.
func (p *Pont) supprimee(id string) {
	p.mu.Lock()
	_, connue := p.convs[id]
	delete(p.convs, id)
	delete(p.msgs, id)
	delete(p.prev, id)
	p.sale = true
	p.mu.Unlock()
	if connue {
		emit("deleted", map[string]any{"conv": id})
	}
}

// nomDe : le nom d'une personne d'une conversation. À appeler avec p.mu.
func (p *Pont) nomDe(c *conv) func(string) string {
	return func(id string) string {
		if c != nil {
			if m := c.membre(id); m != nil {
				return m.Nom
			}
		}
		if id == p.me {
			return p.monNom
		}
		return ""
	}
}

func (p *Pont) listConversations(archive bool, count int) {
	voulu := count
	if count <= 0 {
		count = 100
	}
	count = min(count, 300)
	p.mu.Lock()
	var l []*conv
	for _, c := range p.convs {
		if c.visible() && c.Archivee == archive && (c.Time > 0 || c.LastID != "") {
			l = append(l, c)
		}
	}
	sort.Slice(l, func(i, j int) bool {
		if l[i].Time != l[j].Time {
			return l[i].Time > l[j].Time
		}
		return l[i].ID < l[j].ID
	})
	if len(l) > count {
		l = l[:count]
	}
	items := make([]Conversation, 0, len(l))
	for _, c := range l {
		items = append(items, c.out())
	}
	p.mu.Unlock()
	ev := map[string]any{"items": items}
	if archive {
		ev["folder"] = "archive"
	}
	emit("conversations", ev)
	if !archive {
		go p.fetchAvatars(items)
		// Une page de plus seulement si l'interface en demande plus
		// (défilement), jamais d'elle-même.
		if voulu > 0 {
			go p.suiteDemandee(min(voulu, 300))
		}
	}
}

func (p *Pont) emitConv(id string) {
	p.mu.Lock()
	c := p.convs[id]
	if c == nil || !c.visible() {
		p.mu.Unlock()
		return
	}
	item := c.out()
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item})
}

// ---- Les messages en mémoire ----

// remember garde (ou remplace) un message, rangé par heure. À appeler avec p.mu.
func (p *Pont) remember(m Message) {
	l := p.msgs[m.Conv]
	for i := range l {
		if l[i].ID == m.ID {
			old := l[i]
			if m.TmpID == "" {
				m.TmpID = old.TmpID
			}
			if len(m.ReadBy) == 0 {
				m.ReadBy = old.ReadBy
			}
			if m.FromMe && old.Status == "OUTGOING_READ" {
				m.Status = old.Status
			}
			if m.Kind != "admin" {
				m.Reactions = fusionReactions(old.Reactions, m.Reactions)
			}
			l[i] = m
			p.msgs[m.Conv] = l
			return
		}
	}
	i := sort.Search(len(l), func(i int) bool { return l[i].Time > m.Time })
	l = append(l, Message{})
	copy(l[i+1:], l[i:])
	l[i] = m
	if len(l) > maxEnMem {
		l = l[len(l)-maxEnMem:]
	}
	p.msgs[m.Conv] = l
}

// fusionReactions : les réactions d'une page (anonymes, sauf les miennes)
// gardent les noms déjà connus par le direct, au même compte par émoji.
func fusionReactions(vieilles, neuves []Reaction) []Reaction {
	if len(vieilles) == 0 {
		return neuves
	}
	compte := map[string]int{}
	for _, r := range neuves {
		if !r.FromMe {
			compte[r.Emoji]++
		}
	}
	var l []Reaction
	for _, r := range neuves {
		if r.FromMe {
			l = append(l, r)
		}
	}
	for _, r := range vieilles {
		if !r.FromMe && r.Actor != "" && compte[r.Emoji] > 0 {
			l = append(l, r)
			compte[r.Emoji]--
		}
	}
	for e, n := range compte {
		for range n {
			l = append(l, Reaction{Emoji: e})
		}
	}
	return l
}

// trouver : un message en mémoire (conversation inconnue : toutes). À appeler avec p.mu.
func (p *Pont) trouver(conv, id string) *Message {
	if conv != "" {
		l := p.msgs[conv]
		for i := len(l) - 1; i >= 0; i-- {
			if l[i].ID == id {
				return &l[i]
			}
		}
		return nil
	}
	for c := range p.msgs {
		if m := p.trouver(c, id); m != nil {
			return m
		}
	}
	return nil
}

// noteLast : le dernier message d'une conversation, pour son aperçu. À appeler avec p.mu.
func (p *Pont) noteLast(c *conv, m Message) bool {
	if m.Time < c.Time && c.LastID != "" && c.LastID != m.ID {
		return false
	}
	pre := apercu(m)
	changed := c.LastID != m.ID || c.Preview != pre || c.LastFromMe != m.FromMe || c.Time != max(c.Time, m.Time)
	c.LastID, c.Preview, c.LastFromMe = m.ID, pre, m.FromMe
	c.Time = max(c.Time, m.Time)
	if changed {
		p.sale = true
	}
	return changed
}
