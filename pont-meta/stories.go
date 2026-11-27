package main

// Les stories Facebook des contacts (PROTOCOLE.md, §5.15) : qui a une story
// active, pour un cercle autour de son avatar. Jasette n'affiche pas la
// story, ne télécharge aucun média et ne marque jamais rien comme vu : le lien
// https://www.facebook.com/stories/<id> l'ouvre dans le navigateur ou l'app
// Facebook.
//
// La synchro de Messenger ne les donne pas (storyContactSyncFromBucket arrive
// vide). Il faut le plateau des stories de facebook.com : une requête GraphQL
// web, en lecture, avec la session du pont (une session « Avec Facebook » :
// celle de messenger.com ne parle pas à facebook.com). Son doc_id change avec
// le site : il n'y a pas de valeur par défaut, il se règle sans recompiler dans
// <données>/messenger/stories-docs.json :
//
//	{"nom":"StoriesTrayRectangularQuery","doc_id":"…","variables":{…}}
//
// Seulement si le réglage `stories` est activé (JASETTE_STORIES=1 au
// lancement, puis la commande `stories`), au plus toutes les 20 minutes.
// Sinon, l'événement `stories` dit pourquoi (`indispo`), une fois.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const intervalleStories = 20 * time.Minute

func storiesEnv() bool { return os.Getenv("JASETTE_STORIES") == "1" }

type storiesEtat struct {
	on   bool
	lues time.Time
	now  chan struct{}
}

type docStories struct {
	Nom       string          `json:"nom"`
	DocID     string          `json:"doc_id"`
	Variables json.RawMessage `json:"variables"`
}

// lireDocStories : <données>/messenger/stories-docs.json, lu à chaque fois.
func lireDocStories() (docStories, bool) {
	var d docStories
	b, err := os.ReadFile(filepath.Join(metaDir(), "stories-docs.json"))
	if err != nil || json.Unmarshal(b, &d) != nil {
		return d, false
	}
	d.DocID, d.Nom = strings.TrimSpace(d.DocID), strings.TrimSpace(d.Nom)
	if d.Nom == "" {
		d.Nom = "StoriesTrayRectangularQuery"
	}
	if len(bytes.TrimSpace(d.Variables)) == 0 {
		d.Variables = json.RawMessage(`{}`)
	}
	return d, d.DocID != ""
}

func (p *Pont) setStories(on bool) {
	p.mu.Lock()
	p.stories.on = on
	if !on {
		p.stories.lues = time.Time{}
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
			log.Warn().Err(err).Int("échecs", echecs).Msg("stories Facebook")
			if echecs >= 3 {
				pause = time.Now().Add(time.Hour)
				echecs = 0
			}
		} else {
			echecs = 0
		}
	}
}

// emitStoriesIndispo : aucune story Facebook, et pourquoi (texte pour
// l'utilisateur).
func emitStoriesIndispo(raison string) {
	emit("stories", map[string]any{"items": []any{}, "time": time.Now().UnixMilli(), "indispo": raison})
}

func (p *Pont) lireStories() error {
	cli := p.client()
	if cli == nil || cli.GetHTTP() == nil {
		return errors.New("pas de client")
	}
	if !cli.GetPlatform().IsViaFacebook() {
		emitStoriesIndispo("Pour les stories Facebook, reconnecte Messenger « Avec Facebook ».")
		return nil
	}
	doc, ok := lireDocStories()
	if !ok {
		emitStoriesIndispo("Les stories Facebook ne sont pas encore réglées sur le serveur (stories-docs.json).")
		return nil
	}
	corps, err := p.graphQLStories(doc)
	if err != nil {
		return err
	}
	stories, err := analyserStoriesFB(corps)
	if err != nil {
		return err
	}
	maint := time.Now().UnixMilli()
	items := []map[string]any{}
	p.mu.Lock()
	if !p.stories.on {
		p.mu.Unlock()
		return nil
	}
	for _, s := range stories {
		id, err := strconv.ParseInt(s.Owner, 10, 64)
		if err != nil || id == 0 || id == p.me {
			continue
		}
		if s.Until == 0 {
			s.Until = maint + 24*3600*1000
		}
		if s.Until <= maint {
			continue
		}
		conv := p.convOfContact(id)
		if conv == "" {
			continue
		}
		items = append(items, map[string]any{
			"conv": conv, "user": s.Owner, "latest": s.Latest, "until": s.Until, "seen": s.Seen,
			"link": "https://www.facebook.com/stories/" + s.Owner, "service": "facebook",
		})
	}
	p.mu.Unlock()
	log.Info().Int("plateau", len(stories)).Int("conversations", len(items)).Msg("stories Facebook")
	emit("stories", map[string]any{"items": items, "time": maint})
	return nil
}

// graphQLStories : la requête du plateau, une seule fois.
func (p *Pont) graphQLStories(doc docStories) ([]byte, error) {
	return p.graphQLDoc(p.ctx, doc.Nom, doc.DocID, doc.Variables)
}

// storyFB : un « bucket » du plateau : la personne et ses stories.
type storyFB struct {
	Owner  string
	Latest int64 // ms (0 : inconnue)
	Until  int64 // ms (0 : inconnue)
	Seen   bool
}

// analyserStoriesFB : la réponse change de forme avec le site ; on cherche
// donc, partout, les objets qui ont un `story_bucket_owner` (ou `owner`
// d'un nœud StoryBucket) avec un id : la personne. Sous ce nœud : l'heure la
// plus récente (`creation_time`, en secondes), l'échéance (`expiration_time`)
// et « déjà vu » (`is_bucket_seen_by_viewer`, `is_seen_by_viewer`, ou
// `seen_by_viewer`).
func analyserStoriesFB(corps []byte) ([]storyFB, error) {
	corps = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(corps), []byte("for (;;);")))
	// Plusieurs objets JSON à la suite (réponse en flux) : le premier compte.
	d := json.NewDecoder(bytes.NewReader(corps))
	d.UseNumber()
	var racine map[string]any
	if err := d.Decode(&racine); err != nil {
		return nil, errPerimee
	}
	if _, ok := racine["data"]; !ok {
		if _, err := lireGraphQL(corps, "data"); err != nil {
			return nil, err
		}
		return nil, errPerimee
	}
	parOwner := map[string]*storyFB{}
	ordre := []string{}
	var parcourir func(v any)
	parcourir = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				parcourir(e)
			}
		case map[string]any:
			if owner := ownerBucket(x); owner != "" {
				s := parOwner[owner]
				if s == nil {
					s = &storyFB{Owner: owner, Seen: true}
					parOwner[owner] = s
					ordre = append(ordre, owner)
				}
				vu, connu := vuBucket(x)
				latest, until := heuresBucket(x)
				if latest > s.Latest {
					s.Latest = latest
				}
				if until > s.Until {
					s.Until = until
				}
				if !connu || !vu {
					s.Seen = s.Seen && connu && vu
				}
				return
			}
			for _, e := range x {
				parcourir(e)
			}
		}
	}
	parcourir(racine["data"])
	out := make([]storyFB, 0, len(ordre))
	for _, o := range ordre {
		out = append(out, *parOwner[o])
	}
	return out, nil
}

func chaineID(v any) string {
	switch x := v.(type) {
	case string:
		if _, err := strconv.ParseInt(x, 10, 64); err == nil {
			return x
		}
	case json.Number:
		return x.String()
	}
	return ""
}

func ownerBucket(m map[string]any) string {
	if o, ok := m["story_bucket_owner"].(map[string]any); ok {
		return chaineID(o["id"])
	}
	if t, _ := m["__typename"].(string); t == "StoryBucket" {
		if o, ok := m["owner"].(map[string]any); ok {
			return chaineID(o["id"])
		}
	}
	return ""
}

func vuBucket(m map[string]any) (vu, connu bool) {
	for _, k := range []string{"is_bucket_seen_by_viewer", "is_seen_by_viewer", "seen_by_viewer"} {
		if b, ok := m[k].(bool); ok {
			return b, true
		}
	}
	return false, false
}

// heuresBucket : la plus récente `creation_time` et la plus lointaine
// `expiration_time` sous ce nœud (secondes → ms).
func heuresBucket(m map[string]any) (latest, until int64) {
	var parcourir func(v any)
	parcourir = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				parcourir(e)
			}
		case map[string]any:
			for k, e := range x {
				if n, ok := e.(json.Number); ok {
					s, err := n.Int64()
					if err != nil || s <= 0 {
						continue
					}
					switch k {
					case "creation_time":
						latest = max(latest, s*1000)
					case "expiration_time":
						until = max(until, s*1000)
					}
					continue
				}
				parcourir(e)
			}
		}
	}
	parcourir(m)
	if until == 0 && latest > 0 {
		until = latest + 24*3600*1000
	}
	return latest, until
}
