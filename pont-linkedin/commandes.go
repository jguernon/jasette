package main

// Les commandes de l'interface : historique, envoi, réactions, lu,
// demandes de message, recherche. Chacune est une action de l'utilisateur ;
// le pont n'en fait aucune de lui-même.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.mau.fi/util/jsontime"

	"go.mau.fi/mautrix-linkedin/pkg/linkedingo"
)

var errLectureSeule = errors.New("LinkedIn : mode lecture seule (JASETTE_LINKEDIN_LECTURE_SEULE), rien n'est envoyé")

func (p *Pont) knownConv(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.convs[id] != nil
}

// ---- L'historique ----

// messages : une page d'historique (20 messages au plus, jamais marquée
// comme lue : seul « read » le fait). Une seule page par demande. Hors
// connexion : ce qu'on a en mémoire.
func (p *Pont) messages(cmd Cmd) {
	id := cmd.Conv
	count := int(cmd.Count)
	if count <= 0 || count > maxPage {
		count = maxPage
	}
	older := cmd.Before != "" || cmd.BeforeT > 0
	if !p.knownConv(id) {
		emit("messages", map[string]any{"conv": id, "items": []Message{}, "older": older, "more": false})
		return
	}
	cli, me, err := p.client()
	var page *pageMessage
	if err == nil {
		ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
		page, err = p.pageHistorique(ctx, cli, me, cmd, count)
		annule()
		if err != nil {
			log.Warn().Str("err", erreurCourte(err)).Msg("historique")
			if estErrAuth(err) {
				go p.sessionPerdue(err)
			}
		}
	}
	if err != nil || page == nil {
		items, more := p.cachedPage(id, cmd.Before, count)
		emit("messages", map[string]any{"conv": id, "items": items, "older": older, "more": more})
		return
	}
	items := p.appliquerPage(id, me, page, older)
	more := page.Metadata.PrevCursor != "" && len(page.Elements) > 0
	emit("messages", map[string]any{"conv": id, "items": items, "older": older, "more": more})
	go p.auteursDe(id, items)
	if !older && len(items) > 0 {
		p.emitConv(id)
	}
}

// pageHistorique : la page la plus récente, ou celle d'avant (curseur de
// la page précédente, sinon l'heure du message le plus vieux).
func (p *Pont) pageHistorique(ctx context.Context, cli liAPI, me string, cmd Cmd, count int) (*pageMessage, error) {
	urn := urnConv(cmd.Conv, me)
	if cmd.Before == "" && cmd.BeforeT <= 0 {
		return cli.GetMessagesBefore(ctx, urn, time.Now().Add(time.Minute), count)
	}
	p.mu.Lock()
	curseur := p.prev[cmd.Conv]
	avant := cmd.BeforeT
	if m := p.trouver(cmd.Conv, cmd.Before); m != nil && (avant <= 0 || m.Time < avant) {
		avant = m.Time
	}
	p.mu.Unlock()
	if curseur != "" {
		return cli.GetMessagesWithPrevCursor(ctx, urn, curseur, count)
	}
	if avant <= 0 {
		return &pageMessage{}, nil
	}
	return cli.GetMessagesBefore(ctx, urn, time.UnixMilli(avant-1), count)
}

// appliquerPage : les messages d'une page, du plus vieux au plus récent,
// gardés en mémoire. Les réactions sont celles des résumés (anonymes, sauf
// les miennes) : demander qui a réagi serait une requête par émoji.
func (p *Pont) appliquerPage(id, me string, page *pageMessage, older bool) []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.convs[id]
	if c == nil {
		return []Message{}
	}
	p.prev[id] = page.Metadata.PrevCursor
	l := append([]linkedingo.Message(nil), page.Elements...)
	sort.SliceStable(l, func(i, j int) bool { return l[i].DeliveredAt.Before(l[j].DeliveredAt.Time) })
	items := []Message{}
	for _, lm := range l {
		// Les noms des auteurs : ce que la page en dit.
		if pe := personneDe(lm.Sender); pe.ID != "" && pe.ID != me && pe.Nom != "" {
			if old := c.membre(pe.ID); old != nil {
				old.Nom = pe.Nom
				if pe.Photo != "" {
					old.Photo = pe.Photo
				}
			}
		}
		m := convertir(lm, id, me, p.nomDe(c))
		if m.ID == "" {
			continue
		}
		p.remember(m)
		if mm := p.trouver(id, m.ID); mm != nil {
			m = *mm
		}
		items = append(items, m)
	}
	if !older && len(items) > 0 {
		p.noteLast(c, items[len(items)-1])
	}
	return items
}

// cachedPage : une page tirée de la mémoire (hors connexion).
func (p *Pont) cachedPage(id, before string, count int) ([]Message, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.msgs[id]
	end := len(l)
	if before != "" {
		for i, m := range l {
			if m.ID == before {
				end = i
				break
			}
		}
	}
	start := max(0, end-count)
	return append([]Message{}, l[start:end]...), start > 0
}

// ---- L'envoi ----

// expliquer : les refus courants de LinkedIn, en clair.
func expliquer(err error) error {
	var re *linkedingo.ResponseError
	if errors.As(err, &re) {
		switch re.StatusCode {
		case http.StatusTooManyRequests:
			return errors.New("LinkedIn limite les envois pour l'instant : réessayez plus tard")
		case http.StatusUnauthorized:
			return errors.New("LinkedIn a fermé la session : reconnectez-vous")
		case http.StatusForbidden:
			return errors.New("LinkedIn refuse (conversation fermée, ou réservée à InMail ou à Sales Navigator)")
		case http.StatusRequestEntityTooLarge:
			return errors.New("fichier trop gros pour LinkedIn")
		}
		return fmt.Errorf("LinkedIn : %s", re.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("LinkedIn ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("LinkedIn : %s", erreurCourte(err))
}

func (p *Pont) send(cmd Cmd) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return
	}
	p.sendCommon(cmd, text, "", "")
}

func (p *Pont) sendMedia(cmd Cmd) {
	p.sendCommon(cmd, strings.TrimSpace(cmd.Text), cmd.Path, cmd.Mime)
}

// lireFichier : le fichier à envoyer (20 Mo au plus) et son type.
func lireFichier(path, mimeType string) ([]byte, string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", errors.New("fichier introuvable")
	}
	if st.Size() > maxEnvoi {
		return nil, "", errors.New("fichier trop gros pour LinkedIn (20 Mo au plus)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	if i := strings.IndexByte(mimeType, ';'); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	return data, mimeType, nil
}

// rendu : la pièce jointe téléversée, telle que le site l'envoie.
func rendu(urn linkedingo.URN, nom, mimeType string, taille int) linkedingo.SendRenderContent {
	if strings.HasPrefix(mimeType, "video/") {
		return linkedingo.SendRenderContent{Video: &linkedingo.SendVideo{
			Media:      urn,
			TrackingID: urn,
			Thumbnail:  linkedingo.SendThumbnail{Artifacts: []linkedingo.SendArtifacts{{}}},
			ProgressiveStreams: []linkedingo.SendProgressiveStreams{{
				MediaType: mimeType, Size: taille,
				StreamingLocations: []linkedingo.SendURL{{URL: "blob:https://www.linkedin.com/" + uuid.NewString()}},
			}},
		}}
	}
	return linkedingo.SendRenderContent{File: &linkedingo.SendFile{AssetURN: urn, Name: nom, MediaType: mimeType, ByteSize: taille}}
}

func typeTeleversement(mimeType string) linkedingo.MediaUploadType {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return linkedingo.MediaUploadTypePhotoAttachment
	case strings.HasPrefix(mimeType, "video/"):
		return linkedingo.MediaUploadTypeVideoAttachment
	}
	return linkedingo.MediaUploadTypeFileAttachment
}

func (p *Pont) sendCommon(cmd Cmd, text, path, mimeType string) {
	tmp := firstOf(cmd.TmpID, fmt.Sprintf("tmp_%d", time.Now().UnixNano()))
	fail := func(err error) {
		log.Warn().Str("err", err.Error()).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	if p.lectureSeule {
		fail(errLectureSeule)
		return
	}
	if !p.knownConv(cmd.Conv) {
		fail(errors.New("conversation LinkedIn inconnue"))
		return
	}
	if utf8.RuneCountInString(text) > maxTexte {
		fail(errors.New("LinkedIn limite un message à 8 000 caractères"))
		return
	}
	cli, me, err := p.client()
	if err != nil {
		fail(err)
		return
	}
	var data []byte
	if path != "" {
		if data, mimeType, err = lireFichier(path, mimeType); err != nil {
			fail(err)
			return
		}
	}
	ctx, annule := context.WithTimeout(p.ctx, 5*time.Minute)
	defer annule()

	p.mu.Lock()
	c := p.convs[cmd.Conv]
	etat := linkedingo.ConversationState("")
	if c != nil && c.Pending {
		// Répondre à une demande, c'est l'accepter (comme le site).
		etat = linkedingo.ConversationStateAccepted
	}
	var rc []linkedingo.SendRenderContent
	if cmd.ReplyTo != "" {
		if o := p.trouver(cmd.Conv, cmd.ReplyTo); o != nil && o.Kind != "admin" {
			auteur := linkedingo.NewURN(firstOf(o.senderURN, "urn:li:msg_messagingParticipant:urn:li:fsd_profile:"+firstOf(o.SenderID, me)))
			if o.FromMe {
				auteur = linkedingo.NewURN("urn:li:msg_messagingParticipant:urn:li:fsd_profile:" + me)
			}
			rc = append(rc, linkedingo.SendRenderContent{RepliedMessageContent: &linkedingo.SendRepliedMessage{
				OriginalSenderURN:  auteur,
				OriginalSendAt:     jsontime.UM(time.UnixMilli(o.Time)),
				OriginalMessageURN: urnMsg(o.ID, me),
				MessageBody:        linkedingo.AttributedText{Text: o.corps},
			}})
		}
	}
	p.nettoyerEnvois(cmd.Conv)
	p.envois[cmd.Conv] = append(p.envois[cmd.Conv], envoi{tmp: tmp, texte: text, quand: time.Now()})
	p.mu.Unlock()

	nom := ""
	if data != nil {
		nom = filepath.Base(path)
		if strings.HasPrefix(nom, ".") || nom == "" {
			nom = "fichier" + extension(mimeType, data)
		}
		urn, err := cli.UploadMedia(ctx, typeTeleversement(mimeType), nom, mimeType, len(data), bytes.NewReader(data))
		if err != nil {
			fail(expliquer(err))
			return
		}
		rc = append(rc, rendu(urn, nom, mimeType, len(data)))
	}
	resp, err := cli.SendMessage(ctx, urnConv(cmd.Conv, me), linkedingo.SendMessageBody{Text: text}, rc, uuid.NewString(), etat)
	if err != nil {
		fail(expliquer(err))
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	p.mu.Lock()
	c = p.convs[cmd.Conv]
	var m Message
	ok := false
	if c != nil {
		if etat != "" {
			c.Pending, c.Acceptee = false, false
		}
		lm := resp.Data
		if lm.EntityURN.IsEmpty() {
			// Pas d'id dans la réponse : le direct apportera le message.
			p.mu.Unlock()
			p.emitConv(cmd.Conv)
			return
		}
		if lm.Sender.EntityURN.IsEmpty() {
			lm.Sender.EntityURN = linkedingo.NewURN("urn:li:msg_messagingParticipant:urn:li:fsd_profile:" + me)
		}
		if lm.DeliveredAt.IsZero() {
			lm.DeliveredAt = jsontime.UM(time.Now())
		}
		if lm.Body.Text == "" {
			lm.Body.Text = text
		}
		m = convertir(lm, cmd.Conv, me, p.nomDe(c))
		m.TmpID = tmp
		if data != nil {
			w, h := dimensions(data)
			for i := range m.Attachments {
				a := &m.Attachments[i]
				a.Name = firstOf(nom, a.Name)
				if a.W == 0 && w > 0 {
					a.W, a.H = int64(w), int64(h)
				}
			}
		}
		// Le direct ne la rattachera plus : déjà là.
		l := p.envois[cmd.Conv]
		for i, e := range l {
			if e.tmp == tmp {
				p.envois[cmd.Conv] = append(l[:i], l[i+1:]...)
				break
			}
		}
		p.remember(m)
		if mm := p.trouver(cmd.Conv, m.ID); mm != nil {
			m = *mm
		}
		p.noteLast(c, m)
		c.Unread = false
		p.sale = true
		ok = true
	}
	p.mu.Unlock()
	if ok {
		emit("message", map[string]any{"item": m})
	}
	p.emitConv(cmd.Conv)
}

// ---- Réactions ----

// react : comme les autres services de Jasette, une réaction à la fois : la
// nouvelle remplace la mienne ; vide, la mienne est retirée.
func (p *Pont) react(cmd Cmd) {
	fail := func(err error) {
		emit("error", map[string]any{"where": "react", "text": err.Error(), "conv": cmd.Conv, "id": cmd.ID})
	}
	if p.lectureSeule {
		fail(errLectureSeule)
		return
	}
	cli, me, err := p.client()
	if err != nil {
		fail(err)
		return
	}
	emoji := strings.TrimSpace(cmd.Emoji)
	p.mu.Lock()
	m := p.trouver(cmd.Conv, cmd.ID)
	var miennes []string
	deja := false
	if m != nil {
		for _, r := range m.Reactions {
			if r.FromMe {
				if r.Emoji == emoji {
					deja = true
				} else {
					miennes = append(miennes, r.Emoji)
				}
			}
		}
	}
	p.mu.Unlock()
	if m == nil || cmd.ID == "" {
		fail(errors.New("message inconnu"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	cible := urnMsg(cmd.ID, me)
	for _, e := range miennes {
		if err := cli.RemoveReaction(ctx, cible, e); err != nil {
			fail(expliquer(err))
			return
		}
	}
	if emoji != "" && !deja {
		if err := cli.SendReaction(ctx, cible, emoji); err != nil {
			fail(expliquer(err))
			return
		}
	}
	// Tout de suite à l'écran (le direct confirmera).
	p.mu.Lock()
	m = p.trouver(cmd.Conv, cmd.ID)
	var out Message
	if m != nil {
		var l []Reaction
		for _, r := range m.Reactions {
			if !r.FromMe {
				l = append(l, r)
			}
		}
		if emoji != "" {
			l = append(l, Reaction{Emoji: emoji, FromMe: true})
		}
		m.Reactions = l
		out = *m
	}
	p.mu.Unlock()
	if m != nil {
		emit("message", map[string]any{"item": out})
	}
}

// ---- Lu ----

// markRead : la conversation lue chez LinkedIn. La seule commande qui marque
// lu, sur une action de l'utilisateur, et seulement si elle est non lue.
func (p *Pont) markRead(cmd Cmd) {
	p.mu.Lock()
	c := p.convs[cmd.Conv]
	if c == nil || !c.Unread {
		p.mu.Unlock()
		return
	}
	if p.lectureSeule {
		p.mu.Unlock()
		log.Info().Msg("lu : ignoré (mode lecture seule)")
		return
	}
	c.Unread = false
	p.sale = true
	p.mu.Unlock()
	p.emitConv(cmd.Conv)
	cli, me, err := p.client()
	if err != nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if _, err := cli.MarkConversationRead(ctx, urnConv(cmd.Conv, me)); err != nil {
		log.Warn().Str("err", erreurCourte(err)).Msg("lu")
	}
}

// ---- Demandes de message ----

// acceptRequest : « Accepter ». LinkedIn n'a pas de geste à part : la
// demande s'accepte chez lui à la première réponse. Ici, elle passe dans la
// boîte de réception.
func (p *Pont) acceptRequest(cmd Cmd) {
	p.mu.Lock()
	c := p.convs[cmd.Conv]
	if c != nil {
		c.Acceptee = true
		p.sale = true
	}
	p.mu.Unlock()
	if c == nil {
		emitErr("accept", errors.New("conversation LinkedIn inconnue"))
		return
	}
	p.emitConv(cmd.Conv)
}

// ignoreRequest : « Ignorer » : cachée dans Jasette (rien n'est dit à
// LinkedIn ni à l'expéditeur) ; un nouveau message la fait revenir.
func (p *Pont) ignoreRequest(cmd Cmd) {
	p.mu.Lock()
	c := p.convs[cmd.Conv]
	if c != nil {
		c.Ignoree = true
		p.sale = true
	}
	p.mu.Unlock()
	if c == nil {
		emitErr("ignore", errors.New("conversation LinkedIn inconnue"))
		return
	}
	emit("deleted", map[string]any{"conv": cmd.Conv})
}

// ---- Recherche ----

// search : parmi les personnes des conversations à deux connues (rien n'est
// demandé à LinkedIn). Les résultats ouvrent la conversation existante.
func (p *Pont) search(q string) {
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	items := []item{}
	defer func() { emit("search", map[string]any{"query": q, "items": items}) }()
	lq := plier(strings.TrimSpace(q))
	if lq == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.convs {
		if !c.visible() {
			continue
		}
		nom := c.nom()
		if strings.Contains(plier(nom), lq) {
			items = append(items, item{ID: c.ID, Name: nom})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	if len(items) > 30 {
		items = items[:30]
	}
}

// newChat : un résultat de « search » (une conversation connue) s'ouvre.
// Jasette ne commence pas de conversation LinkedIn (linkedingo enverrait
// un premier message « New chat »).
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimPrefix(strings.TrimSpace(firstOf(cmd.Number, cmd.ID, cmd.Conv, cmd.Text)), "l:")
	p.mu.Lock()
	c := p.convs[q]
	var item Conversation
	if c != nil {
		c.Ignoree = false
		item = c.out()
	}
	p.mu.Unlock()
	if c == nil {
		emitErr("new", errors.New("LinkedIn : commencez la conversation dans LinkedIn, elle apparaîtra ici"))
		return
	}
	emit("conversation", map[string]any{"item": item, "open": true})
}

// searchMessages : les messages en mémoire dont le texte contient « text ».
func (p *Pont) searchMessages(text string, limit int) {
	items := []map[string]any{}
	defer func() {
		emit("search_messages", map[string]any{"query": text, "items": items})
	}()
	q := plier(strings.TrimSpace(text))
	if q == "" {
		return
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	p.mu.Lock()
	var hits []Message
	for _, l := range p.msgs {
		for _, m := range l {
			if m.Kind != "admin" && strings.Contains(plier(m.Text), q) {
				hits = append(hits, m)
			}
		}
	}
	p.mu.Unlock()
	sort.Slice(hits, func(i, j int) bool { return hits[i].Time > hits[j].Time })
	for i, m := range hits {
		if i >= limit {
			break
		}
		items = append(items, map[string]any{
			"conv": m.Conv, "id": m.ID, "text": m.Text, "sender": m.Sender, "time": m.Time,
			"from_me": m.FromMe, "fromMe": m.FromMe,
		})
	}
}

// plier : minuscules, sans accents (comme `plier` de l'interface).
func plier(s string) string {
	r := strings.NewReplacer("à", "a", "â", "a", "ä", "a", "á", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ç", "c", "œ", "oe", "æ", "ae")
	return r.Replace(strings.ToLower(s))
}

// dimensions : largeur et hauteur d'une image (0 si inconnues).
func dimensions(data []byte) (int, int) {
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		return cfg.Width, cfg.Height
	}
	return 0, 0
}
