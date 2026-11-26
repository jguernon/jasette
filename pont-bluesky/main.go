// Jasette — le pont Bluesky (messages privés seulement).
//
// Le frère des ponts WhatsApp, Signal et Telegram : un programme sans fenêtre
// que l'interface lance et à qui elle parle en JSON, une ligne par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur.
//
// Bluesky (AT Protocol) n'a pas besoin de bibliothèque : de simples appels
// XRPC en HTTPS. Les messages privés (lexique chat.bsky.convo.*) passent par
// le serveur personnel (PDS) du compte, qui les relaie au service de
// messagerie de Bluesky (en-tête « atproto-proxy: did:web:api.bsky.chat »).
// Les nouvelles arrivent en interrogeant chat.bsky.convo.getLog toutes les
// quelques secondes : Bluesky n'offre pas de flux en direct pour les messages.
//
// Propre à Bluesky :
//
//   - la connexion : {"cmd":"login","number":"nom.bsky.social ou courriel",
//     "password":"mot de passe d'app"} → paired, account, state ready ; ou
//     error (where : login). Il faut un **mot de passe d'app** avec l'accès
//     aux messages privés (Réglages > Confidentialité et sécurité > Mots de
//     passe d'app, case « Autoriser l'accès à vos messages privés ») : le
//     mot de passe du compte n'est jamais demandé ni gardé.
//   - pas de pièces jointes ni de réponses citées dans les messages privés de
//     Bluesky : send_media répond par une erreur.
//   - pas d'archives chez Bluesky : Jasette les garde elle-même (marques).
//
// Aucune commande ne marque un message comme lu, sauf « read »
// (chat.bsky.convo.updateRead).
//
// Tout l'état est gardé dans <données>/bluesky/ (dossier 700, fichiers 600) :
//   - session.json : le compte (DID, PDS) et ses jetons accessJwt et
//     refreshJwt, renouvelés au besoin ;
//   - bluesky.db   : SQLite (pur Go), le cache des messages (pour la
//     recherche et les réactions).
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	ID   string `json:"id"`
	Conv string `json:"conv"`
	Text string `json:"text"`
	// Un message de service connu (« deleted »…) : le client montre son
	// texte dans sa langue (PROTOCOLE.md §5.14).
	System      string       `json:"system,omitempty"`
	FromMe      bool         `json:"from_me"`
	Sender      string       `json:"sender"`
	Time        int64        `json:"time"` // millisecondes
	Status      string       `json:"status"`
	Kind        string       `json:"kind"` // "bluesky"
	TmpID       string       `json:"tmp_id,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`
}

type Attachment struct {
	MediaID string `json:"media_id"`
	Mime    string `json:"mime"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
}

type Reaction struct {
	Emoji  string `json:"emoji"`
	Sender string `json:"sender"`
	FromMe bool   `json:"from_me"`
	Actor  string `json:"actor"` // le DID de la personne
}

type Cmd struct {
	Cmd      string `json:"cmd"`
	Conv     string `json:"conv"`
	Text     string `json:"text"`
	Count    int64  `json:"count"`
	Limit    int64  `json:"limit"`
	Before   string `json:"before"`
	Msg      string `json:"msg"`
	TmpID    string `json:"tmp_id"`
	MediaID  string `json:"media_id"`
	Number   string `json:"number"`
	Folder   string `json:"folder"`
	ID       string `json:"id"`
	Emoji    string `json:"emoji"`
	Password string `json:"password"`
	// note_set : la source (« main » ou « musique ») et l'id de la demande
	// (notes.go). L'audience ne sert qu'à Instagram.
	Audience int    `json:"audience"`
	Source   string `json:"source"`
	Req      string `json:"req"`
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

func serviceName() string { return "bluesky" }

// Tout ce qui est propre au service : <données>/bluesky (700).
func bsDir() string {
	d := filepath.Join(dataDir(), serviceName())
	_ = os.MkdirAll(d, 0o700)
	_ = os.Chmod(d, 0o700)
	return d
}

// Les avatars téléchargés : JASETTE_MEDIA, sinon <données>/media.
func mediaDir() string {
	if d := os.Getenv("JASETTE_MEDIA"); d != "" {
		return d
	}
	return filepath.Join(dataDir(), "media")
}

// Nom de fichier tiré d'un identifiant, préfixé « bs: » pour ne jamais
// croiser les fichiers des autres ponts.
func hashName(id string) string {
	h := sha256.Sum256([]byte("bs:" + id))
	return hex.EncodeToString(h[:12])
}

// Écrit un fichier en entier (0600), puis le renomme.
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
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Str("pont", "bluesky").Logger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := newPont(ctx)
	emit("hello", map[string]any{"version": 1, "service": serviceName()})
	if err := p.openStore(); err != nil {
		log.Err(err).Msg("base de données")
		emitErr("store", fmt.Errorf("base de données Bluesky illisible : %w", err))
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
			emitErr(cmd.Cmd, fmt.Errorf("le pont Bluesky n'a pas pu ouvrir sa base de données"))
			continue
		}
		switch cmd.Cmd {
		case "login":
			go p.login(strings.TrimSpace(cmd.Number), strings.TrimSpace(cmd.Password))
		case "pair", "pair_phone", "google":
			emitErr(cmd.Cmd, fmt.Errorf("Bluesky se connecte avec un identifiant et un mot de passe d'app (« login »)"))
		case "conversations":
			go p.listConversations(cmd.Folder == "archive")
		case "messages":
			go p.messages(cmd)
		case "send":
			go p.send(cmd)
		case "send_media":
			tmp := cmd.TmpID
			emitErr("send", fmt.Errorf("Bluesky ne permet pas d'envoyer des fichiers en message privé"))
			emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
		case "react":
			go p.react(cmd)
		case "read":
			go p.markRead(cmd)
		case "download":
			emit("media", map[string]any{"media_id": cmd.MediaID, "error": "aucun fichier dans les messages Bluesky"})
		case "new":
			go p.newChat(cmd)
		case "search":
			go p.search(strings.TrimSpace(cmd.Text))
		case "search_messages":
			go p.searchMessages(cmd.Text, int(cmd.Limit))
		case "archive":
			emitErr("archive", fmt.Errorf("Bluesky n'a pas d'archives : Jasette les garde elle-même"))
		case "typing", "history_pin":
			// Bluesky n'a pas d'indicateur de saisie ; history_pin est propre à Messenger.
		case "note_set":
			go p.notes.set(p.ctx, cmd.Text, cmd.Audience, cmd.Source, cmd.Req, p.estPret())
		case "note_clear":
			go p.notes.clear(p.ctx, cmd.Source, cmd.Req, p.estPret())
		case "unpair", "logout":
			go p.unpair()
		default:
			emitErr("cmd", fmt.Errorf("commande inconnue : %q", cmd.Cmd))
		}
	}
	p.shutdown()
}
