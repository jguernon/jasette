package main

// Le coffre de clés que twittermeow utilise pour XChat, gardé dans x.db :
//   - les clés des conversations (une par version), déchiffrées avec la clé
//     secrète du compte ;
//   - les jetons de conversation (donnés par X, nécessaires pour envoyer) ;
//   - la paire de clés du compte, qui vit dans session.json.
//
// Calqué sur connector/keystore.go de mautrix-twitter.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/mautrix-twitter/pkg/twittermeow/crypto"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/methods"
)

type keyStore struct {
	p  *Pont
	mu sync.Mutex
	// La session dont viennent les clés du compte (celle de la connexion en cours).
	sess *Session
}

var _ crypto.KeyStore = (*keyStore)(nil)

type storedConvKey struct {
	Key       []byte     `json:"key"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (ks *keyStore) GetConversationKey(ctx context.Context, conversationID, keyVersion string) (*crypto.ConversationKey, error) {
	var data []byte
	err := ks.p.db.QueryRowContext(context.WithoutCancel(ctx), "SELECT data FROM x_conv_key WHERE conv=$1 AND version=$2", conversationID, keyVersion).Scan(&data)
	if err != nil {
		return nil, crypto.ErrKeyNotFound
	}
	var k storedConvKey
	if err := json.Unmarshal(data, &k); err != nil || len(k.Key) == 0 {
		return nil, crypto.ErrKeyNotFound
	}
	return &crypto.ConversationKey{ConversationID: conversationID, KeyVersion: keyVersion, Key: k.Key, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt}, nil
}

func (ks *keyStore) PutConversationKey(ctx context.Context, key *crypto.ConversationKey) error {
	if key == nil {
		return fmt.Errorf("clé de conversation vide")
	}
	b, _ := json.Marshal(storedConvKey{Key: key.Key, CreatedAt: key.CreatedAt, ExpiresAt: key.ExpiresAt})
	_, err := ks.p.db.ExecContext(context.WithoutCancel(ctx), "INSERT INTO x_conv_key (conv, version, data) VALUES ($1, $2, $3) ON CONFLICT (conv, version) DO UPDATE SET data=excluded.data", key.ConversationID, key.KeyVersion, b)
	if err == nil {
		ks.p.markXChat(key.ConversationID)
	}
	return err
}

func (ks *keyStore) DeleteConversationKey(ctx context.Context, conversationID, keyVersion string) error {
	_, err := ks.p.db.ExecContext(context.WithoutCancel(ctx), "DELETE FROM x_conv_key WHERE conv=$1 AND version=$2", conversationID, keyVersion)
	return err
}

func (ks *keyStore) GetLatestConversationKey(ctx context.Context, conversationID string) (*crypto.ConversationKey, error) {
	rows, err := ks.p.db.QueryContext(context.WithoutCancel(ctx), "SELECT version, data FROM x_conv_key WHERE conv=$1", conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best *crypto.ConversationKey
	for rows.Next() {
		var version string
		var data []byte
		if rows.Scan(&version, &data) != nil {
			continue
		}
		var k storedConvKey
		if json.Unmarshal(data, &k) != nil || len(k.Key) == 0 {
			continue
		}
		// Les versions sont des « snowflakes » : la plus grande est la plus récente.
		if best == nil || methods.CompareSnowflake(version, best.KeyVersion) > 0 {
			best = &crypto.ConversationKey{ConversationID: conversationID, KeyVersion: version, Key: k.Key, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt}
		}
	}
	if best == nil {
		return nil, crypto.ErrKeyNotFound
	}
	return best, nil
}

func (ks *keyStore) GetOwnSigningKey(_ context.Context) (*crypto.SigningKeyPair, error) {
	ks.mu.Lock()
	s := ks.sess
	ks.mu.Unlock()
	if s == nil || (s.SecretKey == "" && s.SigningKey == "") {
		return nil, crypto.ErrKeyNotFound
	}
	return crypto.LoadSigningKeyPair(s.UserID, s.SigningKeyVersion, s.SigningKey, s.SecretKey)
}

func (ks *keyStore) PutOwnSigningKey(_ context.Context, key *crypto.SigningKeyPair) error {
	if key == nil {
		return fmt.Errorf("clé du compte vide")
	}
	ks.mu.Lock()
	s := ks.sess
	ks.mu.Unlock()
	if s == nil {
		return fmt.Errorf("pas de session")
	}
	s.SecretKey = key.DecryptKeyB64
	s.SigningKey = key.SigningKeyB64
	s.SigningKeyVersion = key.KeyVersion
	saveSession(s)
	return nil
}

// Les clés publiques des autres ne sont pas gardées (comme le pont officiel).
func (ks *keyStore) GetPublicKey(_ context.Context, _, _ string) (*crypto.PublicKeyInfo, error) {
	return nil, crypto.ErrKeyNotFound
}

func (ks *keyStore) PutPublicKey(_ context.Context, _ *crypto.PublicKeyInfo) error {
	return nil
}

func (ks *keyStore) GetConversationToken(ctx context.Context, conversationID string) (string, error) {
	var token string
	if err := ks.p.db.QueryRowContext(context.WithoutCancel(ctx), "SELECT token FROM x_conv_token WHERE conv=$1", conversationID).Scan(&token); err != nil || token == "" {
		return "", crypto.ErrKeyNotFound
	}
	return token, nil
}

func (ks *keyStore) PutConversationToken(ctx context.Context, conversationID, token string) error {
	_, err := ks.p.db.ExecContext(context.WithoutCancel(ctx), "INSERT INTO x_conv_token (conv, token) VALUES ($1, $2) ON CONFLICT (conv) DO UPDATE SET token=excluded.token", conversationID, token)
	if err == nil {
		ks.p.markXChat(conversationID)
	}
	return err
}

// Les modes d'envoi possibles pour un fil (comme selectMessageSendMode du pont officiel).
func (p *Pont) hasConvKey(conv string) bool {
	var n int
	_ = p.db.QueryRowContext(context.WithoutCancel(p.ctx), "SELECT COUNT(*) FROM x_conv_key WHERE conv=$1", conv).Scan(&n)
	return n > 0
}

func (p *Pont) hasConvToken(conv string) bool {
	var n int
	_ = p.db.QueryRowContext(context.WithoutCancel(p.ctx), "SELECT COUNT(*) FROM x_conv_token WHERE conv=$1", conv).Scan(&n)
	return n > 0
}
