package main

// « En train d'écrire » dans les fils chiffrés (PROTOCOLE.md, §5.11).
//
// Les fils chiffrés de Messenger passent par l'infrastructure de WhatsApp
// (whatsmeow) : comme WhatsApp, elle ne transmet les « en train d'écrire »
// (ChatPresence) qu'à un appareil qui s'est déclaré **disponible**. Le pont
// fait comme mautrix-meta (HandleMatrixViewingChat) : il se déclare
// disponible quand l'utilisateur ouvre un fil chiffré (commande `messages`
// de la page la plus récente, ou `read`, `send`), et redevient indisponible
// après ecritFenetre sans nouvelle action. Rien n'est envoyé :
//   - si le réglage « Afficher qui est en ligne et qui écrit » est désactivé ;
//   - pour une commande lancée par le serveur lui-même (préchargement, MCP :
//     champ `auto`) ;
//   - pour un fil non chiffré (lightspeed pousse LSUpdateTypingIndicator
//     sans rien demander).
// Le pont n'envoie jamais son propre « en train d'écrire » (SendChatPresence).

import (
	"context"
	"time"

	waTypes "go.mau.fi/whatsmeow/types"
)

// Combien de temps rester disponible après la dernière action dans un fil chiffré.
const ecritFenetre = 5 * time.Minute

// waPresence : déclare l'appareil chiffré disponible ou non.
func (p *Pont) waPresence(pr waTypes.Presence) error {
	if p.waPresenceTest != nil {
		return p.waPresenceTest(pr)
	}
	wa := p.waClient()
	if wa == nil {
		return nil
	}
	ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
	defer annule()
	return wa.SendPresence(ctx, pr)
}

// vueChiffree : l'utilisateur regarde (ou agit dans) le fil `conv`.
func (p *Pont) vueChiffree(conv string, auto bool) {
	if auto || conv == "" {
		return
	}
	p.mu.Lock()
	c := p.convs[conv]
	if !p.presenceOn || c == nil || !c.Type.IsWhatsApp() {
		p.mu.Unlock()
		return
	}
	p.e2eeVue = time.Now()
	envoyer := !p.e2eeDispo && p.e2eeReady.IsSet()
	if envoyer {
		p.e2eeDispo = true
	}
	p.mu.Unlock()
	if envoyer {
		p.dispoChiffree(true)
	}
}

// dispoChiffree envoie la présence et le note au journal.
func (p *Pont) dispoChiffree(on bool) {
	pr, quoi := waTypes.PresenceUnavailable, "indisponible"
	if on {
		pr, quoi = waTypes.PresenceAvailable, "disponible (fil chiffré ouvert)"
	}
	if err := p.waPresence(pr); err != nil {
		log.Warn().Err(err).Msg("présence chiffrée")
		if on {
			p.mu.Lock()
			p.e2eeDispo = false
			p.mu.Unlock()
		}
		return
	}
	log.Info().Msg("canal chiffré : " + quoi)
}

// ecritExpire : listLoop ; plus d'action depuis ecritFenetre, ou réglage
// désactivé : indisponible.
func (p *Pont) ecritExpire() {
	p.mu.Lock()
	fin := p.e2eeDispo && (!p.presenceOn || time.Since(p.e2eeVue) > ecritFenetre)
	if fin {
		p.e2eeDispo = false
	}
	pret := p.e2eeReady.IsSet()
	p.mu.Unlock()
	if fin && pret {
		p.dispoChiffree(false)
	}
}

// ecritConnecte : le canal chiffré vient de (re)connecter ; une connexion
// neuve est indisponible. On rétablit la disponibilité si l'utilisateur
// était encore dans un fil chiffré.
func (p *Pont) ecritConnecte() {
	p.mu.Lock()
	envoyer := p.presenceOn && !p.e2eeVue.IsZero() && time.Since(p.e2eeVue) <= ecritFenetre
	p.e2eeDispo = envoyer
	p.mu.Unlock()
	if envoyer {
		p.dispoChiffree(true)
	}
}

// ecritRecu : trace de diagnostic, une fois par sorte (« chiffré » ou
// « non chiffré ») depuis le lancement ; sans contenu ni identifiant.
func (p *Pont) ecritRecu(chiffre bool) {
	p.mu.Lock()
	vu := &p.ecritVuFB
	sorte := "non chiffré"
	if chiffre {
		vu, sorte = &p.ecritVuE2EE, "chiffré"
	}
	premier := !*vu
	*vu = true
	p.mu.Unlock()
	if premier {
		log.Info().Str("fil", sorte).Msg("premier « en train d'écrire » Messenger reçu")
	}
}
