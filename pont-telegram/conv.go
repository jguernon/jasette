package main

// Les fils (liste, archives), l'historique, l'envoi, la lecture, la
// recherche et les nouvelles conversations.

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// ---- La liste des fils ----

func (p *Pont) conv(id string) *convState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.convs[id]
}

// À appeler avec p.mu.
func (p *Pont) convOut(id string, c *convState) Conversation {
	pe := p.peers[id]
	name := ""
	group, readOnly := false, false
	var members []string
	if pe != nil {
		name = pe.Name
		switch pe.Kind {
		case 'u':
			if pe.Self {
				name = "Messages enregistrés"
			}
			if name != "" {
				members = []string{name}
			}
		case 'g', 'c':
			group = true
			readOnly = !pe.CanPost
		}
	}
	if name == "" {
		name = "Conversation Telegram"
	}
	large, kind := grandEspace(pe)
	return Conversation{
		ID:       id,
		Name:     name,
		Preview:  c.LastText,
		FromMe:   c.LastFromMe,
		Time:     max(c.LastTime, c.LastActivity),
		Unread:   c.Unread > 0 || c.UnreadMark,
		Group:    group,
		Members:  orEmpty(members),
		ReadOnly: readOnly,
		Archived: c.Archived,
		Large:    large,
		Kind:     kind,
		Mentions: c.Mentions,
	}
}

// Le classement de l'onglet Groupes (§5.13) : un canal de diffusion
// (« channel ») ou un supergroupe (« supergroup ») ; une personne ou un
// groupe simple ('g') n'est pas un grand espace.
func grandEspace(pe *peer) (bool, string) {
	if pe == nil || pe.Kind != 'c' {
		return false, ""
	}
	if pe.Broadcast {
		return true, "channel"
	}
	return true, "supergroup"
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
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
	p.sendList(n, false, false)
	if arch {
		p.sendList(n, true, false)
	}
}

func (p *Pont) listConversations(n int, archive bool) {
	p.mu.Lock()
	first := archive && !p.archiveWanted
	if archive {
		p.archiveWanted = true
	} else {
		p.listCount = max(n, p.listCount)
	}
	p.mu.Unlock()
	p.sendList(n, archive, true)
	if first {
		go p.refreshFolder(1)
	}
}

func (p *Pont) sendList(n int, archive, avatars bool) {
	p.mu.Lock()
	items := []Conversation{}
	for id, c := range p.convs {
		if c.Hidden || (!c.InDialogs && c.LastTime == 0 && c.LastActivity == 0) {
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
	ev := map[string]any{"items": items}
	if archive {
		ev["folder"] = "archive"
	}
	emit("conversations", ev)
	if avatars {
		keys := make([]string, 0, len(items))
		for _, it := range items {
			keys = append(keys, it.ID)
		}
		go p.avatars(keys)
	}
}

// Relit la liste des fils chez Telegram (boîte de réception, et archives si
// l'interface les a demandées).
func (p *Pont) refreshDialogs(force bool) {
	p.mu.Lock()
	if !force && time.Since(p.lastDialogs) < 20*time.Second {
		p.mu.Unlock()
		return
	}
	p.lastDialogs = time.Now()
	arch := p.archiveWanted
	p.mu.Unlock()
	p.refreshFolder(0)
	if arch {
		p.refreshFolder(1)
	}
	p.emitList()
	p.mu.Lock()
	n := p.listCount
	p.mu.Unlock()
	go p.avatarsOfList(n)
}

func (p *Pont) avatarsOfList(n int) {
	p.mu.Lock()
	type kv struct {
		k string
		t int64
	}
	var l []kv
	for k, c := range p.convs {
		if !c.Hidden {
			l = append(l, kv{k, max(c.LastTime, c.LastActivity)})
		}
	}
	p.mu.Unlock()
	slices.SortFunc(l, func(a, b kv) int { return cmp.Compare(b.t, a.t) })
	keys := []string{}
	for i := 0; i < len(l) && i < n; i++ {
		keys = append(keys, l[i].k)
	}
	p.avatars(keys)
}

func (p *Pont) refreshFolder(folder int) {
	api := p.tgAPI()
	if api == nil {
		return
	}
	p.mu.Lock()
	want := p.listCount
	p.mu.Unlock()
	want = min(max(want, 50), 200)
	var offPeer tg.InputPeerClass = &tg.InputPeerEmpty{}
	offID, offDate := 0, 0
	seen := 0
	for seen < want {
		req := &tg.MessagesGetDialogsRequest{OffsetPeer: offPeer, OffsetID: offID, OffsetDate: offDate, Limit: min(100, want-seen)}
		req.SetFolderID(folder)
		ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
		res, err := api.MessagesGetDialogs(ctx, req)
		annule()
		if err != nil {
			log.Warn().Err(err).Int("dossier", folder).Msg("liste des fils")
			return
		}
		var dialogs []tg.DialogClass
		var msgs []tg.MessageClass
		total := -1
		switch v := res.(type) {
		case *tg.MessagesDialogs:
			p.noteEntities(v.Users, v.Chats)
			dialogs, msgs = v.Dialogs, v.Messages
		case *tg.MessagesDialogsSlice:
			p.noteEntities(v.Users, v.Chats)
			dialogs, msgs, total = v.Dialogs, v.Messages, v.Count
		default:
			return
		}
		byKey := map[string]*msgRow{}
		for _, m := range msgs {
			if r := p.convert(m); r != nil {
				byKey[r.ID] = r
			}
		}
		var last *tg.Dialog
		for _, dc := range dialogs {
			d, ok := dc.(*tg.Dialog)
			if !ok {
				continue
			}
			last = d
			k := peerKey(d.Peer)
			p.mu.Lock()
			c := p.getOrMakeConv(k)
			c.InDialogs = true
			c.Archived = folder == 1
			c.Pinned = d.Pinned
			c.ReadInbox, c.ReadOutbox = d.ReadInboxMaxID, d.ReadOutboxMaxID
			c.Unread, c.UnreadMark = d.UnreadCount, d.UnreadMark
			c.Mentions = d.UnreadMentionsCount
			c.TopID = max(c.TopID, d.TopMessage)
			c.Hidden = false
			p.saveConv(k, c)
			p.listDirty = true
			p.mu.Unlock()
			if r := byKey[msgKey(k, d.TopMessage)]; r != nil {
				if old := p.getMsg(r.ID); old != nil {
					mergeOld(r, old)
				}
				p.putMsg(r)
				p.noteLast(k, r)
			}
		}
		seen += len(dialogs)
		if last == nil || len(dialogs) == 0 || (total >= 0 && seen >= total) || total < 0 {
			break
		}
		ip, err := p.inputPeer(peerKey(last.Peer))
		if err != nil {
			break
		}
		offPeer, offID = ip, last.TopMessage
		if r := byKey[msgKey(peerKey(last.Peer), last.TopMessage)]; r != nil {
			offDate = int(r.Time / 1000)
		}
	}
}

// ---- L'historique ----

// Lit l'historique chez Telegram (sans le marquer comme lu : seul « read »
// le fait), le garde, et le renvoie. Hors connexion : le cache.
func (p *Pont) messages(cmd Cmd) {
	count := int(cmd.Count)
	if count <= 0 {
		count = 50
	}
	conv := cmd.Conv
	older := cmd.Before != ""
	var rows []*msgRow
	more := false
	api := p.tgAPI()
	fetched := false
	if api != nil {
		if ip, err := p.inputPeer(conv); err == nil {
			offID := 0
			if older {
				if _, n, ok := parseMsgKey(cmd.Before); ok {
					offID = n
				}
			}
			ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
			res, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: ip, OffsetID: offID, Limit: min(count, 100)})
			annule()
			if err != nil {
				log.Warn().Err(err).Str("fil", conv).Msg("historique")
				if tgerr.Is(err, "CHANNEL_PRIVATE", "CHAT_FORBIDDEN", "PEER_ID_INVALID") {
					emitErr("messages", errors.New("Telegram : cette conversation n'est plus accessible"))
				}
			} else {
				var msgs []tg.MessageClass
				switch v := res.(type) {
				case *tg.MessagesMessages:
					p.noteEntities(v.Users, v.Chats)
					msgs = v.Messages
				case *tg.MessagesMessagesSlice:
					p.noteEntities(v.Users, v.Chats)
					msgs = v.Messages
				case *tg.MessagesChannelMessages:
					p.noteEntities(v.Users, v.Chats)
					msgs = v.Messages
				}
				fetched = true
				more = len(msgs) >= min(count, 100)
				// Telegram rend du plus récent au plus vieux.
				for i := len(msgs) - 1; i >= 0; i-- {
					r := p.convert(msgs[i])
					if r == nil {
						continue
					}
					if old := p.getMsg(r.ID); old != nil {
						mergeOld(r, old)
					}
					p.putMsg(r)
					rows = append(rows, r)
				}
				if len(rows) > 0 && !older {
					p.mu.Lock()
					p.getOrMakeConv(conv)
					p.mu.Unlock()
					p.noteLast(conv, rows[len(rows)-1])
				}
			}
		}
	}
	if !fetched {
		rows = p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
		more = len(rows) >= count
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
	emit("messages", map[string]any{"conv": conv, "items": items, "older": older, "more": more})
}

// ---- L'envoi ----

func randomID() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int64(binary.LittleEndian.Uint64(b[:]) >> 1)
}

func explainSend(err error) error {
	switch {
	case tgerr.Is(err, "CHAT_WRITE_FORBIDDEN", "CHAT_ADMIN_REQUIRED"):
		return errors.New("vous ne pouvez pas écrire dans cette conversation")
	case tgerr.Is(err, "USER_IS_BLOCKED", "YOU_BLOCKED_USER"):
		return errors.New("cette personne est bloquée")
	case tgerr.Is(err, "PEER_FLOOD"):
		return errors.New("Telegram limite vos envois pour l'instant")
	case tgerr.Is(err, "MEDIA_EMPTY", "FILE_PARTS_INVALID"):
		return errors.New("Telegram a refusé le fichier")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("Telegram ne répond pas (délai dépassé)")
	}
	if d, ok := tgerr.AsFloodWait(err); ok {
		return fmt.Errorf("Telegram limite les envois : réessayez dans %s", d.Round(time.Second))
	}
	return fmt.Errorf("Telegram : %w", err)
}

func (p *Pont) send(cmd Cmd) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return
	}
	p.sendCommon(cmd, text, "", "")
}

// Le numéro du message envoyé, dans la réponse de Telegram.
func sentID(res tg.UpdatesClass, rid int64) (int, int) {
	switch v := res.(type) {
	case *tg.UpdateShortSentMessage:
		return v.ID, v.Date
	case *tg.Updates:
		return idIn(v.Updates, rid), v.Date
	case *tg.UpdatesCombined:
		return idIn(v.Updates, rid), v.Date
	}
	return 0, 0
}

func idIn(l []tg.UpdateClass, rid int64) int {
	for _, u := range l {
		if m, ok := u.(*tg.UpdateMessageID); ok && m.RandomID == rid {
			return m.ID
		}
	}
	return 0
}

func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	tmp := cmp.Or(cmd.TmpID, fmt.Sprintf("tmp_%d", time.Now().UnixNano()))
	fail := func(err error) {
		log.Warn().Err(err).Str("fil", cmd.Conv).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	api := p.tgAPI()
	if api == nil {
		fail(errors.New("Telegram n'est pas connecté"))
		return
	}
	ip, err := p.inputPeer(cmd.Conv)
	if err != nil {
		fail(err)
		return
	}
	var reply tg.InputReplyToClass
	var replyRow *msgRow
	if cmd.ReplyTo != "" {
		if c, n, ok := parseMsgKey(cmd.ReplyTo); ok && c == cmd.Conv {
			reply = &tg.InputReplyToMessage{ReplyToMsgID: n}
			replyRow = p.getMsg(cmd.ReplyTo)
		}
	}
	ctx, annule := context.WithTimeout(p.ctx, 5*time.Minute)
	defer annule()
	p.mu.Lock()
	p.inflight[cmd.Conv]++
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.inflight[cmd.Conv]--
		p.mu.Unlock()
	}()

	rid := randomID()
	var res tg.UpdatesClass
	var atts []Attachment
	if path != "" {
		media, local, err := p.uploadMedia(ctx, api, path, mime)
		if err != nil {
			fail(fmt.Errorf("envoi du fichier : %w", err))
			return
		}
		atts = local
		req := &tg.MessagesSendMediaRequest{Peer: ip, Media: media, Message: text, RandomID: rid}
		if reply != nil {
			req.SetReplyTo(reply)
		}
		res, err = api.MessagesSendMedia(ctx, req)
		if err != nil {
			fail(explainSend(err))
			return
		}
	} else {
		req := &tg.MessagesSendMessageRequest{Peer: ip, Message: text, RandomID: rid}
		if reply != nil {
			req.SetReplyTo(reply)
		}
		res, err = api.MessagesSendMessage(ctx, req)
		if err != nil {
			fail(explainSend(err))
			return
		}
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	num, date := sentID(res, rid)
	ts := time.Now().UnixMilli()
	if date > 0 {
		ts = int64(date) * 1000
	}
	id := msgKey(cmd.Conv, num)
	if num == 0 {
		id = tmp
	}
	r := p.getMsg(id)
	if r == nil {
		p.mu.Lock()
		r = &msgRow{
			Message:  Message{ID: id, Conv: cmd.Conv, FromMe: true, Time: ts, Status: "OUTGOING_COMPLETE", Kind: "telegram", Sender: p.myName},
			Body:     text,
			SenderID: keyOf('u', p.me),
			Num:      num,
		}
		p.mu.Unlock()
		r.Text = text
		if replyRow != nil {
			who := "Vous"
			if !replyRow.FromMe {
				who = replyRow.Sender
			}
			setReply(r, replyRow.ID, who, snippet(replyRow.Body, replyRow.Attachments))
		}
	}
	r.TmpID = tmp
	if len(atts) > 0 {
		// La copie locale : l'interface l'a sans téléchargement.
		r.Attachments, r.Media = atts, mediaSummary(atts)
	}
	p.mu.Lock()
	c := p.getOrMakeConv(cmd.Conv)
	c.Hidden = false
	c.LastActivity = max(c.LastActivity, r.Time)
	c.TopID = max(c.TopID, num)
	p.saveConv(cmd.Conv, c)
	p.mu.Unlock()
	p.putMsg(r)
	p.noteLast(cmd.Conv, r)
	p.emitNow(r)
}

// ---- Lu ----

// Marque le fil comme lu chez Telegram (et donc sur les autres appareils).
// La seule commande qui le fait.
func (p *Pont) markRead(cmd Cmd) {
	p.mu.Lock()
	c := p.convs[cmd.Conv]
	if c == nil {
		p.mu.Unlock()
		return
	}
	top := max(c.TopID, c.ReadInbox)
	wasUnread := c.Unread > 0 || c.UnreadMark
	mentions := c.Mentions > 0
	if !wasUnread && !mentions {
		// Rien de non lu ni de mention : aucune requête (« Tout marquer comme
		// lu » n'envoie que celles qui servent).
		p.mu.Unlock()
		return
	}
	c.Unread, c.UnreadMark, c.Mentions = 0, false, 0
	c.ReadInbox = top
	p.saveConv(cmd.Conv, c)
	p.listDirty = true
	p.mu.Unlock()
	// Plusieurs « lu » d'un coup (Tout marquer comme lu) : étalés.
	if attendreLu(p.ctx) != nil {
		return
	}
	api := p.tgAPI()
	if api == nil {
		return
	}
	ip, err := p.inputPeer(cmd.Conv)
	if err != nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if ch, ok := ip.(*tg.InputPeerChannel); ok {
		_, err = api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}, MaxID: top})
	} else {
		_, err = api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: ip, MaxID: top})
	}
	if err != nil {
		log.Debug().Err(err).Msg("lu")
	}
	// Lire l'historique n'efface pas les mentions chez Telegram : sans cet
	// appel, la prochaine liste des fils les rendrait (seulement s'il y en a).
	if mentions {
		if _, err := api.MessagesReadMentions(ctx, &tg.MessagesReadMentionsRequest{Peer: ip}); err != nil {
			log.Debug().Err(err).Msg("mentions lues")
		}
	}
	if wasUnread {
		_, _ = api.MessagesMarkDialogUnread(ctx, &tg.MessagesMarkDialogUnreadRequest{Peer: &tg.InputDialogPeer{Peer: ip}, Unread: false})
	}
}

// « Est en train d'écrire ».
func (p *Pont) sendTyping(cmd Cmd) {
	api := p.tgAPI()
	if api == nil {
		return
	}
	ip, err := p.inputPeer(cmd.Conv)
	if err != nil {
		return
	}
	var a tg.SendMessageActionClass = &tg.SendMessageTypingAction{}
	if !cmd.On {
		a = &tg.SendMessageCancelAction{}
	}
	ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
	defer annule()
	_, _ = api.MessagesSetTyping(ctx, &tg.MessagesSetTypingRequest{Peer: ip, Action: a})
}

// ---- Archives ----

// Archive ou désarchive un fil (dossier 1 de Telegram, comme dans l'app).
func (p *Pont) archive(cmd Cmd) {
	api := p.tgAPI()
	if api == nil {
		emitErr("archive", errors.New("Telegram n'est pas connecté"))
		return
	}
	ip, err := p.inputPeer(cmd.Conv)
	if err != nil {
		emitErr("archive", err)
		return
	}
	folder := 0
	if cmd.Archived {
		folder = 1
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if _, err := api.FoldersEditPeerFolders(ctx, []tg.InputFolderPeer{{Peer: ip, FolderID: folder}}); err != nil {
		emitErr("archive", fmt.Errorf("archivage : %w", explainSend(err)))
		return
	}
	p.mu.Lock()
	c := p.getOrMakeConv(cmd.Conv)
	c.Archived = cmd.Archived
	p.saveConv(cmd.Conv, c)
	item := p.convOut(cmd.Conv, c)
	p.listDirty = true
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item})
}

// ---- Nouvelles conversations et recherche ----

func isPhone(s string) bool {
	s = strings.TrimPrefix(s, "+")
	if len(s) < 7 || len(s) > 15 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Ouvre un fil : un nom d'utilisateur (@nom), un numéro (s'il est dans les
// contacts ou visible), ou un fil déjà connu ; sinon une recherche.
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimSpace(cmp.Or(cmd.Number, cmd.Text))
	if q == "" {
		return
	}
	api := p.tgAPI()
	if api == nil {
		emitErr("new", errors.New("Telegram n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	var conv string
	digits := phoneChars.ReplaceAllString(q, "")
	if _, _, ok := splitKey(q); ok && p.peerKnown(q) {
		conv = q
	} else if isPhone(digits) {
		res, err := api.ContactsResolvePhone(ctx, strings.TrimPrefix(digits, "+"))
		if err != nil {
			emitErr("new", fmt.Errorf("%s n'est pas joignable sur Telegram (il faut qu'il soit dans vos contacts ou que son numéro soit visible)", q))
			return
		}
		p.noteEntities(res.Users, res.Chats)
		conv = peerKey(res.Peer)
	} else if u := strings.TrimPrefix(q, "@"); len(u) >= 4 && !strings.ContainsAny(u, " ") {
		res, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: u})
		if err != nil {
			p.search(q)
			return
		}
		p.noteEntities(res.Users, res.Chats)
		conv = peerKey(res.Peer)
	} else {
		p.search(q)
		return
	}
	if conv == "" {
		return
	}
	p.mu.Lock()
	c := p.getOrMakeConv(conv)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	c.Hidden = false
	p.saveConv(conv, c)
	item := p.convOut(conv, c)
	p.listDirty = true
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{conv})
}

func (p *Pont) peerKnown(k string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peers[k] != nil
}

// Recherche de personnes, groupes et canaux : ceux que l'on connaît, puis
// l'annuaire de Telegram (contacts.search).
func (p *Pont) search(q string) {
	type item struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Avatar string `json:"avatar,omitempty"`
	}
	items := []item{}
	if q == "" {
		emit("search", map[string]any{"query": q, "items": items})
		return
	}
	if api := p.tgAPI(); api != nil && len([]rune(q)) >= 2 {
		ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
		res, err := api.ContactsSearch(ctx, &tg.ContactsSearchRequest{Q: q, Limit: 30})
		annule()
		if err == nil {
			p.noteEntities(res.Users, res.Chats)
		}
	}
	lq := strings.ToLower(q)
	p.mu.Lock()
	for id, c := range p.peers {
		if c.Self || c.Deleted || c.Name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(c.Name), lq) || (c.Username != "" && strings.Contains(strings.ToLower(c.Username), strings.TrimPrefix(lq, "@"))) {
			name := c.Name
			if c.Username != "" {
				name = fmt.Sprintf("%s (@%s)", c.Name, c.Username)
			}
			items = append(items, item{ID: id, Name: name})
		}
	}
	p.mu.Unlock()
	slices.SortFunc(items, func(a, b item) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	if len(items) > 50 {
		items = items[:50]
	}
	emit("search", map[string]any{"query": q, "items": items})
}

// Les « lu » partent un à la fois, au moins rythmeLuEcart l'un après l'autre.
const rythmeLuEcart = 400 * time.Millisecond

var rythmeLu struct {
	sync.Mutex
	next time.Time
}

func attendreLu(ctx context.Context) error {
	rythmeLu.Lock()
	at := rythmeLu.next
	if now := time.Now(); at.Before(now) {
		at = now
	}
	rythmeLu.next = at.Add(rythmeLuEcart)
	rythmeLu.Unlock()
	select {
	case <-time.After(time.Until(at)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
