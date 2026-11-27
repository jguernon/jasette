package main

// Les stories des contacts (PROTOCOLE.md, §5.15) : qui a une story Instagram
// active, pour un cercle autour de son avatar. Jasette n'affiche pas la story,
// ne télécharge aucun média et ne marque jamais rien comme vu : le lien
// https://www.instagram.com/stories/<username>/ l'ouvre dans le navigateur ou
// l'app Instagram.
//
// Une seule requête, en lecture : GET /api/v1/feed/reels_tray/ (le plateau des
// stories du site web), avec la session web du pont ; seulement si le réglage
// `stories` est activé (JASETTE_STORIES=1 au lancement, puis la commande
// `stories`), au plus toutes les 20 minutes. Jamais media/seen.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

const intervalleStories = 20 * time.Minute

func storiesEnv() bool { return os.Getenv("JASETTE_STORIES") == "1" }

type storiesEtat struct {
	on       bool
	lues     time.Time
	now      chan struct{}
	derniere map[string]any
}

// setStories : la commande `stories`.
func (p *Pont) setStories(on bool) {
	p.mu.Lock()
	p.stories.on = on
	if !on {
		p.stories.lues = time.Time{}
		p.stories.derniere = nil
	}
	p.mu.Unlock()
	if on {
		p.demanderStories()
	}
}

func (p *Pont) demanderStories() {
	select {
	case p.stories.now <- struct{}{}:
	default:
	}
}

// storiesLoop : le plateau au plus toutes les 20 minutes, seulement si le
// réglage est activé et le pont prêt. Trois échecs de suite : une heure de pause.
func (p *Pont) storiesLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	echecs := 0
	var pause time.Time
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
		case <-p.stories.now:
		}
		p.mu.Lock()
		on, ready, lues := p.stories.on, p.ready, p.stories.lues
		p.mu.Unlock()
		if !on || !ready || time.Now().Before(pause) || time.Since(lues) < intervalleStories {
			continue
		}
		p.mu.Lock()
		p.stories.lues = time.Now()
		p.mu.Unlock()
		if err := p.lireStories(); err != nil {
			echecs++
			log.Warn().Err(err).Int("échecs", echecs).Msg("stories Instagram")
			if echecs >= 3 {
				pause = time.Now().Add(time.Hour)
				echecs = 0
			}
		} else {
			echecs = 0
		}
	}
}

// storyIG : une entrée du plateau.
type storyIG struct {
	User     string
	Username string
	Latest   int64 // ms
	Until    int64 // ms
	Seen     bool
}

// analyserReelsTray : les stories du plateau (sans les comptes mis en
// sourdine ni les entrées sans nom d'utilisateur).
func analyserReelsTray(corps []byte) ([]storyIG, error) {
	var r struct {
		Status string `json:"status"`
		Tray   []struct {
			LatestReelMedia json.Number `json:"latest_reel_media"`
			ExpiringAt      json.Number `json:"expiring_at"`
			Seen            json.Number `json:"seen"`
			Muted           bool        `json:"muted"`
			User            struct {
				Pk       json.Number `json:"pk"`
				PkID     string      `json:"pk_id"`
				Username string      `json:"username"`
			} `json:"user"`
		} `json:"tray"`
	}
	d := json.NewDecoder(bytes.NewReader(corps))
	d.UseNumber()
	if err := d.Decode(&r); err != nil {
		return nil, errors.New("réponse illisible")
	}
	if r.Tray == nil {
		return nil, fmt.Errorf("plateau absent (statut %q)", r.Status)
	}
	out := []storyIG{}
	for _, t := range r.Tray {
		latest, _ := t.LatestReelMedia.Int64()
		if t.Muted || t.User.Username == "" || latest <= 0 {
			continue
		}
		user := t.User.Pk.String()
		if user == "" {
			user = t.User.PkID
		}
		vue, _ := t.Seen.Int64()
		fin, _ := t.ExpiringAt.Int64()
		if fin <= 0 {
			fin = latest + 24*3600
		}
		out = append(out, storyIG{User: user, Username: t.User.Username, Latest: latest * 1000, Until: fin * 1000, Seen: vue >= latest})
	}
	return out, nil
}

func lienStoryIG(username string) string {
	return "https://www.instagram.com/stories/" + username + "/"
}

func (p *Pont) lireStories() error {
	cli := p.client()
	if cli == nil || cli.GetHTTP() == nil {
		return errors.New("pas de client")
	}
	h := cli.GetHTTP()
	en := h.BuildHeaders(true, false)
	en.Set("x-requested-with", "XMLHttpRequest")
	en.Set("referer", cli.GetEndpoint("base_url")+"/")
	en.Set("sec-fetch-dest", "empty")
	en.Set("sec-fetch-mode", "cors")
	en.Set("sec-fetch-site", "same-origin")
	resp, corps, err := h.MakeRequestOnce(p.ctx, "https://www.instagram.com/api/v1/feed/reels_tray/", "GET", en, nil, types.NONE)
	if err != nil {
		return err
	}
	if resp != nil {
		cli.GetCookies().UpdateFromResponse(resp)
		if resp.StatusCode != 200 {
			return fmt.Errorf("statut %d", resp.StatusCode)
		}
	}
	stories, err := analyserReelsTray(corps)
	if err != nil {
		return err
	}
	_, moi := p.identiteIG()
	maint := time.Now().UnixMilli()
	items := []map[string]any{}
	p.mu.Lock()
	if !p.stories.on {
		p.mu.Unlock()
		return nil
	}
	for _, s := range stories {
		if (moi != "" && s.User == moi) || s.Until <= maint {
			continue
		}
		fbid := p.byUserIGID[s.User]
		if fbid == 0 || fbid == p.me {
			continue
		}
		conv := p.convOfContact(fbid)
		if conv == "" {
			continue
		}
		items = append(items, map[string]any{
			"conv": conv, "user": s.User, "username": s.Username, "latest": s.Latest, "until": s.Until,
			"seen": s.Seen, "link": lienStoryIG(s.Username), "service": "instagram",
		})
	}
	ev := map[string]any{"items": items, "time": maint}
	p.stories.derniere = ev
	p.mu.Unlock()
	log.Info().Int("plateau", len(stories)).Int("conversations", len(items)).Msg("stories Instagram")
	emit("stories", ev)
	return nil
}
