package main

// Les nouvelles de X (socket XChat et API REST), traduites pour l'interface.

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-twitter/pkg/twittermeow/data/types"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/methods"
)

// Un événement de X. « rest » : il vient de l'API REST (identifiants « a-b »).
func (p *Pont) onEvent(ctx context.Context, evt types.TwitterEvent, rest bool) {
	convOf := func(id string) string {
		if rest {
			return fromREST(id)
		}
		return id
	}
	me := p.myID()
	switch e := evt.(type) {
	case *types.Message:
		conv := convOf(e.ConversationID)
		if rest {
			// Un fil XChat : XChat fait foi (l'API REST donnerait des doublons).
			p.mu.Lock()
			c := p.convs[conv]
			xchat := c != nil && c.XChat
			p.mu.Unlock()
			if xchat {
				return
			}
		}
		p.handleMessage(conv, e)

	case *types.MessageEdit:
		m := (*types.Message)(e)
		conv := convOf(m.ConversationID)
		target := cmp.Or(m.MessageData.ID, m.ID)
		r := p.getMsg(target)
		if r == nil {
			return
		}
		n := p.xRow(conv, m)
		if n == nil {
			return
		}
		r.Text = n.Text
		p.putMsg(r)
		p.noteLastIfLatest(conv, r)
		p.emitMsg(r, false)

	case *types.MessageDelete:
		for _, d := range e.Messages {
			r := p.getMsg(cmp.Or(d.MessageID, d.MessageCreateEventID))
			if r == nil {
				continue
			}
			r.Text, r.Media, r.Attachments, r.Reactions, r.System = "🚫 Message supprimé", "", nil, nil, "deleted"
			p.putMsg(r)
			p.noteLastIfLatest(r.Conv, r)
			p.emitMsg(r, false)
		}

	case *types.MessageReactionCreate:
		p.react(e.MessageID, e.SenderID, e.EmojiReaction)
	case *types.MessageReactionDelete:
		p.react(e.MessageID, e.SenderID, "")

	case *types.ConversationRead:
		conv := convOf(e.ConversationID)
		ts := p.timeOf(e.LastReadEventID, e.Time)
		if e.SenderID == "" || e.SenderID == me {
			p.mu.Lock()
			if c := p.convs[conv]; c != nil && ts > c.ReadWM {
				c.ReadWM = ts
				p.listDirty = true
				p.saveConv(conv, c)
			}
			p.mu.Unlock()
		} else {
			p.markMineRead(conv, ts, "OUTGOING_READ")
		}

	case *types.XChatTyping:
		p.handleTyping(e.ConversationID, e.SenderID)

	case *types.XChatUnsupportedMessage:
		if r := p.unsupportedRow(e.ConversationID, e.SequenceID, e.SenderID, e.Time); r != nil {
			p.ensureConv(r.Conv)
			p.liveRow(r)
		}

	case *types.ConversationNameUpdate:
		conv := convOf(e.ConversationID)
		name := e.ConversationName
		if !rest {
			name = p.decryptGroupName(ctx, conv, name)
		}
		if name == "" {
			return
		}
		p.mu.Lock()
		c := p.getOrMakeConv(conv)
		c.Name, c.Group = name, true
		p.listDirty = true
		p.saveConv(conv, c)
		p.mu.Unlock()

	case *types.ConversationAvatarUpdate:
		conv := convOf(e.ConversationID)
		p.mu.Lock()
		c := p.getOrMakeConv(conv)
		c.Picture, c.PicKey, c.Group = e.ConversationAvatarImageHttps, e.ConversationKeyVersion, true
		p.saveConv(conv, c)
		p.mu.Unlock()
		forgetAvatar("fil:" + conv)
		go p.avatars([]string{conv})

	case *types.ParticipantsJoin:
		p.members(convOf(e.ConversationID), e.Participants, true)
	case *types.ConversationJoin:
		p.members(convOf(e.ConversationID), e.Participants, true)
	case *types.ParticipantsLeave:
		p.members(convOf(e.ConversationID), e.Participants, false)

	case *types.ConversationCreate:
		conv := convOf(e.ConversationID)
		p.mu.Lock()
		c := p.getOrMakeConv(conv)
		if e.Trusted != nil {
			c.Request = !*e.Trusted
		}
		p.saveConv(conv, c)
		p.mu.Unlock()

	case *types.TrustConversation:
		conv := convOf(e.ConversationID)
		p.mu.Lock()
		if c := p.convs[conv]; c != nil {
			c.Request = false
			p.listDirty = true
			p.saveConv(conv, c)
		}
		p.mu.Unlock()

	case *types.ConversationDelete:
		conv := convOf(e.ConversationID)
		p.mu.Lock()
		if c := p.convs[conv]; c != nil {
			c.Hidden = true
			p.listDirty = true
			p.saveConv(conv, c)
		}
		p.mu.Unlock()

	case *types.XChatMessageFailure:
		log.Debug().Str("fil", e.ConversationID).Str("msg", e.MessageID).Msg("message XChat illisible")

	case *types.PollingError:
		// Traité dans onPolling.

	default:
		log.Debug().Type("type", evt).Msg("événement ignoré")
	}
}

// Le temps (ms) d'un événement : celui du message visé s'il est connu.
func (p *Pont) timeOf(id, fallback string) int64 {
	if r := p.getMsg(id); r != nil {
		return r.Time
	}
	if t := methods.ParseMsecTimestamp(fallback); !t.IsZero() {
		return t.UnixMilli()
	}
	if id != "" {
		return methods.ParseSnowflake(id).UnixMilli()
	}
	return time.Now().UnixMilli()
}

func (p *Pont) ensureConv(conv string) {
	p.mu.Lock()
	c := p.getOrMakeConv(conv)
	c.Hidden = false
	p.mu.Unlock()
}

func (p *Pont) members(conv string, parts []types.Participant, join bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.getOrMakeConv(conv)
	c.Group = true
	for _, m := range parts {
		i := -1
		for j, x := range c.Members {
			if x == m.UserID {
				i = j
			}
		}
		switch {
		case join && i < 0:
			c.Members = append(c.Members, m.UserID)
		case !join && i >= 0:
			c.Members = append(c.Members[:i], c.Members[i+1:]...)
		}
	}
	p.listDirty = true
	p.saveConv(conv, c)
}

func (p *Pont) handleMessage(conv string, m *types.Message) {
	r := p.xRow(conv, m)
	if r == nil {
		return
	}
	p.mu.Lock()
	if tmp, ok := p.pending[m.RequestID]; ok && m.RequestID != "" {
		r.TmpID = tmp
	}
	c := p.getOrMakeConv(conv)
	c.Hidden = false
	if m.Trusted != nil && !*m.Trusted {
		c.Request = true
	}
	p.mu.Unlock()
	p.liveRow(r)
}

// Range un message venu en direct (ou d'un rattrapage) et l'annonce.
func (p *Pont) liveRow(r *msgRow) {
	nouveau := p.getMsg(r.ID) == nil
	changed := p.storeRow(r)
	p.mu.Lock()
	silent := p.silent
	p.mu.Unlock()
	if silent || !changed {
		return
	}
	p.emitMsg(r, nouveau)
}

func (p *Pont) handleTyping(conv, sender string) {
	if sender == "" || sender == p.myID() {
		return
	}
	emit("typing", map[string]any{"conv": conv, "on": true})
	// X n'annonce pas la fin : on l'éteint après quelques secondes de silence.
	p.mu.Lock()
	defer p.mu.Unlock()
	if t := p.typing[conv]; t != nil {
		t.Stop()
	}
	p.typing[conv] = time.AfterFunc(6*time.Second, func() {
		emit("typing", map[string]any{"conv": conv, "on": false})
	})
}

func (p *Pont) react(msgID, sender, emoji string) {
	r := p.getMsg(msgID)
	if r == nil {
		return
	}
	actor, _ := strconv.ParseInt(sender, 10, 64)
	r.Reactions = setReaction(r.Reactions, actor, emoji, sender == p.myID())
	p.putMsg(r)
	p.emitMsg(r, false)
}

// ---- Conversion d'un message X ----

const unsupportedText = "🔒 Message chiffré dans un format que Jasette ne sait pas encore lire : ouvre X pour le voir."
const undecryptableText = "🔒 Message chiffré illisible (clé du fil manquante)."

func (p *Pont) unsupportedRow(conv, id, sender, ms string) *msgRow {
	return p.noticeRow(conv, id, sender, ms, unsupportedText)
}

func (p *Pont) undecryptableRow(conv, id, sender, ms string) *msgRow {
	return p.noticeRow(conv, id, sender, ms, undecryptableText)
}

func (p *Pont) noticeRow(conv, id, sender, ms, text string) *msgRow {
	if id == "" || conv == "" {
		return nil
	}
	if old := p.getMsg(id); old != nil && old.Text != unsupportedText && old.Text != undecryptableText {
		// Déjà lu autrement (par exemple par le socket) : on garde.
		return nil
	}
	fromMe := sender != "" && sender == p.myID()
	r := &msgRow{Message: Message{ID: id, Conv: conv, Text: text, FromMe: fromMe, Time: p.timeOf("", ms), Kind: "x"}, SenderID: sender}
	if t := methods.ParseMsecTimestamp(ms); t.IsZero() {
		r.Time = methods.ParseSnowflake(id).UnixMilli()
	}
	r.Status = statusOf(fromMe)
	return r
}

func statusOf(fromMe bool) string {
	if fromMe {
		return "OUTGOING_COMPLETE"
	}
	return "INCOMING_COMPLETE"
}

// Un message X (XChat ou REST) au format de l'interface.
func (p *Pont) xRow(conv string, m *types.Message) *msgRow {
	id := cmp.Or(m.SequenceID, m.ID, m.MessageData.ID)
	if id == "" || conv == "" {
		return nil
	}
	d := &m.MessageData
	ts := methods.ParseMsecTimestamp(cmp.Or(m.Time, d.Time))
	if ts.IsZero() {
		ts = methods.ParseSnowflake(id)
	}
	sender := d.SenderID
	fromMe := sender != "" && sender == p.myID()
	text := d.Text
	if d.Entities != nil {
		// Les liens t.co : l'adresse complète.
		for _, u := range d.Entities.URLs {
			if u.URL != "" && u.ExpandedURL != "" {
				text = strings.ReplaceAll(text, u.URL, u.ExpandedURL)
			}
		}
	}
	atts := d.Attachments
	if len(atts) == 0 && d.Attachment != nil {
		atts = []*types.Attachment{d.Attachment}
	}
	var out []Attachment
	for i, a := range atts {
		if att, link := p.convertAttachment(conv, id, i, a, cmp.Or(d.ConversationKeyVersion, m.ConversationKeyVersion)); att != nil {
			out = append(out, *att)
			// L'API REST met le lien t.co de la photo dans le texte.
			if link != "" {
				text = strings.TrimSpace(strings.ReplaceAll(text, link, ""))
			}
		}
	}
	if d.ReplyData.Text != "" {
		quote := d.ReplyData.Text
		if len([]rune(quote)) > 80 {
			quote = string([]rune(quote)[:80]) + "…"
		}
		text = "↪ « " + quote + " »\n" + text
	}
	r := &msgRow{
		Message: Message{
			ID:          id,
			Conv:        conv,
			Text:        text,
			FromMe:      fromMe,
			Time:        ts.UnixMilli(),
			Kind:        "x",
			Status:      statusOf(fromMe),
			Attachments: out,
		},
		SenderID:  sender,
		RequestID: m.RequestID,
	}
	r.Media = mediaSummary(out)
	for _, re := range m.MessageReactions {
		actor, _ := strconv.ParseInt(re.SenderID, 10, 64)
		r.Reactions = setReaction(r.Reactions, actor, re.EmojiReaction, re.SenderID == p.myID())
	}
	return r
}

// Une pièce jointe X. Rend aussi le lien t.co qui la représente dans le texte.
func (p *Pont) convertAttachment(conv, msgID string, index int, a *types.Attachment, keyVersion string) (*Attachment, string) {
	if a == nil {
		return nil, ""
	}
	var info *types.AttachmentInfo
	mimeType := ""
	switch {
	case a.Photo != nil:
		info, mimeType = a.Photo, "image/jpeg"
	case a.AnimatedGif != nil:
		info, mimeType = a.AnimatedGif, "video/mp4"
	case a.Video != nil:
		info, mimeType = a.Video, "video/mp4"
		if info.AudioOnly {
			mimeType = "audio/mp4"
		}
	case a.File != nil:
		info = a.File
		mimeType = mime.TypeByExtension(filepath.Ext(info.Filename))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
	case a.Media != nil:
		info, mimeType = a.Media, "image/jpeg"
	default:
		// Carte de lien ou publication : le lien est déjà dans le texte.
		return nil, ""
	}
	url := ""
	if len(info.VideoInfo.Variants) > 0 {
		best := -1
		for i, v := range info.VideoInfo.Variants {
			if v.ContentType == "video/mp4" && (best < 0 || v.Bitrate > info.VideoInfo.Variants[best].Bitrate) {
				best = i
			}
		}
		if best >= 0 {
			url = info.VideoInfo.Variants[best].URL
		}
	}
	url = normalizeURL(cmp.Or(url, info.MediaURLHTTPS, info.MediaURL))
	if info.MediaHashKey == "" && url == "" {
		return nil, ""
	}
	k := mediaKey{URL: url, Mime: mimeType}
	if info.MediaHashKey != "" {
		// Média XChat : chiffré avec la clé du fil (voir media.go).
		k.Conv, k.Hash, k.Ver = conv, info.MediaHashKey, keyVersion
	}
	key, _ := json.Marshal(k)
	name := info.Filename
	if name == "" {
		name = "x-" + msgID + "-" + strconv.Itoa(index) + extension(mimeType, nil)
	}
	att := &Attachment{
		MediaID: msgID + "/" + strconv.Itoa(index),
		Key:     key,
		Mime:    mimeType,
		Name:    name,
		Size:    info.FilesizeBytes,
		W:       int64(info.OriginalInfo.Width),
		H:       int64(info.OriginalInfo.Height),
	}
	return att, info.URL
}

func normalizeURL(raw string) string {
	s := strings.TrimSpace(raw)
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "//"):
		return "https:" + s
	case strings.HasPrefix(s, "http://"):
		return "https://" + strings.TrimPrefix(s, "http://")
	}
	return s
}

func decodeB64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// ---- Le cache des messages ----

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
		if len(r.Reactions) == 0 && len(old.Reactions) > 0 {
			r.Reactions = old.Reactions
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
		switch m := r.Attachments[0].Mime; {
		case strings.HasPrefix(m, "image/"):
			text = firstOf(text, "📷 Photo")
		case strings.HasPrefix(m, "video/"):
			text = firstOf(text, "🎬 Vidéo")
		case strings.HasPrefix(m, "audio/"):
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
