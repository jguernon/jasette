package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10), comme les autres
// ponts : {"ev":"presence","item":{"conv","user","online","last_active"}}.
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé, le pont
// ignore les statuts.
//
// Discord pousse lui-même la présence des amis à un compte utilisateur
// (READY, puis PRESENCE_UPDATE) : aucune requête de plus. « online »,
// « idle » et « dnd » comptent comme en ligne ; Discord ne dit jamais depuis
// quand quelqu'un est hors ligne : last_active 0, sauf si le pont l'a vu
// passer hors ligne (alors, ce moment-là). Le pont, lui, se déclare
// toujours « invisible » (comme mautrix-discord) : le téléphone garde ses
// notifications et personne ne voit Jasette en ligne.

import (
	"os"
	"time"

	"github.com/bwmarrin/discordgo"
)

func presenceEnv() bool { return os.Getenv("JASETTE_PRESENCE") == "1" }

type presenceState struct {
	online bool
	last   int64
}

func (p *Pont) setPresence(on bool) {
	p.mu.Lock()
	p.presenceOn = on
	if !on {
		p.presence = map[string]presenceState{}
	}
	p.mu.Unlock()
}

// notePresence : à appeler avec p.mu. N'émet que les changements, pour les
// messages privés (un à un) connus.
func (p *Pont) notePresence(userID string, st discordgo.Status) {
	if !p.presenceOn || userID == p.meID() {
		return
	}
	conv := ""
	for id, c := range p.chans {
		if c.ch.Type != discordgo.ChannelTypeDM {
			continue
		}
		for _, u := range c.ch.Recipients {
			if u != nil && u.ID == userID {
				conv = id
			}
		}
	}
	if conv == "" {
		return
	}
	online := st == discordgo.StatusOnline || st == discordgo.StatusIdle || st == discordgo.StatusDoNotDisturb
	s := presenceState{online: online}
	if online {
		s.last = time.Now().UnixMilli()
	}
	old, seen := p.presence[conv]
	if seen && old.online == s.online {
		return
	}
	if seen && old.online && !online {
		s.last = time.Now().UnixMilli() // vu en ligne jusqu'à maintenant
	}
	p.presence[conv] = s
	if !seen && !online {
		return // rien à montrer
	}
	emit("presence", map[string]any{"item": map[string]any{
		"conv": conv, "user": userID, "online": s.online, "last_active": s.last,
	}})
}
