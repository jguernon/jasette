package main

// De LinkedIn à Jasette : les ids, les personnes, les conversations et les
// messages (texte, InMail, images, vidéos, vocaux, fichiers, GIF, messages
// transférés, publications partagées, réponses, réactions).

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.mau.fi/mautrix-linkedin/pkg/linkedingo"
)

// ---- Les plafonds ----

const (
	maxTexte   = 8000     // caractères d'un message (comme le site)
	maxEnvoi   = 20 << 20 // octets d'une pièce jointe envoyée
	maxPage    = 20       // messages par page d'historique (comme le site)
	maxEnMem   = 300      // messages gardés en mémoire par conversation
	maxDlFile  = 200 << 20
	maxAvatar  = 4 << 20
	tailleAvat = 200 // largeur voulue d'une photo de profil
	largVign   = 1024
)

// ---- Les ids ----

const (
	prefConv = "urn:li:msg_conversation:(urn:li:fsd_profile:"
	prefMsg  = "urn:li:msg_message:(urn:li:fsd_profile:"
)

// court : la partie propre d'un URN de conversation ou de message de ma
// boîte (« urn:li:msg_conversation:(urn:li:fsd_profile:<moi>,<id>) » →
// « <id> ») ; tout autre URN reste entier.
func court(urn linkedingo.URN, me string) string {
	s := urn.String()
	if urn.IsEmpty() {
		return ""
	}
	for _, pre := range []string{prefConv, prefMsg} {
		if rest, ok := strings.CutPrefix(s, pre+me+","); ok && strings.HasSuffix(rest, ")") && me != "" {
			id := strings.TrimSuffix(rest, ")")
			if id != "" && !strings.ContainsAny(id, "(),") {
				return id
			}
		}
	}
	return s
}

// urnConv, urnMsg : l'URN entier d'un id court.
func urnConv(id, me string) linkedingo.URN {
	if strings.HasPrefix(id, "urn:") {
		return linkedingo.NewURN(id)
	}
	return linkedingo.NewURN(prefConv + me + "," + id + ")")
}

func urnMsg(id, me string) linkedingo.URN {
	if strings.HasPrefix(id, "urn:") {
		return linkedingo.NewURN(id)
	}
	return linkedingo.NewURN(prefMsg + me + "," + id + ")")
}

// convDe : la conversation d'un message (selon ce que LinkedIn a rempli).
func convDe(m linkedingo.Message) linkedingo.URN {
	for _, u := range []linkedingo.URN{m.Conversation.EntityURN, m.ConversationURN, m.BackendConversationURN} {
		if !u.IsEmpty() && strings.HasPrefix(u.String(), "urn:li:msg_conversation:") {
			return u
		}
	}
	return linkedingo.URN{}
}

// ---- Les personnes ----

type personne struct {
	ID    string `json:"id"` // l'id LinkedIn (dernier morceau de l'URN)
	URN   string `json:"urn"`
	Nom   string `json:"nom"`
	Photo string `json:"photo,omitempty"` // adresse d'une petite version
	Org   bool   `json:"org,omitempty"`
}

// adresseImage : l'adresse d'une version de l'image, la plus petite qui
// fait au moins « largeur » px (0 : la plus grande).
func adresseImage(vi *linkedingo.VectorImage, largeur int) (string, int, int) {
	if vi == nil || len(vi.Artifacts) == 0 {
		return "", 0, 0
	}
	var choix *linkedingo.VectorArtifact
	for i := range vi.Artifacts {
		a := &vi.Artifacts[i]
		if a.FileIdentifyingURLPathSegment == "" {
			continue
		}
		switch {
		case choix == nil:
			choix = a
		case largeur == 0:
			if a.Width > choix.Width {
				choix = a
			}
		case choix.Width < largeur && a.Width > choix.Width:
			choix = a
		case a.Width >= largeur && a.Width < choix.Width:
			choix = a
		}
	}
	if choix == nil {
		return "", 0, 0
	}
	u := choix.FileIdentifyingURLPathSegment
	if !strings.HasPrefix(u, "https://") {
		u = vi.RootURL + u
	}
	return u, choix.Width, choix.Height
}

func texteDe(t linkedingo.AttributedText) string { return strings.TrimSpace(t.Text) }

// personneDe : nom et photo d'un participant (membre, page, autre).
func personneDe(p linkedingo.MessagingParticipant) personne {
	r := personne{ID: p.EntityURN.ID(), URN: p.EntityURN.String()}
	pt := p.ParticipantType
	switch {
	case pt.Member != nil:
		r.Nom = strings.TrimSpace(texteDe(pt.Member.FirstName) + " " + texteDe(pt.Member.LastName))
		r.Photo, _, _ = adresseImage(pt.Member.ProfilePicture, tailleAvat)
	case pt.Organization != nil:
		r.Nom, r.Org = texteDe(pt.Organization.Name), true
		r.Photo, _, _ = adresseImage(pt.Organization.Logo, tailleAvat)
	case pt.Custom != nil:
		r.Nom = texteDe(pt.Custom.Name)
		r.Photo, _, _ = adresseImage(pt.Custom.Image, tailleAvat)
	case pt.Agent != nil:
		r.Nom = texteDe(pt.Agent.Name)
		r.Photo, _, _ = adresseImage(pt.Agent.ProfilePicture, tailleAvat)
	}
	return r
}

// ---- Les conversations ----

// conv : ce que le pont sait d'une conversation (gardé dans convs.json).
type conv struct {
	ID         string     `json:"id"`
	Titre      string     `json:"titre,omitempty"`
	Groupe     bool       `json:"groupe,omitempty"`
	Membres    []personne `json:"membres,omitempty"` // sans moi
	Time       int64      `json:"time"`
	Unread     bool       `json:"unread,omitempty"`
	Preview    string     `json:"preview,omitempty"`
	LastFromMe bool       `json:"last_from_me,omitempty"`
	LastID     string     `json:"last_id,omitempty"`
	Pending    bool       `json:"pending,omitempty"`  // demande de message chez LinkedIn
	Acceptee   bool       `json:"acceptee,omitempty"` // acceptée dans Jasette (la réponse l'acceptera chez LinkedIn)
	Ignoree    bool       `json:"ignoree,omitempty"`  // cachée dans Jasette
	Archivee   bool       `json:"archivee,omitempty"`
	Spam       bool       `json:"spam,omitempty"`
	Partie     bool       `json:"partie,omitempty"` // je n'en fais plus partie
	InMail     bool       `json:"inmail,omitempty"`
}

func (c *conv) request() bool { return c.Pending && !c.Acceptee }

func (c *conv) visible() bool { return !c.Ignoree && !c.Spam && !c.Partie }

func (c *conv) nom() string {
	if c.Titre != "" {
		return c.Titre
	}
	var noms []string
	for _, m := range c.Membres {
		if m.Nom != "" {
			noms = append(noms, m.Nom)
		}
	}
	if len(noms) == 0 {
		return "Conversation LinkedIn"
	}
	if c.Groupe && len(noms) > 1 {
		for i, n := range noms {
			noms[i], _, _ = strings.Cut(n, " ")
		}
	}
	return strings.Join(noms, ", ")
}

func (c *conv) out() Conversation {
	o := Conversation{
		ID: c.ID, Name: c.nom(), Preview: c.Preview, FromMe: c.LastFromMe, Time: c.Time,
		Unread: c.Unread, Group: c.Groupe, Archived: c.Archivee, Request: c.request(), Members: []string{},
	}
	for _, m := range c.Membres {
		o.Members = append(o.Members, m.Nom)
	}
	if o.Request && o.Preview != "" {
		o.Preview = "Demande de message · " + o.Preview
	} else if o.Request {
		o.Preview = "Demande de message"
	}
	return o
}

func (c *conv) membre(id string) *personne {
	for i := range c.Membres {
		if c.Membres[i].ID == id {
			return &c.Membres[i]
		}
	}
	return nil
}

// appliquerConv : une conversation de LinkedIn sur ce que le pont en sait.
// Rend le dernier message connu de LinkedIn (pour l'aperçu), s'il y en a un.
func appliquerConv(c *conv, lc linkedingo.Conversation, me string) *linkedingo.Message {
	if lc.Title != "" {
		c.Titre = lc.Title
	}
	c.Groupe = lc.GroupChat
	if len(lc.ConversationParticipants) > 0 {
		var membres []personne
		moi := false
		for _, part := range lc.ConversationParticipants {
			pe := personneDe(part)
			if pe.ID == me {
				moi = true
				continue
			}
			if pe.Nom == "" {
				if old := c.membre(pe.ID); old != nil {
					pe.Nom = old.Nom
				}
			}
			membres = append(membres, pe)
		}
		c.Membres = membres
		c.Partie = !moi
	}
	if !lc.LastActivityAt.IsZero() && lc.LastActivityAt.UnixMilli() > c.Time {
		c.Time = lc.LastActivityAt.UnixMilli()
	}
	if lc.Categories != nil {
		c.Spam = contient(lc.Categories, "SPAM")
		c.Archivee = contient(lc.Categories, "ARCHIVE")
		c.InMail = contient(lc.Categories, "INMAIL")
	}
	var dernier *linkedingo.Message
	for i := range lc.Messages.Elements {
		m := &lc.Messages.Elements[i]
		if dernier == nil || m.DeliveredAt.After(dernier.DeliveredAt.Time) {
			dernier = m
		}
	}
	if lc.State != "" {
		mien := false
		for _, m := range lc.Messages.Elements {
			mien = mien || m.Sender.EntityURN.ID() == me
		}
		c.Pending = lc.State == linkedingo.ConversationStatePending && !mien
	}
	// « read » n'est fiable que dans une conversation complète (un
	// événement partiel le laisse à faux).
	if lc.LastActivityAt.UnixMilli() > 0 || len(lc.ConversationParticipants) > 0 {
		c.Unread = !lc.Read
		if dernier != nil && dernier.Sender.EntityURN.ID() == me {
			c.Unread = false
		}
	}
	return dernier
}

func contient(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ---- Les messages ----

// mediaKey : la clé d'une pièce jointe (en JSON dans « key »).
type mediaKey struct {
	URL   string `json:"u"`           // l'original
	Vign  string `json:"t,omitempty"` // une version réduite (images)
	Conv  string `json:"c"`
	Msg   string `json:"m"`
	Time  int64  `json:"ts"`
	Rang  int    `json:"i"`
	Large bool   `json:"g,omitempty"` // l'original est plus grand que la vignette
}

func (k mediaKey) bytes() []byte {
	b, _ := json.Marshal(k)
	return b
}

func shorten(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// nomParticipant : « Prénom Nom » d'un participant, ou "".
func nomParticipant(p linkedingo.MessagingParticipant) string { return personneDe(p).Nom }

// adresseFil : l'adresse web d'une publication partagée (fsd_update).
func adresseFil(h *linkedingo.HostURNData) string {
	if h == nil || h.Type != "FEED_UPDATE" || h.HostURN.IsEmpty() {
		return ""
	}
	// urn:li:fsd_update:(urn:li:activity:123,MESSAGING_RESHARE,EMPTY,DEFAULT,false)
	id := h.HostURN.String()
	if i := strings.Index(id, "("); i >= 0 {
		id = id[i+1:]
	}
	if i := strings.Index(id, ","); i >= 0 {
		id = id[:i]
	}
	if !strings.HasPrefix(id, "urn:li:") {
		return ""
	}
	return "https://www.linkedin.com/feed/update/" + id + "/"
}

// pieces : les pièces jointes d'un message (images, vidéos, vocaux,
// fichiers, GIF). Rend aussi l'aperçu (« Photo »…).
func pieces(rc []linkedingo.RenderContent, conv, msg string, ts int64) []Attachment {
	var l []Attachment
	ajouter := func(a Attachment, k mediaKey) {
		if k.URL == "" {
			return
		}
		k.Conv, k.Msg, k.Time, k.Rang = conv, msg, ts, len(l)
		a.MediaID = fmt.Sprintf("%s#%d", msg, k.Rang)
		a.Key = k.bytes()
		l = append(l, a)
	}
	for _, r := range rc {
		switch {
		case r.VectorImage != nil:
			u, w, h := adresseImage(r.VectorImage, 0)
			v, vw, _ := adresseImage(r.VectorImage, largVign)
			k := mediaKey{URL: u}
			if v != "" && v != u && vw < w {
				k.Vign, k.Large = v, true
			}
			ajouter(Attachment{Mime: "image/jpeg", Name: "image.jpg", W: int64(w), H: int64(h)}, k)
		case r.Video != nil:
			var meilleur *linkedingo.ProgressiveDownloadMetadata
			for i := range r.Video.ProgressiveStreams {
				s := &r.Video.ProgressiveStreams[i]
				if len(s.StreamingLocations) == 0 {
					continue
				}
				if meilleur == nil || s.BitRate > meilleur.BitRate {
					meilleur = s
				}
			}
			if meilleur == nil {
				continue
			}
			mime := firstOf(meilleur.MediaType, "video/mp4")
			ajouter(Attachment{Mime: mime, Name: "video.mp4", Size: int64(meilleur.Size), W: int64(meilleur.Width), H: int64(meilleur.Height)},
				mediaKey{URL: meilleur.StreamingLocations[0].URL})
		case r.Audio != nil:
			ajouter(Attachment{Mime: "audio/mp4", Name: "message vocal.m4a"}, mediaKey{URL: r.Audio.URL})
		case r.File != nil:
			ajouter(Attachment{Mime: firstOf(r.File.MediaType, "application/octet-stream"), Name: firstOf(r.File.Name, "fichier"), Size: int64(r.File.ByteSize)},
				mediaKey{URL: r.File.URL})
		case r.ExternalMedia != nil:
			m := r.ExternalMedia.Media
			k := mediaKey{URL: m.URL}
			if pv := r.ExternalMedia.PreviewMedia.URL; pv != "" && pv != m.URL {
				k.Vign = pv
			}
			ajouter(Attachment{Mime: "image/gif", Name: firstOf(r.ExternalMedia.Title, "GIF") + ".gif", W: int64(m.OriginalWidth), H: int64(m.OriginalHeight)}, k)
		}
	}
	return l
}

func resume(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	return firstOf(atts[0].Name, atts[0].Mime, "pièce jointe")
}

// apercuPiece : le texte de la liste pour un message sans texte.
func apercuPiece(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	switch m := atts[0].Mime; {
	case strings.HasPrefix(m, "image/"):
		return "📷 Photo"
	case strings.HasPrefix(m, "video/"):
		return "🎬 Vidéo"
	case strings.HasPrefix(m, "audio/"):
		return "🎤 Message vocal"
	}
	return "📎 Fichier"
}

// convertir : un message de LinkedIn en message de Jasette. noms : le nom
// connu d'une personne (par son id), quand le message ne le porte pas.
func convertir(m linkedingo.Message, convID, me string, noms func(string) string) Message {
	id := court(m.EntityURN, me)
	auteur := m.Sender.EntityURN.ID()
	r := Message{
		ID:        id,
		Conv:      convID,
		FromMe:    auteur != "" && auteur == me,
		Time:      m.DeliveredAt.UnixMilli(),
		Kind:      "linkedin",
		senderURN: m.Sender.EntityURN.String(),
	}
	if r.FromMe {
		r.Status = "OUTGOING_DELIVERED"
	} else {
		r.Status = "INCOMING_COMPLETE"
		r.Sender = firstOf(nomParticipant(m.Sender), noms(auteur))
		r.SenderID = auteur
	}
	switch m.MessageBodyRenderFormat {
	case linkedingo.MessageBodyRenderFormatRecalled:
		return retire(r)
	case linkedingo.MessageBodyRenderFormatSystem:
		r.Kind = "admin"
		r.Text = texteDe(m.Body)
		return r
	}
	var lignes []string
	if s := strings.TrimSpace(m.Subject); s != "" {
		// InMail : l'objet en tête.
		lignes = append(lignes, s, "")
	}
	corps := texteDe(m.Body)
	r.corps = corps
	if corps != "" {
		lignes = append(lignes, corps)
	}
	for _, rc := range m.RenderContent {
		switch {
		case rc.ForwardedMessage != nil:
			f := rc.ForwardedMessage
			bloc := "↗️ Message transféré"
			if qui := nomParticipant(f.OriginalSender); qui != "" {
				bloc += "\n" + qui + " :"
			}
			if t := texteDe(f.ForwardedBody); t != "" {
				bloc += "\n" + t
			}
			if t := texteDe(f.FooterText); t != "" {
				bloc += "\n" + t
			}
			lignes = append(lignes, bloc)
		case rc.HostURNData != nil:
			if u := adresseFil(rc.HostURNData); u != "" && !strings.Contains(corps, u) {
				lignes = append(lignes, u)
			}
		case rc.RepliedMessageContent != nil:
			o := rc.RepliedMessageContent.OriginalMessage
			r.ReplyTo = court(o.EntityURN, me)
			cite := texteDe(o.Body)
			if cite == "" {
				cite = apercuPiece(pieces(o.RenderContent, convID, r.ReplyTo, 0))
			}
			r.ReplyText = shorten(cite, 120)
		}
	}
	r.Attachments = pieces(m.RenderContent, convID, id, r.Time)
	r.Media = resume(r.Attachments)
	r.Text = strings.TrimSpace(strings.Join(lignes, "\n"))
	if r.ReplyText != "" {
		r.Text = "↪ « " + shorten(r.ReplyText, 80) + " »\n" + r.Text
	}
	if r.Text == "" && len(r.Attachments) == 0 {
		r.Text = "(pièce jointe non prise en charge)"
	}
	r.Reactions = reactionsDe(m.ReactionSummaries)
	return r
}

// reactionsDe : les réactions d'un message, de ses résumés (LinkedIn ne dit
// pas qui, sauf moi : demander chaque personne serait une rafale). Les
// autres sont anonymes, une par personne ; le direct les nomme ensuite.
func reactionsDe(rs []linkedingo.ReactionSummary) []Reaction {
	var l []Reaction
	for _, s := range rs {
		if s.Emoji == "" || s.Count <= 0 {
			continue
		}
		autres := s.Count
		if s.ViewerReacted {
			l = append(l, Reaction{Emoji: s.Emoji, FromMe: true})
			autres--
		}
		for i := 0; i < autres && i < 50; i++ {
			l = append(l, Reaction{Emoji: s.Emoji})
		}
	}
	return l
}

// retire : le message retiré par son auteur.
func retire(m Message) Message {
	m.Text, m.System = "🚫 Message retiré", "deleted"
	m.Kind = "admin"
	m.Attachments = nil
	m.Reactions = nil
	m.Media = ""
	m.ReplyTo, m.ReplyText = "", ""
	return m
}

// apercu : le texte de la liste pour un message.
func apercu(m Message) string {
	if m.Kind == "admin" {
		return shorten(m.Text, 120)
	}
	t := strings.TrimSpace(m.Text)
	if m.ReplyText != "" {
		if _, reste, ok := strings.Cut(t, "\n"); ok {
			t = strings.TrimSpace(reste)
		}
	}
	if t == "" {
		t = apercuPiece(m.Attachments)
	}
	return shorten(t, 120)
}
