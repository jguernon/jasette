package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10).
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé (ou absent),
// le pont n'en fait rien : aucun abonnement, et l'appareil reste
// « indisponible » hors des moments d'activité (le téléphone garde ses
// notifications). Activé, l'appareil se déclare « disponible » en permanence
// (WhatsApp ne transmet la présence des autres qu'à un appareil disponible) et
// s'abonne à la présence des 50 conversations individuelles les plus récentes.

import (
	"cmp"
	"context"
	"os"
	"slices"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Combien de conversations individuelles suivre.
const presenceMax = 50

func presenceEnv() bool { return os.Getenv("JASETTE_PRESENCE") == "1" }

// setPresence : la commande « presence ».
func (p *Pont) setPresence(on bool) {
	p.mu.Lock()
	was := p.presenceOn
	p.presenceOn = on
	cli := p.cli
	ready := p.ready
	if !on {
		p.subscribed = map[string]bool{}
	}
	p.mu.Unlock()
	if was == on || cli == nil || !ready {
		return
	}
	if on {
		p.presenceUp(cli)
	} else {
		// Plus de présence : le téléphone retrouve ses notifications.
		p.mu.Lock()
		p.online = false
		p.mu.Unlock()
		_ = cli.SendPresence(context.WithoutCancel(p.ctx), types.PresenceUnavailable)
	}
}

// presenceUp : se déclarer disponible et s'abonner aux conversations récentes.
// Après chaque connexion (les abonnements ne survivent pas à une coupure).
func (p *Pont) presenceUp(cli *whatsmeow.Client) {
	p.mu.Lock()
	p.online = true
	p.lastActive = time.Now()
	p.subscribed = map[string]bool{}
	p.mu.Unlock()
	if err := cli.SendPresence(context.WithoutCancel(p.ctx), types.PresenceAvailable); err != nil {
		log.Debug().Err(err).Msg("présence")
	}
	p.subscribeRecent(cli)
}

// subscribeRecent s'abonne aux conversations individuelles les plus récentes
// qui ne le sont pas encore.
func (p *Pont) subscribeRecent(cli *whatsmeow.Client) {
	p.mu.Lock()
	if !p.presenceOn {
		p.mu.Unlock()
		return
	}
	type c struct {
		jid string
		t   int64
	}
	var all []c
	for id, cv := range p.convs {
		if cv.Group || cv.Hidden {
			continue
		}
		all = append(all, c{id, max(cv.LastTime, cv.LastActivity)})
	}
	slices.SortFunc(all, func(a, b c) int { return cmp.Compare(b.t, a.t) })
	if len(all) > presenceMax {
		all = all[:presenceMax]
	}
	var todo []types.JID
	for _, x := range all {
		if p.subscribed[x.jid] {
			continue
		}
		j, err := types.ParseJID(x.jid)
		if err != nil || j.Server != types.DefaultUserServer && j.Server != types.HiddenUserServer {
			continue
		}
		p.subscribed[x.jid] = true
		todo = append(todo, j)
	}
	p.mu.Unlock()
	for _, j := range todo {
		ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
		if err := cli.SubscribePresence(ctx, j); err != nil {
			log.Debug().Err(err).Str("jid", j.String()).Msg("abonnement à la présence")
		}
		annule()
	}
	if len(todo) > 0 {
		log.Info().Int("contacts", len(todo)).Msg("abonné à la présence")
	}
}

// handlePresence : un contact est passé en ligne ou hors ligne.
func (p *Pont) handlePresence(cli *whatsmeow.Client, evt *events.Presence) {
	p.mu.Lock()
	on := p.presenceOn
	p.mu.Unlock()
	if !on {
		return
	}
	jid := p.canon(cli, evt.From)
	if skipChat(jid) || jid.Server == types.GroupServer {
		return
	}
	conv := jid.String()
	p.mu.Lock()
	cv := p.convs[conv]
	me := p.isMe(cli, conv)
	p.mu.Unlock()
	if cv == nil || cv.Group || me {
		return
	}
	var last int64
	if !evt.LastSeen.IsZero() {
		last = evt.LastSeen.UnixMilli()
	}
	if !evt.Unavailable {
		last = time.Now().UnixMilli()
	}
	emit("presence", map[string]any{"item": map[string]any{
		"conv": conv, "user": conv, "online": !evt.Unavailable, "last_active": last,
	}})
}
