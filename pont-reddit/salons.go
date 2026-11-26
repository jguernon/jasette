package main

// Les salons Matrix de Reddit : leur état (tenu à jour par /sync et gardé
// dans salons.json, puisque /sync ne redonne que ce qui change), leur
// classement (à deux, groupe, communauté) et la conversation qu'en voit
// l'interface.
//
// Le classement suit l'événement d'état « com.reddit.chat.type » ({"type":
// …}) : « direct » et « reddit_titled_direct » (à deux), « private_group »
// (groupe). Les chats de subreddit ne sont décrits par aucune référence
// publique : un salon d'un autre type, ou dont un état com.reddit.* nomme un
// subreddit, est pris pour un salon de communauté (onglet Groupes, jamais la
// boîte de réception). Le pont note dans son journal chaque type inconnu
// qu'il rencontre, pour qu'on puisse ajuster.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	sorteDirect     = "direct"
	sorteGroupe     = "group"
	sorteCommunaute = "community"
)

// Au plus tant de conversations à deux ou de groupes dans la liste, et de
// salons de communauté.
const (
	maxConvs  = 200
	maxSalons = 300
)

type membre struct {
	Name       string `json:"name,omitempty"`
	Membership string `json:"membership,omitempty"` // join, invite, leave, ban
	Avatar     string `json:"avatar,omitempty"`     // mxc:// ou https://
}

type salon struct {
	ID       string             `json:"id"`
	Type     string             `json:"type,omitempty"` // com.reddit.chat.type
	Name     string             `json:"name,omitempty"`
	Avatar   string             `json:"avatar,omitempty"` // m.room.avatar
	Members  map[string]*membre `json:"members,omitempty"`
	IsDirect bool               `json:"is_direct,omitempty"` // indice de mon invitation
	Invite   bool               `json:"invite,omitempty"`    // une demande de message (invitation, ou salon « peek »)
	Inviter  string             `json:"inviter,omitempty"`
	Hidden   bool               `json:"hidden,omitempty"` // com.reddit.hidden_chat
	Spam     bool               `json:"spam,omitempty"`   // com.reddit.invite_spam_status

	Subreddit     string `json:"subreddit,omitempty"`      // le nom, sans « r/ »
	SubredditIcon string `json:"subreddit_icon,omitempty"` // https://

	LastID     string `json:"last_id,omitempty"`
	LastTS     int64  `json:"last_ts,omitempty"`
	Preview    string `json:"preview,omitempty"`
	LastFromMe bool   `json:"last_from_me,omitempty"`
	ReadTS     int64  `json:"read_ts,omitempty"` // mon dernier accusé de lecture
	Unread     int    `json:"unread,omitempty"`  // unread_notifications.notification_count
	Mentions   int    `json:"mentions,omitempty"`
	PrevBatch  string `json:"prev_batch,omitempty"`
}

// sorte : à deux, groupe ou communauté.
func (s *salon) sorte() string {
	t := strings.ToLower(s.Type)
	switch t {
	case "direct", "reddit_titled_direct":
		return sorteDirect
	case "private_group", "group":
		return sorteGroupe
	}
	if s.Subreddit != "" || strings.Contains(t, "subreddit") || strings.Contains(t, "community") ||
		strings.Contains(t, "channel") || strings.Contains(t, "public") {
		return sorteCommunaute
	}
	if t == "" {
		// Type pas encore connu (une invitation en état réduit) : l'indice
		// is_direct de l'invitation, sinon le nombre de membres.
		if s.IsDirect || s.nbMembres() <= 2 {
			return sorteDirect
		}
		return sorteGroupe
	}
	// Un type inconnu sans subreddit : un groupe (il reste visible, dans la
	// boîte de réception).
	return sorteGroupe
}

func (s *salon) nbMembres() int {
	n := 0
	for _, m := range s.Members {
		if m.Membership == "join" || m.Membership == "invite" {
			n++
		}
	}
	return n
}

// autres : les membres autres que moi (présents ou invités), triés par nom.
func (s *salon) autres(me string) []string {
	var ids []string
	for id, m := range s.Members {
		if id != me && (m.Membership == "join" || m.Membership == "invite" || m.Membership == "") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// nomCompte : « t2_abc » tiré de « @t2_abc:reddit.com » (faute de mieux).
func nomCompte(uid string) string {
	uid = strings.TrimPrefix(uid, "@")
	if i := strings.IndexByte(uid, ':'); i >= 0 {
		uid = uid[:i]
	}
	return uid
}

// propreNom : le nom d'utilisateur Reddit, sans « u/ ».
func propreNom(n string) string {
	n = strings.TrimSpace(n)
	n = strings.TrimPrefix(n, "/")
	n = strings.TrimPrefix(n, "u/")
	return n
}

func (s *salon) nomDe(uid string) string {
	if m := s.Members[uid]; m != nil && m.Name != "" {
		return m.Name
	}
	return ""
}

func (s *salon) unread() bool {
	if s.LastFromMe {
		return false
	}
	if s.Unread > 0 {
		return true
	}
	return s.LastTS > 0 && s.LastTS > s.ReadTS && s.ReadTS > 0
}

// convOut : la conversation vue par l'interface. noms : les noms connus
// des personnes (événements com.reddit.profile), en plus de l'état du salon.
func (s *salon) convOut(me string, noms map[string]string) Conversation {
	nom := func(uid string) string {
		return firstOf(s.nomDe(uid), noms[uid], nomCompte(uid))
	}
	out := Conversation{
		ID:      s.ID,
		Name:    s.Name,
		Preview: s.Preview,
		FromMe:  s.LastFromMe,
		Time:    s.LastTS,
		Unread:  s.unread(),
		Members: []string{},
		Request: s.Invite,
	}
	autres := s.autres(me)
	switch s.sorte() {
	case sorteDirect:
		if out.Name == "" {
			if len(autres) > 0 {
				out.Name = nom(autres[0])
			} else if s.Inviter != "" {
				out.Name = nom(s.Inviter)
			}
		}
	case sorteGroupe:
		out.Group = true
		for _, id := range autres {
			out.Members = append(out.Members, nom(id))
		}
		if out.Name == "" {
			out.Name = strings.Join(out.Members, ", ")
		}
	case sorteCommunaute:
		out.Group = true
		out.Large = true
		out.Kind = "subreddit"
		if s.Subreddit != "" {
			out.Parent = "sr:" + strings.ToLower(s.Subreddit)
			out.ParentName = "r/" + s.Subreddit
		}
		if out.Unread {
			out.Mentions = s.Mentions
		}
		if out.Name == "" {
			out.Name = firstOf(out.ParentName, "Reddit")
		}
	}
	if out.Name == "" {
		out.Name = "Reddit"
	}
	if s.Invite && out.Preview == "" {
		out.Preview = "Demande de message"
	}
	return out
}

// ---- Les événements d'état ----

// noteState : un événement d'état (ou d'état réduit d'invitation). Rend vrai
// si quelque chose de visible a changé.
func (s *salon) noteState(ev mxEvent, me string) bool {
	if ev.StateKey == nil {
		return false
	}
	var c map[string]any
	_ = json.Unmarshal(ev.Content, &c)
	str := func(k string) string {
		v, _ := c[k].(string)
		return v
	}
	switch ev.Type {
	case "com.reddit.chat.type":
		t := str("type")
		if t != "" && t != s.Type {
			s.Type = t
			s.noteSubreddit(c)
			return true
		}
		s.noteSubreddit(c)
	case "m.room.name":
		if n := str("name"); n != s.Name {
			s.Name = n
			return true
		}
	case "m.room.avatar":
		if u := str("url"); u != s.Avatar {
			s.Avatar = u
			return true
		}
	case "m.room.member":
		uid := *ev.StateKey
		if s.Members == nil {
			s.Members = map[string]*membre{}
		}
		m := s.Members[uid]
		if m == nil {
			m = &membre{}
			s.Members[uid] = m
		}
		avant := *m
		m.Membership = str("membership")
		if n := propreNom(str("displayname")); n != "" {
			m.Name = n
		}
		if a := str("avatar_url"); a != "" {
			m.Avatar = a
		}
		if uid == me {
			if d, ok := c["is_direct"].(bool); ok && d {
				s.IsDirect = true
			}
			if m.Membership == "invite" && ev.Sender != me {
				s.Inviter = ev.Sender
			}
		}
		return avant != *m
	default:
		if strings.HasPrefix(ev.Type, "com.reddit.") {
			return s.noteSubreddit(c)
		}
	}
	return false
}

// noteSubreddit : un nom de subreddit dans un état com.reddit.* (supposé :
// aucune référence publique ne décrit ces salons).
func (s *salon) noteSubreddit(c map[string]any) bool {
	nom := ""
	for _, k := range []string{"subreddit_name", "subreddit_display_name", "sr_name", "subreddit", "community_name"} {
		switch v := c[k].(type) {
		case string:
			nom = v
		case map[string]any:
			for _, kk := range []string{"name", "display_name", "displayName"} {
				if n, ok := v[kk].(string); ok && n != "" {
					nom = n
				}
			}
			if ic, ok := v["icon"].(string); ok && strings.HasPrefix(ic, "https://") {
				s.SubredditIcon = ic
			}
		}
		if nom != "" {
			break
		}
	}
	for _, k := range []string{"subreddit_icon", "icon_url", "community_icon"} {
		if ic, ok := c[k].(string); ok && strings.HasPrefix(ic, "https://") {
			s.SubredditIcon = ic
		}
	}
	nom = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(nom), "/"), "r/")
	if nom == "" || strings.HasPrefix(nom, "t5_") || nom == s.Subreddit {
		return false
	}
	s.Subreddit = nom
	return true
}

// ---- salons.json ----

func salonsPath() string { return filepath.Join(rdDir(), "salons.json") }

func loadSalons() map[string]*salon {
	out := map[string]*salon{}
	b, err := os.ReadFile(salonsPath())
	if err != nil {
		return out
	}
	var l []*salon
	if json.Unmarshal(b, &l) != nil {
		return out
	}
	for _, s := range l {
		if s != nil && s.ID != "" {
			out[s.ID] = s
		}
	}
	return out
}

func saveSalons(m map[string]*salon) {
	l := make([]*salon, 0, len(m))
	for _, s := range m {
		l = append(l, s)
	}
	b, err := json.Marshal(l)
	if err != nil {
		return
	}
	if err := writeWhole(salonsPath(), b); err != nil {
		log.Warn().Err(err).Msg("salons.json")
	}
}

// trierConvs : les conversations à deux et les groupes d'abord (du plus
// récent), puis les salons de communauté, chacun sous son plafond. Les
// salons cachés (com.reddit.hidden_chat) et les demandes marquées comme
// indésirables n'y sont pas.
func trierConvs(salons map[string]*salon, me string, noms map[string]string) []Conversation {
	var list, communautes []Conversation
	for _, s := range salons {
		if s.Hidden || s.Spam {
			continue
		}
		c := s.convOut(me, noms)
		if c.Large {
			communautes = append(communautes, c)
		} else {
			list = append(list, c)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time > list[j].Time })
	if len(list) > maxConvs {
		list = list[:maxConvs]
	}
	sort.Slice(communautes, func(i, j int) bool { return communautes[i].Time > communautes[j].Time })
	if len(communautes) > maxSalons {
		communautes = communautes[:maxSalons]
	}
	if list == nil {
		list = []Conversation{}
	}
	return append(list, communautes...)
}
