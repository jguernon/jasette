package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10).
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé, le pont ne
// demande rien.
//
// instameow ne s'abonne qu'au flux des « en train d'écrire » (le site en ouvre
// aussi un pour la présence, que la bibliothèque n'implémente pas). Le pont
// demande donc le « statut d'activité » à l'API web d'Instagram,
// /api/v1/direct_v2/get_presence/, toutes les minutes tant qu'il est prêt :
// {"user_presence":{"<id Instagram>":{"is_active":true,"last_activity_at_ms":…}}}.
// Après trois échecs de suite, il attend 30 minutes avant de réessayer.

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

const presenceURL = "https://www.instagram.com/api/v1/direct_v2/get_presence/"

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
	if on {
		select {
		case p.presenceNow <- struct{}{}:
		default:
		}
	}
}

// presenceWanted : le réglage actuel (pour une première demande dès la connexion).
func (p *Pont) presenceWanted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.presenceOn
}

// presenceLoop : une demande par minute, seulement si le réglage est activé.
func (p *Pont) presenceLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	echecs := 0
	var pause time.Time
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
		case <-p.presenceNow:
			pause = time.Time{}
			echecs = 0
		}
		p.mu.Lock()
		on, ready := p.presenceOn, p.ready
		p.mu.Unlock()
		if !on || !ready || time.Now().Before(pause) {
			continue
		}
		if err := p.fetchPresence(); err != nil {
			echecs++
			log.Debug().Err(err).Int("échecs", echecs).Msg("présence Instagram")
			if echecs >= 3 {
				log.Warn().Err(err).Msg("présence Instagram indisponible : nouvel essai dans 30 min")
				pause = time.Now().Add(30 * time.Minute)
				echecs = 0
			}
		} else {
			echecs = 0
		}
	}
}

type presenceReply struct {
	UserPresence map[string]struct {
		IsActive         bool        `json:"is_active"`
		LastActivityAtMs json.Number `json:"last_activity_at_ms"`
	} `json:"user_presence"`
	Status string `json:"status"`
}

func (p *Pont) fetchPresence() error {
	cli := p.client()
	if cli == nil {
		return nil
	}
	h := cli.GetHTTP().BuildHeaders(true, false)
	h.Set("x-requested-with", "XMLHttpRequest")
	h.Set("referer", "https://www.instagram.com/direct/inbox/")
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-origin")
	resp, body, err := cli.GetHTTP().MakeRequest(p.ctx, presenceURL, http.MethodGet, h, nil, types.NONE)
	if err != nil {
		return err
	}
	var r presenceReply
	if err := json.Unmarshal(body, &r); err != nil {
		return err
	}
	if resp != nil && resp.StatusCode != http.StatusOK || r.Status != "ok" {
		return errStatus(r.Status)
	}
	p.applyPresence(r)
	return nil
}

type errStatus string

func (e errStatus) Error() string { return "réponse inattendue : " + string(e) }

// applyPresence n'émet que les changements, pour les conversations individuelles connues.
func (p *Pont) applyPresence(r presenceReply) {
	var out []map[string]any
	p.mu.Lock()
	if !p.presenceOn {
		p.mu.Unlock()
		return
	}
	for igid, u := range r.UserPresence {
		fbid := p.byUserIGID[igid]
		if fbid == 0 || fbid == p.me {
			continue
		}
		conv := p.convOfContact(fbid)
		if conv == "" {
			continue
		}
		last, _ := u.LastActivityAtMs.Int64()
		if u.IsActive {
			last = max(last, time.Now().UnixMilli())
		}
		st := presenceState{u.IsActive, last}
		if old, ok := p.presence[conv]; ok && old.online == st.online && (st.online || old.last == st.last) {
			continue
		}
		p.presence[conv] = st
		out = append(out, map[string]any{"item": map[string]any{
			"conv": conv, "user": strconv.FormatInt(fbid, 10), "online": st.online, "last_active": st.last,
		}})
	}
	p.mu.Unlock()
	for _, ev := range out {
		emit("presence", ev)
	}
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
