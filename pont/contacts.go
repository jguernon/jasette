package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"unicode"
)

// Les contacts du téléphone (ListContacts), gardés 10 minutes en mémoire,
// pour la recherche du « Nouveau message ».
var (
	contactsMu    sync.Mutex
	contactsCache []contactTrouve
	contactsLus   time.Time
)

type contactTrouve struct {
	ID     string `json:"id"` // le numéro : ce que `new` attend
	Name   string `json:"name"`
	Number string `json:"number"`         // le numéro tel qu'affiché
	Path   string `json:"path,omitempty"` // la photo du contact, si connue
	cid    string // l'identifiant du contact chez Google (pour sa photo)
}

// Sans accents ni majuscules.
func plierTexte(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	r, _, err := transform.String(t, strings.ToLower(s))
	if err != nil {
		return strings.ToLower(s)
	}
	return r
}

// contacts : la liste (en cache 10 minutes) ; l'erreur éventuelle de ListContacts
// accompagne l'ancienne liste.
func (p *Pont) contacts() ([]contactTrouve, error) {
	contactsMu.Lock()
	defer contactsMu.Unlock()
	if contactsCache != nil && time.Since(contactsLus) < 10*time.Minute {
		return contactsCache, nil
	}
	cli := p.client()
	if cli == nil {
		return contactsCache, nil
	}
	ctx, annule := context.WithTimeout(context.Background(), 20*time.Second)
	defer annule()
	resp, err := cli.ListContacts(ctx)
	if err != nil {
		return contactsCache, err
	}
	liste := []contactTrouve{}
	vus := map[string]bool{}
	for _, c := range resp.GetContacts() {
		n := c.GetNumber()
		if n == nil {
			continue
		}
		num := n.GetNumber()
		if num == "" {
			num = n.GetNumber2()
		}
		if num == "" || vus[num+"|"+c.GetName()] {
			continue
		}
		vus[num+"|"+c.GetName()] = true
		affiche := n.GetFormattedNumber()
		if affiche == "" {
			affiche = num
		}
		liste = append(liste, contactTrouve{ID: num, Name: c.GetName(), Number: affiche, cid: c.GetContactID()})
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].Name < liste[j].Name })
	contactsCache, contactsLus = liste, time.Now()
	return liste, nil
}

// searchContacts : les contacts du téléphone dont le nom ou le numéro contient q.
func (p *Pont) searchContacts(q string) {
	items := []contactTrouve{}
	pq := plierTexte(q)
	chiffres := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q)
	if len([]rune(pq)) >= 2 {
		liste, err := p.contacts()
		if err != nil {
			emitErr("search", err)
		}
		for _, c := range liste {
			if strings.Contains(plierTexte(c.Name), pq) ||
				(len(chiffres) >= 3 && strings.Contains(strings.Map(func(r rune) rune {
					if r >= '0' && r <= '9' {
						return r
					}
					return -1
				}, c.Number), chiffres)) {
				c.Path = photoConnue(c.cid)
				items = append(items, c)
				if len(items) >= 20 {
					break
				}
			}
		}
	}
	emit("search", map[string]any{"query": q, "items": items})
}

// Les photos des contacts (GetContactThumbnail), écrites avec les autres avatars.
var (
	photosChargeMu sync.Mutex               // un seul chargement à la fois
	photosMu       sync.Mutex               // protège les deux tables
	photosConnues  = map[string]string{}    // identifiant du contact → chemin
	photosTentees  = map[string]time.Time{} // déjà demandées (avec ou sans photo) : pas avant 24 h
)

// Le fichier d'une photo de contact : préfixé pour ne pas croiser les participants.
func cheminPhotoContact(cid string) string {
	return filepath.Join(mediaDir(), "avatars", hashName("contact:"+cid))
}

func fraiche(chemin string) bool {
	st, err := os.Stat(chemin)
	return err == nil && time.Since(st.ModTime()) < 24*time.Hour
}

// photoConnue : la photo déjà sur disque (moins d'un jour), sans rien demander.
func photoConnue(cid string) string {
	if cid == "" {
		return ""
	}
	photosMu.Lock()
	c, ok := photosConnues[cid]
	photosMu.Unlock()
	if ok && fraiche(c) {
		return c
	}
	matches, _ := filepath.Glob(cheminPhotoContact(cid) + ".*")
	for _, m := range matches {
		if !strings.HasSuffix(m, ".tmp") && fraiche(m) {
			photosMu.Lock()
			photosConnues[cid] = m
			photosMu.Unlock()
			return m
		}
	}
	return ""
}

// listContacts : tous les contacts du téléphone, avec leur photo quand il y en a.
// Les photos manquantes sont demandées par lots de 10 ; un lot raté n'arrête pas les autres.
func (p *Pont) listContacts() {
	photosChargeMu.Lock()
	defer photosChargeMu.Unlock()
	base, err := p.contacts()
	if err != nil {
		// Demandé en arrière-plan : au journal (sans rien de personnel), pas à l'écran.
		log.Warn().Err(err).Msg("contacts")
	}
	var aDemander []string
	vus := map[string]bool{}
	for _, c := range base {
		if c.cid == "" || vus[c.cid] {
			continue
		}
		vus[c.cid] = true
		if photoConnue(c.cid) != "" {
			continue
		}
		photosMu.Lock()
		t, deja := photosTentees[c.cid]
		photosMu.Unlock()
		if !deja || time.Since(t) > 24*time.Hour {
			aDemander = append(aDemander, c.cid)
		}
	}
	if cli := p.client(); cli != nil {
		for i := 0; i < len(aDemander); i += 10 {
			lot := aDemander[i:min(i+10, len(aDemander))]
			ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
			resp, err := cli.GetContactThumbnail(ctx, lot...)
			annule()
			if err != nil {
				log.Warn().Err(err).Int("lot", i/10).Msg("photos des contacts")
				continue
			}
			photosMu.Lock()
			maintenant := time.Now()
			for _, cid := range lot {
				photosTentees[cid] = maintenant
			}
			photosMu.Unlock()
			for _, t := range resp.GetThumbnail() {
				data := t.GetData().GetImageBuffer()
				if len(data) == 0 || t.GetIdentifier() == "" {
					continue
				}
				chemin := cheminPhotoContact(t.GetIdentifier()) + extension("", data)
				if writeWhole(chemin, data) == nil {
					photosMu.Lock()
					photosConnues[t.GetIdentifier()] = chemin
					photosMu.Unlock()
				}
			}
		}
	}
	// Une copie : le cache de `contacts()` est partagé.
	items := make([]contactTrouve, 0, len(base))
	for _, c := range base {
		c.Path = photoConnue(c.cid)
		items = append(items, c)
	}
	emit("contacts", map[string]any{"items": items})
}
