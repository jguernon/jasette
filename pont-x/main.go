// Jasette — le pont X (Twitter), messages privés.
//
// Le frère des ponts Instagram (../pont-ig) et Messenger (../pont-meta) : un
// programme sans fenêtre que l'interface lance et à qui elle parle en JSON,
// une ligne par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur.
//
// X a déplacé ses messages privés vers « XChat », chiffré de bout en bout.
// Ce pont utilise twittermeow, la bibliothèque de mautrix-twitter (le pont
// officiel) :
//   - la connexion se fait avec les cookies de x.com (auth_token, ct0, twid) ;
//   - les clés de chiffrement du compte sont récupérées une fois, avec le code
//     d'accès X Chat (le « passcode » à 4 chiffres), dans le coffre Juicebox
//     de X : sans ce code, rien n'est lisible ;
//   - la boîte de réception et l'historique viennent de requêtes GraphQL
//     XChat, les nouveautés du socket XChat ;
//   - les demandes de messages (et les vieux groupes) passent encore par
//     l'ancienne API REST de X, interrogée de temps en temps.
//
// Les événements et les commandes sont exactement ceux des ponts Instagram et
// Messenger : l'interface utilise le même code pour tous.
//
// Tout l'état est gardé dans <données>/x/ (dossier 700, fichiers 600) :
//   - session.json : les cookies, l'identité du compte et ses clés XChat ;
//   - x.db         : SQLite (pur Go) avec les clés des conversations, la
//     position dans le flux XChat, et le cache des fils, contacts et messages.
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

// ---- Ce qui part vers l'interface (mêmes formats que les ponts Meta) ----

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
	// Vrai pour les conversations XChat chiffrées de bout en bout.
	E2EE bool `json:"e2ee"`
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
	Kind   string `json:"kind"` // "x" ou "admin"
	TmpID  string `json:"tmp_id,omitempty"`

	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`
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
	Actor  int64  `json:"actor"` // l'identifiant X de la personne

}

type Cmd struct {
	Cmd      string            `json:"cmd"`
	Conv     string            `json:"conv"`
	Text     string            `json:"text"`
	Count    int64             `json:"count"`
	Before   string            `json:"before"`
	BeforeT  int64             `json:"before_time"`
	Msg      string            `json:"msg"`
	TmpID    string            `json:"tmp_id"`
	Cookies  map[string]string `json:"cookies"`
	Pin      string            `json:"pin"`      // le code d'accès X Chat (voir login.go)
	Platform string            `json:"platform"` // toujours « x » ici
	MediaID  string            `json:"media_id"`
	Path     string            `json:"path"`
	Number   string            `json:"number"`
	Folder   string            `json:"folder"`
	Key      []byte            `json:"key"`
	Mime     string            `json:"mime"`
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

// Le service servi par ce pont : toujours X.
func serviceName() string { return "x" }

// Tout ce qui est propre au service : <données>/x (700).
func xDir() string {
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
		if strings.HasPrefix(mime, "audio/") {
			return ".m4a"
		}
		return ".mp4"
	case strings.Contains(mime, "ogg"):
		return ".ogg"
	case strings.Contains(mime, "pdf"):
		return ".pdf"
	}
	if exts, _ := mimepkg.ExtensionsByType(mime); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// Nom de fichier tiré d'un identifiant. Préfixé « x: » pour ne jamais
// croiser les fichiers des autres ponts (même dossier de médias).
func hashName(id string) string {
	h := sha256.Sum256([]byte("x:" + id))
	return hex.EncodeToString(h[:12])
}

// Écrit un fichier en entier, puis le renomme : jamais de fichier à moitié écrit.
func writeWhole(path string, data []byte) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	// Un nom temporaire unique : deux téléchargements du même fichier peuvent se croiser.
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
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Str("pont", "x").Logger()
	// Certaines parties de twittermeow journalisent avec le journal du contexte.
	zerolog.DefaultContextLogger = &log

	ctx, cancel := context.WithCancel(log.WithContext(context.Background()))
	defer cancel()
	p := newPont(ctx)
	emit("hello", map[string]any{"version": 1, "service": serviceName()})
	if err := p.openStore(); err != nil {
		log.Err(err).Msg("base de données")
		emitErr("store", fmt.Errorf("base de données X illisible : %w", err))
		emitState("unpaired")
	} else {
		// Synchrone jusqu'à « state » : l'interface reçoit hello puis l'état.
		p.start()
	}

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
		if p.db == nil {
			emitErr(cmd.Cmd, fmt.Errorf("le pont X n'a pas pu ouvrir sa base de données"))
			continue
		}
		switch cmd.Cmd {
		case "login":
			go p.login(cmd)
		case "pair", "google":
			emitErr(cmd.Cmd, fmt.Errorf("X se connecte avec la fenêtre de connexion (commande « login »)"))
		case "conversations":
			n := cmd.Count
			if n <= 0 {
				n = 50
			}
			go p.listConversations(int(n), cmd.Folder == "archive")
		case "messages":
			go p.messages(cmd)
		case "send":
			go p.send(cmd)
		case "send_media":
			go p.sendMedia(cmd)
		case "read":
			go p.markRead(cmd)
		case "download":
			go p.download(cmd)
		case "new":
			go p.newChat(cmd)
		case "search":
			go p.search(strings.TrimSpace(cmd.Text))
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
