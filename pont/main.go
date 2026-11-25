// Jasette — le pont Google Messages.
//
// Un petit programme sans fenêtre que l'interface (Rust/GPUI) lance et à qui
// elle parle en JSON, une ligne par message :
//   - entrée standard : les commandes de l'interface ({"cmd": ...}) ;
//   - sortie standard : les réponses et les nouvelles ({"ev": ...}).
//
// Les journaux vont dans la sortie d'erreur.
//
// Il utilise libgm, la bibliothèque de mautrix-gmessages : le même canal que
// messages.google.com/web, associé au téléphone par un code QR. La session est
// gardée dans ~/.local/share/jasette/session.json (permissions 600).
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mimepkg "mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/exhttp"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// ---- Ce qui part vers l'interface ----

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
	Number   string   `json:"number,omitempty"` // le numéro de l'autre (conversation à deux), pour appeler
}

type Message struct {
	ID     string `json:"id"`
	Conv   string `json:"conv"`
	Text   string `json:"text"`
	Media  string `json:"media,omitempty"` // nom ou type d'une pièce jointe
	FromMe bool   `json:"from_me"`
	Sender string `json:"sender"`
	Time   int64  `json:"time"` // millisecondes
	Status string `json:"status"`
	Kind   string `json:"kind"` // "sms", "mms" ou "rcs"
	TmpID  string `json:"tmp_id,omitempty"`
	// Les pièces jointes : l'interface demande à les télécharger (commande « download »).
	Attachments []Attachment `json:"attachments,omitempty"`
	// Les réactions (une par personne), voir reaction.go.
	Reactions []Reaction `json:"reactions,omitempty"`
}

type Attachment struct {
	MediaID string `json:"media_id"`
	Key     []byte `json:"key"` // en base64 dans le JSON
	Mime    string `json:"mime"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	W       int64  `json:"w"`
	H       int64  `json:"h"`
}

type Cmd struct {
	Cmd     string            `json:"cmd"`
	Conv    string            `json:"conv"`
	Text    string            `json:"text"`
	Count   int64             `json:"count"`
	Before  string            `json:"before"`      // id du plus vieux message déjà affiché
	BeforeT int64             `json:"before_time"` // et son heure (millisecondes)
	Msg     string            `json:"msg"`
	TmpID   string            `json:"tmp_id"`
	Cookies map[string]string `json:"cookies"`
	MediaID string            `json:"media_id"`
	Path    string            `json:"path"`
	Number  string            `json:"number"`
	Folder  string            `json:"folder"`
	Key     []byte            `json:"key"`
	Mime    string            `json:"mime"`
	Archive bool              `json:"archived"`
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

// ---- La session ----

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
	case strings.Contains(mime, "pdf"):
		return ".pdf"
	}
	if exts, _ := mimepkg.ExtensionsByType(mime); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

func hashName(id string) string {
	h := sha256.Sum256([]byte(id))
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

// Une pièce jointe, téléchargée et déchiffrée une seule fois.
func (p *Pont) download(cmd Cmd) {
	dir := filepath.Join(mediaDir(), "pieces")
	if matches, _ := filepath.Glob(filepath.Join(dir, hashName(cmd.MediaID)+".*")); len(matches) > 0 {
		for _, m := range matches {
			if !strings.HasSuffix(m, ".tmp") {
				emit("media", map[string]any{"media_id": cmd.MediaID, "path": m})
				return
			}
		}
	}
	cli := p.client()
	if cli == nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 2*time.Minute)
	defer annule()
	r, err := cli.DownloadMedia(ctx, cmd.MediaID, cmd.Key)
	if err != nil {
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
		return
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, 200<<20))
	if err != nil {
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
		return
	}
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(cmd.Mime, data))
	if err := writeWhole(path, data); err != nil {
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
		return
	}
	emit("media", map[string]any{"media_id": cmd.MediaID, "path": path})
}

// Les avatars des contacts (et l'image des groupes), gardés une journée.
func (p *Pont) avatars(convs []*gmproto.Conversation) {
	cli := p.client()
	if cli == nil {
		return
	}
	dir := filepath.Join(mediaDir(), "avatars")
	frais := func(id string) (string, bool) {
		matches, _ := filepath.Glob(filepath.Join(dir, hashName(id)+".*"))
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
				return m, true
			}
		}
		return "", false
	}
	// Une conversation à deux : l'autre participant.
	parPart := map[string][]string{}
	var aDemander []string
	// Un groupe sans photo : les photos de ses (jusqu'à 4) premiers membres,
	// que l'interface assemble en mosaïque.
	type membre struct{ pid, nom, couleur string }
	groupes := map[string][]membre{}
	chemins := map[string]string{}
	for _, c := range convs {
		if c.GetIsGroupChat() {
			url := c.GetGroupAvatarURL()
			if url == "" {
				var ms []membre
				for _, part := range c.GetParticipants() {
					pid := part.GetID().GetParticipantID()
					if part.GetIsMe() || pid == "" {
						continue
					}
					nom := part.GetFullName()
					if nom == "" {
						nom = part.GetFormattedNumber()
					}
					ms = append(ms, membre{pid, nom, part.GetAvatarHexColor()})
					if path, ok := frais(pid); ok {
						chemins[pid] = path
					} else if _, deja := parPart[pid]; !deja {
						aDemander = append(aDemander, pid)
						parPart[pid] = nil
					}
					if len(ms) == 4 {
						break
					}
				}
				groupes[c.GetConversationID()] = ms
				continue
			}
			if path, ok := frais(url); ok {
				emit("avatar", map[string]any{"conv": c.GetConversationID(), "path": path})
				continue
			}
			ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
			data, err := cli.DownloadAvatar(ctx, url)
			annule()
			if err == nil && len(data) > 0 {
				path := filepath.Join(dir, hashName(url)+extension("", data))
				if writeWhole(path, data) == nil {
					emit("avatar", map[string]any{"conv": c.GetConversationID(), "path": path})
				}
			}
			continue
		}
		for _, part := range c.GetParticipants() {
			pid := part.GetID().GetParticipantID()
			if part.GetIsMe() || pid == "" {
				continue
			}
			// Les numéros courts (banques, pubs) n'ont pas de photo.
			if n := strings.TrimLeft(part.GetID().GetNumber(), "+"); n != "" && len(n) < 8 {
				continue
			}
			if path, ok := frais(pid); ok {
				emit("avatar", map[string]any{"conv": c.GetConversationID(), "path": path})
				break
			}
			if _, deja := parPart[pid]; !deja {
				aDemander = append(aDemander, pid)
			}
			parPart[pid] = append(parPart[pid], c.GetConversationID())
			break
		}
	}
	// Ma propre photo, telle que Google Messages la connaît (la fiche « moi » du téléphone).
	moi := ""
	for _, c := range convs {
		for _, part := range c.GetParticipants() {
			if part.GetIsMe() && part.GetID().GetParticipantID() != "" {
				moi = part.GetID().GetParticipantID()
				break
			}
		}
		if moi != "" {
			break
		}
	}
	if moi != "" {
		if path, ok := frais(moi); ok {
			emit("avatar_moi", map[string]any{"service": "google", "path": path})
		} else if _, deja := parPart[moi]; !deja {
			aDemander = append(aDemander, moi)
			parPart[moi] = nil
		}
	}
	for i := 0; i < len(aDemander); i += 10 {
		lot := aDemander[i:min(i+10, len(aDemander))]
		ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
		resp, err := cli.GetParticipantThumbnail(ctx, lot...)
		annule()
		if err != nil {
			log.Warn().Err(err).Msg("avatars")
			return
		}
		for _, t := range resp.GetThumbnail() {
			data := t.GetData().GetImageBuffer()
			if len(data) == 0 {
				continue
			}
			path := filepath.Join(dir, hashName(t.GetIdentifier())+extension("", data))
			if writeWhole(path, data) != nil {
				continue
			}
			chemins[t.GetIdentifier()] = path
			for _, conv := range parPart[t.GetIdentifier()] {
				emit("avatar", map[string]any{"conv": conv, "path": path})
			}
			if t.GetIdentifier() == moi {
				emit("avatar_moi", map[string]any{"service": "google", "path": path})
			}
		}
	}
	for conv, ms := range groupes {
		liste := make([]map[string]any, 0, len(ms))
		for _, m := range ms {
			liste = append(liste, map[string]any{"name": m.nom, "color": m.couleur, "path": chemins[m.pid]})
		}
		emit("avatars_groupe", map[string]any{"conv": conv, "members": liste})
	}
}

func sessionPath() string { return filepath.Join(dataDir(), "session.json") }

func loadSession() *libgm.AuthData {
	b, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil
	}
	ad := libgm.NewAuthData()
	if err := json.Unmarshal(b, ad); err != nil {
		log.Warn().Err(err).Msg("session illisible")
		return nil
	}
	if ad.TachyonAuthToken == nil {
		return nil
	}
	return ad
}

func saveSession(ad *libgm.AuthData) {
	b, err := json.Marshal(ad)
	if err != nil {
		log.Err(err).Msg("session : encodage")
		return
	}
	_ = os.MkdirAll(dataDir(), 0o700)
	tmp := sessionPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, sessionPath())
	}
}

// ---- Le pont ----

type Pont struct {
	mu            sync.Mutex
	cli           *libgm.Client
	ctx           context.Context
	convs         map[string]*gmproto.Conversation
	msgs          map[string]*gmproto.Message  // les messages vus (pour « react »)
	names         map[string]map[string]string // conv -> participant -> nom
	pairing       bool
	chargeEnCours bool
}

func newPont(ctx context.Context) *Pont {
	return &Pont{ctx: ctx, convs: map[string]*gmproto.Conversation{}, names: map[string]map[string]string{}, msgs: map[string]*gmproto.Message{}}
}

func httpSettings() exhttp.ClientSettings {
	return exhttp.SensibleClientSettings
}

func (p *Pont) client() *libgm.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cli
}

// Démarre avec la session gardée, sinon dit à l'interface qu'il faut associer.
func (p *Pont) start() {
	ad := loadSession()
	if ad == nil {
		emit("state", map[string]any{"state": "unpaired"})
		return
	}
	cli := libgm.NewClient(ad, nil, log.With().Str("c", "libgm").Logger(), httpSettings())
	cli.SetEventHandler(p.onEvent)
	p.mu.Lock()
	p.cli = cli
	p.mu.Unlock()
	emit("state", map[string]any{"state": "connecting"})
	if err := cli.FetchConfig(p.ctx); err != nil {
		log.Warn().Err(err).Msg("config")
	}
	if err := cli.Connect(p.ctx); err != nil {
		emitErr("connect", err)
		emit("state", map[string]any{"state": "unpaired"})
		return
	}
	saveSession(cli.AuthData)
	go p.pret()
}

// libgm n'envoie aucun signal « prêt » : une fois connecté, on l'annonce
// nous-mêmes et on charge les conversations (en réessayant, le téléphone
// peut mettre quelques secondes à répondre après une association).
func (p *Pont) pret() {
	p.mu.Lock()
	if p.chargeEnCours {
		p.mu.Unlock()
		return
	}
	p.chargeEnCours = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.chargeEnCours = false
		p.mu.Unlock()
	}()
	emit("state", map[string]any{"state": "ready"})
	for essai := 0; essai < 8; essai++ {
		if essai > 0 {
			time.Sleep(time.Duration(2+essai) * time.Second)
		}
		if p.listConversations(50) {
			return
		}
	}
}

// Association : un code QR à scanner dans Google Messages > Associer un appareil.
func (p *Pont) pair() {
	p.mu.Lock()
	if p.pairing {
		p.mu.Unlock()
		return
	}
	p.pairing = true
	if p.cli != nil {
		p.cli.Disconnect()
	}
	cli := libgm.NewClient(libgm.NewAuthData(), nil, log.With().Str("c", "libgm-pair").Logger(), httpSettings())
	cli.SetEventHandler(p.onEvent)
	p.cli = cli
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.pairing = false
		p.mu.Unlock()
	}()
	if err := cli.FetchConfig(p.ctx); err != nil {
		emitErr("pair", err)
		return
	}
	qr, err := cli.StartLogin(p.ctx)
	if err != nil {
		emitErr("pair", err)
		return
	}
	emit("qr", map[string]any{"data": qr})
	// Un code QR dure 30 s : on le renouvelle, 6 fois au plus.
	for i := 0; i < 6; i++ {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			if cli.AuthData.Mobile != nil {
				return // associé : onEvent(PairSuccessful) s'occupe de la suite
			}
		}
		qr, err = cli.RefreshPhoneRelay()
		if err != nil {
			emitErr("pair", err)
			return
		}
		emit("qr", map[string]any{"data": qr})
	}
	emit("state", map[string]any{"state": "unpaired"})
	emitErr("pair", errors.New("le code QR n'a pas été scanné à temps"))
}

// Association par compte Google : avec les cookies de la fenêtre de connexion,
// le téléphone montre des emojis ; on touche celui que Jasette affiche.
func (p *Pont) pairGoogle(cookies map[string]string) {
	p.mu.Lock()
	if p.pairing {
		p.mu.Unlock()
		return
	}
	p.pairing = true
	if p.cli != nil {
		p.cli.Disconnect()
	}
	ad := libgm.NewAuthData()
	ad.SetCookies(cookies)
	cli := libgm.NewClient(ad, nil, log.With().Str("c", "libgm-google").Logger(), httpSettings())
	cli.SetEventHandler(p.onEvent)
	p.cli = cli
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.pairing = false
		p.mu.Unlock()
	}()

	emit("state", map[string]any{"state": "pairing"})
	if err := cli.FetchConfig(p.ctx); err != nil {
		emitErr("google", fmt.Errorf("connexion refusée par Google : %w", err))
		emit("state", map[string]any{"state": "unpaired"})
		return
	}
	if email := cli.Config.GetDeviceInfo().GetEmail(); email != "" {
		emit("account", map[string]any{"email": email})
	}
	err := cli.DoGaiaPairing(p.ctx, func(emoji string) {
		emit("emoji", map[string]any{"emoji": emoji, "svg": libgm.GetEmojiSVG(emoji)})
	})
	if err != nil {
		msg := err.Error()
		switch {
		case errors.Is(err, libgm.ErrNoDevicesFound):
			msg = "aucun téléphone avec Google Messages trouvé sur ce compte"
		case errors.Is(err, libgm.ErrPhoneNotFound), errors.Is(err, libgm.ErrPhoneInactive):
			msg = "le téléphone ne répond pas : ouvre Google Messages et réessaie"
		case errors.Is(err, libgm.ErrPairingInitTimeout):
			msg = "le téléphone n'a pas répondu à temps"
		case errors.Is(err, events.ErrCallerNoPermission):
			msg = "Google refuse l'association pour ce compte"
		}
		emitErr("google", errors.New(msg))
		emit("state", map[string]any{"state": "unpaired"})
		return
	}
	saveSession(cli.AuthData)
}

func (p *Pont) onEvent(raw any) {
	switch evt := raw.(type) {
	case *events.PairSuccessful:
		emit("paired", nil)
		if cli := p.client(); cli != nil {
			saveSession(cli.AuthData)
		}
		// libgm se reconnecte seul après l'association ; on charge ensuite.
		go func() {
			time.Sleep(3 * time.Second)
			p.pret()
		}()
	case *events.ClientReady:
		if cli := p.client(); cli != nil {
			saveSession(cli.AuthData)
		}
		go p.pret()
	case *events.AuthTokenRefreshed:
		if cli := p.client(); cli != nil {
			saveSession(cli.AuthData)
		}
	case *events.PhoneNotResponding:
		emit("state", map[string]any{"state": "phone_offline"})
	case *events.PhoneRespondingAgain:
		emit("state", map[string]any{"state": "ready"})
	case *events.ListenFatalError:
		emitErr("listen", evt.Error)
		emit("state", map[string]any{"state": "unpaired"})
	case *events.ListenTemporaryError:
		emit("state", map[string]any{"state": "reconnecting"})
	case *events.ListenRecovered:
		emit("state", map[string]any{"state": "ready"})
	case *gmproto.RevokePairData:
		_ = os.Remove(sessionPath())
		emit("state", map[string]any{"state": "unpaired"})
	case *events.GaiaLoggedOut:
		emit("state", map[string]any{"state": "unpaired"})
	case *gmproto.Conversation:
		p.remember(evt)
		emit("conversation", map[string]any{"item": convOut(evt)})
	case *libgm.WrappedMessage:
		if evt.IsOld {
			return
		}
		emit("message", map[string]any{"item": p.msgOut(evt.Message)})
	case *gmproto.TypingData:
		emit("typing", map[string]any{"conv": evt.GetConversationID(), "on": evt.GetType() == gmproto.TypingTypes_STARTED_TYPING})
	default:
		log.Debug().Type("type", raw).Msg("évènement ignoré")
	}
}

func (p *Pont) remember(c *gmproto.Conversation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.convs[c.GetConversationID()] = c
	m := map[string]string{}
	for _, part := range c.GetParticipants() {
		name := part.GetFullName()
		if name == "" {
			name = part.GetFirstName()
		}
		if name == "" {
			name = part.GetFormattedNumber()
		}
		m[part.GetID().GetParticipantID()] = name
	}
	p.names[c.GetConversationID()] = m
}

func convOut(c *gmproto.Conversation) Conversation {
	members := []string{}
	number := ""
	for _, part := range c.GetParticipants() {
		if part.GetIsMe() {
			continue
		}
		if number == "" {
			number = part.GetID().GetNumber()
			if number == "" {
				number = part.GetFormattedNumber()
			}
		}
		n := part.GetFullName()
		if n == "" {
			n = part.GetFormattedNumber()
		}
		members = append(members, n)
	}
	return Conversation{
		ID:       c.GetConversationID(),
		Name:     c.GetName(),
		Preview:  c.GetLatestMessage().GetDisplayContent(),
		FromMe:   c.GetLatestMessage().GetFromMe() != 0,
		Time:     c.GetLastMessageTimestamp() / 1000,
		Unread:   c.GetUnread(),
		Group:    c.GetIsGroupChat(),
		RCS:      c.GetType() == gmproto.ConversationType_RCS,
		Color:    c.GetAvatarHexColor(),
		Members:  members,
		ReadOnly: c.GetReadOnly(),
		Archived: c.GetStatus() == gmproto.ConversationStatus_ARCHIVED || c.GetStatus() == gmproto.ConversationStatus_KEEP_ARCHIVED,
		Number:   map[bool]string{true: "", false: number}[c.GetIsGroupChat()],
	}
}

func (p *Pont) msgOut(m *gmproto.Message) Message {
	p.mu.Lock()
	conv := p.convs[m.GetConversationID()]
	names := p.names[m.GetConversationID()]
	p.keepMsg(m)
	p.mu.Unlock()

	// Comme mautrix : les statuts 1 à 99 sont des envois, 100 à 199 des
	// réceptions. Sinon, le participant qui est « moi » dans la conversation.
	var fromMe bool
	switch st := int(m.GetMessageStatus().GetStatus()); {
	case st >= 1 && st < 100:
		fromMe = true
	case st >= 100 && st < 200:
		fromMe = false
	default:
		fromMe = m.GetSenderParticipant().GetIsMe()
		if !fromMe && conv != nil {
			for _, part := range conv.GetParticipants() {
				if part.GetIsMe() && part.GetID().GetParticipantID() == m.GetParticipantID() {
					fromMe = true
				}
			}
		}
	}
	sender := m.GetSenderParticipant().GetFullName()
	if sender == "" && names != nil {
		sender = names[m.GetParticipantID()]
	}
	text, media := "", ""
	var attachments []Attachment
	for _, info := range m.GetMessageInfo() {
		if c := info.GetMessageContent(); c != nil && c.GetContent() != "" {
			if text != "" {
				text += "\n"
			}
			text += c.GetContent()
		}
		if mc := info.GetMediaContent(); mc != nil {
			if mc.GetMediaID() != "" {
				attachments = append(attachments, Attachment{
					MediaID: mc.GetMediaID(),
					Key:     mc.GetDecryptionKey(),
					Mime:    mc.GetMimeType(),
					Name:    mc.GetMediaName(),
					Size:    mc.GetSize(),
					W:       int64(mc.GetDimensions().GetWidth()),
					H:       int64(mc.GetDimensions().GetHeight()),
				})
			}
			media = mc.GetMediaName()
			if media == "" {
				media = mc.GetMimeType()
			}
			if media == "" {
				media = "pièce jointe"
			}
		}
	}
	if m.Subject != nil && m.GetSubject() != "" {
		text = m.GetSubject() + "\n" + text
	}
	kind := "rcs"
	switch m.GetType() {
	case 1:
		kind = "sms"
	case 2, 3:
		kind = "mms"
	}
	return Message{
		ID:     m.GetMessageID(),
		Conv:   m.GetConversationID(),
		Text:   text,
		Media:  media,
		FromMe: fromMe,
		Sender: sender,
		Time:   m.GetTimestamp() / 1000,
		Status: m.GetMessageStatus().GetStatus().String(),
		Kind:   kind,
		TmpID:  m.GetTmpID(),

		Attachments: attachments,
		Reactions:   reactionsOut(m, conv, names),
	}
}

// Les conversations archivées.
func (p *Pont) listArchive(count int64) {
	cli := p.client()
	if cli == nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 25*time.Second)
	defer annule()
	resp, err := cli.ListConversations(ctx, &gmproto.ListConversationsRequest{
		Count:  count,
		Folder: gmproto.ListConversationsRequest_ARCHIVE,
	})
	if err != nil {
		emitErr("conversations", fmt.Errorf("archives : %w", err))
		return
	}
	items := []Conversation{}
	vues := map[string]bool{}
	for _, c := range resp.GetConversations() {
		if vues[c.GetConversationID()] {
			continue
		}
		vues[c.GetConversationID()] = true
		p.remember(c)
		items = append(items, convOut(c))
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time > items[j].Time })
	emit("conversations", map[string]any{"items": items, "folder": "archive"})
	go p.avatars(resp.GetConversations())
}

func (p *Pont) listConversations(count int64) bool {
	cli := p.client()
	if cli == nil {
		return false
	}
	ctx, annule := context.WithTimeout(p.ctx, 25*time.Second)
	defer annule()
	log.Info().Msg("liste des conversations demandée")
	resp, err := cli.ListConversations(ctx, &gmproto.ListConversationsRequest{
		Count:  count,
		Folder: gmproto.ListConversationsRequest_INBOX,
	})
	if err != nil {
		log.Warn().Err(err).Msg("liste des conversations")
		emitErr("conversations", fmt.Errorf("conversations : %w", err))
		return false
	}
	items := []Conversation{}
	var pourAvatars []*gmproto.Conversation
	vues := map[string]bool{}
	for _, c := range resp.GetConversations() {
		if vues[c.GetConversationID()] {
			continue
		}
		vues[c.GetConversationID()] = true
		p.remember(c)
		pourAvatars = append(pourAvatars, c)
		items = append(items, convOut(c))
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time > items[j].Time })
	log.Info().Int("n", len(items)).Msg("conversations reçues")
	emit("conversations", map[string]any{"items": items})
	go p.avatars(pourAvatars)
	return true
}

func (p *Pont) messages(cmd Cmd) {
	cli := p.client()
	if cli == nil {
		return
	}
	count := cmd.Count
	if count <= 0 {
		count = 50
	}
	var cursor *gmproto.Cursor
	if cmd.Before != "" {
		cursor = &gmproto.Cursor{LastItemID: cmd.Before, LastItemTimestamp: cmd.BeforeT * 1000}
	}
	resp, err := cli.FetchMessages(p.ctx, cmd.Conv, count, cursor)
	if err != nil {
		emitErr("messages", err)
		return
	}
	items := []Message{}
	for _, m := range resp.GetMessages() {
		items = append(items, p.msgOut(m))
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time < items[j].Time })
	emit("messages", map[string]any{
		"conv":  cmd.Conv,
		"items": items,
		"older": cmd.Before != "",
		"more":  resp.GetCursor() != nil && len(items) > 0,
	})
}

func (p *Pont) send(cmd Cmd) {
	cli := p.client()
	if cli == nil {
		return
	}
	p.mu.Lock()
	conv := p.convs[cmd.Conv]
	p.mu.Unlock()
	if conv == nil {
		var err error
		conv, err = cli.GetConversation(p.ctx, cmd.Conv)
		if err != nil {
			emitErr("send", err)
			return
		}
		p.remember(conv)
	}
	tmp := cmd.TmpID
	if tmp == "" {
		tmp = fmt.Sprintf("tmp_%012d", time.Now().UnixNano()%1_000_000_000_000)
	}
	req := &gmproto.SendMessageRequest{
		ConversationID: cmd.Conv,
		MessagePayload: &gmproto.MessagePayload{
			TmpID:          tmp,
			ConversationID: cmd.Conv,
			ParticipantID:  conv.GetDefaultOutgoingID(),
			TmpID2:         tmp,
			MessageInfo: []*gmproto.MessageInfo{{
				Data: &gmproto.MessageInfo_MessageContent{MessageContent: &gmproto.MessageContent{Content: cmd.Text}},
			}},
		},
		SIMPayload: conv.GetSimCard().GetSIMData().GetSIMPayload(),
		TmpID:      tmp,
	}
	resp, err := cli.SendMessage(p.ctx, req)
	if err != nil {
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
		return
	}
	ok := resp.GetStatus() == gmproto.SendMessageResponse_SUCCESS
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": ok, "status": resp.GetStatus().String()})
}

// Envoie un fichier (photo, vidéo, message vocal…) dans une conversation.
func (p *Pont) sendMedia(cmd Cmd) {
	cli := p.client()
	if cli == nil {
		return
	}
	conv := p.conversation(cmd.Conv)
	if conv == nil {
		return
	}
	data, err := os.ReadFile(cmd.Path)
	if err != nil {
		emitErr("send", err)
		return
	}
	mime := cmd.Mime
	if mime == "" {
		mime = mimepkg.TypeByExtension(filepath.Ext(cmd.Path))
	}
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	media, err := cli.UploadMedia(ctx, data, filepath.Base(cmd.Path), mime)
	if err != nil {
		emitErr("send", fmt.Errorf("envoi du fichier : %w", err))
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": cmd.TmpID, "ok": false})
		return
	}
	tmp := cmd.TmpID
	if tmp == "" {
		tmp = fmt.Sprintf("tmp_%012d", time.Now().UnixNano()%1_000_000_000_000)
	}
	req := &gmproto.SendMessageRequest{
		ConversationID: cmd.Conv,
		MessagePayload: &gmproto.MessagePayload{
			TmpID:          tmp,
			ConversationID: cmd.Conv,
			ParticipantID:  conv.GetDefaultOutgoingID(),
			TmpID2:         tmp,
			MessageInfo:    []*gmproto.MessageInfo{{Data: &gmproto.MessageInfo_MediaContent{MediaContent: media}}},
		},
		SIMPayload: conv.GetSimCard().GetSIMData().GetSIMPayload(),
		TmpID:      tmp,
	}
	resp, err := cli.SendMessage(ctx, req)
	if err != nil {
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": resp.GetStatus() == gmproto.SendMessageResponse_SUCCESS})
}

// La conversation connue, sinon demandée au téléphone.
func (p *Pont) conversation(id string) *gmproto.Conversation {
	p.mu.Lock()
	conv := p.convs[id]
	p.mu.Unlock()
	if conv != nil {
		return conv
	}
	cli := p.client()
	if cli == nil {
		return nil
	}
	conv, err := cli.GetConversation(p.ctx, id)
	if err != nil {
		emitErr("send", err)
		return nil
	}
	p.remember(conv)
	return conv
}

// Nouvelle conversation (ou celle qui existe déjà) avec un numéro.
func (p *Pont) newChat(cmd Cmd) {
	cli := p.client()
	if cli == nil || strings.TrimSpace(cmd.Number) == "" {
		return
	}
	num := strings.TrimSpace(cmd.Number)
	req := &gmproto.GetOrCreateConversationRequest{
		Numbers: []*gmproto.ContactNumber{{MysteriousInt: 2, Number: num, Number2: num}},
	}
	resp, err := cli.GetOrCreateConversation(p.ctx, req)
	if err == nil && resp.GetStatus() == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
		vrai, vide := true, ""
		req.CreateRCSGroup, req.RCSGroupName = &vrai, &vide
		resp, err = cli.GetOrCreateConversation(p.ctx, req)
	}
	if err != nil || resp.GetConversation().GetConversationID() == "" {
		if err == nil {
			err = errors.New("numéro refusé par le téléphone")
		}
		emitErr("new", fmt.Errorf("nouvelle conversation : %w", err))
		return
	}
	c := resp.GetConversation()
	p.remember(c)
	emit("conversation", map[string]any{"item": convOut(c), "open": true})
}

func (p *Pont) markRead(cmd Cmd) {
	if cli := p.client(); cli != nil && cmd.Msg != "" {
		if err := cli.MarkRead(p.ctx, cmd.Conv, cmd.Msg); err != nil {
			log.Warn().Err(err).Msg("lu")
		}
	}
}

// archive range une conversation dans les archives (ou l'en sort).
func (p *Pont) archive(cmd Cmd) {
	cli := p.client()
	if cli == nil || cmd.Conv == "" {
		return
	}
	statut := gmproto.ConversationStatus_ACTIVE
	if cmd.Archive {
		statut = gmproto.ConversationStatus_ARCHIVED
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	_, err := cli.UpdateConversation(ctx, &gmproto.UpdateConversationRequest{
		ConversationID: cmd.Conv,
		Data: &gmproto.UpdateConversationRequest_UpdateData{
			UpdateData: &gmproto.UpdateConversationData{
				ConversationID: cmd.Conv,
				Data:           &gmproto.UpdateConversationData_Status{Status: statut},
			},
		},
	})
	if err != nil {
		emitErr("archive", fmt.Errorf("archivage : %w", err))
	}
}

// deleteConv supprime une conversation (sur le téléphone aussi).
func (p *Pont) deleteConv(cmd Cmd) {
	cli := p.client()
	if cli == nil || cmd.Conv == "" {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if err := cli.DeleteConversation(ctx, cmd.Conv, ""); err != nil {
		emitErr("delete", fmt.Errorf("suppression : %w", err))
	}
}

func (p *Pont) unpair() {
	if cli := p.client(); cli != nil {
		_ = cli.Unpair(p.ctx)
		cli.Disconnect()
	}
	_ = os.Remove(sessionPath())
	emit("state", map[string]any{"state": "unpaired"})
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	level := zerolog.InfoLevel
	if os.Getenv("JASETTE_DEBUG") != "" {
		level = zerolog.DebugLevel
	}
	log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, NoColor: true}).Level(level).With().Timestamp().Logger()

	ctx, cancel := context.WithCancel(log.WithContext(context.Background()))
	defer cancel()
	p := newPont(ctx)
	emit("hello", map[string]any{"version": 1})
	go p.start()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		var cmd Cmd
		if err := json.Unmarshal(in.Bytes(), &cmd); err != nil {
			emitErr("cmd", err)
			continue
		}
		switch cmd.Cmd {
		case "pair":
			go p.pair()
		case "google":
			go p.pairGoogle(cmd.Cookies)
		case "conversations":
			n := cmd.Count
			if n <= 0 {
				n = 50
			}
			if cmd.Folder == "archive" {
				go p.listArchive(n)
			} else {
				go p.listConversations(n)
			}
		case "messages":
			go p.messages(cmd)
		case "send":
			go p.send(cmd)
		case "read":
			go p.markRead(cmd)
		case "archive":
			go p.archive(cmd)
		case "delete":
			go p.deleteConv(cmd)
		case "download":
			go p.download(cmd)
		case "send_media":
			go p.sendMedia(cmd)
		case "react":
			go p.sendReaction(cmd)
		case "new":
			go p.newChat(cmd)
		case "search":
			go p.searchContacts(strings.TrimSpace(cmd.Text))
		case "unpair":
			go p.unpair()
		case "quit":
			if cli := p.client(); cli != nil {
				cli.Disconnect()
			}
			return
		default:
			emitErr("cmd", fmt.Errorf("commande inconnue : %q", cmd.Cmd))
		}
	}
	// L'interface est partie : on ferme proprement.
	if cli := p.client(); cli != nil {
		cli.Disconnect()
	}
}
