package main

// Les tables « lightspeed » que Meta pousse (page, socket, réponses) :
// fils, contacts, participants, messages, pièces jointes, réactions, lectures.

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

// Les autocollants « pouce » de Messenger, montrés comme l'emoji.
var thumbsUp = map[int64]bool{369239263222822: true, 369239343222814: true, 369239383222810: true}

func (p *Pont) handleTable(tbl *table.LSTable, initial bool) {
	if tbl == nil {
		return
	}
	p.tableMu.Lock()
	defer p.tableMu.Unlock()

	touched := map[string]bool{}
	p.mu.Lock()
	me := p.me

	// Contacts.
	for _, ct := range tbl.LSDeleteThenInsertContact {
		p.upsertContact(ct.Id, ct.Name, ct.FirstName, ct.GetAvatarURL())
	}
	for _, ct := range tbl.LSVerifyContactRowExists {
		p.upsertContact(ct.ContactId, ct.Name, "", ct.ProfilePictureUrl)
		if ct.IsSelf && ct.ProfilePictureUrl != "" {
			p.myAvatar = ct.ProfilePictureUrl
		}
	}

	// Fils remplacés par un fil chiffré.
	mapsChanged := false
	for _, m := range tbl.LSUpdateThreadAuthorityAndMappingWithOTIDFromJID {
		if m.ThreadKey != 0 && m.ThreadJID != 0 && m.ThreadKey != m.ThreadJID {
			p.threadMap[m.ThreadKey] = m.ThreadJID
			mapsChanged = true
		}
	}
	for _, h := range tbl.LSVerifyHybridThreadExists {
		if h.ThreadKey != 0 && h.ThreadJID != 0 && h.ThreadKey != h.ThreadJID {
			p.threadMap[h.ThreadKey] = h.ThreadJID
			mapsChanged = true
			if old := p.convs[idStr(h.ThreadKey)]; old != nil && !old.Type.IsWhatsApp() {
				old.Hidden = true
				p.saveConv(idStr(h.ThreadKey), old)
			}
			c := p.getOrMakeConv(h.ThreadJID, h.ThreadType)
			c.LastActivity = max(c.LastActivity, h.LastActivityTimestampMS)
			c.ReadWM = max(c.ReadWM, h.LastReadWatermarkTimestampMS)
			touched[idStr(h.ThreadJID)] = true
		}
	}
	for _, cut := range tbl.LSCreateOpenToE2EEThreadLink {
		if cut.OpenThreadID != 0 && cut.ArmadilloThreadID != 0 {
			p.openThread[cut.OpenThreadID] = cut.ArmadilloThreadID
			mapsChanged = true
			if cut.OpenThreadID != cut.ArmadilloThreadID {
				if old := p.convs[idStr(cut.OpenThreadID)]; old != nil && !old.Type.IsWhatsApp() {
					old.Hidden = true
					p.saveConv(idStr(cut.OpenThreadID), old)
				}
			}
		}
	}
	if mapsChanged {
		p.saveKV("thread_map", p.threadMap)
		p.saveKV("open_threads", p.openThread)
	}

	// Fils.
	for _, th := range tbl.LSDeleteThenInsertThread {
		key := p.canon(th.ThreadKey)
		c := p.getOrMakeConv(key, th.ThreadType)
		c.Name = th.ThreadName
		if th.Snippet != "" || th.LastActivityTimestampMs >= c.LastActivity {
			c.Snippet = th.Snippet
			c.SnippetFrom = th.SnippetSenderContactId
		}
		c.Picture = th.ThreadPictureUrl
		if th.FolderName != "" {
			c.Folder = th.FolderName
		}
		c.LastActivity = max(c.LastActivity, th.LastActivityTimestampMs)
		c.ReadWM = max(c.ReadWM, th.LastReadWatermarkTimestampMs)
		c.Hidden = false
		touched[idStr(key)] = true
	}
	for _, th := range tbl.LSUpdateOrInsertThread {
		key := p.canon(th.ThreadKey)
		c := p.getOrMakeConv(key, th.ThreadType)
		if th.ThreadName != "" {
			c.Name = th.ThreadName
		}
		if th.Snippet != "" {
			c.Snippet = th.Snippet
			c.SnippetFrom = th.SnippetSenderContactId
		}
		if th.ThreadPictureUrl != "" {
			c.Picture = th.ThreadPictureUrl
		}
		if th.FolderName != "" {
			c.Folder = th.FolderName
		}
		c.LastActivity = max(c.LastActivity, th.LastActivityTimestampMs)
		c.ReadWM = max(c.ReadWM, th.LastReadWatermarkTimestampMs)
		c.Hidden = false
		touched[idStr(key)] = true
	}
	for _, th := range tbl.LSVerifyThreadExists {
		key := p.canon(th.ThreadKey)
		c := p.getOrMakeConv(key, th.ThreadType)
		if th.FolderName != "" && c.Folder == "" {
			c.Folder = th.FolderName
		}
		touched[idStr(key)] = true
	}
	for _, s := range tbl.LSUpdateThreadSnippet {
		key := p.canon(s.ThreadKey)
		if c := p.convs[idStr(key)]; c != nil {
			c.Snippet = s.Snippet
			c.SnippetFrom = s.SnippetSenderContactId
			touched[idStr(key)] = true
		}
	}
	for _, m := range tbl.LSMoveThreadToArchivedFolder {
		if c := p.convs[idStr(p.canon(m.ThreadKey))]; c != nil {
			c.Folder = "archived"
			touched[idStr(p.canon(m.ThreadKey))] = true
		}
	}
	for _, m := range tbl.LSMoveThreadToInboxAndUpdateParent {
		if c := p.convs[idStr(p.canon(m.ThreadKey))]; c != nil {
			c.Folder = "inbox"
			touched[idStr(p.canon(m.ThreadKey))] = true
		}
	}
	for _, m := range tbl.LSMoveThreadToE2EECutoverFolder {
		if c := p.convs[idStr(m.ThreadKey)]; c != nil && !c.Type.IsWhatsApp() {
			c.Hidden = true
			touched[idStr(m.ThreadKey)] = true
		}
	}
	for _, d := range tbl.LSDeleteThread {
		if c := p.convs[idStr(p.canon(d.ThreadKey))]; c != nil {
			c.Hidden = true
			touched[idStr(p.canon(d.ThreadKey))] = true
		}
	}

	// Participants.
	for _, pa := range tbl.LSAddParticipantIdToGroupThread {
		key := p.canon(pa.ThreadKey)
		c := p.convs[idStr(key)]
		if c == nil {
			c = p.getOrMakeConv(key, table.UNKNOWN_THREAD_TYPE)
		}
		if !containsID(c.Members, pa.ContactId) {
			c.Members = append(c.Members, pa.ContactId)
		}
		if pa.Nickname != "" {
			if ct := p.contacts[pa.ContactId]; ct == nil || ct.Name == "" {
				p.upsertContact(pa.ContactId, pa.Nickname, "", "")
			}
		}
		touched[idStr(key)] = true
	}
	for _, r := range tbl.LSRemoveParticipantFromThread {
		key := p.canon(r.ThreadKey)
		if c := p.convs[idStr(key)]; c != nil {
			c.Members = removeID(c.Members, r.ParticipantId)
			touched[idStr(key)] = true
		}
	}

	// Lectures : les miennes (fil lu) et celles des autres (mes messages lus).
	for _, r := range tbl.LSMarkThreadRead {
		if c := p.convs[idStr(p.canon(r.ThreadKey))]; c != nil {
			c.ReadWM = max(c.ReadWM, r.LastReadWatermarkTimestampMs)
			touched[idStr(p.canon(r.ThreadKey))] = true
		}
	}
	for _, r := range tbl.LSMarkThreadReadV2 {
		if c := p.convs[idStr(p.canon(r.ThreadKey))]; c != nil {
			c.ReadWM = max(c.ReadWM, r.LastReadWatermarkTimestampMs)
			touched[idStr(p.canon(r.ThreadKey))] = true
		}
	}
	type receipt struct {
		conv string
		ts   int64
	}
	var receipts []receipt
	for _, r := range tbl.LSUpdateReadReceipt {
		if r.ContactId != me && r.ReadWatermarkTimestampMs > 0 {
			receipts = append(receipts, receipt{idStr(p.canon(r.ThreadKey)), r.ReadWatermarkTimestampMs})
		}
	}

	// Plages de messages : y en a-t-il de plus vieux ?
	for _, rng := range tbl.LSInsertNewMessageRange {
		if c := p.convs[idStr(p.canon(rng.ThreadKey))]; c != nil {
			more := rng.HasMoreBefore
			c.MoreBefore = &more
		}
	}
	type typing struct {
		conv string
		on   bool
	}
	var typings []typing
	for _, t := range tbl.LSUpdateTypingIndicator {
		if t.SenderId != me {
			typings = append(typings, typing{idStr(p.canon(t.ThreadKey)), t.IsTyping})
		}
	}
	for id := range touched {
		if c := p.convs[id]; c != nil {
			p.saveConv(id, c)
		}
	}
	if len(touched) > 0 {
		p.listDirty = true
	}
	p.mu.Unlock()

	// Messages.
	upserts, inserts := tbl.WrapMessages()
	var live []*msgRow
	for _, up := range upserts {
		for _, wm := range up.Messages {
			r := p.fbRow(wm)
			if r == nil {
				continue
			}
			if p.storeRow(r, false) && !initial {
				live = append(live, r)
			}
		}
	}
	for _, wm := range inserts {
		r := p.fbRow(wm)
		if r == nil {
			continue
		}
		if p.storeRow(r, true) && !initial {
			live = append(live, r)
		}
	}
	for _, r := range live {
		p.emitMsg(r, true)
	}

	// Modifications de messages déjà connus.
	for _, e := range tbl.LSEditMessage {
		if r := p.getMsg(e.MessageID); r != nil {
			r.Text = e.Text
			p.putMsg(r)
			p.emitMsg(r, false)
		}
	}
	for _, d := range tbl.LSDeleteMessage {
		if r := p.getMsg(d.MessageId); r != nil {
			r.Text, r.Media, r.Attachments = "🚫 Message retiré", "", nil
			p.putMsg(r)
			p.emitMsg(r, false)
		}
	}
	for _, re := range tbl.LSUpsertReaction {
		if r := p.getMsg(re.MessageId); r != nil {
			r.Reactions = setReaction(r.Reactions, re.ActorId, re.Reaction, re.ActorId == me)
			p.putMsg(r)
			p.emitMsg(r, false)
		}
	}
	for _, re := range tbl.LSDeleteReaction {
		if r := p.getMsg(re.MessageId); r != nil {
			r.Reactions = setReaction(r.Reactions, re.ActorId, "", false)
			p.putMsg(r)
			p.emitMsg(r, false)
		}
	}
	for _, rc := range receipts {
		p.markMineRead(rc.conv, rc.ts, "OUTGOING_READ")
	}
	for _, d := range tbl.LSUpdateDeliveryReceipt {
		if d.ContactId != me && d.DeliveredWatermarkTimestampMs > 0 {
			p.mu.Lock()
			conv := idStr(p.canon(d.ThreadKey))
			p.mu.Unlock()
			p.markMineRead(conv, d.DeliveredWatermarkTimestampMs, "OUTGOING_DELIVERED")
		}
	}
	for _, t := range typings {
		emit("typing", map[string]any{"conv": t.conv, "on": t.on})
	}
}

// À appeler avec p.mu.
func (p *Pont) upsertContact(id int64, name, first, avatar string) {
	if id == 0 {
		return
	}
	c := p.contacts[id]
	if c == nil {
		c = &contact{}
		p.contacts[id] = c
	}
	changed := false
	if name != "" && name != c.Name {
		c.Name, changed = name, true
	}
	if first != "" && first != c.First {
		c.First, changed = first, true
	}
	if avatar != "" && avatar != c.Avatar {
		c.Avatar, changed = avatar, true
	}
	if changed {
		p.saveContact(id, c)
		p.listDirty = true
	}
}

func containsID(l []int64, id int64) bool {
	for _, x := range l {
		if x == id {
			return true
		}
	}
	return false
}

func removeID(l []int64, id int64) []int64 {
	out := l[:0]
	for _, x := range l {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func setReaction(l []Reaction, actor int64, emoji string, fromMe bool) []Reaction {
	out := l[:0]
	for _, r := range l {
		if r.Actor != actor {
			out = append(out, r)
		}
	}
	if emoji != "" {
		out = append(out, Reaction{Emoji: emoji, Actor: actor, FromMe: fromMe})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Un message d'une table, au format de l'interface.
func (p *Pont) fbRow(wm *table.WrappedMessage) *msgRow {
	if wm == nil || wm.LSInsertMessage == nil || wm.MessageId == "" {
		return nil
	}
	p.mu.Lock()
	me := p.me
	conv := idStr(p.canon(wm.ThreadKey))
	tmp := p.pending[wm.OfflineThreadingId]
	p.mu.Unlock()
	r := &msgRow{
		Message: Message{
			ID:     wm.MessageId,
			Conv:   conv,
			Text:   wm.Text,
			FromMe: wm.SenderId == me && me != 0,
			Time:   wm.TimestampMs,
			Kind:   "messenger",
			TmpID:  tmp,
		},
		SenderID: wm.SenderId,
	}
	if wm.IsAdminMessage {
		r.Kind = "admin"
	}
	if r.FromMe {
		r.Status = "OUTGOING_COMPLETE"
	} else {
		r.Status = "INCOMING_COMPLETE"
	}
	if thumbsUp[wm.StickerId] && r.Text == "" {
		r.Text = "👍"
	}
	add := func(fbid string, url, mime, name string, size, w, h int64, kind table.AttachmentType) {
		if url == "" {
			return
		}
		if mime == "" {
			switch kind {
			case table.AttachmentTypeImage, table.AttachmentTypeSticker, table.AttachmentTypeEphemeralImage:
				mime = "image/jpeg"
			case table.AttachmentTypeAnimatedImage:
				mime = "image/gif"
			case table.AttachmentTypeVideo, table.AttachmentTypeEphemeralVideo:
				mime = "video/mp4"
			case table.AttachmentTypeAudio:
				mime = "audio/mpeg"
			default:
				mime = "application/octet-stream"
			}
		}
		id := "fb:" + fbid
		if fbid == "" {
			id = "fb:" + wm.MessageId + ":" + idStr(int64(len(r.Attachments)))
		}
		key, _ := json.Marshal(mediaKey{URL: url, Mime: mime, Msg: wm.MessageId})
		r.Attachments = append(r.Attachments, Attachment{MediaID: id, Key: key, Mime: mime, Name: name, Size: size, W: w, H: h})
	}
	for _, a := range wm.Attachments {
		url := a.PlayableUrl
		mime := a.AttachmentMimeType
		if url == "" || a.AttachmentType == table.AttachmentTypeImage {
			url = firstOf(a.PreviewUrl, a.PlayableUrl)
			if a.AttachmentType == table.AttachmentTypeImage {
				mime = firstOf(a.PreviewUrlMimeType, a.ImageUrlMimeType, mime)
			}
		}
		mime = firstOf(mime, a.PlayableUrlMimeType, a.PreviewUrlMimeType)
		add(a.AttachmentFbid, url, mime, a.Filename, a.Filesize, a.PreviewWidth, a.PreviewHeight, a.AttachmentType)
	}
	for _, a := range wm.BlobAttachments {
		url := firstOf(a.PlayableUrl, a.PreviewUrl)
		mime := firstOf(a.AttachmentMimeType, a.PlayableUrlMimeType, a.PreviewUrlMimeType)
		if a.AttachmentType == table.AttachmentTypeImage {
			url = firstOf(a.PreviewUrl, a.PlayableUrl)
		}
		add(a.AttachmentFbid, url, mime, a.Filename, a.Filesize, a.PreviewWidth, a.PreviewHeight, a.AttachmentType)
	}
	for _, s := range wm.Stickers {
		if thumbsUp[wm.StickerId] {
			continue
		}
		url := firstOf(s.PlayableUrl, s.PreviewUrl, s.ImageUrl)
		mime := firstOf(s.PlayableUrlMimeType, s.PreviewUrlMimeType, s.ImageUrlMimeType, "image/png")
		add(s.AttachmentFbid, url, mime, "autocollant", 0, s.PreviewWidth, s.PreviewHeight, table.AttachmentTypeSticker)
	}
	for _, x := range wm.XMAAttachments {
		var parts []string
		if x.TitleText != "" {
			parts = append(parts, x.TitleText)
		}
		if x.SubtitleText != "" {
			parts = append(parts, x.SubtitleText)
		}
		link := x.ActionUrl
		if x.CTA != nil {
			link = firstOf(link, x.CTA.ActionUrl, x.CTA.NativeUrl)
		}
		if strings.HasPrefix(link, "http") && !strings.Contains(r.Text, link) {
			parts = append(parts, link)
		}
		if len(parts) > 0 {
			extra := strings.Join(parts, "\n")
			if r.Text == "" {
				r.Text = extra
			} else if !strings.Contains(r.Text, extra) {
				r.Text += "\n\n" + extra
			}
		}
		if x.PreviewUrl != "" {
			add(x.AttachmentFbid, x.PreviewUrl, firstOf(x.PreviewUrlMimeType, "image/jpeg"), "", 0, x.PreviewWidth, x.PreviewHeight, table.AttachmentTypeImage)
		}
	}
	if len(wm.Reactions) > 0 {
		for _, re := range wm.Reactions {
			r.Reactions = setReaction(r.Reactions, re.ActorId, re.Reaction, re.ActorId == me)
		}
	}
	if wm.IsUnsent {
		r.Text, r.Attachments = "🚫 Message retiré", nil
	}
	if wm.PollQuestion != "" {
		r.Text = "📊 Sondage : " + wm.PollQuestion
		for _, o := range wm.PollOptions {
			r.Text += "\n• " + o.Text
		}
	}
	r.Media = mediaSummary(r.Attachments)
	if r.Text == "" && len(r.Attachments) == 0 {
		if wm.IsAdminMessage {
			return nil
		}
		r.Text = "(message non pris en charge : ouvre Messenger)"
	}
	return r
}

func mediaSummary(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	return firstOf(atts[0].Name, atts[0].Mime, "pièce jointe")
}

// La première chaîne non vide.
func firstOf(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// Range un message. Vrai s'il est nouveau ou changé (à montrer en direct).
func (p *Pont) storeRow(r *msgRow, isInsert bool) bool {
	old := p.getMsg(r.ID)
	if old != nil {
		if old.TmpID != "" && r.TmpID == "" {
			r.TmpID = old.TmpID
		}
		if r.Status == "OUTGOING_COMPLETE" && strings.HasPrefix(old.Status, "OUTGOING_") {
			r.Status = old.Status
		}
		if len(r.Reactions) == 0 {
			r.Reactions = old.Reactions
		}
		r.Read = old.Read
	}
	p.mu.Lock()
	c := p.convs[r.Conv]
	if c == nil {
		if k, err := strconv.ParseInt(r.Conv, 10, 64); err == nil {
			c = p.getOrMakeConv(k, table.UNKNOWN_THREAD_TYPE)
			c.LastActivity = r.Time
			p.listDirty = true
		}
	}
	p.mu.Unlock()
	p.putMsg(r)
	p.noteLast(r.Conv, r)
	p.signal(r.Conv)
	if c == nil {
		return false
	}
	if old == nil {
		// Une nouveauté si elle est récente ou si le fil est ouvert dans l'interface.
		p.mu.Lock()
		_, loaded := p.loaded[r.Conv]
		fetching := p.fetching[r.Conv]
		p.mu.Unlock()
		if fetching {
			return false
		}
		return isInsert || loaded || time.Since(time.UnixMilli(r.Time)) < 10*time.Minute
	}
	return old.Text != r.Text || len(old.Attachments) != len(r.Attachments) || old.TmpID != r.TmpID
}

// Garde le dernier message d'un fil pour l'aperçu.
func (p *Pont) noteLast(conv string, r *msgRow) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.convs[conv]
	if c == nil || r.Time < c.LastTime {
		return
	}
	text := r.Text
	if text == "" {
		switch {
		case len(r.Attachments) > 0 && strings.HasPrefix(r.Attachments[0].Mime, "image/"):
			text = "📷 Photo"
		case len(r.Attachments) > 0 && strings.HasPrefix(r.Attachments[0].Mime, "audio/"):
			text = "🎤 Message vocal"
		default:
			text = "📎 Pièce jointe"
		}
	}
	c.LastText, c.LastFromMe, c.LastTime = text, r.FromMe, r.Time
	if r.FromMe {
		c.ReadWM = max(c.ReadWM, r.Time)
	}
	p.listDirty = true
	p.saveConv(conv, c)
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

// Les accusés : mes messages jusqu'à « ts » passent à lus / distribués.
func (p *Pont) markMineRead(conv string, ts int64, status string) {
	for _, r := range p.myRecentMsgs(conv, ts, 30) {
		if r.Status == status || r.Status == "OUTGOING_READ" {
			continue
		}
		r.Status = status
		p.putMsg(r)
		p.emitMsg(r, false)
	}
}
