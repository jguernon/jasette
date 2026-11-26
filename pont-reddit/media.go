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

// hôtes d'images acceptés pour une adresse https (photos, images migrées).
func hoteImageSur(u string) bool {
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
func (p *Pont) obtenir(ctx context.Context, media string, limit int64) ([]byte, string, error) {
	u, jeton, err := urlMedia(media)
	if err != nil {
		return nil, "", err
	}
	if err := p.cdn.wait(ctx); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
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
			return nil, "", errors.New("Reddit n'est pas connecté")
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
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", errors.New("fichier trop gros")
	}
	return data, resp.Header.Get("Content-Type"), nil
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
	data, mime, err := p.obtenir(ctx, cmd.MediaID, maxDlFile)
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
	path := filepath.Join(mediaDir(), "avatars", hashName(media)+".png")
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path
	}
	data, _, err := p.obtenir(ctx, media, 4<<20)
	if err != nil {
		log.Debug().Str("err", err.Error()).Msg("photo")
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
			if ic := p.icones[uid]; ic != "" {
				return ic
			}
			if m := s.Members[uid]; m != nil && m.Avatar != "" {
				return m.Avatar
			}
		}
	}
	return ""
}

// fetchAvatars : les photos des conversations (personne, groupe) et des
// subreddits, une à la fois, seulement celles qui ont changé ; 60 au plus.
func (p *Pont) fetchAvatars(list []Conversation) {
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	n := 0
	parents := map[string]string{}
	for _, c := range list {
		if n >= 60 || ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		u := ""
		if c.Large {
			if s := p.salons[c.ID]; s != nil && c.Parent != "" && s.SubredditIcon != "" {
				parents[c.Parent] = s.SubredditIcon
			}
		} else {
			u = p.photoDe(c)
		}
		fait := u == "" || p.avatars[c.ID] == u
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
		fait := p.avatars[parent] == u
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
	vus := map[string]bool{}
	for _, m := range items {
		if m.SenderID == "" || vus[m.SenderID] {
			continue
		}
		vus[m.SenderID] = true
		u := p.icones[m.SenderID]
		if u == "" {
			if mb := s.Members[m.SenderID]; mb != nil {
				u = mb.Avatar
			}
		}
		if u != "" && p.avatars["@"+m.SenderID] != u {
			l = append(l, demande{m.SenderID, u})
		}
	}
	p.mu.Unlock()
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
