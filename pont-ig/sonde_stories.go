package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

// Sonde « stories actives » (étude de faisabilité) : désactivée par défaut.
//
// Active seulement si JASETTE_STORIES_SONDE=1 ou si le fichier
// <dossier du pont>/stories-sonde existe. Elle écrit, en 0600, dans
// <dossier du pont>/stories-sonde.log, une ligne JSON par observation :
// qui a une story active (ids, nom, heures), jamais le média ni le texte.
// Elle ne marque rien comme vu.

func sondeStoriesActive() bool {
	if os.Getenv("JASETTE_STORIES_SONDE") == "1" {
		return true
	}
	_, err := os.Stat(filepath.Join(igDir(), "stories-sonde"))
	return err == nil
}

var sondeStoriesMu sync.Mutex

func sondeStoriesNoter(sorte string, v any) {
	sondeStoriesMu.Lock()
	defer sondeStoriesMu.Unlock()
	b, err := json.Marshal(map[string]any{"t": time.Now().Format(time.RFC3339), "sorte": sorte, "v": v})
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(igDir(), "stories-sonde.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

var sondeStoriesUneFois sync.Once

// sondeStoriesIG : une seule fois par processus, deux minutes après la
// connexion : GET /api/v1/feed/reels_tray/ (le plateau des stories du site
// web). S'il échoue, un seul /api/v1/feed/reels_media/ pour les contacts des
// conversations. Lecture seule : jamais media/seen, aucun média téléchargé.
func (p *Pont) sondeStoriesIG(ctx context.Context) {
	if !sondeStoriesActive() {
		return
	}
	sondeStoriesUneFois.Do(func() {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Minute):
			}
			cli := p.client()
			if cli == nil || cli.GetHTTP() == nil {
				sondeStoriesNoter("instagram.erreur", "pas de client")
				return
			}
			if p.sondeTrayIG(ctx, cli) {
				return
			}
			time.Sleep(15 * time.Second)
			p.sondeReelsMediaIG(ctx, cli)
		}()
	})
}

type sondeTrayUser struct {
	Pk       json.Number `json:"pk"`
	Username string      `json:"username"`
	FullName string      `json:"full_name"`
}

func (p *Pont) sondeTrayIG(ctx context.Context, cli *instameow.Client) bool {
	h := cli.GetHTTP()
	en := h.BuildHeaders(true, false)
	en.Set("x-requested-with", "XMLHttpRequest")
	en.Set("referer", cli.GetEndpoint("base_url")+"/")
	en.Set("sec-fetch-dest", "empty")
	en.Set("sec-fetch-mode", "cors")
	en.Set("sec-fetch-site", "same-origin")
	resp, corps, err := h.MakeRequestOnce(ctx, "https://www.instagram.com/api/v1/feed/reels_tray/", "GET", en, nil, types.NONE)
	if err != nil {
		sondeStoriesNoter("instagram.reels_tray.erreur", err.Error())
		return false
	}
	statut := 0
	if resp != nil {
		statut = resp.StatusCode
		cli.GetCookies().UpdateFromResponse(resp)
	}
	var r struct {
		Status string `json:"status"`
		Tray   []struct {
			ID              any           `json:"id"`
			LatestReelMedia int64         `json:"latest_reel_media"`
			ExpiringAt      int64         `json:"expiring_at"`
			Seen            int64         `json:"seen"`
			MediaCount      int           `json:"media_count"`
			HasBesties      bool          `json:"has_besties_media"`
			Muted           bool          `json:"muted"`
			ReelType        string        `json:"reel_type"`
			User            sondeTrayUser `json:"user"`
		} `json:"tray"`
	}
	if err := json.Unmarshal(corps, &r); err != nil || r.Tray == nil {
		apercu := string(corps)
		if len(apercu) > 200 {
			apercu = apercu[:200]
		}
		sondeStoriesNoter("instagram.reels_tray.echec", map[string]any{"statut": statut, "debut": apercu})
		return false
	}
	p.mu.Lock()
	rows := []map[string]any{}
	for _, t := range r.Tray {
		row := map[string]any{
			"pk": t.User.Pk.String(), "username": t.User.Username, "nom": t.User.FullName,
			"derniere": time.Unix(t.LatestReelMedia, 0).Format(time.RFC3339), "vue_jusqua": t.Seen,
			"non_vue": t.Seen < t.LatestReelMedia, "n": t.MediaCount, "amis_proches": t.HasBesties, "muet": t.Muted, "type": t.ReelType,
			"lien": "https://www.instagram.com/stories/" + t.User.Username + "/",
		}
		if fbid := p.byUserIGID[t.User.Pk.String()]; fbid != 0 {
			row["conv"] = p.convOfContact(fbid)
		}
		rows = append(rows, row)
	}
	p.mu.Unlock()
	sondeStoriesNoter("instagram.reels_tray", map[string]any{"statut": statut, "n": len(rows), "plateau": rows})
	return true
}

func (p *Pont) sondeReelsMediaIG(ctx context.Context, cli *instameow.Client) {
	p.mu.Lock()
	ids := []string{}
	for igid := range p.byUserIGID {
		if len(ids) >= 30 {
			break
		}
		ids = append(ids, igid)
	}
	p.mu.Unlock()
	if len(ids) == 0 {
		sondeStoriesNoter("instagram.reels_media", "aucun contact")
		return
	}
	res, err := cli.FetchReel(ctx, ids, "")
	if err != nil {
		sondeStoriesNoter("instagram.reels_media.erreur", err.Error())
		return
	}
	rows := []map[string]any{}
	for id, r := range res.Reels {
		row := map[string]any{"reel": id, "derniere": time.Unix(int64(r.LatestReelMedia), 0).Format(time.RFC3339), "vue_jusqua": r.Seen, "n": r.MediaCount}
		if r.User != nil {
			row["username"], row["nom"] = r.User.Username, r.User.FullName
			row["lien"] = "https://www.instagram.com/stories/" + r.User.Username + "/"
		}
		rows = append(rows, row)
	}
	sondeStoriesNoter("instagram.reels_media", map[string]any{"demandes": len(ids), "actifs": len(rows), "reels": rows, "_": strconv.Itoa(len(res.ReelsMedia))})
}
