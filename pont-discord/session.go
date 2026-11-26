package main

// La session (session.json, 0600), le transport HTTP qui imite Chrome et
// l'espacement des requêtes.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/imroc/req/v3"
	utls "github.com/refraction-networking/utls"
)

// ---- session.json ----

type sessionFile struct {
	Token     string                      `json:"token"`
	UserID    string                      `json:"user_id"`
	Heartbeat *discordgo.HeartbeatSession `json:"heartbeat,omitempty"`
}

func sessionPath() string { return filepath.Join(dcDir(), "session.json") }

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
	if s.Token == "" {
		return nil, os.ErrNotExist
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
}

// cleanToken : le jeton collé à la main, sans guillemets ni espaces. Rend
// une erreur lisible pour ce qui n'en est manifestement pas un.
func cleanToken(t string) (string, error) {
	t = strings.TrimSpace(t)
	t = strings.Trim(t, "\"'` ")
	t = strings.TrimSpace(t)
	if strings.HasPrefix(strings.ToLower(t), "bot ") {
		return "", fmt.Errorf("c'est le jeton d'un robot : il faut celui de votre compte")
	}
	if len(t) < 50 || len(t) > 200 || strings.ContainsAny(t, " \t\r\n") {
		return "", fmt.Errorf("ce texte ne ressemble pas à un jeton Discord")
	}
	return t, nil
}

// ---- Le transport ----

// compileTransport : un client HTTP qui se présente comme Chrome jusque dans
// la poignée de main TLS (empreinte JA3/JA4), comme mautrix-discord (http.go,
// AGPL, © Tulir Asokan), dont cette fonction est reprise. Le WebSocket de la
// passerelle ne doit annoncer que http/1.1 en ALPN.
func compileTransport(onlyAdvertiseHTTP1InALPN bool) http.RoundTripper {
	reqClient := req.C().ImpersonateChrome()
	reqClient.SetProxy(nil)
	if onlyAdvertiseHTTP1InALPN {
		forceHTTP1ChromeFingerprint(reqClient)
		reqClient.EnableForceHTTP1()
	}
	return reqClient.Transport
}

// forceHTTP1ChromeFingerprint garde l'empreinte de Chrome, mais n'annonce que
// http/1.1 en ALPN (repris de mautrix-discord).
func forceHTTP1ChromeFingerprint(c *req.Client) {
	c.SetTLSHandshake(func(ctx context.Context, addr string, plainConn net.Conn) (net.Conn, *tls.ConnectionState, error) {
		hostname := addr
		if i := strings.LastIndex(addr, ":"); i != -1 {
			hostname = addr[:i]
		}
		// Doit correspondre à ce qu'utilise ImpersonateChrome de req.
		spec, err := utls.UTLSIdToSpec(utls.HelloChrome_120)
		if err != nil {
			return nil, nil, fmt.Errorf("empreinte TLS de Chrome : %w", err)
		}
		exts := spec.Extensions[:0]
		for _, ext := range spec.Extensions {
			switch e := ext.(type) {
			case *utls.ApplicationSettingsExtension:
				continue
			case *utls.ALPNExtension:
				e.AlpnProtocols = []string{"http/1.1"}
			}
			exts = append(exts, ext)
		}
		spec.Extensions = exts
		tlsConfig := c.GetTLSClientConfig()
		uconn := utls.UClient(plainConn, &utls.Config{
			ServerName:   hostname,
			NextProtos:   []string{"http/1.1"},
			RootCAs:      tlsConfig.RootCAs,
			KeyLogWriter: tlsConfig.KeyLogWriter,
		}, utls.HelloCustom)
		if err := uconn.ApplyPreset(&spec); err != nil {
			return nil, nil, fmt.Errorf("empreinte TLS de Chrome : %w", err)
		}
		if err := uconn.HandshakeContext(ctx); err != nil {
			return nil, nil, err
		}
		cs := uconn.ConnectionState()
		return uconn, &tls.ConnectionState{
			Version:            cs.Version,
			HandshakeComplete:  cs.HandshakeComplete,
			DidResume:          cs.DidResume,
			CipherSuite:        cs.CipherSuite,
			NegotiatedProtocol: cs.NegotiatedProtocol,
			ServerName:         cs.ServerName,
			PeerCertificates:   cs.PeerCertificates,
			VerifiedChains:     cs.VerifiedChains,
		}, nil
	})
}

// ---- L'espacement des requêtes ----

// throttle : au plus une requête par « gap » (plus un peu de hasard), pour
// ne jamais envoyer de rafale à Discord. Chaque appel attend son tour.
type throttle struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func (t *throttle) wait(ctx context.Context) error {
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
