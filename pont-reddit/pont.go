package main

// La session Reddit : la connexion (jeton de chat), la boucle /sync, et
// l'état des salons et des messages en mémoire.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

type reactionMem struct {
	conv, target, key, sender string
}

type Pont struct {
	ctx context.Context

	mu            sync.Mutex
	sf            *sessionFile
	cli           *mautrix.Client
	gen           int // la session en cours ; une boucle /sync d'une ancienne s'arrête
	ready         bool
	stop          context.CancelFunc
	me            string // @t2_xxx:reddit.com
	monNom        string // le nom d'utilisateur Reddit
	salons        map[string]*salon
	msgs          map[string][]Message // par conversation, du plus vieux au plus récent
	older         map[string]string    // conversation → jeton de la page plus ancienne
	reacts        map[string]reactionMem
	noms          map[string]string // id Matrix → nom d'utilisateur (com.reddit.profile)
	icones        map[string]string // id Matrix → adresse de sa photo
	typing        map[string]map[string]bool
	avatars       map[string]string // conversation ou personne → adresse déjà annoncée
	inconnus      map[string]bool   // types de salon inconnus déjà notés au journal
	photos        *annuaire         // photos des personnes et icônes des subreddits (photos.go)
	auteursVoulus map[string]bool   // auteurs dont on attend la photo (sender_avatar)
	sale          bool              // salons.json à réécrire

	refreshMu sync.Mutex // un seul renouvellement du jeton à la fois

	rest throttle // matrix.redditspace.com et www.reddit.com
	cdn  throttle // images (i.redd.it…)

	httpCli *http.Client // /sync et REST, rythmé
	webCli  *http.Client // www.reddit.com (jeton), rythmé aussi
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:           ctx,
		salons:        map[string]*salon{},
		msgs:          map[string][]Message{},
		older:         map[string]string{},
		reacts:        map[string]reactionMem{},
		noms:          map[string]string{},
		icones:        map[string]string{},
		typing:        map[string]map[string]bool{},
		avatars:       map[string]string{},
		inconnus:      map[string]bool{},
		photos:        nouvelAnnuaire(),
		auteursVoulus: map[string]bool{},
		rest:          throttle{gap: 350 * time.Millisecond},
		cdn:           throttle{gap: 200 * time.Millisecond},
	}
	p.httpCli = newHTTPClient(&p.rest, 75*time.Second)
	p.webCli = newHTTPClient(&p.rest, 45*time.Second)
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
	p.me = sf.UserID
	p.salons = loadSalons()
	p.mu.Unlock()
	emitState("connecting")
	p.connect(false)
}

// login : les cookies de la fenêtre de connexion (reddit.com).
func (p *Pont) login(raw map[string]string) {
	cookies := cleanCookies(raw)
	if cookies["reddit_session"] == "" && cookies["token_v2"] == "" {
		emitErr("login", errors.New("il manque le cookie reddit_session : connectez-vous sur reddit.com dans la fenêtre, puis réessayez"))
		emitState("unpaired")
		return
	}
	p.arreter()
	emitState("pairing")
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	tok, err := fetchToken(ctx, p.webCli, cookies)
	if err != nil {
		log.Warn().Str("err", err.Error()).Msg("connexion : pas de jeton de chat")
		emitErr("login", fmt.Errorf("Reddit : %v", err))
		emitState("unpaired")
		return
	}
	sf := &sessionFile{Cookies: cookies, Token: tok.Token, Expires: tok.Expires, UserID: matrixUserID(tok.Info.Account)}
	p.mu.Lock()
	if p.sf == nil || p.sf.UserID != sf.UserID {
		// Un autre compte : on oublie tout de l'ancien.
		p.salons = map[string]*salon{}
		p.msgs = map[string][]Message{}
		p.reacts = map[string]reactionMem{}
		p.avatars = map[string]string{}
	}
	p.sf = sf
	p.me = sf.UserID
	p.mu.Unlock()
	if err := saveSession(sf); err != nil {
		log.Warn().Err(err).Msg("session.json")
	}
	log.Info().Str("compte", tok.Info.Account).Time("echeance", tok.Expires).Msg("connexion : jeton de chat obtenu")
	emit("paired", nil)
	p.connect(true)
}

// newClient : le client Matrix (mautrix-go) avec le jeton en Bearer.
func (p *Pont) newClient(sf *sessionFile) (*mautrix.Client, error) {
	cli, err := mautrix.NewClient(matrixBase, id.UserID(sf.UserID), sf.Token)
	if err != nil {
		return nil, err
	}
	cli.Client = p.httpCli
	cli.UserAgent = browserUA
	cli.DefaultHTTPRetries = 1
	// Les journaux de mautrix : avertissements seulement (jamais de contenu).
	cli.Log = log.With().Str("matrix", "client").Logger().Level(zerolog.WarnLevel)
	return cli, nil
}

// connect : client Matrix, compte, puis la boucle /sync.
func (p *Pont) connect(fromLogin bool) {
	p.mu.Lock()
	sf := p.sf
	p.mu.Unlock()
	if sf == nil {
		emitState("unpaired")
		return
	}
	if aRenouveler(sf.Expires, time.Now()) {
		if err := p.refreshToken(p.ctx, sf.Token); err != nil {
			if p.sessionPerdue(err) {
				return
			}
			log.Warn().Str("err", err.Error()).Msg("jeton : renouvellement raté, essai avec l'ancien")
		}
		p.mu.Lock()
		sf = p.sf
		p.mu.Unlock()
		if sf == nil {
			return
		}
	}
	cli, err := p.newClient(sf)
	if err != nil {
		emitErr("login", err)
		emitState("unpaired")
		return
	}
	ctx, annule := context.WithCancel(p.ctx)
	p.mu.Lock()
	p.gen++
	gen := p.gen
	p.cli = cli
	p.stop = annule
	p.mu.Unlock()
	go p.syncLoop(ctx, gen, fromLogin)
	go p.boucleJeton(ctx, gen)
	go p.bouclePhotos(ctx)
}

// arreter : la boucle /sync en cours s'arrête.
func (p *Pont) arreter() {
	p.mu.Lock()
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	p.gen++
	p.ready = false
	p.mu.Unlock()
}

// ---- Le jeton ----

// refreshToken : un jeton neuf (un seul renouvellement à la fois). « vu » :
// le jeton que l'appelant avait ; si un autre l'a déjà remplacé, rien à faire.
func (p *Pont) refreshToken(ctx context.Context, vu string) error {
	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	p.mu.Lock()
	sf := p.sf
	p.mu.Unlock()
	if sf == nil {
		return errSession
	}
	if sf.Token != vu && !aRenouveler(sf.Expires, time.Now()) {
		return nil
	}
	cookies := map[string]string{}
	for k, v := range sf.Cookies {
		cookies[k] = v
	}
	cctx, annule := context.WithTimeout(ctx, time.Minute)
	defer annule()
	tok, err := fetchToken(cctx, p.webCli, cookies)
	if err != nil {
		return err
	}
	if sf.UserID != "" && matrixUserID(tok.Info.Account) != sf.UserID {
		return fmt.Errorf("%w (le jeton neuf est celui d'un autre compte)", errSession)
	}
	neuf := *sf
	neuf.Cookies, neuf.Token, neuf.Expires = cookies, tok.Token, tok.Expires
	p.mu.Lock()
	p.sf = &neuf
	if p.cli != nil {
		p.cli.AccessToken = tok.Token
	}
	p.mu.Unlock()
	_ = saveSession(&neuf)
	log.Info().Time("echeance", tok.Expires).Msg("jeton de chat renouvelé")
	return nil
}

// boucleJeton : renouvelle le jeton une heure avant son échéance.
func (p *Pont) boucleJeton(ctx context.Context, gen int) {
	for {
		p.mu.Lock()
		sf := p.sf
		cur := p.gen == gen
		p.mu.Unlock()
		if sf == nil || !cur {
			return
		}
		attente := time.Until(sf.Expires.Add(-refreshAvant))
		if attente < time.Minute {
			attente = time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(attente):
		}
		if err := p.refreshToken(ctx, sf.Token); err != nil {
			if p.sessionPerdue(err) {
				return
			}
			log.Warn().Str("err", err.Error()).Msg("jeton : renouvellement raté, nouvel essai plus tard")
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Minute):
			}
		}
	}
}

// sessionPerdue : les cookies ne valent plus rien → unpaired. Rend vrai si
// c'est le cas (l'appelant s'arrête).
func (p *Pont) sessionPerdue(err error) bool {
	if !errors.Is(err, errSession) {
		return false
	}
	log.Warn().Str("err", err.Error()).Msg("session Reddit perdue")
	emitErr("login", err)
	p.arreter()
	p.forget()
	emitState("unpaired")
	return true
}

// estErrJeton : Matrix refuse le jeton (expiré ou révoqué).
func estErrJeton(err error) bool {
	if err == nil {
		return false
	}
	var he mautrix.HTTPError
	if errors.As(err, &he) {
		if he.Response != nil && he.Response.StatusCode == http.StatusUnauthorized {
			return true
		}
		if he.RespError != nil && strings.Contains(he.RespError.ErrCode, "TOKEN") {
			return true
		}
	}
	return errors.Is(err, mautrix.MUnknownToken) || errors.Is(err, mautrix.MMissingToken)
}

func estLimite(err error) bool {
	var he mautrix.HTTPError
	if errors.As(err, &he) && he.Response != nil && he.Response.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return errors.Is(err, mautrix.MLimitExceeded)
}

// ---- Compte ----

func (p *Pont) forget() {
	removeSession()
	p.mu.Lock()
	p.sf, p.cli, p.me, p.monNom = nil, nil, "", ""
	p.salons = map[string]*salon{}
	p.msgs = map[string][]Message{}
	p.older = map[string]string{}
	p.reacts = map[string]reactionMem{}
	p.noms = map[string]string{}
	p.icones = map[string]string{}
	p.avatars = map[string]string{}
	p.auteursVoulus = map[string]bool{}
	p.mu.Unlock()
	p.photos.oublier()
}

// unpair : on oublie la session. Pas de /logout : le jeton de chat est celui
// de la session reddit.com de l'utilisateur, qu'on ne veut pas fermer.
func (p *Pont) unpair() {
	p.arreter()
	p.forget()
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.mu.Lock()
	sale := p.sale
	salons := p.salons
	p.mu.Unlock()
	if sale && p.sf != nil {
		saveSalons(salons)
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
	emit("account", map[string]any{"service": serviceName(), "name": firstOf(nom, nomCompte(me)), "id": me, "email": ""})
}

// api : le client prêt, ou une erreur lisible.
func (p *Pont) api() (*mautrix.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cli == nil || p.me == "" {
		return nil, errors.New("Reddit n'est pas connecté")
	}
	return p.cli, nil
}

func (p *Pont) isReady() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

// ---- Les conversations ----

func (p *Pont) listConversations(archive bool) {
	if archive {
		emit("conversations", map[string]any{"items": []Conversation{}, "folder": "archive"})
		return
	}
	p.mu.Lock()
	list := trierConvs(p.salons, p.me, p.noms)
	p.mu.Unlock()
	emit("conversations", map[string]any{"items": list})
	go p.fetchAvatars(list)
}

func (p *Pont) emitConv(conv string) {
	p.mu.Lock()
	s := p.salons[conv]
	if s == nil || s.Hidden || s.Spam {
		p.mu.Unlock()
		return
	}
	item := s.convOut(p.me, p.noms)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item})
}

// ---- Les messages en mémoire ----

// remember garde (ou remplace) un message, rangé par heure. À appeler avec p.mu.
func (p *Pont) remember(m Message) {
	l := p.msgs[m.Conv]
	for i := range l {
		if l[i].ID == m.ID {
			if m.Reactions == nil {
				m.Reactions = l[i].Reactions
			}
			if m.TmpID == "" {
				m.TmpID = l[i].TmpID
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

func (p *Pont) recalled(conv, id string) *Message { // à appeler avec p.mu
	l := p.msgs[conv]
	for i := len(l) - 1; i >= 0; i-- {
		if l[i].ID == id {
			return &l[i]
		}
	}
	return nil
}

// noteLast : le dernier message d'un salon, pour son aperçu. À appeler avec p.mu.
func (p *Pont) noteLast(s *salon, m Message) bool {
	if m.Time < s.LastTS {
		return false
	}
	pre := apercu(m)
	changed := s.LastID != m.ID || s.Preview != pre || s.LastFromMe != m.FromMe
	s.LastID, s.LastTS, s.Preview, s.LastFromMe = m.ID, m.Time, pre, m.FromMe
	if m.FromMe {
		s.ReadTS = m.Time
		s.Unread = 0
	}
	if changed {
		p.sale = true
	}
	return changed
}

// jitter : un délai au hasard autour de d (±20 %).
func jitter(d time.Duration) time.Duration {
	return d - d/5 + time.Duration(rand.Int64N(int64(d/5)*2+1))
}
