package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
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
	_, err := os.Stat(filepath.Join(metaDir(), "stories-sonde"))
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
	f, err := os.OpenFile(filepath.Join(metaDir(), "stories-sonde.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// sondeStoriesMeta : les lignes brutes de storyContactSyncFromBucket, avec
// le nom des contacts connus dont l'id y paraît.
func (p *Pont) sondeStoriesMeta(tbl *table.LSTable, initial bool) {
	rows := []map[string]any{}
	p.mu.Lock()
	for _, r := range tbl.LSStoryContactSyncFromBucket {
		noms := map[string]string{}
		for _, v := range r.Unrecognized {
			var id int64
			switch x := v.(type) {
			case int64:
				id = x
			case float64:
				id = int64(x)
			case string:
				id, _ = strconv.ParseInt(x, 10, 64)
			}
			if c := p.contacts[id]; id != 0 && c != nil {
				noms[strconv.FormatInt(id, 10)] = c.Name
			}
		}
		champs := map[string]string{}
		for k, v := range r.Unrecognized {
			champs[strconv.Itoa(k)] = fmt.Sprintf("%T:%v", v, v)
		}
		rows = append(rows, map[string]any{"champs": champs, "noms": noms})
	}
	p.mu.Unlock()
	sondeStoriesNoter("messenger.storyContactSyncFromBucket", map[string]any{"initial": initial, "lignes": rows})
}
