package main

// Des événements Matrix aux messages de Jasette : texte, image, réponse
// (Reddit répond par des fils m.thread), réaction (palette propre à Reddit),
// retrait (rédaction).

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// mxEvent : un événement Matrix tel que /sync et /messages le rendent. Le
// contenu reste brut : chaque sorte le lit à sa façon.
type mxEvent struct {
	Type     string          `json:"type"`
	Sender   string          `json:"sender"`
	ID       string          `json:"event_id"`
	RoomID   string          `json:"room_id,omitempty"`
	TS       int64           `json:"origin_server_ts"`
	StateKey *string         `json:"state_key,omitempty"`
	Content  json.RawMessage `json:"content"`
	Redacts  string          `json:"redacts,omitempty"`
	Unsigned struct {
		TxnID           string          `json:"transaction_id,omitempty"`
		RedactedBecause json.RawMessage `json:"redacted_because,omitempty"`
	} `json:"unsigned"`
}

// ---- Les plafonds ----

const (
	maxTexte  = 10000    // caractères d'un message
	maxImage  = 20 << 20 // octets d'une image
	maxGIF    = 100 << 20
	maxPage   = 50 // messages par page d'historique
	maxEnMem  = 200
	maxDlFile = 120 << 20
)

// mimesImages : seules les images passent (Reddit vérifie les octets).
var mimesImages = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

// plafondImage : la taille maximale d'une image de ce type ; 0 = refusé.
func plafondImage(mime string) int64 {
	if !mimesImages[mime] {
		return 0
	}
	if mime == "image/gif" {
		return maxGIF
	}
	return maxImage
}

func texteTropLong(s string) bool { return utf8.RuneCountInString(s) > maxTexte }

// ---- La palette des réactions ----

// Reddit n'accepte que les clés de sa palette (des noms de fichiers sur
// i.redd.it), jamais un émoji Unicode. Les émojis de Jasette qui ont un
// équivalent passent ; les autres sont refusés clairement.
var paletteReddit = map[string]string{
	"care": "8r21ukpfa7081.gif", "cry": "mag7v6tfa7081.gif", "disapproval": "iuqmp7ufa7081.gif",
	"dizzy_face": "zn7iubvfa7081.gif", "doge": "ytv3x0sfa7081.png", "facepalm": "qzl5vyxfa7081.gif",
	"feels_bad_man": "mi2jolzfa7081.gif", "feels_good_man": "k7ry7t1ga7081.gif", "flip_out": "tspuf53ga7081.gif",
	"flushed": "evwks24ga7081.gif", "give_upvote": "dfxygs4ga7081.gif", "grimacing": "ax7wu47ga7081.gif",
	"grin": "uy83aa8ga7081.gif", "heart_eyes": "t2r5xc9ga7081.gif", "heart_eyes_rainbow": "sjs1a2fyt7081.gif",
	"hug": "ksz4fmaga7081.gif", "joy": "jvuspmbga7081.gif", "kissing_heart": "mp9zclcga7081.gif",
	"laughing": "9ut7iedga7081.gif", "money_face": "ul2w17ega7081.gif", "neutral_face": "0ku6twega7081.gif",
	"no_mouth": "cdnhvqfga7081.gif", "put_back": "b5s6cohga7081.gif", "rage": "av9z8iiga7081.gif",
	"scream": "00brcfjga7081.gif", "shrug": "pleyoikga7081.gif", "sleep": "m7uy86lga7081.gif",
	"slightly_smiling": "8kw138jyt7081.gif", "smile": "wbrgz1nga7081.gif", "snoo": "fsg2a1oga7081.gif",
	"sob": "8zx5ixoga7081.gif", "stuck_out_tongue": "t5bqwxdyt7081.gif", "sunglasses": "xjls1pqga7081.gif",
	"surprise": "7rc03trga7081.gif", "sweat": "d45pfmsga7081.gif", "sweat_smile": "g6akcitga7081.gif",
	"table": "t1djdguga7081.gif", "table_flip": "19b5q4vga7081.gif", "thinking_face_hmm": "4zhlw4iyt7081.gif",
	"thumbs_down": "3s0glewga7081.gif", "thumbs_up": "7url39xga7081.gif", "trollface": "d2kn6yxga7081.gif",
	"upvote": "foyijyyga7081.gif", "wave": "2o3aooqfa7081.gif", "wink": "3i2arwzga7081.gif",
	"yummy": "79opsq0ha7081.gif",
}

// emojiVersNom : les émojis Unicode qui ont un équivalent dans la palette.
var emojiVersNom = map[string]string{
	"👍": "thumbs_up", "👎": "thumbs_down", "😂": "joy", "🤣": "laughing", "😆": "laughing",
	"😍": "heart_eyes", "😘": "kissing_heart", "❤": "heart_eyes", "♥": "heart_eyes", "😢": "cry",
	"😭": "sob", "😮": "surprise", "😯": "surprise", "😲": "surprise", "😱": "scream", "😡": "rage",
	"😠": "rage", "🤔": "thinking_face_hmm", "😎": "sunglasses", "😉": "wink", "👋": "wave",
	"🙂": "slightly_smiling", "😊": "smile", "😄": "smile", "😃": "smile", "😁": "grin",
	"😀": "grin", "😛": "stuck_out_tongue", "😜": "stuck_out_tongue", "😋": "yummy", "😅": "sweat_smile",
	"😓": "sweat", "😬": "grimacing", "😳": "flushed", "😵": "dizzy_face", "😐": "neutral_face",
	"😶": "no_mouth", "🤑": "money_face", "🤗": "hug", "🥰": "care", "😴": "sleep", "🤦": "facepalm",
	"🤷": "shrug", "⬆": "upvote", "🔼": "upvote", "🙁": "disapproval", "☹": "disapproval", "😒": "disapproval",
}

// nomVersEmoji : la clé reçue, montrée comme un émoji quand c'est possible.
var nomVersEmoji = map[string]string{
	"thumbs_up": "👍", "thumbs_down": "👎", "joy": "😂", "laughing": "🤣", "heart_eyes": "😍",
	"heart_eyes_rainbow": "😍", "kissing_heart": "😘", "cry": "😢", "sob": "😭", "surprise": "😮",
	"scream": "😱", "rage": "😡", "thinking_face_hmm": "🤔", "sunglasses": "😎", "wink": "😉",
	"wave": "👋", "slightly_smiling": "🙂", "smile": "😊", "grin": "😁", "stuck_out_tongue": "😛",
	"yummy": "😋", "sweat_smile": "😅", "sweat": "😓", "grimacing": "😬", "flushed": "😳",
	"dizzy_face": "😵", "neutral_face": "😐", "no_mouth": "😶", "money_face": "🤑", "hug": "🤗",
	"care": "🥰", "sleep": "😴", "facepalm": "🤦", "shrug": "🤷", "upvote": "⬆️", "give_upvote": "⬆️",
	"disapproval": "🙁", "snoo": "👽", "doge": "🐶", "table_flip": "(╯°□°)╯︵ ┻━┻",
	"flip_out": "(╯°□°)╯︵ ┻━┻", "put_back": "┬─┬ノ( º _ ºノ)", "table": "┬─┬", "trollface": "😈",
	"feels_good_man": "🐸", "feels_bad_man": "🐸",
}

// cleReaction : la clé Reddit d'un émoji de Jasette (ou d'un nom de la
// palette, « :joy: »). Vide : pas d'équivalent.
func cleReaction(emoji string) string {
	e := strings.TrimSpace(emoji)
	if n := strings.Trim(e, ":"); paletteReddit[n] != "" {
		return paletteReddit[n]
	}
	for _, k := range paletteReddit {
		if k == e {
			return k
		}
	}
	// Sans sélecteur de variante ni teinte de peau.
	var b strings.Builder
	for _, r := range e {
		if r == 0xFE0F || r == 0xFE0E || (r >= 0x1F3FB && r <= 0x1F3FF) || r == 0x200D {
			continue
		}
		b.WriteRune(r)
	}
	if n := emojiVersNom[b.String()]; n != "" {
		return paletteReddit[n]
	}
	return ""
}

// emojiDeCle : ce que l'interface montre pour une clé reçue.
func emojiDeCle(key string) string {
	for n, k := range paletteReddit {
		if k == key {
			if e := nomVersEmoji[n]; e != "" {
				return e
			}
			return ":" + n + ":"
		}
	}
	if key == "" {
		return "❔"
	}
	// Un émoji Unicode (autre client Matrix), ou une clé inconnue.
	if strings.ContainsAny(key, ".") {
		return ":" + strings.TrimSuffix(strings.TrimSuffix(key, ".gif"), ".png") + ":"
	}
	return key
}

// ---- Le contenu des messages ----

type relatesTo struct {
	RelType   string `json:"rel_type,omitempty"`
	EventID   string `json:"event_id,omitempty"`
	Key       string `json:"key,omitempty"`
	Falling   bool   `json:"is_falling_back,omitempty"`
	InReplyTo *struct {
		EventID string `json:"event_id"`
	} `json:"m.in_reply_to,omitempty"`
}

type msgContent struct {
	MsgType   string     `json:"msgtype"`
	Body      string     `json:"body"`
	URL       string     `json:"url"`
	RelatesTo *relatesTo `json:"m.relates_to"`
	Info      *struct {
		Mime string `json:"mimetype"`
		Size int64  `json:"size"`
		W    int64  `json:"w"`
		H    int64  `json:"h"`
	} `json:"info"`
	// Réaction « à plat » (le client de Reddit en envoie parfois).
	EventID string `json:"event_id"`
	Key     string `json:"key"`
	RelType string `json:"rel_type"`
	// com.reddit.profile
	Username     string `json:"username"`
	DisplayName  string `json:"displayname"`
	IconURL      string `json:"icon_url"`
	SnoovatarURL string `json:"snoovatar_url"`
	// com.reddit.invite_spam_status
	Status string `json:"status"`
	// m.room.redaction (v11 : dans le contenu)
	Redacts string `json:"redacts"`
	// Image migrée (lien direct vers le CDN)
	NSFW bool `json:"com.reddit.nsfw_image"`
}

func parseContent(raw json.RawMessage) msgContent {
	var c msgContent
	_ = json.Unmarshal(raw, &c)
	return c
}

// cible : le message visé par une relation (réaction).
func (c msgContent) cibleReaction() (cible, cle string) {
	if c.RelatesTo != nil && c.RelatesTo.RelType == "m.annotation" {
		return c.RelatesTo.EventID, c.RelatesTo.Key
	}
	if c.RelType == "m.annotation" {
		return c.EventID, c.Key
	}
	return "", ""
}

// repondA : le message auquel celui-ci répond (fil m.thread : la racine,
// sinon le message cité ; le repli « is_falling_back » vise la racine).
func (c msgContent) repondA() (cible, racine string) {
	r := c.RelatesTo
	if r == nil {
		return "", ""
	}
	if r.RelType == "m.thread" {
		racine = r.EventID
		if r.InReplyTo != nil && r.InReplyTo.EventID != "" && !r.Falling {
			return r.InReplyTo.EventID, racine
		}
		return racine, racine
	}
	if r.InReplyTo != nil {
		return r.InReplyTo.EventID, ""
	}
	return "", ""
}

// sansCitation : le corps sans les lignes « > … » de repli d'une réponse.
func sansCitation(body string) string {
	if !strings.HasPrefix(body, "> ") {
		return body
	}
	lignes := strings.Split(body, "\n")
	i := 0
	for i < len(lignes) && strings.HasPrefix(lignes[i], ">") {
		i++
	}
	for i < len(lignes) && strings.TrimSpace(lignes[i]) == "" {
		i++
	}
	return strings.Join(lignes[i:], "\n")
}

func shorten(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func quoteLine(s string) string { return "↪ " + s + "\n" }

// estMessage : les sortes d'événements montrées comme des messages.
func estMessage(t string) bool { return t == "m.room.message" || t == "m.sticker" }

// convertir : un événement m.room.message (ou m.sticker) en message de
// Jasette. nom : le nom d'une personne ; ok faux si l'événement ne se montre
// pas (modification, type inconnu).
func convertir(ev mxEvent, conv, me string, nom func(string) string) (Message, bool) {
	if !estMessage(ev.Type) {
		return Message{}, false
	}
	c := parseContent(ev.Content)
	if c.RelatesTo != nil && c.RelatesTo.RelType == "m.replace" {
		// Reddit ne permet pas de modifier ; une modification venue d'ailleurs est ignorée.
		return Message{}, false
	}
	m := Message{
		ID:     ev.ID,
		Conv:   conv,
		FromMe: ev.Sender == me,
		Time:   ev.TS,
		Kind:   "reddit",
		TmpID:  ev.Unsigned.TxnID,
	}
	if m.FromMe {
		m.Status = "OUTGOING_DELIVERED"
	} else {
		m.Status = "INCOMING_COMPLETE"
		m.Sender = nom(ev.Sender)
		m.SenderID = ev.Sender
	}
	if len(ev.Unsigned.RedactedBecause) > 0 || (len(ev.Content) <= 2 && ev.Type == "m.room.message") {
		return retire(m), true
	}
	switch {
	case ev.Type == "m.sticker" || c.MsgType == "m.image":
		a := Attachment{MediaID: c.URL, Mime: "image/png", Name: "image"}
		if c.Info != nil {
			a.Mime = firstOf(c.Info.Mime, a.Mime)
			a.Size, a.W, a.H = c.Info.Size, c.Info.W, c.Info.H
		}
		if ev.Type == "m.sticker" {
			a.Name = "sticker"
		}
		if a.MediaID != "" {
			m.Attachments = []Attachment{a}
			m.Media = "Photo"
		}
		// Le corps d'une image est « Image » (ou le nom du fichier) : pas un texte.
		if b := strings.TrimSpace(c.Body); b != "" && !strings.EqualFold(b, "image") && b != c.URL && !strings.Contains(b, ".") {
			m.Text = b
		}
	case c.MsgType == "m.text" || c.MsgType == "m.notice" || c.MsgType == "m.emote" || c.MsgType == "":
		m.Text = sansCitation(c.Body)
		if c.MsgType == "m.emote" {
			m.Text = "* " + m.Text
		}
	default:
		// Vidéo, son, fichier : Reddit n'en a pas ; on le dit sans planter.
		m.Text = firstOf(c.Body, "(pièce jointe non prise en charge)")
	}
	if cible, racine := c.repondA(); cible != "" {
		m.ReplyTo = cible
		m.racine = racine
	}
	return m, true
}

// retire : le message effacé (rédaction).
func retire(m Message) Message {
	m.Text, m.System = "Message retiré", "deleted"
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
		return m.Text
	}
	t := strings.TrimSpace(m.Text)
	if t == "" && len(m.Attachments) > 0 {
		t = "📷 Photo"
	}
	return shorten(t, 120)
}

// mentionne : le texte me nomme (« u/moi » ou « @moi »).
func mentionne(text, monNom string) bool {
	if monNom == "" {
		return false
	}
	l := strings.ToLower(text)
	n := strings.ToLower(monNom)
	for _, pre := range []string{"u/" + n, "@" + n} {
		i := strings.Index(l, pre)
		for i >= 0 {
			fin := i + len(pre)
			if fin >= len(l) || !estCarNom(l[fin]) {
				return true
			}
			j := strings.Index(l[fin:], pre)
			if j < 0 {
				break
			}
			i = fin + j
		}
	}
	return false
}

func estCarNom(b byte) bool {
	return b == '_' || b == '-' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z')
}
