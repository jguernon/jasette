package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10).
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé, le pont
// ignore la présence que Meta pousse.
//
// Meta la pousse dans la table lightspeed LSDeleteThenInsertContactPresence
// (ContactId, Status, LastActiveTimestampMs, ExpirationTimestampMs) ; la
// table LSTruncatePresenceDatabase l'efface. Status : 0 hors ligne, sinon
// actif (comme l'ancien « /orca_presence » : p = 2 actif, 0 absent).
// L'état « actif » vaut jusqu'à ExpirationTimestampMs : passé ce délai sans
// nouvelle ligne, le pont annonce le contact hors ligne.

import (
	"os"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

func presenceEnv() bool { return os.Getenv("JASETTE_PRESENCE") == "1" }

// Ce qu'on sait d'un contact actif.
type presenceRow struct {
	conv    string
	last    int64
	expires int64 // ms ; 0 = sans échéance
}

func (p *Pont) setPresence(on bool) {
	p.mu.Lock()
	p.presenceOn = on
	if !on {
		p.presence = map[int64]presenceRow{}
	}
	p.mu.Unlock()
}

// convOfContact : la conversation individuelle d'un contact. À appeler avec p.mu.
func (p *Pont) convOfContact(id int64) string {
	k := idStr(p.canon(id))
	if c := p.convs[k]; c != nil && c.Type.IsOneToOne() && !c.Hidden {
		return k
	}
	for k, c := range p.convs {
		if c.Hidden || !c.Type.IsOneToOne() {
			continue
		}
		for _, m := range c.Members {
			if m == id {
				return k
			}
		}
	}
	return ""
}

// handlePresenceTable : à appeler avec p.mu ; rend les événements à émettre.
func (p *Pont) handlePresenceTable(tbl *table.LSTable, me int64) []map[string]any {
	if !p.presenceOn {
		return nil
	}
	var out []map[string]any
	if len(tbl.LSTruncatePresenceDatabase) > 0 {
		for id, r := range p.presence {
			out = append(out, presenceItem(r.conv, id, false, r.last))
		}
		p.presence = map[int64]presenceRow{}
	}
	if n := len(tbl.LSDeleteThenInsertContactPresence); n > 0 && !p.presenceSeen {
		p.presenceSeen = true
		log.Info().Int("contacts", n).Msg("présence Messenger reçue")
	}
	now := time.Now().UnixMilli()
	for _, cp := range tbl.LSDeleteThenInsertContactPresence {
		if cp.ContactId == 0 || cp.ContactId == me {
			continue
		}
		conv := p.convOfContact(cp.ContactId)
		if conv == "" {
			continue
		}
		online := cp.Status != 0 && (cp.ExpirationTimestampMs == 0 || cp.ExpirationTimestampMs > now)
		last := cp.LastActiveTimestampMs
		if online {
			last = max(last, now)
			p.presence[cp.ContactId] = presenceRow{conv, last, cp.ExpirationTimestampMs}
		} else {
			delete(p.presence, cp.ContactId)
		}
		out = append(out, presenceItem(conv, cp.ContactId, online, last))
	}
	return out
}

func presenceItem(conv string, user int64, online bool, last int64) map[string]any {
	return map[string]any{"item": map[string]any{"conv": conv, "user": idStr(user), "online": online, "last_active": last}}
}

// presenceExpire : les contacts actifs dont l'échéance est passée (listLoop).
func (p *Pont) presenceExpire() {
	now := time.Now().UnixMilli()
	var out []map[string]any
	p.mu.Lock()
	for id, r := range p.presence {
		if r.expires > 0 && r.expires <= now {
			delete(p.presence, id)
			out = append(out, presenceItem(r.conv, id, false, max(r.last, r.expires)))
		}
	}
	p.mu.Unlock()
	for _, ev := range out {
		emit("presence", ev)
	}
}
