package main

// La conversion des messages de Discord vers le format de Jasette. Rien ici
// ne parle au réseau : ces fonctions sont testées seules (convert_test.go).

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// L'époque des identifiants de Discord (« snowflakes ») : 2015-01-01.
const discordEpoch = 1420070400000

// snowflakeTime : le moment (ms) où l'id a été créé ; 0 s'il est illisible.
func snowflakeTime(id string) int64 {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n == 0 {
		return 0
	}
	return int64(n>>22) + discordEpoch
}

// compareIDs compare deux snowflakes (-1, 0, 1) ; vide = le plus petit.
func compareIDs(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// generateNonce : un snowflake de l'heure actuelle, comme le client web (et
// mautrix-discord) le met dans chaque envoi.
func generateNonce() string {
	return strconv.FormatInt((time.Now().UnixMilli()-discordEpoch)<<22, 10)
}

// resolver : les noms connus du pont (personnes, salons).
type resolver interface {
	userName(id string) string
	channelName(id string) string
	meID() string
}

var (
	reUser    = regexp.MustCompile(`<@!?(\d+)>`)
	reRole    = regexp.MustCompile(`<@&(\d+)>`)
	reChannel = regexp.MustCompile(`<#(\d+)>`)
	reEmoji   = regexp.MustCompile(`<a?:([A-Za-z0-9_~\-]+):\d+>`)
	reTime    = regexp.MustCompile(`<t:(-?\d+)(?::([tTdDfFR]))?>`)
	reCommand = regexp.MustCompile(`</([^:>]+):\d+>`)
)

// renderContent : le texte d'un message, mentions et emojis du serveur
// remplacés par leur nom. La mise en forme (**gras**, `code`…) reste telle
// quelle, comme dans les notifications de Discord.
func renderContent(s string, r resolver) string {
	if !strings.Contains(s, "<") {
		return s
	}
	s = reUser.ReplaceAllStringFunc(s, func(m string) string {
		id := reUser.FindStringSubmatch(m)[1]
		if n := r.userName(id); n != "" {
			return "@" + n
		}
		return "@quelqu'un"
	})
	s = reRole.ReplaceAllString(s, "@rôle")
	s = reChannel.ReplaceAllStringFunc(s, func(m string) string {
		id := reChannel.FindStringSubmatch(m)[1]
		if n := r.channelName(id); n != "" {
			return "#" + n
		}
		return "#salon"
	})
	s = reEmoji.ReplaceAllString(s, ":$1:")
	s = reCommand.ReplaceAllString(s, "/$1")
	s = reTime.ReplaceAllStringFunc(s, func(m string) string {
		sub := reTime.FindStringSubmatch(m)
		n, err := strconv.ParseInt(sub[1], 10, 64)
		if err != nil {
			return m
		}
		t := time.Unix(n, 0).Local()
		switch sub[2] {
		case "t":
			return t.Format("15:04")
		case "T":
			return t.Format("15:04:05")
		case "d", "D":
			return t.Format("2006-01-02")
		default:
			return t.Format("2006-01-02 15:04")
		}
	})
	return s
}

// emojiText : l'emoji d'une réaction ; « :nom: » pour un emoji d'un serveur.
func emojiText(e *discordgo.Emoji) string {
	if e == nil {
		return ""
	}
	if e.ID != "" {
		return ":" + e.Name + ":"
	}
	return e.Name
}

// emojiAPI : l'emoji tel que l'API l'attend dans l'adresse d'une réaction.
func emojiAPI(e *discordgo.Emoji) string {
	if e.ID != "" {
		return e.Name + ":" + e.ID
	}
	return e.Name
}

// mediaKey : de quoi retrouver une pièce jointe (Attachment.Key).
type mediaKey struct {
	URL     string `json:"u"`
	Channel string `json:"c,omitempty"`
	Message string `json:"m,omitempty"`
	ID      string `json:"a,omitempty"`
}

func attachmentsOf(m *discordgo.Message) []Attachment {
	var out []Attachment
	for _, a := range m.Attachments {
		if a == nil || a.URL == "" {
			continue
		}
		mime := a.ContentType
		if i := strings.IndexByte(mime, ';'); i > 0 {
			mime = strings.TrimSpace(mime[:i])
		}
		if mime == "" {
			mime = mimeFromName(a.Filename)
		}
		name := a.Filename
		if m.Flags&discordgo.MessageFlagsIsVoiceMessage != 0 && strings.HasPrefix(mime, "audio/") {
			name = "message vocal"
		}
		key, _ := json.Marshal(mediaKey{URL: a.URL, Channel: m.ChannelID, Message: m.ID, ID: a.ID})
		out = append(out, Attachment{
			MediaID: "dc:" + a.ID,
			Key:     key,
			Mime:    mime,
			Name:    name,
			Size:    int64(a.Size),
			W:       int64(a.Width),
			H:       int64(a.Height),
		})
	}
	return out
}

func mimeFromName(name string) string {
	ext := strings.ToLower(name[strings.LastIndexByte(name, '.')+1:])
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "mp4":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	case "ogg":
		return "audio/ogg"
	case "mp3":
		return "audio/mpeg"
	case "pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}

func mediaSummary(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	return firstOf(atts[0].Name, atts[0].Mime, "pièce jointe")
}

// previewOf : l'aperçu d'un message dans la liste.
func previewOf(text string, atts []Attachment) string {
	if len(atts) == 0 {
		return text
	}
	switch mime := atts[0].Mime; {
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

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n-1]) + "…"
	}
	return s
}

func quoteLine(s string) string { return "↪ " + s + "\n" }

// linkPreviewOf : l'aperçu du premier lien, tel que Discord l'a fabriqué
// (embed « link », « article », « video »…). L'image est une adresse https
// que l'interface ira chercher elle-même (mêmes garde-fous que ses aperçus).
func linkPreviewOf(m *discordgo.Message) *LinkPreview {
	for _, e := range m.Embeds {
		if e == nil || e.URL == "" {
			continue
		}
		switch e.Type {
		case discordgo.EmbedTypeLink, discordgo.EmbedTypeArticle, discordgo.EmbedTypeVideo, discordgo.EmbedTypeImage, discordgo.EmbedTypeGifv:
		default:
			continue
		}
		title, desc := strings.TrimSpace(e.Title), strings.TrimSpace(e.Description)
		if title == "" && desc == "" {
			continue
		}
		lp := &LinkPreview{URL: e.URL, Title: title, Description: shorten(desc, 300)}
		if e.Provider != nil {
			lp.Site = e.Provider.Name
		}
		if lp.Site == "" {
			if u, err := url.Parse(e.URL); err == nil {
				lp.Site = u.Hostname()
			}
		}
		img := ""
		if e.Thumbnail != nil {
			img = e.Thumbnail.URL
		} else if e.Image != nil {
			img = e.Image.URL
		}
		if strings.HasPrefix(img, "https://") {
			lp.ImageURL = img
		}
		return lp
	}
	return nil
}

// embedText : le texte d'un message sans contenu fait seulement d'un embed
// riche (le message d'une application, par exemple).
func embedText(m *discordgo.Message) string {
	for _, e := range m.Embeds {
		if e == nil {
			continue
		}
		parts := []string{}
		if e.Title != "" {
			parts = append(parts, e.Title)
		}
		if e.Description != "" {
			parts = append(parts, e.Description)
		}
		if len(parts) == 0 && e.URL != "" {
			parts = append(parts, e.URL)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

// systemText : le texte d'un message de service (ajout au groupe, appel…) ;
// vide pour un message ordinaire.
func systemText(m *discordgo.Message, who string, r resolver) string {
	target := func() string {
		if len(m.Mentions) > 0 && m.Mentions[0] != nil {
			if n := r.userName(m.Mentions[0].ID); n != "" {
				return n
			}
			return m.Mentions[0].DisplayName()
		}
		return "quelqu'un"
	}
	switch m.Type {
	case discordgo.MessageTypeRecipientAdd:
		return who + " a ajouté " + target()
	case discordgo.MessageTypeRecipientRemove:
		t := target()
		if m.Author != nil && len(m.Mentions) > 0 && m.Mentions[0] != nil && m.Mentions[0].ID == m.Author.ID {
			return who + " a quitté le groupe"
		}
		return who + " a retiré " + t
	case discordgo.MessageTypeCall:
		return "📞 " + who + " a lancé un appel"
	case discordgo.MessageTypeChannelNameChange:
		return who + " a renommé le groupe : " + m.Content
	case discordgo.MessageTypeChannelIconChange:
		return who + " a changé l'image du groupe"
	case discordgo.MessageTypeChannelPinnedMessage:
		return who + " a épinglé un message"
	}
	return ""
}

// reactionsOf : les réactions telles que Discord les donne avec un message
// (un nombre par emoji, et « me ») : une entrée par personne, celle des
// autres sans nom (Discord ne dit pas qui, seulement combien).
func reactionsOf(m *discordgo.Message) []Reaction {
	var out []Reaction
	for _, r := range m.Reactions {
		if r == nil || r.Emoji == nil {
			continue
		}
		e := emojiText(r.Emoji)
		n := r.Count
		if r.Me {
			out = append(out, Reaction{Emoji: e, FromMe: true})
			n--
		}
		for i := 0; i < n && i < 50; i++ {
			out = append(out, Reaction{Emoji: e})
		}
	}
	return out
}

// convertMessage : un message de Discord pour l'interface. « reply » est le
// message cité, s'il est connu (sinon ReferencedMessage, s'il est là).
func convertMessage(m *discordgo.Message, r resolver) Message {
	me := r.meID()
	fromMe := m.Author != nil && m.Author.ID == me
	who := ""
	if m.Author != nil {
		who = firstOf(r.userName(m.Author.ID), m.Author.DisplayName())
	}
	ts := m.Timestamp.UnixMilli()
	if m.Timestamp.IsZero() {
		ts = snowflakeTime(m.ID)
	}
	out := Message{
		ID:     m.ID,
		Conv:   m.ChannelID,
		FromMe: fromMe,
		Time:   ts,
		Kind:   "discord",
		Status: "INCOMING_COMPLETE",
	}
	if fromMe {
		out.Status = "OUTGOING_COMPLETE"
	} else {
		out.Sender = who
	}
	if sys := systemText(m, firstOf(who, "Quelqu'un"), r); sys != "" {
		out.Kind = "admin"
		out.Text = sys
		return out
	}
	body := renderContent(m.Content, r)
	atts := attachmentsOf(m)
	if body == "" && len(atts) == 0 {
		body = embedText(m)
	}
	for _, s := range m.StickerItems {
		if s != nil {
			body = strings.TrimSpace(body + "\n🏷️ " + s.Name)
		}
	}
	if body == "" && len(atts) == 0 && len(m.MessageSnapshots) > 0 {
		body = "↗️ Message transféré"
		if snap := m.MessageSnapshots[0].Message; snap != nil {
			fwd := renderContent(snap.Content, r)
			atts = attachmentsOf(&discordgo.Message{ID: m.ID, ChannelID: m.ChannelID, Attachments: snap.Attachments, Flags: snap.Flags})
			if fwd != "" {
				body += "\n" + fwd
			}
		}
	}
	if m.Poll != nil {
		body = strings.TrimSpace(body + "\n📊 " + m.Poll.Question.Text)
	}
	out.Text = body
	out.Attachments = atts
	out.Media = mediaSummary(atts)
	out.Reactions = reactionsOf(m)
	out.LinkPreview = linkPreviewOf(m)
	if m.MessageReference != nil && m.MessageReference.MessageID != "" && m.Type == discordgo.MessageTypeReply {
		out.ReplyTo = m.MessageReference.MessageID
		if ref := m.ReferencedMessage; ref != nil {
			qwho := "Vous"
			if ref.Author == nil || ref.Author.ID != me {
				qwho = ""
				if ref.Author != nil {
					qwho = firstOf(r.userName(ref.Author.ID), ref.Author.DisplayName())
				}
			}
			quoted := shorten(renderContent(ref.Content, r), 80)
			if quoted == "" {
				quoted = previewOf("", attachmentsOf(ref))
			}
			line := quoted
			if qwho != "" {
				line = qwho + " : " + quoted
			}
			out.ReplyText = line
			out.Text = quoteLine(line) + out.Text
		} else {
			out.ReplyText = "Message supprimé"
			out.Text = quoteLine(out.ReplyText) + out.Text
		}
	}
	return out
}

// retired : un message retiré ; il reste, barré, comme chez les autres services.
func retired(m Message) Message {
	m.Text, m.Media, m.Attachments, m.ReplyTo, m.ReplyText, m.Reactions, m.LinkPreview = "🚫 Message retiré", "", nil, "", "", nil, nil
	return m
}

// textBody : le texte d'un message sans la ligne citée (pour les aperçus).
func textBody(m Message) string {
	if m.ReplyText != "" {
		return strings.TrimPrefix(m.Text, quoteLine(m.ReplyText))
	}
	return m.Text
}

func errorText(err error) string {
	if re, ok := err.(*discordgo.RESTError); ok && re.Message != nil {
		return fmt.Sprintf("%s (code %d)", re.Message.Message, re.Message.Code)
	}
	return err.Error()
}
