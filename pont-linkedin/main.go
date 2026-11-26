// Jasette — le pont LinkedIn (messagerie seulement).
//
// Le frère des ponts X, Reddit et Discord : un programme sans fenêtre que
// l'interface lance et à qui elle parle en JSON, une ligne par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur. Jamais de cookie ni de jeton.
//
// Ce pont utilise linkedingo, la bibliothèque de mautrix-linkedin (le pont
// bridgev2 officiel de mautrix ; seule la bibliothèque, sans Matrix ni
// SQLite, donc sans CGO) :
//   - la connexion se fait avec les cookies de linkedin.com (li_at et
//     JSESSIONID, le second servant aussi de jeton CSRF), pris dans la
//     fenêtre de connexion où l'utilisateur s'est connecté lui-même ;
//   - la liste et l'historique viennent des requêtes GraphQL de la
//     messagerie (voyagerMessagingGraphQL), comme le site web ;
//   - le direct vient du flux « realtime » du site (messages, réactions,
//     « écrit », lu), avec un battement par minute, comme le site.
//
// Prudence : LinkedIn surveille l'automatisation. Le pont se présente comme
// le navigateur de la fenêtre de connexion (son User-Agent), espace ses
// requêtes (au moins une seconde, plus du hasard), ne charge qu'une page
// d'historique à la fois (20 messages, sur demande), ne lit la liste que
// par petites pages, n'accuse pas réception des messages, n'envoie jamais
// « écrit… », et ne marque lu, n'envoie ni ne réagit qu'à la demande de
// l'utilisateur. JASETTE_LINKEDIN_LECTURE_SEULE=1 interdit même ces trois-là.
//
// Les événements et les commandes sont ceux des autres ponts. Propre à LinkedIn :
//
//   - la connexion : {"cmd":"login","cookies":{"li_at":"…","JSESSIONID":"…",
//     …,"jasette_user_agent":"…"}} → paired, account, state ready ; ou
//     error (where : login) et state unpaired.
//   - les ids : la partie propre d'un URN de conversation
//     (« urn:li:msg_conversation:(urn:li:fsd_profile:<moi>,<id>) » → « <id> ») ;
//     un message : de même pour « urn:li:msg_message:(…,<id>) ».
//   - les demandes de message (state PENDING) : request:true ; « accept »
//     les accepte ici (la première réponse les accepte chez LinkedIn),
//     « ignore » les cache dans Jasette.
//   - InMail : l'objet en tête du texte. Sales Navigator, Recruiter et les
//     boîtes des pages d'entreprise : pas pris en charge (autres boîtes).
//   - pas d'archives, de suppression, de création de groupe ni de
//     modification : refusés clairement.
//
// Tout l'état est gardé dans <données>/linkedin/ (dossier 700, fichiers 600) :
//   - session.json : les cookies de linkedin.com, l'identité du compte, le
//     User-Agent de la fenêtre de connexion et le jeton de synchronisation ;
//   - convs.json   : les conversations connues (pour ne pas tout relire).
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

	// Une demande de message (pas encore acceptée).
	Request bool `json:"request,omitempty"`
}

type Message struct {
	ID   string `json:"id"`
	Conv string `json:"conv"`
	Text string `json:"text"`
	// Un message de service connu (« deleted »…) : le client montre son
	// texte dans sa langue (PROTOCOLE.md §5.14).
	System string `json:"system,omitempty"`
	Media  string `json:"media,omitempty"`
	FromMe bool   `json:"from_me"`
	Sender string `json:"sender"`
	Time   int64  `json:"time"` // millisecondes
	Status string `json:"status"`
	Kind   string `json:"kind"` // "linkedin" ou "admin"
	TmpID  string `json:"tmp_id,omitempty"`

	Attachments []Attachment `json:"attachments,omitempty"`
	Reactions   []Reaction   `json:"reactions,omitempty"`

	// Le message cité ; le texte cité est aussi en tête de « text ».
	ReplyTo   string `json:"reply_to,omitempty"`
	ReplyText string `json:"reply_text,omitempty"`

	// Groupes : qui a lu ce message (reçus du direct).
	ReadBy []string `json:"read_by,omitempty"`

	// L'id LinkedIn de l'auteur (groupes) : sa photo arrive par « sender_avatar ».
	SenderID string `json:"sender_id,omitempty"`

	// L'URN complet de l'auteur et le texte brut, pour répondre (pas envoyés).
	senderURN string
	corps     string
}

type Attachment struct {
	MediaID string `json:"media_id"` // « <message>#<rang> »
	// De quoi retrouver le fichier (adresse, vignette, message) : du JSON,
	// en base64 dans la trame. Jamais null (le bureau ne lisait plus le fil
	// Reddit quand il l'était).
	Key  []byte `json:"key"`
	Mime string `json:"mime"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	W    int64  `json:"w"`
	H    int64  `json:"h"`
}

type Reaction struct {
	Emoji  string `json:"emoji"`
	Sender string `json:"sender"`
	FromMe bool   `json:"from_me"`
	Actor  string `json:"actor"` // l'id LinkedIn de la personne ("" : inconnue)
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
	Key     []byte            `json:"key"`
	Path    string            `json:"path"`
	Mime    string            `json:"mime"`
	Folder  string            `json:"folder"`
	Full    bool              `json:"full"`
	ReplyTo string            `json:"reply_to"`
	ID      string            `json:"id"`
	Emoji   string            `json:"emoji"`
	Number  string            `json:"number"`
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

// Le service servi par ce pont : toujours LinkedIn.
func serviceName() string { return "linkedin" }

// Tout ce qui est propre au service : <données>/linkedin (700).
func liDir() string {
	d := filepath.Join(dataDir(), serviceName())
	_ = os.MkdirAll(d, 0o700)
	_ = os.Chmod(d, 0o700)
	return d
}

// Les photos et pièces jointes téléchargées : JASETTE_MEDIA, sinon <données>/media.
func mediaDir() string {
	if d := os.Getenv("JASETTE_MEDIA"); d != "" {
		return d
	}
	return filepath.Join(dataDir(), "media")
}

func extension(mime string, data []byte) string {
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
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
	case strings.Contains(mime, "pdf"):
		return ".pdf"
	}
	if exts, _ := mimepkg.ExtensionsByType(mime); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

// Nom de fichier tiré d'un identifiant. Préfixé « li: » pour ne jamais
// croiser les fichiers des autres ponts (même dossier de médias).
func hashName(id string) string {
	h := sha256.Sum256([]byte("li:" + id))
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

// lectureSeule : JASETTE_LINKEDIN_LECTURE_SEULE=1 interdit tout ce qui
// écrit chez LinkedIn (envoi, réaction, lu). Pour un premier essai sur un
// vrai compte.
func lectureSeule() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("JASETTE_LINKEDIN_LECTURE_SEULE")))
	return v != "" && v != "0" && v != "non" && v != "false"
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	level := zerolog.InfoLevel
	if os.Getenv("JASETTE_DEBUG") != "" {
		level = zerolog.DebugLevel
	}
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Str("pont", "linkedin").Logger()

	// linkedingo journalise par le journal du contexte : avertissements
	// seulement (ses lignes de débogage nomment les adresses demandées).
	libLog := log.With().Str("lib", "linkedingo").Logger().Level(zerolog.WarnLevel)
	ctx, cancel := context.WithCancel(libLog.WithContext(context.Background()))
	defer cancel()
	p := newPont(ctx)
	emit("hello", map[string]any{"version": 1, "service": serviceName()})
	if p.lectureSeule {
		log.Info().Msg("mode lecture seule : ni envoi, ni réaction, ni « lu »")
	}
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
		p.dispatch(cmd)
	}
	// « quit » ou l'interface est partie : on ferme proprement.
	p.shutdown()
}

// dispatch : une commande de l'interface (chacune dans sa goroutine).
func (p *Pont) dispatch(cmd Cmd) {
	switch cmd.Cmd {
	case "login":
		go p.login(cmd.Cookies)
	case "pair", "pair_phone", "google", "login_code", "login_password", "api":
		emitErr(cmd.Cmd, fmt.Errorf("LinkedIn se connecte par la fenêtre de linkedin.com (« login » avec ses cookies)"))
	case "conversations":
		go p.listConversations(cmd.Folder == "archive", int(cmd.Count))
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
	case "new":
		go p.newChat(cmd)
	case "create_group":
		emitErr(cmd.Cmd, fmt.Errorf("LinkedIn : créez le groupe dans LinkedIn, il apparaîtra ici"))
	case "edit":
		emitErr("edit", fmt.Errorf("LinkedIn : modifiez le message dans LinkedIn"))
	case "typing":
		// Jasette n'annonce jamais « écrit… » à notre nom (prudence).
	case "presence":
		// LinkedIn : rien à suivre.
	case "note_set", "note_clear":
		emit("note_result", map[string]any{"op": cmd.Cmd, "ok": false, "error": "LinkedIn n'a pas de note ni de statut"})
	case "archive", "delete":
		emitErr(cmd.Cmd, fmt.Errorf("LinkedIn : pas d'archives ni de suppression ici (celles de Jasette s'en chargent)"))
	case "history_pin":
		// Propre aux fils chiffrés de Messenger : rien à faire ici.
	case "unpair", "logout":
		go p.unpair()
	default:
		emitErr("cmd", fmt.Errorf("commande inconnue : %q", cmd.Cmd))
	}
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
