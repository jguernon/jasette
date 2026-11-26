package main

// Les fichiers : les images des messages (téléchargées à la demande), les
// photos des conversations et des auteurs (paresseusement, au rythme du CDN).
//
// Les médias Matrix de Reddit se lisent par /_matrix/media/v3/download (la
// version authentifiée v1 répond 404), qui renvoie (308) vers le CDN public
// i.redd.it : Go retire de lui-même l'en-tête Authorization en changeant
// d'hôte. Les photos des personnes viennent des événements
// com.reddit.profile (adresses https, sans leur signature).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// hoteEssai : un faux serveur d'images, pour les essais seulement.
var hoteEssai string

// hôtes d'images acceptés pour une adresse https (photos, images migrées).
func hoteImageSur(u string) bool {
	if hoteEssai != "" && strings.HasPrefix(u, hoteEssai+"/") {
		return true
	}
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" {
		return false
	}
	h := strings.ToLower(pu.Hostname())
	for _, ok := range []string{"i.redd.it", "preview.redd.it", "external-preview.redd.it", "styles.redditmedia.com",
		"www.redditstatic.com", "redditstatic.com", "a.thumbs.redditmedia.com", "b.thumbs.redditmedia.com", "emoji.redditmedia.com"} {
		if h == ok {
			return true
		}
	}
	return strings.HasSuffix(h, ".redd.it") || strings.HasSuffix(h, ".redditmedia.com")
}

// urlMedia : l'adresse de téléchargement d'un média (mxc:// ou https://).
// avecJeton : il faut l'en-tête Authorization (serveur Matrix).
func urlMedia(media string) (string, bool, error) {
	if strings.HasPrefix(media, "mxc://") {
		rest := strings.TrimPrefix(media, "mxc://")
		i := strings.IndexByte(rest, '/')
		if i <= 0 || i == len(rest)-1 || strings.ContainsAny(rest, "?#") || strings.Contains(rest[i+1:], "/") {
			return "", false, errors.New("adresse de média illisible")
		}
		return matrixBase + "/_matrix/media/v3/download/" + url.PathEscape(rest[:i]) + "/" + url.PathEscape(rest[i+1:]), true, nil
	}
	if hoteImageSur(media) {
		return media, false, nil
	}
	return "", false, errors.New("adresse de média refusée")
}

// obtenir : un fichier (au plus limit octets), au rythme du CDN.
//
// Un média mxc:// passe d'abord par /_matrix/media/v3/download (la voie du
// client web de Reddit) ; si Reddit y répond 404, 401 ou 403, une seconde
// fois par la voie authentifiée de Matrix, /_matrix/client/v1/media/download,
// avec le même Bearer.
func (p *Pont) obtenir(ctx context.Context, media string) ([]byte, string, error) {
	return p.obtenirMax(ctx, media, maxDlFile)
}

func (p *Pont) obtenirMax(ctx context.Context, media string, limit int64) ([]byte, string, error) {
	u, jeton, err := urlMedia(media)
	if err != nil {
		return nil, "", err
	}
	data, mime, code, err := p.telecharger(ctx, u, jeton, limit)
	if jeton && (code == http.StatusNotFound || code == http.StatusUnauthorized || code == http.StatusForbidden) {
		v1 := strings.Replace(u, "/_matrix/media/v3/download/", "/_matrix/client/v1/media/download/", 1)
		log.Info().Int("http", code).Msg("média : v3 refusé, essai par /_matrix/client/v1/media/download")
		data, mime, code, err = p.telecharger(ctx, v1, true, limit)
		if err != nil {
			log.Info().Int("http", code).Msg("média : v1 refusé aussi")
		}
	}
	return data, mime, err
}

// telecharger : une requête GET (code HTTP rendu, 0 sans réponse).
func (p *Pont) telecharger(ctx context.Context, u string, jeton bool, limit int64) ([]byte, string, int, error) {
	if err := p.cdn.wait(ctx); err != nil {
		return nil, "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", 0, errors.New("adresse illisible")
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "image/avif,image/webp,image/*,*/*;q=0.8")
	if jeton {
		p.mu.Lock()
		tok := ""
		if p.sf != nil {
			tok = p.sf.Token
		}
		p.mu.Unlock()
		if tok == "" {
			return nil, "", 0, errors.New("Reddit n'est pas connecté")
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	cli := &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("trop de redirections")
		}
		// La redirection ne mène qu'au CDN d'images de Reddit (ou au serveur lui-même).
		if r.URL.Host != req.URL.Host && !hoteImageSur(r.URL.String()) {
			return errors.New("redirection refusée")
		}
		return nil
	}}
	resp, err := cli.Do(req)
	if err != nil {
		// Sans l'adresse (elle peut être signée).
		return nil, "", 0, errors.New("réseau ou redirection refusée")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", resp.StatusCode, err
	}
	if int64(len(data)) > limit {
		return nil, "", resp.StatusCode, errors.New("fichier trop gros")
	}
	return data, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

// download : une image d'un message, téléchargée une seule fois.
func (p *Pont) download(cmd Cmd) {
	dir := filepath.Join(mediaDir(), "pieces")
	if matches, _ := filepath.Glob(filepath.Join(dir, hashName(cmd.MediaID)+".*")); len(matches) > 0 {
		for _, m := range matches {
			if !strings.HasSuffix(m, ".tmp") {
				emit("media", map[string]any{"media_id": cmd.MediaID, "path": m})
				return
			}
		}
	}
	fail := func(err error) {
		log.Warn().Str("err", err.Error()).Msg("téléchargement")
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
	}
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	data, mime, err := p.obtenir(ctx, cmd.MediaID)
	if err != nil {
		fail(fmt.Errorf("Reddit : %w", err))
		return
	}
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(firstOf(cmd.Mime, mime), data))
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	emit("media", map[string]any{"media_id": cmd.MediaID, "path": path})
}

// image : une photo gardée sous avatars/ (une seule fois par adresse).
func (p *Pont) image(ctx context.Context, media string) string {
	// La signature (?…&s=) change d'un jour à l'autre : le fichier n'en dépend pas.
	path := filepath.Join(mediaDir(), "avatars", hashName(cleImage(media))+".png")
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path
	}
	data, _, err := p.obtenirMax(ctx, media, 4<<20)
	if err != nil {
		log.Info().Str("raison", erreurCourte(err)).Msg("photo : téléchargement raté")
		return ""
	}
	if writeWhole(path, data) != nil {
		return ""
	}
	return path
}

// photoDe : l'adresse de la photo d'une conversation. À appeler avec p.mu.
func (p *Pont) photoDe(c Conversation) string {
	s := p.salons[c.ID]
	if s == nil {
		return ""
	}
	if s.Avatar != "" {
		return s.Avatar
	}
	if s.sorte() == sorteDirect {
		for _, uid := range s.autres(p.me) {
			if ic := p.iconeDe(s, uid); ic != "" {
				return ic
			}
		}
	}
	return ""
}

// iconeDe : la photo connue d'une personne (profil vu, cache des photos,
// membre Matrix). À appeler avec p.mu.
func (p *Pont) iconeDe(s *salon, uid string) string {
	if ic := p.icones[uid]; ic != "" {
		return ic
	}
	if u, _ := p.photos.url(uid); u != "" {
		return u
	}
	if s != nil {
		if m := s.Members[uid]; m != nil && m.Avatar != "" {
			return m.Avatar
		}
	}
	return ""
}

// aDemander : les personnes d'une conversation à deux dont la photo n'a pas
// été demandée aujourd'hui. À appeler avec p.mu.
func (p *Pont) aDemander(s *salon) []string {
	var l []string
	for _, uid := range s.autres(p.me) {
		if _, ajour := p.photos.url(uid); !ajour {
			l = append(l, uid)
		}
	}
	return l
}

// photoMoi : ma photo (avatar_moi), du cache, puis de Reddit une fois par jour.
func (p *Pont) photoMoi(ctx context.Context) {
	p.mu.Lock()
	me := p.me
	u := p.iconeDe(nil, me)
	p.mu.Unlock()
	if me == "" {
		return
	}
	if u != "" {
		if path := p.image(ctx, u); path != "" {
			emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
		}
	}
	p.photos.demander([]string{me}, nil)
}

// fetchAvatars : les photos des conversations (personne, groupe) et des
// subreddits, une à la fois, seulement celles qui ont changé ; 60 au plus.
func (p *Pont) fetchAvatars(list []Conversation) {
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	n := 0
	parents := map[string]string{}
	// Ce qui manque, demandé à Reddit (photos.go) : les personnes des 60
	// premières conversations à deux de la liste, et les subreddits.
	var personnes []string
	subreddits := map[string]string{}
	p.mu.Lock()
	directes := 0
	for _, c := range list {
		s := p.salons[c.ID]
		if s == nil {
			continue
		}
		if c.Large && s.Subreddit != "" && c.Parent != "" && s.SubredditIcon == "" {
			if _, ajour := p.photos.url(c.Parent); !ajour {
				subreddits[c.Parent] = s.Subreddit
			}
		} else if !c.Large && s.sorte() == sorteDirect && directes < 60 {
			directes++
			personnes = append(personnes, p.aDemander(s)...)
		}
	}
	p.mu.Unlock()
	cles := personnes
	for k := range subreddits {
		cles = append(cles, k)
	}
	p.photos.demander(cles, subreddits)
	for _, c := range list {
		if n >= 60 || ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		u := ""
		if c.Large {
			if s := p.salons[c.ID]; s != nil && c.Parent != "" {
				ic := s.SubredditIcon
				if ic == "" {
					ic, _ = p.photos.url(c.Parent)
				}
				if ic != "" {
					parents[c.Parent] = ic
				}
			}
		} else {
			u = p.photoDe(c)
		}
		fait := u == "" || cleImage(p.avatars[c.ID]) == cleImage(u)
		p.mu.Unlock()
		if fait {
			continue
		}
		n++
		if path := p.image(ctx, u); path != "" {
			p.mu.Lock()
			p.avatars[c.ID] = u
			p.mu.Unlock()
			emit("avatar", map[string]any{"conv": c.ID, "path": path})
		}
	}
	for parent, u := range parents {
		p.mu.Lock()
		fait := cleImage(p.avatars[parent]) == cleImage(u)
		p.mu.Unlock()
		if fait {
			continue
		}
		if path := p.image(ctx, u); path != "" {
			p.mu.Lock()
			p.avatars[parent] = u
			p.mu.Unlock()
			emit("avatar", map[string]any{"conv": parent, "path": path})
		}
	}
}

// auteursDe : les photos des auteurs des messages d'un groupe ou d'un salon
// de communauté (sender_avatar), une fois par personne, 20 par appel au plus.
func (p *Pont) auteursDe(conv string, items []Message) {
	p.mu.Lock()
	s := p.salons[conv]
	if s == nil || s.sorte() == sorteDirect {
		p.mu.Unlock()
		return
	}
	type demande struct{ uid, u string }
	var l []demande
	var manquent []string
	vus := map[string]bool{}
	for _, m := range items {
		if m.SenderID == "" || m.FromMe || vus[m.SenderID] {
			continue
		}
		vus[m.SenderID] = true
		u := p.iconeDe(s, m.SenderID)
		if _, ajour := p.photos.url(m.SenderID); !ajour && len(manquent) < 40 {
			// Pas encore demandée aujourd'hui : à Reddit, puis sender_avatar.
			p.auteursVoulus[m.SenderID] = true
			manquent = append(manquent, m.SenderID)
		}
		if u != "" && cleImage(p.avatars["@"+m.SenderID]) != cleImage(u) {
			l = append(l, demande{m.SenderID, u})
		}
	}
	p.mu.Unlock()
	p.photos.demander(manquent, nil)
	if len(l) > 20 {
		l = l[:20]
	}
	ctx, annule := context.WithTimeout(p.ctx, 5*time.Minute)
	defer annule()
	for _, d := range l {
		if path := p.image(ctx, d.u); path != "" {
			p.mu.Lock()
			p.avatars["@"+d.uid] = d.u
			p.mu.Unlock()
			emit("sender_avatar", map[string]any{"service": serviceName(), "id": d.uid, "path": path})
		}
	}
}

// cleImage : une adresse d'image sans sa requête (la signature d'une photo
// change d'un jour à l'autre, l'image non).
func cleImage(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}
