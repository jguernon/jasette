package main

// Le pont lui-même : la session, la liste des conversations, l'historique,
// les nouvelles (chat.bsky.convo.getLog), l'envoi, les réactions, la
// lecture, la recherche et les nouvelles conversations.

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// L'intervalle entre deux interrogations de getLog (les nouvelles).
const pollEvery = 5 * time.Second

type profile struct {
	DID         string `json:"did"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Avatar      string `json:"avatar"`
}

func (pr profile) name() string {
	if n := strings.TrimSpace(pr.DisplayName); n != "" {
		return n
	}
	if pr.Handle != "" && pr.Handle != "handle.invalid" {
		return "@" + pr.Handle
	}
	return "Bluesky"
}

type reactionView struct {
	Value  string `json:"value"`
	Sender struct {
		DID string `json:"did"`
	} `json:"sender"`
}

// msgView : chat.bsky.convo.defs#messageView ou #deletedMessageView.
type msgView struct {
	Type      string          `json:"$type"`
	ID        string          `json:"id"`
	Text      string          `json:"text"`
	Embed     json.RawMessage `json:"embed"`
	Reactions []reactionView  `json:"reactions"`
	Sender    struct {
		DID string `json:"did"`
	} `json:"sender"`
	SentAt string `json:"sentAt"`
}

func (m *msgView) deleted() bool { return strings.HasSuffix(m.Type, "#deletedMessageView") }

type convoView struct {
	ID          string    `json:"id"`
	Members     []profile `json:"members"`
	LastMessage *msgView  `json:"lastMessage"`
	Muted       bool      `json:"muted"`
	Status      string    `json:"status"` // « request » ou « accepted »
	UnreadCount int64     `json:"unreadCount"`
}

type Pont struct {
	ctx    context.Context
	cancel context.CancelFunc
	db     *sql.DB
	a      auth

	mu        sync.Mutex
	gen       int // change à chaque connexion ou déconnexion
	me        string
	myName    string
	ready     bool
	loginBusy bool
	convs     map[string]*convoView
	profiles  map[string]profile
	cursors   map[string]string // conv → curseur de la page plus ancienne
	loaded    map[string]bool   // les fils ouverts dans l'interface
	sent      map[string]bool   // mes messages déjà annoncés (l'écho de getLog est ignoré)

	// « Quoi de neuf ? » (notes.go).
	notes *notesPont
}

func newPont(ctx context.Context) *Pont {
	c, cancel := context.WithCancel(ctx)
	p := &Pont{
		ctx: c, cancel: cancel,
		convs:    map[string]*convoView{},
		profiles: map[string]profile{},
		cursors:  map[string]string{},
		loaded:   map[string]bool{},
		sent:     map[string]bool{},
	}
	p.a.lost = p.authLost
	p.notes = nouvellesNotesBS(p)
	return p
}

func (p *Pont) start() {
	s := loadSession()
	if s == nil {
		emitState("unpaired")
		return
	}
	p.a.set(s)
	p.begin(s)
}

func (p *Pont) isGen(g int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gen == g
}

// begin : la session est là ; le compte, les fils, puis les nouvelles.
func (p *Pont) begin(s *session) {
	p.mu.Lock()
	p.gen++
	g := p.gen
	p.me = s.DID
	p.ready = false
	p.mu.Unlock()
	emitState("connecting")
	wait := 2 * time.Second
	for {
		if !p.isGen(g) || p.ctx.Err() != nil {
			return
		}
		err := p.fetchConvos()
		if err == nil {
			break
		}
		if errors.Is(err, errSessionLost) || errors.Is(err, errNotConnected) {
			return
		}
		if isScopeErr(err) {
			emitErr("login", errScope)
			p.forget()
			return
		}
		log.Warn().Err(err).Msg("connexion")
		emitState("reconnecting")
		select {
		case <-p.ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, time.Minute)
	}
	p.loadMe(s)
	p.mu.Lock()
	p.ready = true
	p.mu.Unlock()
	emitState("ready")
	p.emitList()
	go p.notes.annoncer()
	go p.avatars(nil)
	go p.poll(g)
}

// loadMe : mon nom et ma photo (app.bsky.actor.getProfile).
func (p *Pont) loadMe(s *session) {
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	var me profile
	if err := p.a.call(ctx, "app.bsky.actor.getProfile", url.Values{"actor": {s.DID}}, nil, "", &me); err != nil {
		log.Warn().Err(err).Msg("mon profil")
		me = profile{DID: s.DID, Handle: s.Handle}
	}
	p.mu.Lock()
	p.myName = me.name()
	p.profiles[s.DID] = me
	p.mu.Unlock()
	emit("account", map[string]any{"service": serviceName(), "name": me.name(), "id": s.DID, "email": ""})
	if me.Avatar != "" {
		go func() {
			if path := p.fetchAvatar(me.Avatar); path != "" {
				emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
			}
		}()
	}
}

func (p *Pont) authLost() {
	p.mu.Lock()
	p.gen++
	p.ready = false
	p.mu.Unlock()
	emitErr("login", errSessionLost)
	emitState("unpaired")
}

// forget : oublie la session (sans toucher au compte chez Bluesky).
func (p *Pont) forget() {
	p.a.set(nil)
	_ = os.Remove(sessionPath())
	p.mu.Lock()
	p.gen++
	p.ready = false
	p.mu.Unlock()
	emitState("unpaired")
}

// ---- La connexion ----

func (p *Pont) login(ident, password string) {
	if ident == "" || password == "" {
		emitErr("login", errors.New("entrez votre identifiant Bluesky et un mot de passe d'app"))
		return
	}
	p.mu.Lock()
	if p.loginBusy {
		p.mu.Unlock()
		return
	}
	p.loginBusy = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.loginBusy = false
		p.mu.Unlock()
	}()
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	s, err := createSession(ctx, ident, password)
	if err != nil {
		log.Warn().Err(err).Msg("connexion")
		emitErr("login", err)
		if p.a.get() == nil {
			emitState("unpaired")
		}
		return
	}
	// Le mot de passe d'app doit donner accès aux messages privés.
	var probe struct{}
	if err := rawCall(ctx, s.PDS, "chat.bsky.convo.listConvos", url.Values{"limit": {"1"}}, nil, s.Access, chatProxy, &probe); err != nil {
		if isScopeErr(err) || isXErr(err, "InvalidToken", "AuthMissing") {
			err = errScope
			_ = rawCall(ctx, s.PDS, "com.atproto.server.deleteSession", nil, struct{}{}, s.Refresh, "", nil)
		}
		emitErr("login", err)
		if p.a.get() == nil {
			emitState("unpaired")
		}
		return
	}
	var old string
	if p.loadKV("account", &old) && old != "" && old != s.DID {
		p.wipeStore()
		p.mu.Lock()
		p.convs = map[string]*convoView{}
		p.cursors = map[string]string{}
		p.mu.Unlock()
	}
	p.saveKV("account", s.DID)
	if err := saveSession(s); err != nil {
		emitErr("login", fmt.Errorf("session : %w", err))
		return
	}
	p.a.set(s)
	emit("paired", nil)
	p.begin(s)
}

func (p *Pont) unpair() {
	if s := p.a.get(); s != nil {
		ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
		_ = rawCall(ctx, s.PDS, "com.atproto.server.deleteSession", nil, struct{}{}, s.Refresh, "", nil)
		annule()
	}
	p.a.set(nil)
	_ = os.Remove(sessionPath())
	p.wipeStore()
	p.mu.Lock()
	p.gen++
	p.ready = false
	p.me, p.myName = "", ""
	p.convs = map[string]*convoView{}
	p.cursors = map[string]string{}
	p.loaded = map[string]bool{}
	p.mu.Unlock()
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.cancel()
	if p.db != nil {
		_ = p.db.Close()
	}
}

// ---- Les fils ----

// fetchConvos : tous les fils (chat.bsky.convo.listConvos, par pages).
func (p *Pont) fetchConvos() error {
	ctx, annule := context.WithTimeout(p.ctx, 90*time.Second)
	defer annule()
	all := map[string]*convoView{}
	cursor := ""
	for range 10 {
		q := url.Values{"limit": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var r struct {
			Cursor string       `json:"cursor"`
			Convos []*convoView `json:"convos"`
		}
		if err := p.a.chat(ctx, "chat.bsky.convo.listConvos", q, nil, &r); err != nil {
			return err
		}
		for _, c := range r.Convos {
			all[c.ID] = c
		}
		if r.Cursor == "" || len(r.Convos) == 0 {
			break
		}
		cursor = r.Cursor
	}
	p.mu.Lock()
	p.convs = all
	for _, c := range all {
		p.noteMembers(c)
	}
	p.mu.Unlock()
	return nil
}

// À appeler avec p.mu.
func (p *Pont) noteMembers(c *convoView) {
	for _, m := range c.Members {
		p.profiles[m.DID] = m
	}
}

// À appeler avec p.mu.
func (p *Pont) others(c *convoView) []profile {
	var l []profile
	for _, m := range c.Members {
		if m.DID != p.me {
			l = append(l, m)
		}
	}
	return l
}

func parseTime(s string) int64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// À appeler avec p.mu.
func (p *Pont) convOut(c *convoView) Conversation {
	others := p.others(c)
	names := make([]string, 0, len(others))
	for _, o := range others {
		names = append(names, o.name())
	}
	name := strings.Join(names, ", ")
	if name == "" {
		name = cmp.Or(p.myName, "Bluesky")
	}
	out := Conversation{ID: c.ID, Name: name, Members: names, Group: len(others) > 1, Unread: c.UnreadCount > 0}
	if out.Members == nil {
		out.Members = []string{}
	}
	if m := c.LastMessage; m != nil {
		out.Time = parseTime(m.SentAt)
		out.FromMe = m.Sender.DID == p.me
		if m.deleted() {
			out.Preview = "Message retiré"
		} else {
			out.Preview = shorten(strings.ReplaceAll(p.textOf(m), "\n", " "), 120)
		}
	}
	if c.Status == "request" {
		out.Preview = strings.TrimSuffix("Demande de message · "+out.Preview, " · ")
	}
	return out
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (p *Pont) sortedConvs() []Conversation {
	p.mu.Lock()
	items := make([]Conversation, 0, len(p.convs))
	for _, c := range p.convs {
		items = append(items, p.convOut(c))
	}
	p.mu.Unlock()
	slices.SortFunc(items, func(a, b Conversation) int { return cmp.Compare(b.Time, a.Time) })
	return items
}

func (p *Pont) emitList() {
	emit("conversations", map[string]any{"items": p.sortedConvs()})
}

func (p *Pont) emitConv(id string, open bool) {
	p.mu.Lock()
	c := p.convs[id]
	if c == nil {
		p.mu.Unlock()
		return
	}
	item := p.convOut(c)
	p.mu.Unlock()
	ev := map[string]any{"item": item}
	if open {
		ev["open"] = true
	}
	emit("conversation", ev)
}

func (p *Pont) listConversations(archive bool) {
	if archive {
		// Bluesky n'a pas d'archives : celles de Jasette sont gardées par l'interface.
		emit("conversations", map[string]any{"items": []Conversation{}, "folder": "archive"})
		return
	}
	p.mu.Lock()
	ready := p.ready
	p.mu.Unlock()
	if ready {
		if err := p.fetchConvos(); err != nil {
			log.Warn().Err(err).Msg("liste des fils")
		}
	}
	p.emitList()
	if ready {
		go p.avatars(nil)
	}
}

// refreshConv : un fil relu (chat.bsky.convo.getConvo), puis annoncé.
func (p *Pont) refreshConv(id string, open bool) error {
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	var r struct {
		Convo *convoView `json:"convo"`
	}
	if err := p.a.chat(ctx, "chat.bsky.convo.getConvo", url.Values{"convoId": {id}}, nil, &r); err != nil {
		return err
	}
	if r.Convo == nil {
		return errors.New("conversation introuvable")
	}
	p.mu.Lock()
	_, connu := p.convs[id]
	p.convs[id] = r.Convo
	p.noteMembers(r.Convo)
	p.mu.Unlock()
	p.emitConv(id, open)
	if !connu {
		go p.avatars([]string{id})
	}
	return nil
}

// ---- Les avatars ----

// fetchAvatar : télécharge une photo (CDN de Bluesky) une seule fois.
func (p *Pont) fetchAvatar(u string) string {
	if !strings.HasPrefix(u, "https://") {
		return ""
	}
	path := filepath.Join(mediaDir(), "avatar-"+hashName(u)+".jpg")
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Jasette")
	res, err := httpc.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil || len(data) == 0 {
		return ""
	}
	if writeWhole(path, data) != nil {
		return ""
	}
	return path
}

// avatars : la photo de l'autre personne de chaque fil (ids nil = tous).
func (p *Pont) avatars(ids []string) {
	type av struct{ conv, url string }
	var l []av
	p.mu.Lock()
	for id, c := range p.convs {
		if ids != nil && !slices.Contains(ids, id) {
			continue
		}
		if o := p.others(c); len(o) > 0 && o[0].Avatar != "" {
			l = append(l, av{id, o[0].Avatar})
		}
	}
	p.mu.Unlock()
	for _, a := range l {
		if p.ctx.Err() != nil {
			return
		}
		if path := p.fetchAvatar(a.url); path != "" {
			emit("avatar", map[string]any{"conv": a.conv, "path": path})
		}
	}
}

// ---- Les messages ----

func msgKey(conv, id string) string { return conv + "/" + id }

// splitMsgKey : « conv/id » → conv, id. L'interface peut laisser le préfixe
// « b: » : on coupe au dernier « / ».
func splitMsgKey(k string) (string, string, bool) {
	i := strings.LastIndexByte(k, '/')
	if i <= 0 || i == len(k)-1 {
		return "", "", false
	}
	return strings.TrimPrefix(k[:i], "b:"), k[i+1:], true
}

// textOf : le texte d'un message, avec le lien du message cité s'il y en a un.
func (p *Pont) textOf(m *msgView) string {
	text := m.Text
	if link := embedLink(m.Embed); link != "" {
		text = strings.TrimSpace(text + "\n" + link)
	}
	return text
}

// embedLink : un message privé peut citer une publication
// (app.bsky.embed.record#view) : son adresse sur bsky.app.
func embedLink(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var e struct {
		Record struct {
			URI    string `json:"uri"`
			Author struct {
				Handle string `json:"handle"`
			} `json:"author"`
		} `json:"record"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return ""
	}
	return postLink(e.Record.URI)
}

// at://did/app.bsky.feed.post/rkey → https://bsky.app/profile/did/post/rkey
func postLink(uri string) string {
	rest, ok := strings.CutPrefix(uri, "at://")
	if !ok {
		return ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return ""
	}
	switch parts[1] {
	case "app.bsky.feed.post":
		return "https://bsky.app/profile/" + parts[0] + "/post/" + parts[2]
	case "app.bsky.feed.generator":
		return "https://bsky.app/profile/" + parts[0] + "/feed/" + parts[2]
	case "app.bsky.graph.list":
		return "https://bsky.app/profile/" + parts[0] + "/lists/" + parts[2]
	}
	return ""
}

// À appeler avec p.mu.
func (p *Pont) personName(did string) string {
	if pr, ok := p.profiles[did]; ok {
		return pr.name()
	}
	return did
}

func (p *Pont) convert(conv string, m *msgView) Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	mine := m.Sender.DID == p.me
	out := Message{
		ID:     msgKey(conv, m.ID),
		Conv:   conv,
		FromMe: mine,
		Time:   parseTime(m.SentAt),
		Kind:   "bluesky",
		Status: "INCOMING_COMPLETE",
	}
	if mine {
		out.Status = "OUTGOING_DELIVERED"
	} else {
		out.Sender = p.personName(m.Sender.DID)
	}
	if m.deleted() {
		out.Text, out.System = "🚫 Message retiré", "deleted"
		return out
	}
	out.Text = p.textOf(m)
	for _, r := range m.Reactions {
		out.Reactions = append(out.Reactions, Reaction{
			Emoji: r.Value, Sender: p.personName(r.Sender.DID), FromMe: r.Sender.DID == p.me, Actor: r.Sender.DID,
		})
	}
	return out
}

// messages : l'historique d'un fil (chat.bsky.convo.getMessages). Ne marque
// rien comme lu.
func (p *Pont) messages(cmd Cmd) {
	conv := strings.TrimPrefix(cmd.Conv, "b:")
	count := int(cmd.Count)
	if count <= 0 {
		count = 50
	}
	count = min(count, 100)
	older := cmd.Before != ""
	p.mu.Lock()
	cursor := p.cursors[conv]
	p.mu.Unlock()
	if older && cursor == "" {
		emit("messages", map[string]any{"conv": cmd.Conv, "items": []Message{}, "older": true, "more": false})
		return
	}
	q := url.Values{"convoId": {conv}, "limit": {fmt.Sprint(count)}}
	if older {
		q.Set("cursor", cursor)
	}
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	defer annule()
	var r struct {
		Cursor   string     `json:"cursor"`
		Messages []*msgView `json:"messages"`
	}
	items := []Message{}
	more := false
	if err := p.a.chat(ctx, "chat.bsky.convo.getMessages", q, nil, &r); err != nil {
		log.Warn().Err(err).Str("fil", conv).Msg("historique")
		if !older {
			items = p.cached(conv, count)
		}
	} else {
		// Bluesky rend du plus récent au plus vieux.
		for i := len(r.Messages) - 1; i >= 0; i-- {
			m := p.convert(conv, r.Messages[i])
			p.putMsg(m)
			items = append(items, m)
		}
		more = r.Cursor != "" && len(r.Messages) >= count
		p.mu.Lock()
		p.cursors[conv] = r.Cursor
		p.mu.Unlock()
	}
	p.mu.Lock()
	p.loaded[conv] = true
	p.mu.Unlock()
	emit("messages", map[string]any{"conv": cmd.Conv, "items": items, "older": older, "more": more})
}

// cached : les derniers messages gardés, quand Bluesky ne répond pas.
func (p *Pont) cached(conv string, n int) []Message {
	items := []Message{}
	rows, err := p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv = $1 ORDER BY time DESC LIMIT $2", conv, n)
	if err != nil {
		return items
	}
	defer rows.Close()
	for rows.Next() {
		var b []byte
		var m Message
		if rows.Scan(&b) == nil && json.Unmarshal(b, &m) == nil {
			items = append(items, m)
		}
	}
	slices.Reverse(items)
	return items
}

// ---- Les nouvelles : chat.bsky.convo.getLog ----

type logEntry struct {
	Type    string   `json:"$type"`
	ConvoID string   `json:"convoId"`
	Message *msgView `json:"message"`
}

func (p *Pont) poll(g int) {
	ctx := p.ctx
	cursor := ""
	// Le point de départ : le curseur le plus récent (les vieilles entrées
	// sont déjà dans la liste des fils).
	for cursor == "" {
		if !p.isGen(g) || ctx.Err() != nil {
			return
		}
		var r struct {
			Cursor string `json:"cursor"`
		}
		c, annule := context.WithTimeout(ctx, 30*time.Second)
		err := p.a.chat(c, "chat.bsky.convo.getLog", nil, nil, &r)
		annule()
		if err == nil {
			cursor = r.Cursor
			if cursor != "" {
				break
			}
		} else if errors.Is(err, errSessionLost) || errors.Is(err, errNotConnected) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
	fails := 0
	lastList := time.Now()
	for {
		wait := pollEvery
		if fails > 0 {
			wait = min(pollEvery*time.Duration(1<<min(fails, 4)), time.Minute)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if !p.isGen(g) {
			return
		}
		var r struct {
			Cursor string     `json:"cursor"`
			Logs   []logEntry `json:"logs"`
		}
		c, annule := context.WithTimeout(ctx, 30*time.Second)
		err := p.a.chat(c, "chat.bsky.convo.getLog", url.Values{"cursor": {cursor}}, nil, &r)
		annule()
		if err != nil {
			if errors.Is(err, errSessionLost) || errors.Is(err, errNotConnected) {
				return
			}
			fails++
			log.Warn().Err(err).Int("essais", fails).Msg("nouvelles")
			if fails == 3 {
				emitState("reconnecting")
			}
			continue
		}
		if fails >= 3 {
			emitState("ready")
		}
		fails = 0
		if r.Cursor != "" {
			cursor = r.Cursor
		}
		p.apply(r.Logs)
		// De temps en temps, toute la liste (noms, photos, demandes).
		if time.Since(lastList) > 15*time.Minute {
			lastList = time.Now()
			if p.fetchConvos() == nil {
				p.emitList()
			}
		}
	}
}

func (p *Pont) apply(logs []logEntry) {
	refresh := map[string]bool{}
	left := false
	for _, l := range logs {
		conv := l.ConvoID
		kind := l.Type[strings.LastIndexByte(l.Type, '#')+1:]
		switch kind {
		case "logCreateMessage":
			if l.Message == nil {
				continue
			}
			m := p.convert(conv, l.Message)
			p.mu.Lock()
			deja := p.sent[m.ID]
			delete(p.sent, m.ID)
			c := p.convs[conv]
			if c == nil {
				refresh[conv] = true
			} else {
				c.LastMessage = l.Message
				if !m.FromMe {
					c.UnreadCount++
				}
			}
			p.mu.Unlock()
			if deja {
				continue
			}
			p.putMsg(m)
			emit("message", map[string]any{"item": m})
			if c != nil {
				p.emitConv(conv, false)
			}
		case "logDeleteMessage", "logAddReaction", "logRemoveReaction":
			if l.Message == nil {
				continue
			}
			m := p.convert(conv, l.Message)
			if l.Message.deleted() {
				if old := p.getMsg(m.ID); old != nil {
					m.Time, m.Sender, m.FromMe, m.Status = old.Time, old.Sender, old.FromMe, old.Status
				}
				refresh[conv] = true
			}
			p.putMsg(m)
			p.mu.Lock()
			shown := p.loaded[conv]
			p.mu.Unlock()
			if shown {
				emit("message", map[string]any{"item": m})
			}
		case "logLeaveConvo":
			p.mu.Lock()
			delete(p.convs, conv)
			p.mu.Unlock()
			left = true
		default:
			// logBeginConvo, logAcceptConvo, logReadMessage, logMuteConvo… :
			// le fil relu.
			if conv != "" {
				refresh[conv] = true
			}
		}
	}
	for conv := range refresh {
		if err := p.refreshConv(conv, false); err != nil {
			log.Warn().Err(err).Str("fil", conv).Msg("fil")
		}
	}
	if left {
		p.emitList()
	}
}

// ---- Envoyer ----

var urlRe = regexp.MustCompile(`https?://[^\s<>"]+[^\s<>".,;:!?)\]}'»]`)

// linkFacets : les liens du texte, pour qu'ils soient cliquables chez Bluesky
// (positions en octets UTF-8).
func linkFacets(text string) []map[string]any {
	var l []map[string]any
	for _, loc := range urlRe.FindAllStringIndex(text, -1) {
		l = append(l, map[string]any{
			"index":    map[string]any{"byteStart": loc[0], "byteEnd": loc[1]},
			"features": []map[string]any{{"$type": "app.bsky.richtext.facet#link", "uri": text[loc[0]:loc[1]]}},
		})
	}
	return l
}

func explainSend(err error) error {
	var x *xrpcErr
	if errors.As(err, &x) {
		m := strings.ToLower(x.Message)
		switch {
		case strings.Contains(m, "disabled") || strings.Contains(m, "block") || strings.Contains(m, "not accept"):
			return errors.New("cette personne n'accepte pas vos messages privés sur Bluesky")
		case x.Name == "ConvoLocked":
			return errors.New("cette conversation est verrouillée")
		case x.Name == "RateLimitExceeded":
			return errors.New("trop de messages d'un coup : réessayez dans un moment")
		}
	}
	return fmt.Errorf("Bluesky : %w", err)
}

func (p *Pont) send(cmd Cmd) {
	conv := strings.TrimPrefix(cmd.Conv, "b:")
	tmp := cmp.Or(cmd.TmpID, fmt.Sprintf("tmp_%d", time.Now().UnixNano()))
	fail := func(err error) {
		log.Warn().Err(err).Str("fil", conv).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		fail(errors.New("message vide"))
		return
	}
	if utf8.RuneCountInString(text) > 1000 || len(text) > 10000 {
		fail(errors.New("message trop long pour Bluesky (1000 caractères au plus)"))
		return
	}
	msg := map[string]any{"text": text}
	if f := linkFacets(text); len(f) > 0 {
		msg["facets"] = f
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	var r msgView
	if err := p.a.chat(ctx, "chat.bsky.convo.sendMessage", nil, map[string]any{"convoId": conv, "message": msg}, &r); err != nil {
		fail(explainSend(err))
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	m := p.convert(conv, &r)
	m.TmpID = tmp
	p.mu.Lock()
	p.sent[m.ID] = true
	if c := p.convs[conv]; c != nil {
		c.LastMessage = &r
	}
	p.mu.Unlock()
	p.putMsg(m)
	emit("message", map[string]any{"item": m})
	p.emitConv(conv, false)
}

// ---- Les réactions ----

func (p *Pont) react(cmd Cmd) {
	conv, id, ok := splitMsgKey(cmd.ID)
	fail := func(err error) {
		log.Warn().Err(err).Str("msg", cmd.ID).Msg("réaction")
		emit("error", map[string]any{"where": "react", "text": err.Error(), "conv": cmd.Conv, "id": cmd.ID})
	}
	if !ok {
		fail(fmt.Errorf("message inconnu : %s", cmd.ID))
		return
	}
	emoji := strings.TrimSpace(cmd.Emoji)
	m := p.getMsg(msgKey(conv, id))
	old := ""
	if m != nil {
		for _, r := range m.Reactions {
			if r.FromMe {
				old = r.Emoji
			}
		}
	}
	if old == emoji {
		if m != nil {
			emit("message", map[string]any{"item": *m})
		}
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	var r struct {
		Message *msgView `json:"message"`
	}
	if old != "" {
		if err := p.a.chat(ctx, "chat.bsky.convo.removeReaction", nil, map[string]any{"convoId": conv, "messageId": id, "value": old}, &r); err != nil {
			fail(explainReact(err))
			return
		}
	}
	if emoji != "" {
		if err := p.a.chat(ctx, "chat.bsky.convo.addReaction", nil, map[string]any{"convoId": conv, "messageId": id, "value": emoji}, &r); err != nil {
			fail(explainReact(err))
			if r.Message == nil && m != nil {
				emit("message", map[string]any{"item": *m})
			}
		}
	}
	if r.Message != nil {
		n := p.convert(conv, r.Message)
		p.putMsg(n)
		emit("message", map[string]any{"item": n})
	}
}

func explainReact(err error) error {
	switch {
	case isXErr(err, "ReactionMessageDeleted"):
		return errors.New("ce message a été retiré")
	case isXErr(err, "ReactionLimitReached"):
		return errors.New("trop de réactions sur ce message")
	case isXErr(err, "ReactionInvalidValue"):
		return errors.New("Bluesky n'accepte qu'un seul emoji comme réaction")
	}
	return fmt.Errorf("Bluesky : %w", err)
}

// ---- Lu ----

// markRead : la seule façon de marquer un fil comme lu (commande « read »).
func (p *Pont) markRead(cmd Cmd) {
	conv := strings.TrimPrefix(cmd.Conv, "b:")
	if conv == "" {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	if err := p.a.chat(ctx, "chat.bsky.convo.updateRead", nil, map[string]any{"convoId": conv}, nil); err != nil {
		log.Warn().Err(err).Str("fil", conv).Msg("lu")
		return
	}
	p.mu.Lock()
	if c := p.convs[conv]; c != nil {
		c.UnreadCount = 0
	}
	p.mu.Unlock()
	p.emitConv(conv, false)
}

// ---- Nouvelle conversation et recherche ----

// newChat : un nom d'utilisateur (nom.bsky.social, @nom), un DID ou un fil
// connu ; sinon, une recherche.
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimPrefix(strings.TrimSpace(cmp.Or(cmd.Number, cmd.Text)), "b:")
	if q == "" {
		return
	}
	p.mu.Lock()
	_, connu := p.convs[q]
	p.mu.Unlock()
	if connu {
		p.emitConv(q, true)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	did := ""
	h := strings.ToLower(strings.TrimPrefix(q, "@"))
	switch {
	case strings.HasPrefix(q, "did:"):
		did = q
	case !strings.ContainsAny(h, " ") && len(h) >= 3:
		if !strings.Contains(h, ".") {
			h += ".bsky.social"
		}
		var r struct {
			DID string `json:"did"`
		}
		if err := p.a.call(ctx, "com.atproto.identity.resolveHandle", url.Values{"handle": {h}}, nil, "", &r); err == nil {
			did = r.DID
		}
	}
	if did == "" {
		p.search(q)
		return
	}
	var r struct {
		Convo *convoView `json:"convo"`
	}
	if err := p.a.chat(ctx, "chat.bsky.convo.getConvoForMembers", url.Values{"members": {did}}, nil, &r); err != nil || r.Convo == nil {
		if err == nil {
			err = errors.New("conversation introuvable")
		}
		emitErr("new", explainSend(err))
		return
	}
	p.mu.Lock()
	p.convs[r.Convo.ID] = r.Convo
	p.noteMembers(r.Convo)
	p.mu.Unlock()
	p.emitConv(r.Convo.ID, true)
	go p.avatars([]string{r.Convo.ID})
}

// search : des personnes (app.bsky.actor.searchActorsTypeahead) et celles
// des fils connus. L'id est leur DID (passé ensuite à « new »).
func (p *Pont) search(q string) {
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	items := []item{}
	seen := map[string]bool{}
	add := func(pr profile) {
		if pr.DID == "" || seen[pr.DID] {
			return
		}
		seen[pr.DID] = true
		name := pr.name()
		if pr.Handle != "" && !strings.HasPrefix(name, "@") {
			name = fmt.Sprintf("%s (@%s)", name, pr.Handle)
		}
		items = append(items, item{ID: pr.DID, Name: name})
	}
	defer func() { emit("search", map[string]any{"query": q, "items": items}) }()
	if q == "" {
		return
	}
	lq := strings.ToLower(strings.TrimPrefix(q, "@"))
	p.mu.Lock()
	for did, pr := range p.profiles {
		if did == p.me {
			continue
		}
		if strings.Contains(strings.ToLower(pr.DisplayName), lq) || strings.Contains(strings.ToLower(pr.Handle), lq) {
			add(pr)
		}
	}
	p.mu.Unlock()
	if len([]rune(lq)) < 2 {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	var r struct {
		Actors []profile `json:"actors"`
	}
	if err := p.a.call(ctx, "app.bsky.actor.searchActorsTypeahead", url.Values{"q": {lq}, "limit": {"20"}}, nil, "", &r); err != nil {
		log.Warn().Err(err).Msg("recherche")
		return
	}
	for _, a := range r.Actors {
		add(a)
	}
}
