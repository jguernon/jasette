package main

// Les conversations chiffrées de bout en bout (« Armadillo ») : Messenger les
// transporte sur l'infrastructure de WhatsApp. On enregistre un appareil
// (comme un navigateur de plus sur le compte), on se connecte avec whatsmeow,
// et on (dé)chiffre avec Signal. Les clés sont dans messenger.db.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/util/ffmpeg"
	"go.mau.fi/whatsmeow"
	armadillo "go.mau.fi/whatsmeow/proto"
	"go.mau.fi/whatsmeow/proto/waArmadilloApplication"
	"go.mau.fi/whatsmeow/proto/waArmadilloXMA"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	"go.mau.fi/whatsmeow/proto/waMsgApplication"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"encoding/json"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

func (p *Pont) waitE2EE(ctx context.Context, d time.Duration) error {
	ctx, annule := context.WithTimeout(ctx, d)
	defer annule()
	if err := p.e2eeReady.Wait(ctx); err != nil {
		return errors.New("le canal chiffré de Messenger n'est pas encore connecté")
	}
	return nil
}

func emitE2EE(state, text string) {
	f := map[string]any{"state": state}
	if text != "" {
		f["text"] = text
	}
	emit("e2ee", f)
}

// Enregistre (la première fois) puis connecte l'appareil chiffré.
func (p *Pont) connectE2EE() {
	p.mu.Lock()
	cli, s, me := p.cli, p.sess, p.me
	if cli == nil || s == nil || me == 0 || p.e2ee != nil {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	if err := p.doConnectE2EE(cli, s, me); err != nil {
		log.Err(err).Msg("canal chiffré")
		emitE2EE("error", "conversations chiffrées indisponibles : "+err.Error())
		if errors.Is(err, messagix.ErrICDCUnauthorized) {
			go p.fullReconnect()
			return
		}
		go func() {
			time.Sleep(time.Minute)
			if p.client() == cli && p.waClient() == nil {
				p.connectE2EE()
			}
		}()
	}
}

func (p *Pont) doConnectE2EE(cli *messagix.Client, s *Session, me int64) error {
	ctx := p.ctx
	emitE2EE("connecting", "")
	var dev = p.waDevice
	var err error
	if dev == nil && s.WADeviceID != 0 {
		dev, err = p.waStore.GetDevice(ctx, waTypes.JID{User: idStr(me), Device: s.WADeviceID, Server: waTypes.MessengerServer})
		if err != nil {
			return fmt.Errorf("appareil chiffré illisible : %w", err)
		}
	}
	isNew := dev == nil
	if isNew {
		dev = p.waStore.NewDevice()
	}
	cli.SetDevice(dev)
	if isNew {
		log.Info().Msg("enregistrement d'un nouvel appareil chiffré")
		if err := cli.RegisterE2EE(ctx, me); err != nil {
			return fmt.Errorf("enregistrement de l'appareil : %w", err)
		}
		if err := dev.Save(ctx); err != nil {
			return fmt.Errorf("sauvegarde de l'appareil : %w", err)
		}
		s.WADeviceID = dev.ID.Device
		saveSession(s)
	}
	wa, err := cli.PrepareE2EEClient()
	if err != nil {
		return err
	}
	wa.SynchronousAck = true
	wa.EnableDecryptedEventBuffer = true
	wa.AddEventHandlerWithSuccessStatus(p.onWAEvent)
	p.mu.Lock()
	p.waDevice = dev
	p.e2ee = wa
	p.mu.Unlock()
	if err := wa.Connect(); err != nil {
		p.mu.Lock()
		p.e2ee = nil
		p.mu.Unlock()
		return fmt.Errorf("connexion au serveur chiffré : %w", err)
	}
	return nil
}

// L'appareil chiffré ne vaut plus rien : on l'oublie et on en refait un (une fois).
func (p *Pont) resetE2EE(deleteDevice bool) {
	p.mu.Lock()
	wa, dev, s := p.e2ee, p.waDevice, p.sess
	p.e2ee, p.waDevice = nil, nil
	retry := !p.e2eeRetried
	p.e2eeRetried = true
	p.mu.Unlock()
	p.e2eeReady.Clear()
	if wa != nil {
		wa.Disconnect()
	}
	if deleteDevice && dev != nil {
		_ = dev.Delete(context.WithoutCancel(p.ctx))
	}
	if s != nil {
		s.WADeviceID = 0
		saveSession(s)
	}
	if retry {
		go p.connectE2EE()
	} else {
		emitE2EE("error", "l'appareil chiffré a été refusé par Messenger ; reconnecte-toi")
	}
}

func (p *Pont) onWAEvent(raw any) bool {
	switch evt := raw.(type) {
	case *events.FBMessage:
		p.handleWAMessage(evt, false)
	case *events.UndecryptableMessage:
		if evt.DecryptFailMode == events.DecryptFailHide {
			return true
		}
		conv := p.ensureWAConv(evt.Info.Chat)
		r := &msgRow{
			Message: Message{
				ID: evt.Info.ID, Conv: conv, FromMe: evt.Info.IsFromMe, Time: evt.Info.Timestamp.UnixMilli(),
				Kind: "e2ee", Text: "🔒 Message chiffré pas encore lisible (il arrivera peut-être plus tard)",
			},
			SenderID: int64(evt.Info.Sender.UserInt()),
			WASender: evt.Info.Sender.ToNonAD().String(),
		}
		r.Status = statusFor(r.FromMe)
		if old := p.getMsg(r.ID); old != nil {
			return true
		}
		p.storeRow(r, true)
		p.emitMsg(r, true)
	case *events.Receipt:
		conv := evt.Chat.User
		switch evt.Type {
		case waTypes.ReceiptTypeReadSelf:
			p.mu.Lock()
			if c := p.convs[conv]; c != nil {
				c.ReadWM = max(c.ReadWM, evt.Timestamp.UnixMilli())
				p.saveConv(conv, c)
				p.listDirty = true
			}
			p.mu.Unlock()
		case waTypes.ReceiptTypeRead, waTypes.ReceiptTypeDelivered:
			status := "OUTGOING_READ"
			if evt.Type == waTypes.ReceiptTypeDelivered {
				status = "OUTGOING_DELIVERED"
			}
			for _, id := range evt.MessageIDs {
				if r := p.getMsg(id); r != nil && r.FromMe && r.Status != "OUTGOING_READ" {
					r.Status = status
					p.putMsg(r)
					p.emitMsg(r, false)
				}
			}
		}
	case *events.ChatPresence:
		emit("typing", map[string]any{"conv": evt.Chat.User, "on": evt.State == waTypes.ChatPresenceComposing})
	case *events.Connected:
		log.Info().Msg("canal chiffré connecté")
		p.mu.Lock()
		p.e2eeRetried = false
		p.mu.Unlock()
		p.e2eeReady.Set()
		emitE2EE("ready", "")
		go p.checkHistoryBackup()
	case *events.Disconnected:
		p.e2eeReady.Clear()
		emitE2EE("connecting", "")
	case *events.OfflineSyncCompleted:
		log.Info().Int("n", evt.Count).Msg("canal chiffré : messages en attente reçus")
	case *events.CATRefreshError:
		log.Err(evt.Error).Msg("canal chiffré : jeton CAT")
		go p.fullReconnect()
	case *events.LoggedOut:
		log.Warn().Any("raison", evt.Reason).Msg("canal chiffré : appareil déconnecté")
		go p.resetE2EE(false)
	case *events.ConnectFailure:
		log.Warn().Any("raison", evt.Reason).Str("message", evt.Message).Msg("canal chiffré : connexion refusée")
		switch evt.Reason {
		case events.ConnectFailureNotFound:
			go p.resetE2EE(true)
		case events.ConnectFailureClientUnknown, events.ConnectFailureGeneric:
			go p.fullReconnect()
		default:
			emitE2EE("error", "connexion chiffrée refusée : "+evt.Reason.String())
		}
	case events.PermanentDisconnect:
		emitE2EE("error", evt.PermanentDisconnectDescription())
	default:
		log.Debug().Type("type", raw).Msg("évènement chiffré ignoré")
	}
	return true
}

func statusFor(fromMe bool) string {
	if fromMe {
		return "OUTGOING_COMPLETE"
	}
	return "INCOMING_COMPLETE"
}

// Le fil d'un message chiffré (créé au besoin).
func (p *Pont) ensureWAConv(chat waTypes.JID) string {
	key, _ := strconv.ParseInt(chat.User, 10, 64)
	tt := table.ENCRYPTED_OVER_WA_ONE_TO_ONE
	if chat.Server == waTypes.GroupServer {
		tt = table.ENCRYPTED_OVER_WA_GROUP
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.convs[chat.User]; c == nil || !c.Type.IsWhatsApp() || c.Hidden {
		c = p.getOrMakeConv(key, tt)
		c.Hidden = false
		p.saveConv(chat.User, c)
		p.listDirty = true
	}
	return chat.User
}

// Un message chiffré reçu (ou restauré de l'historique si fromHistory).
func (p *Pont) handleWAMessage(evt *events.FBMessage, fromHistory bool) *msgRow {
	if evt.Info.Chat.Server == waTypes.BroadcastServer {
		return nil
	}
	conv := p.ensureWAConv(evt.Info.Chat)
	senderID := int64(evt.Info.Sender.UserInt())
	p.mu.Lock()
	me := p.me
	p.mu.Unlock()
	fromMe := evt.Info.IsFromMe || (senderID == me && me != 0)

	switch msg := evt.Message.(type) {
	case *waConsumerApplication.ConsumerApplication:
		payload := msg.GetPayload()
		if app := payload.GetApplicationData(); app != nil {
			if rev := app.GetRevoke(); rev != nil {
				if r := p.getMsg(rev.GetKey().GetID()); r != nil {
					r.Text, r.Media, r.Attachments = "🚫 Message retiré", "", nil
					p.putMsg(r)
					p.emitMsg(r, false)
				}
			}
			return nil
		}
		content := payload.GetContent()
		if content == nil {
			return nil
		}
		if edit := content.GetEditMessage(); edit != nil {
			if r := p.getMsg(edit.GetKey().GetID()); r != nil {
				r.Text = p.waText(edit.GetMessage())
				p.putMsg(r)
				p.emitMsg(r, false)
			}
			return nil
		}
		if re := content.GetReactionMessage(); re != nil {
			if r := p.getMsg(re.GetKey().GetID()); r != nil {
				r.Reactions = setReaction(r.Reactions, senderID, re.GetText(), fromMe)
				p.putMsg(r)
				p.emitMsg(r, false)
			}
			return nil
		}
	case *waArmadilloApplication.Armadillo:
		if msg.GetPayload().GetContent() == nil {
			return nil
		}
	case nil:
		return nil
	}

	r := &msgRow{
		Message: Message{
			ID:     evt.Info.ID,
			Conv:   conv,
			FromMe: fromMe,
			Time:   evt.Info.Timestamp.UnixMilli(),
			Kind:   "e2ee",
			Status: statusFor(fromMe),
		},
		SenderID: senderID,
		WASender: evt.Info.Sender.ToNonAD().String(),
	}
	if !fromMe && evt.Info.PushName != "" {
		r.Sender = evt.Info.PushName
	}
	p.waContent(r, evt)
	if r.Text == "" && len(r.Attachments) == 0 {
		return nil
	}
	r.Media = mediaSummary(r.Attachments)
	if fromHistory {
		r.Read = true
		p.putMsg(r)
		return r
	}
	if p.storeRow(r, true) {
		p.emitMsg(r, true)
	}
	return r
}

// Le texte, avec les mentions « @123@msgr » remplacées par le nom.
func (p *Pont) waText(t *waCommon.MessageText) string {
	text := t.GetText()
	for _, jid := range t.GetMentionedJID() {
		parsed, err := waTypes.ParseJID(jid)
		if err != nil {
			continue
		}
		p.mu.Lock()
		name := p.contactName(int64(parsed.UserInt()))
		p.mu.Unlock()
		if name != "" {
			text = strings.ReplaceAll(text, "@"+jid, "@"+name)
			text = strings.ReplaceAll(text, "@"+parsed.User, "@"+name)
		}
	}
	return text
}

func (p *Pont) waContent(r *msgRow, evt *events.FBMessage) {
	switch msg := evt.Message.(type) {
	case *waConsumerApplication.ConsumerApplication:
		p.waConsumer(r, msg.GetPayload().GetContent())
	case *waArmadilloApplication.Armadillo:
		p.waArmadillo(r, msg.GetPayload().GetContent())
	default:
		r.Text = "(message chiffré non pris en charge : ouvre Messenger)"
	}
	if qm := evt.FBApplication.GetMetadata().GetQuotedMessage(); qm != nil && r.Text != "" {
		if q := p.getMsg(qm.GetStanzaID()); q != nil && q.Text != "" {
			cite := q.Text
			if len([]rune(cite)) > 80 {
				cite = string([]rune(cite)[:80]) + "…"
			}
			r.Text = "↪ « " + cite + " »\n" + r.Text
		}
	}
}

func (p *Pont) waConsumer(r *msgRow, c *waConsumerApplication.ConsumerApplication_Content) {
	if c == nil {
		return
	}
	switch content := c.GetContent().(type) {
	case *waConsumerApplication.ConsumerApplication_Content_MessageText:
		r.Text = p.waText(content.MessageText)
	case *waConsumerApplication.ConsumerApplication_Content_ExtendedTextMessage:
		r.Text = p.waText(content.ExtendedTextMessage.GetText())
	case *waConsumerApplication.ConsumerApplication_Content_ImageMessage:
		if t, err := content.ImageMessage.Decode(); err == nil {
			p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaImage, "image", int64(t.GetAncillary().GetWidth()), int64(t.GetAncillary().GetHeight()))
		}
		r.Text = p.waText(content.ImageMessage.GetCaption())
	case *waConsumerApplication.ConsumerApplication_Content_StickerMessage:
		if t, err := content.StickerMessage.Decode(); err == nil {
			if t.GetIntegral().GetTransport().GetIntegral().GetDirectPath() != "" {
				p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaImage, "autocollant", int64(t.GetAncillary().GetWidth()), int64(t.GetAncillary().GetHeight()))
			} else {
				r.Text = "(autocollant)"
			}
		}
	case *waConsumerApplication.ConsumerApplication_Content_VideoMessage:
		if t, err := content.VideoMessage.Decode(); err == nil {
			p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaVideo, "video", int64(t.GetAncillary().GetWidth()), int64(t.GetAncillary().GetHeight()))
		}
		r.Text = p.waText(content.VideoMessage.GetCaption())
	case *waConsumerApplication.ConsumerApplication_Content_AudioMessage:
		if t, err := content.AudioMessage.Decode(); err == nil {
			p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaAudio, "audio", 0, 0)
		}
	case *waConsumerApplication.ConsumerApplication_Content_DocumentMessage:
		if t, err := content.DocumentMessage.Decode(); err == nil {
			p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaDocument, firstOf(content.DocumentMessage.GetFileName(), "fichier"), 0, 0)
		}
	case *waConsumerApplication.ConsumerApplication_Content_ViewOnceMessage:
		switch v := content.ViewOnceMessage.GetViewOnceContent().(type) {
		case *waConsumerApplication.ConsumerApplication_ViewOnceMessage_ImageMessage:
			if t, err := v.ImageMessage.Decode(); err == nil {
				p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaImage, "image", int64(t.GetAncillary().GetWidth()), int64(t.GetAncillary().GetHeight()))
			}
		case *waConsumerApplication.ConsumerApplication_ViewOnceMessage_VideoMessage:
			if t, err := v.VideoMessage.Decode(); err == nil {
				p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaVideo, "video", int64(t.GetAncillary().GetWidth()), int64(t.GetAncillary().GetHeight()))
			}
		}
	case *waConsumerApplication.ConsumerApplication_Content_LocationMessage:
		loc := content.LocationMessage.GetLocation()
		r.Text = fmt.Sprintf("📍 %s\n%s\nhttps://www.openstreetmap.org/?mlat=%f&mlon=%f",
			loc.GetName(), content.LocationMessage.GetAddress(), loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
		r.Text = strings.ReplaceAll(strings.TrimSpace(r.Text), "📍 \n\n", "📍 ")
	case *waConsumerApplication.ConsumerApplication_Content_LiveLocationMessage:
		r.Text = "📍 Partage de position en direct (visible dans Messenger)"
	case *waConsumerApplication.ConsumerApplication_Content_ContactMessage, *waConsumerApplication.ConsumerApplication_Content_ContactsArrayMessage:
		r.Text = "👤 Fiche de contact (visible dans Messenger)"
	case *waConsumerApplication.ConsumerApplication_Content_PollCreationMessage:
		r.Text = "📊 Sondage : " + content.PollCreationMessage.GetName()
		for _, o := range content.PollCreationMessage.GetOptions() {
			r.Text += "\n• " + o.GetOptionName()
		}
	case *waConsumerApplication.ConsumerApplication_Content_PollUpdateMessage:
		return
	default:
		log.Debug().Type("type", content).Msg("contenu chiffré non pris en charge")
		r.Text = "(message chiffré non pris en charge : ouvre Messenger)"
	}
}

func (p *Pont) waArmadillo(r *msgRow, c *waArmadilloApplication.Armadillo_Content) {
	switch content := c.GetContent().(type) {
	case *waArmadilloApplication.Armadillo_Content_ExtendedContentMessage:
		x := content.ExtendedContentMessage
		text := x.GetMessageText()
		var extra []string
		if x.GetTargetType() != waArmadilloXMA.ExtendedContentMessage_MSG_LOCATION_SHARING_V2 {
			if t := x.GetTitleText(); t != "" && !strings.Contains(text, t) {
				extra = append(extra, t)
			}
			if t := x.GetSubtitleText(); t != "" && !strings.Contains(text, t) {
				extra = append(extra, t)
			}
		} else {
			extra = append(extra, "📍 "+x.GetTitleText())
		}
		for _, cta := range x.GetCtas() {
			for _, u := range []string{cta.GetNativeURL(), cta.GetActionURL()} {
				if strings.HasPrefix(u, "http") && !strings.Contains(text, u) {
					extra = append(extra, u)
					break
				}
			}
		}
		if len(extra) > 0 {
			if text != "" {
				text += "\n\n"
			}
			text += strings.Join(extra, "\n")
		}
		r.Text = firstOf(text, "(contenu partagé : ouvre Messenger)")
	case *waArmadilloApplication.Armadillo_Content_RavenMessage_:
		p.waRaven(r, content.RavenMessage)
	case *waArmadilloApplication.Armadillo_Content_RavenMessageMsgr:
		p.waRaven(r, content.RavenMessageMsgr)
	case *waArmadilloApplication.Armadillo_Content_ImageGalleryMessage_:
		imgs, err := content.ImageGalleryMessage.Decode()
		if err == nil {
			for _, t := range imgs {
				p.waAttach(r, t.GetIntegral().GetTransport(), whatsmeow.MediaImage, "image", int64(t.GetAncillary().GetWidth()), int64(t.GetAncillary().GetHeight()))
			}
		}
	case *waArmadilloApplication.Armadillo_Content_BumpExistingMessage_:
		r.Text = "⤴ a remonté un message"
	default:
		log.Debug().Type("type", content).Msg("contenu Armadillo non pris en charge")
		r.Text = "(message chiffré non pris en charge : ouvre Messenger)"
	}
}

func (p *Pont) waRaven(r *msgRow, m *waArmadilloApplication.Armadillo_Content_RavenMessage) {
	if img, err := m.DecodeImageMessage(); err == nil && img != nil && img.GetIntegral() != nil {
		p.waAttach(r, img.GetIntegral().GetTransport(), whatsmeow.MediaImage, "image", int64(img.GetAncillary().GetWidth()), int64(img.GetAncillary().GetHeight()))
		return
	}
	if vid, err := m.DecodeVideoMessage(); err == nil && vid != nil && vid.GetIntegral() != nil {
		p.waAttach(r, vid.GetIntegral().GetTransport(), whatsmeow.MediaVideo, "video", int64(vid.GetAncillary().GetWidth()), int64(vid.GetAncillary().GetHeight()))
		return
	}
	r.Text = "(photo éphémère : ouvre Messenger)"
}

// Ajoute une pièce jointe chiffrée (téléchargée plus tard, à la demande).
func (p *Pont) waAttach(r *msgRow, t *waMediaTransport.WAMediaTransport, mt whatsmeow.MediaType, name string, w, h int64) {
	if t == nil || t.GetIntegral() == nil {
		return
	}
	in := t.GetIntegral()
	mime := t.GetAncillary().GetMimetype()
	if mime == "" {
		switch mt {
		case whatsmeow.MediaImage:
			mime = "image/jpeg"
		case whatsmeow.MediaVideo:
			mime = "video/mp4"
		case whatsmeow.MediaAudio:
			mime = "audio/mp4"
		default:
			mime = "application/octet-stream"
		}
	}
	// Facebook met parfois des images dans des messages vidéo.
	if mt == whatsmeow.MediaVideo && strings.HasPrefix(mime, "image/") {
		name = "image"
	}
	key, _ := json.Marshal(mediaKey{
		WA: true, Type: string(mt), Mime: mime,
		Key: in.GetMediaKey(), SHA256: in.GetFileSHA256(), EncSHA256: in.GetFileEncSHA256(), Path: in.GetDirectPath(),
	})
	if !strings.ContainsRune(name, '.') && name != "autocollant" {
		name += extension(mime, nil)
	}
	r.Attachments = append(r.Attachments, Attachment{
		MediaID: "wa:" + r.ID + ":" + strconv.Itoa(len(r.Attachments)),
		Key:     key,
		Mime:    mime,
		Name:    name,
		Size:    int64(t.GetAncillary().GetFileLength()),
		W:       w,
		H:       h,
	})
}

// ---- Envoi chiffré ----

func (p *Pont) chatJID(c *convState) waTypes.JID {
	if c.Type == table.ENCRYPTED_OVER_WA_GROUP {
		return waTypes.NewJID(idStr(c.Key), waTypes.GroupServer)
	}
	return waTypes.NewJID(idStr(c.Key), waTypes.MessengerServer)
}

func (p *Pont) sendE2EE(conv string, c *convState, otid int64, text, path, mime string) (string, []Attachment, error) {
	if err := p.waitE2EE(p.ctx, time.Minute); err != nil {
		return "", nil, err
	}
	wa := p.waClient()
	if wa == nil {
		return "", nil, errors.New("canal chiffré pas connecté")
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	var content waConsumerApplication.ConsumerApplication_Content
	var atts []Attachment
	if path != "" {
		data, m, err := readMedia(path, mime)
		if err != nil {
			return "", nil, err
		}
		c, a, err := p.waMedia(ctx, wa, path, data, m, text)
		if err != nil {
			return "", nil, fmt.Errorf("envoi du fichier : %w", err)
		}
		content.Content, atts = c, a
	} else {
		content.Content = &waConsumerApplication.ConsumerApplication_Content_MessageText{
			MessageText: &waCommon.MessageText{Text: proto.String(text)},
		}
	}
	msg := &waConsumerApplication.ConsumerApplication{
		Payload: &waConsumerApplication.ConsumerApplication_Payload{
			Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{Content: &content},
		},
	}
	meta := &waMsgApplication.MessageApplication_Metadata{}
	id := strconv.FormatInt(otid, 10)
	chat := p.chatJID(c)
	if _, err := wa.SendFBMessage(ctx, chat, msg, meta, whatsmeow.SendRequestExtra{ID: id}); err != nil {
		return "", nil, fmt.Errorf("envoi chiffré : %w", err)
	}
	p.uploadSentToHistory(chat, id, msg, meta)
	return id, atts, nil
}

func (p *Pont) waMedia(ctx context.Context, wa *whatsmeow.Client, path string, data []byte, mime, caption string) (waConsumerApplication.ConsumerApplication_Content_Content, []Attachment, error) {
	name := path[strings.LastIndexByte(path, '/')+1:]
	var w, h int
	isImage := strings.HasPrefix(mime, "image/")
	if isImage {
		if mime == "image/png" {
			data = trimPNGTrailer(data)
		}
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			w, h = cfg.Width, cfg.Height
		}
	}
	isAudio := strings.HasPrefix(mime, "audio/")
	// Messenger veut ses messages vocaux en AAC (m4a), comme mautrix-meta.
	if isAudio && !strings.HasPrefix(mime, "audio/mp4") && ffmpegOK() {
		conv, err := ffmpeg.ConvertBytes(ctx, data, ".m4a", []string{}, []string{"-c:a", "aac"}, mime)
		if err == nil {
			data, mime, name = conv, "audio/mp4", name+".m4a"
		} else {
			log.Warn().Err(err).Msg("conversion du message vocal")
		}
	}
	mt := whatsmeow.MediaDocument
	gif := mime == "image/gif"
	switch {
	case isImage && !gif:
		mt = whatsmeow.MediaImage
	case gif, strings.HasPrefix(mime, "video/"):
		mt = whatsmeow.MediaVideo
	case isAudio:
		mt = whatsmeow.MediaAudio
	}
	up, err := wa.Upload(ctx, data, mt)
	if err != nil {
		return nil, nil, err
	}
	tw, th := clampTo400(w, h)
	if tw == 0 && isImage {
		tw, th = 400, 400
	}
	var fileLength *uint64
	if mt != whatsmeow.MediaVideo {
		fileLength = proto.Uint64(uint64(len(data)))
	}
	transport := &waMediaTransport.WAMediaTransport{
		Integral: &waMediaTransport.WAMediaTransport_Integral{
			FileSHA256:        up.FileSHA256,
			MediaKey:          up.MediaKey,
			FileEncSHA256:     up.FileEncSHA256,
			DirectPath:        &up.DirectPath,
			MediaKeyTimestamp: proto.Int64(time.Now().Unix()),
		},
		Ancillary: &waMediaTransport.WAMediaTransport_Ancillary{
			FileLength: fileLength,
			Mimetype:   &mime,
			Thumbnail: &waMediaTransport.WAMediaTransport_Ancillary_Thumbnail{
				ThumbnailWidth:  proto.Uint32(uint32(tw)),
				ThumbnailHeight: proto.Uint32(uint32(th)),
			},
			ObjectID: &up.ObjectID,
		},
	}
	capt := &waCommon.MessageText{}
	if caption != "" {
		capt.Text = proto.String(caption)
	}
	atts := localAttachment(path, mime, data)
	switch mt {
	case whatsmeow.MediaImage:
		m := &waConsumerApplication.ConsumerApplication_ImageMessage{Caption: capt}
		err = m.Set(&waMediaTransport.ImageTransport{
			Integral:  &waMediaTransport.ImageTransport_Integral{Transport: transport},
			Ancillary: &waMediaTransport.ImageTransport_Ancillary{Width: proto.Uint32(uint32(w)), Height: proto.Uint32(uint32(h))},
		})
		return &waConsumerApplication.ConsumerApplication_Content_ImageMessage{ImageMessage: m}, atts, err
	case whatsmeow.MediaVideo:
		m := &waConsumerApplication.ConsumerApplication_VideoMessage{Caption: capt}
		err = m.Set(&waMediaTransport.VideoTransport{
			Integral: &waMediaTransport.VideoTransport_Integral{Transport: transport},
			Ancillary: &waMediaTransport.VideoTransport_Ancillary{
				Width: proto.Uint32(uint32(w)), Height: proto.Uint32(uint32(h)),
				Caption: capt, GifPlayback: proto.Bool(gif), Sidecar: up.StreamingSidecar, IsHd: proto.Bool(false),
			},
		})
		return &waConsumerApplication.ConsumerApplication_Content_VideoMessage{VideoMessage: m}, atts, err
	case whatsmeow.MediaAudio:
		m := &waConsumerApplication.ConsumerApplication_AudioMessage{PTT: proto.Bool(true)}
		err = m.Set(&waMediaTransport.AudioTransport{
			Integral:  &waMediaTransport.AudioTransport_Integral{Transport: transport},
			Ancillary: &waMediaTransport.AudioTransport_Ancillary{},
		})
		return &waConsumerApplication.ConsumerApplication_Content_AudioMessage{AudioMessage: m}, atts, err
	default:
		m := &waConsumerApplication.ConsumerApplication_DocumentMessage{FileName: &name}
		err = m.Set(&waMediaTransport.DocumentTransport{
			Integral:  &waMediaTransport.DocumentTransport_Integral{Transport: transport},
			Ancillary: &waMediaTransport.DocumentTransport_Ancillary{},
		})
		return &waConsumerApplication.ConsumerApplication_Content_DocumentMessage{DocumentMessage: m}, atts, err
	}
}

func ffmpegOK() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil && ffmpeg.Supported()
}

func clampTo400(w, h int) (int, int) {
	if w > 400 {
		h = h * 400 / w
		w = 400
	}
	if h > 400 {
		w = w * 400 / h
		h = 400
	}
	return w, h
}

// Messenger mobile refuse les PNG avec des données après IEND.
func trimPNGTrailer(data []byte) []byte {
	const header = "\x89PNG\r\n\x1a\n"
	if !bytes.HasPrefix(data, []byte(header)) {
		return data
	}
	for pos := 8; pos+12 <= len(data); {
		length := int(uint32(data[pos])<<24 | uint32(data[pos+1])<<16 | uint32(data[pos+2])<<8 | uint32(data[pos+3]))
		end := pos + 12 + length
		if end > len(data) || end <= pos || length < 0 {
			break
		}
		if string(data[pos+4:pos+8]) == "IEND" {
			return data[:end]
		}
		pos = end
	}
	return data
}

// Marque lus les messages chiffrés reçus (accusés envoyés aux expéditeurs).
func (p *Pont) markReadE2EE(conv string, c *convState, ts int64) {
	wa := p.waClient()
	if wa == nil || !p.e2eeReady.IsSet() {
		return
	}
	rows := p.unreadIncoming(conv, ts)
	if len(rows) == 0 {
		return
	}
	chat := p.chatJID(c)
	bySender := map[string][]string{}
	for _, r := range rows {
		k := ""
		if chat.Server == waTypes.GroupServer {
			k = r.WASender
		}
		bySender[k] = append(bySender[k], r.ID)
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	for sender, ids := range bySender {
		var sj waTypes.JID
		if sender != "" {
			sj, _ = waTypes.ParseJID(sender)
		}
		if err := wa.MarkRead(ctx, ids, time.Now(), chat, sj); err != nil {
			log.Warn().Err(err).Msg("lu (chiffré)")
			continue
		}
	}
	for _, r := range rows {
		r.Read = true
		p.putMsg(r)
	}
}

var _ armadillo.RealMessageApplicationSub = (*waConsumerApplication.ConsumerApplication)(nil)
