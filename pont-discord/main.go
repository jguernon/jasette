// Jasette — le pont Discord (messages privés et groupes privés).
//
// Le frère des ponts WhatsApp, Signal, Telegram et Bluesky : un programme
// sans fenêtre que l'interface lance et à qui elle parle en JSON, une ligne
// par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur.
//
// Ce pont repose sur la bibliothèque de mautrix-discord (go.mau.fi/mautrix-
// discord, AGPL) : sa version modifiée de discordgo (github.com/beeper/
// discordgo), qui se présente comme le client web de Discord (en-têtes,
// X-Super-Properties, IDENTIFY d'un navigateur, empreinte TLS de Chrome), et
// son paquet remoteauth (la connexion par code QR). Jasette est une session
// de plus du compte, comme un onglet discord.com ouvert.
//
// Prudence : Discord interdit les « self-bots » (un compte utilisateur piloté
// par un programme). Ce pont n'agit que sur une action de l'utilisateur
// (lire, envoyer, réagir, marquer comme lu), ne fait aucune action
// automatique, espace ses requêtes (voir throttle) et ne précharge que peu
// d'historique. Le risque reste : voir le README.
//
// Les événements et les commandes sont ceux des autres ponts : l'interface
// utilise le même code pour tous. Propre à Discord :
//
//   - la connexion :
//     {"cmd":"pair"} → des {"ev":"qr","code":"https://discord.com/ra/…"}, à
//     scanner dans l'app Discord du téléphone (Paramètres → Scanner le
//     code QR) ; renouvelé à l'expiration (au plus quelques fois).
//     {"cmd":"login","token":"…"} → le jeton du compte, collé à la main.
//     Réussite : paired, account, state ready. Échec : error (where : pair ou
//     login) et state unpaired.
//   - seulement les messages privés et les groupes privés (pas les salons des
//     serveurs Discord) : voir le README.
//   - pas d'archives chez Discord : Jasette les garde elle-même (marques).
//
// Aucune commande ne marque un message comme lu, sauf « read ».
//
// Tout l'état est gardé dans <données>/discord/ (dossier 700, fichiers 600) :
//   - session.json : le jeton du compte, son id, et la session de
//     « heartbeat » du client (comme le client web la garde).
//
// Les messages ne sont pas gardés sur le disque : Discord garde l'historique.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mimepkg "mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ---- Ce qui part vers l'interface (mêmes formats que les autres ponts) ----

type Conversation struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Preview  string   `json:"preview"`
	FromMe   bool     `json:"from_me"`
	Time     int64    `json:"time"` // millisecondes
	Unread   bool     `json:"unread"`
	Group    bool     `json:"group"`
	RCS      bool     `json:"rcs"`
	Color    string   `json:"color"`
	Members  []string `json:"members"`
	ReadOnly bool     `json:"read_only"`
	Archived bool     `json:"archived"`
	E2EE     bool     `json:"e2ee"`
}

type Message struct {
	ID     string `json:"id"`
	Conv   string `json:"conv"`
	Text   string `json:"text"`
	Media  string `json:"media,omitempty"`
	FromMe bool   `json:"from_me"`
	Sender string `json:"sender"`
	Time   int64  `json:"time"` // millisecondes
	Status string `json:"status"`
	Kind   string `json:"kind"` // "discord" ou "admin"
	TmpID  string `json:"tmp_id,omitempty"`

	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`

	// Le message cité ; le texte cité est aussi en tête de « text ».
	ReplyTo   string `json:"reply_to,omitempty"`
	ReplyText string `json:"reply_text,omitempty"`

	LinkPreview *LinkPreview `json:"link_preview,omitempty"`
}

type Attachment struct {
	MediaID string `json:"media_id"`
	Key     []byte `json:"key"` // en base64 dans le JSON : de quoi retrouver le fichier (voir media.go)
	Mime    string `json:"mime"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	W       int64  `json:"w"`
	H       int64  `json:"h"`
}

type Reaction struct {
	Emoji  string `json:"emoji"`
	Sender string `json:"sender"`
	FromMe bool   `json:"from_me"`
	// L'id Discord de la personne ; vide quand Discord ne donne que le nombre.
	Actor string `json:"actor"`
}

// LinkPreview : le champ « link_preview » d'un message (PROTOCOLE.md, §5.9).
type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Site        string `json:"site,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

type Cmd struct {
	Cmd     string `json:"cmd"`
	Conv    string `json:"conv"`
	Text    string `json:"text"`
	Count   int64  `json:"count"`
	Limit   int64  `json:"limit"` // search_messages : nombre de résultats au plus
	Before  string `json:"before"`
	BeforeT int64  `json:"before_time"`
	Msg     string `json:"msg"`
	TmpID   string `json:"tmp_id"`
	MediaID string `json:"media_id"`
	Path    string `json:"path"`
	Number  string `json:"number"`
	Folder  string `json:"folder"`
	Key     []byte `json:"key"`
	Mime    string `json:"mime"`
	On      bool   `json:"on"`
	ReplyTo string `json:"reply_to"`
	// react : le message visé et l'emoji (vide = retirer ma réaction).
	ID    string `json:"id"`
	Emoji string `json:"emoji"`
	// Connexion par jeton (« token », ou « password » comme Bluesky).
	Token    string `json:"token"`
	Password string `json:"password"`
	// create_group : les personnes (ids Discord) et le nom, facultatif.
	Members []string `json:"members"`
	Name    string   `json:"name"`
}

var (
	outMu sync.Mutex
	out   = json.NewEncoder(os.Stdout)
	log   zerolog.Logger
)

func emit(ev string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["ev"] = ev
	outMu.Lock()
	defer outMu.Unlock()
	_ = out.Encode(fields)
}

func emitErr(where string, err error) {
	emit("error", map[string]any{"where": where, "text": err.Error()})
}

func emitState(state string) {
	emit("state", map[string]any{"state": state})
}

// ---- Les dossiers ----

func dataDir() string {
	if d := os.Getenv("JASETTE_DATA"); d != "" {
		return d
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "jasette")
}

// Le service servi par ce pont : toujours Discord.
func serviceName() string { return "discord" }

// Tout ce qui est propre au service : <données>/discord (700).
func dcDir() string {
	d := filepath.Join(dataDir(), serviceName())
	_ = os.MkdirAll(d, 0o700)
	_ = os.Chmod(d, 0o700)
	return d
}

// Les photos et avatars téléchargés : JASETTE_MEDIA, sinon <données>/media.
func mediaDir() string {
	if d := os.Getenv("JASETTE_MEDIA"); d != "" {
		return d
	}
	return filepath.Join(dataDir(), "media")
}

func extension(mime string, data []byte) string {
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
	}
	switch {
	case strings.Contains(mime, "jpeg"):
		return ".jpg"
	case strings.Contains(mime, "png"):
		return ".png"
	case strings.Contains(mime, "gif"):
		return ".gif"
	case strings.Contains(mime, "webp"):
		return ".webp"
	case strings.Contains(mime, "mp4"):
		return ".mp4"
	case strings.Contains(mime, "ogg"), strings.Contains(mime, "opus"):
		return ".ogg"
	case strings.Contains(mime, "aac"):
		return ".aac"
	case strings.Contains(mime, "mpeg") && strings.HasPrefix(mime, "audio/"):
		return ".mp3"
	case strings.Contains(mime, "pdf"):
		return ".pdf"
	}
	if exts, _ := mimepkg.ExtensionsByType(mime); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// Nom de fichier tiré d'un identifiant. Préfixé « dc: » pour ne jamais
// croiser les fichiers des autres ponts (même dossier de médias).
func hashName(id string) string {
	h := sha256.Sum256([]byte("dc:" + id))
	return hex.EncodeToString(h[:12])
}

// Écrit un fichier en entier, puis le renomme : jamais de fichier à moitié écrit.
func writeWhole(path string, data []byte) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	level := zerolog.InfoLevel
	if os.Getenv("JASETTE_DEBUG") != "" {
		level = zerolog.DebugLevel
	}
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Str("pont", "discord").Logger()
	zerolog.DefaultContextLogger = &log

	ctx, cancel := context.WithCancel(log.WithContext(context.Background()))
	defer cancel()
	p := newPont(ctx)
	emit("hello", map[string]any{"version": 1, "service": serviceName()})
	go p.start()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		var cmd Cmd
		if err := json.Unmarshal(in.Bytes(), &cmd); err != nil {
			emitErr("cmd", fmt.Errorf("commande illisible : %w", err))
			continue
		}
		if cmd.Cmd == "quit" {
			break
		}
		switch cmd.Cmd {
		case "pair":
			go p.pairQR()
		case "login":
			go p.loginToken(strings.TrimSpace(firstOf(cmd.Token, cmd.Password)))
		case "pair_phone", "google", "login_code", "login_password", "api":
			emitErr(cmd.Cmd, fmt.Errorf("Discord se connecte par code QR (« pair ») ou par jeton (« login »)"))
		case "conversations":
			go p.listConversations(cmd.Folder == "archive")
		case "messages":
			go p.messages(cmd)
		case "send":
			go p.send(cmd)
		case "send_media":
			go p.sendMedia(cmd)
		case "react":
			go p.react(cmd)
		case "read":
			go p.markRead(cmd)
		case "download":
			go p.download(cmd)
		case "new":
			go p.newChat(cmd)
		case "create_group":
			go p.createGroup(cmd)
		case "search":
			go p.search(strings.TrimSpace(cmd.Text))
		case "search_messages":
			go p.searchMessages(cmd.Text, int(cmd.Limit))
		case "typing":
			go p.sendTyping(cmd)
		case "presence":
			p.setPresence(cmd.On)
		case "archive", "delete":
			emitErr(cmd.Cmd, fmt.Errorf("Discord : pas d'archives ni de suppression de conversation (celles de Jasette s'en chargent)"))
		case "history_pin":
			// Propre aux fils chiffrés de Messenger : rien à faire ici (et
			// surtout pas de réponse, l'interface la prendrait pour Messenger).
		case "unpair", "logout":
			go p.unpair()
		default:
			emitErr("cmd", fmt.Errorf("commande inconnue : %q", cmd.Cmd))
		}
	}
	// « quit » ou l'interface est partie : on ferme proprement.
	p.shutdown()
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
