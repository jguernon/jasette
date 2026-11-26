package main

// L'historique (« history sync »), les messages en direct, les accusés, la
// saisie, et la conversion des messages de WhatsApp au format de l'interface.

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type waCommonKey = waCommon.MessageKey

func jsonDecode(b []byte, v any) error { return json.Unmarshal(b, v) }

// ---- L'historique ----

// Un morceau d'historique envoyé par le téléphone (juste après l'association,
// ou à la demande pour un fil).
func (p *Pont) handleHistory(cli *whatsmeow.Client, evt *events.HistorySync) {
	d := evt.Data
	if d == nil {
		return
	}
	kind := d.GetSyncType()
	onDemand := kind == waHistorySync.HistorySync_ON_DEMAND
	log.Info().Str("type", kind.String()).Uint32("morceau", d.GetChunkOrder()).Uint32("progrès", d.GetProgress()).
		Int("fils", len(d.GetConversations())).Msg("historique reçu")
	var touched []string
	for _, hc := range d.GetConversations() {
		orig, err := types.ParseJID(hc.GetID())
		if err != nil {
			continue
		}
		chat := p.canon(cli, orig)
		if skipChat(chat) {
			continue
		}
		msgs := hc.GetMessages()
		p.mu.Lock()
		id, c := p.getOrMakeConv(chat)
		if name := cmp.Or(hc.GetName(), hc.GetDisplayName()); name != "" && (c.Group || c.Name == "") {
			c.Name = name
		}
		if !onDemand {
			c.Archived = hc.GetArchived()
			c.Unread = int(hc.GetUnreadCount())
			c.MarkedUnread = hc.GetMarkedAsUnread()
			c.ReadOnly = c.ReadOnly || hc.GetReadOnly() || hc.GetTerminated()
		}
		c.LastActivity = max(c.LastActivity, int64(hc.GetConversationTimestamp())*1000, int64(hc.GetLastMsgTimestamp())*1000)
		if hc.GetEndOfHistoryTransferType() == waHistorySync.Conversation_COMPLETE_AND_NO_MORE_MESSAGE_REMAIN_ON_PRIMARY ||
			(onDemand && len(msgs) == 0) {
			c.NoMore = true
		}
		c.Hidden = false
		p.saveConv(id, c)
		p.listDirty = true
		p.mu.Unlock()

		for _, hm := range msgs {
			web := hm.GetMessage()
			if web == nil {
				continue
			}
			me, err := cli.ParseWebMessage(orig, web)
			if err != nil {
				log.Debug().Err(err).Msg("historique : message illisible")
				continue
			}
			p.handleMessage(cli, me, false)
			p.historyReactions(cli, id, web)
		}
		p.mu.Lock()
		if c.Unread == 0 && !c.MarkedUnread {
			c.ReadWM = max(c.ReadWM, c.LastTime)
		}
		p.saveConv(id, c)
		p.mu.Unlock()
		touched = append(touched, id)
	}
	// Ceux qui attendaient une page d'historique.
	p.mu.Lock()
	for _, id := range touched {
		for _, ch := range p.histWait[id] {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
	p.mu.Unlock()
	go func() {
		p.reloadContacts(cli)
		p.emitList()
		p.avatars(touched)
	}()
}

// Les réactions gardées avec un message de l'historique.
func (p *Pont) historyReactions(cli *whatsmeow.Client, conv string, web *waWeb.WebMessageInfo) {
	if len(web.GetReactions()) == 0 {
		return
	}
	r := p.getMsg(web.GetKey().GetID())
	if r == nil {
		return
	}
	for _, re := range web.GetReactions() {
		k := re.GetKey()
		actor := ""
		fromMe := k.GetFromMe()
		if fromMe && cli.Store.ID != nil {
			actor = cli.Store.ID.ToNonAD().String()
		} else if j, err := types.ParseJID(cmp.Or(k.GetParticipant(), k.GetRemoteJID())); err == nil {
			actor = p.canon(cli, j).String()
		}
		if actor == "" {
			continue
		}
		r.Reactions = setReaction(r.Reactions, actor, re.GetText(), fromMe)
	}
	p.putMsg(r)
}

// ---- Les messages ----

// Un message (en direct si live, sinon de l'historique).
func (p *Pont) handleMessage(cli *whatsmeow.Client, evt *events.Message, live bool) {
	chat := p.canon(cli, evt.Info.Chat)
	if skipChat(chat) || evt.Message == nil {
		return
	}
	conv := chat.String()
	m := evt.Message
	p.learnSender(cli, &evt.Info)

	if re := m.GetReactionMessage(); re != nil {
		p.applyReaction(cli, &evt.Info, re.GetKey().GetID(), re.GetText())
		return
	}
	if pm := m.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			if r := p.getMsg(pm.GetKey().GetID()); r != nil {
				r.Text, r.Body, r.Media, r.Attachments, r.ReplyTo, r.ReplyText = "🚫 Message retiré", "🚫 Message retiré", "", nil, "", ""
				r.System = "deleted"
				p.putMsg(r)
				p.noteLastIfLatest(r.Conv, r)
				p.emitMsg(r, false)
			}
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if r := p.getMsg(pm.GetKey().GetID()); r != nil {
				if body := plainText(pm.GetEditedMessage()); body != "" {
					r.Body = body
					r.Text = body
					if r.ReplyText != "" {
						r.Text = quoteLine(r.ReplyText) + body
					}
					p.putMsg(r)
					p.noteLastIfLatest(r.Conv, r)
					p.emitMsg(r, false)
				}
			}
		}
		return
	}
	r := p.waRow(cli, conv, evt)
	if r == nil {
		return
	}
	p.mu.Lock()
	id, c := p.getOrMakeConv(chat)
	c.LastActivity = max(c.LastActivity, r.Time)
	c.Hidden = false
	if r.FromMe {
		c.ReadWM = max(c.ReadWM, r.Time)
		c.Unread, c.MarkedUnread = 0, false
	} else if live && r.Time > c.ReadWM && r.Kind != "admin" {
		c.Unread++
	}
	needGroup := c.Group && c.Name == ""
	p.saveConv(id, c)
	p.listDirty = true
	p.mu.Unlock()
	if needGroup && live {
		go p.fetchGroup(cli, chat)
	}
	if p.storeRow(r) && live {
		p.emitMsg(r, true)
	}
}

// Le nom public et le numéro d'un expéditeur, vus dans un message.
func (p *Pont) learnSender(cli *whatsmeow.Client, info *types.MessageInfo) {
	sender := info.Sender.ToNonAD()
	p.mu.Lock()
	defer p.mu.Unlock()
	if sender.Server == types.HiddenUserServer && info.SenderAlt.Server == types.DefaultUserServer {
		p.lidPN[sender.String()] = info.SenderAlt.ToNonAD().String()
	}
	if info.PushName != "" && !info.IsFromMe {
		key := sender.String()
		if pn := p.lidPN[key]; pn != "" {
			key = pn
		}
		if p.names[key] == "" {
			p.names[key] = info.PushName
			p.listDirty = true
		}
	}
}

func (p *Pont) fetchGroup(cli *whatsmeow.Client, jid types.JID) {
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	if g, err := cli.GetGroupInfo(ctx, jid); err == nil {
		p.upsertGroup(cli, g)
		go p.avatars([]string{jid.String()})
	}
}

func (p *Pont) applyReaction(cli *whatsmeow.Client, info *types.MessageInfo, target, emoji string) {
	r := p.getMsg(target)
	if r == nil {
		return
	}
	actor := p.canon(cli, info.Sender).String()
	if info.IsFromMe && cli.Store.ID != nil {
		// Ma réaction (d'un autre de mes appareils) : comme celles envoyées d'ici.
		r.Reactions = setMyReaction(r.Reactions, cli.Store.ID.ToNonAD().String(), emoji)
		p.putMsg(r)
		p.emitMsg(r, false)
		return
	}
	r.Reactions = setReaction(r.Reactions, actor, emoji, info.IsFromMe)
	p.putMsg(r)
	p.emitMsg(r, false)
}

// Un message que ce pont n'a pas pu déchiffrer (WhatsApp le redemande).
func (p *Pont) handleUndecryptable(cli *whatsmeow.Client, evt *events.UndecryptableMessage) {
	if evt.DecryptFailMode == events.DecryptFailHide {
		return
	}
	chat := p.canon(cli, evt.Info.Chat)
	if skipChat(chat) {
		return
	}
	text := "⏳ Message en attente (ouvre WhatsApp sur le téléphone s'il n'arrive pas)"
	if evt.UnavailableType == events.UnavailableTypeViewOnce {
		text = "👁 Message à vue unique : ouvre WhatsApp sur le téléphone"
	}
	if p.getMsg(evt.Info.ID) != nil {
		return
	}
	r := &msgRow{
		Message:   Message{ID: evt.Info.ID, Conv: chat.String(), Text: text, FromMe: evt.Info.IsFromMe, Time: evt.Info.Timestamp.UnixMilli(), Kind: "whatsapp", Status: "INCOMING_COMPLETE"},
		SenderJID: p.canon(cli, evt.Info.Sender).String(),
		ChatJID:   evt.Info.Chat.ToNonAD().String(),
		Body:      text,
	}
	if r.FromMe {
		r.Status = "OUTGOING_COMPLETE"
	}
	p.mu.Lock()
	id, c := p.getOrMakeConv(chat)
	c.LastActivity = max(c.LastActivity, r.Time)
	p.saveConv(id, c)
	p.mu.Unlock()
	if p.storeRow(r) {
		p.emitMsg(r, true)
	}
}

// Les accusés : distribués et lus (mes messages), ou lus ailleurs (les leurs).
func (p *Pont) handleReceipt(cli *whatsmeow.Client, evt *events.Receipt) {
	chat := p.canon(cli, evt.Chat)
	if skipChat(chat) {
		return
	}
	conv := chat.String()
	if evt.IsFromMe {
		// Lu sur un autre de mes appareils.
		if evt.Type == types.ReceiptTypeRead || evt.Type == types.ReceiptTypeReadSelf {
			p.mu.Lock()
			if c := p.convs[conv]; c != nil {
				c.Unread, c.MarkedUnread = 0, false
				c.ReadWM = max(c.ReadWM, evt.Timestamp.UnixMilli())
				p.saveConv(conv, c)
				p.listDirty = true
			}
			p.mu.Unlock()
		}
		return
	}
	var status string
	switch evt.Type {
	case types.ReceiptTypeDelivered:
		status = "OUTGOING_DELIVERED"
	case types.ReceiptTypeRead, types.ReceiptTypePlayed:
		status = "OUTGOING_READ"
	default:
		return
	}
	// Dans un groupe, chaque membre envoie ses accusés : on garde qui a lu.
	reader := ""
	if chat.Server == types.GroupServer && status == "OUTGOING_READ" && !evt.Sender.IsEmpty() {
		reader = p.canon(cli, evt.Sender).String()
	}
	for _, id := range evt.MessageIDs {
		r := p.getMsg(id)
		if r == nil || !r.FromMe {
			continue
		}
		var added bool
		r.Readers, added = addReader(r.Readers, reader)
		// Ne jamais revenir en arrière (lu > distribué > envoyé).
		if r.Status == "OUTGOING_READ" || r.Status == status {
			if !added {
				continue
			}
		} else {
			r.Status = status
		}
		p.putMsg(r)
		p.emitMsg(r, false)
	}
}

// « Est en train d'écrire » (ou d'enregistrer un vocal).
func (p *Pont) handleTyping(cli *whatsmeow.Client, evt *events.ChatPresence) {
	chat := p.canon(cli, evt.Chat)
	if skipChat(chat) || evt.IsFromMe {
		return
	}
	sender := p.canon(cli, evt.Sender).String()
	p.mu.Lock()
	me := p.isMe(cli, sender)
	name := p.nameOf(sender)
	p.mu.Unlock()
	if me {
		return
	}
	ev := map[string]any{"conv": chat.String(), "on": evt.State == types.ChatPresenceComposing}
	if name != "" {
		ev["name"] = name
	}
	if evt.Media == types.ChatPresenceMediaAudio {
		ev["audio"] = true
	}
	emit("typing", ev)
}

// ---- Conversion ----

// Les champs d'un message qui ne montrent rien à l'interface.
var invisibleFields = map[protoreflect.Name]bool{
	"senderKeyDistributionMessage": true,
	"messageContextInfo":           true,
	"protocolMessage":              true,
	"pollUpdateMessage":            true,
	"encReactionMessage":           true,
	"keepInChatMessage":            true,
	"pinInChatMessage":             true,
	"reactionMessage":              true,
	"encCommentMessage":            true,
	"encEventResponseMessage":      true,
	"secretEncryptedMessage":       true,
	"messageHistoryNotice":         true,
	"messageHistoryBundle":         true,
}

// Un message de WhatsApp, au format de l'interface (nil s'il n'y a rien à montrer).
func (p *Pont) waRow(cli *whatsmeow.Client, conv string, evt *events.Message) *msgRow {
	info := &evt.Info
	m := evt.Message
	if info.ID == "" {
		return nil
	}
	sender := p.canon(cli, info.Sender).String()
	if info.IsFromMe && cli.Store.ID != nil {
		sender = cli.Store.ID.ToNonAD().String()
	}
	p.mu.Lock()
	tmp := p.pending[info.ID]
	senderName := p.nameOf(sender)
	p.mu.Unlock()
	r := &msgRow{
		Message: Message{
			ID:     info.ID,
			Conv:   conv,
			FromMe: info.IsFromMe,
			Sender: cmp.Or(senderName, info.PushName),
			Time:   info.Timestamp.UnixMilli(),
			Kind:   "whatsapp",
			TmpID:  tmp,
		},
		SenderJID: sender,
		ChatJID:   info.Chat.ToNonAD().String(),
	}
	if r.Time <= 0 {
		r.Time = time.Now().UnixMilli()
	}
	if r.FromMe {
		r.Status = "OUTGOING_COMPLETE"
		if web := evt.SourceWebMsg; web != nil {
			switch web.GetStatus() {
			case waWeb.WebMessageInfo_DELIVERY_ACK:
				r.Status = "OUTGOING_DELIVERED"
			case waWeb.WebMessageInfo_READ, waWeb.WebMessageInfo_PLAYED:
				r.Status = "OUTGOING_READ"
			}
		}
	} else {
		r.Status = "INCOMING_COMPLETE"
	}
	add := func(kind string, msg proto.Message, mime, name string, w, h uint32, size uint64) {
		key := encodeMediaKey(kind, msg, mime, info)
		if key == nil {
			return
		}
		r.Attachments = append(r.Attachments, Attachment{
			MediaID: "wa:" + info.ID, Key: key, Mime: mime, Name: name, Size: int64(size), W: int64(w), H: int64(h),
		})
	}
	var text string
	switch {
	case m.Conversation != nil:
		text = m.GetConversation()
	case m.ExtendedTextMessage != nil:
		x := m.GetExtendedTextMessage()
		text = x.GetText()
		if x.GetMatchedText() != "" && !strings.Contains(text, x.GetMatchedText()) {
			text += "\n" + x.GetMatchedText()
		}
		r.LinkPreview = apercuWA(x)
	case m.ImageMessage != nil:
		x := m.GetImageMessage()
		text = x.GetCaption()
		add("image", x, cmp.Or(x.GetMimetype(), "image/jpeg"), "", x.GetWidth(), x.GetHeight(), x.GetFileLength())
	case m.VideoMessage != nil:
		x := m.GetVideoMessage()
		text = x.GetCaption()
		add("video", x, cmp.Or(x.GetMimetype(), "video/mp4"), "", x.GetWidth(), x.GetHeight(), x.GetFileLength())
	case m.PtvMessage != nil:
		x := m.GetPtvMessage()
		add("video", x, cmp.Or(x.GetMimetype(), "video/mp4"), "message vidéo", x.GetWidth(), x.GetHeight(), x.GetFileLength())
	case m.AudioMessage != nil:
		x := m.GetAudioMessage()
		name := ""
		if x.GetPTT() {
			name = "message vocal"
		}
		add("audio", x, cmp.Or(x.GetMimetype(), "audio/ogg"), name, 0, 0, x.GetFileLength())
	case m.DocumentMessage != nil:
		x := m.GetDocumentMessage()
		text = x.GetCaption()
		add("document", x, cmp.Or(x.GetMimetype(), "application/octet-stream"), cmp.Or(x.GetFileName(), x.GetTitle(), "fichier"), 0, 0, x.GetFileLength())
	case m.StickerMessage != nil:
		x := m.GetStickerMessage()
		add("sticker", x, cmp.Or(x.GetMimetype(), "image/webp"), "autocollant", x.GetWidth(), x.GetHeight(), x.GetFileLength())
	case m.ContactMessage != nil:
		text = "👤 Contact : " + m.GetContactMessage().GetDisplayName()
	case m.ContactsArrayMessage != nil:
		x := m.GetContactsArrayMessage()
		var noms []string
		for _, c := range x.GetContacts() {
			noms = append(noms, c.GetDisplayName())
		}
		text = "👥 Contacts : " + strings.Join(noms, ", ")
	case m.LocationMessage != nil:
		x := m.GetLocationMessage()
		text = fmt.Sprintf("📍 %s\nhttps://maps.google.com/?q=%.6f,%.6f", cmp.Or(x.GetName(), x.GetAddress(), "Position"), x.GetDegreesLatitude(), x.GetDegreesLongitude())
	case m.LiveLocationMessage != nil:
		x := m.GetLiveLocationMessage()
		text = fmt.Sprintf("📍 Position en direct\nhttps://maps.google.com/?q=%.6f,%.6f", x.GetDegreesLatitude(), x.GetDegreesLongitude())
	case m.PollCreationMessage != nil || m.PollCreationMessageV2 != nil || m.PollCreationMessageV3 != nil:
		x := cmp.Or(m.GetPollCreationMessage(), m.GetPollCreationMessageV2(), m.GetPollCreationMessageV3())
		var opts []string
		for _, o := range x.GetOptions() {
			opts = append(opts, "• "+o.GetOptionName())
		}
		text = "📊 " + x.GetName() + "\n" + strings.Join(opts, "\n") + "\n(vote sur le téléphone)"
	case m.EventMessage != nil:
		x := m.GetEventMessage()
		text = "📅 " + x.GetName()
		if x.GetStartTime() > 0 {
			text += " — " + time.Unix(x.GetStartTime(), 0).Format("2006-01-02 15:04")
		}
	case m.Call != nil:
		text = "📞 Appel"
	case m.GroupInviteMessage != nil:
		text = "👥 Invitation au groupe « " + m.GetGroupInviteMessage().GetGroupName() + " »"
	case m.ButtonsResponseMessage != nil:
		text = m.GetButtonsResponseMessage().GetSelectedDisplayText()
	case m.ListResponseMessage != nil:
		text = m.GetListResponseMessage().GetTitle()
	case m.TemplateMessage != nil:
		t := m.GetTemplateMessage().GetHydratedTemplate()
		text = strings.TrimSpace(t.GetHydratedTitleText() + "\n" + t.GetHydratedContentText())
	}
	r.Body = text
	r.Text = text
	r.Media = mediaSummary(r.Attachments)
	if ci := contextOf(m); ci != nil && ci.GetStanzaID() != "" {
		who := ""
		if pj, err := types.ParseJID(ci.GetParticipant()); err == nil && !pj.IsEmpty() {
			pjs := p.canon(cli, pj).String()
			p.mu.Lock()
			if p.isMe(cli, pjs) {
				who = "moi"
			} else {
				who = p.nameOf(pjs)
			}
			p.mu.Unlock()
		}
		quoted := ""
		if q := ci.GetQuotedMessage(); q != nil {
			quoted = plainText(q)
			if quoted == "" {
				quoted = mediaWord(q)
			}
		}
		orig := p.getMsg(ci.GetStanzaID())
		if orig != nil {
			quoted = cmp.Or(quoted, snippet(cmp.Or(orig.Body, orig.Text), orig.Attachments))
		}
		// Répond à un de mes messages : l'auteur cité (participant) est moi,
		// ou, faute de participant, l'original gardé est de moi.
		r.ReplyToMe = !r.FromMe && (who == "moi" || (orig != nil && orig.FromMe))
		setReply(r, ci.GetStanzaID(), who, shorten(quoted, 80))
	}
	// Me mentionne : mon numéro ou mon LID parmi les JID mentionnés.
	if ci := contextOf(m); ci != nil && !r.FromMe && len(ci.GetMentionedJID()) > 0 && cli.Store.ID != nil {
		r.MentionsMe = mentionneMoi(ci.GetMentionedJID(), *cli.Store.ID, cli.Store.LID)
	}
	if r.Text == "" && len(r.Attachments) == 0 {
		if !hasVisibleField(m) {
			return nil
		}
		log.Debug().Str("id", info.ID).Msg("message non pris en charge")
		r.Text = "(message non pris en charge : ouvre WhatsApp)"
		r.Body = r.Text
	}
	return r
}

// Vrai si le message a autre chose que des champs techniques.
func hasVisibleField(m *waE2E.Message) bool {
	visible := false
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if !invisibleFields[protoreflect.Name(fd.JSONName())] && !invisibleFields[fd.Name()] {
			visible = true
			return false
		}
		return true
	})
	return visible
}

// Le texte simple d'un message (pour les citations et les modifications).
func plainText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	return cmp.Or(m.GetConversation(), m.GetExtendedTextMessage().GetText(), m.GetImageMessage().GetCaption(),
		m.GetVideoMessage().GetCaption(), m.GetDocumentMessage().GetCaption())
}

func mediaWord(m *waE2E.Message) string {
	switch {
	case m.ImageMessage != nil:
		return "📷 Photo"
	case m.VideoMessage != nil, m.PtvMessage != nil:
		return "🎬 Vidéo"
	case m.AudioMessage != nil:
		return "🎤 Message vocal"
	case m.DocumentMessage != nil:
		return "📎 " + cmp.Or(m.GetDocumentMessage().GetFileName(), "Fichier")
	case m.StickerMessage != nil:
		return "Autocollant"
	}
	return ""
}

// Vrai si un des JID mentionnés (texte) est moi : mon numéro ou mon LID,
// sans tenir compte de l'appareil.
func mentionneMoi(mentionnes []string, moi, lid types.JID) bool {
	for _, s := range mentionnes {
		j, err := types.ParseJID(s)
		if err != nil || j.IsEmpty() {
			continue
		}
		j = j.ToNonAD()
		if (!moi.IsEmpty() && j == moi.ToNonAD()) || (!lid.IsEmpty() && j == lid.ToNonAD()) {
			return true
		}
	}
	return false
}

// Le contexte (citation) d'un message, quel que soit son type.
func contextOf(m *waE2E.Message) *waE2E.ContextInfo {
	for _, ci := range []*waE2E.ContextInfo{
		m.GetExtendedTextMessage().GetContextInfo(), m.GetImageMessage().GetContextInfo(),
		m.GetVideoMessage().GetContextInfo(), m.GetAudioMessage().GetContextInfo(),
		m.GetDocumentMessage().GetContextInfo(), m.GetStickerMessage().GetContextInfo(),
		m.GetPtvMessage().GetContextInfo(), m.GetLocationMessage().GetContextInfo(),
		m.GetContactMessage().GetContextInfo(),
	} {
		if ci != nil {
			return ci
		}
	}
	return nil
}

func quoteLine(s string) string { return "↪ " + s + "\n" }

// Met la citation : champs à part, et une ligne en tête du texte.
func setReply(r *msgRow, id, who, quoted string) {
	r.ReplyTo = id
	line := quoted
	if who != "" {
		line = who + " : " + quoted
	}
	r.ReplyText = line
	r.Text = quoteLine(line) + r.Body
}

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

// Le message à citer dans un envoi.
func replyContext(cli *whatsmeow.Client, reply *msgRow) *waE2E.ContextInfo {
	if reply == nil {
		return nil
	}
	participant := reply.SenderJID
	if reply.FromMe && cli.Store.ID != nil {
		participant = cli.Store.ID.ToNonAD().String()
	}
	quoted := cmp.Or(reply.Body, previewOf("", reply.Attachments), " ")
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(reply.ID),
		Participant:   proto.String(participant),
		QuotedMessage: &waE2E.Message{Conversation: proto.String(quoted)},
	}
}

func textMessage(cli *whatsmeow.Client, text string, reply *msgRow) *waE2E.Message {
	if reply == nil {
		return &waE2E.Message{Conversation: proto.String(text)}
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String(text),
		ContextInfo: replyContext(cli, reply),
	}}
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

func setReaction(l []Reaction, actor string, emoji string, fromMe bool) []Reaction {
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
		if len(r.Reactions) == 0 {
			r.Reactions = old.Reactions
		}
		if len(r.Readers) == 0 {
			r.Readers = old.Readers
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
	c.LastText, c.LastFromMe, c.LastTime = previewOf(cmp.Or(r.Body, r.Text), r.Attachments), r.FromMe, r.Time
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
