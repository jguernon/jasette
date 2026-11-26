package main

// Les fichiers : pièces jointes (téléchargement et envoi) et avatars.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	mimepkg "mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
)

// La limite d'envoi d'un compte sans Nitro.
const maxUpload = 10 << 20

// Au plus tant d'octets par pièce jointe téléchargée.
const maxDownload = 500 << 20

// cdnGet : un fichier du CDN de Discord, avec les en-têtes du navigateur.
func (p *Pont) cdnGet(ctx context.Context, cli *http.Client, url string, limit int64) ([]byte, int, error) {
	if err := p.cdn.wait(ctx); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range discordgo.DroidDownloadHeaders {
		req.Header.Set(k, v)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(data)) > limit {
		return nil, resp.StatusCode, errors.New("fichier trop gros")
	}
	return data, resp.StatusCode, nil
}

// httpClient : celui de la session (empreinte de Chrome), sinon un simple.
func (p *Pont) httpClient() *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sess != nil {
		return &http.Client{Transport: p.sess.Client.Transport, Timeout: 10 * time.Minute}
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// Les téléchargements de pièces jointes en même temps, au plus.
const dlSimultanes = 3

// La vignette d'affichage : largeur et hauteur au plus, et en deçà de quoi
// l'original suffit (petit fichier, petite image).
const (
	vignetteLargeur = 1024
	vignetteHauteur = 2048
	vignetteSeuil   = 300 << 10
)

// vignetteURL : l'adresse d'un aperçu redimensionné en WebP (proxy
// media.discordapp.net, comme le client web), ou "" si l'original convient
// (pas une image fixe, dimensions inconnues, ou déjà petite).
func vignetteURL(k mediaKey, mime string) string {
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/bmp", "image/avif", "":
	default:
		return "" // GIF animés, vidéos, sons, documents : l'original
	}
	if k.W <= 0 || k.H <= 0 {
		return ""
	}
	if k.W <= vignetteLargeur && k.H <= vignetteHauteur && k.Size > 0 && k.Size <= vignetteSeuil {
		return ""
	}
	u, err := url.Parse(k.URL)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	switch u.Host {
	case "cdn.discordapp.com", "media.discordapp.net":
	default:
		return ""
	}
	if !strings.HasPrefix(u.Path, "/attachments/") && !strings.HasPrefix(u.Path, "/ephemeral-attachments/") {
		return ""
	}
	w, h := k.W, k.H
	if w > vignetteLargeur {
		h = max(1, h*vignetteLargeur/w)
		w = vignetteLargeur
	}
	if h > vignetteHauteur {
		w = max(1, w*vignetteHauteur/h)
		h = vignetteHauteur
	}
	u.Host = "media.discordapp.net"
	q := u.Query() // ex, is, hm : la signature de l'adresse, gardée
	q.Set("format", "webp")
	q.Set("width", strconv.Itoa(w))
	q.Set("height", strconv.Itoa(h))
	u.RawQuery = q.Encode()
	return u.String()
}

// nomVignette : le fichier de la vignette d'une pièce (à côté de l'original,
// sans le confondre avec lui : « <hash>.* » ne le trouve pas).
func nomVignette(dir, mediaID string) string {
	return filepath.Join(dir, hashName(mediaID)+"-v.webp")
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
// une vignette (WebP, 1024 px de large au plus) ; l'original seulement sur
// demande (`full` : ouvrir, enregistrer). Au plus dlSimultanes à la fois,
// une seule fois par fichier en même temps (la réponse `media` sert tous
// ceux qui l'attendent). Les adresses du CDN de Discord expirent (environ un
// jour) : une adresse refusée est renouvelée en relisant le message.
func (p *Pont) download(cmd Cmd) {
	dir := filepath.Join(mediaDir(), "pieces")
	ok := func(path string, full bool) {
		emit("media", map[string]any{"media_id": cmd.MediaID, "path": path, "full": full})
	}
	if m := original(dir, cmd.MediaID); m != "" {
		ok(m, true)
		return
	}
	var k mediaKey
	if len(cmd.Key) == 0 || json.Unmarshal(cmd.Key, &k) != nil || !strings.HasPrefix(k.URL, "https://") {
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": "clé de pièce jointe illisible"})
		return
	}
	vignette := ""
	if !cmd.Full {
		vignette = vignetteURL(k, cmd.Mime)
		if vignette != "" {
			if st, err := os.Stat(nomVignette(dir, cmd.MediaID)); err == nil && st.Size() > 0 {
				ok(nomVignette(dir, cmd.MediaID), false)
				return
			}
		}
	}
	// Un seul téléchargement à la fois par fichier (et par sorte).
	cle := cmd.MediaID
	if vignette != "" {
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
	fail := func(err error) {
		log.Warn().Str("err", err.Error()).Str("media", cmd.MediaID).Msg("téléchargement")
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
	}
	if !trustedCDN(k.URL) {
		fail(errors.New("adresse de pièce jointe refusée"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	select {
	case p.dl <- struct{}{}:
		defer func() { <-p.dl }()
	case <-ctx.Done():
		fail(errors.New("Discord : délai dépassé"))
		return
	}
	cli := p.httpClient()
	adresse := func(brute string) string {
		if vignette == "" {
			return brute
		}
		k2 := k
		k2.URL = brute
		if v := vignetteURL(k2, cmd.Mime); v != "" {
			return v
		}
		return brute
	}
	data, status, err := p.cdnGet(ctx, cli, adresse(k.URL), maxDownload)
	if err != nil && (status == http.StatusForbidden || status == http.StatusNotFound) && k.Channel != "" && k.Message != "" {
		if fresh := p.freshURL(ctx, k); fresh != "" {
			data, status, err = p.cdnGet(ctx, cli, adresse(fresh), maxDownload)
		}
	}
	if err != nil && vignette != "" && status >= 400 && status < 500 && status != http.StatusForbidden && status != http.StatusNotFound && status != http.StatusTooManyRequests {
		// Le proxy refuse ce redimensionnement : l'original.
		vignette = ""
		data, _, err = p.cdnGet(ctx, cli, k.URL, maxDownload)
	}
	if err != nil {
		fail(fmt.Errorf("Discord : %w", err))
		return
	}
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(cmd.Mime, data))
	if vignette != "" {
		path = nomVignette(dir, cmd.MediaID)
	}
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	ok(path, vignette == "")
}

// trustedCDN : seulement les hôtes de fichiers de Discord.
func trustedCDN(u string) bool {
	for _, h := range []string{"https://cdn.discordapp.com/", "https://media.discordapp.net/", "https://images-ext-1.discordapp.net/", "https://images-ext-2.discordapp.net/"} {
		if strings.HasPrefix(u, h) {
			return true
		}
	}
	return false
}

// freshURL relit le message pour une adresse à jour de la pièce jointe.
func (p *Pont) freshURL(ctx context.Context, k mediaKey) string {
	sess, err := p.api()
	if err != nil || p.rest.wait(ctx) != nil {
		return ""
	}
	p.mu.Lock()
	conv := p.convFor(k.Channel)
	p.mu.Unlock()
	dm, err := sess.ChannelMessage(k.Channel, k.Message, referer(conv), discordgo.WithContext(ctx))
	if err != nil {
		return ""
	}
	for _, a := range dm.Attachments {
		if a != nil && a.ID == k.ID && trustedCDN(a.URL) {
			return a.URL
		}
	}
	return ""
}

// ---- L'envoi d'un fichier ----

var uploadID atomic.Int64

func readMedia(path, mime string) ([]byte, string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", fmt.Errorf("fichier introuvable : %w", err)
	}
	if st.Size() > maxUpload {
		return nil, "", fmt.Errorf("fichier trop gros pour Discord (10 Mo au plus sans Nitro)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if mime == "" {
		mime = mimepkg.TypeByExtension(filepath.Ext(path))
	}
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	if i := strings.IndexByte(mime, ';'); i > 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	return data, mime, nil
}

// upload : comme le client web, le fichier part d'abord vers le stockage de
// Discord (adresse préparée par l'API), puis le message le désigne.
func (p *Pont) upload(ctx context.Context, sess *discordgo.Session, conv, path, mime string) (*discordgo.MessageAttachment, []Attachment, error) {
	data, mime, err := readMedia(path, mime)
	if err != nil {
		return nil, nil, err
	}
	name := filepath.Base(path)
	if err := p.rest.wait(ctx); err != nil {
		return nil, nil, err
	}
	isClip := false
	prep, err := sess.ChannelAttachmentCreate(rawChannel(conv), &discordgo.ReqPrepareAttachments{
		Files: []*discordgo.FilePrepare{{
			Size: len(data), Name: name, ID: strconv.FormatInt(uploadID.Add(2), 10),
			IsClip: &isClip, OriginalContentType: mime,
		}},
	}, referer(conv), discordgo.WithContext(ctx))
	if err != nil {
		return nil, nil, explainSend(err)
	}
	if len(prep.Attachments) == 0 {
		return nil, nil, errors.New("Discord n'a pas préparé l'envoi du fichier")
	}
	pr := prep.Attachments[0]
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, pr.UploadURL, bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	for k, v := range discordgo.DroidBaseHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Referer", "https://discord.com/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := sess.Client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("envoi du fichier : %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("envoi du fichier : HTTP %d", resp.StatusCode)
	}
	att := &discordgo.MessageAttachment{ID: "0", Filename: name, UploadedFilename: pr.UploadFilename, OriginalContentType: mime}
	// La copie locale, dans pieces/ : l'interface la trouve sans téléchargement.
	a := Attachment{MediaID: "local:" + hashName(path+time.Now().String()), Mime: mime, Name: name, Size: int64(len(data))}
	if strings.HasPrefix(mime, "image/") {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			a.W, a.H = int64(cfg.Width), int64(cfg.Height)
		}
	}
	_ = writeWhole(filepath.Join(mediaDir(), "pieces", hashName(a.MediaID)+extension(mime, data)), data)
	return att, []Attachment{a}, nil
}

// ---- Avatars ----

// avatarURL : l'image d'une personne (statique, 128 px) ; vide sans avatar.
func avatarURL(u *discordgo.User) string {
	if u == nil || u.Avatar == "" {
		return ""
	}
	return discordgo.EndpointUserAvatar(u.ID, u.Avatar) + "?size=128"
}

// fetchImage : une image du CDN, gardée sous avatars/ (une seule fois par adresse).
func (p *Pont) fetchImage(ctx context.Context, url string) string {
	path := filepath.Join(mediaDir(), "avatars", hashName(url)+".png")
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return path
	}
	data, _, err := p.cdnGet(ctx, p.httpClient(), url, 4<<20)
	if err != nil {
		log.Debug().Err(err).Msg("avatar")
		return ""
	}
	if writeWhole(path, data) != nil {
		return ""
	}
	return path
}

// fetchAvatars : les avatars des conversations (personne, ou image du
// groupe), un à la fois, seulement ceux qui ont changé.
func (p *Pont) fetchAvatars(ids []string) {
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	for _, id := range ids {
		p.mu.Lock()
		c := p.chans[id]
		url := ""
		if c != nil {
			if c.ch.Type == discordgo.ChannelTypeGroupDM {
				if c.ch.Icon != "" {
					url = discordgo.EndpointGroupIcon(id, c.ch.Icon) + "?size=128"
				}
			} else if r := p.recipients(c); len(r) > 0 {
				url = avatarURL(r[0])
			}
		}
		done := p.avatars[id] == url
		p.mu.Unlock()
		if url == "" || done {
			continue
		}
		path := p.fetchImage(ctx, url)
		if path == "" {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		p.mu.Lock()
		p.avatars[id] = url
		p.mu.Unlock()
		emit("avatar", map[string]any{"conv": id, "path": path})
	}
}

func (p *Pont) myAvatar() {
	p.mu.Lock()
	url := avatarURL(p.me)
	p.mu.Unlock()
	if url == "" {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	if path := p.fetchImage(ctx, url); path != "" {
		emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
	}
}
