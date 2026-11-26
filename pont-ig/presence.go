package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10).
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé, le pont ne
// demande rien et ferme le flux.
//
// Le « statut d'activité » vient du flux PresenceUnifiedJSON de la passerelle
// Instagram (presence_flux.go), celui qu'ouvre le site instagram.com. L'ancien
// point d'accès /api/v1/direct_v2/get_presence/ répond 404 depuis 2026 : le
// pont ne l'appelle plus. Les ids du flux sont les ids de messagerie (EIMU),
// ceux des membres des fils (InteropMessagingUserFBID).

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

func presenceEnv() bool { return os.Getenv("JASETTE_PRESENCE") == "1" }

type presenceState struct {
	online bool
	last   int64
	user   int64
}

func (p *Pont) newFluxPresence() *fluxPresence {
	return &fluxPresence{nom: "Instagram", config: p.presenceConfig, contacts: p.presenceContacts, recu: p.applyPresence}
}

// setPresence : le réglage (commande, ou connexion au socket).
func (p *Pont) setPresence(on bool) {
	p.mu.Lock()
	p.presenceOn = on
	ready := p.ready
	if !on {
		p.presence = map[string]presenceState{}
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

// L'état de la page que garde instameow (DumpState) : ids du socket.
type igEtat struct {
	Configs struct {
		BrowserConfigTable *types.SchedulerJSDefineConfig
	}
}

// presenceConfig : de quoi ouvrir le socket streamcontroller, comme instameow
// pour la saisie (mêmes ids, mêmes cookies).
func (p *Pont) presenceConfig() (*fluxConfig, error) {
	p.mu.Lock()
	cli, ready, on := p.cli, p.ready, p.presenceOn
	p.mu.Unlock()
	if cli == nil || !ready || !on {
		return nil, nil
	}
	st, err := cli.DumpState()
	if err != nil || st == nil {
		return nil, err
	}
	var e igEtat
	if err := json.Unmarshal(st, &e); err != nil {
		return nil, err
	}
	t := e.Configs.BrowserConfigTable
	if t == nil || cmp.Or(t.PolarisViewer.ID, t.PolarisViewer.Data.Fbid) == "" {
		return nil, errors.New("ids de la page Instagram absents")
	}
	dial := cli.GetHTTP().GetWebsocketDialer()
	if dial == nil {
		return nil, nil
	}
	return &fluxConfig{
		WSURL:     cli.GetEndpoint("dgw_streamcontroller"),
		Origin:    cli.GetEndpoint("base_url"),
		Referer:   "https://www.instagram.com/direct/inbox/",
		AppID:     cmp.Or(t.DGWWebConfig.AppID, "936619743392459"),
		UserID:    cmp.Or(t.PolarisViewer.ID, t.PolarisViewer.Data.Fbid),
		DeviceID:  t.IGDMqttWebDeviceID.ClientID,
		AppFamily: 3,
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
		if cv.Group || cv.Hidden {
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

// applyPresence n'émet que les changements, pour les conversations
// individuelles connues ; rend combien de personnes ont une conversation.
// complet : la publication donne tout l'état (les absents sont hors ligne).
func (p *Pont) applyPresence(maj []majPresence, complet bool) int {
	var out []map[string]any
	rattaches := 0
	now := time.Now().UnixMilli()
	p.mu.Lock()
	if !p.presenceOn {
		p.mu.Unlock()
		return 0
	}
	vus := map[string]bool{}
	for _, u := range maj {
		if u.ID == p.me {
			continue
		}
		conv := p.convOfContact(u.ID)
		if conv == "" {
			continue
		}
		rattaches++
		vus[conv] = true
		last := u.Derniere
		if u.Actif {
			last = max(last, now)
		}
		st := presenceState{u.Actif, last, u.ID}
		if old, ok := p.presence[conv]; ok && old.online == st.online && (st.online || old.last == st.last) {
			continue
		}
		p.presence[conv] = st
		out = append(out, presenceEv(conv, u.ID, st))
	}
	if complet {
		for conv, st := range p.presence {
			if vus[conv] || !st.online {
				continue
			}
			st.online = false
			p.presence[conv] = st
			out = append(out, presenceEv(conv, st.user, st))
		}
	}
	p.mu.Unlock()
	for _, ev := range out {
		emit("presence", ev)
	}
	return rattaches
}

func presenceEv(conv string, user int64, st presenceState) map[string]any {
	return map[string]any{"item": map[string]any{
		"conv": conv, "user": strconv.FormatInt(user, 10), "online": st.online, "last_active": st.last,
	}}
}

// convOfContact : la conversation individuelle d'une personne. À appeler avec p.mu.
func (p *Pont) convOfContact(fbid int64) string {
	for k, c := range p.convs {
		if c.Group || c.Hidden {
			continue
		}
		for _, m := range c.Members {
			if m == fbid {
				return k
			}
		}
	}
	return ""
}
