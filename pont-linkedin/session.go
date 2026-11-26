package main

// La session (session.json, 0600), l'accès à LinkedIn (linkedingo derrière
// une interface, pour les essais) et l'espacement des requêtes.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.mau.fi/mautrix-linkedin/pkg/linkedingo"
)

// ---- session.json ----

type sessionFile struct {
	Cookies      *linkedingo.StringCookieJar `json:"cookies"`
	Profile      string                      `json:"profile"` // l'id fsd_profile du compte (« ACoAA… »)
	Nom          string                      `json:"nom"`
	Photo        string                      `json:"photo,omitempty"`
	UserAgent    string                      `json:"user_agent,omitempty"`
	PageInstance string                      `json:"page_instance,omitempty"`
	XLITrack     string                      `json:"x_li_track,omitempty"`
	SyncToken    string                      `json:"sync_token,omitempty"`
}

func sessionPath() string { return filepath.Join(liDir(), "session.json") }

func loadSession() (*sessionFile, error) {
	b, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(sessionPath(), 0o600)
	var s sessionFile
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Cookies == nil || s.Profile == "" || s.Cookies.GetCookie("li_at") == "" {
		return nil, os.ErrNotExist
	}
	return &s, nil
}

func saveSession(s *sessionFile) error {
	if s == nil {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeWhole(sessionPath(), b)
}

func removeSession() {
	_ = os.Remove(sessionPath())
	_ = os.Remove(convsPath())
}

// ---- Les cookies de la fenêtre de connexion ----

var nomCookie = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_|~-]{1,64}$`)

// cookiesValides : les cookies de linkedin.com tels que la fenêtre les rend,
// sans les champs propres à Jasette (jasette_*). Rend aussi le User-Agent
// de la fenêtre.
func cookiesValides(raw map[string]string) (map[string]string, string) {
	c := map[string]string{}
	ua := ""
	for k, v := range raw {
		v = strings.TrimSpace(v)
		switch {
		case k == "jasette_user_agent":
			if len(v) < 400 && !strings.ContainsAny(v, "\r\n") {
				ua = v
			}
		case strings.HasPrefix(k, "jasette_"):
		case nomCookie.MatchString(k) && v != "" && len(v) < 4096 && !strings.ContainsAny(v, ";\r\n"):
			c[k] = v
		}
	}
	return c, ua
}

// jarDe : le bocal de cookies de linkedingo (www.linkedin.com).
func jarDe(c map[string]string) (*linkedingo.StringCookieJar, error) {
	var parts []string
	for k, v := range c {
		parts = append(parts, k+"="+v)
	}
	if len(parts) == 0 {
		return nil, errors.New("aucun cookie")
	}
	return linkedingo.NewJarFromCookieHeader(strings.Join(parts, "; "))
}

// enTetesNavigateur : les en-têtes « navigateur » à partir du User-Agent de
// la fenêtre de connexion. Un Chrome (Android) garde des Sec-Ch-* cohérents ;
// WebKit (la fenêtre du bureau) n'en envoie pas.
func enTetesNavigateur(ua string) http.Header {
	if ua == "" {
		return nil
	}
	h := http.Header{"User-Agent": {ua}}
	m := regexp.MustCompile(`Chrome/(\d+)`).FindStringSubmatch(ua)
	if m == nil || strings.Contains(ua, "Edg/") {
		return h
	}
	v := m[1]
	h.Set("Sec-Ch-Ua", `"Chromium";v="`+v+`", "Not-A.Brand";v="99"`)
	if strings.Contains(ua, "Android") {
		h.Set("Sec-Ch-Ua-Platform", `"Android"`)
	} else {
		h.Set("Sec-Ch-Ua-Platform", `"Linux"`)
	}
	if strings.Contains(ua, "Mobile") {
		h.Set("Sec-Ch-Ua-Mobile", "?1")
	} else {
		h.Set("Sec-Ch-Ua-Mobile", "?0")
	}
	return h
}

// pageInstanceNeuve : l'en-tête X-LI-Page-Instance, propre à ce compte
// (pas celui, fixe, que linkedingo prend par défaut).
func pageInstanceNeuve() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.IntN(256))
	}
	return "urn:li:page:d_flagship3_messaging_conversation_detail;" + base64.StdEncoding.EncodeToString(b)
}

// ---- L'espacement des requêtes ----

// throttle : au plus une requête par « gap » (plus un peu de hasard), pour
// ne jamais envoyer de rafale à LinkedIn. Chaque appel attend son tour.
type throttle struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func (t *throttle) wait(ctx context.Context) error {
	if t.gap <= 0 {
		return ctx.Err()
	}
	t.mu.Lock()
	now := time.Now()
	at := t.next
	if at.Before(now) {
		at = now
	}
	jitter := time.Duration(rand.Int64N(int64(t.gap/2) + 1))
	t.next = at.Add(t.gap + jitter)
	t.mu.Unlock()
	if d := time.Until(at); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

// ---- L'accès à LinkedIn ----

type (
	pageConvs   = linkedingo.CollectionResponse[linkedingo.ConversationCursorMetadata, linkedingo.Conversation]
	deltaConvs  = linkedingo.CollectionResponse[linkedingo.ConversationSyncMetadata, linkedingo.Conversation]
	pageMessage = linkedingo.CollectionResponse[linkedingo.MessageMetadata, linkedingo.Message]
)

// liAPI : ce que le pont demande à LinkedIn (*linkedingo.Client le fait ;
// les essais, un faux qui note les appels).
type liAPI interface {
	GetCurrentUserProfile(ctx context.Context) (*linkedingo.UserProfile, error)
	GetConversationsBySyncToken(ctx context.Context) (*deltaConvs, error)
	GetConversationsUpdatedBefore(ctx context.Context, before time.Time) (*pageConvs, error)
	GetMessagesBefore(ctx context.Context, conv linkedingo.URN, before time.Time, count int) (*pageMessage, error)
	GetMessagesWithPrevCursor(ctx context.Context, conv linkedingo.URN, cursor string, count int) (*pageMessage, error)
	SendMessage(ctx context.Context, conv linkedingo.URN, body linkedingo.SendMessageBody, rc []linkedingo.SendRenderContent, txn string, state linkedingo.ConversationState) (*linkedingo.MessageSentResponse, error)
	SendReaction(ctx context.Context, msg linkedingo.URN, emoji string) error
	RemoveReaction(ctx context.Context, msg linkedingo.URN, emoji string) error
	MarkConversationRead(ctx context.Context, convs ...linkedingo.URN) (*linkedingo.MarkThreadReadResponse, error)
	UploadMedia(ctx context.Context, t linkedingo.MediaUploadType, filename, contentType string, size int, r io.Reader) (linkedingo.URN, error)
	DownloadHTTP(ctx context.Context, url string) (*http.Response, error)
	RealtimeConnect(ctx context.Context) error
	RealtimeDisconnect()
	IsLoggedIn() bool
}

var _ liAPI = (*linkedingo.Client)(nil)

// rythme : chaque requête attend son tour (rest pour l'API, cdn pour les
// fichiers). Le flux « realtime » et son battement (une fois par minute)
// passent sans attendre.
type rythme struct {
	api  liAPI
	rest *throttle
	cdn  *throttle
}

func (r *rythme) GetCurrentUserProfile(ctx context.Context) (*linkedingo.UserProfile, error) {
	if err := r.rest.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.GetCurrentUserProfile(ctx)
}

func (r *rythme) GetConversationsBySyncToken(ctx context.Context) (*deltaConvs, error) {
	if err := r.rest.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.GetConversationsBySyncToken(ctx)
}

func (r *rythme) GetConversationsUpdatedBefore(ctx context.Context, before time.Time) (*pageConvs, error) {
	if err := r.rest.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.GetConversationsUpdatedBefore(ctx, before)
}

func (r *rythme) GetMessagesBefore(ctx context.Context, conv linkedingo.URN, before time.Time, count int) (*pageMessage, error) {
	if err := r.rest.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.GetMessagesBefore(ctx, conv, before, count)
}

func (r *rythme) GetMessagesWithPrevCursor(ctx context.Context, conv linkedingo.URN, cursor string, count int) (*pageMessage, error) {
	if err := r.rest.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.GetMessagesWithPrevCursor(ctx, conv, cursor, count)
}

func (r *rythme) SendMessage(ctx context.Context, conv linkedingo.URN, body linkedingo.SendMessageBody, rc []linkedingo.SendRenderContent, txn string, state linkedingo.ConversationState) (*linkedingo.MessageSentResponse, error) {
	if err := r.rest.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.SendMessage(ctx, conv, body, rc, txn, state)
}

func (r *rythme) SendReaction(ctx context.Context, msg linkedingo.URN, emoji string) error {
	if err := r.rest.wait(ctx); err != nil {
		return err
	}
	return r.api.SendReaction(ctx, msg, emoji)
}

func (r *rythme) RemoveReaction(ctx context.Context, msg linkedingo.URN, emoji string) error {
	if err := r.rest.wait(ctx); err != nil {
		return err
	}
	return r.api.RemoveReaction(ctx, msg, emoji)
}

func (r *rythme) MarkConversationRead(ctx context.Context, convs ...linkedingo.URN) (*linkedingo.MarkThreadReadResponse, error) {
	if err := r.rest.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.MarkConversationRead(ctx, convs...)
}

func (r *rythme) UploadMedia(ctx context.Context, t linkedingo.MediaUploadType, filename, contentType string, size int, rd io.Reader) (linkedingo.URN, error) {
	if err := r.rest.wait(ctx); err != nil {
		return linkedingo.URN{}, err
	}
	return r.api.UploadMedia(ctx, t, filename, contentType, size, rd)
}

func (r *rythme) DownloadHTTP(ctx context.Context, url string) (*http.Response, error) {
	if err := r.cdn.wait(ctx); err != nil {
		return nil, err
	}
	return r.api.DownloadHTTP(ctx, url)
}

func (r *rythme) RealtimeConnect(ctx context.Context) error { return r.api.RealtimeConnect(ctx) }
func (r *rythme) RealtimeDisconnect()                       { r.api.RealtimeDisconnect() }
func (r *rythme) IsLoggedIn() bool                          { return r.api.IsLoggedIn() }
