package main

// Les appels XRPC (HTTPS + JSON) au serveur personnel (PDS) du compte, la
// session (jetons accessJwt / refreshJwt, renouvelés au besoin) et la
// connexion.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Le service de messagerie de Bluesky, derrière le PDS.
const chatProxy = "did:web:api.bsky.chat#bsky_chat"

// Le serveur d'entrée de Bluesky (comptes bsky.social, connexion par courriel).
const entryway = "https://bsky.social"

// L'annuaire public (résoudre un nom d'utilisateur sans être connecté).
const publicAPI = "https://public.api.bsky.app"

var httpc = &http.Client{Timeout: 45 * time.Second}

type session struct {
	DID     string `json:"did"`
	Handle  string `json:"handle"`
	PDS     string `json:"pds"`
	Access  string `json:"accessJwt"`
	Refresh string `json:"refreshJwt"`
}

func sessionPath() string { return filepath.Join(bsDir(), "session.json") }

func loadSession() *session {
	b, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil
	}
	var s session
	if json.Unmarshal(b, &s) != nil || s.DID == "" || s.Refresh == "" || s.PDS == "" {
		return nil
	}
	return &s
}

func saveSession(s *session) error {
	b, _ := json.Marshal(s)
	return writeWhole(sessionPath(), b)
}

// xrpcErr : une erreur rendue par le serveur ({"error":…, "message":…}).
type xrpcErr struct {
	Status  int
	Name    string `json:"error"`
	Message string `json:"message"`
}

func (e *xrpcErr) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s : %s", e.Name, e.Message)
	}
	if e.Name != "" {
		return e.Name
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

func isXErr(err error, names ...string) bool {
	var x *xrpcErr
	if !errors.As(err, &x) {
		return false
	}
	for _, n := range names {
		if x.Name == n {
			return true
		}
	}
	return false
}

// rawCall : un appel XRPC sans gestion de session. body nil = GET.
func rawCall(ctx context.Context, host, nsid string, params url.Values, body any, token, proxy string, out any) error {
	u := strings.TrimRight(host, "/") + "/xrpc/" + nsid
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	method := http.MethodGet
	var rd io.Reader
	if body != nil {
		method = http.MethodPost
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Jasette")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if proxy != "" {
		req.Header.Set("atproto-proxy", proxy)
	}
	res, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return err
	}
	if res.StatusCode/100 != 2 {
		e := &xrpcErr{Status: res.StatusCode}
		_ = json.Unmarshal(data, e)
		if e.Name == "" && res.StatusCode == http.StatusTooManyRequests {
			e.Name = "RateLimitExceeded"
		}
		return e
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ---- La session en cours ----

type auth struct {
	mu   sync.Mutex
	sess *session
	// Appelé quand la session est perdue pour de bon (jeton de
	// renouvellement refusé).
	lost func()
}

func (a *auth) get() *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil {
		return nil
	}
	c := *a.sess
	return &c
}

func (a *auth) set(s *session) {
	a.mu.Lock()
	a.sess = s
	a.mu.Unlock()
}

// refresh : de nouveaux jetons (com.atproto.server.refreshSession).
func (a *auth) refresh(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil {
		return errNotConnected
	}
	var r struct {
		Access  string `json:"accessJwt"`
		Refresh string `json:"refreshJwt"`
		Handle  string `json:"handle"`
		DID     string `json:"did"`
	}
	err := rawCall(ctx, a.sess.PDS, "com.atproto.server.refreshSession", nil, struct{}{}, a.sess.Refresh, "", &r)
	if err != nil {
		var x *xrpcErr
		if errors.As(err, &x) && (x.Status == 400 || x.Status == 401) {
			// Jeton expiré ou révoqué (mot de passe d'app supprimé) : il faut
			// se reconnecter.
			a.sess = nil
			_ = os.Remove(sessionPath())
			if a.lost != nil {
				go a.lost()
			}
			return errSessionLost
		}
		return err
	}
	a.sess.Access, a.sess.Refresh = r.Access, r.Refresh
	if r.Handle != "" {
		a.sess.Handle = r.Handle
	}
	if err := saveSession(a.sess); err != nil {
		log.Warn().Err(err).Msg("session")
	}
	return nil
}

var (
	errNotConnected = errors.New("Bluesky n'est pas connecté")
	errSessionLost  = errors.New("la session Bluesky a expiré : reconnectez-vous")
)

// call : un appel XRPC avec la session ; renouvelle les jetons s'ils ont
// expiré, puis réessaie une fois.
func (a *auth) call(ctx context.Context, nsid string, params url.Values, body any, proxy string, out any) error {
	s := a.get()
	if s == nil {
		return errNotConnected
	}
	err := rawCall(ctx, s.PDS, nsid, params, body, s.Access, proxy, out)
	if isXErr(err, "ExpiredToken") || isXErr(err, "InvalidToken") && !isScopeErr(err) {
		if e := a.refresh(ctx); e != nil {
			return e
		}
		s = a.get()
		if s == nil {
			return errSessionLost
		}
		err = rawCall(ctx, s.PDS, nsid, params, body, s.Access, proxy, out)
	}
	return err
}

func (a *auth) chat(ctx context.Context, nsid string, params url.Values, body any, out any) error {
	return a.call(ctx, nsid, params, body, chatProxy, out)
}

// Le mot de passe d'app n'a pas l'accès aux messages privés.
func isScopeErr(err error) bool {
	var x *xrpcErr
	if !errors.As(err, &x) {
		return false
	}
	m := strings.ToLower(x.Message)
	return strings.Contains(m, "scope") || strings.Contains(m, "not permitted") || strings.Contains(m, "privileged")
}

var errScope = errors.New("ce mot de passe d'app n'a pas accès aux messages privés. Créez-en un autre dans Bluesky (Réglages > Confidentialité et sécurité > Mots de passe d'app) en cochant « Autoriser l'accès à vos messages privés »")

// ---- La connexion ----

// pdsOf : le PDS d'un compte, d'après son document DID.
func pdsOf(ctx context.Context, did string) (string, error) {
	var u string
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		u = "https://plc.directory/" + did
	case strings.HasPrefix(did, "did:web:"):
		u = "https://" + strings.TrimPrefix(did, "did:web:") + "/.well-known/did.json"
	default:
		return "", fmt.Errorf("identifiant inconnu : %s", did)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Jasette")
	res, err := httpc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("document DID : HTTP %d", res.StatusCode)
	}
	var doc didDoc
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return "", err
	}
	if p := doc.pds(); p != "" {
		return p, nil
	}
	return "", errors.New("aucun serveur personnel (PDS) dans le document DID")
}

type didDoc struct {
	Service []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Endpoint string `json:"serviceEndpoint"`
	} `json:"service"`
}

func (d didDoc) pds() string {
	for _, s := range d.Service {
		if strings.HasSuffix(s.ID, "#atproto_pds") && strings.HasPrefix(s.Endpoint, "https://") {
			return s.Endpoint
		}
	}
	return ""
}

// resolveHandle : le DID d'un nom d'utilisateur (nom.bsky.social).
func resolveHandle(ctx context.Context, handle string) (string, error) {
	var r struct {
		DID string `json:"did"`
	}
	err := rawCall(ctx, publicAPI, "com.atproto.identity.resolveHandle", url.Values{"handle": {handle}}, nil, "", "", &r)
	if err != nil {
		return "", err
	}
	return r.DID, nil
}

// createSession : la connexion avec un mot de passe d'app. Le PDS du compte
// est trouvé d'après le nom d'utilisateur ; pour un courriel, c'est celui
// de bsky.social (qui renvoie vers le bon PDS dans son document DID).
func createSession(ctx context.Context, ident, password string) (*session, error) {
	ident = strings.TrimPrefix(strings.TrimSpace(ident), "@")
	host := entryway
	if strings.HasPrefix(ident, "did:") {
		if p, err := pdsOf(ctx, ident); err == nil {
			host = p
		}
	} else if !strings.Contains(ident, "@") {
		ident = strings.ToLower(ident)
		if !strings.Contains(ident, ".") {
			ident += ".bsky.social"
		}
		did, err := resolveHandle(ctx, ident)
		if err != nil {
			if isXErr(err, "InvalidRequest", "HandleNotFound") {
				return nil, fmt.Errorf("nom d'utilisateur Bluesky introuvable : %s", ident)
			}
			return nil, fmt.Errorf("impossible de joindre Bluesky : %w", err)
		}
		if p, err := pdsOf(ctx, did); err == nil {
			host = p
		}
	}
	var r struct {
		Access  string          `json:"accessJwt"`
		Refresh string          `json:"refreshJwt"`
		Handle  string          `json:"handle"`
		DID     string          `json:"did"`
		DIDDoc  json.RawMessage `json:"didDoc"`
	}
	err := rawCall(ctx, host, "com.atproto.server.createSession", nil,
		map[string]any{"identifier": ident, "password": password}, "", "", &r)
	if err != nil {
		switch {
		case isXErr(err, "AuthFactorTokenRequired"):
			return nil, errors.New("Bluesky demande un code reçu par courriel : utilisez plutôt un mot de passe d'app (Réglages > Confidentialité et sécurité > Mots de passe d'app)")
		case isXErr(err, "AuthenticationRequired"):
			return nil, errors.New("identifiant ou mot de passe d'app incorrect")
		case isXErr(err, "AccountTakedown", "AccountDeactivated"):
			return nil, errors.New("ce compte Bluesky est désactivé ou suspendu")
		case isXErr(err, "RateLimitExceeded"):
			return nil, errors.New("trop d'essais : attendez quelques minutes avant de réessayer")
		}
		return nil, fmt.Errorf("connexion à Bluesky : %w", err)
	}
	s := &session{DID: r.DID, Handle: r.Handle, PDS: host, Access: r.Access, Refresh: r.Refresh}
	var doc didDoc
	if json.Unmarshal(r.DIDDoc, &doc) == nil {
		if p := doc.pds(); p != "" {
			s.PDS = p
		}
	}
	return s, nil
}
