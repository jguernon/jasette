package main

// La connexion à Telegram (gotd) : l'application (api_id, api_hash), la
// session, la connexion par numéro + code (+ mot de passe) ou par code QR,
// et le suivi des mises à jour.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/auth/qrlogin"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type Pont struct {
	ctx context.Context
	db  *sql.DB

	mu     sync.Mutex
	convs  map[string]*convState
	peers  map[string]*peer
	loaded map[string]int64 // fil -> plus vieux message montré à l'interface
	me     int64
	myName string

	// Le client en marche (nil entre deux).
	client    *telegram.Client
	api       *tg.Client
	gaps      *updates.Manager
	runCancel context.CancelFunc
	runGen    int
	appID     int
	appHash   string
	authed    bool
	ready     bool
	authCh    chan struct{}
	clientUp  chan struct{} // fermé quand le client de runGen est joignable
	// Le signal « jeton QR accepté » du client en marche.
	loginTokens qrlogin.LoggedIn

	// La connexion en cours.
	phone     string
	codeHash  string
	pwWait    chan string // le QR attend le mot de passe
	qrCancel  context.CancelFunc
	loginBusy bool

	listDirty     bool
	listCount     int
	archiveWanted bool
	inflight      map[string]int // envois en cours, par fil
	lastDialogs   time.Time

	// La présence des contacts (presence.go).
	presenceOn bool
	presence   map[string]presenceState

	// « Quoi de neuf ? » (notes.go).
	notes *notesPont
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:        ctx,
		convs:      map[string]*convState{},
		peers:      map[string]*peer{},
		loaded:     map[string]int64{},
		inflight:   map[string]int{},
		listCount:  50,
		authCh:     make(chan struct{}, 1),
		presenceOn: presenceEnv(),
		presence:   map[string]presenceState{},
	}
	p.notes = nouvellesNotesTG(p)
	go p.listLoop()
	return p
}

func (p *Pont) tgAPI() *tg.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.authed {
		return nil
	}
	return p.api
}

// ---- L'application (api_id, api_hash) ----

type apiConf struct {
	ID   int    `json:"api_id"`
	Hash string `json:"api_hash"`
}

func apiPath() string { return filepath.Join(tgDir(), "api.json") }

var hexHash = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func (a apiConf) valid() bool { return a.ID > 0 && hexHash.MatchString(a.Hash) }

// L'environnement d'abord, puis le fichier.
func loadAPI() (apiConf, bool) {
	var a apiConf
	if id, err := strconv.Atoi(strings.TrimSpace(os.Getenv("JASETTE_TELEGRAM_API_ID"))); err == nil {
		a = apiConf{ID: id, Hash: strings.TrimSpace(os.Getenv("JASETTE_TELEGRAM_API_HASH"))}
		if a.valid() {
			return a, true
		}
	}
	a = apiConf{}
	if b, err := os.ReadFile(apiPath()); err == nil && json.Unmarshal(b, &a) == nil && a.valid() {
		return a, false
	}
	return apiConf{}, false
}

func parseAPIID(raw json.RawMessage) int {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// Règle l'application si la commande en apporte une. Rend vrai si
// l'application est réglée (avant ou maintenant).
func (p *Pont) setAPI(cmd Cmd, where string) bool {
	cur, fromEnv := loadAPI()
	neuf := apiConf{ID: parseAPIID(cmd.APIID), Hash: strings.ToLower(strings.TrimSpace(cmd.APIHash))}
	if len(cmd.APIID) > 0 || cmd.APIHash != "" {
		switch {
		case fromEnv:
			// L'environnement l'emporte : on ne l'écrase pas.
		case !neuf.valid():
			emitErr(where, errors.New("api_id ou api_hash invalide : l'api_id est un nombre, l'api_hash 32 caractères hexadécimaux (my.telegram.org)"))
			emit("api", map[string]any{"set": cur.valid()})
			return false
		case neuf != cur:
			b, _ := json.Marshal(neuf)
			if err := writeWhole(apiPath(), b); err != nil {
				emitErr(where, fmt.Errorf("api.json : %w", err))
				return false
			}
			cur = neuf
			p.mu.Lock()
			running := p.client != nil
			p.mu.Unlock()
			if running {
				// Une autre application : on repart avec elle.
				p.stopClient()
			}
		}
	}
	emit("api", map[string]any{"set": cur.valid()})
	if !cur.valid() {
		if where != "api" {
			emitErr(where, errors.New("Telegram demande l'api_id et l'api_hash de votre application (my.telegram.org) : entrez-les dans Réglages > Comptes"))
		}
		return false
	}
	p.mu.Lock()
	running := p.client != nil && p.appID == cur.ID && p.appHash == cur.Hash
	p.mu.Unlock()
	if !running {
		p.runClient(cur)
	}
	return true
}

// ---- Démarrage ----

func (p *Pont) start() {
	a, _ := loadAPI()
	emit("api", map[string]any{"set": a.valid()})
	if !a.valid() {
		emitState("unpaired")
		return
	}
	p.runClient(a)
}

func sessionPath() string { return filepath.Join(tgDir(), "session.json") }
func updatesPath() string { return filepath.Join(tgDir(), "updates.json") }

// Lance le client de Telegram (et le relance s'il tombe).
func (p *Pont) runClient(a apiConf) {
	p.mu.Lock()
	if p.runCancel != nil {
		p.runCancel()
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.runCancel = cancel
	p.runGen++
	gen := p.runGen
	p.appID, p.appHash = a.ID, a.Hash
	up := make(chan struct{})
	p.clientUp = up
	p.mu.Unlock()

	d := tg.NewUpdateDispatcher()
	gaps := updates.New(updates.Config{
		Handler:      d,
		Storage:      updates.NewFileStorage(updatesPath()),
		AccessHasher: updates.NewFileStorage(updatesPath() + ".hash"),
		OnTooLong:    func() { go p.refreshDialogs(true) },
		OnChannelTooLong: func(int64) {
			go p.refreshDialogs(true)
		},
	})
	p.handlers(d)
	loggedIn := qrlogin.OnLoginToken(d)
	p.mu.Lock()
	p.loginTokens = loggedIn
	p.mu.Unlock()

	for _, f := range []string{sessionPath(), updatesPath(), updatesPath() + ".hash"} {
		_ = os.Chmod(f, 0o600)
	}
	client := telegram.NewClient(a.ID, a.Hash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: sessionPath()},
		UpdateHandler:  gaps,
		Middlewares:    []telegram.Middleware{hook.UpdateHook(gaps.Handle), hook.AffectedHook(gaps)},
		Device: telegram.DeviceConfig{
			DeviceModel:    "Jasette",
			SystemVersion:  runtime.GOOS,
			AppVersion:     "1.0",
			SystemLangCode: "fr",
			LangCode:       "fr",
		},
		OnConnectionState: func(s telegram.ConnectionState) {
			p.mu.Lock()
			ok := p.runGen == gen && p.authed
			wasReady := p.ready
			if ok && s == telegram.ConnectionStateDisconnected {
				p.ready = false
			}
			p.mu.Unlock()
			if ok && s == telegram.ConnectionStateDisconnected && wasReady {
				emitState("reconnecting")
			}
			if ok && s == telegram.ConnectionStateReady && !wasReady {
				go p.markReadyAgain(gen)
			}
		},
		OnAuthLost: func(ctx context.Context, err error) error {
			log.Warn().Err(err).Msg("session perdue")
			go p.authLost(gen)
			return nil
		},
	})

	go func() {
		for essai := 0; ctx.Err() == nil; essai++ {
			err := client.Run(ctx, func(ctx context.Context) error {
				p.mu.Lock()
				if p.runGen != gen {
					p.mu.Unlock()
					return nil
				}
				p.client, p.api, p.gaps = client, client.API(), gaps
				select {
				case <-up:
				default:
					close(up)
				}
				p.mu.Unlock()
				essai = 0
				return p.session(ctx, gen, client, gaps)
			})
			if ctx.Err() != nil {
				break
			}
			log.Warn().Err(err).Int("essai", essai).Msg("client Telegram")
			p.mu.Lock()
			was := p.authed
			p.ready = false
			p.mu.Unlock()
			if was {
				emitState("reconnecting")
			}
			select {
			case <-time.After(min(time.Duration(2<<min(essai, 6))*time.Second, 120*time.Second)):
			case <-ctx.Done():
			}
		}
		p.mu.Lock()
		if p.runGen == gen {
			p.client, p.api, p.gaps = nil, nil, nil
			p.ready = false
		}
		p.mu.Unlock()
	}()
}

func (p *Pont) stopClient() {
	p.mu.Lock()
	if p.runCancel != nil {
		p.runCancel()
		p.runCancel = nil
	}
	p.runGen++
	p.client, p.api, p.gaps = nil, nil, nil
	p.authed, p.ready = false, false
	p.mu.Unlock()
}

// Attend que le client soit joignable (connexion en cours).
func (p *Pont) waitClient(ctx context.Context) (*telegram.Client, error) {
	p.mu.Lock()
	up := p.clientUp
	p.mu.Unlock()
	if up == nil {
		return nil, errors.New("Telegram : application non réglée (api_id, api_hash)")
	}
	select {
	case <-up:
	case <-ctx.Done():
		return nil, errors.New("Telegram ne répond pas (délai dépassé)")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		return nil, errors.New("Telegram n'est pas joignable")
	}
	return p.client, nil
}

// La vie d'une connexion : attendre d'être connecté, puis suivre les mises à jour.
func (p *Pont) session(ctx context.Context, gen int, client *telegram.Client, gaps *updates.Manager) error {
	st, err := client.Auth().Status(ctx)
	if err != nil {
		return fmt.Errorf("état de la session : %w", err)
	}
	if !st.Authorized {
		p.mu.Lock()
		p.authed = false
		busy := p.loginBusy
		p.mu.Unlock()
		if !busy {
			emitState("unpaired")
		}
		select {
		case <-p.authCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	self, err := client.Self(ctx)
	if err != nil {
		return fmt.Errorf("mon compte : %w", err)
	}
	p.onSelf(self)
	p.mu.Lock()
	p.authed = true
	p.mu.Unlock()
	emitState("connecting")
	p.emitAccount()
	return gaps.Run(ctx, client.API(), self.ID, updates.AuthOptions{
		OnStart: func(ctx context.Context) {
			p.mu.Lock()
			ok := p.runGen == gen
			if ok {
				p.ready = true
			}
			p.mu.Unlock()
			if ok {
				emitState("ready")
				p.emitList()
				go p.notes.annoncer()
				go p.afterReady(ctx)
			}
		},
	})
}

// Le client s'est reconnecté (après une coupure) : de nouveau prêt.
func (p *Pont) markReadyAgain(gen int) {
	p.mu.Lock()
	ok := p.runGen == gen && p.authed && !p.ready && p.gaps != nil
	if ok {
		p.ready = true
	}
	p.mu.Unlock()
	if ok {
		emitState("ready")
	}
}

func (p *Pont) onSelf(u *tg.User) {
	p.mu.Lock()
	if p.me != 0 && p.me != u.ID {
		p.mu.Unlock()
		p.forgetAccount()
		p.mu.Lock()
	}
	p.me = u.ID
	p.myName = userName(u)
	p.noteUser(u)
	p.mu.Unlock()
	var old int64
	if p.loadKV("account", &old) && old != 0 && old != u.ID {
		p.forgetAccount()
	}
	p.saveKV("account", u.ID)
}

func (p *Pont) emitAccount() {
	p.mu.Lock()
	me, name := p.me, p.myName
	var phone string
	if c := p.peers[keyOf('u', me)]; c != nil {
		phone = c.Phone
	}
	p.mu.Unlock()
	if me == 0 {
		return
	}
	if name == "" && phone != "" {
		name = "+" + phone
	}
	emit("account", map[string]any{"service": serviceName(), "name": firstOf(name, "Telegram"), "id": strconv.FormatInt(me, 10), "email": ""})
}

// Une fois prêt : les fils, mon avatar, puis les fils de temps en temps.
func (p *Pont) afterReady(ctx context.Context) {
	p.refreshDialogs(false)
	go p.myAvatarDownload()
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			p.refreshDialogs(false)
		case <-ctx.Done():
			return
		}
	}
}

// La session a été fermée ailleurs (Appareils > Terminer la session).
func (p *Pont) authLost(gen int) {
	p.mu.Lock()
	if p.runGen != gen {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.stopClient()
	_ = os.Remove(sessionPath())
	emitErr("listen", errors.New("Telegram a fermé la session de Jasette : reconnectez-vous"))
	emitState("unpaired")
	if a, _ := loadAPI(); a.valid() {
		p.runClient(a)
	}
}

// ---- La connexion par numéro ----

var phoneChars = regexp.MustCompile(`[\s\-().]`)

func (p *Pont) beginLogin() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.authed {
		return false
	}
	p.loginBusy = true
	return true
}

func explainAuth(err error) error {
	switch {
	case tgerr.Is(err, "PHONE_NUMBER_INVALID"):
		return errors.New("numéro invalide : entrez-le avec l'indicatif du pays (ex. : +1 514 555 1234)")
	case tgerr.Is(err, "PHONE_NUMBER_BANNED"):
		return errors.New("ce numéro est banni de Telegram")
	case tgerr.Is(err, "PHONE_CODE_INVALID"):
		return errors.New("code invalide : vérifiez-le et réessayez")
	case tgerr.Is(err, "PHONE_CODE_EXPIRED"):
		return errors.New("code expiré : demandez-en un nouveau")
	case tgerr.Is(err, "PASSWORD_HASH_INVALID"), errors.Is(err, auth.ErrPasswordInvalid):
		return errors.New("mot de passe incorrect")
	case tgerr.Is(err, "API_ID_INVALID"), tgerr.Is(err, "API_ID_PUBLISHED_FLOOD"):
		return errors.New("Telegram refuse cet api_id / api_hash : vérifiez-les sur my.telegram.org")
	case tgerr.Is(err, "AUTH_RESTART"):
		return errors.New("Telegram demande de recommencer la connexion")
	}
	if d, ok := tgerr.AsFloodWait(err); ok {
		return fmt.Errorf("Telegram limite les essais : réessayez dans %s", d.Round(time.Second))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("Telegram ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("Telegram : %w", err)
}

func (p *Pont) loginPhone(cmd Cmd) {
	if !p.setAPI(cmd, "login") {
		emitState("unpaired")
		return
	}
	if !p.beginLogin() {
		emitErr("login", errors.New("Telegram est déjà connecté"))
		return
	}
	number := phoneChars.ReplaceAllString(strings.TrimSpace(cmd.Number), "")
	if !strings.HasPrefix(number, "+") {
		number = "+" + number
	}
	if len(number) < 8 {
		emitErr("login", errors.New("entrez le numéro complet, avec l'indicatif du pays"))
		return
	}
	p.cancelQR()
	ctx, annule := context.WithTimeout(p.ctx, 60*time.Second)
	defer annule()
	client, err := p.waitClient(ctx)
	if err != nil {
		emitErr("login", err)
		return
	}
	emitState("pairing")
	sent, err := client.Auth().SendCode(ctx, number, auth.SendCodeOptions{})
	if err != nil {
		log.Warn().Err(err).Msg("envoi du code")
		emitErr("login", explainAuth(err))
		emitState("unpaired")
		return
	}
	sc, ok := sent.(*tg.AuthSentCode)
	if !ok {
		// Déjà connecté (auth.sentCodeSuccess) : rare, mais possible.
		if s, ok := sent.(*tg.AuthSentCodeSuccess); ok {
			a, _ := s.Authorization.(*tg.AuthAuthorization)
			p.loggedIn(a)
			return
		}
		emitErr("login", errors.New("réponse inattendue de Telegram"))
		emitState("unpaired")
		return
	}
	p.mu.Lock()
	p.phone, p.codeHash = number, sc.PhoneCodeHash
	p.mu.Unlock()
	via := "app"
	switch sc.Type.(type) {
	case *tg.AuthSentCodeTypeSMS, *tg.AuthSentCodeTypeSMSWord, *tg.AuthSentCodeTypeSMSPhrase:
		via = "sms"
	case *tg.AuthSentCodeTypeCall, *tg.AuthSentCodeTypeFlashCall, *tg.AuthSentCodeTypeMissedCall:
		via = "call"
	case *tg.AuthSentCodeTypeEmailCode, *tg.AuthSentCodeTypeSetUpEmailRequired:
		via = "email"
	}
	emit("login_step", map[string]any{"step": "code", "via": via})
}

func (p *Pont) loginCode(code string) {
	code = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
	p.mu.Lock()
	phone, hash, client := p.phone, p.codeHash, p.client
	p.mu.Unlock()
	if phone == "" || hash == "" || client == nil {
		emitErr("login_code", errors.New("demandez d'abord un code (numéro de téléphone)"))
		return
	}
	if code == "" {
		emitErr("login_code", errors.New("entrez le code reçu dans Telegram"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 60*time.Second)
	defer annule()
	a, err := client.Auth().SignIn(ctx, phone, code, hash)
	switch {
	case errors.Is(err, auth.ErrPasswordAuthNeeded):
		p.askPasswordStep(ctx, client.API())
		return
	case err != nil:
		var su *auth.SignUpRequired
		if errors.As(err, &su) {
			emitErr("login_code", errors.New("ce numéro n'a pas de compte Telegram : créez-le d'abord dans l'app Telegram"))
			emitState("unpaired")
			return
		}
		log.Warn().Err(err).Msg("code")
		emitErr("login_code", explainAuth(err))
		if tgerr.Is(err, "PHONE_CODE_EXPIRED") || tgerr.Is(err, "AUTH_RESTART") {
			emitState("unpaired")
		} else {
			emit("login_step", map[string]any{"step": "code", "retry": true})
		}
		return
	}
	p.loggedIn(a)
}

// Le mot de passe (vérification en deux étapes) : l'indice, s'il y en a un.
func (p *Pont) askPasswordStep(ctx context.Context, api *tg.Client) {
	ev := map[string]any{"step": "password"}
	if pw, err := api.AccountGetPassword(ctx); err == nil && pw.Hint != "" {
		ev["hint"] = pw.Hint
	}
	emit("login_step", ev)
}

func (p *Pont) loginPassword(pw string) {
	p.mu.Lock()
	wait, client := p.pwWait, p.client
	p.mu.Unlock()
	if pw == "" {
		emitErr("login_password", errors.New("entrez votre mot de passe Telegram"))
		return
	}
	if wait != nil {
		// Le QR attend ce mot de passe.
		select {
		case wait <- pw:
		default:
		}
		return
	}
	if client == nil {
		emitErr("login_password", errors.New("Telegram n'est pas joignable"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 60*time.Second)
	defer annule()
	a, err := client.Auth().Password(ctx, pw)
	if err != nil {
		log.Warn().Err(err).Msg("mot de passe")
		emitErr("login_password", explainAuth(err))
		emit("login_step", map[string]any{"step": "password", "retry": true})
		return
	}
	p.loggedIn(a)
}

// ---- La connexion par code QR ----

func (p *Pont) cancelQR() {
	p.mu.Lock()
	if p.qrCancel != nil {
		p.qrCancel()
		p.qrCancel = nil
	}
	p.mu.Unlock()
}

func (p *Pont) pairQR(cmd Cmd) {
	if !p.setAPI(cmd, "pair") {
		emitState("unpaired")
		return
	}
	if !p.beginLogin() {
		emitErr("pair", errors.New("Telegram est déjà connecté"))
		return
	}
	p.cancelQR()
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Minute)
	pwc := make(chan string, 1)
	p.mu.Lock()
	p.qrCancel = cancel
	p.pwWait = pwc
	p.mu.Unlock()
	defer func() {
		cancel()
		p.mu.Lock()
		if p.pwWait == pwc {
			p.pwWait = nil
		}
		p.mu.Unlock()
	}()
	client, err := p.waitClient(ctx)
	if err != nil {
		emitErr("pair", err)
		emitState("unpaired")
		return
	}
	emitState("pairing")
	p.mu.Lock()
	appID, appHash := p.appID, p.appHash
	p.mu.Unlock()
	api := client.API()
	qr := qrlogin.NewQR(api, appID, appHash, qrlogin.Options{
		Migrate: client.MigrateTo,
		Password: func(ctx context.Context) (string, error) {
			p.askPasswordStep(ctx, api)
			select {
			case pw := <-pwc:
				return pw, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	})
	a, err := p.qrAuth(ctx, qr)
	if err != nil {
		if p.ctx.Err() != nil {
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Warn().Err(err).Msg("code QR")
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("le code QR n'a pas été scanné à temps : recommencez")
		} else {
			err = explainAuth(err)
		}
		emitErr("pair", err)
		emitState("unpaired")
		return
	}
	p.loggedIn(a)
}

// QR.Auth avec mot de passe à réessayer : un mot de passe faux redemande.
func (p *Pont) qrAuth(ctx context.Context, qr qrlogin.QR) (*tg.AuthAuthorization, error) {
	p.mu.Lock()
	tokens := p.loginTokens
	p.mu.Unlock()
	for {
		a, err := qr.Auth(ctx, tokens, func(ctx context.Context, token qrlogin.Token) error {
			emit("qr", map[string]any{"code": token.URL(), "data": token.URL()})
			return nil
		})
		if err != nil && (tgerr.Is(err, "PASSWORD_HASH_INVALID") || errors.Is(err, auth.ErrPasswordInvalid)) {
			emitErr("login_password", explainAuth(err))
			continue
		}
		return a, err
	}
}

// Connecté : la session est gardée par gotd ; on passe au suivi.
func (p *Pont) loggedIn(a *tg.AuthAuthorization) {
	p.mu.Lock()
	p.phone, p.codeHash = "", ""
	p.loginBusy = false
	p.mu.Unlock()
	_ = os.Chmod(sessionPath(), 0o600)
	if a != nil {
		if u, ok := a.User.(*tg.User); ok {
			p.onSelf(u)
		}
	}
	log.Info().Msg("connecté à Telegram")
	emit("paired", nil)
	select {
	case p.authCh <- struct{}{}:
	default:
	}
}

// ---- Déconnexion ----

// Oublie le compte : cache.
func (p *Pont) forgetAccount() {
	p.mu.Lock()
	p.convs = map[string]*convState{}
	p.peers = map[string]*peer{}
	p.loaded = map[string]int64{}
	p.myName = ""
	p.mu.Unlock()
	if err := p.wipeStore(); err != nil {
		log.Warn().Err(err).Msg("effacement du cache")
	}
}

// Ferme la session de Jasette chez Telegram (comme Appareils > Terminer),
// puis oublie tout.
func (p *Pont) unpair() {
	p.cancelQR()
	api := p.tgAPI()
	if api != nil {
		ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
		if _, err := api.AuthLogOut(ctx); err != nil {
			log.Warn().Err(err).Msg("fermeture de la session")
		}
		annule()
	}
	p.stopClient()
	for _, f := range []string{sessionPath(), updatesPath(), updatesPath() + ".hash"} {
		_ = os.Remove(f)
	}
	p.forgetAccount()
	p.mu.Lock()
	p.me = 0
	p.loginBusy = false
	p.phone, p.codeHash = "", ""
	p.mu.Unlock()
	emitState("unpaired")
	if a, _ := loadAPI(); a.valid() {
		p.runClient(a)
	}
}

func (p *Pont) shutdown() {
	p.cancelQR()
	p.stopClient()
	if p.db != nil {
		_ = p.db.Close()
	}
}

func firstOf(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
