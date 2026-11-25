package main

// Le cœur du pont : la connexion à X, la boîte de réception XChat, le socket
// en direct, l'ancienne API REST (demandes de messages), et les commandes de
// l'interface (liste, messages, envoi, lu, nouvelle conversation, recherche).
//
// Les identifiants des fils sont ceux de XChat : « a:b » pour un 1-à-1 (les
// deux identifiants triés), « g123 » pour un groupe. L'API REST les écrit
// « a-b » et « 123 » : restID() et fromREST() font la conversion.

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

	"github.com/google/uuid"

	"go.mau.fi/mautrix-twitter/pkg/twittermeow"
	xcookies "go.mau.fi/mautrix-twitter/pkg/twittermeow/cookies"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/crypto"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/data/payload"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/data/response"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/data/types"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/methods"
)

type Pont struct {
	ctx context.Context
	db  *sql.DB
	ks  *keyStore

	mu         sync.Mutex
	sess       *Session
	me         string
	cli        *twittermeow.Client
	connCancel context.CancelFunc
	loggingIn  bool
	ready      bool
	// Premier import de la boîte (rien en cache) : pas d'annonce « message »
	// pour chacun des vieux messages.
	silent bool
	// Le plus grand numéro de séquence XChat traité (reprise après coupure).
	maxSeq string

	convs    map[string]*convState
	contacts map[string]*contact
	// Fils ouverts dans l'interface : le plus vieux message montré (ms).
	loaded map[string]int64
	// Envois en cours : identifiant client (UUID) -> tmp_id de l'interface.
	pending map[string]string
	typing  map[string]*time.Timer

	listDirty     bool
	listCount     int
	archiveWanted bool
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:       ctx,
		convs:     map[string]*convState{},
		contacts:  map[string]*contact{},
		loaded:    map[string]int64{},
		pending:   map[string]string{},
		typing:    map[string]*time.Timer{},
		listCount: 50,
	}
	p.ks = &keyStore{p: p}
	go p.listLoop()
	return p
}

func (p *Pont) client() *twittermeow.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cli
}

func (p *Pont) myID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.me
}

// Au démarrage : la session gardée, s'il y en a une.
func (p *Pont) start() {
	s := loadSession()
	if s == nil {
		if old := loadSessionAny(); old != nil && old.Cookies != "" && !old.hasKeys() {
			emitErr("login", errors.New("X Chat demande ton code d'accès : reconnecte-toi"))
		}
		emitState("unpaired")
		return
	}
	p.mu.Lock()
	p.sess = s
	p.me = s.UserID
	p.mu.Unlock()
	p.emitAccount(s)
	go p.connect(s)
}

func (p *Pont) emitAccount(s *Session) {
	name := cmp.Or(s.Name, s.Username, s.UserID)
	if s.Username != "" && s.Name != "" {
		name = fmt.Sprintf("%s (@%s)", s.Name, s.Username)
	}
	emit("account", map[string]any{"service": serviceName(), "name": name, "id": s.UserID})
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Se connecte avec une session (cookies + clés), puis reste connecté.
func (p *Pont) connect(s *Session) {
	p.disconnect()
	ctx, cancel := context.WithCancel(p.ctx)
	p.mu.Lock()
	p.connCancel = cancel
	p.mu.Unlock()
	emitState("connecting")

	p.ks.mu.Lock()
	p.ks.sess = s
	p.ks.mu.Unlock()
	cli := twittermeow.NewClient(xcookies.NewCookiesFromString(s.Cookies), p.ks, log.With().Str("c", "twittermeow").Logger())
	cli.SetCurrentUserID(s.UserID)

	// La page des messages de x.com : elle valide les cookies et prépare les jetons.
	delay := 2 * time.Second
	for {
		prof, err := cli.LoadMessagesPage(ctx)
		if err == nil {
			s.Name = cmp.Or(prof.Name, s.Name)
			s.Username = cmp.Or(prof.ScreenName, s.Username)
			s.Avatar = cmp.Or(prof.AvatarURL, s.Avatar)
			break
		}
		if ctx.Err() != nil {
			return
		}
		if isAuthError(err) {
			emitErr("login", explain(err))
			p.dropCookies()
			emitState("unpaired")
			return
		}
		log.Warn().Err(err).Dur("dans", delay).Msg("connexion : nouvel essai")
		emitState("reconnecting")
		if !sleepCtx(ctx, delay) {
			return
		}
		delay = min(delay*2, 5*time.Minute)
	}
	s.Cookies = cli.GetCookieString()
	saveSession(s)
	p.mu.Lock()
	p.cli = cli
	p.mu.Unlock()
	p.emitAccount(s)
	p.upsertUser(s.UserID, s.Name, s.Username, s.Avatar)
	go p.myAvatarDownload()

	cli.SetXChatEventHandler(func(ctx context.Context, evt types.TwitterEvent) bool {
		p.onEvent(ctx, evt, false)
		// Rendre faux couperait le socket : on garde toujours la connexion.
		return true
	})
	cli.SetConversationDataCallback(func(ctx context.Context, convID string, item *response.XChatInboxItem) {
		if item != nil {
			p.upsertItem(ctx, item)
		}
	})
	cli.SetEventHandler(p.onPolling, nil, func(context.Context) {
		p.saveKV("polling_cursor", cli.GetSession().PollingCursor)
	})

	proc := cli.GetXChatProcessor()
	proc.ResetSequenceState()
	var seq string
	p.loadKV("max_seq", &seq)
	p.mu.Lock()
	p.maxSeq = seq
	p.silent = seq == "" && len(p.convs) == 0
	p.mu.Unlock()

	// L'import de la boîte XChat (complet la première fois, ensuite seulement
	// ce qui a changé depuis le dernier numéro de séquence).
	delay = 2 * time.Second
	for {
		err := p.syncInbox(ctx, cli)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return
		}
		if isAuthError(err) {
			emitErr("login", explain(err))
			p.dropCookies()
			emitState("unpaired")
			return
		}
		log.Warn().Err(err).Dur("dans", delay).Msg("boîte XChat : nouvel essai")
		emitErr("sync", fmt.Errorf("boîte de réception X : %w", explain(err)))
		emitState("reconnecting")
		if !sleepCtx(ctx, delay) {
			return
		}
		delay = min(delay*2, 5*time.Minute)
	}
	p.mu.Lock()
	p.silent = false
	p.ready = true
	p.mu.Unlock()
	emitState("ready")
	p.emitList()

	// Le socket XChat : à chaque (re)connexion, on rattrape d'abord ce qui a
	// été manqué, puis les nouvelles arrivent en direct.
	cli.SetXChatConnectHandler(func(ctx context.Context, drain twittermeow.XChatLiveDrain) error {
		if err := p.syncInbox(ctx, cli); err != nil {
			log.Warn().Err(err).Msg("rattrapage XChat")
		}
		return drain(nil)
	})
	go func() {
		delay := 5 * time.Second
		for ctx.Err() == nil {
			err := cli.StartXChatWebsocket(ctx)
			if err == nil {
				log.Info().Msg("socket XChat connecté")
				return
			}
			if ctx.Err() != nil {
				return
			}
			log.Warn().Err(err).Dur("dans", delay).Msg("socket XChat : nouvel essai")
			if isAuthError(err) {
				emitErr("login", explain(err))
				return
			}
			emitState("reconnecting")
			if !sleepCtx(ctx, delay) {
				return
			}
			emitState("ready")
			delay = min(delay*2, 5*time.Minute)
		}
	}()

	// Les demandes de messages (et vieux groupes) : l'ancienne API REST.
	go func() {
		if err := p.syncRequests(ctx, cli); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("demandes de messages")
		}
		if ctx.Err() == nil {
			cli.StartPolling(ctx)
		}
	}()
}

// Les cookies ne marchent plus : on les oublie (les clés restent, mais une
// reconnexion redemande le code d'accès).
func (p *Pont) dropCookies() {
	p.disconnect()
	p.mu.Lock()
	s := p.sess
	p.mu.Unlock()
	if s != nil {
		s.Cookies = ""
		saveSession(s)
	}
}

func (p *Pont) disconnect() {
	p.mu.Lock()
	cancel, cli := p.connCancel, p.cli
	p.connCancel, p.cli, p.ready = nil, nil, false
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cli != nil {
		cli.Disconnect()
	}
}

// ---- La boîte de réception XChat ----

func (p *Pont) noteSeq(seq string) {
	if seq == "" {
		return
	}
	p.mu.Lock()
	if methods.CompareSnowflake(seq, p.maxSeq) <= 0 {
		p.mu.Unlock()
		return
	}
	p.maxSeq = seq
	p.mu.Unlock()
	p.saveKV("max_seq", seq)
}

// Le curseur de la page suivante (nil : c'était la dernière).
func nextInboxCursor(page *response.XChatInboxPage) *payload.XChatCursor {
	if c := page.MessageEventsCursor; c != nil {
		if c.PullFinished || c.MaxLocalSequenceID == "" {
			return nil
		}
		return &payload.XChatCursor{MaxLocalSequenceID: c.MaxLocalSequenceID}
	}
	if page.InboxCursor.PullFinished || page.InboxCursor.CursorID == "" {
		return nil
	}
	return &payload.XChatCursor{CursorId: page.InboxCursor.CursorID, GraphSnapshotId: page.InboxCursor.GraphSnapshotID}
}

// Importe la boîte XChat depuis le dernier numéro de séquence connu.
func (p *Pont) syncInbox(ctx context.Context, cli *twittermeow.Client) error {
	proc := cli.GetXChatProcessor()
	// Les pages mêlent plusieurs fils, pas en ordre de séquence : on ne garde
	// le nouveau numéro qu'à la fin, une fois tout importé.
	proc.SetSequenceIDCallback(nil)
	defer proc.SetSequenceIDCallback(p.noteSeq)

	p.mu.Lock()
	seq := p.maxSeq
	p.mu.Unlock()
	resp, err := cli.GetInitialXChatPage(ctx, payload.NewInitialXChatPageQueryVariables(seq))
	if err != nil {
		return err
	}
	page := resp.Data.GetInboxPage
	best := seq
	var last *payload.XChatCursor
	for n := 0; ; n++ {
		if page.MaxUserSequenceID != nil && methods.CompareSnowflake(*page.MaxUserSequenceID, best) > 0 {
			best = *page.MaxUserSequenceID
		}
		if err := p.processInboxPage(ctx, cli, &page); err != nil {
			return err
		}
		next := nextInboxCursor(&page)
		if next == nil || n >= 500 || (last != nil && *last == *next) {
			break
		}
		last = next
		r, err := cli.GetInboxPageRequest(ctx, payload.NewInboxPageRequestQueryVariables(next))
		if err != nil {
			return err
		}
		page = r.Data.GetInboxPage
	}
	if h := proc.MaxHandledSequenceID(); methods.CompareSnowflake(h, best) > 0 {
		best = h
	}
	p.noteSeq(best)
	p.markDirty()
	return nil
}

func (p *Pont) processInboxPage(ctx context.Context, cli *twittermeow.Client, page *response.XChatInboxPage) error {
	proc := cli.GetXChatProcessor()
	if page.MessageEventsCursor != nil {
		// Page incrémentale : seulement des événements.
		return proc.ProcessEncodedMessageEvents(ctx, page.EncodedMessageEvents)
	}
	// Les personnes sans fiche dans la page : demandées d'un coup.
	var missing []string
	p.mu.Lock()
	for i := range page.Items {
		d := &page.Items[i].ConversationDetail
		for _, l := range [][]response.XChatUserResult{d.ParticipantsResults, d.GroupMembersResults} {
			for _, u := range l {
				id := u.RestID
				if u.Result != nil {
					id = cmp.Or(id, u.Result.RestID)
				}
				if id != "" && u.Result == nil && p.contacts[id] == nil && !slices.Contains(missing, id) {
					missing = append(missing, id)
				}
			}
		}
	}
	p.mu.Unlock()
	if len(missing) > 0 {
		p.fetchUsers(ctx, cli, missing)
	}
	for i := range page.Items {
		item := &page.Items[i]
		if item.ConversationUnavailable || item.ConversationDetail.ConversationID == "" {
			continue
		}
		conv := item.ConversationDetail.ConversationID
		if err := proc.ProcessKeyChangeEvents(ctx, item); err != nil {
			log.Warn().Err(err).Str("fil", conv).Msg("clés du fil")
		}
		p.upsertItem(ctx, item)
		// Un temps nul ferait ignorer les messages : « depuis le début ».
		if err := proc.ProcessMessageAndReadEvents(ctx, item, time.UnixMilli(1), ""); err != nil {
			if errors.Is(err, twittermeow.ErrXChatFailurePersistence) {
				return err
			}
			log.Warn().Err(err).Str("fil", conv).Msg("messages du fil")
		}
	}
	return nil
}

// Les fiches de personnes, par paquets.
func (p *Pont) fetchUsers(ctx context.Context, cli *twittermeow.Client, ids []string) {
	for len(ids) > 0 {
		n := min(len(ids), 50)
		resp, err := cli.GetUsersByIdsForXChat(ctx, payload.NewGetUsersByIdsForXChatVariables(ids[:n]))
		ids = ids[n:]
		if err != nil {
			log.Debug().Err(err).Msg("fiches de personnes")
			continue
		}
		for _, r := range resp.Data.GetMemberResults.Results {
			if r.MemberResults != nil {
				p.upsertXUser(r.MemberResults)
			}
		}
	}
}

func (p *Pont) upsertXUser(ur *response.XChatUserResult) string {
	if ur == nil {
		return ""
	}
	id := ur.RestID
	if ur.Result == nil {
		return id
	}
	id = cmp.Or(id, ur.Result.RestID)
	if u := twittermeow.ConvertXChatUserToUser(ur.Result); u != nil {
		p.upsertUser(id, u.Name, u.ScreenName, cmp.Or(u.ProfileImageURLHTTPS, u.ProfileImageURL))
	}
	return id
}

func (p *Pont) upsertUser(id, name, username, avatar string) {
	if id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.contacts[id]
	if c == nil {
		c = &contact{}
		p.contacts[id] = c
	}
	n := contact{Name: cmp.Or(name, c.Name), Username: cmp.Or(username, c.Username), Avatar: cmp.Or(avatar, c.Avatar)}
	if n == *c {
		return
	}
	*c = n
	p.listDirty = true
	p.saveContact(id, c)
}

// Le fil (créé au besoin). À appeler avec p.mu.
func (p *Pont) getOrMakeConv(id string) *convState {
	c := p.convs[id]
	if c == nil {
		c = &convState{ID: id, Group: strings.HasPrefix(id, "g")}
		if !c.Group {
			c.Members = strings.Split(id, ":")
		}
		p.convs[id] = c
	}
	return c
}

func (p *Pont) conv(id string) *convState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.convs[id]
}

// Le fil vit dans XChat (on en a une clé ou un jeton).
func (p *Pont) markXChat(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.convs[id]; c != nil && !c.XChat {
		c.XChat = true
		p.saveConv(id, c)
	}
}

// Les infos d'un fil de la boîte XChat : membres, nom et image du groupe.
func (p *Pont) upsertItem(ctx context.Context, item *response.XChatInboxItem) {
	d := &item.ConversationDetail
	id := d.ConversationID
	if id == "" {
		return
	}
	group := strings.HasPrefix(id, "g") || d.GroupMetadata != nil || len(d.GroupMembersResults) > 0 || len(d.GroupAdminsResults) > 0
	var members []string
	src := d.ParticipantsResults
	if len(d.GroupMembersResults) > 0 {
		src = d.GroupMembersResults
	}
	for i := range src {
		if m := p.upsertXUser(&src[i]); m != "" && !slices.Contains(members, m) {
			members = append(members, m)
		}
	}
	var name, pic string
	var updated int64
	if gm := d.GroupMetadata; gm != nil {
		name = p.decryptGroupName(ctx, id, gm.GroupName)
		pic = gm.GroupAvatarURL
		updated = methods.ParseMsecTimestamp(gm.UpdatedAtMsec).UnixMilli()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.getOrMakeConv(id)
	c.Group = c.Group || group
	c.XChat = true
	c.Muted = d.IsMuted
	c.Hidden = false
	if len(members) > 0 {
		c.Members = members
	}
	if name != "" {
		c.Name = name
	}
	if pic != "" && pic != c.Picture {
		c.Picture = pic
		c.PicKey = ""
	}
	if updated > 0 && c.LastActivity == 0 {
		c.LastActivity = updated
	}
	p.listDirty = true
	p.saveConv(id, c)
}

// Les noms de groupe XChat sont chiffrés avec la clé du fil (« version:base64 »).
func (p *Pont) decryptGroupName(ctx context.Context, conv, enc string) string {
	if enc == "" {
		return ""
	}
	ver, data := "", enc
	if i := strings.IndexByte(enc, ':'); i > 0 {
		ver, data = enc[:i], enc[i+1:]
	}
	ct, err := decodeB64(data)
	looksEncrypted := err == nil && len(ct) >= 40
	if !looksEncrypted {
		return enc
	}
	var key *crypto.ConversationKey
	if ver != "" {
		key, _ = p.ks.GetConversationKey(ctx, conv, ver)
	}
	if key == nil {
		key, _ = p.ks.GetLatestConversationKey(ctx, conv)
	}
	if key == nil {
		return ""
	}
	pt, err := crypto.SecretboxDecrypt(ct, key.Key)
	if err != nil {
		log.Debug().Err(err).Str("fil", conv).Msg("nom du groupe")
		return ""
	}
	return string(pt)
}

// ---- L'ancienne API REST : demandes de messages, vieux groupes ----

// « a:b » -> « a-b », « g123 » -> « 123 ».
func restID(conv string) string {
	return strings.ReplaceAll(strings.TrimPrefix(conv, "g"), ":", "-")
}

// « a-b » -> « a:b », « 123 » -> « g123 ».
func fromREST(id string) string {
	if id == "" || strings.HasPrefix(id, "g") {
		return id
	}
	if strings.Contains(id, "-") || strings.Contains(id, ":") {
		return strings.ReplaceAll(id, "-", ":")
	}
	return "g" + id
}

func (p *Pont) syncRequests(ctx context.Context, cli *twittermeow.Client) error {
	q := payload.DMRequestQuery{}.Default()
	q.FilterLowQuality = false
	resp, err := cli.GetInitialInboxState(ctx, &q)
	if err != nil {
		return err
	}
	inbox := resp.InboxInitialState
	if inbox == nil {
		return nil
	}
	var cursor string
	if p.loadKV("polling_cursor", &cursor) && cursor != "" {
		cli.GetSession().PollingCursor = cursor
	} else if inbox.Cursor != "" {
		cli.GetSession().PollingCursor = inbox.Cursor
		p.saveKV("polling_cursor", inbox.Cursor)
	}
	p.processRESTInbox(inbox, true)
	return nil
}

// Les fils et personnes d'une réponse REST ; avec « msgs », ses messages aussi.
func (p *Pont) processRESTInbox(inbox *response.TwitterInboxData, msgs bool) {
	for id, u := range inbox.Users {
		if u != nil {
			p.upsertUser(cmp.Or(u.IDStr, id), u.Name, u.ScreenName, cmp.Or(u.ProfileImageURLHTTPS, u.ProfileImageURL))
		}
	}
	skip := map[string]bool{}
	for rid, rc := range inbox.Conversations {
		if rc == nil {
			continue
		}
		id := fromREST(cmp.Or(rc.ConversationID, rid))
		p.mu.Lock()
		c := p.convs[id]
		// Un fil XChat connu : XChat fait foi (pas de doublons).
		if c != nil && c.XChat {
			skip[id] = true
			p.mu.Unlock()
			continue
		}
		c = p.getOrMakeConv(id)
		c.Group = rc.Type == types.ConversationTypeGroupDM
		c.Request = !rc.Trusted
		c.ReadOnly = rc.ReadOnly
		c.Muted = rc.Muted
		if rc.Name != "" {
			c.Name = rc.Name
		}
		if rc.AvatarImageHttps != "" {
			c.Picture = rc.AvatarImageHttps
		}
		if len(rc.Participants) > 0 {
			c.Members = c.Members[:0]
			for _, m := range rc.Participants {
				c.Members = append(c.Members, m.UserID)
			}
		}
		if ts := methods.ParseMsecTimestamp(rc.SortTimestamp).UnixMilli(); ts > c.LastActivity {
			c.LastActivity = ts
		}
		if rc.LastReadEventID != "" {
			c.ReadWM = max(c.ReadWM, methods.ParseSnowflake(rc.LastReadEventID).UnixMilli())
		}
		p.listDirty = true
		p.saveConv(id, c)
		p.mu.Unlock()
	}
	if !msgs {
		return
	}
	for _, e := range inbox.Entries {
		evt := e.ParseWithErrorLog(&log)
		if m, ok := evt.(*types.Message); ok {
			// Au démarrage : rangés sans annonce (ce ne sont pas des nouveautés).
			conv := fromREST(m.ConversationID)
			if skip[conv] {
				continue
			}
			if r := p.xRow(conv, m); r != nil {
				p.ensureConv(conv)
				p.storeRow(r)
			}
		} else if evt != nil {
			p.onEvent(p.ctx, evt, true)
		}
	}
}

// Les nouvelles de l'API REST (interrogée toutes les quelques secondes).
func (p *Pont) onPolling(evt types.TwitterEvent, inbox *response.TwitterInboxData) bool {
	if inbox != nil {
		p.processRESTInbox(inbox, false)
	}
	if evt == nil {
		return true
	}
	if pe, ok := evt.(*types.PollingError); ok {
		if pe.IsAuth {
			log.Warn().Err(pe.Error).Msg("X a refusé la session")
			emitErr("login", explain(pe.Error))
			go func() {
				p.dropCookies()
				emitState("unpaired")
			}()
			return false
		}
		if pe.Error != nil {
			log.Debug().Err(pe.Error).Msg("interrogation REST")
		}
		return true
	}
	p.onEvent(p.ctx, evt, true)
	return true
}

// ---- La liste des fils ----

func (p *Pont) contactName(id string) string {
	c := p.contacts[id]
	if c == nil {
		return ""
	}
	if c.Name != "" {
		return c.Name
	}
	if c.Username != "" {
		return "@" + c.Username
	}
	return ""
}

// L'autre personne d'un 1-à-1. À appeler avec p.mu.
func (p *Pont) otherOf(c *convState) string {
	for _, m := range c.Members {
		if m != p.me {
			return m
		}
	}
	for _, m := range strings.Split(c.ID, ":") {
		if m != p.me {
			return m
		}
	}
	return ""
}

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
		if n := p.contactName(p.otherOf(c)); n != "" {
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
		name = "Conversation X"
	}
	if c.Request {
		name += " (demande)"
	}
	t := max(c.LastActivity, c.LastTime)
	unread := !c.LastFromMe && c.LastTime > 0 && t > c.ReadWM
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
		E2EE:     c.XChat,
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

// X n'a pas de dossier d'archives : la liste « archive » est toujours vide.
func (p *Pont) sendList(n int, archive, avatars bool) {
	items := []Conversation{}
	if !archive {
		p.mu.Lock()
		for id, c := range p.convs {
			if c.Hidden {
				continue
			}
			co := p.convOut(id, c)
			if co.Time == 0 && co.Preview == "" {
				continue
			}
			items = append(items, co)
		}
		p.mu.Unlock()
	}
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
	if avatars && len(keys) > 0 {
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
	// Première ouverture du fil depuis le démarrage : la page la plus récente.
	p.mu.Lock()
	_, dejaVu := p.loaded[conv]
	p.mu.Unlock()
	if cmd.Before == "" && !dejaVu {
		if _, err := p.fetchPage(conv, ""); err != nil {
			log.Warn().Err(err).Str("fil", conv).Msg("messages récents")
		}
	}
	rows := p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
	more := len(rows) >= count
	p.mu.Lock()
	noMore := c.NoMore
	p.mu.Unlock()
	if len(rows) < count && !noMore && p.client() != nil {
		// Le plus vieux message connu du serveur : l'historique part de là.
		anchor := ""
		if o := p.oldestServerMsg(conv); o != nil {
			anchor = o.ID
		}
		for tries := 0; tries < 3 && len(rows) < count; tries++ {
			n, err := p.fetchPage(conv, anchor)
			if err != nil {
				log.Warn().Err(err).Str("fil", conv).Msg("historique")
				break
			}
			rows = p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
			p.mu.Lock()
			noMore = c.NoMore
			p.mu.Unlock()
			if noMore || n == "" || n == anchor {
				break
			}
			anchor = n
		}
		more = len(rows) >= count || (!noMore && len(rows) > 0)
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
	if r.SenderID != "" && !m.FromMe {
		if n := p.contactName(r.SenderID); n != "" {
			m.Sender = n
		}
	}
	m.Reactions = slices.Clone(m.Reactions)
	for i := range m.Reactions {
		if n := p.contactName(strconv.FormatInt(m.Reactions[i].Actor, 10)); n != "" {
			m.Reactions[i].Sender = n
		}
	}
	return m
}

// Une page d'historique avant le message « before » (vide : la plus récente).
// Rend le repère pour la page suivante.
func (p *Pont) fetchPage(conv, before string) (string, error) {
	cli := p.client()
	if cli == nil {
		return "", nil
	}
	p.mu.Lock()
	c := p.convs[conv]
	xchat := c != nil && (c.XChat || !c.Request)
	p.mu.Unlock()
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	defer annule()
	if xchat {
		next, err := p.fetchXChatPage(ctx, cli, conv, before)
		if err == nil {
			return next, nil
		}
		if c != nil && c.XChat {
			return "", err
		}
		log.Debug().Err(err).Str("fil", conv).Msg("historique XChat, essai par l'API REST")
	}
	return p.fetchRESTPage(ctx, cli, conv, before)
}

const maxSnowflake = "9223372036854775807"

func (p *Pont) fetchXChatPage(ctx context.Context, cli *twittermeow.Client, conv, before string) (string, error) {
	settings := payload.DefaultGetConversationPageQuerySettings()
	settings.ConversationEventLimit = 50
	minKey := maxSnowflake
	if k, err := p.ks.GetLatestConversationKey(ctx, conv); err == nil && k.KeyVersion != "" {
		minKey = k.KeyVersion
	}
	resp, err := cli.GetConversationPage(ctx, payload.NewGetConversationPageQueryVariables(conv, cmp.Or(before, maxSnowflake), minKey, settings))
	if err != nil {
		return "", err
	}
	page := resp.Data.GetConversationPage
	// D'abord les clés manquantes, pour pouvoir tout déchiffrer.
	for _, enc := range page.MissingConversationKeyChangeEvents {
		if evt, err := twittermeow.DecodeMessageEvent(enc); err == nil && evt.Detail != nil && evt.Detail.ConversationKeyChangeEvent != nil {
			if err := p.storeKeyChange(ctx, cli, conv, evt); err != nil {
				log.Debug().Err(err).Msg("clé du fil")
			}
		}
	}
	var events []*payload.MessageEvent
	next := ""
	for _, enc := range page.EncodedMessageEvents {
		evt, err := twittermeow.DecodeMessageEvent(enc)
		if err != nil || evt.Detail == nil {
			continue
		}
		if seq := ptrVal(evt.SequenceId); seq != "" && (next == "" || methods.CompareSnowflake(seq, next) < 0) {
			next = seq
		}
		if evt.Detail.ConversationKeyChangeEvent != nil {
			if err := p.storeKeyChange(ctx, cli, conv, evt); err != nil {
				log.Debug().Err(err).Msg("clé du fil")
			}
			continue
		}
		events = append(events, evt)
	}
	for _, evt := range events {
		if evt.Detail.MessageCreateEvent == nil {
			continue
		}
		r := p.decodeHistoryMessage(ctx, conv, evt)
		if r == nil {
			continue
		}
		if p.storeRow(r) {
			p.emitMsg(r, false)
		}
	}
	if !page.HasMore || len(page.EncodedMessageEvents) == 0 {
		p.mu.Lock()
		if c := p.convs[conv]; c != nil && before != "" {
			c.NoMore = true
			p.saveConv(conv, c)
		}
		p.mu.Unlock()
	}
	return next, nil
}

func ptrVal[T any](v *T) T {
	if v == nil {
		var zero T
		return zero
	}
	return *v
}

// Une clé de fil venue de l'historique : déchiffrée avec la clé secrète du compte.
func (p *Pont) storeKeyChange(ctx context.Context, cli *twittermeow.Client, conv string, evt *payload.MessageEvent) error {
	ck := evt.Detail.ConversationKeyChangeEvent
	version := ptrVal(ck.ConversationKeyVersion)
	if version == "" {
		return errors.New("version de clé absente")
	}
	own, err := p.ks.GetOwnSigningKey(ctx)
	if err != nil {
		return err
	}
	me := cli.GetCurrentUserID()
	var mine string
	for _, pk := range ck.ConversationParticipantKeys {
		if ptrVal(pk.UserId) == me {
			mine = ptrVal(pk.EncryptedConversationKey)
			break
		}
	}
	if mine == "" {
		return nil
	}
	key, err := crypto.UnwrapConversationKey(mine, own.DecryptKeyB64)
	if errors.Is(err, crypto.ErrKeyNotForUs) {
		return nil
	} else if err != nil {
		return err
	}
	created := methods.ParseMsecTimestamp(ptrVal(evt.CreatedAtMsec))
	if created.IsZero() {
		created = time.Now()
	}
	return p.ks.PutConversationKey(ctx, &crypto.ConversationKey{ConversationID: cmp.Or(ptrVal(evt.ConversationId), conv), KeyVersion: version, Key: key, CreatedAt: created})
}

// Un message de l'historique XChat, déchiffré.
func (p *Pont) decodeHistoryMessage(ctx context.Context, conv string, evt *payload.MessageEvent) *msgRow {
	mce := evt.Detail.MessageCreateEvent
	keyVersion := ptrVal(mce.ConversationKeyVersion)
	seq := ptrVal(evt.SequenceId)
	if ptrVal(mce.ContentsEncryptionVersion) != 0 {
		if keyVersion != "" && mce.IsUserMetadata() {
			return nil
		}
		return p.unsupportedRow(conv, seq, ptrVal(evt.SenderId), ptrVal(evt.CreatedAtMsec))
	}
	if len(mce.Contents) == 0 {
		return nil
	}
	var entry *payload.MessageEntryContents
	if keyVersion != "" {
		if mce.IsUserMetadata() {
			return nil
		}
		key, err := p.ks.GetConversationKey(ctx, conv, keyVersion)
		if err != nil {
			return p.undecryptableRow(conv, seq, ptrVal(evt.SenderId), ptrVal(evt.CreatedAtMsec))
		}
		dl := log.With().Str("fil", conv).Logger()
		entry, err = crypto.DecryptMessageEntryContentsBytesDebug(mce.Contents, key.Key, &dl)
		if err != nil {
			return p.undecryptableRow(conv, seq, ptrVal(evt.SenderId), ptrVal(evt.CreatedAtMsec))
		}
	} else {
		var err error
		entry, err = crypto.ParseMessageEntryContentsBytes(mce.Contents)
		if err != nil {
			return nil
		}
	}
	if entry == nil || entry.Message == nil {
		return nil
	}
	if entry.Message.MessageText == nil && len(entry.Message.Attachments) == 0 && !twittermeow.HasForwardedText(entry.Message.ForwardedMessage) {
		return nil
	}
	msg := twittermeow.ConvertXChatMessageContentsToMessage(evt, entry.Message, keyVersion)
	if msg == nil {
		return nil
	}
	return p.xRow(conv, msg)
}

// L'historique d'un fil qui n'existe que dans l'API REST.
func (p *Pont) fetchRESTPage(ctx context.Context, cli *twittermeow.Client, conv, before string) (string, error) {
	q := payload.DMRequestQuery{}.Default()
	q.Count = 50
	if before != "" {
		q.MaxID = before
	}
	resp, err := cli.FetchConversationContext(ctx, restID(conv), &q, payload.CONTEXT_FETCH_DM_CONVERSATION_HISTORY)
	if err != nil {
		var te twittermeow.TwitterError
		if errors.As(err, &te) && te.Code == 279 {
			p.mu.Lock()
			if c := p.convs[conv]; c != nil {
				c.NoMore = true
				p.saveConv(conv, c)
			}
			p.mu.Unlock()
			return "", nil
		}
		return "", err
	}
	inbox := resp.ConversationTimeline
	if inbox == nil {
		return "", nil
	}
	p.processRESTInbox(inbox, false)
	next := ""
	for _, e := range inbox.Entries {
		m, ok := e.ParseWithErrorLog(&log).(*types.Message)
		if !ok {
			continue
		}
		if next == "" || methods.CompareSnowflake(m.ID, next) < 0 {
			next = m.ID
		}
		r := p.xRow(conv, m)
		if r != nil && p.storeRow(r) {
			p.emitMsg(r, false)
		}
	}
	if inbox.Status == types.PaginationStatusAtEnd || next == "" {
		p.mu.Lock()
		if c := p.convs[conv]; c != nil && before != "" {
			c.NoMore = true
			p.saveConv(conv, c)
		}
		p.mu.Unlock()
	}
	return next, nil
}

// ---- L'envoi ----

func (p *Pont) echoSent(conv, id, tmp, text string, ts int64, atts []Attachment) {
	r := p.getMsg(id)
	if r == nil {
		r = &msgRow{Message: Message{ID: id, Conv: conv, Text: text, FromMe: true, Time: ts, Kind: "x", Attachments: atts}, SenderID: p.myID()}
		r.Media = mediaSummary(atts)
	} else if len(atts) > 0 {
		// La copie locale de la pièce jointe envoyée reste la bonne.
		r.Attachments, r.Media = atts, mediaSummary(atts)
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

// La façon d'envoyer dans un fil (comme selectMessageSendMode du pont officiel).
type sendMode int

const (
	sendREST sendMode = iota
	sendPlain
	sendEncrypted
)

func (p *Pont) modeOf(conv string) sendMode {
	switch {
	case p.hasConvKey(conv):
		return sendEncrypted
	case p.hasConvToken(conv):
		return sendPlain
	}
	return sendREST
}

func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	msgID := uuid.NewString()
	tmp := cmp.Or(cmd.TmpID, "tmp_"+msgID)
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
		fail(errors.New("X n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	mode := p.modeOf(cmd.Conv)
	opts := twittermeow.SendEncryptedMessageOpts{ConversationID: cmd.Conv, MessageID: msgID, Text: text}
	var atts []Attachment
	restMedia := ""
	if path != "" {
		data, mime, err := readMedia(path, mime)
		if err != nil {
			fail(fmt.Errorf("fichier illisible : %w", err))
			return
		}
		atts = localAttachment(path, mime, data)
		att, mediaID, err := p.uploadMedia(ctx, cli, cmd.Conv, msgID, mode, filepathBase(path), mime, data, atts)
		if err != nil {
			fail(fmt.Errorf("envoi du fichier : %w", explain(err)))
			return
		}
		if att != nil {
			opts.Attachments = append(opts.Attachments, att)
		}
		restMedia = mediaID
	}
	p.mu.Lock()
	p.pending[msgID] = tmp
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, msgID)
		p.mu.Unlock()
	}()

	var sentID string
	var err error
	if mode != sendREST {
		var resp *response.SendMessageMutationResponse
		if mode == sendPlain {
			resp, err = cli.SendUnencryptedMessage(ctx, opts)
		} else {
			resp, err = cli.SendEncryptedMessage(ctx, opts)
		}
		if err == nil {
			sentID, err = sentSequence(resp)
		} else if errors.Is(err, crypto.ErrKeyNotFound) && path == "" {
			// Pas de clé finalement : l'ancienne API, comme le pont officiel.
			mode = sendREST
			err = nil
		}
	}
	if mode == sendREST && err == nil {
		sentID, err = p.sendREST(ctx, cli, cmd.Conv, msgID, text, restMedia)
	}
	if err != nil {
		fail(explain(err))
		return
	}
	if sentID == "" {
		sentID = msgID
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	p.echoSent(cmd.Conv, sentID, tmp, text, time.Now().UnixMilli(), atts)
}

// Le numéro de séquence (l'identifiant) du message envoyé par XChat.
func sentSequence(resp *response.SendMessageMutationResponse) (string, error) {
	if resp == nil || resp.Data.XChatSendCreateMessageEvent.EncodedMessageEvent == "" {
		if resp != nil && len(resp.Errors) > 0 {
			return "", fmt.Errorf("X a refusé le message : %s", resp.Errors[0].Message)
		}
		return "", errors.New("X n'a pas confirmé l'envoi")
	}
	evt, err := twittermeow.DecodeSendMessageMutationMessageEvent(resp.Data.XChatSendCreateMessageEvent.EncodedMessageEvent)
	if err != nil {
		return "", fmt.Errorf("réponse d'envoi illisible : %w", err)
	}
	return ptrVal(evt.SequenceId), nil
}

func (p *Pont) sendREST(ctx context.Context, cli *twittermeow.Client, conv, msgID, text, mediaID string) (string, error) {
	resp, err := cli.SendDirectMessage(ctx, &payload.SendDirectMessagePayload{
		ConversationID:    restID(conv),
		RequestID:         msgID,
		Text:              text,
		MediaID:           mediaID,
		CardsPlatform:     "Web-12",
		IncludeCards:      1,
		IncludeQuoteCount: true,
		DMUsers:           false,
	})
	if err != nil {
		return "", err
	}
	if resp != nil {
		for _, e := range resp.Entries {
			if m, ok := e.ParseWithErrorLog(&log).(*types.Message); ok && m.ID != "" {
				return m.ID, nil
			}
		}
	}
	return "", nil
}

// ---- Lu, saisie, nouvelle conversation, recherche ----

func (p *Pont) markRead(cmd Cmd) {
	c := p.conv(cmd.Conv)
	if c == nil {
		return
	}
	r := p.getMsg(cmd.Msg)
	if r == nil || r.FromMe || r.Conv != cmd.Conv || !isDigits(r.ID) {
		r = p.lastIncoming(cmd.Conv)
	}
	p.mu.Lock()
	c.ReadWM = max(c.ReadWM, time.Now().UnixMilli())
	p.saveConv(cmd.Conv, c)
	p.listDirty = true
	xchat := c.XChat
	p.mu.Unlock()
	cli := p.client()
	if cli == nil || r == nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if xchat {
		if err := cli.SendXChatReadReceipt(ctx, cmd.Conv, r.ID, time.Now()); err == nil {
			return
		} else {
			log.Debug().Err(err).Msg("lu (XChat)")
		}
	}
	if err := cli.MarkConversationRead(ctx, &payload.MarkConversationReadQuery{ConversationID: restID(cmd.Conv), LastReadEventID: r.ID}); err != nil {
		log.Debug().Err(err).Msg("lu (REST)")
	}
}

// Nouvelle conversation : un identifiant X (chiffres), sinon une recherche.
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimSpace(cmp.Or(strings.TrimSpace(cmd.Number), cmd.Text))
	if q == "" {
		return
	}
	if !isDigits(q) {
		p.search(strings.TrimPrefix(q, "@"))
		return
	}
	cli := p.client()
	if cli == nil {
		emitErr("new", errors.New("X n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	me := p.myID()
	if q == me {
		emitErr("new", errors.New("c'est ton propre compte"))
		return
	}
	perms, err := cli.GetDMPermissions(ctx, payload.GetDMPermissionsQuery{RecipientIDs: q, DMUsers: true})
	if err != nil {
		emitErr("new", fmt.Errorf("nouvelle conversation : %w", explain(err)))
		return
	}
	if pu := perms.Permissions.GetPermissionsForUser(q); pu == nil || !pu.CanDM {
		emitErr("new", errors.New("cette personne n'accepte pas les messages privés de ton compte"))
		return
	}
	for id, u := range perms.Users {
		p.upsertUser(cmp.Or(u.IDStr, id), u.Name, u.ScreenName, cmp.Or(u.ProfileImageURLHTTPS, u.ProfileImageURL))
	}
	conv := methods.CreateConversationID([]string{me, q})
	// Les clés du fil, s'il existe déjà dans XChat.
	if resp, err := cli.GetConversationData(ctx, payload.NewInboxPageConversationDataQueryVariables(conv, true)); err == nil {
		if item, err := resp.Item(conv); err == nil {
			if err := cli.GetXChatProcessor().ProcessKeyChangeEvents(ctx, &item); err != nil {
				log.Debug().Err(err).Msg("nouvelle conversation : clés")
			}
			p.upsertItem(ctx, &item)
		}
	} else {
		log.Debug().Err(err).Msg("nouvelle conversation : infos du fil")
	}
	p.mu.Lock()
	c := p.getOrMakeConv(conv)
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
	resp, err := cli.Search(ctx, payload.SearchQuery{
		IncludeExtIsBlueVerified:    "1",
		IncludeExtVerifiedType:      "1",
		IncludeExtProfileImageShape: "1",
		Query:                       q,
		Src:                         "compose_message",
		ResultType:                  payload.SEARCH_RESULT_TYPE_USERS,
	})
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
	vus := map[string]bool{}
	for _, u := range resp.Users {
		id := cmp.Or(u.IDStr, strconv.FormatInt(u.ID, 10))
		if id == "" || id == "0" || vus[id] {
			continue
		}
		vus[id] = true
		avatar := cmp.Or(u.ProfileImageURLHTTPS, u.ProfileImageURL)
		p.upsertUser(id, u.Name, u.ScreenName, avatar)
		name := cmp.Or(u.Name, u.ScreenName)
		if u.Name != "" && u.ScreenName != "" {
			name = fmt.Sprintf("%s (@%s)", u.Name, u.ScreenName)
		}
		items = append(items, item{ID: id, Name: name, Avatar: avatar})
	}
	emit("search", map[string]any{"query": q, "items": items})
}

// ---- Fin ----

// Oublie le compte : cache, clés des fils et état.
func (p *Pont) forgetAccount() {
	p.mu.Lock()
	p.convs = map[string]*convState{}
	p.contacts = map[string]*contact{}
	p.loaded = map[string]int64{}
	p.maxSeq = ""
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
	p.me = ""
	p.mu.Unlock()
	p.ks.mu.Lock()
	p.ks.sess = nil
	p.ks.mu.Unlock()
	_ = removeFile(sessionPath())
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.mu.Lock()
	s, cli := p.sess, p.cli
	p.mu.Unlock()
	if s != nil && cli != nil && s.Cookies != "" {
		// Les cookies rafraîchis par X pendant la session.
		s.Cookies = cli.GetCookieString()
		saveSession(s)
	}
	p.disconnect()
	if p.db != nil {
		_ = p.db.Close()
	}
}
