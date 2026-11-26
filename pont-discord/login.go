package main

// La connexion : par code QR (le « remote auth » de Discord, comme
// mautrix-discord) ou par jeton collé à la main.

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Au plus tant de codes QR de suite (chacun vit environ deux minutes).
const maxQR = 3

// cancelPairing arrête une connexion par QR en cours ; vrai s'il y en avait une.
func (p *Pont) cancelPairing() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pairing == nil {
		return false
	}
	p.pairing()
	p.pairing = nil
	return true
}

// pairQR : un code QR à scanner dans l'app Discord du téléphone
// (Paramètres → Scanner le code QR). Le téléphone demande ensuite de
// confirmer ; Discord rend alors le jeton de la nouvelle session, chiffré
// pour nous seuls (clé RSA tirée pour l'occasion).
func (p *Pont) pairQR() {
	p.cancelPairing()
	p.mu.Lock()
	if p.me != nil && p.sess != nil {
		p.mu.Unlock()
		emitErr("pair", errors.New("Discord est déjà connecté"))
		return
	}
	ctx, annule := context.WithCancel(p.ctx)
	p.pairing = annule
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if ctx.Err() == nil && p.pairing != nil {
			p.pairing = nil
		}
		p.mu.Unlock()
		annule()
	}()
	emitState("pairing")
	for essai := 0; essai < maxQR; essai++ {
		token, err := p.oneQR(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil && token != "" {
			p.finishLogin(ctx, token, "pair")
			return
		}
		if err != nil && !errors.Is(err, errQRExpired) {
			log.Warn().Err(err).Msg("connexion par QR")
			emitErr("pair", fmt.Errorf("Discord : %w", err))
			emitState("unpaired")
			return
		}
	}
	emitErr("pair", errors.New("le code QR a expiré : recommencez"))
	emitState("unpaired")
}

var errQRExpired = errors.New("code QR expiré")

func (p *Pont) oneQR(ctx context.Context) (string, error) {
	user, err := remoteAuth(ctx, func(code string) {
		emit("qr", map[string]any{"code": code, "data": code})
	}, func(u qrUser) {
		log.Info().Msg("code QR scanné : en attente de la confirmation sur le téléphone")
		emit("qr_scanned", map[string]any{"name": u.Username})
	})
	if err != nil {
		return "", err
	}
	if user.Token == "" {
		return "", errQRExpired
	}
	return user.Token, nil
}

// loginToken : le jeton collé à la main (Réglages > Comptes > Discord).
func (p *Pont) loginToken(raw string) {
	p.cancelPairing()
	token, err := cleanToken(raw)
	if err != nil {
		emitErr("login", err)
		emitState("unpaired")
		return
	}
	p.mu.Lock()
	connected := p.me != nil && p.sess != nil
	p.mu.Unlock()
	if connected {
		emitErr("login", errors.New("Discord est déjà connecté"))
		return
	}
	emitState("pairing")
	p.finishLogin(p.ctx, token, "login")
}

// finishLogin ouvre la session avec le nouveau jeton ; READY dira paired,
// account et ready (onReady). Sans READY après une minute : échec.
func (p *Pont) finishLogin(ctx context.Context, token, where string) {
	emitState("connecting")
	if err := p.connect(token, true); err != nil {
		log.Warn().Err(err).Msg("connexion")
		p.mu.Lock()
		p.login = false
		p.mu.Unlock()
		emitErr(where, fmt.Errorf("Discord a refusé la connexion : %s", errorText(err)))
		emitState("unpaired")
		return
	}
	deadline := time.After(60 * time.Second)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			p.mu.Lock()
			done := p.me != nil || !p.login
			p.mu.Unlock()
			if done {
				return
			}
		case <-deadline:
			p.mu.Lock()
			waiting := p.login && p.me == nil
			sess := p.sess
			if waiting {
				p.login = false
				p.sess = nil
				p.gen++
			}
			p.mu.Unlock()
			if waiting {
				if sess != nil {
					_ = sess.Close()
				}
				emitErr(where, errors.New("Discord ne répond pas : réessayez"))
				emitState("unpaired")
			}
			return
		case <-ctx.Done():
			return
		}
	}
}
