package main

// La boucle /sync : le direct. Sans filtre (Reddit omet les réactions et
// les profils dès qu'il y en a un), set_presence=offline (le pont ne se
// déclare jamais en ligne). La réponse a une section propre à Reddit,
// rooms.peek : les demandes de message en aperçu.
//
// /sync ne liste que 20 salons rejoints et 20 invitations au plus : après
// la première synchronisation, /joined_rooms (une fois) dit lesquels
// manquent, et le pont lit leur état, lentement (reconcilier).

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"maunium.net/go/mautrix"
)

type syncRoom struct {
	State struct {
		Events []mxEvent `json:"events"`
	} `json:"state"`
	Timeline struct {
		Events    []mxEvent `json:"events"`
		Limited   bool      `json:"limited"`
		PrevBatch string    `json:"prev_batch"`
	} `json:"timeline"`
	Ephemeral struct {
		Events []mxEvent `json:"events"`
	} `json:"ephemeral"`
	AccountData struct {
		Events []mxEvent `json:"events"`
	} `json:"account_data"`
	InviteState struct {
		Events []mxEvent `json:"events"`
	} `json:"invite_state"`
	Unread *struct {
		Notifications int `json:"notification_count"`
		Highlights    int `json:"highlight_count"`
	} `json:"unread_notifications"`
}

type syncResp struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join   map[string]*syncRoom `json:"join"`
		Invite map[string]*syncRoom `json:"invite"`
		Leave  map[string]*syncRoom `json:"leave"`
		Peek   map[string]*syncRoom `json:"peek"`
	} `json:"rooms"`
}

// syncOnce : une requête /sync (au plus 30 s d'attente côté serveur).
func (p *Pont) syncOnce(ctx context.Context, cli *mautrix.Client, since string, timeout int) (*syncResp, error) {
	q := map[string]string{"timeout": itoa(timeout), "set_presence": "offline"}
	if since != "" {
		q["since"] = since
	}
	u := cli.BuildURLWithQuery(mautrix.ClientURLPath{"v3", "sync"}, q)
	var r syncResp
	_, err := cli.MakeFullRequest(ctx, mautrix.FullRequest{Method: "GET", URL: u, ResponseJSON: &r, MaxAttempts: 1})
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

func (p *Pont) syncLoop(ctx context.Context, gen int, fromLogin bool) {
	premier := true
	echecs := 0
	var limiteDepuis time.Time
	for ctx.Err() == nil {
		p.mu.Lock()
		cli, sf, cur := p.cli, p.sf, p.gen == gen
		p.mu.Unlock()
		if !cur || cli == nil || sf == nil {
			return
		}
		timeout := 30000
		if premier {
			timeout = 0
		}
		r, err := p.syncOnce(ctx, cli, sf.NextBatch, timeout)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			switch {
			case estErrJeton(err):
				log.Info().Msg("sync : jeton refusé, renouvellement")
				if e := p.refreshToken(ctx, sf.Token); e != nil {
					if p.sessionPerdue(e) {
						return
					}
					echecs++
				}
			case estLimite(err):
				// Reddit délest /sync par courtes rafales de 429 : réessayer
				// sans compter d'échec avant une minute de suite.
				if limiteDepuis.IsZero() {
					limiteDepuis = time.Now()
				}
				if time.Since(limiteDepuis) > time.Minute {
					echecs++
				}
			default:
				echecs++
				log.Warn().Str("err", erreurCourte(err)).Int("echecs", echecs).Msg("sync")
			}
			if echecs >= 3 && p.isReady() {
				p.mu.Lock()
				p.ready = false
				p.mu.Unlock()
				emitState("reconnecting")
			}
			attente := 5 * time.Second
			for i := 1; i < echecs && attente < 5*time.Minute; i++ {
				attente *= 2
			}
			if attente > 5*time.Minute {
				attente = 5 * time.Minute
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(jitter(attente)):
			}
			continue
		}
		echecs = 0
		limiteDepuis = time.Time{}
		p.traiterSync(r, !premier)
		p.mu.Lock()
		if p.gen != gen || p.sf == nil {
			p.mu.Unlock()
			return
		}
		p.sf.NextBatch = r.NextBatch
		sfc := *p.sf
		pret := p.ready
		p.ready = true
		salons := p.salons
		sale := p.sale
		p.sale = false
		p.mu.Unlock()
		_ = saveSession(&sfc)
		if sale {
			p.mu.Lock()
			saveSalons(salons)
			p.mu.Unlock()
		}
		if !pret {
			if premier {
				p.chargerProfil(ctx, cli)
			}
			p.emitAccount()
			emitState("ready")
			p.listConversations(false)
			go p.photoMoi(ctx)
		}
		if premier {
			premier = false
			go p.reconcilier(ctx, gen, fromLogin)
		}
	}
}

func erreurCourte(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// chargerProfil : mon nom d'utilisateur (profil Matrix : le nom Reddit).
func (p *Pont) chargerProfil(ctx context.Context, cli *mautrix.Client) {
	cctx, annule := context.WithTimeout(ctx, 20*time.Second)
	defer annule()
	prof, err := cli.GetProfile(cctx, cli.UserID)
	if err != nil || prof == nil {
		return
	}
	p.mu.Lock()
	p.monNom = propreNom(prof.DisplayName)
	p.mu.Unlock()
}

// traiterSync : une réponse /sync. direct : les messages sont nouveaux
// (sinon c'est la première synchronisation : rien n'est annoncé un par un,
// la liste suit).
func (p *Pont) traiterSync(r *syncResp, direct bool) {
	// Les demandes en aperçu, puis les invitations : leurs données de compte
	// (salon caché) d'abord, une demande cachée pouvant être dans les deux.
	for rid, room := range r.Rooms.Peek {
		p.traiterSalon(rid, room, true, direct)
	}
	for rid, room := range r.Rooms.Invite {
		p.traiterInvitation(rid, room, direct)
	}
	for rid, room := range r.Rooms.Join {
		p.traiterSalon(rid, room, false, direct)
	}
	for rid := range r.Rooms.Leave {
		p.mu.Lock()
		_, ok := p.salons[rid]
		delete(p.salons, rid)
		delete(p.msgs, rid)
		p.sale = true
		p.mu.Unlock()
		if ok && direct {
			emit("deleted", map[string]any{"conv": rid})
		}
	}
}

func (p *Pont) salonDe(rid string) *salon { // à appeler avec p.mu
	s := p.salons[rid]
	if s == nil {
		s = &salon{ID: rid, Members: map[string]*membre{}}
		p.salons[rid] = s
		p.sale = true
	}
	return s
}

func (p *Pont) traiterInvitation(rid string, room *syncRoom, direct bool) {
	p.mu.Lock()
	s := p.salonDe(rid)
	s.Invite = true
	for _, ev := range room.InviteState.Events {
		s.noteState(ev, p.me)
		if ev.Type == "m.room.member" && ev.StateKey != nil && *ev.StateKey == p.me && s.LastTS == 0 {
			s.LastTS = ev.TS
		}
	}
	p.noterType(s)
	conv := rid
	p.mu.Unlock()
	if direct {
		p.emitConv(conv)
	}
}

// traiterSalon : un salon rejoint (ou en aperçu, peek : une demande).
func (p *Pont) traiterSalon(rid string, room *syncRoom, peek, direct bool) {
	var aEmettre []Message
	var typing []map[string]any
	p.mu.Lock()
	s := p.salonDe(rid)
	me := p.me
	changed := false
	if peek {
		changed = !s.Invite
		s.Invite = true
	} else if s.Invite {
		s.Invite = false
		changed = true
	}
	for _, ev := range room.AccountData.Events {
		if ev.Type == "com.reddit.hidden_chat" {
			var c struct {
				Hidden bool `json:"hidden"`
			}
			_ = json.Unmarshal(ev.Content, &c)
			if c.Hidden != s.Hidden {
				s.Hidden = c.Hidden
				changed = true
				p.sale = true
			}
		}
	}
	for _, ev := range room.State.Events {
		changed = s.noteState(ev, me) || changed
	}
	if room.Timeline.PrevBatch != "" && (room.Timeline.Limited || p.older[rid] == "") {
		s.PrevBatch = room.Timeline.PrevBatch
	}
	for _, ev := range room.Timeline.Events {
		if ev.StateKey != nil {
			changed = s.noteState(ev, me) || changed
		}
		m, ok, ch := p.evenement(s, ev)
		changed = changed || ch
		if ok {
			aEmettre = append(aEmettre, m)
		}
	}
	if room.Unread != nil {
		if room.Unread.Notifications != s.Unread {
			s.Unread = room.Unread.Notifications
			changed = true
		}
		if room.Unread.Highlights != s.Mentions && s.sorte() == sorteCommunaute {
			s.Mentions = room.Unread.Highlights
			changed = true
		}
	}
	for _, ev := range room.Ephemeral.Events {
		switch ev.Type {
		case "m.receipt":
			changed = p.noteRecu(s, ev) || changed
		case "m.typing":
			typing = append(typing, p.noteTyping(s, ev)...)
		}
	}
	p.noterType(s)
	if changed {
		p.sale = true
	}
	// Un salon caché revient avec un nouveau message des autres (comme Reddit).
	revient := false
	if s.Hidden && direct {
		for _, m := range aEmettre {
			if !m.FromMe && m.Kind != "admin" {
				s.Hidden = false
				revient = true
			}
		}
	}
	cache := s.Hidden || s.Spam
	p.mu.Unlock()
	if !direct || cache {
		return
	}
	for _, t := range typing {
		emit("typing", t)
	}
	for _, m := range aEmettre {
		emit("message", map[string]any{"item": m})
	}
	if changed || len(aEmettre) > 0 || revient {
		p.emitConv(rid)
	}
	if len(aEmettre) > 0 {
		go p.auteursDe(rid, aEmettre)
	}
}

// noterType : un type de salon inconnu est noté une fois au journal (sans
// contenu), pour ajuster le classement des chats de subreddit. À appeler avec p.mu.
func (p *Pont) noterType(s *salon) {
	t := s.Type
	switch t {
	case "", "direct", "reddit_titled_direct", "private_group", "reddit_modmail":
		return
	}
	if p.inconnus[t] {
		return
	}
	p.inconnus[t] = true
	log.Info().Str("type", t).Str("subreddit", s.Subreddit).Str("classe", s.sorte()).Msg("type de salon Reddit peu connu")
}

// evenement : un événement de la chronologie d'un salon. Rend le message à
// annoncer (ok), et si la conversation a changé. À appeler avec p.mu.
func (p *Pont) evenement(s *salon, ev mxEvent) (Message, bool, bool) {
	conv := s.ID
	switch ev.Type {
	case "m.room.message", "m.sticker":
		m, ok := convertir(ev, conv, p.me, func(uid string) string { return p.nomDe(s, uid) })
		if !ok {
			return Message{}, false, false
		}
		p.completer(s, &m)
		p.remember(m)
		ch := p.noteLast(s, m)
		if !m.FromMe && s.sorte() == sorteCommunaute && (m.MentionsMe || m.ReplyToMe) {
			s.Mentions++
			ch = true
		}
		if t := p.typing[conv]; t[ev.Sender] {
			delete(t, ev.Sender)
		}
		if old := p.recalled(conv, m.ID); old != nil {
			return *old, true, ch
		}
		return m, true, ch
	case "m.reaction":
		return p.reaction(s, ev)
	case "m.room.redaction":
		return p.redaction(s, ev)
	case "com.reddit.profile":
		return p.profil(s, ev)
	case "com.reddit.invite_spam_status":
		c := parseContent(ev.Content)
		spam := c.Status == "spam"
		if spam != s.Spam {
			s.Spam = spam
			return Message{}, false, true
		}
	}
	return Message{}, false, false
}

// completer : réponse (texte cité, à moi ?) et mention. À appeler avec p.mu.
func (p *Pont) completer(s *salon, m *Message) {
	if m.ReplyTo != "" {
		if old := p.recalled(s.ID, m.ReplyTo); old != nil {
			m.ReplyText = shorten(firstOf(old.Text, apercu(*old)), 80)
			m.ReplyToMe = old.FromMe && !m.FromMe
			if m.ReplyText != "" && m.Kind != "admin" {
				m.Text = quoteLine(m.ReplyText) + m.Text
			}
		}
	}
	if !m.FromMe {
		m.MentionsMe = mentionne(m.Text, p.monNom)
	}
}

func (p *Pont) nomDe(s *salon, uid string) string { // à appeler avec p.mu
	return firstOf(s.nomDe(uid), p.noms[uid], nomCompte(uid))
}

// reaction : une réaction reçue (ou la mienne, renvoyée par /sync).
func (p *Pont) reaction(s *salon, ev mxEvent) (Message, bool, bool) {
	c := parseContent(ev.Content)
	cible, cle := c.cibleReaction()
	if cible == "" {
		return Message{}, false, false
	}
	if _, deja := p.reacts[ev.ID]; deja {
		return Message{}, false, false
	}
	p.reacts[ev.ID] = reactionMem{conv: s.ID, target: cible, key: cle, sender: ev.Sender}
	old := p.recalled(s.ID, cible)
	if old == nil {
		return Message{}, false, false
	}
	m := *old
	fromMe := ev.Sender == p.me
	nom := ""
	if !fromMe {
		nom = p.nomDe(s, ev.Sender)
	}
	m.Reactions = append(append([]Reaction(nil), m.Reactions...), Reaction{Emoji: emojiDeCle(cle), Sender: nom, FromMe: fromMe, Actor: ev.Sender})
	p.remember(m)
	return m, true, false
}

// redaction : un message ou une réaction retirés.
func (p *Pont) redaction(s *salon, ev mxEvent) (Message, bool, bool) {
	cible := firstOf(ev.Redacts, parseContent(ev.Content).Redacts)
	if cible == "" {
		return Message{}, false, false
	}
	if r, ok := p.reacts[cible]; ok {
		delete(p.reacts, cible)
		old := p.recalled(r.conv, r.target)
		if old == nil {
			return Message{}, false, false
		}
		m := *old
		var garde []Reaction
		retiree := false
		for _, x := range m.Reactions {
			if !retiree && x.Actor == r.sender && x.Emoji == emojiDeCle(r.key) {
				retiree = true
				continue
			}
			garde = append(garde, x)
		}
		m.Reactions = garde
		if m.Reactions == nil {
			m.Reactions = []Reaction{}
		}
		p.remember(m)
		return m, true, false
	}
	old := p.recalled(s.ID, cible)
	if old == nil {
		return Message{}, false, false
	}
	m := retire(*old)
	p.remember(m)
	ch := false
	if s.LastID == m.ID {
		s.Preview = m.Text
		ch = true
	}
	return m, true, ch
}

// profil : com.reddit.profile, le nom et la photo de l'auteur du message visé.
func (p *Pont) profil(s *salon, ev mxEvent) (Message, bool, bool) {
	c := parseContent(ev.Content)
	cible := ""
	if c.RelatesTo != nil {
		cible = c.RelatesTo.EventID
	}
	old := p.recalled(s.ID, cible)
	if old == nil {
		return Message{}, false, false
	}
	uid := old.SenderID
	if old.FromMe {
		uid = p.me
	}
	if uid == "" {
		return Message{}, false, false
	}
	nom := propreNom(firstOf(c.Username, c.DisplayName))
	ic := sansRequete(firstOf(c.IconURL, c.SnoovatarURL))
	ch := false
	if nom != "" && p.noms[uid] != nom {
		p.noms[uid] = nom
		ch = true
		if uid == p.me && p.monNom == "" {
			p.monNom = nom
		}
	}
	if ic != "" {
		p.icones[uid] = ic
	}
	if ch && !old.FromMe && old.Sender != nom {
		m := *old
		m.Sender = nom
		p.remember(m)
		return m, true, true
	}
	return Message{}, false, ch
}

// sansRequete : l'adresse d'une photo sans sa signature (qui change à chaque fois).
func sansRequete(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	if !strings.HasPrefix(u, "https://") {
		return ""
	}
	return u
}

// noteRecu : mes accusés de lecture (lus ailleurs : le salon devient lu).
func (p *Pont) noteRecu(s *salon, ev mxEvent) bool {
	var c map[string]map[string]map[string]struct {
		TS int64 `json:"ts"`
	}
	if json.Unmarshal(ev.Content, &c) != nil {
		return false
	}
	ch := false
	for evID, sortes := range c {
		for sorte, qui := range sortes {
			if sorte != "m.read" && sorte != "m.read.private" {
				continue
			}
			r, ok := qui[p.me]
			if !ok {
				continue
			}
			ts := r.TS
			if m := p.recalled(s.ID, evID); m != nil && m.Time > ts {
				ts = m.Time
			}
			if evID == s.LastID && ts < s.LastTS {
				ts = s.LastTS
			}
			if ts > s.ReadTS {
				s.ReadTS = ts
				if ts >= s.LastTS {
					s.Unread = 0
					s.Mentions = 0
				}
				ch = true
			}
		}
	}
	return ch
}

// noteTyping : m.typing donne la liste de qui écrit ; on en tire les
// débuts et les fins. Rien pour les salons de communauté (trop de bruit).
func (p *Pont) noteTyping(s *salon, ev mxEvent) []map[string]any {
	var c struct {
		UserIDs []string `json:"user_ids"`
	}
	if json.Unmarshal(ev.Content, &c) != nil || s.sorte() == sorteCommunaute {
		return nil
	}
	avant := p.typing[s.ID]
	maintenant := map[string]bool{}
	var out []map[string]any
	for _, u := range c.UserIDs {
		if u == p.me {
			continue
		}
		maintenant[u] = true
		if !avant[u] {
			e := map[string]any{"conv": s.ID, "on": true}
			if s.sorte() == sorteGroupe {
				e["name"] = p.nomDe(s, u)
			}
			out = append(out, e)
		}
	}
	for u := range avant {
		if !maintenant[u] {
			out = append(out, map[string]any{"conv": s.ID, "on": false})
		}
	}
	p.typing[s.ID] = maintenant
	return out
}
