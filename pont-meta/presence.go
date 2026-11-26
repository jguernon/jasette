package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10).
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé, le pont
// ferme le flux et ignore la présence que Meta pousse.
//
// Deux sources, qui donnent les mêmes événements :
//
//   - le flux PresenceUnifiedJSON de la passerelle (presence_flux.go), celui
//     qu'ouvre le site facebook.com depuis que l'ancien MQTT (/orca_presence)
//     n'existe plus : c'est la voie principale ;
//   - la table lightspeed LSDeleteThenInsertContactPresence (ContactId,
//     Status, LastActiveTimestampMs, ExpirationTimestampMs) et
//     LSTruncatePresenceDatabase, si jamais Meta les pousse encore sur le
//     socket principal (jamais vu en 2026). Status : 0 hors ligne, sinon
//     actif, jusqu'à ExpirationTimestampMs.

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

func presenceEnv() bool { return os.Getenv("JASETTE_PRESENCE") == "1" }

// Ce qu'on sait d'un contact actif.
type presenceRow struct {
	conv    string
	last    int64
	expires int64 // ms ; 0 = sans échéance
}

func (p *Pont) newFluxPresence() *fluxPresence {
	return &fluxPresence{nom: "Messenger", config: p.presenceConfig, contacts: p.presenceContacts, recu: p.applyPresenceFlux}
}

// setPresence : le réglage (commande, ou connexion au socket).
func (p *Pont) setPresence(on bool) {
	p.mu.Lock()
	p.presenceOn = on
	ready := p.ready
	if !on {
		p.presence = map[int64]presenceRow{}
		p.presenceHors = map[int64]int64{}
	}
	p.mu.Unlock()
	if on && ready {
		p.flux.demarrer(p.ctx)
	} else if !on {
		p.flux.arreter()
	}
}

// presenceWanted : le réglage actuel.
func (p *Pont) presenceWanted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.presenceOn
}

// L'état de la page que garde messagix (DumpState) : ids du socket.
type fbEtat struct {
	Configs struct {
		BrowserConfigTable *types.SchedulerJSDefineConfig
	}
}

// presenceConfig : de quoi ouvrir le socket streamcontroller de la passerelle,
// avec les mêmes ids et cookies que le socket principal de messagix.
func (p *Pont) presenceConfig() (*fluxConfig, error) {
	p.mu.Lock()
	cli, ready, on, me := p.cli, p.ready, p.presenceOn, p.me
	p.mu.Unlock()
	if cli == nil || !ready || !on {
		return nil, nil
	}
	st, err := cli.DumpState()
	if err != nil || st == nil {
		return nil, err
	}
	var e fbEtat
	if err := json.Unmarshal(st, &e); err != nil {
		return nil, err
	}
	t := e.Configs.BrowserConfigTable
	if t == nil {
		return nil, errors.New("configuration de la page Messenger absente")
	}
	user := cmp.Or(t.CurrentUserInitialData.UserID, idStr(me))
	if user == "" || user == "0" {
		return nil, errors.New("id du compte Messenger absent")
	}
	ws := cli.GetEndpoint("dgw_lightspeed")
	if !strings.HasSuffix(ws, "/ws/lightspeed") {
		return nil, errors.New("adresse de la passerelle inattendue : " + ws)
	}
	dial := cli.GetHTTP().GetWebsocketDialer()
	if dial == nil {
		return nil, nil
	}
	return &fluxConfig{
		WSURL:     strings.TrimSuffix(ws, "/ws/lightspeed") + "/ws/streamcontroller",
		Origin:    cli.GetEndpoint("base_url"),
		Referer:   strings.TrimSuffix(cli.GetEndpoint("messages"), "/") + "/",
		AppID:     cmp.Or(t.DGWWebConfig.AppID, t.CurrentUserInitialData.AppID, "2220391788200892"),
		UserID:    user,
		DeviceID:  cmp.Or(t.MqttWebDeviceID.ClientID, t.IGDMqttWebDeviceID.ClientID),
		Facebook:  true,
		AppFamily: 1,
		Cookies:   cli.GetCookies().String,
		Dial:      *dial,
	}, nil
}

// presenceContacts : les personnes des conversations individuelles, les plus
// récentes d'abord.
func (p *Pont) presenceContacts() []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	type c struct {
		id int64
		t  int64
	}
	var all []c
	vu := map[int64]bool{}
	for _, cv := range p.convs {
		if cv.Hidden || !cv.Type.IsOneToOne() {
			continue
		}
		for _, m := range cv.Members {
			if m != 0 && m != p.me && !vu[m] {
				vu[m] = true
				all = append(all, c{m, max(cv.LastTime, cv.LastActivity)})
			}
		}
	}
	slices.SortFunc(all, func(a, b c) int { return cmp.Compare(b.t, a.t) })
	out := make([]int64, 0, len(all))
	for _, x := range all {
		out = append(out, x.id)
	}
	return out
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

// applyPresenceFlux : une publication du flux ; rend combien de personnes ont
// une conversation. complet : tout l'état (les absents sont hors ligne).
func (p *Pont) applyPresenceFlux(maj []majPresence, complet bool) int {
	now := time.Now().UnixMilli()
	var out []map[string]any
	rattaches := 0
	p.mu.Lock()
	if !p.presenceOn {
		p.mu.Unlock()
		return 0
	}
	vus := map[int64]bool{}
	for _, u := range maj {
		if u.ID == p.me {
			continue
		}
		conv := p.convOfContact(u.ID)
		if conv == "" {
			continue
		}
		rattaches++
		vus[u.ID] = true
		old, avant := p.presence[u.ID]
		if u.Actif {
			if !avant {
				out = append(out, presenceItem(conv, u.ID, true, max(u.Derniere, now)))
			}
			p.presence[u.ID] = presenceRow{conv, max(u.Derniere, now), 0}
			continue
		}
		delete(p.presence, u.ID)
		last := max(u.Derniere, old.last)
		if avant || (last != 0 && p.presenceHors[u.ID] != last) {
			p.presenceHors[u.ID] = last
			out = append(out, presenceItem(conv, u.ID, false, last))
		}
	}
	if complet {
		for id, r := range p.presence {
			if !vus[id] {
				delete(p.presence, id)
				out = append(out, presenceItem(r.conv, id, false, r.last))
			}
		}
	}
	p.mu.Unlock()
	for _, ev := range out {
		emit("presence", ev)
	}
	return rattaches
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
		log.Info().Int("contacts", n).Str("voie", "lightspeed").Msg("présence Messenger reçue (table lightspeed)")
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
