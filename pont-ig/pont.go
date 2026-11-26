package main

// La connexion à Instagram (instameow) : connexion avec les cookies,
// chargement de la boîte de réception, socket « iris », et toutes les
// commandes de l'interface.

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/util/exhttp"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/util/ptr"

	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/dgw"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/mautrix-meta/pkg/msgconv/mediadl"
)

type Pont struct {
	ctx context.Context
	db  *sql.DB

	mu       sync.Mutex
	sess     *Session
	live     *instameow.Client // le client dont on écoute les événements
	cli      *instameow.Client // le client prêt pour les commandes
	me       int64
	myAvatar string

	convs      map[string]*convState
	contacts   map[int64]*contact
	byIGID     map[string]string // id court du fil -> fil
	byThreadID map[string]string // id long du fil -> fil
	byUserIGID map[string]int64  // id Instagram d'une personne -> FBID

	pending  map[string]string // otid -> tmp_id de l'interface
	loaded   map[string]int64  // fil -> plus vieux message montré à l'interface
	fetching map[string]bool

	auteurs     map[int64]bool // photos d'auteurs déjà demandées (auteurs.go)
	fileAuteurs chan int64

	listDirty     bool
	listCount     int
	archiveWanted bool
	ready         bool
	connecting    bool
	loggingIn     bool
	gotMailbox    bool // la boîte a été chargée pour cette connexion
	backfilling   bool
	lastFull      time.Time
	connCancel    context.CancelFunc

	// La présence des contacts (presence.go).
	presenceOn bool
	presence   map[string]presenceState
	flux       *fluxPresence

	// « Quoi de neuf ? » (notes.go) : ma note, et les notes des contacts.
	notes         *notesPont
	notesOn       bool
	notesLues     time.Time
	notesNow      chan struct{}
	notesDerniere map[string]any
	maNoteTray    string // l'id de ma note vue dans le plateau
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:         ctx,
		convs:       map[string]*convState{},
		contacts:    map[int64]*contact{},
		byIGID:      map[string]string{},
		byThreadID:  map[string]string{},
		byUserIGID:  map[string]int64{},
		presenceOn:  presenceEnv(),
		presence:    map[string]presenceState{},
		notesOn:     notesEnv(),
		notesNow:    make(chan struct{}, 1),
		pending:     map[string]string{},
		loaded:      map[string]int64{},
		fetching:    map[string]bool{},
		listCount:   50,
		auteurs:     map[int64]bool{},
		fileAuteurs: make(chan int64, 256),
	}
	mediadl.SetHTTP(exhttp.SensibleClientSettings)
	go p.listLoop()
	go p.boucleAuteurs()
	p.flux = p.newFluxPresence()
	p.notes = nouvellesNotes(opsNote{
		nom: "Instagram", limite: 60, parHeure: 6, duree: 24 * time.Hour,
		publier: p.publierNoteIG, effacer: p.effacerNoteIG,
	})
	go p.notesLoop()
	return p
}

func (p *Pont) client() *instameow.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cli
}

// ---- Démarrage et connexion ----

// Démarre avec la session gardée, sinon dit à l'interface qu'il faut se connecter.
func (p *Pont) start() {
	s := loadSession()
	if s == nil {
		emitState("unpaired")
		return
	}
	p.mu.Lock()
	p.sess = s
	p.me = s.UserID
	p.mu.Unlock()
	p.connect(s, nil)
}

// Connexion avec les cookies de la fenêtre de connexion.
func (p *Pont) login(cmd Cmd) {
	p.mu.Lock()
	if p.loggingIn {
		p.mu.Unlock()
		emitErr("login", errors.New("une connexion est déjà en cours"))
		return
	}
	p.loggingIn = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.loggingIn = false
		p.mu.Unlock()
	}()

	if len(cmd.Cookies) == 0 {
		emitErr("login", errors.New("aucun cookie reçu de la fenêtre de connexion"))
		emitState("unpaired")
		return
	}
	if pl := strings.ToLower(cmd.Platform); pl != "" && !strings.HasPrefix(pl, "instagram") && pl != "ig" {
		log.Warn().Str("platform", cmd.Platform).Msg("connexion : plateforme inattendue, on continue en Instagram")
	}
	c := &cookies.Cookies{Platform: types.Instagram}
	vals := map[cookies.MetaCookieName]string{}
	for k, v := range cmd.Cookies {
		vals[cookies.MetaCookieName(k)] = v
	}
	c.UpdateValues(vals)
	if missing := c.GetMissingCookieNames(); len(missing) > 0 {
		names := make([]string, len(missing))
		for i, m := range missing {
			names[i] = string(m)
		}
		emitErr("login", fmt.Errorf("cookies manquants : %s (es-tu bien connecté à instagram.com ?)", strings.Join(names, ", ")))
		emitState("unpaired")
		return
	}
	log.Info().Strs("cookies", c.GetNames()).Msg("connexion avec les cookies")
	p.disconnect()
	emitState("pairing")
	cli := p.newClient(c)
	viewer, mailbox, err := cli.LoadIndex(p.ctx)
	if err != nil {
		log.Err(err).Msg("connexion : chargement de la boîte de réception")
		emitErr("login", explain(err))
		emitState("unpaired")
		return
	}
	own := cli.GetOwnFBID()
	if own == 0 {
		emitErr("login", errors.New("Instagram n'a pas donné l'identifiant du compte (réessaie, ou ouvre instagram.com/direct dans un navigateur)"))
		emitState("unpaired")
		return
	}
	if ds := c.Get(cookies.IGCookieDSUserID); viewer.ID != "" && ds != "" && viewer.ID != ds {
		log.Warn().Msg("connexion : le compte de la page ne correspond pas au cookie ds_user_id")
	}
	// Un autre compte qu'avant : on repart d'un cache vide.
	old := loadSessionAny()
	if old == nil || old.UserID != own {
		p.forgetAccount()
	}
	s := &Session{Cookies: c, UserID: own, IGID: viewer.ID, Name: viewer.GetName(), Username: viewer.GetUsername()}
	saveSession(s)
	// L'état d'instameow, tout frais (la page vient d'être chargée).
	if st, err := cli.DumpState(); err == nil && st != nil {
		p.saveKVRaw("state", st)
	}
	p.mu.Lock()
	p.sess = s
	p.me = own
	p.mu.Unlock()
	emit("paired", nil)
	p.connect(s, &loaded{cli: cli, viewer: viewer, mailbox: mailbox})
}

type loaded struct {
	cli     *instameow.Client
	viewer  *types.PolarisViewer
	mailbox *slidetypes.Mailbox
}

func (p *Pont) newClient(c *cookies.Cookies) *instameow.Client {
	cli := instameow.NewClient(instameow.ClientParams{
		Cookies:  c,
		Log:      log.With().Str("c", "instameow").Logger(),
		Settings: exhttp.SensibleClientSettings,
	})
	cli.SetEventHandler(func(ctx context.Context, evt slidetypes.ClientEvent) error {
		return p.onIGEvent(ctx, cli, evt)
	})
	p.mu.Lock()
	p.live = cli
	p.mu.Unlock()
	return cli
}

// Les erreurs d'Instagram, en français.
func explain(err error) error {
	switch {
	case errors.Is(err, httpclient.ErrTokenInvalidated), errors.Is(err, httpclient.ErrUserIDIsZero),
		errors.Is(err, types.ErrPleaseLogIn):
		return errors.New("Instagram a fermé la session : reconnecte-toi")
	case errors.Is(err, httpclient.ErrCheckpointRequired):
		return errors.New("Instagram demande une vérification du compte : ouvre instagram.com dans un navigateur, puis reconnecte-toi")
	case errors.Is(err, httpclient.ErrChallengeRequired):
		return errors.New("Instagram demande de confirmer que c'est bien toi : ouvre instagram.com, puis reconnecte-toi")
	case errors.Is(err, httpclient.ErrConsentRequired):
		return errors.New("Instagram demande d'accepter ses conditions : ouvre instagram.com, puis reconnecte-toi")
	case errors.Is(err, httpclient.ErrAccountSuspended):
		return errors.New("compte Instagram suspendu")
	case errors.Is(err, types.ErrContentUnavailable):
		return errors.New("Instagram limite le compte : ouvre l'application ou instagram.com")
	case errors.Is(err, httpclient.ErrRateLimited):
		return errors.New("Instagram limite les connexions : réessaie dans quelques minutes")
	case errors.Is(err, instameow.ErrThreadNotFound):
		return errors.New("conversation introuvable sur Instagram")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("Instagram ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("Instagram : %w", err)
}

// Les erreurs qui demandent une action de l'utilisateur (on arrête de réessayer).
func isAuthError(err error) bool {
	return errors.Is(err, httpclient.ErrTokenInvalidated) || errors.Is(err, httpclient.ErrUserIDIsZero) ||
		errors.Is(err, httpclient.ErrCheckpointRequired) || errors.Is(err, httpclient.ErrChallengeRequired) ||
		errors.Is(err, httpclient.ErrConsentRequired) || errors.Is(err, httpclient.ErrAccountSuspended) ||
		errors.Is(err, types.ErrPleaseLogIn) || errors.Is(err, types.ErrContentUnavailable)
}

// Seule l'invalidation de la session rend les cookies inutiles (comme igconnector) :
// après une vérification sur instagram.com, les mêmes cookies remarchent.
func isLoggedOut(err error) bool {
	return errors.Is(err, httpclient.ErrTokenInvalidated) || errors.Is(err, httpclient.ErrUserIDIsZero) ||
		errors.Is(err, types.ErrPleaseLogIn)
}

// Charge la boîte de réception (ou reprend l'état gardé), puis ouvre le socket.
func (p *Pont) connect(s *Session, pre *loaded) {
	p.mu.Lock()
	if p.connecting {
		p.mu.Unlock()
		return
	}
	p.connecting = true
	ctx, cancel := context.WithCancel(p.ctx)
	if p.connCancel != nil {
		p.connCancel()
	}
	p.connCancel = cancel
	p.gotMailbox = false
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.connecting = false
		p.mu.Unlock()
	}()

	emitState("connecting")
	var cli *instameow.Client
	var viewer *types.PolarisViewer
	var mailbox *slidetypes.Mailbox
	if pre != nil {
		cli, viewer, mailbox = pre.cli, pre.viewer, pre.mailbox
	} else {
		cli = p.newClient(s.Cookies)
		// Reprise rapide (comme igconnector) : l'état de la page gardé (moins de
		// 24 h) et la position dans le flux de deltas. Le socket rattrape alors
		// tout ce qui s'est passé depuis, sans recharger la page.
		fast := false
		if st := p.loadKV("state"); st != nil {
			if err := cli.LoadState(st); err != nil {
				log.Info().Err(err).Msg("état gardé inutilisable, on recharge la page")
			} else {
				var seq seqState
				if raw := p.loadKV("seq"); raw != nil && json.Unmarshal(raw, &seq) == nil && seq.SeqID != 0 {
					cli.SetSeqID(seq.SeqID, seq.TS)
					fast = true
				}
			}
		}
		if fast {
			log.Info().Msg("reprise avec l'état gardé")
		} else {
			for essai := 0; ; essai++ {
				var err error
				viewer, mailbox, err = cli.LoadIndex(ctx)
				if err == nil {
					break
				}
				log.Err(err).Int("essai", essai).Msg("chargement de la boîte de réception")
				if ctx.Err() != nil {
					return
				}
				if errors.Is(err, types.ErrPleaseReloadPage) {
					p.delKV("state")
				}
				if isAuthError(err) || essai >= 6 {
					emitErr("connect", explain(err))
					if isLoggedOut(err) {
						p.dropCookies()
					}
					emitState("unpaired")
					return
				}
				emitState("reconnecting")
				select {
				case <-time.After(time.Duration(2<<essai) * time.Second):
				case <-ctx.Done():
					return
				}
			}
		}
	}
	p.mu.Lock()
	p.cli = cli
	if own := cli.GetOwnFBID(); own != 0 {
		p.me = own
		s.UserID = own
	}
	if viewer != nil {
		s.Name = cmp.Or(viewer.GetName(), s.Name)
		s.Username = cmp.Or(viewer.GetUsername(), s.Username)
		s.IGID = cmp.Or(viewer.ID, s.IGID)
		p.myAvatar = viewer.GetAvatarURL()
	}
	me := p.me
	p.mu.Unlock()
	saveSession(s)
	emit("account", map[string]any{"service": serviceName(), "name": cmp.Or(s.Name, s.Username), "id": idStr(me), "email": ""})

	if mailbox != nil {
		start := time.Now()
		p.processMailbox(mailbox)
		p.mu.Lock()
		p.gotMailbox = true
		p.mu.Unlock()
		p.saveKV("seq", seqState{SeqID: mailbox.UQSeqID, TS: start})
		if !mailbox.ThreadsByFolder.PageInfo.HasNextPage {
			p.saveKV("threads_done", true)
		} else if p.loadKV("threads_done") == nil && p.loadKV("threads_cursor") == nil {
			p.saveKV("threads_cursor", mailbox.ThreadsByFolder.PageInfo.EndCursor)
		}
	}
	if ctx.Err() != nil {
		return
	}
	go cli.Connect(ctx)
	go p.periodicSave(ctx)
}

// Les cookies ne marchent plus : on garde le reste (cache).
func (p *Pont) dropCookies() {
	p.mu.Lock()
	s := p.sess
	p.mu.Unlock()
	if s != nil {
		b := *s
		b.Cookies = &cookies.Cookies{Platform: types.Instagram}
		saveSession(&b)
	}
	p.delKV("state")
}

// Les cookies changent avec les réponses d'Instagram : on les garde de temps en temps.
func (p *Pont) periodicSave(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.mu.Lock()
			s := p.sess
			p.mu.Unlock()
			if s != nil && s.Cookies != nil && s.Cookies.IsLoggedIn() {
				saveSession(s)
			}
		}
	}
}

func (p *Pont) disconnect() {
	p.mu.Lock()
	cli := p.live
	p.live, p.cli = nil, nil
	p.ready = false
	if p.connCancel != nil {
		p.connCancel()
		p.connCancel = nil
	}
	p.mu.Unlock()
	p.flux.arreter()
	if cli != nil {
		cli.SetEventHandler(nil)
		cli.Disconnect()
	}
}

// Recharge tout (page + boîte + socket), au plus une fois par deux minutes.
func (p *Pont) fullReconnect(force bool) {
	p.mu.Lock()
	s := p.sess
	if s == nil || s.Cookies == nil || !s.Cookies.IsLoggedIn() || (!force && time.Since(p.lastFull) < 2*time.Minute) {
		p.mu.Unlock()
		return
	}
	p.lastFull = time.Now()
	p.mu.Unlock()
	log.Info().Msg("reconnexion complète")
	p.disconnect()
	p.connect(s, nil)
}

// Les événements d'instameow. Une erreur rendue sur un delta fait reconnecter
// le socket et rejouer le delta : on ne la rend que si le pont s'arrête.
func (p *Pont) onIGEvent(ctx context.Context, cli *instameow.Client, raw slidetypes.ClientEvent) error {
	p.mu.Lock()
	current := p.live == cli
	p.mu.Unlock()
	if !current {
		return errors.New("ancien client")
	}
	switch evt := raw.(type) {
	case *slidetypes.Connected:
		log.Info().Int64("seq", evt.SubscribedSeqID).Int64("dernier", evt.LatestSeqID).Msg("connecté au socket Instagram")
		p.mu.Lock()
		p.ready = true
		p.mu.Unlock()
		emitState("ready")
		p.emitList()
		p.setPresence(p.presenceWanted())
		go p.notes.annoncer()
		p.demanderNotes(false)
		go p.threadBackfill(cli)
		go p.myAvatarDownload()
	case *slidetypes.Disconnected:
		p.mu.Lock()
		p.ready = false
		p.mu.Unlock()
		if dgw.IsUnauthorized(evt.Error) || errors.Is(evt.Error, instameow.ErrUserIDMissing) {
			log.Err(evt.Error).Msg("socket Instagram : session refusée")
			emitErr("listen", errors.New("Instagram a fermé la session : reconnecte-toi"))
			p.dropCookies()
			go p.disconnect()
			emitState("unpaired")
			return errors.New("session refusée")
		}
		log.Warn().Err(evt.Error).Int("échecs", evt.FailureCount).Msg("socket Instagram coupé")
		emitState("reconnecting")
		if evt.FailureCount > 5 && errors.Is(evt.Error, instameow.ErrMainStreamClosed) {
			// Le socket n'arrive plus à s'abonner : on recharge tout un peu plus tard.
			go func() {
				select {
				case <-time.After(time.Minute):
					p.fullReconnect(true)
				case <-p.ctx.Done():
				}
			}()
			return errors.New("flux principal fermé trop souvent")
		}
	case *slidetypes.AuthError:
		log.Warn().Err(evt.Error).Msg("Instagram : erreur de session")
		emitErr("listen", explain(evt.Error))
		if isLoggedOut(evt.Error) {
			p.dropCookies()
		}
		if isAuthError(evt.Error) {
			go p.disconnect()
			emitState("unpaired")
		}
	case *slidetypes.SeqIDUpdate:
		p.saveKV("seq", seqState{SeqID: evt.SeqID, TS: evt.Timestamp})
	case *slidetypes.ReconnectionStateUpdate:
		if evt.State != nil {
			p.saveKVRaw("state", evt.State)
		}
	case *slidetypes.ResnapshotRequired:
		log.Info().Msg("Instagram demande de tout recharger")
		p.delKV("seq")
		go p.fullReconnect(true)
	case *slidetypes.Delta:
		p.handleDelta(ctx, cli, evt)
	case *slidetypes.TypingNotification:
		p.handleTyping(evt)
	default:
		log.Debug().Type("type", raw).Msg("évènement instameow ignoré")
	}
	return nil
}

// ---- Les fils ----

func (p *Pont) conv(id string) *convState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.convs[id]
}

// À appeler avec p.mu.
func (p *Pont) getOrMakeConv(key int64) *convState {
	id := idStr(key)
	c := p.convs[id]
	if c == nil {
		c = &convState{Key: key, Folder: "inbox"}
		p.convs[id] = c
	}
	return c
}

// À appeler avec p.mu : une personne vue dans un fil ou un message.
func (p *Pont) upsertUser(u *slidetypes.User) int64 {
	if u == nil || u.InteropMessagingUserFBID == 0 {
		return 0
	}
	id := u.InteropMessagingUserFBID
	c := p.contacts[id]
	if c == nil {
		c = &contact{}
		p.contacts[id] = c
	}
	igid := cmp.Or(u.ID, u.PK)
	changed := false
	set := func(dst *string, v string) {
		if v != "" && *dst != v {
			*dst, changed = v, true
		}
	}
	set(&c.Name, cmp.Or(u.FullName, u.Username))
	set(&c.Username, u.Username)
	set(&c.IGID, igid)
	set(&c.Avatar, u.ProfilePicURL)
	if igid != "" {
		p.byUserIGID[igid] = id
	}
	if changed {
		p.saveContact(id, c)
		p.listDirty = true
	}
	return id
}

func folderOf(t *slidetypes.ThreadInfo) string {
	return folderName(cmp.Or(t.SystemFolder, t.Folder))
}

// Les dossiers d'Instagram, dans les mots du pont Messenger : « inbox » pour
// la boîte principale, le reste va dans les archives de l'interface.
func folderName(f string) string {
	switch strings.ToUpper(f) {
	case "PENDING":
		return "pending"
	case "SPAM":
		return "spam"
	case "HIDDEN", "OTHER":
		return "hidden"
	default:
		return "inbox"
	}
}

// Range un fil (boîte de réception, page d'un fil, delta). Avec withMsgs, les
// messages livrés avec le fil vont aussi dans le cache (fil complet) ; sinon
// seul le plus récent sert d'aperçu (la boîte ne livre que des messages abrégés).
func (p *Pont) upsertThread(t *slidetypes.ThreadInfo, withMsgs bool) string {
	if t == nil {
		return ""
	}
	key := t.ThreadKey
	if key == 0 {
		key, _ = strconv.ParseInt(t.ID, 10, 64)
	}
	if e := log.Debug(); e.Enabled() {
		var types []string
		if t.SlideMessages != nil {
			for _, m := range t.SlideMessages.Edges {
				if m.Node != nil {
					types = append(types, m.Node.Content.TypeName+"/"+m.Node.ContentType)
				}
			}
		}
		var extra []string
		for k := range t.Unrecognized {
			extra = append(extra, k)
		}
		e.Int64("fil", key).Strs("types", types).Int("accusés", len(t.SlideReadReceipts)).
			Bool("non_lu", t.MarkedAsUnread).Str("dossier", t.SystemFolder+"/"+t.Folder).
			// Pour reconnaître un jour les canaux de diffusion (§5.13) : instameow
			// ne connaît que les sous-types direct, entreprise, IA et groupe.
			Str("sous_type", string(t.ThreadSubtype)).Bool("diffusion", t.CreatorBroadcastThreadData != nil).
			Strs("champs_inconnus", extra).Msg("fil reçu")
	}
	if key == 0 {
		return ""
	}
	id := idStr(key)
	p.mu.Lock()
	me := p.me
	c := p.getOrMakeConv(key)
	c.IGID = cmp.Or(t.ID, c.IGID)
	c.IGThreadID = cmp.Or(t.ThreadID, c.IGThreadID)
	c.Group = t.IsGroup || len(t.Users) > 1
	if c.Group {
		c.Name = t.ThreadTitle
		c.Picture = t.ThreadImageURL
	}
	var members []int64
	for _, u := range t.Users {
		if m := p.upsertUser(u); m != 0 {
			members = append(members, m)
		}
	}
	if t.Viewer != nil {
		if v := p.upsertUser(t.Viewer); v != 0 && v == me && t.Viewer.ProfilePicURL != "" {
			p.myAvatar = t.Viewer.ProfilePicURL
		}
	}
	if len(members) > 0 || len(t.Users) == 0 && t.Viewer != nil {
		c.Members = members
	}
	c.Folder = folderOf(t)
	c.LastActivity = max(c.LastActivity, t.LastActivityTimestampMS.UnixMilli())
	var receipts []int64
	for _, rr := range t.SlideReadReceipts {
		ts := rr.WatermarkTimestampMS.UnixMilli()
		if rr.ParticipantFBID == me {
			c.ReadWM = max(c.ReadWM, ts)
		} else if ts > 0 {
			receipts = append(receipts, ts)
		}
	}
	c.MarkedUnread = t.MarkedAsUnread
	c.Hidden = false
	p.indexConv(id, c)
	p.saveConv(id, c)
	p.listDirty = true
	p.mu.Unlock()

	if t.SlideMessages != nil {
		apercu := false
		for _, e := range t.SlideMessages.Edges {
			r := p.igRow(id, e.Node)
			if r == nil {
				continue
			}
			if withMsgs {
				p.storeRow(r)
			} else if !apercu {
				// Du plus récent au plus vieux : le premier sert d'aperçu.
				p.noteLast(id, r)
				apercu = true
			}
		}
		if withMsgs && !t.SlideMessages.PageInfo.HasNextPage && len(t.SlideMessages.Edges) > 0 {
			p.mu.Lock()
			c.NoMore = true
			p.saveConv(id, c)
			p.mu.Unlock()
		}
	}
	for _, ts := range receipts {
		p.markMineRead(id, ts, "OUTGOING_READ")
	}
	return id
}

func (p *Pont) processMailbox(mb *slidetypes.Mailbox) {
	for _, pin := range mb.PinnedThreadsV2 {
		p.upsertThread(pin.AsIGDirectThread, false)
	}
	for _, e := range mb.ThreadsByFolder.Edges {
		p.upsertThread(e.Node.AsIGDirectThread, false)
	}
	log.Info().Int("fils", len(mb.ThreadsByFolder.Edges)).Bool("encore", mb.ThreadsByFolder.PageInfo.HasNextPage).Msg("boîte de réception chargée")
	p.markDirty()
}

// Les fils plus anciens que la première page de la boîte de réception.
func (p *Pont) threadBackfill(cli *instameow.Client) {
	p.mu.Lock()
	if p.backfilling {
		p.mu.Unlock()
		return
	}
	p.backfilling = true
	got := p.gotMailbox
	me := p.me
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.backfilling = false
		p.mu.Unlock()
	}()
	// Reprise rapide : la première page n'a pas été rechargée, on la redemande
	// pour rafraîchir noms, avatars et aperçus (sans toucher au flux de deltas).
	if !got {
		ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
		resp, err := cli.GetMailbox(ctx)
		annule()
		if err != nil || resp.GetMailbox() == nil {
			log.Warn().Err(err).Msg("boîte de réception")
		} else {
			p.processMailbox(resp.Mailbox)
			p.mu.Lock()
			p.gotMailbox = true
			p.mu.Unlock()
			if p.loadKV("threads_done") == nil && p.loadKV("threads_cursor") == nil && resp.Mailbox.ThreadsByFolder.PageInfo.HasNextPage {
				p.saveKV("threads_cursor", resp.Mailbox.ThreadsByFolder.PageInfo.EndCursor)
			}
		}
	}
	if p.loadKV("threads_done") != nil || me == 0 {
		p.emitList()
		return
	}
	var cursor string
	_ = json.Unmarshal(p.loadKV("threads_cursor"), &cursor)
	for lot := 0; lot < 10 && cursor != ""; lot++ {
		if p.client() != cli {
			return
		}
		req := slidetypes.MakePaginateMailboxRequest(me, cursor, "INBOX", nil)
		req.Count = 20
		ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
		resp, err := cli.PaginateMailbox(ctx, req)
		annule()
		if err != nil && (resp == nil || resp.Mailbox == nil) {
			log.Warn().Err(err).Msg("fils plus anciens")
			break
		}
		if resp.Mailbox == nil {
			break
		}
		for _, e := range resp.Mailbox.ThreadsByFolder.Edges {
			p.upsertThread(e.Node.AsIGDirectThread, false)
		}
		pi := resp.Mailbox.ThreadsByFolder.PageInfo
		if !pi.HasNextPage || pi.EndCursor == "" {
			p.saveKV("threads_done", true)
			p.delKV("threads_cursor")
			break
		}
		cursor = pi.EndCursor
		p.saveKV("threads_cursor", cursor)
		time.Sleep(2 * time.Second)
	}
	p.emitList()
}

// L'identifiant court du fil (et le long) : demandés à Instagram s'il le faut.
func (p *Pont) ensureIGID(ctx context.Context, cli *instameow.Client, conv string, c *convState) (string, string, error) {
	p.mu.Lock()
	igid, long := c.IGID, c.IGThreadID
	p.mu.Unlock()
	if igid != "" && long != "" {
		return igid, long, nil
	}
	ids, err := cli.FetchThreadID(ctx, c.Key)
	if err != nil {
		return igid, long, err
	}
	p.mu.Lock()
	c.IGID, c.IGThreadID = ids.ShortID, ids.LongID
	p.indexConv(conv, c)
	p.saveConv(conv, c)
	p.mu.Unlock()
	return ids.ShortID, ids.LongID, nil
}

func (p *Pont) contactName(id int64) string {
	if c := p.contacts[id]; c != nil {
		return cmp.Or(c.Name, c.Username)
	}
	return ""
}

var hiddenFolders = map[string]bool{"hidden": true, "spam": true}

// À appeler avec p.mu.
func (p *Pont) convOut(id string, c *convState) Conversation {
	name := c.Name
	var members []string
	for _, m := range c.Members {
		if m == p.me {
			continue
		}
		if n := p.contactName(m); n != "" {
			members = append(members, n)
		}
	}
	if !c.Group {
		if n := p.contactName(c.Key); n != "" {
			name = n
		}
		if len(members) == 0 && name != "" {
			members = []string{name}
		}
	}
	if name == "" && len(members) > 0 {
		name = strings.Join(members, ", ")
	}
	if name == "" {
		name = "Conversation Instagram"
	}
	t := max(c.LastActivity, c.LastTime)
	unread := c.MarkedUnread || (!c.LastFromMe && t > c.ReadWM && t > 0 && c.LastTime > 0)
	return Conversation{
		ID:       id,
		Name:     name,
		Preview:  c.LastText,
		FromMe:   c.LastFromMe,
		Time:     t,
		Unread:   unread,
		Group:    c.Group,
		Members:  orEmpty(members),
		ReadOnly: c.ReadOnly,
		Archived: c.Folder != "" && c.Folder != "inbox",
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (p *Pont) markDirty() {
	p.mu.Lock()
	p.listDirty = true
	p.mu.Unlock()
}

// Renvoie la liste à l'interface quand elle a changé (au plus toutes les 2 s).
func (p *Pont) listLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
			p.mu.Lock()
			dirty := p.listDirty && p.ready
			p.mu.Unlock()
			if dirty {
				p.emitList()
			}
		}
	}
}

func (p *Pont) emitList() {
	p.mu.Lock()
	n, arch := p.listCount, p.archiveWanted
	p.listDirty = false
	p.mu.Unlock()
	p.sendList(n, false, true)
	if arch {
		p.sendList(n, true, false)
	}
}

func (p *Pont) listConversations(n int, archive bool) {
	p.mu.Lock()
	if archive {
		p.archiveWanted = true
	} else {
		p.listCount = max(n, p.listCount)
	}
	p.mu.Unlock()
	p.sendList(n, archive, true)
}

func (p *Pont) sendList(n int, archive, avatars bool) {
	p.mu.Lock()
	items := []Conversation{}
	for id, c := range p.convs {
		if c.Hidden || hiddenFolders[c.Folder] {
			continue
		}
		co := p.convOut(id, c)
		if co.Archived != archive || co.Time == 0 && co.Preview == "" {
			continue
		}
		items = append(items, co)
	}
	p.mu.Unlock()
	slices.SortStableFunc(items, func(a, b Conversation) int { return cmp.Compare(b.Time, a.Time) })
	if len(items) > n {
		items = items[:n]
	}
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.ID)
	}
	ev := map[string]any{"items": items}
	if archive {
		ev["folder"] = "archive"
	}
	emit("conversations", ev)
	if avatars {
		go p.avatars(keys)
	}
}

// ---- Les messages ----

func (p *Pont) messages(cmd Cmd) {
	count := int(cmd.Count)
	if count <= 0 {
		count = 50
	}
	conv := cmd.Conv
	c := p.conv(conv)
	if c == nil {
		emitErr("messages", fmt.Errorf("conversation inconnue : %s", conv))
		emit("messages", map[string]any{"conv": conv, "items": []Message{}, "older": cmd.Before != "", "more": false})
		return
	}
	// Première ouverture du fil depuis le démarrage : la page la plus récente,
	// pour combler ce qui a pu arriver pendant que Jasette était fermée.
	p.mu.Lock()
	_, dejaVu := p.loaded[conv]
	p.mu.Unlock()
	if cmd.Before == "" && !dejaVu {
		if err := p.fetchLatest(conv, c); err != nil {
			log.Warn().Err(err).Str("fil", conv).Msg("messages récents")
		}
	}
	rows := p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
	more := len(rows) >= count
	p.mu.Lock()
	noMore := c.NoMore
	p.mu.Unlock()
	if len(rows) < count && !noMore && p.client() != nil {
		oldestID := cmd.Before
		if len(rows) > 0 {
			oldestID = rows[0].ID
		}
		remoteMore := p.fetchOlder(conv, c, oldestID, count-len(rows))
		rows = p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
		more = len(rows) >= count || (remoteMore && len(rows) > 0)
	}
	items := make([]Message, 0, len(rows))
	p.mu.Lock()
	for _, r := range rows {
		items = append(items, p.msgOut(r))
	}
	if len(items) > 0 {
		if old, ok := p.loaded[conv]; !ok || items[0].Time < old {
			p.loaded[conv] = items[0].Time
		}
	} else if _, ok := p.loaded[conv]; !ok {
		p.loaded[conv] = time.Now().UnixMilli()
	}
	p.mu.Unlock()
	emit("messages", map[string]any{"conv": conv, "items": items, "older": cmd.Before != "", "more": more})
}

// À appeler avec p.mu : le format de l'interface, avec les noms à jour.
func (p *Pont) msgOut(r *msgRow) Message {
	m := r.Message
	if r.SenderID != 0 && !m.FromMe {
		if n := p.contactName(r.SenderID); n != "" {
			m.Sender = n
		}
	}
	p.noteAuteur(r, &m)
	m.Reactions = slices.Clone(m.Reactions)
	for i := range m.Reactions {
		if n := p.contactName(m.Reactions[i].Actor); n != "" {
			m.Reactions[i].Sender = n
		}
	}
	return m
}

// La page la plus récente d'un fil (avec les infos du fil).
func (p *Pont) fetchLatest(conv string, c *convState) error {
	cli := p.client()
	if cli == nil {
		return nil
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	igid, _, err := p.ensureIGID(ctx, cli, conv, c)
	if igid == "" {
		if errors.Is(err, instameow.ErrThreadNotFound) {
			// Nouvelle conversation pas encore créée chez Instagram : rien à charger.
			return nil
		}
		return err
	}
	resp, err := cli.GetThread(ctx, slidetypes.MakeGetThreadInfoRequest(igid))
	if err != nil {
		return err
	}
	p.upsertThread(resp.ThreadInfo.AsIGDirectThread, true)
	return nil
}

// Demande des messages plus anciens que « beforeID » à Instagram. Vrai s'il en reste encore.
func (p *Pont) fetchOlder(conv string, c *convState, beforeID string, count int) bool {
	cli := p.client()
	if cli == nil {
		return false
	}
	p.mu.Lock()
	if p.fetching[conv] {
		p.mu.Unlock()
		return false
	}
	p.fetching[conv] = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.fetching, conv)
		p.mu.Unlock()
	}()
	ctx, annule := context.WithTimeout(p.ctx, 60*time.Second)
	defer annule()
	igid, _, err := p.ensureIGID(ctx, cli, conv, c)
	if igid == "" {
		if err != nil && !errors.Is(err, instameow.ErrThreadNotFound) {
			log.Warn().Err(err).Str("fil", conv).Msg("historique : identifiant du fil")
		}
		return false
	}
	// Un message local (envoi pas encore confirmé) ne sert pas de repère.
	if strings.HasPrefix(beforeID, "tmp_") || strings.HasPrefix(beforeID, "local:") {
		beforeID = ""
		for _, r := range p.queryMsgs(conv, 0, "", 200) {
			if !strings.HasPrefix(r.ID, "tmp_") {
				beforeID = r.ID
				break
			}
		}
	}
	if beforeID == "" {
		resp, err := cli.GetThread(ctx, slidetypes.MakeGetThreadInfoRequest(igid))
		if err != nil {
			log.Warn().Err(err).Str("fil", conv).Msg("historique")
			return false
		}
		t := resp.ThreadInfo.AsIGDirectThread
		p.upsertThread(t, true)
		return t != nil && t.SlideMessages != nil && t.SlideMessages.PageInfo.HasNextPage
	}
	anchor := &beforeID
	var cursor *string
	hasMore, got := true, 0
	for hasMore && got < count {
		resp, err := cli.PaginateMessages(ctx, &slidetypes.PaginateMessagesRequest{
			AfterCursor:             cursor,
			OlderThanMessageID:      anchor,
			ThreadID:                igid,
			FirstN:                  20,
			InitialMessagePageCount: 20,
		})
		if err != nil {
			log.Warn().Err(err).Str("fil", conv).Msg("historique")
			return false
		}
		t := resp.ThreadInfo.AsIGDirectThread
		if t == nil || t.Messages == nil {
			hasMore = false
			break
		}
		for _, e := range t.Messages.Edges {
			if r := p.igRow(conv, e.Node); r != nil {
				p.storeRow(r)
			}
		}
		got += len(t.Messages.Edges)
		hasMore = t.Messages.PageInfo.HasNextPage && t.Messages.PageInfo.EndCursor != ""
		anchor, cursor = nil, ptr.Ptr(t.Messages.PageInfo.EndCursor)
		if len(t.Messages.Edges) == 0 {
			hasMore = false
		}
	}
	if !hasMore {
		p.mu.Lock()
		c.NoMore = true
		p.saveConv(conv, c)
		p.mu.Unlock()
	}
	return hasMore
}

func tmpOr(tmp string, otid int64) string {
	if tmp != "" {
		return tmp
	}
	return "tmp_" + strconv.FormatInt(otid, 10)
}

// Un message envoyé : on l'annonce tout de suite (l'écho du socket peut tarder).
func (p *Pont) echoSent(conv, id, tmp, text string, ts int64, atts []Attachment) {
	r := p.getMsg(id)
	if r == nil {
		p.mu.Lock()
		me := p.me
		p.mu.Unlock()
		r = &msgRow{Message: Message{ID: id, Conv: conv, Text: text, FromMe: true, Time: ts, Kind: "instagram", Attachments: atts}, SenderID: me}
		r.Media = mediaSummary(atts)
	}
	r.TmpID = tmp
	if !strings.HasPrefix(r.Status, "OUTGOING_") || r.Status == "OUTGOING_SENDING" {
		r.Status = "OUTGOING_COMPLETE"
	}
	r.FromMe = true
	p.putMsg(r)
	p.noteLast(conv, r)
	p.mu.Lock()
	out := p.msgOut(r)
	p.mu.Unlock()
	emit("message", map[string]any{"item": out})
}

func (p *Pont) send(cmd Cmd) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return
	}
	p.sendCommon(cmd, text, "", "")
}

// Envoi commun texte/fichier.
func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	otid := methods.GenerateEpochID()
	tmp := tmpOr(cmd.TmpID, otid)
	fail := func(err error) {
		log.Warn().Err(err).Str("fil", cmd.Conv).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	c := p.conv(cmd.Conv)
	if c == nil {
		fail(fmt.Errorf("conversation inconnue : %s", cmd.Conv))
		return
	}
	cli := p.client()
	if cli == nil {
		fail(errors.New("Instagram n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	igid, _, err := p.ensureIGID(ctx, cli, cmd.Conv, c)
	p.mu.Lock()
	group := c.Group
	p.mu.Unlock()
	if igid == "" && (path != "" || group || !errors.Is(err, instameow.ErrThreadNotFound)) {
		if err == nil {
			err = errors.New("identifiant du fil inconnu")
		}
		fail(explain(err))
		return
	}

	if path != "" {
		id, atts, err := p.uploadMedia(ctx, cli, c.Key, path, mime)
		if err != nil {
			fail(fmt.Errorf("envoi du fichier : %w", explain(err)))
			return
		}
		otidStr := strconv.FormatInt(otid, 10)
		p.mu.Lock()
		p.pending[otidStr] = tmp
		p.mu.Unlock()
		resp, err := cli.SendMedia(ctx, &slidetypes.SendMediaRequest{
			AttachmentFBID:     strconv.FormatInt(id, 10),
			ThreadID:           igid,
			OfflineThreadingID: otidStr,
		})
		if err != nil {
			fail(explain(err))
			return
		}
		sent := resp.GetMessage()
		msgID := cmp.Or(sent.ID, sent.MessageID)
		if msgID == "" {
			fail(errors.New("Instagram n'a pas confirmé l'envoi"))
			return
		}
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
		p.echoSent(cmd.Conv, msgID, tmp, "", cmp.Or(sent.TimestampMS.UnixMilli(), time.Now().UnixMilli()), atts)
		if text == "" {
			return
		}
		// La légende part en message séparé (Instagram n'a pas de légende).
		otid = methods.GenerateEpochID()
		tmp = "tmp_" + strconv.FormatInt(otid, 10)
	}

	otidStr := strconv.FormatInt(otid, 10)
	req := &slidetypes.SendTextRequest{
		OfflineThreadingID: otidStr,
		Text:               slidetypes.SensitiveString{Value: text},
		SendAttribution:    ptr.Ptr("igd_web_chat_tab:in_thread"),
	}
	if igid != "" {
		req.IGThreadIGID = &igid
	} else {
		// Pas encore de fil : Instagram le crée au premier message (comme igconnector).
		req.RecipientIGIDs = []string{idStr(c.Key)}
	}
	p.mu.Lock()
	p.pending[otidStr] = tmp
	p.mu.Unlock()
	resp, err := cli.SendMessage(ctx, req)
	if err != nil {
		fail(explain(err))
		return
	}
	sent := resp.GetMessage()
	msgID := cmp.Or(sent.ID, sent.MessageID)
	if msgID == "" {
		fail(errors.New("Instagram n'a pas confirmé l'envoi"))
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	p.echoSent(cmd.Conv, msgID, tmp, text, cmp.Or(sent.TimestampMS.UnixMilli(), time.Now().UnixMilli()), nil)
	if igid == "" {
		// Le fil existe maintenant : ses identifiants viendront au prochain besoin.
		go func() {
			ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
			defer annule()
			_, _, _ = p.ensureIGID(ctx, cli, cmd.Conv, c)
		}()
	}
}

func (p *Pont) markRead(cmd Cmd) {
	c := p.conv(cmd.Conv)
	cli := p.client()
	if c == nil {
		return
	}
	r := p.getMsg(cmd.Msg)
	if r == nil || r.FromMe || r.Conv != cmd.Conv || strings.HasPrefix(r.ID, "tmp_") {
		r = p.lastIncoming(cmd.Conv)
	}
	p.mu.Lock()
	c.ReadWM = max(c.ReadWM, time.Now().UnixMilli())
	c.MarkedUnread = false
	p.saveConv(cmd.Conv, c)
	p.listDirty = true
	p.mu.Unlock()
	if cli == nil || r == nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	_, long, err := p.ensureIGID(ctx, cli, cmd.Conv, c)
	if long == "" {
		log.Warn().Err(err).Msg("lu : identifiant du fil")
		return
	}
	if _, err := cli.MarkRead(ctx, &slidetypes.MarkReadRequest{
		Metadata: slidetypes.MarkReadMetadata{IGThreadIGID: long},
		Data:     slidetypes.MarkReadData{MessageID: r.ID, ItemID: ptr.Ptr("")},
	}); err != nil {
		log.Warn().Err(err).Msg("lu")
		return
	}
	if _, err := cli.MarkReadValidation(ctx, &slidetypes.MarkReadRequest{
		Metadata: slidetypes.MarkReadMetadata{IGThreadIGID: long},
		Data:     slidetypes.MarkReadData{MessageID: r.ID, MessageTimestampMS: jsontime.UnixMilliString{Time: time.UnixMilli(r.Time)}},
	}); err != nil {
		log.Warn().Err(err).Msg("lu : validation")
	}
}

// Nouvelle conversation : un identifiant (chiffres) d'une personne, sinon une recherche.
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimSpace(cmd.Number)
	if q == "" {
		q = strings.TrimSpace(cmd.Text)
	}
	if q == "" {
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(q, "+"), 10, 64)
	if err != nil || id <= 0 {
		p.search(strings.TrimPrefix(q, "@"))
		return
	}
	cli := p.client()
	if cli == nil {
		emitErr("new", errors.New("Instagram n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	conv := idStr(id)
	ids, err := cli.FetchThreadID(ctx, id)
	switch {
	case err == nil:
		resp, err := cli.GetThread(ctx, slidetypes.MakeGetThreadInfoRequest(ids.ShortID))
		if err != nil {
			emitErr("new", fmt.Errorf("nouvelle conversation : %w", explain(err)))
			return
		}
		if got := p.upsertThread(resp.ThreadInfo.AsIGDirectThread, true); got != "" {
			conv = got
		}
	case errors.Is(err, instameow.ErrThreadNotFound):
		// Jamais écrit à cette personne : le fil sera créé au premier message.
		if u, err := cli.GetUserForNewDM(ctx, id); err == nil && u.Data != nil {
			u.Data.InteropMessagingUserFBID = cmp.Or(u.Data.InteropMessagingUserFBID, id)
			p.mu.Lock()
			p.upsertUser(u.Data)
			p.mu.Unlock()
		} else if err != nil {
			log.Warn().Err(err).Msg("nouvelle conversation : profil")
		}
	default:
		emitErr("new", fmt.Errorf("nouvelle conversation : %w", explain(err)))
		return
	}
	p.mu.Lock()
	c := p.getOrMakeConv(id)
	if len(c.Members) == 0 {
		c.Members = []int64{id}
	}
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	c.Hidden = false
	p.saveConv(conv, c)
	item := p.convOut(conv, c)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{conv})
}

// Recherche de personnes : « search{items:[{id,name,avatar}]} ».
func (p *Pont) search(q string) {
	cli := p.client()
	if cli == nil || q == "" {
		emit("search", map[string]any{"query": q, "items": []any{}})
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	resp, err := cli.SearchUsers(ctx, q)
	if err != nil {
		emitErr("search", fmt.Errorf("recherche : %w", explain(err)))
		emit("search", map[string]any{"query": q, "items": []any{}})
		return
	}
	type item struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Avatar string `json:"avatar,omitempty"`
	}
	items := []item{}
	vus := map[int64]bool{}
	p.mu.Lock()
	for _, r := range resp.Data.Results {
		if r == nil || r.InteropMessagingUserFBID == 0 || vus[r.InteropMessagingUserFBID] {
			continue
		}
		vus[r.InteropMessagingUserFBID] = true
		p.upsertUser(&slidetypes.User{
			InteropMessagingUserFBID: r.InteropMessagingUserFBID,
			Username:                 r.Username,
			FullName:                 r.FullName,
			PK:                       r.PK,
			ProfilePicURL:            r.ProfilePicURL,
		})
		name := cmp.Or(r.FullName, r.Username)
		if r.FullName != "" && r.Username != "" {
			name = fmt.Sprintf("%s (@%s)", r.FullName, r.Username)
		}
		items = append(items, item{ID: idStr(r.InteropMessagingUserFBID), Name: name, Avatar: r.ProfilePicURL})
	}
	p.mu.Unlock()
	emit("search", map[string]any{"query": q, "items": items})
}

// Oublie le compte : cache et état.
func (p *Pont) forgetAccount() {
	p.mu.Lock()
	p.convs = map[string]*convState{}
	p.contacts = map[int64]*contact{}
	p.byIGID = map[string]string{}
	p.byThreadID = map[string]string{}
	p.byUserIGID = map[string]int64{}
	p.loaded = map[string]int64{}
	p.mu.Unlock()
	if err := p.wipeStore(); err != nil {
		log.Warn().Err(err).Msg("effacement du cache")
	}
}

func (p *Pont) unpair() {
	p.disconnect()
	p.forgetAccount()
	p.mu.Lock()
	p.sess = nil
	p.me = 0
	p.mu.Unlock()
	_ = removeFile(sessionPath())
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.mu.Lock()
	s := p.sess
	p.mu.Unlock()
	if s != nil && s.Cookies != nil && s.Cookies.IsLoggedIn() {
		saveSession(s)
	}
	p.disconnect()
	if p.db != nil {
		_ = p.db.Close()
	}
}
