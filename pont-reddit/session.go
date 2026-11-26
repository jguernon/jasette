package main

// La session (session.json, 0600), le transport HTTP et l'espacement des
// requêtes.

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---- session.json ----

type sessionFile struct {
	Cookies   map[string]string `json:"cookies"`
	Token     string            `json:"token"`
	Expires   time.Time         `json:"expires"`
	UserID    string            `json:"user_id"` // « @t2_xxx:reddit.com »
	NextBatch string            `json:"next_batch,omitempty"`
}

func sessionPath() string { return filepath.Join(rdDir(), "session.json") }

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
	if len(s.Cookies) == 0 && s.Token == "" {
		return nil, os.ErrNotExist
	}
	if s.Cookies == nil {
		s.Cookies = map[string]string{}
	}
	return &s, nil
}

func saveSession(s *sessionFile) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeWhole(sessionPath(), b)
}

func removeSession() {
	_ = os.Remove(sessionPath())
	_ = os.Remove(salonsPath())
}

// ---- L'espacement des requêtes ----

// throttle : au plus une requête par « gap » (plus un peu de hasard), pour
// ne jamais envoyer de rafale à Reddit. Chaque appel attend son tour.
type throttle struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func (t *throttle) wait(ctx context.Context) error {
	if t.gap <= 0 {
		return nil
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
	return nil
}

// ---- Le transport ----

// transportPoli : le User-Agent du navigateur sur chaque requête, et le
// rythme REST (sauf /sync, qui attend lui-même jusqu'à 30 secondes).
type transportPoli struct {
	base http.RoundTripper
	rest *throttle
}

func (t *transportPoli) RoundTrip(req *http.Request) (*http.Response, error) {
	if ua := req.Header.Get("User-Agent"); ua == "" || strings.HasPrefix(ua, "mautrix") {
		req.Header.Set("User-Agent", browserUA)
	}
	if !strings.HasSuffix(req.URL.Path, "/sync") {
		if err := t.rest.wait(req.Context()); err != nil {
			return nil, err
		}
	}
	return t.base.RoundTrip(req)
}

func newHTTPClient(rest *throttle, timeout time.Duration) *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.MaxIdleConnsPerHost = 4
	return &http.Client{Transport: &transportPoli{base: base, rest: rest}, Timeout: timeout}
}
