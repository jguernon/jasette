package main

// Les événements de signalmeow : messages (texte, pièces jointes, réactions,
// modifications, suppressions), accusés, saisie, contacts.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-signal/pkg/signalmeow"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/events"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/protobuf/signalpb"
)

type eventAny = events.SignalEvent

// L'identifiant d'un message chez Signal : l'ACI de l'auteur et l'horodatage
// d'envoi (ms), comme dans mautrix-signal.
func msgID(sender uuid.UUID, ts uint64) string {
	return sender.String() + "|" + strconv.FormatUint(ts, 10)
}

func parseMsgID(id string) (uuid.UUID, uint64, error) {
	a, b, ok := strings.Cut(id, "|")
	if !ok {
		return uuid.Nil, 0, fmt.Errorf("identifiant de message invalide : %s", id)
	}
	u, err := uuid.Parse(a)
	if err != nil {
		return uuid.Nil, 0, err
	}
	ts, err := strconv.ParseUint(b, 10, 64)
	return u, ts, err
}

// Le gestionnaire de signalmeow. Rendre « false » fait rejouer l'événement
// plus tard : on ne le fait jamais (tout est rangé dans le cache tout de suite).
func (p *Pont) onEvent(raw eventAny) bool {
	switch evt := raw.(type) {
	case *events.ChatEvent:
		p.handleChat(evt)
	case *events.DecryptionError:
		p.handleDecryptionError(evt)
	case *events.Receipt:
		p.handleReceipt(evt)
	case *events.ReadSelf:
		p.handleReadSelf(evt)
	case *events.Call:
		if evt.IsRinging {
			p.adminMessage(evt.Info.ChatID, evt.Info.Sender, evt.Timestamp, "📞 Appel Signal : réponds sur le téléphone")
		}
	case *events.ContactList:
		p.handleContactList(evt)
	case *events.ACIFound:
		p.handleACIFound(evt)
	case *events.DeleteForMe:
		// Effacement d'un message ou d'un fil sur un autre appareil : on
		// garde la copie locale (Jasette n'efface rien sans qu'on le demande).
	case *events.MessageRequestResponse, *events.ViewOnceOpen, *events.PrimaryDeviceSeen:
	case *events.QueueEmpty:
		log.Info().Msg("file d'attente de Signal vidée")
	case *events.LoggedOut:
		log.Warn().Err(evt.Error).Msg("Signal : déconnecté")
		go p.loggedOut()
	case *events.IdentityChange:
		if evt.ServiceID.UUID != p.myACI() {
			p.adminMessage(evt.ServiceID.String(), evt.ServiceID.UUID, uint64(time.Now().UnixMilli()), "🔐 Le numéro de sécurité de cette personne a changé")
		}
	default:
		log.Debug().Type("type", raw).Msg("événement signalmeow ignoré")
	}
	return true
}

func (p *Pont) handleChat(evt *events.ChatEvent) {
	conv := evt.Info.ChatID
	if conv == "" {
		return
	}
	me := p.myACI()
	fromMe := evt.Info.Sender == me
	switch content := evt.Event.(type) {
	case *signalpb.TypingMessage:
		if fromMe {
			return
		}
		p.handleTyping(conv, content.GetAction() == signalpb.TypingMessage_STARTED)
	case *signalpb.EditMessage:
		dm := content.GetDataMessage()
		target := p.getMsg(msgID(evt.Info.Sender, content.GetTargetSentTimestamp()))
		if target == nil || dm == nil {
			return
		}
		nr := p.convertData(conv, evt.Info.Sender, dm)
		if nr == nil {
			return
		}
		target.Text = nr.Text
		if len(nr.Attachments) > 0 {
			target.Attachments, target.Media = nr.Attachments, nr.Media
		}
		p.putMsg(target)
		p.noteLastIfLatest(conv, target)
		p.emitMsg(target, false)
	case *signalpb.DataMessage:
		p.handleData(evt, conv, content, fromMe)
	}
}

func (p *Pont) handleData(evt *events.ChatEvent, conv string, dm *signalpb.DataMessage, fromMe bool) {
	sender := evt.Info.Sender
	// Une modification du groupe (nom, membres, image) : on relit le groupe.
	if gv2 := dm.GetGroupV2(); gv2 != nil && isGroupID(conv) {
		p.mu.Lock()
		c := p.convs[conv]
		stale := c == nil || c.Revision < gv2.GetRevision() || c.Name == ""
		p.mu.Unlock()
		if stale {
			if cli := p.client(); cli != nil {
				go p.refreshGroup(p.ctx, cli, conv, 0)
			}
		}
	}
	switch {
	case dm.GetReaction() != nil:
		p.handleReaction(conv, sender, dm.GetReaction())
		return
	case dm.GetDelete() != nil:
		p.handleDelete(conv, msgID(sender, dm.GetDelete().GetTargetSentTimestamp()))
		return
	case dm.GetAdminDelete() != nil:
		ad := dm.GetAdminDelete()
		author := sender
		if b := ad.GetTargetAuthorAciBinary(); len(b) == 16 {
			author = uuid.UUID(b)
		}
		p.handleDelete(conv, msgID(author, ad.GetTargetSentTimestamp()))
		return
	}
	r := p.convertData(conv, sender, dm)
	if r == nil {
		return
	}
	if fromMe {
		r.Status = "OUTGOING_COMPLETE"
	}
	p.liveMessage(conv, r)
}

// Un message en direct : rangé, puis annoncé à l'interface.
func (p *Pont) liveMessage(conv string, r *msgRow) {
	p.mu.Lock()
	c := p.getOrMakeConv(conv)
	c.LastActivity = max(c.LastActivity, r.Time)
	c.Hidden = false
	if r.FromMe {
		c.ReadWM = max(c.ReadWM, r.Time)
	}
	p.saveConv(conv, c)
	p.listDirty = true
	isNew := c.Name == "" && c.Group
	p.mu.Unlock()
	if isNew {
		if cli := p.client(); cli != nil {
			go p.refreshGroup(p.ctx, cli, conv, 0)
		}
	}
	if r.SenderID != "" {
		p.mu.Lock()
		known := p.contacts[r.SenderID] != nil && p.contacts[r.SenderID].Name != ""
		p.mu.Unlock()
		if !known {
			go p.fetchUnknownNames([]string{r.SenderID})
		}
	}
	// Une conversation à une personne : la personne est le seul membre.
	if !isGroupID(conv) {
		p.mu.Lock()
		if c := p.convs[conv]; c != nil && len(c.Members) == 0 {
			c.Members = []string{conv}
			p.saveConv(conv, c)
		}
		p.mu.Unlock()
	}
	if p.storeRow(r) {
		p.emitMsg(r, true)
	}
	p.stopTyping(conv)
}

// Un message de Signal au format de l'interface (nil s'il n'y a rien à montrer).
func (p *Pont) convertData(conv string, sender uuid.UUID, dm *signalpb.DataMessage) *msgRow {
	me := p.myACI()
	ts := dm.GetTimestamp()
	if ts == 0 {
		ts = uint64(time.Now().UnixMilli())
	}
	r := &msgRow{
		Message: Message{
			ID:     msgID(sender, ts),
			Conv:   conv,
			FromMe: sender == me && me != uuid.Nil,
			Time:   int64(ts),
			Kind:   "signal",
			Status: "INCOMING_COMPLETE",
		},
		SenderID: sender.String(),
	}
	if r.FromMe {
		r.Status = "OUTGOING_COMPLETE"
	}
	r.Text = dm.GetBody()
	if dm.GetIsViewOnce() {
		r.Text = "👁 Média à vue unique : ouvre Signal sur le téléphone pour le voir"
		return r
	}
	for i, a := range dm.GetAttachments() {
		if a == nil {
			continue
		}
		// Un texte trop long pour le message lui-même arrive en pièce jointe.
		if a.GetContentType() == "text/x-signal-plain" {
			if long := p.longText(a); long != "" {
				r.Text = long
			}
			continue
		}
		r.Attachments = append(r.Attachments, attachmentOf(r.ID, i, a, ""))
	}
	if st := dm.GetSticker(); st != nil && st.GetData() != nil {
		att := attachmentOf(r.ID, 0, st.GetData(), "autocollant")
		if att.Mime == "" || att.Mime == "application/octet-stream" {
			att.Mime = "image/webp"
		}
		r.Attachments = append(r.Attachments, att)
	}
	for _, ct := range dm.GetContact() {
		name := strings.TrimSpace(ct.GetName().GetGivenName() + " " + ct.GetName().GetFamilyName())
		var nums []string
		for _, n := range ct.GetNumber() {
			nums = append(nums, n.GetValue())
		}
		line := "👤 " + firstOf(name, ct.GetOrganization(), "Contact")
		if len(nums) > 0 {
			line += " : " + strings.Join(nums, ", ")
		}
		r.Text = joinText(r.Text, line)
	}
	if pc := dm.GetPollCreate(); pc != nil {
		r.Text = joinText(r.Text, "📊 Sondage : "+pc.GetQuestion()+"\n• "+strings.Join(pc.GetOptions(), "\n• ")+"\n(vote dans Signal)")
	}
	if dm.GetPayment() != nil {
		r.Text = joinText(r.Text, "💸 Paiement Signal : ouvre Signal sur le téléphone")
	}
	if dm.GetGiftBadge() != nil {
		r.Text = joinText(r.Text, "🎁 Badge cadeau : ouvre Signal sur le téléphone")
	}
	if dm.GetGroupCallUpdate() != nil && r.Text == "" {
		r.Kind = "admin"
		r.Text = "📞 Appel de groupe Signal"
	}
	if dm.GetFlags()&uint32(signalpb.DataMessage_EXPIRATION_TIMER_UPDATE) != 0 && r.Text == "" && len(r.Attachments) == 0 {
		r.Kind = "admin"
		if t := dm.GetExpireTimer(); t > 0 {
			r.Text = "⏱ Messages éphémères : " + (time.Duration(t) * time.Second).String()
		} else {
			r.Text = "⏱ Messages éphémères désactivés"
		}
	}
	if dm.GetRequiredProtocolVersion() > uint32(signalpb.DataMessage_CURRENT) && r.Text == "" && len(r.Attachments) == 0 {
		r.Text = "(message non pris en charge : ouvre Signal)"
	}
	if r.Text != "" {
		r.LinkPreview = p.apercuSignal(dm)
	}
	r.Media = mediaSummary(r.Attachments)
	if r.Text == "" && len(r.Attachments) == 0 {
		// Mise à jour du groupe, clé de profil, fin d'appel… : rien à montrer.
		return nil
	}
	return r
}

func joinText(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n\n" + b
}

// Une pièce jointe : le « pointeur » de Signal (où la trouver et sa clé de
// déchiffrement) voyage dans le champ « key » ; l'interface le renvoie tel
// quel avec la commande « download ».
func attachmentOf(msg string, index int, a *signalpb.AttachmentPointer, name string) Attachment {
	key, _ := proto.Marshal(a)
	mid := "sg:" + msg + ":" + strconv.Itoa(index)
	if a.GetCdnKey() != "" {
		mid = "sg:" + a.GetCdnKey()
	} else if a.GetCdnId() != 0 {
		mid = "sg:" + strconv.FormatUint(a.GetCdnId(), 10)
	}
	mime := a.GetContentType()
	if name == "" {
		name = a.GetFileName()
	}
	if a.GetFlags()&uint32(signalpb.AttachmentPointer_VOICE_MESSAGE) != 0 {
		name = "message vocal"
	}
	return Attachment{
		MediaID: mid,
		Key:     key,
		Mime:    mime,
		Name:    name,
		Size:    int64(a.GetSize()),
		W:       int64(a.GetWidth()),
		H:       int64(a.GetHeight()),
	}
}

// Le texte complet d'un long message (pièce jointe « text/x-signal-plain »).
func (p *Pont) longText(a *signalpb.AttachmentPointer) string {
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	data, err := signalmeow.DownloadAttachmentWithPointer(ctx, a, nil, nil)
	if err != nil {
		log.Warn().Err(err).Msg("texte long")
		return ""
	}
	return string(data)
}

// Un message « système » (appel, erreur de déchiffrement…).
func (p *Pont) adminMessage(conv string, sender uuid.UUID, ts uint64, text string) {
	if conv == "" {
		return
	}
	r := &msgRow{
		Message: Message{
			ID:     "admin|" + msgID(sender, ts),
			Conv:   conv,
			Text:   text,
			Time:   int64(ts),
			Kind:   "admin",
			Status: "INCOMING_COMPLETE",
		},
	}
	p.liveMessage(conv, r)
}

func (p *Pont) handleDecryptionError(evt *events.DecryptionError) {
	log.Warn().Err(evt.Err).Stringer("de", evt.Sender).Msg("message indéchiffrable")
	p.adminMessage(evt.Sender.String(), evt.Sender, evt.Timestamp, "⚠ Un message n'a pas pu être déchiffré : vérifie dans Signal sur le téléphone")
}

// Une réaction (ou son retrait) : la réaction suit le message visé.
func (p *Pont) handleReaction(conv string, sender uuid.UUID, re *signalpb.DataMessage_Reaction) {
	author, err := signalmeow.ParseStringOrBinaryUUID(re.GetTargetAuthorAci(), re.GetTargetAuthorAciBinary())
	if err != nil || author == uuid.Nil {
		author = sender
	}
	target := p.getMsg(msgID(author, re.GetTargetSentTimestamp()))
	if target == nil {
		log.Debug().Str("fil", conv).Msg("réaction à un message inconnu")
		return
	}
	emoji := re.GetEmoji()
	if re.GetRemove() {
		emoji = ""
	}
	me := p.myACI()
	target.Reactions = setReaction(target.Reactions, sender.String(), emoji, sender == me)
	p.putMsg(target)
	p.emitMsg(target, false)
}

// Un message effacé pour tout le monde par son auteur.
func (p *Pont) handleDelete(conv, id string) {
	target := p.getMsg(id)
	if target == nil {
		return
	}
	target.Text = "🚫 Message supprimé"
	target.Attachments, target.Media, target.Reactions = nil, "", nil
	p.putMsg(target)
	p.noteLastIfLatest(conv, target)
	p.emitMsg(target, false)
}

// « Est en train d'écrire » : s'éteint seul après 15 s (comme Signal).
func (p *Pont) handleTyping(conv string, on bool) {
	p.mu.Lock()
	if t := p.typing[conv]; t != nil {
		t.Stop()
		delete(p.typing, conv)
	}
	if on {
		p.typing[conv] = time.AfterFunc(15*time.Second, func() { p.stopTyping(conv) })
	}
	p.mu.Unlock()
	emit("typing", map[string]any{"conv": conv, "on": on})
}

func (p *Pont) stopTyping(conv string) {
	p.mu.Lock()
	t := p.typing[conv]
	delete(p.typing, conv)
	p.mu.Unlock()
	if t != nil {
		t.Stop()
		emit("typing", map[string]any{"conv": conv, "on": false})
	}
}

// Les accusés de réception et de lecture de mes messages.
func (p *Pont) handleReceipt(evt *events.Receipt) {
	var status string
	switch evt.Content.GetType() {
	case signalpb.ReceiptMessage_DELIVERY:
		status = "OUTGOING_DELIVERED"
	case signalpb.ReceiptMessage_READ, signalpb.ReceiptMessage_VIEWED:
		status = "OUTGOING_READ"
	default:
		return
	}
	me := p.myACI()
	for _, ts := range evt.Content.GetTimestamp() {
		r := p.getMsg(msgID(me, ts))
		if r == nil || !r.FromMe {
			continue
		}
		// Dans un groupe, chaque membre envoie ses accusés : on garde qui a lu.
		added := false
		if status == "OUTGOING_READ" && isGroupID(r.Conv) && evt.Sender != uuid.Nil {
			r.Readers, added = addReader(r.Readers, evt.Sender.String())
		}
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

// J'ai lu des messages sur un autre appareil (le téléphone).
func (p *Pont) handleReadSelf(evt *events.ReadSelf) {
	for _, rd := range evt.Messages {
		sender, err := signalmeow.ParseStringOrBinaryUUID(rd.GetSenderAci(), rd.GetSenderAciBinary())
		if err != nil {
			continue
		}
		r := p.getMsg(msgID(sender, rd.GetTimestamp()))
		if r == nil {
			continue
		}
		p.mu.Lock()
		if c := p.convs[r.Conv]; c != nil && (r.Time > c.ReadWM || r.Time > c.ReceiptWM) {
			c.ReadWM = max(c.ReadWM, r.Time)
			c.ReceiptWM = max(c.ReceiptWM, r.Time)
			p.saveConv(r.Conv, c)
			p.listDirty = true
		}
		p.mu.Unlock()
	}
}

// Les contacts du téléphone (ou du stockage de Signal) : noms et photos.
func (p *Pont) handleContactList(evt *events.ContactList) {
	cli := p.client()
	for _, ct := range evt.Contacts {
		if ct == nil || ct.ACI == uuid.Nil {
			continue
		}
		full := ct
		if !evt.IsFromDB && cli != nil {
			// La liste du téléphone ne contient que le carnet : on complète
			// avec ce que signalmeow sait (profil).
			if r, err := cli.Store.RecipientStore.LoadAndUpdateRecipient(p.ctx, ct.ACI, uuid.Nil, nil); err == nil && r != nil {
				r.ContactAvatar = ct.ContactAvatar
				full = r
			}
		}
		p.updateContact(full)
		if img := full.ContactAvatar.Image; len(img) > 0 {
			// La photo du carnet d'adresses du téléphone, livrée avec la liste.
			_ = saveAvatar("contact:"+ct.ACI.String(), img)
		}
	}
	p.markDirty()
	log.Info().Int("contacts", len(evt.Contacts)).Bool("base", evt.IsFromDB).Msg("contacts reçus")
}

// Une personne connue par son numéro (PNI) a maintenant son ACI : son fil
// change d'identifiant.
func (p *Pont) handleACIFound(evt *events.ACIFound) {
	from, to := evt.PNI.String(), evt.ACI.String()
	p.mu.Lock()
	c := p.convs[from]
	if c == nil {
		p.mu.Unlock()
		return
	}
	delete(p.convs, from)
	if dst := p.convs[to]; dst != nil {
		dst.LastActivity = max(dst.LastActivity, c.LastActivity)
		dst.ReadWM = max(dst.ReadWM, c.ReadWM)
		if c.LastTime > dst.LastTime {
			dst.LastText, dst.LastFromMe, dst.LastTime = c.LastText, c.LastFromMe, c.LastTime
		}
		c = dst
	}
	c.Members = []string{to}
	p.convs[to] = c
	p.saveConv(to, c)
	p.delConv(from)
	p.listDirty = true
	p.mu.Unlock()
	p.moveMsgs(from, to)
	log.Info().Str("de", from).Str("vers", to).Msg("fil renommé (ACI trouvé)")
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

func setReaction(l []Reaction, actor, emoji string, fromMe bool) []Reaction {
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
