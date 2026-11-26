package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10).
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé, le pont
// ignore les statuts.
//
// Telegram donne le statut de chaque personne (tg.User.Status, dans les
// listes) et en pousse les changements (updateUserStatus) : en ligne
// (UserStatusOnline), hors ligne depuis WasOnline (UserStatusOffline), ou
// approximatif (« récemment », « cette semaine »… : hors ligne, heure
// inconnue). Le pont ne se déclare jamais « en ligne » lui-même
// (account.updateStatus) : le téléphone garde ses notifications.

import (
	"os"
	"time"

	"github.com/gotd/td/tg"
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
// conversations individuelles connues.
func (p *Pont) notePresence(k string, st tg.UserStatusClass) {
	if !p.presenceOn || st == nil || p.convs[k] == nil || k == keyOf('u', p.me) {
		return
	}
	var s presenceState
	switch v := st.(type) {
	case *tg.UserStatusOnline:
		s = presenceState{true, time.Now().UnixMilli()}
	case *tg.UserStatusOffline:
		s = presenceState{false, int64(v.WasOnline) * 1000}
	default:
		s = presenceState{false, 0}
	}
	if old, ok := p.presence[k]; ok && old.online == s.online && (s.online || old.last == s.last) {
		return
	}
	if _, ok := p.presence[k]; !ok && !s.online && s.last == 0 {
		// Rien à montrer : inutile de l'annoncer.
		p.presence[k] = s
		return
	}
	p.presence[k] = s
	emit("presence", map[string]any{"item": map[string]any{
		"conv": k, "user": k, "online": s.online, "last_active": s.last,
	}})
}
