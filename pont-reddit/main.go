// Jasette — le pont Reddit (le chat de Reddit).
//
// Le frère des ponts WhatsApp, Signal, Telegram, Bluesky et Discord : un
// programme sans fenêtre que l'interface lance et à qui elle parle en JSON,
// une ligne par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur. Jamais de cookie ni de jeton.
//
// Le chat de Reddit est un serveur Matrix (https://matrix.redditspace.com,
// identifiants « @t2_xxx:reddit.com », sans chiffrement de bout en bout).
// Les anciens messages privés y sont fusionnés depuis août 2025. Le pont est
// un client Matrix ordinaire (mautrix-go) : /sync pour le direct, /messages
// pour l'historique, /media pour les images. Seule la connexion est propre à
// Reddit (jeton.go) : les cookies de reddit.com, pris dans la fenêtre de
// connexion où l'utilisateur s'est connecté lui-même, donnent un jeton de
// chat (un JWT d'environ 24 h), renouvelé une heure avant son échéance.
//
// Prudence : Reddit n'offre pas d'API officielle pour le chat. Le pont se
// présente comme le navigateur (User-Agent), espace ses requêtes, ne charge
// qu'une page d'historique à l'ouverture d'un salon, ne marque lu et
// n'envoie qu'à la demande de l'utilisateur, et n'annonce jamais « écrit… ».
//
// Les événements et les commandes sont ceux des autres ponts. Propre à Reddit :
//
//   - la connexion : {"cmd":"login","cookies":{"reddit_session":"…",
//     "csrf_token":"…","token_v2":"…",…}} → paired, account, state ready ;
//     ou error (where : login) et state unpaired.
//   - les ids : celui du salon Matrix (« !abc:reddit.com ») ; un message :
//     son id d'événement Matrix (« $… »).
//   - les salons de communauté (chats de subreddit) sont des grands espaces
//     de l'onglet Groupes (PROTOCOLE.md §5.13) : large, kind « subreddit »,
//     parent « sr:<nom> », parent_name « r/<nom> ».
//   - les demandes de message (invitations) : request:true dans la liste ;
//     {"cmd":"accept","conv":…} les accepte, {"cmd":"ignore","conv":…} les
//     refuse (on quitte le salon).
//   - images seulement (JPEG, PNG, GIF, WebP ; 20 Mo, 100 Mo pour un GIF) ;
//     pas de modification ; réactions dans la palette de Reddit seulement.
//   - pas d'archives chez Reddit : Jasette les garde elle-même (marques).
//
// Aucune commande ne marque un message comme lu, sauf « read ».
//
// Tout l'état est gardé dans <données>/reddit/ (dossier 700, fichiers 600) :
//   - session.json : les cookies de reddit.com, le jeton de chat, son
//     échéance, l'identifiant Matrix et le jeton de reprise de /sync.
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

	// Une demande de message (invitation pas encore acceptée).
	Request bool `json:"request,omitempty"`

	// Salon de communauté (PROTOCOLE.md, §5.13) : grand espace de l'onglet Groupes.
	Large      bool   `json:"large,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Parent     string `json:"parent,omitempty"`
	ParentName string `json:"parent_name,omitempty"`
	Mentions   int    `json:"mentions,omitempty"`
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
	Kind   string `json:"kind"` // "reddit" ou "admin"
	TmpID  string `json:"tmp_id,omitempty"`

	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`

	// Le message cité ; le texte cité est aussi en tête de « text ».
	ReplyTo   string `json:"reply_to,omitempty"`
	ReplyText string `json:"reply_text,omitempty"`

	// Le message me mentionne, ou répond à un des miens (§5.13).
	MentionsMe bool `json:"mentions_me,omitempty"`
	ReplyToMe  bool `json:"reply_to_me,omitempty"`

	// L'id Matrix de l'auteur (groupes et salons) : sa photo arrive par
	// « sender_avatar ».
	SenderID string `json:"sender_id,omitempty"`

	// La racine du fil m.thread où il est (pour y répondre) ; pas envoyé.
	racine string
}

type Attachment struct {
	MediaID string `json:"media_id"` // l'adresse mxc:// de l'image
	Key     []byte `json:"key"`      // inutilisé (Reddit ne chiffre pas)
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
	Actor  string `json:"actor"` // l'id Matrix de la personne
}

type Cmd struct {
	Cmd     string            `json:"cmd"`
	Conv    string            `json:"conv"`
	Text    string            `json:"text"`
	Count   int64             `json:"count"`
	Limit   int64             `json:"limit"`
	Before  string            `json:"before"`
	BeforeT int64             `json:"before_time"`
	Msg     string            `json:"msg"`
	TmpID   string            `json:"tmp_id"`
	MediaID string            `json:"media_id"`
	Path    string            `json:"path"`
	Mime    string            `json:"mime"`
	Folder  string            `json:"folder"`
	On      bool              `json:"on"`
	ReplyTo string            `json:"reply_to"`
	ID      string            `json:"id"`
	Emoji   string            `json:"emoji"`
	Cookies map[string]string `json:"cookies"`
	Auto    bool              `json:"auto"`
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

// Le service servi par ce pont : toujours Reddit.
func serviceName() string { return "reddit" }

// Tout ce qui est propre au service : <données>/reddit (700).
func rdDir() string {
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
	}
	if exts, _ := mimepkg.ExtensionsByType(mime); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// Nom de fichier tiré d'un identifiant. Préfixé « rd: » pour ne jamais
// croiser les fichiers des autres ponts (même dossier de médias).
func hashName(id string) string {
	h := sha256.Sum256([]byte("rd:" + id))
	return hex.EncodeToString(h[:12])
}

// Écrit un fichier en entier (0600), puis le renomme : jamais de fichier à moitié écrit.
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
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Str("pont", "reddit").Logger()

	ctx, cancel := context.WithCancel(context.Background())
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
		case "login":
			go p.login(cmd.Cookies)
		case "pair", "pair_phone", "google", "login_code", "login_password", "api":
			emitErr(cmd.Cmd, fmt.Errorf("Reddit se connecte par la fenêtre de reddit.com (« login » avec ses cookies)"))
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
		case "accept":
			go p.acceptRequest(cmd)
		case "ignore":
			go p.ignoreRequest(cmd)
		case "search":
			go p.search(strings.TrimSpace(cmd.Text))
		case "search_messages":
			go p.searchMessages(cmd.Text, int(cmd.Limit))
		case "new", "create_group":
			emitErr(cmd.Cmd, fmt.Errorf("Reddit : commencez la conversation dans Reddit, elle apparaîtra ici"))
		case "edit":
			emitErr("edit", fmt.Errorf("Reddit ne permet pas de modifier un message"))
		case "typing":
			// Jasette n'annonce jamais « écrit… » à notre nom (prudence).
		case "presence":
			// Reddit ne publie pas de présence : rien à suivre.
		case "note_set", "note_clear":
			emit("note_result", map[string]any{"op": cmd.Cmd, "ok": false, "error": "Reddit n'a pas de note ni de statut"})
		case "archive", "delete":
			emitErr(cmd.Cmd, fmt.Errorf("Reddit : pas d'archives ni de suppression de conversation (celles de Jasette s'en chargent)"))
		case "history_pin":
			// Propre aux fils chiffrés de Messenger : rien à faire ici.
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
