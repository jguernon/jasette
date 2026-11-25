package main

// Les mises à jour de Telegram (nouveaux messages, modifications,
// suppressions, réactions, lectures ailleurs, saisie) et la conversion des
// messages vers le format de l'interface.

import (
	"cmp"
	"context"
	"strings"
	"time"

	"github.com/gotd/td/tg"
)

func (p *Pont) handlers(d tg.UpdateDispatcher) {
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		p.noteEnt(e)
		p.incoming(u.Message, true)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		p.noteEnt(e)
		p.incoming(u.Message, true)
		return nil
	})
	d.OnEditMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		p.noteEnt(e)
		p.incoming(u.Message, false)
		return nil
	})
	d.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		p.noteEnt(e)
		p.incoming(u.Message, false)
		return nil
	})
	d.OnDeleteMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteMessages) error {
		for _, n := range u.Messages {
			for _, r := range p.msgsByNum(n) {
				p.retire(r)
			}
		}
		return nil
	})
	d.OnDeleteChannelMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		conv := keyOf('c', u.ChannelID)
		for _, n := range u.Messages {
			if r := p.getMsg(msgKey(conv, n)); r != nil {
				p.retire(r)
			}
		}
		return nil
	})
	d.OnMessageReactions(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessageReactions) error {
		p.noteEnt(e)
		conv := peerKey(u.Peer)
		r := p.getMsg(msgKey(conv, u.MsgID))
		if r == nil {
			return nil
		}
		p.mu.Lock()
		r.Reactions = p.reactionsOf(u.Reactions)
		p.mu.Unlock()
		p.putMsg(r)
		p.emitMsg(r, false)
		return nil
	})
	// Lu ailleurs (le téléphone, un autre ordinateur).
	d.OnReadHistoryInbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadHistoryInbox) error {
		p.readInbox(peerKey(u.Peer), u.MaxID, u.StillUnreadCount)
		return nil
	})
	d.OnReadChannelInbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadChannelInbox) error {
		p.readInbox(keyOf('c', u.ChannelID), u.MaxID, u.StillUnreadCount)
		return nil
	})
	// Mes messages lus par l'autre.
	d.OnReadHistoryOutbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadHistoryOutbox) error {
		p.readOutbox(peerKey(u.Peer), u.MaxID)
		return nil
	})
	d.OnReadChannelOutbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadChannelOutbox) error {
		p.readOutbox(keyOf('c', u.ChannelID), u.MaxID)
		return nil
	})
	d.OnDialogUnreadMark(func(ctx context.Context, e tg.Entities, u *tg.UpdateDialogUnreadMark) error {
		if dp, ok := u.Peer.(*tg.DialogPeer); ok {
			p.mu.Lock()
			if c := p.convs[peerKey(dp.Peer)]; c != nil {
				c.UnreadMark = u.Unread
				p.saveConv(peerKey(dp.Peer), c)
				p.listDirty = true
			}
			p.mu.Unlock()
		}
		return nil
	})
	// Archivé ou désarchivé ailleurs.
	d.OnFolderPeers(func(ctx context.Context, e tg.Entities, u *tg.UpdateFolderPeers) error {
		p.mu.Lock()
		for _, fp := range u.FolderPeers {
			k := peerKey(fp.Peer)
			if c := p.convs[k]; c != nil {
				c.Archived = fp.FolderID == 1
				p.saveConv(k, c)
			}
		}
		p.listDirty = true
		p.mu.Unlock()
		return nil
	})
	d.OnUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateUserTyping) error {
		p.typing(keyOf('u', u.UserID), keyOf('u', u.UserID), u.Action)
		return nil
	})
	d.OnChatUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateChatUserTyping) error {
		p.typing(keyOf('g', u.ChatID), peerKey(u.FromID), u.Action)
		return nil
	})
	d.OnChannelUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateChannelUserTyping) error {
		p.typing(keyOf('c', u.ChannelID), peerKey(u.FromID), u.Action)
		return nil
	})
}

// Un message reçu ou modifié (nouveau : il fait sonner l'interface).
func (p *Pont) incoming(mc tg.MessageClass, nouveau bool) {
	r := p.convert(mc)
	if r == nil {
		return
	}
	p.mu.Lock()
	c := p.getOrMakeConv(r.Conv)
	if nouveau && !r.FromMe && r.Kind != "admin" && r.Num > c.ReadInbox {
		c.Unread++
	}
	c.Hidden = false
	if nouveau && r.Num > c.TopID {
		c.TopID = r.Num
	}
	held := r.FromMe && p.inflight[r.Conv] > 0
	p.mu.Unlock()
	old := p.getMsg(r.ID)
	if old != nil {
		mergeOld(r, old)
	}
	p.putMsg(r)
	p.noteLast(r.Conv, r)
	if held && old == nil {
		// Mon envoi en cours : c'est lui qui l'annoncera, avec son tmp_id.
		return
	}
	p.emitMsg(r, nouveau && old == nil)
}

// Garde d'un ancien enregistrement ce que Telegram ne redonne pas.
func mergeOld(r, old *msgRow) {
	if r.TmpID == "" {
		r.TmpID = old.TmpID
	}
	if len(old.Attachments) > 0 && strings.HasPrefix(old.Attachments[0].MediaID, "local:") && len(r.Attachments) == len(old.Attachments) {
		r.Attachments, r.Media = old.Attachments, old.Media
	}
}

// Un message retiré : il reste, barré, comme sur les autres services.
func (p *Pont) retire(r *msgRow) {
	r.Text, r.Body, r.Media, r.Attachments, r.ReplyTo, r.ReplyText, r.Reactions = "🚫 Message retiré", "🚫 Message retiré", "", nil, "", "", nil
	p.putMsg(r)
	p.noteLastIfLatest(r.Conv, r)
	p.emitMsg(r, false)
}

func (p *Pont) readInbox(conv string, maxID, still int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.convs[conv]
	if c == nil {
		return
	}
	c.ReadInbox = max(c.ReadInbox, maxID)
	c.Unread = still
	if still == 0 {
		c.UnreadMark = false
	}
	p.saveConv(conv, c)
	p.listDirty = true
}

func (p *Pont) readOutbox(conv string, maxID int) {
	p.mu.Lock()
	c := p.convs[conv]
	if c == nil || maxID <= c.ReadOutbox {
		p.mu.Unlock()
		return
	}
	from := c.ReadOutbox
	c.ReadOutbox = maxID
	p.saveConv(conv, c)
	p.mu.Unlock()
	// Les coches « lu » des messages déjà affichés.
	for _, r := range p.queryMsgs(conv, 0, "", 30) {
		if r.FromMe && r.Num > from && r.Num <= maxID {
			p.emitMsg(r, false)
		}
	}
}

func (p *Pont) typing(conv, who string, action tg.SendMessageActionClass) {
	p.mu.Lock()
	me := keyOf('u', p.me)
	name := p.peerName(who)
	p.mu.Unlock()
	if who == me {
		return
	}
	on := true
	audio := false
	switch action.(type) {
	case *tg.SendMessageCancelAction:
		on = false
	case *tg.SendMessageRecordAudioAction, *tg.SendMessageUploadAudioAction:
		audio = true
	}
	ev := map[string]any{"conv": conv, "on": on}
	if name != "" {
		ev["name"] = name
	}
	if audio {
		ev["audio"] = true
	}
	emit("typing", ev)
}

// ---- Conversion ----

// À appeler avec p.mu.
func (p *Pont) getOrMakeConv(id string) *convState {
	c := p.convs[id]
	if c == nil {
		c = &convState{}
		p.convs[id] = c
	}
	return c
}

func reactionEmoji(rc tg.ReactionClass) string {
	if v, ok := rc.(*tg.ReactionEmoji); ok {
		return v.Emoticon
	}
	return ""
}

// À appeler avec p.mu : les réactions d'un message.
func (p *Pont) reactionsOf(mr tg.MessageReactions) []Reaction {
	var out []Reaction
	seen := map[string]int{}
	myRecent := false
	for _, rr := range mr.RecentReactions {
		e := reactionEmoji(rr.Reaction)
		if e == "" {
			continue
		}
		k := peerKey(rr.PeerID)
		out = append(out, Reaction{Emoji: e, Actor: k, FromMe: rr.My, Sender: p.peerName(k)})
		seen[e]++
		if rr.My {
			myRecent = true
		}
	}
	for _, rc := range mr.Results {
		e := reactionEmoji(rc.Reaction)
		if e == "" {
			continue
		}
		n := rc.Count - seen[e]
		mine := rc.ChosenOrder != 0 && !myRecent
		if mine {
			out = append(out, Reaction{Emoji: e, Actor: keyOf('u', p.me), FromMe: true, Sender: p.myName})
			n--
		}
		// Telegram ne donne que le nombre : des réactions sans auteur.
		for i := 0; i < min(n, 20); i++ {
			out = append(out, Reaction{Emoji: e})
		}
	}
	return out
}

// Le texte d'un message de service (quelqu'un entre, le titre change…).
func (p *Pont) actionText(a tg.MessageActionClass, who string) string {
	who = firstOf(who, "Quelqu'un")
	switch v := a.(type) {
	case *tg.MessageActionChatCreate:
		return who + " a créé le groupe « " + v.Title + " »"
	case *tg.MessageActionChannelCreate:
		return "Canal « " + v.Title + " » créé"
	case *tg.MessageActionChatEditTitle:
		return who + " a renommé le groupe « " + v.Title + " »"
	case *tg.MessageActionChatEditPhoto:
		return who + " a changé la photo du groupe"
	case *tg.MessageActionChatDeletePhoto:
		return who + " a retiré la photo du groupe"
	case *tg.MessageActionChatAddUser:
		var names []string
		for _, u := range v.Users {
			names = append(names, firstOf(p.peerName(keyOf('u', u)), "quelqu'un"))
		}
		return who + " a ajouté " + strings.Join(names, ", ")
	case *tg.MessageActionChatDeleteUser:
		k := keyOf('u', v.UserID)
		if p.peerName(k) == who {
			return who + " a quitté le groupe"
		}
		return who + " a retiré " + firstOf(p.peerName(k), "quelqu'un")
	case *tg.MessageActionChatJoinedByLink, *tg.MessageActionChatJoinedByRequest:
		return who + " a rejoint le groupe"
	case *tg.MessageActionPinMessage:
		return who + " a épinglé un message"
	case *tg.MessageActionPhoneCall:
		return "📞 Appel"
	case *tg.MessageActionContactSignUp:
		return who + " est maintenant sur Telegram"
	case *tg.MessageActionChatMigrateTo, *tg.MessageActionChannelMigrateFrom:
		return "Le groupe est devenu un supergroupe"
	case *tg.MessageActionHistoryClear:
		return "Historique effacé"
	}
	return ""
}

// Un message de Telegram vers une ligne du cache (nil : rien à montrer).
func (p *Pont) convert(mc tg.MessageClass) *msgRow {
	switch m := mc.(type) {
	case *tg.Message:
		return p.convertMsg(m)
	case *tg.MessageService:
		conv := peerKey(m.PeerID)
		if conv == "" {
			return nil
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		sender := conv
		if f, ok := m.GetFromID(); ok {
			sender = peerKey(f)
		}
		text := p.actionText(m.Action, p.peerName(sender))
		if text == "" {
			return nil
		}
		return &msgRow{
			Message: Message{ID: msgKey(conv, m.ID), Conv: conv, Text: text, FromMe: m.Out, Time: int64(m.Date) * 1000, Kind: "admin", Status: "INCOMING_COMPLETE"},
			Body:    text, SenderID: sender, Num: m.ID,
		}
	}
	return nil
}

func (p *Pont) convertMsg(m *tg.Message) *msgRow {
	conv := peerKey(m.PeerID)
	if conv == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	sender := conv
	if f, ok := m.GetFromID(); ok {
		sender = peerKey(f)
	} else if m.Out {
		sender = keyOf('u', p.me)
	}
	r := &msgRow{
		Message: Message{
			ID:     msgKey(conv, m.ID),
			Conv:   conv,
			FromMe: m.Out,
			Time:   int64(m.Date) * 1000,
			Kind:   "telegram",
			Status: "INCOMING_COMPLETE",
		},
		Body:     m.Message,
		SenderID: sender,
		Num:      m.ID,
	}
	if m.Out {
		r.Status = "OUTGOING_COMPLETE"
	}
	if m.Out {
		r.Sender = p.myName
	} else {
		r.Sender = p.peerName(sender)
		if m.PostAuthor != "" {
			r.Sender = m.PostAuthor
		}
	}
	if fw, ok := m.GetFwdFrom(); ok {
		from := fw.FromName
		if f, ok := fw.GetFromID(); ok {
			from = firstOf(p.peerName(peerKey(f)), from)
		}
		if from != "" {
			r.Body = "↗ Transféré de " + from + "\n" + r.Body
		}
	}
	if m.Media != nil {
		atts, extra := p.attachmentsOf(conv, m.ID, m.Media)
		r.Attachments = atts
		if extra != "" {
			r.Body = strings.TrimSpace(r.Body + "\n" + extra)
		}
		r.Media = mediaSummary(atts)
		r.LinkPreview = apercuTG(conv, m.ID, m.Media)
	}
	if rs, ok := m.GetReactions(); ok {
		r.Reactions = p.reactionsOf(rs)
	}
	r.Text = r.Body
	if rh, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok && rh.ReplyToMsgID != 0 {
		rconv := conv
		if rp, ok := rh.GetReplyToPeerID(); ok {
			rconv = peerKey(rp)
		}
		rid := msgKey(rconv, rh.ReplyToMsgID)
		quoted, who := rh.QuoteText, ""
		p.mu.Unlock()
		orig := p.getMsg(rid)
		p.mu.Lock()
		if orig != nil {
			quoted = cmp.Or(quoted, snippet(orig.Body, orig.Attachments))
			if orig.FromMe {
				who = "Vous"
			} else {
				who = firstOf(p.peerName(orig.SenderID), orig.Sender)
			}
		}
		if quoted != "" {
			setReply(r, rid, who, shorten(quoted, 80))
		} else {
			r.ReplyTo = rid
		}
	}
	return r
}

// À appeler avec p.mu : le format de l'interface, avec les noms et les coches à jour.
func (p *Pont) msgOut(r *msgRow) Message {
	m := r.Message
	if r.SenderID != "" && !m.FromMe && m.Sender == "" {
		m.Sender = p.peerName(r.SenderID)
	}
	if m.FromMe && r.Num > 0 {
		if c := p.convs[r.Conv]; c != nil && r.Num <= c.ReadOutbox {
			m.Status = "OUTGOING_READ"
		}
	}
	m.Reactions = append([]Reaction(nil), m.Reactions...)
	for i := range m.Reactions {
		if n := p.peerName(m.Reactions[i].Actor); n != "" {
			m.Reactions[i].Sender = n
		}
	}
	return m
}

func setReply(r *msgRow, id, who, quoted string) {
	r.ReplyTo = id
	line := quoted
	if who != "" {
		line = who + " : " + quoted
	}
	r.ReplyText = line
	r.Text = quoteLine(line) + r.Body
}

func quoteLine(s string) string { return "↪ " + s + "\n" }

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n-1]) + "…"
	}
	return s
}

func snippet(text string, atts []Attachment) string {
	if text != "" {
		return shorten(text, 80)
	}
	return previewOf("", atts)
}

func mediaSummary(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	return firstOf(atts[0].Name, atts[0].Mime, "pièce jointe")
}

func previewOf(text string, atts []Attachment) string {
	if len(atts) == 0 {
		return text
	}
	switch mime := atts[0].Mime; {
	case atts[0].Name == "autocollant":
		return firstOf(text, "Autocollant")
	case strings.HasPrefix(mime, "image/"):
		return firstOf(text, "📷 Photo")
	case strings.HasPrefix(mime, "video/"):
		return firstOf(text, "🎬 Vidéo")
	case strings.HasPrefix(mime, "audio/"):
		return firstOf(text, "🎤 Message vocal")
	default:
		return firstOf(text, "📎 "+firstOf(atts[0].Name, "Pièce jointe"))
	}
}

// Garde le dernier message d'un fil pour l'aperçu.
func (p *Pont) noteLast(conv string, r *msgRow) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.convs[conv]
	if c == nil || r.Time < c.LastTime {
		return
	}
	c.LastText, c.LastFromMe, c.LastTime = previewOf(r.Body, r.Attachments), r.FromMe, r.Time
	if r.Kind == "admin" && c.LastText == "" {
		c.LastText = r.Text
	}
	p.listDirty = true
	p.saveConv(conv, c)
}

func (p *Pont) noteLastIfLatest(conv string, r *msgRow) {
	p.mu.Lock()
	c := p.convs[conv]
	latest := c != nil && r.Time >= c.LastTime
	p.mu.Unlock()
	if latest {
		p.noteLast(conv, r)
	}
}

// Annonce un message à l'interface. Une mise à jour (nouveau=false) n'est
// envoyée que si l'interface a déjà ce message affiché, pour ne pas faire
// sonner une notification pour un vieux message.
func (p *Pont) emitMsg(r *msgRow, nouveau bool) {
	p.mu.Lock()
	oldest, ok := p.loaded[r.Conv]
	if !nouveau && (!ok || r.Time < oldest) {
		p.mu.Unlock()
		return
	}
	out := p.msgOut(r)
	p.mu.Unlock()
	emit("message", map[string]any{"item": out})
}

func (p *Pont) emitNow(r *msgRow) {
	p.mu.Lock()
	out := p.msgOut(r)
	p.mu.Unlock()
	emit("message", map[string]any{"item": out})
}

var _ = time.Now
