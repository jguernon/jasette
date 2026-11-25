package main

// La connexion : les cookies de la fenêtre de connexion, puis la récupération
// des clés XChat du compte avec le code d'accès X Chat.
//
// X garde les clés privées de chaque compte dans un coffre « Juicebox »
// (plusieurs serveurs, chacun n'a qu'une part du secret) : le code d'accès
// à 4 chiffres que l'on choisit dans X Chat ouvre ce coffre. Un nombre limité
// d'essais est permis ; après, X Chat est verrouillé et il faut changer le
// code depuis l'application X. Ce pont ne fait donc qu'un essai par commande
// « login », et ne crée jamais de code (ni de nouvelles clés) : un compte sans
// code doit d'abord activer X Chat sur x.com ou dans l'application.
//
// Adapté de connector/login.go et connector/keybackup.go de mautrix-twitter.

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.mau.fi/mautrix-twitter/pkg/juiceboxgo"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow"
	xcookies "go.mau.fi/mautrix-twitter/pkg/twittermeow/cookies"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/crypto"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/data/response"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/methods"
)

// Les cookies sans lesquels X ne reconnaît pas la session (comme le pont officiel).
var requiredCookies = []string{"auth_token", "ct0"}

// Le nom sous lequel la fenêtre de connexion glisse le code d'accès parmi les
// cookies (l'interface transmet les cookies tels quels au pont).
const pinCookie = "jasette_pin"

// Vérifie la forme des cookies avant de déranger X : auth_token et ct0 sont
// des chaînes hexadécimales.
func checkCookies(c map[string]string) error {
	var missing []string
	for _, name := range requiredCookies {
		if strings.TrimSpace(c[name]) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("cookies manquants : %s (es-tu bien connecté à x.com ?)", strings.Join(missing, ", "))
	}
	isHex := func(s string) bool {
		for _, r := range s {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
				return false
			}
		}
		return true
	}
	if at := strings.TrimSpace(c["auth_token"]); len(at) < 32 || !isHex(at) {
		return errors.New("cookie auth_token invalide : reconnecte-toi à x.com dans la fenêtre de connexion")
	}
	if ct := strings.TrimSpace(c["ct0"]); len(ct) < 32 || !isHex(ct) {
		return errors.New("cookie ct0 invalide : reconnecte-toi à x.com dans la fenêtre de connexion")
	}
	return nil
}

// L'identifiant du compte, tiré du cookie twid (« u=123 », parfois encodé).
func userIDFromTwid(twid string) string {
	twid = strings.Trim(twid, "\"")
	twid = strings.ReplaceAll(twid, "u%3D", "")
	return strings.TrimPrefix(twid, "u=")
}

// Connexion avec les cookies (et le code d'accès) de la fenêtre de connexion.
func (p *Pont) login(cmd Cmd) {
	p.mu.Lock()
	if p.loggingIn {
		p.mu.Unlock()
		emitErr("login", errors.New("une connexion est déjà en cours"))
		return
	}
	p.loggingIn = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.loggingIn = false
		p.mu.Unlock()
	}()

	fail := func(err error) {
		log.Warn().Err(err).Msg("connexion")
		emitErr("login", err)
		emitState("unpaired")
	}
	if len(cmd.Cookies) == 0 {
		fail(errors.New("aucun cookie reçu de la fenêtre de connexion"))
		return
	}
	vals := map[string]string{}
	pin := strings.TrimSpace(cmd.Pin)
	for k, v := range cmd.Cookies {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == pinCookie {
			pin = cmp.Or(pin, v)
			continue
		}
		if k != "" && v != "" {
			vals[k] = v
		}
	}
	if err := checkCookies(vals); err != nil {
		fail(err)
		return
	}
	if pin == "" {
		fail(errors.New("code d'accès X Chat manquant : X chiffre maintenant les messages privés, et ce code (4 chiffres, choisi dans X Chat) est nécessaire pour les lire"))
		return
	}

	log.Info().Strs("cookies", sortedKeys(vals)).Msg("connexion avec les cookies")
	p.disconnect()
	emitState("pairing")
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()

	cli := twittermeow.NewClient(xcookies.NewCookies(vals), nil, log.With().Str("c", "twittermeow").Logger())
	if id := userIDFromTwid(vals["twid"]); id != "" {
		cli.SetCurrentUserID(id)
	}
	profile, err := cli.LoadMessagesPage(ctx)
	if err != nil {
		fail(explain(err))
		return
	}
	userID := strings.TrimSpace(cmp.Or(profile.ID, cli.GetCurrentUserID()))
	if userID == "" {
		fail(errors.New("X n'a pas donné l'identifiant du compte (cookie twid absent) : reconnecte-toi dans la fenêtre de connexion"))
		return
	}
	cli.SetCurrentUserID(userID)

	keys, err := recoverAccountKeys(ctx, cli, userID, pin)
	if err != nil {
		fail(err)
		return
	}

	// Un autre compte qu'avant : on repart d'un cache vide.
	old := loadSessionAny()
	if old == nil || old.UserID != userID {
		p.forgetAccount()
	}
	s := &Session{
		Cookies:           cli.GetCookieString(),
		UserID:            userID,
		Name:              profile.Name,
		Username:          profile.ScreenName,
		Avatar:            profile.AvatarURL,
		SecretKey:         keys.SecretKey,
		SigningKey:        keys.SigningKey,
		SigningKeyVersion: keys.SigningKeyVersion,
	}
	saveSession(s)
	p.mu.Lock()
	p.sess = s
	p.me = userID
	p.mu.Unlock()
	emit("paired", nil)
	p.connect(s)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Les erreurs de X, en français.
func explain(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, twittermeow.ErrUserSuspended), errors.Is(err, twittermeow.ErrAccountSuspended):
		return errors.New("compte X suspendu")
	case errors.Is(err, twittermeow.ErrAccountTemporarilyLocked):
		return errors.New("compte X verrouillé temporairement : ouvre x.com dans un navigateur, puis reconnecte-toi")
	case errors.Is(err, twittermeow.ErrNotActive):
		return errors.New("compte X désactivé")
	case twittermeow.IsAuthError(err):
		return errors.New("X a refusé la session (cookies expirés ?) : reconnecte-toi")
	case errors.Is(err, twittermeow.ErrRatelimitExceeded):
		return errors.New("X limite les requêtes : réessaie dans quelques minutes")
	case errors.Is(err, twittermeow.ErrCloudflareChallenge):
		return errors.New("X demande une vérification anti-robot : ouvre x.com dans un navigateur, puis réessaie")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("X ne répond pas (délai dépassé)")
	}
	msg := err.Error()
	if strings.Contains(msg, "country code not found") || strings.Contains(msg, "guest token not found") {
		return errors.New("X n'a pas reconnu la session (cookies expirés ?) : reconnecte-toi")
	}
	return fmt.Errorf("X : %w", err)
}

// Les erreurs qui demandent une action de l'utilisateur (on arrête de réessayer).
func isAuthError(err error) bool {
	return twittermeow.IsAuthError(err) || errors.Is(err, twittermeow.ErrUserSuspended) ||
		errors.Is(err, twittermeow.ErrNotActive) || errors.Is(err, twittermeow.ErrAccountTemporarilyLocked)
}

// ---- Les clés du compte (Juicebox) ----

type accountKeys struct {
	SecretKey         string `json:"secret_key"`
	SigningKey        string `json:"signing_key"`
	SigningKeyVersion string `json:"signing_key_version"`
}

// Ouvre le coffre Juicebox du compte avec le code d'accès et rend ses clés.
func recoverAccountKeys(ctx context.Context, cli *twittermeow.Client, userID, pin string) (*accountKeys, error) {
	resp, err := cli.GetPublicKeys(ctx, []string{userID})
	if err != nil {
		return nil, fmt.Errorf("clés XChat du compte : %w", explain(err))
	}
	keyData, found, err := selectRegisteredKey(ctx, cli.HTTP, resp)
	if !found {
		return nil, errors.New("X Chat n'est pas activé sur ce compte : choisis un code d'accès dans X Chat (x.com/messages ou l'application X), puis reconnecte-toi")
	}
	if err != nil {
		return nil, explainJuicebox(err)
	}
	configJSON, tokens := juiceboxConfig(keyData.TokenMap)
	jb, err := newJuiceboxClient(configJSON, tokens, cli.HTTP)
	if err != nil {
		return nil, fmt.Errorf("coffre de clés X : %w", err)
	}
	defer jb.Close()
	secret, err := jb.Recover(ctx, []byte(pin), nil)
	if err != nil {
		return nil, explainJuicebox(err)
	}
	keys := parseRecoveredSecret(secret)
	keys.SigningKeyVersion = keyData.PublicKeyWithMetadata.Version
	for name, k := range map[string]string{"secrète": keys.SecretKey, "de signature": keys.SigningKey} {
		if k == "" {
			continue
		}
		if _, err := crypto.ParsePrivateKeyScalar(k); err != nil {
			return nil, fmt.Errorf("le coffre de X a rendu une clé %s invalide : %w", name, err)
		}
	}
	if keys.SecretKey == "" {
		return nil, errors.New("le coffre de X n'a rendu aucune clé")
	}
	log.Info().Str("version", keys.SigningKeyVersion).Bool("signature", keys.SigningKey != "").Msg("clés XChat récupérées")
	return keys, nil
}

func explainJuicebox(err error) error {
	var rec *juiceboxgo.RecoverError
	if errors.As(err, &rec) {
		if rec.GuessesRemaining == nil {
			return errors.New("code d'accès X Chat incorrect")
		}
		if n := *rec.GuessesRemaining; n > 0 {
			return fmt.Errorf("code d'accès X Chat incorrect (encore %d essai(s) avant le verrouillage de X Chat)", n)
		}
		return errors.New("trop d'essais : X Chat est verrouillé. Change ton code d'accès depuis l'application X (« Code oublié ») où X Chat est déjà ouvert, puis reconnecte-toi")
	}
	switch {
	case errors.Is(err, juiceboxgo.ErrNotRegistered):
		return errors.New("aucun code d'accès X Chat enregistré : choisis-en un dans X Chat, puis reconnecte-toi")
	case errors.Is(err, juiceboxgo.ErrRateLimitExceeded):
		return errors.New("trop d'essais de code d'accès : réessaie plus tard")
	case errors.Is(err, juiceboxgo.ErrInvalidAuth):
		return errors.New("le coffre de clés de X a refusé la session : reconnecte-toi")
	case errors.Is(err, juiceboxgo.ErrUpgradeRequired):
		return errors.New("le coffre de clés de X a changé de version : le pont doit être mis à jour")
	case errors.Is(err, juiceboxgo.ErrTransient):
		return errors.New("le coffre de clés de X ne répond pas : réessaie dans un moment")
	}
	return fmt.Errorf("coffre de clés X : %w", err)
}

func juiceboxConfig(tm response.KeyStoreTokenMap) (string, map[string]string) {
	tokens := make(map[string]string, len(tm.TokenMap))
	for _, e := range tm.TokenMap {
		realm, token := strings.ToLower(strings.TrimSpace(e.Key)), strings.TrimSpace(e.Value.Token)
		if realm != "" && token != "" {
			tokens[realm] = token
		}
	}
	return strings.TrimSpace(tm.KeyStoreTokenMapJSON), tokens
}

func newJuiceboxClient(configJSON string, tokens map[string]string, httpClient *http.Client) (*juiceboxgo.Client, error) {
	config, err := juiceboxgo.ConfigurationFromJSON(configJSON)
	if err != nil {
		return nil, fmt.Errorf("configuration illisible : %w", err)
	}
	typed := make(map[string]juiceboxgo.AuthToken, len(tokens))
	for k, v := range tokens {
		typed[strings.ToLower(k)] = juiceboxgo.AuthToken(v)
	}
	return juiceboxgo.NewClient(config, typed, httpClient, log.With().Str("c", "juicebox").Logger())
}

// La clé la plus récente du compte qui a un coffre Juicebox enregistré.
// « found » est faux si le compte n'a aucun coffre (X Chat jamais activé).
func selectRegisteredKey(ctx context.Context, httpClient *http.Client, data *response.GetPublicKeysResponse) (response.PublicKeyWithTokenMap, bool, error) {
	if data == nil || len(data.Data.UserResultsByRestIDs) == 0 {
		return response.PublicKeyWithTokenMap{}, false, nil
	}
	candidates := slices.Clone(data.Data.UserResultsByRestIDs[0].Result.GetPublicKeys.PublicKeysWithTokenMap)
	slices.SortStableFunc(candidates, func(a, b response.PublicKeyWithTokenMap) int {
		return methods.CompareSnowflake(b.PublicKeyWithMetadata.Version, a.PublicKeyWithMetadata.Version)
	})
	found := false
	for _, k := range candidates {
		configJSON, tokens := juiceboxConfig(k.TokenMap)
		if configJSON == "" || len(tokens) == 0 {
			continue
		}
		found = true
		jb, err := newJuiceboxClient(configJSON, tokens, httpClient)
		if err != nil {
			return response.PublicKeyWithTokenMap{}, true, err
		}
		err = jb.CheckRegistration(ctx)
		jb.Close()
		switch {
		case err == nil:
			return k, true, nil
		case errors.Is(err, juiceboxgo.ErrNotRegistered):
			continue
		default:
			return response.PublicKeyWithTokenMap{}, true, err
		}
	}
	if found {
		return response.PublicKeyWithTokenMap{}, true, juiceboxgo.ErrNotRegistered
	}
	return response.PublicKeyWithTokenMap{}, false, nil
}

// Le secret du coffre : du JSON chez certains clients, sinon les deux scalaires
// bout à bout (clé secrète, puis clé de signature).
func parseRecoveredSecret(secret []byte) *accountKeys {
	if len(secret) > 0 && secret[0] == '{' {
		var k accountKeys
		if json.Unmarshal(secret, &k) == nil {
			return &k
		}
	}
	var priv, sign []byte
	switch {
	case len(secret) >= 64 && len(secret)%2 == 0:
		priv, sign = secret[:len(secret)/2], secret[len(secret)/2:]
	case len(secret) > 32:
		priv, sign = secret[:32], secret[32:]
	default:
		priv = secret
	}
	k := &accountKeys{SecretKey: base64.StdEncoding.EncodeToString(priv)}
	if len(sign) > 0 {
		k.SigningKey = base64.StdEncoding.EncodeToString(sign)
	}
	return k
}
