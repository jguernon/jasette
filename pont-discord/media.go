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

// download : une pièce jointe, téléchargée une seule fois. Les adresses du
// CDN de Discord expirent (environ un jour) : une adresse refusée est
// renouvelée en relisant le message.
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
		log.Warn().Str("err", err.Error()).Str("media", cmd.MediaID).Msg("téléchargement")
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
	}
	var k mediaKey
	if len(cmd.Key) == 0 || json.Unmarshal(cmd.Key, &k) != nil || !strings.HasPrefix(k.URL, "https://") {
		fail(errors.New("clé de pièce jointe illisible"))
		return
	}
	if !trustedCDN(k.URL) {
		fail(errors.New("adresse de pièce jointe refusée"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	cli := p.httpClient()
	data, status, err := p.cdnGet(ctx, cli, k.URL, maxDownload)
	if err != nil && (status == http.StatusForbidden || status == http.StatusNotFound) && k.Channel != "" && k.Message != "" {
		if fresh := p.freshURL(ctx, k); fresh != "" {
			data, _, err = p.cdnGet(ctx, cli, fresh, maxDownload)
		}
	}
	if err != nil {
		fail(fmt.Errorf("Discord : %w", err))
		return
	}
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(cmd.Mime, data))
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	emit("media", map[string]any{"media_id": cmd.MediaID, "path": path})
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
