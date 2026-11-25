package main

// Les nouveautés du socket (« deltas ») et la conversion des messages
// d'Instagram au format de l'interface.

import (
	"cmp"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
)

// Le fil d'un delta. S'il est inconnu (et que le delta peut le créer), il est
// demandé à Instagram.
func (p *Pont) convFor(ctx context.Context, cli *instameow.Client, igid string, allowCreate bool) string {
	if igid == "" {
		return ""
	}
	p.mu.Lock()
	conv := p.byIGID[igid]
	if conv == "" {
		// Les groupes : l'identifiant court est aussi la clé du fil.
		if _, ok := p.convs[igid]; ok {
			conv = igid
		}
	}
	p.mu.Unlock()
	if conv != "" || !allowCreate {
		return conv
	}
	ctx, annule := context.WithTimeout(ctx, 30*time.Second)
	defer annule()
	resp, err := cli.GetThread(ctx, slidetypes.MakeGetThreadInfoRequest(igid))
	if err != nil {
		log.Warn().Err(err).Str("igid", igid).Msg("fil inconnu : infos")
		return ""
	}
	// Sans ses messages : le delta qui a fait venir le fil va l'annoncer lui-même.
	conv = p.upsertThread(resp.ThreadInfo.AsIGDirectThread, false)
	if conv != "" {
		go p.avatars([]string{conv})
	}
	return conv
}

func (p *Pont) handleDelta(ctx context.Context, cli *instameow.Client, d *slidetypes.Delta) {
	defer func() {
		if v := recover(); v != nil {
			log.Error().Any("panique", v).Str("type", d.TypeName).Msg("delta")
		}
	}()
	igid := d.ThreadIGID
	allowCreate := true
	switch evt := d.Data.(type) {
	case *slidetypes.DeleteThreadEvent, *slidetypes.DeleteMessageEvent, *slidetypes.DeleteReactionEvent,
		*slidetypes.ParticipantLeaveEvent, *slidetypes.MarkReadEvent, *slidetypes.MarkUnreadEvent,
		*slidetypes.ReadReceiptEvent, *slidetypes.MuteThreadEvent, *slidetypes.PinMessageEvent,
		*slidetypes.AdminChangeEvent, *slidetypes.EditMessageEvent:
		allowCreate = false
	case *slidetypes.PinThreadEvent:
		allowCreate = evt.IsPinned
	case *slidetypes.UpdateThreadFolderEvent:
		allowCreate = evt.Folder != "OTHER"
	case *slidetypes.NewMessageEvent:
		// Certains messages (éphémères ?) n'ont pas le fil en haut : il est dans le message.
		if igid == "" && evt.Message != nil {
			igid = evt.Message.ThreadFBID
		}
	case slidetypes.UnknownEvent:
		log.Debug().Str("type", d.TypeName).Msg("delta inconnu")
		return
	case slidetypes.IgnoredEvent:
		return
	}
	conv := p.convFor(ctx, cli, igid, allowCreate)
	if conv == "" {
		log.Debug().Str("type", d.TypeName).Msg("delta sans fil connu")
		return
	}
	c := p.conv(conv)
	if c == nil {
		return
	}

	switch evt := d.Data.(type) {
	case *slidetypes.NewMessageEvent:
		p.liveMessage(conv, evt.Message)
	case *slidetypes.AdminMessageEvent:
		p.liveMessage(conv, evt.Message)
	case *slidetypes.EditMessageEvent:
		if r := p.getMsg(evt.MessageID); r != nil {
			r.Text = evt.TextBody
			p.putMsg(r)
			p.noteLastIfLatest(conv, r)
			p.emitMsg(r, false)
		}
	case *slidetypes.CreateReactionEvent:
		p.mu.Lock()
		me := p.me
		p.mu.Unlock()
		p.putReaction(evt.Reaction.LogMessageID, evt.MessageID, evt.Reaction.SenderFBID)
		if r := p.getMsg(evt.MessageID); r != nil {
			r.Reactions = setReaction(r.Reactions, evt.Reaction.SenderFBID, evt.Reaction.Reaction, evt.Reaction.SenderFBID == me)
			p.putMsg(r)
			p.emitMsg(r, false)
		}
	case *slidetypes.DeleteReactionEvent:
		target, sender := evt.MessageID, evt.Reaction.SenderFBID
		if sender == 0 {
			target, sender = p.getReaction(evt.Reaction.LogMessageID)
		}
		if r := p.getMsg(target); r != nil && sender != 0 {
			r.Reactions = setReaction(r.Reactions, sender, "", false)
			p.putMsg(r)
			p.emitMsg(r, false)
		}
	case *slidetypes.DeleteMessageEvent:
		if r := p.getMsg(evt.MessageID); r != nil {
			r.Text, r.Media, r.Attachments = "🚫 Message retiré", "", nil
			p.putMsg(r)
			p.noteLastIfLatest(conv, r)
			p.emitMsg(r, false)
		}
	case *slidetypes.DeleteThreadEvent:
		p.mu.Lock()
		c.Hidden = true
		p.saveConv(conv, c)
		p.listDirty = true
		p.mu.Unlock()
	case *slidetypes.UpdateThreadFolderEvent:
		p.mu.Lock()
		c.Folder = folderName(evt.Folder)
		p.saveConv(conv, c)
		p.listDirty = true
		p.mu.Unlock()
	case *slidetypes.UpdateThreadNameEvent:
		p.mu.Lock()
		c.Name = evt.ThreadName
		p.saveConv(conv, c)
		p.listDirty = true
		p.mu.Unlock()
		p.liveMessage(conv, evt.Message)
	case *slidetypes.UpdateThreadImageEvent:
		p.mu.Lock()
		c.Picture = evt.ThreadImage.URI
		p.saveConv(conv, c)
		p.listDirty = true
		p.mu.Unlock()
		p.liveMessage(conv, evt.Message)
		go p.avatars([]string{conv})
	case *slidetypes.ParticipantJoinEvent:
		if t := evt.Thread.AsIGDirectThread; t != nil {
			p.mu.Lock()
			for _, u := range t.Users {
				if id := p.upsertUser(u); id != 0 && !containsID(c.Members, id) {
					c.Members = append(c.Members, id)
				}
			}
			if t.ThreadTitle != "" {
				c.Name = t.ThreadTitle
			}
			p.saveConv(conv, c)
			p.listDirty = true
			p.mu.Unlock()
		}
		p.liveMessage(conv, evt.Message)
	case *slidetypes.ParticipantLeaveEvent:
		p.mu.Lock()
		c.Members = removeID(c.Members, evt.LeftParticipantFBID)
		if evt.LeftParticipantFBID == p.me && p.me != 0 {
			c.ReadOnly = true
		}
		p.saveConv(conv, c)
		p.listDirty = true
		p.mu.Unlock()
		p.liveMessage(conv, evt.Message)
	case *slidetypes.MarkReadEvent:
		p.mu.Lock()
		c.ReadWM = max(c.ReadWM, evt.ReadTimestampMS.UnixMilli())
		c.MarkedUnread = false
		p.saveConv(conv, c)
		p.listDirty = true
		p.mu.Unlock()
	case *slidetypes.MarkUnreadEvent:
		p.mu.Lock()
		c.MarkedUnread = evt.MarkedAsUnread
		p.saveConv(conv, c)
		p.listDirty = true
		p.mu.Unlock()
	case *slidetypes.ReadReceiptEvent:
		ts := evt.ReadReceipt.WatermarkTimestampMS.UnixMilli()
		p.mu.Lock()
		mine := evt.ReadReceipt.ParticipantFBID == p.me && p.me != 0
		if mine {
			c.ReadWM = max(c.ReadWM, ts)
			p.saveConv(conv, c)
			p.listDirty = true
		}
		p.mu.Unlock()
		if !mine && ts > 0 {
			p.markMineRead(conv, ts, "OUTGOING_READ")
		}
	default:
		// Épinglage, sourdine, administrateurs : rien à montrer dans Jasette.
	}
}

// « Est en train d'écrire » : le fil vient de l'identifiant long, la personne
// de son identifiant Instagram.
func (p *Pont) handleTyping(evt *slidetypes.TypingNotification) {
	p.mu.Lock()
	conv := p.byThreadID[evt.ThreadID]
	sender := p.byUserIGID[strconv.FormatInt(evt.SenderID, 10)]
	me, myIG := p.me, ""
	if p.sess != nil {
		myIG = p.sess.IGID
	}
	p.mu.Unlock()
	if conv == "" || (sender != 0 && sender == me) || (myIG != "" && strconv.FormatInt(evt.SenderID, 10) == myIG) {
		return
	}
	emit("typing", map[string]any{"conv": conv, "on": evt.ActivityStatus != 0})
}

// Un message arrivé en direct : rangé, puis annoncé à l'interface.
func (p *Pont) liveMessage(conv string, m *slidetypes.Message) {
	r := p.igRow(conv, m)
	if r == nil {
		return
	}
	p.mu.Lock()
	if c := p.convs[conv]; c != nil {
		c.LastActivity = max(c.LastActivity, r.Time)
		c.Hidden = false
		if r.FromMe {
			c.ReadWM = max(c.ReadWM, r.Time)
			c.MarkedUnread = false
		}
		p.saveConv(conv, c)
		p.listDirty = true
	}
	p.mu.Unlock()
	if p.storeRow(r) {
		p.emitMsg(r, true)
	}
}

// ---- Conversion ----

// Un message d'Instagram, au format de l'interface (nil s'il n'y a rien à montrer).
func (p *Pont) igRow(conv string, m *slidetypes.Message) *msgRow {
	if m == nil {
		return nil
	}
	id := cmp.Or(m.ID, m.MessageID)
	if id == "" {
		return nil
	}
	p.mu.Lock()
	me := p.me
	tmp := p.pending[m.OfflineThreadingID]
	if m.Sender != nil {
		u := m.Sender.UserDict
		u.InteropMessagingUserFBID = cmp.Or(u.InteropMessagingUserFBID, m.SenderFBID)
		if u.FullName == "" && u.Username == "" {
			u.FullName = m.Sender.Name
		}
		p.upsertUser(&u)
	}
	p.mu.Unlock()
	r := &msgRow{
		Message: Message{
			ID:     id,
			Conv:   conv,
			FromMe: m.SenderFBID == me && me != 0,
			Time:   m.TimestampMS.UnixMilli(),
			Kind:   "instagram",
			TmpID:  tmp,
		},
		SenderID: m.SenderFBID,
	}
	if r.Time == 0 {
		r.Time = time.Now().UnixMilli()
	}
	if r.FromMe {
		r.Status = "OUTGOING_COMPLETE"
	} else {
		r.Status = "INCOMING_COMPLETE"
	}
	add := func(fbid, url, mime, name string, w, h int) {
		if url == "" {
			return
		}
		mid := "ig:" + id + ":" + strconv.Itoa(len(r.Attachments))
		if fbid != "" {
			mid = "ig:" + fbid
		}
		key, _ := json.Marshal(mediaKey{URL: url, Mime: mime, Msg: id, Conv: conv, FBID: fbid, Index: len(r.Attachments)})
		r.Attachments = append(r.Attachments, Attachment{MediaID: mid, Key: key, Mime: mime, Name: name, W: int64(w), H: int64(h)})
	}
	addAtt := func(a *slidetypes.Attachment, mime string) {
		if a == nil {
			return
		}
		if mime == "" {
			mime = "image/jpeg"
			if a.DashManifest != "" || strings.Contains(strings.ToLower(a.Typename), "video") {
				mime = "video/mp4"
			}
		}
		url := cmp.Or(a.AttachmentCDNURL, a.AttachmentCDNFallbackURL, a.PreviewCDNURL, a.PreviewCDNFallbackURL)
		add(a.AttachmentFBID, url, mime, "", a.PreviewWidth, a.PreviewHeight)
	}
	switch content := m.Content.Content.(type) {
	case *slidetypes.MessageContentText:
		r.Text = content.TextBody
	case *slidetypes.MessageContentAdminText:
		if content.IsReactionActionLog {
			// « X a réagi à un message » : la réaction elle-même suffit.
			return nil
		}
		r.Kind = "admin"
		var b strings.Builder
		for _, f := range content.TextFragments {
			b.WriteString(f.Plaintext)
		}
		r.Text = cmp.Or(strings.TrimSpace(b.String()), m.IGDSnippet)
	case *slidetypes.MessageContentImage:
		for _, a := range content.Attachments {
			addAtt(a, "image/jpeg")
		}
	case *slidetypes.MessageContentVideo:
		for _, a := range content.Videos {
			addAtt(a, "video/mp4")
		}
	case *slidetypes.MessageContentAudio:
		for _, a := range content.AudioAttachments {
			if a != nil {
				add(a.AttachmentFBID, a.AttachmentCDNURL, "audio/mp4", "message vocal", 0, 0)
			}
		}
	case *slidetypes.MessageContentMultiMedia:
		for _, a := range content.Attachments {
			addAtt(a, "")
		}
	case *slidetypes.MessageContentAnimatedMedia:
		for _, a := range content.AnimatedMedia {
			if a == nil {
				continue
			}
			switch {
			case a.AttachmentWebpURL != "" && (a.IsSticker || a.AttachmentMP4URL == ""):
				add("", a.AttachmentWebpURL, "image/webp", a.AltText, a.PreviewWidth, a.PreviewHeight)
			case a.AttachmentMP4URL != "":
				add("", a.AttachmentMP4URL, "video/mp4", a.AltText, a.PreviewWidth, a.PreviewHeight)
			case a.PreviewCDNURL != "":
				add("", a.PreviewCDNURL, "image/jpeg", a.AltText, a.PreviewWidth, a.PreviewHeight)
			}
		}
	case *slidetypes.MessageContentRavenImage:
		if content.Attachment == nil {
			r.Text = "📷 Photo éphémère : ouvre Instagram pour la voir"
		} else {
			addAtt(content.Attachment, "image/jpeg")
		}
	case *slidetypes.MessageContentRavenVideo:
		if content.Attachment == nil {
			r.Text = "🎬 Vidéo éphémère : ouvre Instagram pour la voir"
		} else {
			addAtt(content.Attachment, "video/mp4")
		}
	case *slidetypes.MessageContentSticker:
		if content.PreviewURL != "" {
			add("", content.PreviewURL, "image/webp", "autocollant", content.PreviewWidth, content.PreviewHeight)
		} else {
			r.Text = cmp.Or(content.AltText, "autocollant")
		}
	case *slidetypes.MessageContentMusicSticker:
		r.Text = "🎵 " + strings.TrimSpace(content.AudioTrack.Title+" — "+content.AudioTrack.DisplayArtist)
		add(content.MediaContentFBID, content.AudioTrack.Web30SPreviewDownloadURL, "audio/mpeg", content.AudioTrack.Title, 0, 0)
	case *slidetypes.MessageContentXMA:
		r.Text = content.XMATextBody
		if x := content.XMA; x != nil {
			var parts []string
			title := cmp.Or(x.TitleText, x.XMATitle)
			if x.HeaderTitleText != "" && x.HeaderTitleText != title {
				parts = append(parts, x.HeaderTitleText)
			}
			if title != "" {
				parts = append(parts, title)
			}
			if sub := cmp.Or(x.SubtitleText, x.CaptionBodyText); sub != "" {
				parts = append(parts, sub)
			}
			link := xmaLink(x)
			if link != "" && !strings.Contains(r.Text, link) {
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
			if img := cmp.Or(x.PreviewImage, x.XMAPreviewImage); img != nil && img.URL != "" {
				add("", img.URL, "image/jpeg", "", img.Width, img.Height)
			}
		}
	case *slidetypes.MessageContentAIRichResponse:
		r.Text = content.UnifiedResponse
	case *slidetypes.MessageContentAISearchResponse:
		r.Text = content.MessageTextBody
	}
	if r.Text == "" && r.Kind != "admin" {
		r.Text = m.TextBody
	}
	// La boîte de réception ne livre que des messages abrégés (partages sans
	// contenu) : l'aperçu d'Instagram dit alors de quoi il s'agit.
	if r.Text == "" && len(r.Attachments) == 0 {
		r.Text = m.IGDSnippet
	}
	for _, re := range m.Reactions {
		if re == nil {
			continue
		}
		r.Reactions = setReaction(r.Reactions, re.SenderFBID, re.Reaction, re.SenderFBID == me)
		p.putReaction(re.LogMessageID, id, re.SenderFBID)
	}
	r.Media = mediaSummary(r.Attachments)
	if r.Text == "" && len(r.Attachments) == 0 {
		if r.Kind == "admin" {
			return nil
		}
		if e := log.Debug(); e.Enabled() {
			var keys []string
			if u, ok := m.Content.Content.(slidetypes.MessageContentUnknown); ok {
				for k := range u {
					keys = append(keys, k)
				}
			}
			e.Str("type", m.Content.TypeName).Str("content_type", m.ContentType).Strs("champs", keys).
				Bool("apercu", m.IGDSnippet != "").Msg("message non pris en charge")
		}
		r.Text = "(message non pris en charge : ouvre Instagram)"
	}
	return r
}

// Le lien d'un partage (publication, reel, lien externe).
func xmaLink(x *slidetypes.XMAContent) string {
	link := x.TargetURL
	if u, err := url.Parse(link); err == nil && (u.Host == "l.facebook.com" || u.Host == "l.instagram.com") {
		if real := u.Query().Get("u"); real != "" {
			link = real
		}
	}
	if !strings.HasPrefix(link, "http") {
		for _, b := range x.CTAButtons {
			if b != nil && strings.HasPrefix(b.ActionURL, "http") {
				return b.ActionURL
			}
		}
		return ""
	}
	return link
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

// Range un message. Vrai s'il est nouveau ou changé (à montrer en direct).
func (p *Pont) storeRow(r *msgRow) bool {
	old := p.getMsg(r.ID)
	if old != nil {
		if old.TmpID != "" && r.TmpID == "" {
			r.TmpID = old.TmpID
		}
		if r.Status == "OUTGOING_COMPLETE" && strings.HasPrefix(old.Status, "OUTGOING_") {
			r.Status = old.Status
		}
		// Une pièce jointe envoyée d'ici : la copie locale reste la bonne.
		if len(old.Attachments) > 0 && strings.HasPrefix(old.Attachments[0].MediaID, "local:") && len(r.Attachments) == len(old.Attachments) {
			r.Attachments, r.Media = old.Attachments, old.Media
		}
	}
	p.putMsg(r)
	p.noteLast(r.Conv, r)
	if old == nil {
		return true
	}
	return old.Text != r.Text || len(old.Attachments) != len(r.Attachments) || old.TmpID != r.TmpID || !sameReactions(old.Reactions, r.Reactions)
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
	if len(r.Attachments) > 0 && (text == "" || r.Media != "") {
		switch mime := r.Attachments[0].Mime; {
		case strings.HasPrefix(mime, "image/"):
			text = firstOf(text, "📷 Photo")
		case strings.HasPrefix(mime, "video/"):
			text = firstOf(text, "🎬 Vidéo")
		case strings.HasPrefix(mime, "audio/"):
			text = firstOf(text, "🎤 Message vocal")
		default:
			text = firstOf(text, "📎 Pièce jointe")
		}
	}
	c.LastText, c.LastFromMe, c.LastTime = text, r.FromMe, r.Time
	if r.FromMe {
		c.ReadWM = max(c.ReadWM, r.Time)
	}
	p.listDirty = true
	p.saveConv(conv, c)
}

// Après une modification : l'aperçu suit si c'était le dernier message.
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

// Les accusés : mes messages jusqu'à « ts » passent à lus.
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
