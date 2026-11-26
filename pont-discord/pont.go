package main

// La session Discord : la connexion à la passerelle, l'état des messages
// privés (tenu à jour par les événements) et les commandes de l'interface.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Au plus tant de conversations dans la liste (les plus récentes).
const maxConvs = 200

// Au plus tant de messages gardés en mémoire par conversation (réactions,
// citations, recherche).
const maxMsgsPerConv = 200

// Au plus tant de messages par page d'historique (le client web en demande 50).
const maxPage = 50

type chanState struct {
	ch      *discordgo.Channel
	lastID  string // le dernier message (snowflake)
	readID  string // le dernier message lu (read state)
	preview string
	fromMe  bool
	known   bool // preview et fromMe viennent d'un vrai message
}

type Pont struct {
	ctx context.Context

	mu      sync.Mutex
	sess    *discordgo.Session
	gen     int // la session en cours ; les événements d'une ancienne sont ignorés
	sf      *sessionFile
	me      *discordgo.User
	ready   bool
	login   bool // connexion en cours depuis « pair » ou « login » : émettre paired
	users   map[string]*discordgo.User
	nicks   map[string]string // surnoms d'amis
	friends map[string]bool
	chans   map[string]*chanState
	msgs    map[string][]Message // par conversation, du plus vieux au plus récent
	pending map[string]string    // nonce → tmp_id des envois en cours
	typing  map[string]*time.Timer
	typed   map[string]time.Time // dernier « typing » envoyé, par conversation
	avatars map[string]string    // conversation → avatar déjà annoncé (chemin)
	pairing context.CancelFunc

	presenceOn bool
	presence   map[string]presenceState

	rest throttle // api discord.com
	cdn  throttle // cdn.discordapp.com (images, pièces jointes)

	events chan any

	// « Quoi de neuf ? » (notes.go).
	notes *notesPont
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:        ctx,
		users:      map[string]*discordgo.User{},
		nicks:      map[string]string{},
		friends:    map[string]bool{},
		chans:      map[string]*chanState{},
		msgs:       map[string][]Message{},
		pending:    map[string]string{},
		typing:     map[string]*time.Timer{},
		typed:      map[string]time.Time{},
		avatars:    map[string]string{},
		presenceOn: presenceEnv(),
		presence:   map[string]presenceState{},
		rest:       throttle{gap: 400 * time.Millisecond},
		cdn:        throttle{gap: 150 * time.Millisecond},
		events:     make(chan any, 8192),
	}
	p.notes = nouvellesNotesDiscord(p)
	go p.eventLoop()
	return p
}

// ---- resolver (convert.go) ----

func (p *Pont) userName(id string) string { // à appeler avec p.mu
	if n := p.nicks[id]; n != "" {
		return n
	}
	if u := p.users[id]; u != nil {
		return u.DisplayName()
	}
	return ""
}

func (p *Pont) channelName(id string) string { // à appeler avec p.mu
	if c := p.chans[id]; c != nil {
		return p.convName(c)
	}
	return ""
}

func (p *Pont) meID() string { // à appeler avec p.mu
	if p.me != nil {
		return p.me.ID
	}
	if p.sf != nil {
		return p.sf.UserID
	}
	return ""
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
	p.mu.Unlock()
	emitState("connecting")
	if err := p.connect(sf.Token, false); err != nil {
		log.Warn().Err(err).Msg("connexion")
		emitErr("connect", fmt.Errorf("Discord : %s", errorText(err)))
		emitState("reconnecting")
		go p.retryLater()
	}
}

// retryLater : une nouvelle tentative après un échec de la première
// connexion (réseau absent au lancement), avec une attente croissante.
func (p *Pont) retryLater() {
	delay := 30 * time.Second
	for {
		select {
		case <-time.After(delay):
		case <-p.ctx.Done():
			return
		}
		p.mu.Lock()
		sf, live := p.sf, p.sess != nil
		p.mu.Unlock()
		if sf == nil || live {
			return
		}
		if err := p.connect(sf.Token, false); err == nil {
			return
		}
		delay = min(delay*2, 10*time.Minute)
	}
}

// connect ouvre la passerelle avec ce jeton, comme le client web : la page
// principale d'abord (numéro de version du client), puis IDENTIFY.
func (p *Pont) connect(token string, fromLogin bool) error {
	sess, err := discordgo.New(token)
	if err != nil {
		return err
	}
	if !sess.IsUser {
		return errors.New("jeton refusé")
	}
	p.mu.Lock()
	if p.sess != nil {
		old := p.sess
		p.sess = nil
		go func() { _ = old.Close() }()
	}
	p.gen++
	gen := p.gen
	sf := p.sf
	if sf == nil || sf.Token != token {
		sf = &sessionFile{Token: token}
	}
	if sf.Heartbeat == nil || sf.Heartbeat.IsExpired() {
		hb := discordgo.NewHeartbeatSession()
		sf.Heartbeat = &hb
	}
	sf.Heartbeat.BumpLastUsed()
	if !fromLogin {
		p.sf = sf
		_ = saveSession(sf)
	}
	p.login = fromLogin
	p.ready = false
	p.mu.Unlock()

	sess.HeartbeatSession = *sf.Heartbeat
	sess.Client.Transport = compileTransport(false)
	sess.GatewayHTTPClient.Transport = compileTransport(true)
	sess.StateEnabled = false
	sess.LogLevel = discordgo.LogWarning
	if os.Getenv("JASETTE_DEBUG") != "" {
		sess.LogLevel = discordgo.LogInformational
	}
	sess.Logger = func(lvl, caller int, format string, a ...any) {
		msg := strings.TrimSpace(fmt.Sprintf(format, a...))
		switch lvl {
		case discordgo.LogError:
			log.Error().Str("discordgo", "").Msg(msg)
		case discordgo.LogWarning:
			log.Warn().Str("discordgo", "").Msg(msg)
		default:
			log.Debug().Str("discordgo", "").Msg(msg)
		}
	}
	sess.EventHandler = func(evt any) {
		select {
		case p.events <- genEvent{gen, evt, sf}:
		default:
			log.Warn().Msg("file d'événements pleine : événement perdu")
		}
	}
	if err := sess.LoadMainPage(p.ctx); err != nil {
		log.Warn().Err(err).Msg("page principale de Discord")
	}
	p.mu.Lock()
	if p.gen != gen {
		p.mu.Unlock()
		return errors.New("connexion remplacée")
	}
	p.sess = sess
	p.mu.Unlock()
	for i := 0; ; i++ {
		err = sess.Open()
		if errors.Is(err, discordgo.ErrImmediateDisconnect) && i < 3 {
			log.Warn().Err(err).Msg("nouvel essai dans 5 secondes")
			select {
			case <-time.After(5 * time.Second):
			case <-p.ctx.Done():
				return p.ctx.Err()
			}
			continue
		}
		break
	}
	if err != nil {
		p.mu.Lock()
		if p.gen == gen {
			p.sess = nil
		}
		p.mu.Unlock()
		return err
	}
	return nil
}

type genEvent struct {
	gen int
	evt any
	sf  *sessionFile
}

// Les événements, un à la fois et dans l'ordre (messages, réactions…).
func (p *Pont) eventLoop() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case e := <-p.events:
			ge := e.(genEvent)
			p.mu.Lock()
			current := ge.gen == p.gen
			p.mu.Unlock()
			if current {
				p.safeHandle(ge)
			}
		}
	}
}

func (p *Pont) safeHandle(ge genEvent) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Any("panique", r).Msg("événement Discord")
		}
	}()
	p.handle(ge)
}

func (p *Pont) handle(ge genEvent) {
	switch evt := ge.evt.(type) {
	case *discordgo.Ready:
		p.onReady(evt, ge.sf)
	case *discordgo.ReadySupplemental:
		p.mu.Lock()
		for _, ch := range evt.LazyPrivateChannels {
			p.addChannel(ch)
		}
		p.mu.Unlock()
	case *discordgo.Resumed:
		p.mu.Lock()
		ok := p.me != nil
		p.ready = ok
		p.mu.Unlock()
		if ok {
			emitState("ready")
		}
	case *discordgo.Disconnect:
		p.mu.Lock()
		was := p.ready
		p.ready = false
		p.mu.Unlock()
		if was {
			emitState("reconnecting")
		}
	case *discordgo.InvalidAuth:
		p.onInvalidAuth()
	case *discordgo.ChannelCreate:
		p.onChannel(evt.Channel)
	case *discordgo.ChannelUpdate:
		p.onChannel(evt.Channel)
	case *discordgo.ChannelDelete:
		if evt.Channel != nil && evt.GuildID == "" {
			p.mu.Lock()
			_, ok := p.chans[evt.ID]
			delete(p.chans, evt.ID)
			delete(p.msgs, evt.ID)
			p.mu.Unlock()
			if ok {
				emit("deleted", map[string]any{"conv": evt.ID})
			}
		}
	case *discordgo.ChannelRecipientAdd:
		p.mu.Lock()
		c := p.chans[evt.ChannelID]
		if c != nil && evt.User != nil {
			p.users[evt.User.ID] = evt.User
			c.ch.Recipients = append(c.ch.Recipients, evt.User)
		}
		p.mu.Unlock()
		if c != nil {
			p.emitConv(evt.ChannelID, false)
		}
	case *discordgo.ChannelRecipientRemove:
		p.mu.Lock()
		c := p.chans[evt.ChannelID]
		if c != nil && evt.User != nil {
			kept := c.ch.Recipients[:0]
			for _, u := range c.ch.Recipients {
				if u != nil && u.ID != evt.User.ID {
					kept = append(kept, u)
				}
			}
			c.ch.Recipients = kept
		}
		p.mu.Unlock()
		if c != nil {
			p.emitConv(evt.ChannelID, false)
		}
	case *discordgo.MessageCreate:
		p.onMessage(evt.Message, false)
	case *discordgo.MessageUpdate:
		p.onMessage(evt.Message, true)
	case *discordgo.MessageDelete:
		p.onDelete(evt.ChannelID, evt.ID)
	case *discordgo.MessageDeleteBulk:
		for _, id := range evt.Messages {
			p.onDelete(evt.ChannelID, id)
		}
	case *discordgo.MessageReactionAdd:
		if evt.MessageReaction != nil {
			p.onReaction(evt.MessageReaction, true)
		}
	case *discordgo.MessageReactionRemove:
		if evt.MessageReaction != nil {
			p.onReaction(evt.MessageReaction, false)
		}
	case *discordgo.MessageAck:
		p.onAck(evt.ChannelID, evt.MessageID)
	case *discordgo.TypingStart:
		p.onTyping(evt)
	case *discordgo.PresenceUpdate:
		if evt.GuildID == "" && evt.User != nil {
			p.mu.Lock()
			p.notePresence(evt.User.ID, evt.Status)
			p.mu.Unlock()
		}
	case *discordgo.RelationshipAdd:
		p.onRelationship(evt.Relationship, true)
	case *discordgo.RelationshipUpdate:
		p.onRelationship(evt.Relationship, true)
	case *discordgo.RelationshipRemove:
		p.onRelationship(evt.Relationship, false)
	case *discordgo.UserUpdate:
		if evt.User != nil {
			p.mu.Lock()
			isMe := p.me != nil && evt.ID == p.me.ID
			if isMe {
				p.me = evt.User
			}
			p.mu.Unlock()
			if isMe {
				p.emitAccount()
			}
		}
	}
}

func (p *Pont) onReady(r *discordgo.Ready, sf *sessionFile) {
	if r.User == nil {
		return
	}
	p.mu.Lock()
	if p.sf != nil && p.sf.UserID != "" && p.sf.UserID != r.User.ID {
		// Un autre compte : on oublie tout de l'ancien.
		p.chans = map[string]*chanState{}
		p.msgs = map[string][]Message{}
		p.avatars = map[string]string{}
	}
	p.me = r.User
	p.users[r.User.ID] = r.User
	for _, u := range r.Users {
		if u != nil {
			p.users[u.ID] = u
		}
	}
	p.nicks, p.friends = map[string]string{}, map[string]bool{}
	for _, rel := range r.Relationships {
		p.noteRelationship(rel, true)
	}
	for _, ch := range r.PrivateChannels {
		p.addChannel(ch)
	}
	if r.ReadState != nil {
		for _, e := range r.ReadState.Entries {
			if c := p.chans[e.ID]; c != nil {
				c.readID = string(e.LastMessageID)
			}
		}
	}
	sf.UserID = r.User.ID
	p.sf = sf
	_ = saveSession(sf)
	login := p.login
	p.login = false
	p.ready = true
	if p.pairing != nil {
		p.pairing()
		p.pairing = nil
	}
	for _, pr := range r.Presences {
		if pr != nil && pr.User != nil {
			p.notePresence(pr.User.ID, pr.Status)
		}
	}
	p.mu.Unlock()
	if login {
		emit("paired", nil)
	}
	p.emitAccount()
	emitState("ready")
	go p.notes.annoncer()
	go p.myAvatar()
}

func (p *Pont) onInvalidAuth() {
	p.mu.Lock()
	sess := p.sess
	p.sess = nil
	p.gen++
	p.ready = false
	login := p.login
	p.login = false
	p.mu.Unlock()
	if sess != nil {
		go func() { _ = sess.Close() }()
	}
	if login {
		emitErr("login", errors.New("Discord a refusé ce jeton (invalide ou expiré)"))
	} else {
		emitErr("login", errors.New("Discord a fermé la session (jeton expiré, mot de passe changé ou appareil déconnecté) : reconnectez Discord"))
	}
	p.forget()
	emitState("unpaired")
}

// forget : plus de compte (déconnexion, jeton refusé).
func (p *Pont) forget() {
	removeSession()
	p.mu.Lock()
	p.sf, p.me = nil, nil
	p.chans = map[string]*chanState{}
	p.msgs = map[string][]Message{}
	p.users = map[string]*discordgo.User{}
	p.nicks, p.friends = map[string]string{}, map[string]bool{}
	p.avatars = map[string]string{}
	p.presence = map[string]presenceState{}
	p.mu.Unlock()
}

func (p *Pont) emitAccount() {
	p.mu.Lock()
	me := p.me
	p.mu.Unlock()
	if me == nil {
		return
	}
	emit("account", map[string]any{"service": serviceName(), "name": me.DisplayName(), "id": me.ID, "email": ""})
}

func (p *Pont) unpair() {
	if p.cancelPairing() {
		emitState("unpaired")
		return
	}
	p.mu.Lock()
	sess := p.sess
	p.sess = nil
	p.gen++
	p.ready = false
	p.mu.Unlock()
	if sess != nil {
		// Comme « Déconnexion » du client web : la session disparaît aussi
		// de la liste des appareils du compte.
		ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
		if p.rest.wait(ctx) == nil {
			if err := sess.Logout(); err != nil {
				log.Warn().Err(err).Msg("déconnexion")
			}
		}
		annule()
		_ = sess.Close()
	}
	p.forget()
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.cancelPairing()
	p.mu.Lock()
	sess := p.sess
	p.sess = nil
	p.gen++
	p.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
}

// api : la session prête, ou une erreur lisible.
func (p *Pont) api() (*discordgo.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess == nil || p.me == nil {
		return nil, errors.New("Discord n'est pas connecté")
	}
	return p.sess, nil
}

// ---- Les conversations ----

// addChannel garde un fil privé (message privé ou groupe). À appeler avec p.mu.
func (p *Pont) addChannel(ch *discordgo.Channel) {
	if ch == nil || ch.GuildID != "" || (ch.Type != discordgo.ChannelTypeDM && ch.Type != discordgo.ChannelTypeGroupDM) {
		return
	}
	// Les destinataires : en entier (événements), ou par id (READY).
	if len(ch.Recipients) == 0 && len(ch.RecipientIDs) > 0 {
		for _, id := range ch.RecipientIDs {
			if u := p.users[id]; u != nil {
				ch.Recipients = append(ch.Recipients, u)
			} else {
				ch.Recipients = append(ch.Recipients, &discordgo.User{ID: id})
			}
		}
	}
	for _, u := range ch.Recipients {
		if u != nil && u.Username != "" {
			p.users[u.ID] = u
		}
	}
	c := p.chans[ch.ID]
	if c == nil {
		c = &chanState{}
		p.chans[ch.ID] = c
	}
	c.ch = ch
	if compareIDs(ch.LastMessageID, c.lastID) > 0 {
		c.lastID = ch.LastMessageID
		c.known = false
	}
}

func (p *Pont) onChannel(ch *discordgo.Channel) {
	if ch == nil || ch.GuildID != "" {
		return
	}
	p.mu.Lock()
	_, existed := p.chans[ch.ID]
	if existed && len(ch.Recipients) == 0 && len(ch.RecipientIDs) == 0 {
		ch.Recipients = p.chans[ch.ID].ch.Recipients
	}
	p.addChannel(ch)
	_, ok := p.chans[ch.ID]
	p.mu.Unlock()
	if ok {
		p.emitConv(ch.ID, false)
		if !existed {
			go p.fetchAvatars([]string{ch.ID})
		}
	}
}

// recipients : les autres membres du fil. À appeler avec p.mu.
func (p *Pont) recipients(c *chanState) []*discordgo.User {
	me := p.meID()
	var out []*discordgo.User
	for _, u := range c.ch.Recipients {
		if u != nil && u.ID != me {
			if full := p.users[u.ID]; full != nil {
				u = full
			}
			out = append(out, u)
		}
	}
	return out
}

// convName : le nom affiché d'un fil. À appeler avec p.mu.
func (p *Pont) convName(c *chanState) string {
	if c.ch.Name != "" {
		return c.ch.Name
	}
	var names []string
	for _, u := range p.recipients(c) {
		names = append(names, firstOf(p.userName(u.ID), u.Username, "?"))
	}
	if len(names) == 0 {
		return "Discord"
	}
	return strings.Join(names, ", ")
}

// unread : un message des autres plus récent que le dernier lu. À appeler avec p.mu.
func (c *chanState) unread() bool {
	if c.lastID == "" || compareIDs(c.lastID, c.readID) <= 0 {
		return false
	}
	return !(c.known && c.fromMe)
}

func (p *Pont) convOut(id string, c *chanState) Conversation { // à appeler avec p.mu
	out := Conversation{
		ID:      id,
		Name:    p.convName(c),
		Preview: c.preview,
		FromMe:  c.known && c.fromMe,
		Time:    snowflakeTime(c.lastID),
		Unread:  c.unread(),
		Group:   c.ch.Type == discordgo.ChannelTypeGroupDM,
		Members: []string{},
	}
	if out.Time == 0 {
		out.Time = snowflakeTime(id)
	}
	if out.Group {
		for _, u := range p.recipients(c) {
			out.Members = append(out.Members, firstOf(p.userName(u.ID), u.Username))
		}
	}
	return out
}

func (p *Pont) sortedConvs() []Conversation {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := make([]Conversation, 0, len(p.chans))
	for id, c := range p.chans {
		list = append(list, p.convOut(id, c))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time > list[j].Time })
	if len(list) > maxConvs {
		list = list[:maxConvs]
	}
	return list
}

func (p *Pont) listConversations(archive bool) {
	if archive {
		emit("conversations", map[string]any{"items": []Conversation{}, "folder": "archive"})
		return
	}
	list := p.sortedConvs()
	emit("conversations", map[string]any{"items": list})
	ids := make([]string, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	if len(ids) > 60 {
		ids = ids[:60]
	}
	go p.fetchAvatars(ids)
}

func (p *Pont) emitConv(id string, open bool) {
	p.mu.Lock()
	c := p.chans[id]
	if c == nil {
		p.mu.Unlock()
		return
	}
	item := p.convOut(id, c)
	p.mu.Unlock()
	ev := map[string]any{"item": item}
	if open {
		ev["open"] = true
	}
	emit("conversation", ev)
}

// ---- Les messages en mémoire ----

// remember garde (ou remplace) un message. À appeler avec p.mu.
func (p *Pont) remember(m Message) {
	l := p.msgs[m.Conv]
	i := sort.Search(len(l), func(i int) bool { return compareIDs(l[i].ID, m.ID) >= 0 })
	if i < len(l) && l[i].ID == m.ID {
		l[i] = m
	} else {
		l = append(l, Message{})
		copy(l[i+1:], l[i:])
		l[i] = m
	}
	if len(l) > maxMsgsPerConv {
		l = l[len(l)-maxMsgsPerConv:]
	}
	p.msgs[m.Conv] = l
}

func (p *Pont) recalled(conv, id string) *Message { // à appeler avec p.mu
	l := p.msgs[conv]
	i := sort.Search(len(l), func(i int) bool { return compareIDs(l[i].ID, id) >= 0 })
	if i < len(l) && l[i].ID == id {
		return &l[i]
	}
	return nil
}

// noteLast : le dernier message d'un fil, pour son aperçu. À appeler avec p.mu.
// Rend vrai si la conversation a changé.
func (p *Pont) noteLast(c *chanState, m Message) bool {
	if compareIDs(m.ID, c.lastID) < 0 {
		return false
	}
	preview := shorten(previewOf(textBody(m), m.Attachments), 120)
	if m.Kind == "admin" {
		preview = m.Text
	}
	changed := c.lastID != m.ID || c.preview != preview || c.fromMe != m.FromMe || !c.known
	c.lastID, c.preview, c.fromMe, c.known = m.ID, preview, m.FromMe, true
	if m.FromMe {
		c.readID = m.ID
	}
	return changed
}

// ---- Les événements de messages ----

func (p *Pont) onMessage(dm *discordgo.Message, update bool) {
	if dm == nil || dm.GuildID != "" {
		return
	}
	p.mu.Lock()
	c := p.chans[dm.ChannelID]
	p.mu.Unlock()
	if c == nil {
		if update {
			return
		}
		// Un message privé d'un fil encore inconnu : on le demande une fois.
		ch, err := p.fetchChannel(dm.ChannelID)
		if err != nil || ch.GuildID != "" {
			return
		}
		p.mu.Lock()
		p.addChannel(ch)
		c = p.chans[dm.ChannelID]
		p.mu.Unlock()
		if c == nil {
			return
		}
		go p.fetchAvatars([]string{dm.ChannelID})
	}
	p.mu.Lock()
	if dm.Author != nil && dm.Author.Username != "" {
		p.users[dm.Author.ID] = dm.Author
	}
	var m Message
	old := p.recalled(dm.ChannelID, dm.ID)
	if update && (dm.Author == nil || dm.Timestamp.IsZero()) {
		// Mise à jour partielle (l'aperçu d'un lien, en général).
		if old == nil {
			p.mu.Unlock()
			return
		}
		m = *old
		if lp := linkPreviewOf(dm); lp != nil {
			m.LinkPreview = lp
		}
	} else {
		m = convertMessage(dm, p)
		if old != nil {
			m.TmpID = old.TmpID
			if len(old.Reactions) == len(m.Reactions) {
				m.Reactions = old.Reactions
			}
		}
	}
	if tmp, ok := p.pending[string(dm.Nonce)]; ok && dm.Nonce != "" {
		m.TmpID = tmp
		delete(p.pending, string(dm.Nonce))
	}
	p.remember(m)
	changed := false
	if !update {
		changed = p.noteLast(c, m)
	} else if c.lastID == m.ID {
		changed = p.noteLast(c, m)
	}
	var stopTyping string
	if dm.Author != nil && !update {
		if t := p.typing[dm.ChannelID+"/"+dm.Author.ID]; t != nil {
			t.Stop()
			delete(p.typing, dm.ChannelID+"/"+dm.Author.ID)
			stopTyping = dm.Author.ID
		}
	}
	p.mu.Unlock()
	if stopTyping != "" {
		emit("typing", map[string]any{"conv": dm.ChannelID, "on": false})
	}
	emit("message", map[string]any{"item": m})
	if changed {
		p.emitConv(dm.ChannelID, false)
	}
}

func (p *Pont) onDelete(conv, id string) {
	p.mu.Lock()
	if p.chans[conv] == nil {
		p.mu.Unlock()
		return
	}
	var m Message
	if old := p.recalled(conv, id); old != nil {
		m = retired(*old)
	} else {
		m = retired(Message{ID: id, Conv: conv, Time: snowflakeTime(id), Kind: "discord", Status: "INCOMING_COMPLETE"})
	}
	p.remember(m)
	p.mu.Unlock()
	emit("message", map[string]any{"item": m})
}

func (p *Pont) onReaction(r *discordgo.MessageReaction, add bool) {
	if r.GuildID != "" {
		return
	}
	p.mu.Lock()
	old := p.recalled(r.ChannelID, r.MessageID)
	if old == nil {
		// Un message qu'on n'a pas en mémoire : rien à mettre à jour (pas de
		// requête pour aller le chercher).
		p.mu.Unlock()
		return
	}
	m := *old
	emoji := emojiText(&r.Emoji)
	fromMe := r.UserID == p.meID()
	reacts := append([]Reaction(nil), m.Reactions...)
	if add {
		dup := false
		for _, x := range reacts {
			if x.Emoji == emoji && ((fromMe && x.FromMe) || (x.Actor != "" && x.Actor == r.UserID)) {
				dup = true
			}
		}
		if !dup {
			name := ""
			if !fromMe {
				name = p.userName(r.UserID)
			}
			reacts = append(reacts, Reaction{Emoji: emoji, Sender: name, FromMe: fromMe, Actor: r.UserID})
		}
	} else {
		idx := -1
		for i, x := range reacts {
			if x.Emoji != emoji {
				continue
			}
			if (fromMe && x.FromMe) || (x.Actor != "" && x.Actor == r.UserID) {
				idx = i
				break
			}
			if idx < 0 && !fromMe && x.Actor == "" && !x.FromMe {
				idx = i
			}
		}
		if idx >= 0 {
			reacts = append(reacts[:idx], reacts[idx+1:]...)
		}
	}
	m.Reactions = reacts
	p.remember(m)
	p.mu.Unlock()
	emit("message", map[string]any{"item": m})
}

func (p *Pont) onAck(conv, id string) {
	p.mu.Lock()
	c := p.chans[conv]
	changed := false
	if c != nil && compareIDs(id, c.readID) > 0 {
		was := c.unread()
		c.readID = id
		changed = was != c.unread()
	}
	p.mu.Unlock()
	if changed {
		p.emitConv(conv, false)
	}
}

func (p *Pont) onTyping(t *discordgo.TypingStart) {
	if t.GuildID != "" {
		return
	}
	p.mu.Lock()
	if p.chans[t.ChannelID] == nil || t.UserID == p.meID() {
		p.mu.Unlock()
		return
	}
	name := p.userName(t.UserID)
	k := t.ChannelID + "/" + t.UserID
	if old := p.typing[k]; old != nil {
		old.Stop()
	}
	// Discord n'envoie pas de fin : le « … » tombe après 10 secondes.
	var timer *time.Timer
	timer = time.AfterFunc(10*time.Second, func() {
		p.mu.Lock()
		cur := p.typing[k] == timer
		if cur {
			delete(p.typing, k)
		}
		p.mu.Unlock()
		if cur {
			emit("typing", map[string]any{"conv": t.ChannelID, "on": false})
		}
	})
	p.typing[k] = timer
	p.mu.Unlock()
	ev := map[string]any{"conv": t.ChannelID, "on": true}
	if name != "" {
		ev["name"] = name
	}
	emit("typing", ev)
}

func (p *Pont) noteRelationship(rel *discordgo.Relationship, present bool) { // à appeler avec p.mu
	if rel == nil {
		return
	}
	if !present {
		delete(p.nicks, rel.ID)
		delete(p.friends, rel.ID)
		return
	}
	if rel.Nickname != "" {
		p.nicks[rel.ID] = rel.Nickname
	} else {
		delete(p.nicks, rel.ID)
	}
	p.friends[rel.ID] = rel.Type == discordgo.RelationshipFriend
}

func (p *Pont) onRelationship(rel *discordgo.Relationship, present bool) {
	if rel == nil {
		return
	}
	p.mu.Lock()
	p.noteRelationship(rel, present)
	var convs []string
	for id, c := range p.chans {
		if c.ch.Type == discordgo.ChannelTypeDM {
			for _, u := range c.ch.Recipients {
				if u != nil && u.ID == rel.ID {
					convs = append(convs, id)
				}
			}
		}
	}
	p.mu.Unlock()
	for _, id := range convs {
		p.emitConv(id, false)
	}
}

// fetchChannel : un fil inconnu (premier message d'une nouvelle personne).
func (p *Pont) fetchChannel(id string) (*discordgo.Channel, error) {
	sess, err := p.api()
	if err != nil {
		return nil, err
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	if err := p.rest.wait(ctx); err != nil {
		return nil, err
	}
	return sess.Channel(id, discordgo.WithContext(ctx))
}
