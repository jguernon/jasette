package main

// L'historique sécurisé des conversations chiffrées (« stockage sécurisé » de
// Messenger, protégé par un NIP à 6 chiffres). Sans lui, un nouvel appareil
// ne voit que les messages chiffrés reçus après sa connexion.
//
// Événements : history_pin{needed:true}        -> l'interface demande le NIP ;
//              history_pin{ok:true}             -> historique restauré ;
//              history_pin{ok:false,text,attempts?} -> NIP refusé, etc.
// Commande   : history_pin{pin:"123456"} (pin vide = ne plus demander).

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"go.mau.fi/whatsmeow"
	armadillo "go.mau.fi/whatsmeow/proto"
	"go.mau.fi/whatsmeow/proto/waArmadilloApplication"
	"go.mau.fi/whatsmeow/proto/waArmadilloBackupMessage"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	"go.mau.fi/whatsmeow/proto/waMsgApplication"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-meta/pkg/messagix/securehistory"
)

// Après la connexion chiffrée : y a-t-il un historique à restaurer ?
func (p *Pont) checkHistoryBackup() {
	p.mu.Lock()
	s, cli := p.sess, p.cli
	if s == nil || cli == nil || p.historyChecked || s.HistoryDeclined || (s.HistoryDevice != nil && s.HistoryDevice.ID != "") {
		p.mu.Unlock()
		return
	}
	p.historyChecked = true
	p.mu.Unlock()
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	backup, err := cli.GetSecureHistoryClient().GetBackup(ctx)
	if errors.Is(err, securehistory.ErrNoPINBackup) {
		log.Info().Msg("pas d'historique chiffré sauvegardé sur ce compte")
		return
	} else if err != nil {
		log.Warn().Err(err).Msg("historique chiffré : vérification")
		return
	}
	p.mu.Lock()
	p.historyBackup = backup
	p.mu.Unlock()
	emit("history_pin", map[string]any{"needed": true})
}

func (p *Pont) historyPIN(pin string) {
	p.mu.Lock()
	s, cli, me := p.sess, p.cli, p.me
	p.mu.Unlock()
	if s == nil || cli == nil {
		emit("history_pin", map[string]any{"ok": false, "text": "Messenger n'est pas connecté"})
		return
	}
	if pin == "" {
		s.HistoryDeclined = true
		saveSession(s)
		emit("history_pin", map[string]any{"ok": false, "declined": true})
		return
	}
	if len(pin) != 6 || strings6(pin) {
		emit("history_pin", map[string]any{"ok": false, "text": "le NIP doit avoir 6 chiffres"})
		return
	}
	if err := p.waitE2EE(p.ctx, time.Minute); err != nil {
		emit("history_pin", map[string]any{"ok": false, "text": err.Error()})
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 2*time.Minute)
	defer annule()
	err := p.restoreHistory(ctx, s, me, []byte(pin))
	if err != nil {
		log.Warn().Err(err).Msg("historique chiffré : restauration")
		ev := map[string]any{"ok": false, "text": "impossible de restaurer l'historique : " + err.Error()}
		var re *securehistory.RecoveryError
		if errors.As(err, &re) && re.AttemptsRemaining != nil {
			ev["attempts"] = *re.AttemptsRemaining
			ev["text"] = fmt.Sprintf("NIP refusé (encore %d essais)", *re.AttemptsRemaining)
			if *re.AttemptsRemaining == 0 {
				ev["text"] = "NIP refusé : plus d'essais permis par Messenger"
			}
		}
		emit("history_pin", ev)
		return
	}
	s.HistoryDeclined = false
	saveSession(s)
	emit("history_pin", map[string]any{"ok": true})
}

func strings6(pin string) bool {
	for _, c := range pin {
		if c < '0' || c > '9' {
			return true
		}
	}
	return false
}

func (p *Pont) restoreHistory(ctx context.Context, s *Session, me int64, pin []byte) error {
	p.mu.Lock()
	cli, dev, backup := p.cli, p.waDevice, p.historyBackup
	p.mu.Unlock()
	if cli == nil || dev == nil {
		return errors.New("canal chiffré pas prêt")
	}
	history := cli.GetSecureHistoryClient()
	if backup == nil {
		var err error
		if backup, err = history.GetBackup(ctx); err != nil {
			if errors.Is(err, securehistory.ErrNoPINBackup) {
				return errors.New("ce compte n'a pas d'historique sauvegardé")
			}
			return err
		}
	}
	recovery, err := history.RecoverPIN(ctx, backup, pin)
	if err != nil {
		return err
	}
	virtual, err := history.GetVirtualDevice(ctx, backup, me, recovery.Code)
	if err != nil {
		return err
	}
	epochs, err := virtual.DeriveEpochs()
	if err != nil {
		return err
	}
	enrollment, err := virtual.PrepareDevice(backup.ID, history.RegistrationID, dev.IdentityKey, epochs)
	if err != nil {
		return err
	}
	// Gardé avant l'ajout : si le serveur l'accepte mais que la réponse se perd,
	// on a encore les clés.
	s.HistoryDevice = enrollment.Device
	saveSession(s)
	device, err := history.AddDevice(ctx, enrollment)
	if err != nil {
		var rejected *securehistory.EnrollmentError
		if errors.As(err, &rejected) {
			s.HistoryDevice = nil
			saveSession(s)
		}
		return err
	}
	log.Info().Str("appareil", device.ID).Msg("historique chiffré : appareil inscrit")
	s.HistoryDevice = device
	saveSession(s)
	return nil
}

// Une page de l'historique chiffré d'un fil, rangée dans le cache.
func (p *Pont) fetchSecureHistory(conv string, c *convState, beforeT int64, count int) (bool, error) {
	p.mu.Lock()
	s, cli, me := p.sess, p.cli, p.me
	p.mu.Unlock()
	if s == nil || cli == nil || s.HistoryDevice == nil {
		return false, nil
	}
	device := s.HistoryDevice
	ts := beforeT
	if ts <= 0 {
		ts = time.Now().Add(time.Minute).UnixMilli()
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	chat := p.chatJID(c)
	page, err := cli.GetSecureHistoryClient().FetchMessages(ctx, device, chat.User, true, ts, max(count, 20))
	if err != nil {
		return false, err
	}
	if page.Epochs != nil && len(page.Epochs.Edges) > 0 {
		nd, err := device.WithEpochs(page.Epochs)
		if err != nil {
			return false, err
		}
		device = nd
		p.mu.Lock()
		s.HistoryDevice = nd
		p.mu.Unlock()
		saveSession(s)
	}
	more := page.Range != nil && page.Range.HasMoreBefore != nil && *page.Range.HasMoreBefore
	n := 0
	for _, dm := range page.Decrypt(ctx, device, chat.User, me, chat.Server == waTypes.GroupServer) {
		if p.archiveMessage(chat, dm) {
			n++
		}
	}
	log.Debug().Int("n", n).Str("fil", conv).Bool("encore", more).Msg("historique chiffré")
	return more, nil
}

func (p *Pont) decodeArchive(chat waTypes.JID, m *waArmadilloBackupMessage.BackupMessage, ts int64) (*events.FBMessage, error) {
	var decoded events.FBMessage
	switch payload := m.GetPayload().(type) {
	case *waArmadilloBackupMessage.BackupMessage_EncryptedTransportMessage:
		var app waMsgApplication.MessageApplication
		if err := proto.Unmarshal(payload.EncryptedTransportMessage, &app); err != nil {
			return nil, err
		}
		var err error
		if decoded, err = whatsmeow.DecodeFBApplication(&app); err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}
	md := m.GetMetadata()
	if md.GetMessageID() == "" || md.GetSenderID() == "" {
		return nil, errors.New("message d'archive sans identifiant")
	}
	p.mu.Lock()
	me := p.me
	p.mu.Unlock()
	decoded.Info = waTypes.MessageInfo{
		MessageSource: waTypes.MessageSource{
			Chat:     chat,
			Sender:   waTypes.NewJID(md.GetSenderID(), waTypes.MessengerServer),
			IsFromMe: md.GetSenderID() == idStr(me),
			IsGroup:  chat.Server == waTypes.GroupServer,
		},
		ID:        md.GetMessageID(),
		Timestamp: time.UnixMilli(ts),
	}
	return &decoded, nil
}

// Range un message de l'archive (avec sa dernière modification et ses réactions).
func (p *Pont) archiveMessage(chat waTypes.JID, dm *securehistory.DecryptedMessage) bool {
	if dm == nil || dm.Message == nil || dm.Message.Message == nil {
		return false
	}
	parent, err := p.decodeArchive(chat, dm.Message.Message, dm.Message.TimestampMS)
	if err != nil || parent == nil {
		if err != nil {
			log.Debug().Err(err).Str("id", dm.ID).Msg("archive illisible")
		}
		return false
	}
	type reac struct {
		ts    int64
		emoji string
		from  int64
	}
	reacs := map[string]reac{}
	var lastEdit *waConsumerApplication.ConsumerApplication_EditMessage
	var editTS int64
	for _, sup := range dm.Supplementals {
		if sup == nil || sup.Message == nil {
			continue
		}
		d, err := p.decodeArchive(chat, sup.Message, sup.TimestampMS)
		if err != nil || d == nil {
			continue
		}
		content := d.GetConsumerApplication().GetPayload().GetContent()
		if re := content.GetReactionMessage(); re != nil {
			prev, ok := reacs[d.Info.Sender.User]
			if !ok || sup.TimestampMS >= prev.ts {
				reacs[d.Info.Sender.User] = reac{sup.TimestampMS, re.GetText(), int64(d.Info.Sender.UserInt())}
			}
		} else if ed := content.GetEditMessage(); ed != nil && ed.GetMessage() != nil {
			if lastEdit == nil || sup.TimestampMS > editTS {
				lastEdit, editTS = ed, sup.TimestampMS
			}
		}
	}
	if lastEdit != nil {
		parent.Message = &waConsumerApplication.ConsumerApplication{
			Payload: &waConsumerApplication.ConsumerApplication_Payload{
				Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{
					Content: &waConsumerApplication.ConsumerApplication_Content{
						Content: &waConsumerApplication.ConsumerApplication_Content_MessageText{MessageText: lastEdit.GetMessage()},
					},
				},
			},
		}
	}
	// Les pièces jointes de l'archive ont leur propre adresse sur le CDN.
	paths := map[string]string{}
	for _, a := range dm.Attachments {
		if a.ObjectID != "" && a.URL != "" {
			paths[a.ObjectID] = a.URL
		}
	}
	if p.getMsg(parent.Info.ID) != nil {
		return false
	}
	r := p.handleWAMessage(parent, true)
	if r == nil {
		return false
	}
	if len(paths) > 0 {
		p.fixArchivePaths(r, parent, paths)
	}
	p.mu.Lock()
	me := p.me
	p.mu.Unlock()
	keys := make([]string, 0, len(reacs))
	for k := range reacs {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(reacs[a].ts, reacs[b].ts) })
	for _, k := range keys {
		re := reacs[k]
		if re.emoji != "" {
			r.Reactions = setReaction(r.Reactions, re.from, re.emoji, re.from == me)
		}
	}
	p.putMsg(r)
	p.noteLast(r.Conv, r)
	return true
}

// Remplace le chemin WhatsApp des pièces jointes par l'adresse de l'archive.
func (p *Pont) fixArchivePaths(r *msgRow, evt *events.FBMessage, paths map[string]string) {
	var objectIDs []string
	switch msg := evt.Message.(type) {
	case *waConsumerApplication.ConsumerApplication:
		c := msg.GetPayload().GetContent()
		var oid string
		switch {
		case c.GetImageMessage() != nil:
			if t, err := c.GetImageMessage().Decode(); err == nil {
				oid = t.GetIntegral().GetTransport().GetAncillary().GetObjectID()
			}
		case c.GetVideoMessage() != nil:
			if t, err := c.GetVideoMessage().Decode(); err == nil {
				oid = t.GetIntegral().GetTransport().GetAncillary().GetObjectID()
			}
		case c.GetAudioMessage() != nil:
			if t, err := c.GetAudioMessage().Decode(); err == nil {
				oid = t.GetIntegral().GetTransport().GetAncillary().GetObjectID()
			}
		case c.GetDocumentMessage() != nil:
			if t, err := c.GetDocumentMessage().Decode(); err == nil {
				oid = t.GetIntegral().GetTransport().GetAncillary().GetObjectID()
			}
		case c.GetStickerMessage() != nil:
			if t, err := c.GetStickerMessage().Decode(); err == nil {
				oid = t.GetIntegral().GetTransport().GetAncillary().GetObjectID()
			}
		}
		objectIDs = append(objectIDs, oid)
	case *waArmadilloApplication.Armadillo:
		if g := msg.GetPayload().GetContent().GetImageGalleryMessage(); g != nil {
			if imgs, err := g.Decode(); err == nil {
				for _, t := range imgs {
					objectIDs = append(objectIDs, t.GetIntegral().GetTransport().GetAncillary().GetObjectID())
				}
			}
		}
	}
	for i := range r.Attachments {
		if i >= len(objectIDs) || paths[objectIDs[i]] == "" {
			continue
		}
		var k mediaKey
		if json.Unmarshal(r.Attachments[i].Key, &k) == nil && k.WA {
			k.Path = paths[objectIDs[i]]
			r.Attachments[i].Key, _ = json.Marshal(k)
		}
	}
}

// Les messages que j'envoie dans un fil chiffré 1-à-1 sont aussi déposés dans
// l'historique sécurisé (comme le fait Messenger), pour mes autres appareils.
func (p *Pont) uploadSentToHistory(chat waTypes.JID, messageID string, msg armadillo.RealMessageApplicationSub, meta *waMsgApplication.MessageApplication_Metadata) {
	p.mu.Lock()
	s, cli, dev, me := p.sess, p.cli, p.waDevice, p.me
	p.mu.Unlock()
	if s == nil || cli == nil || dev == nil || dev.IdentityKey == nil || s.HistoryDevice == nil || s.HistoryDevice.ID == "" ||
		!s.Platform.IsMessenger() || chat.Server != waTypes.MessengerServer || meta == nil || len(meta.GetFrankingKey()) == 0 {
		return
	}
	hc := cli.GetSecureHistoryClient()
	if hc.AppID == "" {
		return
	}
	go func() {
		ctx, annule := context.WithTimeout(context.WithoutCancel(p.ctx), 2*time.Minute)
		defer annule()
		other, err := strconv.ParseInt(chat.User, 10, 64)
		if err != nil {
			return
		}
		otid, err := strconv.ParseInt(messageID, 10, 64)
		if err != nil {
			return
		}
		transport, err := marshalSentFBMessage(msg, meta)
		if err != nil {
			return
		}
		fr := hmac.New(sha256.New, meta.GetFrankingKey())
		fr.Write(transport)
		err = hc.UploadSentMessage(ctx, &securehistory.MinosSentMessage{
			BackupID:       s.HistoryDevice.BackupID,
			SenderID:       me,
			OtherUserID:    other,
			MessageID:      messageID,
			TimestampMS:    otid >> 22,
			TransportBytes: transport,
			FrankingTag:    fr.Sum(nil),
			IdentityPriv:   dev.IdentityKey.Priv,
			IdentityPub:    dev.IdentityKey.Pub,
		})
		if err != nil {
			log.Warn().Err(err).Msg("dépôt dans l'historique chiffré")
		}
	}()
}

func marshalSentFBMessage(message armadillo.RealMessageApplicationSub, metadata *waMsgApplication.MessageApplication_Metadata) ([]byte, error) {
	var sub waMsgApplication.MessageApplication_SubProtocolPayload
	sub.FutureProof = waCommon.FutureProofBehavior_PLACEHOLDER.Enum()
	switch m := message.(type) {
	case *waConsumerApplication.ConsumerApplication:
		payload, err := proto.Marshal(m)
		if err != nil {
			return nil, err
		}
		sub.SubProtocol = &waMsgApplication.MessageApplication_SubProtocolPayload_ConsumerMessage{
			ConsumerMessage: &waCommon.SubProtocol{Payload: payload, Version: proto.Int32(whatsmeow.FBConsumerMessageVersion)},
		}
	case *waArmadilloApplication.Armadillo:
		payload, err := proto.Marshal(m)
		if err != nil {
			return nil, err
		}
		sub.SubProtocol = &waMsgApplication.MessageApplication_SubProtocolPayload_Armadillo{
			Armadillo: &waCommon.SubProtocol{Payload: payload, Version: proto.Int32(whatsmeow.FBArmadilloMessageVersion)},
		}
	default:
		return nil, fmt.Errorf("type de message inattendu %T", message)
	}
	return proto.Marshal(&waMsgApplication.MessageApplication{
		Payload: &waMsgApplication.MessageApplication_Payload{
			Content: &waMsgApplication.MessageApplication_Payload_SubProtocol{SubProtocol: &sub},
		},
		Metadata: metadata,
	})
}
