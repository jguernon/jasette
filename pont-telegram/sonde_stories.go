package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gotd/td/tg"
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
	_, err := os.Stat(filepath.Join(tgDir(), "stories-sonde"))
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
	f, err := os.OpenFile(filepath.Join(tgDir(), "stories-sonde.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

var sondeStoriesUneFois sync.Once

// sondeStoriesTelegram : une seule fois par processus, une minute après
// « ready », stories.getAllStories (liste active, puis liste cachée) : en
// lecture seule (rien n'est marqué vu ; jamais stories.readStories).
func (p *Pont) sondeStoriesTelegram(ctx context.Context) {
	if !sondeStoriesActive() {
		return
	}
	sondeStoriesUneFois.Do(func() {
		go func() {
			for i, cachee := range []bool{false, true} {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(60+20*i) * time.Second):
				}
				api := p.tgAPI()
				if api == nil {
					return
				}
				res, err := api.StoriesGetAllStories(ctx, &tg.StoriesGetAllStoriesRequest{Hidden: cachee})
				if err != nil {
					sondeStoriesNoter("telegram.erreur", map[string]any{"cachee": cachee, "erreur": err.Error()})
					continue
				}
				all, ok := res.(*tg.StoriesAllStories)
				if !ok {
					sondeStoriesNoter("telegram.inchange", map[string]any{"cachee": cachee})
					continue
				}
				users := map[int64]*tg.User{}
				for _, u := range all.Users {
					if uu, ok := u.(*tg.User); ok {
						users[uu.ID] = uu
					}
				}
				chats := map[int64]string{}
				for _, c := range all.Chats {
					switch cc := c.(type) {
					case *tg.Channel:
						chats[cc.ID] = cc.Title + " @" + cc.Username
					case *tg.Chat:
						chats[cc.ID] = cc.Title
					}
				}
				rows := []map[string]any{}
				for _, ps := range all.PeerStories {
					r := map[string]any{"max_lu": ps.MaxReadID}
					var nomUtil string
					switch pe := ps.Peer.(type) {
					case *tg.PeerUser:
						r["peer"] = "user:" + strconv.FormatInt(pe.UserID, 10)
						if u := users[pe.UserID]; u != nil {
							r["nom"] = u.FirstName + " " + u.LastName
							nomUtil = u.Username
							if nomUtil == "" && len(u.Usernames) > 0 {
								nomUtil = u.Usernames[0].Username
							}
							r["contact"] = u.Contact
						}
					case *tg.PeerChannel:
						r["peer"] = "channel:" + strconv.FormatInt(pe.ChannelID, 10)
						r["nom"] = chats[pe.ChannelID]
					case *tg.PeerChat:
						r["peer"] = "chat:" + strconv.FormatInt(pe.ChatID, 10)
					}
					r["username"] = nomUtil
					items := []map[string]any{}
					for _, s := range ps.Stories {
						switch it := s.(type) {
						case *tg.StoryItem:
							items = append(items, map[string]any{"id": it.ID, "date": time.Unix(int64(it.Date), 0).Format(time.RFC3339), "expire": time.Unix(int64(it.ExpireDate), 0).Format(time.RFC3339), "amis_proches": it.CloseFriends})
						case *tg.StoryItemSkipped:
							items = append(items, map[string]any{"id": it.ID, "saute": true, "date": time.Unix(int64(it.Date), 0).Format(time.RFC3339), "expire": time.Unix(int64(it.ExpireDate), 0).Format(time.RFC3339)})
						case *tg.StoryItemDeleted:
							items = append(items, map[string]any{"id": it.ID, "efface": true})
						}
					}
					r["stories"] = items
					if nomUtil != "" && len(items) > 0 {
						if id, ok := items[0]["id"].(int); ok {
							r["lien"] = "https://t.me/" + nomUtil + "/s/" + strconv.Itoa(id)
						}
					}
					rows = append(rows, r)
				}
				sondeStoriesNoter("telegram.getAllStories", map[string]any{"cachee": cachee, "count": all.Count, "has_more": all.HasMore, "pairs": rows})
			}
		}()
	})
}
