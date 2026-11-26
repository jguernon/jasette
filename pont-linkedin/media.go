package main

// Les fichiers : les pièces jointes (téléchargées à la demande, une
// vignette pour l'affichage), les photos des conversations, des auteurs et
// la mienne (paresseusement, au rythme du CDN).
//
// Les images de LinkedIn ont plusieurs tailles (VectorImage) : l'affichage
// prend celle d'environ 1024 px, l'original vient sur demande (`full`). Les
// adresses sont signées et expirent : une adresse refusée est renouvelée en
// relisant le message (une requête).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/mautrix-linkedin/pkg/linkedingo"
)

// hoteSur : les hôtes de fichiers acceptés (LinkedIn, son CDN, et les GIF
// des claviers de GIF).
func hoteSur(u string) bool {
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" {
		return false
	}
	h := strings.ToLower(pu.Hostname())
	switch {
	case h == "www.linkedin.com", h == "linkedin.com", h == "media.licdn.com", strings.HasSuffix(h, ".licdn.com"):
		return true
	case strings.HasSuffix(h, ".giphy.com"), strings.HasSuffix(h, ".tenor.com"), h == "tenor.com":
		return true
	}
	return false
}

// telecharger : un fichier (au plus limit octets), au rythme du CDN. Rend
// le code HTTP d'un refus (0 sans réponse).
func (p *Pont) telecharger(ctx context.Context, cli liAPI, u string, limit int64) ([]byte, string, int, error) {
	if !hoteSur(u) {
		return nil, "", 0, errors.New("adresse de fichier refusée")
	}
	resp, err := cli.DownloadHTTP(ctx, u)
	if err != nil {
		var re *linkedingo.ResponseError
		if errors.As(err, &re) {
			return nil, "", re.StatusCode, fmt.Errorf("HTTP %d", re.StatusCode)
		}
		// Sans l'adresse (elle est signée).
		return nil, "", 0, errors.New("réseau")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", resp.StatusCode, errors.New("lecture interrompue")
	}
	if int64(len(data)) > limit {
		return nil, "", resp.StatusCode, errors.New("fichier trop gros")
	}
	return data, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

func expiree(code int) bool { return code == 401 || code == 403 || code == 404 || code == 410 }

// nomVignette : la vignette d'une pièce, à côté de l'original sans se
// confondre avec lui (« <hash>.* » ne la trouve pas).
func nomVignette(dir, mediaID string) string {
	return filepath.Join(dir, hashName(mediaID)+"-v.jpg")
}

// original : la copie déjà gardée de l'original, ou "".
func original(dir, mediaID string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, hashName(mediaID)+".*"))
	for _, m := range matches {
		if !strings.HasSuffix(m, ".tmp") {
			return m
		}
	}
	return ""
}

// download : une pièce jointe, téléchargée une seule fois. Pour l'affichage,
// la vignette (images) ; l'original sur demande (`full`). Au plus
// dlSimultanes à la fois, une seule fois par fichier en même temps.
func (p *Pont) download(cmd Cmd) {
	dir := filepath.Join(mediaDir(), "pieces")
	ok := func(path string, full bool) {
		emit("media", map[string]any{"media_id": cmd.MediaID, "path": path, "full": full})
	}
	fail := func(err error) {
		log.Warn().Str("err", err.Error()).Msg("téléchargement")
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
	}
	if m := original(dir, cmd.MediaID); m != "" {
		ok(m, true)
		return
	}
	var k mediaKey
	if len(cmd.Key) == 0 || json.Unmarshal(cmd.Key, &k) != nil || k.URL == "" {
		fail(errors.New("clé de pièce jointe illisible"))
		return
	}
	vignette := !cmd.Full && k.Vign != ""
	if vignette {
		if st, err := os.Stat(nomVignette(dir, cmd.MediaID)); err == nil && st.Size() > 0 {
			ok(nomVignette(dir, cmd.MediaID), false)
			return
		}
	}
	cle := cmd.MediaID
	if vignette {
		cle += "\x00v"
	}
	p.mu.Lock()
	if p.dlEnvol[cle] {
		p.mu.Unlock()
		return
	}
	p.dlEnvol[cle] = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.dlEnvol, cle)
		p.mu.Unlock()
	}()
	cli, _, err := p.client()
	if err != nil {
		fail(err)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	select {
	case p.dl <- struct{}{}:
		defer func() { <-p.dl }()
	case <-ctx.Done():
		fail(errors.New("LinkedIn : délai dépassé"))
		return
	}
	adresse := func(k mediaKey) string {
		if vignette && k.Vign != "" {
			return k.Vign
		}
		return k.URL
	}
	data, typ, code, err := p.telecharger(ctx, cli, adresse(k), maxDlFile)
	if err != nil && expiree(code) {
		if neuve, trouvee := p.cleFraiche(ctx, cli, k); trouvee {
			data, typ, _, err = p.telecharger(ctx, cli, adresse(neuve), maxDlFile)
		}
	}
	if err != nil {
		fail(fmt.Errorf("LinkedIn : %w", err))
		return
	}
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(firstOf(typ, cmd.Mime), data))
	if vignette {
		path = nomVignette(dir, cmd.MediaID)
	}
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	ok(path, !vignette)
}

// cleFraiche : la pièce jointe relue dans son message (adresses neuves).
func (p *Pont) cleFraiche(ctx context.Context, cli liAPI, k mediaKey) (mediaKey, bool) {
	p.mu.Lock()
	me := p.me
	p.mu.Unlock()
	if k.Conv == "" || k.Msg == "" || k.Time <= 0 {
		return k, false
	}
	page, err := cli.GetMessagesBefore(ctx, urnConv(k.Conv, me), time.UnixMilli(k.Time+1), 1)
	if err != nil || page == nil {
		return k, false
	}
	for _, lm := range page.Elements {
		if court(lm.EntityURN, me) != k.Msg {
			continue
		}
		l := pieces(lm.RenderContent, k.Conv, k.Msg, k.Time)
		if k.Rang < len(l) {
			var neuve mediaKey
			if json.Unmarshal(l[k.Rang].Key, &neuve) == nil && neuve.URL != "" {
				return neuve, true
			}
		}
	}
	return k, false
}

// ---- Les photos ----

// cleImage : une adresse d'image sans sa requête (la signature change,
// l'image non).
func cleImage(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

// image : une photo gardée sous avatars/ (une seule fois par image).
func (p *Pont) image(ctx context.Context, u string) string {
	if u == "" {
		return ""
	}
	path := filepath.Join(mediaDir(), "avatars", hashName(cleImage(u))+".jpg")
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path
	}
	cli, _, err := p.client()
	if err != nil {
		return ""
	}
	data, _, _, err := p.telecharger(ctx, cli, u, maxAvatar)
	if err != nil {
		log.Info().Str("raison", err.Error()).Msg("photo : téléchargement raté")
		return ""
	}
	if writeWhole(path, data) != nil {
		return ""
	}
	return path
}

// photoMoi : ma photo (avatar_moi).
func (p *Pont) photoMoi() {
	p.mu.Lock()
	u := ""
	if p.sf != nil {
		u = p.sf.Photo
	}
	p.mu.Unlock()
	if u == "" {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 2*time.Minute)
	defer annule()
	if path := p.image(ctx, u); path != "" {
		emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
	}
}

// fetchAvatars : les photos des conversations à deux, une à la fois,
// seulement celles qui ont changé ; 60 au plus.
func (p *Pont) fetchAvatars(list []Conversation) {
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	n := 0
	for _, it := range list {
		if n >= 60 || ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		u := ""
		if c := p.convs[it.ID]; c != nil && !c.Groupe && len(c.Membres) > 0 {
			u = c.Membres[0].Photo
		}
		fait := u == "" || cleImage(p.avatars[it.ID]) == cleImage(u)
		p.mu.Unlock()
		if fait {
			continue
		}
		n++
		if path := p.image(ctx, u); path != "" {
			p.mu.Lock()
			p.avatars[it.ID] = u
			p.mu.Unlock()
			emit("avatar", map[string]any{"conv": it.ID, "path": path})
		}
	}
}

// auteursDe : les photos des auteurs des messages d'un groupe
// (sender_avatar), une fois par personne, 20 par appel au plus.
func (p *Pont) auteursDe(id string, items []Message) {
	type demande struct{ qui, u string }
	var l []demande
	p.mu.Lock()
	c := p.convs[id]
	if c == nil || !c.Groupe {
		p.mu.Unlock()
		return
	}
	vus := map[string]bool{}
	for _, m := range items {
		if m.SenderID == "" || m.FromMe || vus[m.SenderID] {
			continue
		}
		vus[m.SenderID] = true
		if pe := c.membre(m.SenderID); pe != nil && pe.Photo != "" && cleImage(p.avatars["@"+m.SenderID]) != cleImage(pe.Photo) {
			l = append(l, demande{m.SenderID, pe.Photo})
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
			p.avatars["@"+d.qui] = d.u
			p.mu.Unlock()
			emit("sender_avatar", map[string]any{"service": serviceName(), "id": d.qui, "path": path})
		}
	}
}
