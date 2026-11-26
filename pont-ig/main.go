// Jasette — le pont Instagram.
//
// Le frère du pont Messenger (../pont-meta) : un programme sans fenêtre que
// l'interface lance et à qui elle parle en JSON, une ligne par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur.
//
// Instagram a quitté le canal « lightspeed » de messagix pour ses messages
// privés. Ce pont utilise instameow, la nouvelle bibliothèque de mautrix-meta
// (celle du pont officiel igconnector) : la page instagram.com/direct, des
// requêtes GraphQL pour les fils et les messages, et le socket « iris » pour
// les nouveautés en direct (les « deltas »).
//
// Les événements et les commandes sont exactement ceux du pont Messenger :
// l'interface utilise le même code pour les deux.
//
// Tout l'état est gardé dans <données>/instagram/ (dossier 700, fichiers 600) :
//   - session.json  : les cookies et l'identité du compte ;
//   - instagram.db  : SQLite (pur Go) avec l'état d'instameow (pour se
//     reconnecter vite), la position dans le flux de deltas, et le cache des
//     fils, contacts et messages.
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

// ---- Ce qui part vers l'interface (mêmes formats que le pont Messenger) ----

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
	// Gardé pour le même format que Messenger : jamais vrai sur Instagram.
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
	Kind   string `json:"kind"` // "instagram" ou "admin"
	TmpID  string `json:"tmp_id,omitempty"`

	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`

	// L'aperçu du lien, tel qu'Instagram l'envoie (apercu.go).
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
	Actor  int64  `json:"actor"`
}

type Cmd struct {
	Cmd      string            `json:"cmd"`
	Conv     string            `json:"conv"`
	Text     string            `json:"text"`
	Count    int64             `json:"count"`
	Limit    int64             `json:"limit"` // search_messages : nombre de résultats au plus
	Before   string            `json:"before"`
	BeforeT  int64             `json:"before_time"`
	Msg      string            `json:"msg"`
	TmpID    string            `json:"tmp_id"`
	Cookies  map[string]string `json:"cookies"`
	Platform string            `json:"platform"` // toujours « instagram » ici
	MediaID  string            `json:"media_id"`
	Path     string            `json:"path"`
	Number   string            `json:"number"`
	Folder   string            `json:"folder"`
	Key      []byte            `json:"key"`
	Mime     string            `json:"mime"`
	// create_group : les identifiants (FBID de messagerie) des membres.
	Members []string `json:"members"`
	// react : le message visé et l'emoji (vide = retirer ma réaction).
	ID    string `json:"id"`
	Emoji string `json:"emoji"`
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

// Le service servi par ce pont : toujours Instagram.
func serviceName() string { return "instagram" }

// Tout ce qui est propre au service : <données>/instagram (700).
func igDir() string {
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

// Nom de fichier tiré d'un identifiant. Préfixé « ig: » pour ne jamais
// croiser les fichiers du pont Messenger (même dossier de médias).
func hashName(id string) string {
	h := sha256.Sum256([]byte("ig:" + id))
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
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Str("pont", "ig").Logger()
	// Certaines parties d'instameow journalisent avec le journal par défaut.
	zerolog.DefaultContextLogger = &log

	ctx, cancel := context.WithCancel(log.WithContext(context.Background()))
	defer cancel()
	p := newPont(ctx)
	emit("hello", map[string]any{"version": 1, "service": serviceName()})
	if err := p.openStore(); err != nil {
		log.Err(err).Msg("base de données")
		emitErr("store", fmt.Errorf("base de données Instagram illisible : %w", err))
		emitState("unpaired")
	} else {
		go p.start()
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
			emitErr(cmd.Cmd, fmt.Errorf("le pont Instagram n'a pas pu ouvrir sa base de données"))
			continue
		}
		switch cmd.Cmd {
		case "login":
			go p.login(cmd)
		case "pair", "google":
			emitErr(cmd.Cmd, fmt.Errorf("Instagram se connecte avec la fenêtre de connexion (commande « login »)"))
		case "conversations":
			n := cmd.Count
			if n <= 0 {
				n = 50
			}
			go p.listConversations(int(n), cmd.Folder == "archive")
		case "messages":
			go p.messages(cmd)
		case "react":
			go p.sendReaction(cmd)
		case "send":
			go p.send(cmd)
		case "send_media":
			go p.sendMedia(cmd)
		case "read":
			go p.markRead(cmd)
		case "delete":
			go p.deleteConv(cmd)
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
