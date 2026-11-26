package main

// La connexion par code QR (« remote auth » de Discord), reprise du paquet
// remoteauth de mautrix-discord (AGPL, © Tulir Asokan et ses contributeurs) :
// même protocole, mêmes en-têtes, mais sans ses verrous imbriqués (le délai
// d'expiration y bloquait le client) et avec le WebSocket de discordgo
// (coder/websocket).
//
// Le déroulement :
//   1. hello (délai, intervalle des battements) → init (notre clé publique RSA) ;
//   2. nonce_proof (un nonce chiffré pour nous) → la preuve (SHA-256 du nonce) ;
//   3. pending_remote_init (une empreinte) → le code QR
//      https://discordapp.com/ra/<empreinte>, scanné par l'app du téléphone ;
//   4. pending_ticket (l'utilisateur, chiffré) : scanné, en attente de
//      confirmation sur le téléphone ;
//   5. pending_login (un ticket) → POST /users/@me/remote-auth/login → le
//      jeton, chiffré pour nous ; ou cancel (refusé sur le téléphone).

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/coder/websocket"
)

const remoteAuthURL = "wss://remote-auth-gateway.discord.gg/?v=2"

var errQRCancelled = errors.New("connexion refusée sur le téléphone")

// qrUser : ce que le téléphone a envoyé après le scan.
type qrUser struct {
	ID       string
	Username string
	Token    string
}

// remoteAuth suit une connexion par QR jusqu'au jeton. onQR est appelé avec
// l'adresse à montrer en code QR ; onScanned quand le téléphone l'a lue.
func remoteAuth(ctx context.Context, onQR func(string), onScanned func(qrUser)) (qrUser, error) {
	var user qrUser
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return user, err
	}
	h := http.Header{}
	for k, v := range discordgo.DroidWSHeaders {
		if k == "Accept-Encoding" {
			continue // le WebSocket s'en occupe
		}
		h.Set(k, v)
	}
	ctx, annule := context.WithCancel(ctx)
	defer annule()
	conn, _, err := websocket.Dial(ctx, remoteAuthURL, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return user, err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	send := func(v any) error {
		b, _ := json.Marshal(v)
		wctx, wannule := context.WithTimeout(ctx, 15*time.Second)
		defer wannule()
		return conn.Write(wctx, websocket.MessageText, b)
	}
	decrypt := func(payload string) ([]byte, error) {
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, err
		}
		return rsa.DecryptOAEP(sha256.New(), nil, key, raw, nil)
	}

	var deadline <-chan time.Time
	acks := make(chan struct{}, 4)
	for {
		type packet struct {
			OP                   string `json:"op"`
			Timeout              int    `json:"timeout_ms"`
			HeartbeatInterval    int    `json:"heartbeat_interval"`
			EncryptedNonce       string `json:"encrypted_nonce"`
			Fingerprint          string `json:"fingerprint"`
			EncryptedUserPayload string `json:"encrypted_user_payload"`
			Ticket               string `json:"ticket"`
		}
		type result struct {
			b   []byte
			err error
		}
		rc := make(chan result, 1)
		go func() {
			_, b, err := conn.Read(ctx)
			rc <- result{b, err}
		}()
		var r result
		select {
		case r = <-rc:
		case <-deadline:
			return user, errQRExpired
		case <-ctx.Done():
			return user, ctx.Err()
		}
		if r.err != nil {
			if websocket.CloseStatus(r.err) == websocket.StatusNormalClosure || ctx.Err() != nil {
				return user, errQRExpired
			}
			return user, r.err
		}
		var pk packet
		if err := json.Unmarshal(r.b, &pk); err != nil {
			return user, err
		}
		switch pk.OP {
		case "hello":
			if pk.Timeout > 0 {
				deadline = time.After(time.Duration(pk.Timeout)*time.Millisecond + 5*time.Second)
			}
			if pk.HeartbeatInterval > 0 {
				go heartbeat(ctx, annule, send, time.Duration(pk.HeartbeatInterval)*time.Millisecond, acks)
			}
			pub, err := x509.MarshalPKIXPublicKey(key.Public())
			if err != nil {
				return user, err
			}
			if err := send(map[string]string{"op": "init", "encoded_public_key": base64.RawStdEncoding.EncodeToString(pub)}); err != nil {
				return user, err
			}
		case "nonce_proof":
			plain, err := decrypt(pk.EncryptedNonce)
			if err != nil {
				return user, err
			}
			sum := sha256.Sum256(plain)
			if err := send(map[string]string{"op": "nonce_proof", "proof": base64.RawURLEncoding.EncodeToString(sum[:])}); err != nil {
				return user, err
			}
		case "pending_remote_init":
			onQR("https://discordapp.com/ra/" + pk.Fingerprint)
		case "pending_ticket":
			plain, err := decrypt(pk.EncryptedUserPayload)
			if err != nil {
				return user, err
			}
			parts := strings.Split(string(plain), ":")
			if len(parts) == 4 {
				user.ID, user.Username = parts[0], parts[3]
			}
			if onScanned != nil {
				onScanned(user)
			}
		case "pending_login":
			sess, err := discordgo.New("")
			if err != nil {
				return user, err
			}
			enc, err := sess.RemoteAuthLogin(pk.Ticket)
			if err != nil {
				return user, fmt.Errorf("Discord demande peut-être une vérification (captcha) : essayez le jeton (%s)", errorText(err))
			}
			plain, err := decrypt(enc)
			if err != nil {
				return user, err
			}
			user.Token = string(plain)
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return user, nil
		case "cancel":
			return user, errQRCancelled
		case "heartbeat_ack":
			select {
			case acks <- struct{}{}:
			default:
			}
		}
	}
}

// heartbeat : les battements du protocole ; deux sans réponse, on abandonne.
func heartbeat(ctx context.Context, annule context.CancelFunc, send func(any) error, every time.Duration, acks chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	missed := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-acks:
			missed = 0
		case <-t.C:
			missed++
			if missed > 2 || send(map[string]string{"op": "heartbeat"}) != nil {
				annule()
				return
			}
		}
	}
}
