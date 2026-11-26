// Jasette — le pont Telegram.
//
// Le frère des ponts WhatsApp (../pont-wa) et Signal (../pont-signal) : un
// programme sans fenêtre que l'interface lance et à qui elle parle en JSON,
// une ligne par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur.
//
// Ce pont utilise gotd/td (MTProto en Go pur, la bibliothèque du
// mautrix-telegram récent) : Jasette est une session de plus du compte
// Telegram, comme Telegram Desktop. Il n'y a ni téléphone à garder allumé ni
// clés à recevoir : l'historique est sur les serveurs de Telegram.
//
// Les événements et les commandes sont ceux des autres ponts : l'interface
// utilise le même code pour tous. Propre à Telegram :
//
//   - l'application : Telegram exige un api_id et un api_hash, ceux d'une
//     application enregistrée sur https://my.telegram.org (« API development
//     tools »). Le pont les prend dans JASETTE_TELEGRAM_API_ID et
//     JASETTE_TELEGRAM_API_HASH, sinon dans <données>/telegram/api.json
//     (0600), écrit quand une commande de connexion les apporte
//     (« api_id », « api_hash »). Aucune valeur par défaut n'est fournie :
//     surtout pas celles, publiques, d'une autre application.
//   - la connexion :
//       {"cmd":"login","number":"+15145551234","api_id":123,"api_hash":"…"}
//         → {"ev":"login_step","step":"code","via":"app|sms|…"}
//       {"cmd":"login_code","code":"12345"}
//         → ready, ou {"ev":"login_step","step":"password","hint":"…"}
//       {"cmd":"login_password","password":"…"}
//       {"cmd":"pair","api_id":123,"api_hash":"…"} (code QR : Paramètres >
//         Appareils > Connecter un appareil sur le téléphone)
//         → des {"ev":"qr","code":"tg://login?token=…","data":"…"} renouvelés,
//           puis peut-être le mot de passe (login_step password).
//     Réussite : paired, account, state ready. Échec : error (where : login,
//     login_code, login_password ou pair) ; l'étape reste ouverte quand on
//     peut réessayer (code ou mot de passe faux).
//   - « archive » (conv, archived) : les archives de Telegram (dossier 1).
//   - « api » : {"ev":"api","set":true|false} dit si l'application est réglée.
//
// Aucune commande ne marque un message comme lu, sauf « read ».
//
// Tout l'état est gardé dans <données>/telegram/ (dossier 700, fichiers 600) :
//   - session.json : la clé de session MTProto (gotd) ;
//   - updates.json : où en sont les mises à jour (pour rattraper au retour) ;
//   - telegram.db  : SQLite (pur Go), le cache de Jasette (fils, personnes,
//     messages) ;
//   - api.json     : api_id et api_hash, s'ils ne viennent pas de
//     l'environnement.
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
	// Le sens qu'il a pour Messenger (fil « secret » à part) : toujours faux
	// ici (les conversations secrètes de Telegram ne passent pas par l'API).
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
	Kind   string `json:"kind"` // "telegram" ou "admin"
	TmpID  string `json:"tmp_id,omitempty"`

	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`

	// Le message cité (comme WhatsApp) ; le texte cité est aussi en tête de « text ».
	ReplyTo   string `json:"reply_to,omitempty"`
	ReplyText string `json:"reply_text,omitempty"`

	// L'aperçu du lien, tel que Telegram l'envoie (apercu.go).
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
	// Le fil de la personne (« u123 », ou « c123 » pour un canal) ; vide
	// quand Telegram ne donne que le nombre.
	Actor string `json:"actor"`
}

type Cmd struct {
	Cmd      string `json:"cmd"`
	Conv     string `json:"conv"`
	Text     string `json:"text"`
	Count    int64  `json:"count"`
	Limit    int64  `json:"limit"` // search_messages : nombre de résultats au plus
	Before   string `json:"before"`
	BeforeT  int64  `json:"before_time"`
	Msg      string `json:"msg"`
	TmpID    string `json:"tmp_id"`
	MediaID  string `json:"media_id"`
	Path     string `json:"path"`
	Number   string `json:"number"`
	Folder   string `json:"folder"`
	Key      []byte `json:"key"`
	Mime     string `json:"mime"`
	Archived bool   `json:"archived"`
	On       bool   `json:"on"`
	ReplyTo  string `json:"reply_to"`
	// create_group : les clés (u<n>) ou numéros des membres et le nom (facultatif).
	Members []string `json:"members"`
	Name    string   `json:"name"`
	// react : le message visé et l'emoji (vide = retirer ma réaction).
	ID    string `json:"id"`
	Emoji string `json:"emoji"`
	// Connexion.
	Code     string          `json:"code"`
	Password string          `json:"password"`
	APIID    json.RawMessage `json:"api_id"` // nombre ou texte
	APIHash  string          `json:"api_hash"`
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

// Le service servi par ce pont : toujours Telegram.
func serviceName() string { return "telegram" }

// Tout ce qui est propre au service : <données>/telegram (700).
func tgDir() string {
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

// Nom de fichier tiré d'un identifiant. Préfixé « tg: » pour ne jamais
// croiser les fichiers des autres ponts (même dossier de médias).
func hashName(id string) string {
	h := sha256.Sum256([]byte("tg:" + id))
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
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Str("pont", "telegram").Logger()
	zerolog.DefaultContextLogger = &log

	ctx, cancel := context.WithCancel(log.WithContext(context.Background()))
	defer cancel()
	p := newPont(ctx)
	emit("hello", map[string]any{"version": 1, "service": serviceName()})
	if err := p.openStore(); err != nil {
		log.Err(err).Msg("base de données")
		emitErr("store", fmt.Errorf("base de données Telegram illisible : %w", err))
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
			emitErr(cmd.Cmd, fmt.Errorf("le pont Telegram n'a pas pu ouvrir sa base de données"))
			continue
		}
		switch cmd.Cmd {
		case "login":
			go p.loginPhone(cmd)
		case "login_code":
			go p.loginCode(strings.TrimSpace(cmd.Code))
		case "login_password":
			go p.loginPassword(cmd.Password)
		case "pair":
			go p.pairQR(cmd)
		case "api":
			// Règle l'application seulement (sans se connecter).
			go p.setAPI(cmd, "api")
		case "pair_phone", "google":
			emitErr(cmd.Cmd, fmt.Errorf("Telegram se connecte par numéro (« login ») ou code QR (« pair »)"))
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
		case "react":
			go p.sendReaction(cmd)
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
		case "archive":
			go p.archive(cmd)
		case "typing":
			go p.sendTyping(cmd)
		case "presence":
			p.setPresence(cmd.On)
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
