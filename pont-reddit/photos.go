package main

// Les photos des personnes et les icônes des subreddits.
//
// Le serveur Matrix de Reddit ne donne aucune photo : ses m.room.member
// n'ont pas d'avatar_url, et son profil Matrix non plus. Le client web les
// lit chez Reddit :
//
//   - les personnes : POST https://www.reddit.com/svc/shreddit/graphql,
//     opération « ChatRedditors » ({"ids": ["t2_…"]}), avec le jeton CSRF
//     et les cookies de la session ; réponse
//     data.redditorsInfoByIds[] = {id, icon: {url}} ;
//   - les subreddits : GET https://www.reddit.com/r/<nom>/about.json
//     (data.community_icon, sinon data.icon_img).
//
// Prudence : seulement pour les personnes des conversations de la liste,
// les auteurs des messages chargés et moi ; chaque personne au plus une
// fois par jour (cache sur disque, <données>/reddit/photos.json, 0600, « sans
// photo » compris) ; les personnes groupées par requête (25 au plus) ; au
// plus 10 requêtes par minute ; après un refus, une pause qui double (5 min
// à 1 h). Le journal (Info) donne un bilan par requête, sans adresse ni
// jeton.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	photosParRequete = 25
	photosParMinute  = 10
	photosPauseMin   = 5 * time.Minute
	photosPauseMax   = time.Hour
)

func photosPath() string { return filepath.Join(rdDir(), "photos.json") }

// photoConnue : l'adresse d'une photo (vide : la personne n'en a pas) et le
// jour où Reddit l'a donnée.
type photoConnue struct {
	URL  string `json:"url,omitempty"`
	Jour string `json:"jour"`
}

// annuaire : le cache des photos et la file des demandes. Clés : l'id
// Matrix d'une personne (« @t2_…:reddit.com »), ou « sr:<nom> » pour un
// subreddit.
type annuaire struct {
	mu       sync.Mutex
	connues  map[string]photoConnue
	charge   bool
	file     []string
	enFile   map[string]bool
	srNoms   map[string]string // « sr:<minuscules> » → nom tel quel
	reveil   chan struct{}
	recentes []time.Time // les requêtes de la dernière minute
	pause    time.Duration
	reprise  time.Time

	// Le bilan depuis le lancement (journal).
	demandees, obtenues, sansPhoto, refusees int

	maintenant func() time.Time // pour les essais
}

func nouvelAnnuaire() *annuaire {
	return &annuaire{
		connues:    map[string]photoConnue{},
		enFile:     map[string]bool{},
		srNoms:     map[string]string{},
		reveil:     make(chan struct{}, 1),
		maintenant: time.Now,
	}
}

func (a *annuaire) jour() string { return a.maintenant().Format("2006-01-02") }

// charger : le cache du disque, une fois. À appeler avec a.mu.
func (a *annuaire) charger() {
	if a.charge {
		return
	}
	a.charge = true
	b, err := os.ReadFile(photosPath())
	if err != nil {
		return
	}
	var m map[string]photoConnue
	if json.Unmarshal(b, &m) == nil {
		for k, v := range m {
			a.connues[k] = v
		}
	}
}

// garder : le cache sur le disque (0600). À appeler avec a.mu.
func (a *annuaire) garder() {
	b, err := json.Marshal(a.connues)
	if err == nil {
		_ = writeWhole(photosPath(), b)
	}
}

// url : l'adresse connue (même d'un autre jour), et si elle est à jour.
func (a *annuaire) url(cle string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.charger()
	c, ok := a.connues[cle]
	return c.URL, ok && c.Jour == a.jour()
}

// demander : met en file les clés pas encore demandées aujourd'hui.
// nomSr : le nom d'un subreddit tel qu'il s'écrit (pour « sr:… »).
func (a *annuaire) demander(cles []string, nomSr map[string]string) int {
	a.mu.Lock()
	a.charger()
	n := 0
	for _, k := range cles {
		if k == "" || a.enFile[k] {
			continue
		}
		if c, ok := a.connues[k]; ok && c.Jour == a.jour() {
			continue
		}
		if strings.HasPrefix(k, "sr:") {
			if nomSr[k] == "" {
				continue
			}
			a.srNoms[k] = nomSr[k]
		} else if t2De(k) == "" {
			continue
		}
		a.enFile[k] = true
		a.file = append(a.file, k)
		n++
	}
	a.mu.Unlock()
	if n > 0 {
		select {
		case a.reveil <- struct{}{}:
		default:
		}
	}
	return n
}

// prochain : le lot suivant (des personnes, ou un subreddit), sans le
// retirer de la file. Vide si rien n'attend.
func (a *annuaire) prochain() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var lot []string
	for _, k := range a.file {
		if strings.HasPrefix(k, "sr:") {
			if len(lot) == 0 {
				return []string{k}
			}
			continue
		}
		lot = append(lot, k)
		if len(lot) == photosParRequete {
			break
		}
	}
	return lot
}

// attente : combien attendre avant la prochaine requête (pause après un
// refus, 10 par minute au plus). Note la requête si on peut y aller.
func (a *annuaire) attente() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.maintenant()
	if now.Before(a.reprise) {
		return a.reprise.Sub(now)
	}
	garde := a.recentes[:0]
	for _, t := range a.recentes {
		if now.Sub(t) < time.Minute {
			garde = append(garde, t)
		}
	}
	a.recentes = garde
	if len(a.recentes) >= photosParMinute {
		return a.recentes[0].Add(time.Minute).Sub(now)
	}
	a.recentes = append(a.recentes, now)
	return 0
}

// resultat : la réponse d'un lot. trouvees : clé → adresse (vide : sans
// photo). err : le lot reste en file, et une pause commence.
func (a *annuaire) resultat(lot []string, trouvees map[string]string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.demandees += len(lot)
	if err != nil {
		a.refusees += len(lot)
		if a.pause == 0 {
			a.pause = photosPauseMin
		} else if a.pause *= 2; a.pause > photosPauseMax {
			a.pause = photosPauseMax
		}
		a.reprise = a.maintenant().Add(a.pause)
		return
	}
	a.pause = 0
	j := a.jour()
	for _, k := range lot {
		u := trouvees[k]
		if u != "" {
			a.obtenues++
		} else {
			a.sansPhoto++
		}
		a.connues[k] = photoConnue{URL: u, Jour: j}
		delete(a.enFile, k)
	}
	reste := a.file[:0]
	for _, k := range a.file {
		if a.enFile[k] {
			reste = append(reste, k)
		}
	}
	a.file = reste
	a.garder()
}

func (a *annuaire) oublier() {
	a.mu.Lock()
	a.connues = map[string]photoConnue{}
	a.file, a.enFile = nil, map[string]bool{}
	a.charge = true
	a.mu.Unlock()
	_ = os.Remove(photosPath())
}

// t2De : « @t2_abc:reddit.com » → « t2_abc » (sinon "").
func t2De(uid string) string {
	l := strings.TrimPrefix(uid, "@")
	if i := strings.IndexByte(l, ':'); i > 0 {
		l = l[:i]
	}
	if !strings.HasPrefix(l, "t2_") || len(l) < 4 || strings.Trim(l[3:], "0123456789abcdefghijklmnopqrstuvwxyz") != "" {
		return ""
	}
	return l
}

// ---- Les requêtes ----

// requeteWeb : une requête vers www.reddit.com avec la session (cookies,
// agent du navigateur), au rythme REST. Les cookies renouvelés en chemin
// sont repris.
func (p *Pont) requeteWeb(ctx context.Context, method, chemin string, corps []byte) ([]byte, error) {
	p.mu.Lock()
	if p.sf == nil {
		p.mu.Unlock()
		return nil, errors.New("pas de session")
	}
	cookies := map[string]string{}
	for k, v := range p.sf.Cookies {
		cookies[k] = v
	}
	p.mu.Unlock()
	var body io.Reader
	if corps != nil {
		body = bytes.NewReader(corps)
	}
	req, err := http.NewRequestWithContext(ctx, method, redditBase+chemin, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", redditBase+"/chat/")
	req.Header.Set("Cookie", cookieHeader(cookies))
	if corps != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", redditBase)
		req.Header.Set("X-Reddit-Client-Version", firstOf(cookies["jasette_client_version"], clientVersionDefaut))
	}
	resp, err := p.webCli.Do(req)
	if err != nil {
		return nil, errors.New("réseau")
	}
	defer resp.Body.Close()
	frais := map[string]string{}
	noteSetCookies(frais, resp)
	if len(frais) > 0 {
		p.mu.Lock()
		if p.sf != nil {
			for k, v := range frais {
				p.sf.Cookies[k] = v
			}
		}
		p.mu.Unlock()
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return b, nil
}

// iconesPersonnes : ChatRedditors pour un lot d'ids Matrix.
func (p *Pont) iconesPersonnes(ctx context.Context, lot []string) (map[string]string, error) {
	p.mu.Lock()
	csrf := ""
	if p.sf != nil {
		csrf = p.sf.Cookies["csrf_token"]
	}
	p.mu.Unlock()
	if csrf == "" {
		return nil, errors.New("pas de cookie csrf_token")
	}
	parT2 := map[string]string{}
	var ids []string
	for _, k := range lot {
		t2 := t2De(k)
		parT2[t2] = k
		ids = append(ids, t2)
	}
	corps, _ := json.Marshal(map[string]any{
		"operation":  "ChatRedditors",
		"variables":  map[string]any{"ids": ids, "includeRedditorAttributes": true},
		"csrf_token": csrf,
	})
	b, err := p.requeteWeb(ctx, http.MethodPost, "/svc/shreddit/graphql", corps)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data *struct {
			Redditors []struct {
				ID   string `json:"id"`
				Icon *struct {
					URL string `json:"url"`
				} `json:"icon"`
			} `json:"redditorsInfoByIds"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(b, &r) != nil {
		return nil, errors.New("réponse illisible")
	}
	if len(r.Errors) > 0 || r.Data == nil {
		return nil, errors.New("requête GraphQL refusée")
	}
	out := map[string]string{}
	for _, x := range r.Data.Redditors {
		if k := parT2[x.ID]; k != "" && x.Icon != nil && hoteImageSur(x.Icon.URL) {
			out[k] = x.Icon.URL
		}
	}
	return out, nil
}

// iconeSubreddit : /r/<nom>/about.json. Un subreddit privé ou banni (403,
// 404) n'a simplement pas d'icône.
func (p *Pont) iconeSubreddit(ctx context.Context, cle, nom string) (map[string]string, error) {
	b, err := p.requeteWeb(ctx, http.MethodGet, "/r/"+url.PathEscape(nom)+"/about.json?raw_json=1", nil)
	if err != nil {
		if strings.HasPrefix(err.Error(), "HTTP 40") && err.Error() != "HTTP 401" && err.Error() != "HTTP 429" {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var r struct {
		Data struct {
			CommunityIcon string `json:"community_icon"`
			IconImg       string `json:"icon_img"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &r) != nil {
		return nil, errors.New("réponse illisible")
	}
	u := html.UnescapeString(firstOf(r.Data.CommunityIcon, r.Data.IconImg))
	if !hoteImageSur(u) {
		u = ""
	}
	return map[string]string{cle: u}, nil
}

// bouclePhotos : vide la file des photos, au rythme permis, tant que la
// session dure.
func (p *Pont) bouclePhotos(ctx context.Context) {
	a := p.photos
	for {
		lot := a.prochain()
		if len(lot) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-a.reveil:
			}
			continue
		}
		if d := a.attente(); d > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
			continue
		}
		var trouvees map[string]string
		var err error
		if strings.HasPrefix(lot[0], "sr:") {
			a.mu.Lock()
			nom := a.srNoms[lot[0]]
			a.mu.Unlock()
			trouvees, err = p.iconeSubreddit(ctx, lot[0], nom)
		} else {
			trouvees, err = p.iconesPersonnes(ctx, lot)
		}
		if ctx.Err() != nil {
			return
		}
		a.resultat(lot, trouvees, err)
		a.mu.Lock()
		ev := log.Info().Int("demandees", len(lot)).Int("obtenues", len(nonVides(trouvees))).
			Int("total_demandees", a.demandees).Int("total_obtenues", a.obtenues).
			Int("total_sans_photo", a.sansPhoto).Int("total_refusees", a.refusees)
		if err != nil {
			ev = ev.Str("refus", erreurCourte(err)).Dur("pause", a.pause)
		}
		a.mu.Unlock()
		quoi := "photos Reddit : bilan"
		if strings.HasPrefix(lot[0], "sr:") {
			quoi = "icône de subreddit : bilan"
		}
		ev.Msg(quoi)
		if err == nil {
			p.photosArrivees(ctx, trouvees)
		}
	}
}

func nonVides(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// photosArrivees : les photos obtenues vont où on les attend (conversation
// à deux, auteur d'un groupe, moi, salons d'un subreddit).
func (p *Pont) photosArrivees(ctx context.Context, trouvees map[string]string) {
	var convs []Conversation
	var auteurs []string
	moi := ""
	p.mu.Lock()
	for k, u := range trouvees {
		if u == "" {
			continue
		}
		if strings.HasPrefix(k, "sr:") {
			for _, s := range p.salons {
				if s.sorte() == sorteCommunaute && "sr:"+strings.ToLower(s.Subreddit) == k {
					s.SubredditIcon = u
					p.sale = true
				}
			}
			continue
		}
		p.icones[k] = u
		if k == p.me {
			moi = u
		}
		if p.auteursVoulus[k] {
			auteurs = append(auteurs, k)
		}
	}
	for _, s := range p.salons {
		if s.Hidden || s.Spam {
			continue
		}
		vise := false
		if s.sorte() == sorteDirect {
			for _, uid := range s.autres(p.me) {
				if trouvees[uid] != "" {
					vise = true
				}
			}
		} else if s.sorte() == sorteCommunaute && trouvees["sr:"+strings.ToLower(s.Subreddit)] != "" {
			vise = true
		}
		if vise {
			convs = append(convs, s.convOut(p.me, p.noms))
		}
	}
	p.mu.Unlock()
	if len(convs) > 0 {
		p.fetchAvatars(convs)
	}
	for _, uid := range auteurs {
		p.envoyerAuteur(ctx, uid, trouvees[uid])
	}
	if moi != "" {
		if path := p.image(ctx, moi); path != "" {
			emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
		}
	}
}

// envoyerAuteur : la photo d'un auteur (sender_avatar), une fois par adresse.
func (p *Pont) envoyerAuteur(ctx context.Context, uid, u string) {
	p.mu.Lock()
	deja := cleImage(p.avatars["@"+uid]) == cleImage(u)
	p.mu.Unlock()
	if deja {
		return
	}
	if path := p.image(ctx, u); path != "" {
		p.mu.Lock()
		p.avatars["@"+uid] = u
		p.mu.Unlock()
		emit("sender_avatar", map[string]any{"service": serviceName(), "id": uid, "path": path})
	}
}
