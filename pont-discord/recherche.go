package main

// La recherche : des personnes (commande « search ») parmi les amis et les
// gens des conversations connues, et des messages (« search_messages »)
// parmi ceux que le pont a en mémoire. Rien n'est demandé à Discord : pas
// de requête de recherche en rafale.

import (
	"sort"
	"strings"
	"unicode"
)

// plier : minuscules, sans accents (comme `plier` de l'interface).
func plier(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		r = unicode.ToLower(r)
		switch r {
		case 'à', 'â', 'ä', 'á', 'ã', 'å':
			r = 'a'
		case 'é', 'è', 'ê', 'ë':
			r = 'e'
		case 'î', 'ï', 'í', 'ì':
			r = 'i'
		case 'ô', 'ö', 'ó', 'ò', 'õ':
			r = 'o'
		case 'ù', 'û', 'ü', 'ú':
			r = 'u'
		case 'ç':
			r = 'c'
		case 'ñ':
			r = 'n'
		case 'ÿ', 'ý':
			r = 'y'
		case 'œ':
			b.WriteString("oe")
			continue
		case 'æ':
			b.WriteString("ae")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// search : l'id rendu est celui de la personne (à passer à « new »).
func (p *Pont) search(q string) {
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	items := []item{}
	defer func() { emit("search", map[string]any{"query": q, "items": items}) }()
	lq := plier(strings.TrimPrefix(strings.TrimSpace(q), "@"))
	if lq == "" {
		return
	}
	p.mu.Lock()
	me := p.meID()
	type cand struct {
		item
		friend bool
	}
	var found []cand
	for id, u := range p.users {
		if id == me || u == nil || u.Bot {
			continue
		}
		nick := p.nicks[id]
		if !strings.Contains(plier(nick), lq) && !strings.Contains(plier(u.GlobalName), lq) && !strings.Contains(plier(u.Username), lq) {
			continue
		}
		name := firstOf(nick, u.DisplayName())
		if u.Username != "" && u.Username != name {
			name += " (@" + u.Username + ")"
		}
		found = append(found, cand{item{ID: id, Name: name}, p.friends[id]})
	}
	p.mu.Unlock()
	sort.Slice(found, func(i, j int) bool {
		if found[i].friend != found[j].friend {
			return found[i].friend
		}
		return found[i].Name < found[j].Name
	})
	for i, c := range found {
		if i >= 30 {
			break
		}
		items = append(items, c.item)
	}
}

// searchMessages : les messages en mémoire dont le texte contient « text »,
// les plus récents d'abord, au plus « limit ».
func (p *Pont) searchMessages(text string, limit int) {
	items := []map[string]any{}
	defer func() {
		emit("search_messages", map[string]any{"query": text, "items": items})
	}()
	q := plier(strings.TrimSpace(text))
	if q == "" {
		return
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	p.mu.Lock()
	var hits []Message
	for _, l := range p.msgs {
		for _, m := range l {
			if m.Kind != "admin" && strings.Contains(plier(m.Text), q) {
				hits = append(hits, m)
			}
		}
	}
	p.mu.Unlock()
	sort.Slice(hits, func(i, j int) bool { return hits[i].Time > hits[j].Time })
	for i, m := range hits {
		if i >= limit {
			break
		}
		items = append(items, map[string]any{
			"conv": m.Conv, "id": m.ID, "text": m.Text, "sender": m.Sender, "time": m.Time,
			"from_me": m.FromMe, "fromMe": m.FromMe,
		})
	}
}
