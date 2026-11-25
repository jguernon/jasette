package main

// La connexion à Meta (messagix) : connexion avec les cookies, chargement de
// la page, socket lightspeed, et toutes les commandes de l'interface.

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/util/exhttp"
	"go.mau.fi/util/exsync"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/dgw"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/securehistory"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/mautrix-meta/pkg/msgconv/mediadl"
)

type Pont struct {
	ctx context.Context

	db      *sql.DB
	waStore *sqlstore.Container

	mu       sync.Mutex
	sess     *Session
	cli      *messagix.Client
	e2ee     *whatsmeow.Client
	waDevice *store.Device
	me       int64
	myAvatar string

	convs      map[string]*convState
	contacts   map[int64]*contact
	threadMap  map[int64]int64 // fil Facebook « hybride » -> fil chiffré
	openThread map[int64]int64 // fil ouvert (ancien) -> fil chiffré qui l'a remplacé

	tableMu  sync.Mutex
	waiters  map[string][]chan struct{} // fil -> qui attend des messages
	pending  map[string]string          // otid -> tmp_id de l'interface
	loaded   map[string]int64           // fil -> plus vieux message montré à l'interface
	fetching map[string]bool

	listDirty     bool
	listCount     int
	archiveWanted bool
	ready         bool
	connecting    bool
	loggingIn     bool
	lastFull      time.Time
	connCancel    context.CancelFunc

	e2eeReady      *exsync.Event
	e2eeRetried    bool
	historyBackup  *securehistory.Backup
	historyChecked bool
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:        ctx,
		convs:      map[string]*convState{},
		contacts:   map[int64]*contact{},
		threadMap:  map[int64]int64{},
		openThread: map[int64]int64{},
		waiters:    map[string][]chan struct{}{},
		pending:    map[string]string{},
		loaded:     map[string]int64{},
		fetching:   map[string]bool{},
		listCount:  50,
		e2eeReady:  exsync.NewEvent(),
	}
	mediadl.SetHTTP(exhttp.SensibleClientSettings)
	go p.listLoop()
	return p
}

func (p *Pont) client() *messagix.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cli
}

func (p *Pont) waClient() *whatsmeow.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.e2ee
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
	p.connect(s, nil, nil)
}

func platformFrom(name string) types.Platform {
	switch strings.ToLower(name) {
	case "facebook", "facebook.com", "fb":
		return types.Facebook
	default:
		return types.Messenger
	}
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
	plat := platformFrom(cmd.Platform)
	c := &cookies.Cookies{Platform: plat}
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
		emitErr("login", fmt.Errorf("cookies manquants : %s (es-tu bien connecté ?)", strings.Join(names, ", ")))
		emitState("unpaired")
		return
	}
	p.disconnect()
	emitState("pairing")
	cli := p.newClient(c)
	user, tbl, err := cli.LoadMessagesPage(p.ctx)
	if err != nil {
		log.Err(err).Msg("connexion : chargement de la page")
		emitErr("login", explain(err))
		emitState("unpaired")
		return
	}
	if user.GetFBID() != c.GetUserID() {
		emitErr("login", errors.New("le compte connecté ne correspond pas aux cookies"))
		emitState("unpaired")
		return
	}
	// Un autre compte qu'avant : on repart d'un cache vide.
	old := loadSessionAny()
	if old == nil || old.UserID != user.GetFBID() {
		p.forgetAccount()
	}
	s := &Session{Platform: plat, Cookies: c, UserID: user.GetFBID(), Name: user.GetName()}
	if old != nil && old.UserID == s.UserID {
		s.WADeviceID = old.WADeviceID
		s.HistoryDevice = old.HistoryDevice
		s.HistoryDeclined = old.HistoryDeclined
	}
	saveSession(s)
	p.mu.Lock()
	p.sess = s
	p.me = s.UserID
	p.mu.Unlock()
	emit("paired", nil)
	p.connect(s, cli, &loaded{user: user, tbl: tbl})
}

type loaded struct {
	user types.UserInfo
	tbl  *table.LSTable
}

func (p *Pont) newClient(c *cookies.Cookies) *messagix.Client {
	cli := messagix.NewClient(c, log.With().Str("c", "messagix").Logger(), &messagix.Config{
		ClientSettings: exhttp.SensibleClientSettings,
	})
	cli.SetEventHandler(p.onMetaEvent)
	return cli
}

// Les erreurs de Meta, en français.
func explain(err error) error {
	switch {
	case errors.Is(err, httpclient.ErrTokenInvalidated), errors.Is(err, httpclient.ErrUserIDIsZero),
		errors.Is(err, types.ErrPleaseLogIn):
		return errors.New("Messenger a fermé la session : reconnecte-toi")
	case errors.Is(err, httpclient.ErrCheckpointRequired):
		return errors.New("Facebook demande une vérification du compte : ouvre facebook.com dans un navigateur, puis reconnecte-toi")
	case errors.Is(err, httpclient.ErrChallengeRequired):
		return errors.New("Facebook demande de confirmer que c'est bien toi : ouvre facebook.com, puis reconnecte-toi")
	case errors.Is(err, httpclient.ErrConsentRequired):
		return errors.New("Facebook demande d'accepter ses conditions : ouvre facebook.com, puis reconnecte-toi")
	case errors.Is(err, httpclient.ErrAccountSuspended):
		return errors.New("compte Facebook suspendu")
	case errors.Is(err, httpclient.ErrRateLimited):
		return errors.New("Facebook limite les connexions : réessaie dans quelques minutes")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("Messenger ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("Messenger : %w", err)
}

func isAuthError(err error) bool {
	return errors.Is(err, httpclient.ErrTokenInvalidated) || errors.Is(err, httpclient.ErrUserIDIsZero) ||
		errors.Is(err, httpclient.ErrCheckpointRequired) || errors.Is(err, httpclient.ErrChallengeRequired) ||
		errors.Is(err, httpclient.ErrConsentRequired) || errors.Is(err, httpclient.ErrAccountSuspended) ||
		errors.Is(err, types.ErrPleaseLogIn)
}

// Charge la page (fils et messages récents), puis ouvre le socket.
func (p *Pont) connect(s *Session, cli *messagix.Client, pre *loaded) {
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
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.connecting = false
		p.mu.Unlock()
	}()

	emitState("connecting")
	if cli == nil {
		cli = p.newClient(s.Cookies)
	}
	if pre == nil {
		for essai := 0; ; essai++ {
			user, tbl, err := cli.LoadMessagesPage(ctx)
			if err == nil {
				pre = &loaded{user: user, tbl: tbl}
				break
			}
			log.Err(err).Int("essai", essai).Msg("chargement de la page Messenger")
			if ctx.Err() != nil {
				return
			}
			if isAuthError(err) || essai >= 6 || errors.Is(err, httpclient.ErrVersionIDNotFound) {
				emitErr("connect", explain(err))
				if isAuthError(err) {
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
	p.mu.Lock()
	p.cli = cli
	p.me = pre.user.GetFBID()
	p.myAvatar = pre.user.GetAvatarURL()
	s.Name = cmp.Or(pre.user.GetName(), s.Name)
	p.mu.Unlock()
	saveSession(s)
	emit("account", map[string]any{"service": "messenger", "name": s.Name, "id": idStr(p.me), "email": ""})

	p.handleTable(pre.tbl, true)
	if err := cli.Connect(ctx); err != nil {
		emitErr("connect", explain(err))
		emitState("unpaired")
		return
	}
	go p.periodicSave(ctx)
}

// Les cookies ne marchent plus : on garde le reste (cache, appareil E2EE).
func (p *Pont) dropCookies() {
	p.mu.Lock()
	s := p.sess
	p.mu.Unlock()
	if s != nil {
		b := *s
		b.Cookies = &cookies.Cookies{Platform: s.Platform}
		saveSession(&b)
	}
}

// Les cookies changent avec les réponses de Meta : on les garde de temps en temps.
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
	cli, wa := p.cli, p.e2ee
	p.cli, p.e2ee = nil, nil
	p.waDevice = nil
	p.ready = false
	if p.connCancel != nil {
		p.connCancel()
		p.connCancel = nil
	}
	p.mu.Unlock()
	p.e2eeReady.Clear()
	if wa != nil {
		wa.Disconnect()
	}
	if cli != nil {
		cli.SetEventHandler(nil)
		cli.Disconnect()
	}
}

// Recharge tout (page + socket), au plus une fois par deux minutes.
func (p *Pont) fullReconnect() {
	p.mu.Lock()
	s := p.sess
	if s == nil || s.Cookies == nil || time.Since(p.lastFull) < 2*time.Minute {
		p.mu.Unlock()
		return
	}
	p.lastFull = time.Now()
	p.mu.Unlock()
	log.Info().Msg("reconnexion complète")
	p.disconnect()
	p.connect(s, nil, nil)
}

func (p *Pont) onMetaEvent(ctx context.Context, raw any) {
	switch evt := raw.(type) {
	case *table.LSTable:
		p.handleTable(evt, false)
		if cli := p.client(); cli != nil {
			cli.PostHandlePublishResponse(evt)
		}
	case *messagix.ConnectedEvent:
		log.Info().Msg("connecté au socket Messenger")
		p.mu.Lock()
		p.ready = true
		p.mu.Unlock()
		emitState("ready")
		p.emitList()
		go p.connectE2EE()
		go p.threadBackfill()
		go p.myAvatarDownload()
	case *messagix.ReconnectedEvent:
		p.mu.Lock()
		p.ready = true
		p.mu.Unlock()
		emitState("ready")
		if p.waClient() == nil {
			go p.connectE2EE()
		}
	case *messagix.TransientDisconnectEvent:
		log.Warn().Err(evt.Err).Msg("socket Messenger coupé")
		p.mu.Lock()
		p.ready = false
		p.mu.Unlock()
		emitState("reconnecting")
		if errors.Is(evt.Err, messagix.ErrFatalSocketSyncError) {
			go p.fullReconnect()
		}
	case *messagix.PermanentErrorEvent:
		log.Err(evt.Err).Msg("socket Messenger : erreur permanente")
		if dgw.IsUnauthorized(evt.Err) {
			emitErr("listen", errors.New("Messenger a fermé la session : reconnecte-toi"))
			p.dropCookies()
			go p.disconnect()
			emitState("unpaired")
			return
		}
		emitErr("listen", fmt.Errorf("Messenger : %w", evt.Err))
		emitState("reconnecting")
		go func() {
			time.Sleep(30 * time.Second)
			p.fullReconnect()
		}()
	default:
		log.Debug().Type("type", raw).Msg("évènement messagix ignoré")
	}
}

// Récupère les fils plus anciens que la première page (fils normaux puis chiffrés).
func (p *Pont) threadBackfill() {
	cli := p.client()
	if cli == nil {
		return
	}
	var deja bool
	var raw []byte
	if p.db.QueryRowContext(p.ctx, "SELECT v FROM jasette_kv WHERE k='threads_done'").Scan(&raw) == nil {
		deja = string(raw) == "true"
	}
	limite := 15
	if deja {
		limite = 2
	}
	for _, groupe := range []int64{1, 95} {
		if groupe == 95 {
			ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
			err := cli.WaitForInitialThreadRange(ctx, groupe)
			annule()
			if err != nil {
				log.Debug().Err(err).Msg("fils chiffrés : pas de plage initiale")
				continue
			}
		}
		var prev int64
		for lot := 0; lot < limite; lot++ {
			if p.client() != cli {
				return
			}
			gen := cli.ThreadRangeGeneration(groupe)
			ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
			ks, tbl, err := cli.FetchMoreThreads(ctx, groupe)
			annule()
			if err != nil {
				log.Warn().Err(err).Int64("groupe", groupe).Msg("fils plus anciens")
				break
			}
			if tbl == nil {
				break
			}
			p.handleTable(tbl, false)
			if groupe == 95 {
				ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
				_ = cli.WaitForThreadRange(ctx, groupe, gen)
				annule()
			}
			if ks == nil || !ks.HasMoreBefore || ks.MinThreadKey == prev {
				break
			}
			prev = ks.MinThreadKey
			time.Sleep(time.Second)
		}
	}
	p.saveKV("threads_done", true)
	p.emitList()
}

// ---- Les fils ----

// Le fil où ranger une clé Meta (fils hybrides et anciens fils remplacés).
func (p *Pont) canon(k int64) int64 {
	if t, ok := p.threadMap[k]; ok && t != 0 {
		return t
	}
	if t, ok := p.openThread[k]; ok && t != 0 {
		return t
	}
	return k
}

// Le fil d'origine d'un fil chiffré (pour l'historique non chiffré d'avant).
func (p *Pont) openThreadOf(k int64) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	for open, arm := range p.openThread {
		if arm == k && open != k {
			return open
		}
	}
	return 0
}

func (p *Pont) conv(id string) *convState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.convs[id]
}

// À appeler avec p.mu.
func (p *Pont) getOrMakeConv(key int64, tt table.ThreadType) *convState {
	id := idStr(key)
	c := p.convs[id]
	if c == nil {
		c = &convState{Key: key, Type: tt, Folder: "inbox"}
		p.convs[id] = c
	}
	// Un fil chiffré le reste (le fil ouvert d'avant a souvent la même clé).
	if tt != table.UNKNOWN_THREAD_TYPE && (!c.Type.IsWhatsApp() || tt.IsWhatsApp()) {
		c.Type = tt
	}
	return c
}

func (p *Pont) contactName(id int64) string {
	if c := p.contacts[id]; c != nil {
		return c.Name
	}
	return ""
}

func isGroupType(tt table.ThreadType) bool {
	return tt != table.UNKNOWN_THREAD_TYPE && !tt.IsOneToOne()
}

var hiddenFolders = map[string]bool{"hidden": true, "disabled": true, "montage": true, "legacy": true, "blocked": true}

// À appeler avec p.mu.
func (p *Pont) convOut(id string, c *convState) Conversation {
	group := isGroupType(c.Type)
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
	if !group {
		if n := p.contactName(c.Key); n != "" {
			name = n
		}
		if len(members) == 0 && name != "" {
			members = []string{name}
		}
	}
	if name == "" && len(members) > 0 {
		var firsts []string
		for _, m := range c.Members {
			if m == p.me {
				continue
			}
			if ct := p.contacts[m]; ct != nil {
				firsts = append(firsts, cmp.Or(ct.First, ct.Name))
			}
		}
		name = strings.Join(firsts, ", ")
	}
	if name == "" {
		name = "Conversation Messenger"
	}
	preview, fromMe := c.Snippet, c.SnippetFrom == p.me && p.me != 0
	if c.LastText != "" && (c.LastTime >= c.LastActivity || preview == "") {
		preview, fromMe = c.LastText, c.LastFromMe
	}
	t := max(c.LastActivity, c.LastTime)
	unread := !fromMe && t > c.ReadWM && c.ReadWM >= 0 && t > 0
	return Conversation{
		ID:       id,
		Name:     name,
		Preview:  preview,
		FromMe:   fromMe,
		Time:     t,
		Unread:   unread,
		Group:    group,
		Members:  orEmpty(members),
		ReadOnly: c.ReadOnly,
		Archived: c.Folder != "" && c.Folder != "inbox",
		E2EE:     c.Type.IsWhatsApp(),
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
	var keys []string
	for id, c := range p.convs {
		if c.Hidden || hiddenFolders[c.Folder] {
			continue
		}
		co := p.convOut(id, c)
		if co.Archived != archive || co.Time == 0 && co.Preview == "" && !co.E2EE {
			continue
		}
		items = append(items, co)
	}
	p.mu.Unlock()
	slices.SortStableFunc(items, func(a, b Conversation) int { return cmp.Compare(b.Time, a.Time) })
	if len(items) > n {
		items = items[:n]
	}
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

func (p *Pont) addWaiter(conv string) chan struct{} {
	ch := make(chan struct{}, 1)
	p.mu.Lock()
	p.waiters[conv] = append(p.waiters[conv], ch)
	p.mu.Unlock()
	return ch
}

func (p *Pont) removeWaiter(conv string, ch chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.waiters[conv]
	for i, c := range l {
		if c == ch {
			p.waiters[conv] = append(l[:i], l[i+1:]...)
			break
		}
	}
	if len(p.waiters[conv]) == 0 {
		delete(p.waiters, conv)
	}
}

func (p *Pont) signal(conv string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ch := range p.waiters[conv] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

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
	rows := p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
	more := len(rows) >= count
	if len(rows) < count && p.client() != nil {
		oldestT, oldestID := cmd.BeforeT, cmd.Before
		if len(rows) > 0 {
			oldestT, oldestID = rows[0].Time, rows[0].ID
		}
		remoteMore := p.fetchHistory(conv, c, oldestT, oldestID, count-len(rows))
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
	for i := range m.Reactions {
		if n := p.contactName(m.Reactions[i].Actor); n != "" {
			m.Reactions[i].Sender = n
		}
	}
	if m.Attachments == nil {
		m.Attachments = nil
	}
	return m
}

// Demande des messages plus anciens à Meta. Vrai s'il en reste encore.
func (p *Pont) fetchHistory(conv string, c *convState, beforeT int64, beforeID string, count int) bool {
	p.mu.Lock()
	if p.fetching[conv] {
		p.mu.Unlock()
		return false
	}
	p.fetching[conv] = true
	s := p.sess
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.fetching, conv)
		p.mu.Unlock()
	}()
	if c.Type.IsWhatsApp() {
		if s != nil && s.HistoryDevice != nil && s.HistoryDevice.ID != "" {
			more, err := p.fetchSecureHistory(conv, c, beforeT, count)
			if err != nil {
				log.Warn().Err(err).Str("fil", conv).Msg("historique chiffré")
				emitErr("messages", fmt.Errorf("historique chiffré : %w", err))
			}
			if more || err != nil {
				return more
			}
		}
		// L'ancien fil non chiffré, s'il y en a un.
		open := p.openThreadOf(c.Key)
		if open == 0 {
			return false
		}
		return p.fetchFB(conv, open, beforeT, beforeID)
	}
	return p.fetchFB(conv, c.Key, beforeT, beforeID)
}

// Historique d'un fil normal : la réponse arrive par le socket (tables).
func (p *Pont) fetchFB(conv string, key int64, beforeT int64, beforeID string) bool {
	cli := p.client()
	if cli == nil {
		return false
	}
	ch := p.addWaiter(conv)
	defer p.removeWaiter(conv, ch)
	var task socket.Task = &socket.FetchMessagesTask{
		ThreadKey:            key,
		Direction:            0,
		ReferenceTimestampMs: beforeT,
		ReferenceMessageId:   beforeID,
		SyncGroup:            1,
		Cursor:               cli.GetCursor(1),
	}
	if beforeID == "" || strings.HasPrefix(beforeID, "jasette_") || !strings.HasPrefix(beforeID, "mid.") {
		task = &socket.CreateThreadTask{ThreadFBID: key, SyncGroup: 1}
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	tbl, err := cli.ExecuteTasks(ctx, task)
	annule()
	if err != nil {
		log.Warn().Err(err).Str("fil", conv).Msg("historique")
		return false
	}
	if tbl != nil {
		p.handleTable(tbl, false)
	}
	select {
	case <-ch:
		// D'autres morceaux peuvent suivre de près.
		deadline := time.After(1500 * time.Millisecond)
	encore:
		for {
			select {
			case <-ch:
			case <-deadline:
				break encore
			}
		}
	case <-time.After(10 * time.Second):
	case <-p.ctx.Done():
	}
	c := p.conv(conv)
	if c == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return c.MoreBefore == nil || *c.MoreBefore
}

func newOTID() int64 { return methods.GenerateEpochID() }

func tmpOr(tmp string, otid int64) string {
	if tmp != "" {
		return tmp
	}
	return "tmp_" + strconv.FormatInt(otid, 10)
}

// Un message envoyé : on l'annonce tout de suite (Meta ne renvoie pas
// toujours l'écho, et jamais pour les fils chiffrés).
func (p *Pont) echoSent(conv, id, tmp, text string, kind string, atts []Attachment) {
	r := p.getMsg(id)
	if r == nil {
		r = &msgRow{Message: Message{ID: id, Conv: conv, Text: text, FromMe: true, Time: time.Now().UnixMilli(), Kind: kind, Attachments: atts}, SenderID: p.me}
	}
	r.TmpID = tmp
	r.Status = "OUTGOING_COMPLETE"
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

// Envoi commun texte/fichier vers un fil normal ou chiffré.
func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	otid := newOTID()
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
	if c.Type.IsWhatsApp() {
		id, atts, err := p.sendE2EE(cmd.Conv, c, otid, text, path, mime)
		if err != nil {
			fail(err)
			return
		}
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
		p.echoSent(cmd.Conv, id, tmp, text, "e2ee", atts)
		return
	}
	cli := p.client()
	if cli == nil {
		fail(errors.New("Messenger n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	task := &socket.SendMessageTask{
		ThreadId:         c.Key,
		Otid:             otid,
		Source:           table.MESSENGER_INBOX_IN_THREAD,
		InitiatingSource: table.FACEBOOK_INBOX,
		SendType:         table.TEXT,
		SyncGroup:        1,
		Text:             text,
	}
	if c.Folder == "pending" {
		task.Source = table.MESSENGER_INBOX_PENDING_REQUESTS
	}
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		task.TextHasLinks = 1
	}
	var atts []Attachment
	if path != "" {
		id, a, err := p.uploadFB(ctx, cli, c.Key, path, mime)
		if err != nil {
			fail(fmt.Errorf("envoi du fichier : %w", err))
			return
		}
		task.SendType = table.MEDIA
		task.AttachmentFBIds = []int64{id}
		atts = a
	}
	otidStr := strconv.FormatInt(otid, 10)
	p.mu.Lock()
	p.pending[otidStr] = tmp
	p.mu.Unlock()
	readTask := &socket.ThreadMarkReadTask{ThreadId: c.Key, SyncGroup: 1, LastReadWatermarkTs: time.Now().UnixMilli()}
	var resp *table.LSTable
	var err error
	for essai := 0; essai < 4; essai++ {
		if err = cli.WaitUntilCanSendMessages(ctx, 15*time.Second); err == nil {
			resp, err = cli.ExecuteTasks(ctx, task, readTask)
			if err == nil {
				break
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		fail(explain(err))
		return
	}
	msgID := ""
	if resp != nil {
		for _, r := range resp.LSReplaceOptimsiticMessage {
			if r.OfflineThreadingId == otidStr {
				msgID = r.MessageId
			}
		}
		if msgID == "" {
			for _, f := range resp.LSMarkOptimisticMessageFailed {
				fail(fmt.Errorf("Messenger a refusé le message : %s", f.Message))
				return
			}
			for _, f := range resp.LSHandleFailedTask {
				fail(fmt.Errorf("Messenger n'a pas confirmé le message : %s", f.Message))
				return
			}
		}
		p.handleTable(resp, false)
	}
	if msgID == "" {
		fail(errors.New("Messenger n'a pas confirmé l'envoi"))
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	p.echoSent(cmd.Conv, msgID, tmp, text, "messenger", atts)
}

func (p *Pont) markRead(cmd Cmd) {
	c := p.conv(cmd.Conv)
	cli := p.client()
	if c == nil || cli == nil {
		return
	}
	ts := time.Now().UnixMilli()
	if r := p.getMsg(cmd.Msg); r != nil && r.Time > 0 {
		ts = max(ts, r.Time)
	}
	p.mu.Lock()
	c.ReadWM = max(c.ReadWM, ts)
	p.saveConv(cmd.Conv, c)
	p.mu.Unlock()
	var groupe int64 = 1
	if c.Type.IsWhatsApp() {
		groupe = 95
		p.markReadE2EE(cmd.Conv, c, ts)
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if _, err := cli.ExecuteTasks(ctx, &socket.ThreadMarkReadTask{ThreadId: c.Key, LastReadWatermarkTs: ts, SyncGroup: groupe}); err != nil {
		log.Warn().Err(err).Msg("lu")
	}
}

// Nouvelle conversation : un identifiant Facebook (chiffres), sinon une recherche.
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
		p.search(q)
		return
	}
	cli := p.client()
	if cli == nil {
		emitErr("new", errors.New("Messenger n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	tbl, err := cli.ExecuteTasks(ctx, &socket.CreateThreadTask{ThreadFBID: id, SyncGroup: 1})
	if err != nil {
		emitErr("new", fmt.Errorf("nouvelle conversation : %w", explain(err)))
		return
	}
	if tbl != nil {
		p.handleTable(tbl, false)
	}
	// Comme Messenger aujourd'hui : les nouvelles conversations sont chiffrées.
	tt := table.ENCRYPTED_OVER_WA_ONE_TO_ONE
	if err := p.createWhatsAppDM(ctx, cli, id); err != nil {
		log.Warn().Err(err).Msg("fil chiffré")
		tt = table.ONE_TO_ONE
	}
	conv := idStr(id)
	p.mu.Lock()
	c := p.getOrMakeConv(id, tt)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	p.saveConv(conv, c)
	item := p.convOut(conv, c)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{conv})
}

func (p *Pont) createWhatsAppDM(ctx context.Context, cli *messagix.Client, id int64) error {
	resp, err := cli.ExecuteTasks(ctx, &socket.CreateWhatsAppThreadTask{
		WAJID:            id,
		OfflineThreadKey: methods.GenerateEpochID(),
		ThreadType:       table.ENCRYPTED_OVER_WA_ONE_TO_ONE,
		FolderType:       table.INBOX,
		BumpTimestampMS:  time.Now().UnixMilli(),
	})
	if err != nil {
		return err
	}
	if len(resp.LSIssueNewTask) > 0 {
		tasks := make([]socket.Task, len(resp.LSIssueNewTask))
		for i, t := range resp.LSIssueNewTask {
			tasks[i] = t
		}
		if resp, err = cli.ExecuteTasks(ctx, tasks...); err != nil {
			return err
		}
	}
	if resp != nil {
		p.handleTable(resp, false)
	}
	return nil
}

// Recherche de personnes : « search{items:[{id,name,avatar}]} ».
func (p *Pont) search(q string) {
	cli := p.client()
	if cli == nil || q == "" {
		emit("search", map[string]any{"query": q, "items": []any{}})
		return
	}
	task := &socket.SearchUserTask{
		Query: q,
		SupportedTypes: []table.SearchType{
			table.SearchTypeContact, table.SearchTypeGroup, table.SearchTypePage, table.SearchTypeNonContact,
		},
		SurfaceType: 5,
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	resp, err := cli.ExecuteTasks(ctx, task)
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
	for _, r := range resp.LSInsertSearchResult {
		id := r.GetFBID()
		if id == 0 || vus[id] || !r.ThreadType.IsOneToOne() {
			continue
		}
		vus[id] = true
		items = append(items, item{ID: idStr(id), Name: r.DisplayName, Avatar: r.ProfilePicUrl})
	}
	emit("search", map[string]any{"query": q, "items": items})
}

// Oublie le compte : appareil chiffré, cache et session.
func (p *Pont) forgetAccount() {
	p.mu.Lock()
	p.convs = map[string]*convState{}
	p.contacts = map[int64]*contact{}
	p.threadMap = map[int64]int64{}
	p.openThread = map[int64]int64{}
	p.loaded = map[string]int64{}
	p.mu.Unlock()
	if err := p.wipeStore(); err != nil {
		log.Warn().Err(err).Msg("effacement du cache")
	}
	if p.waStore != nil {
		devs, _ := p.waStore.GetAllDevices(context.WithoutCancel(p.ctx))
		for _, d := range devs {
			_ = d.Delete(context.WithoutCancel(p.ctx))
		}
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
