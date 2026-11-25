package main

// Pièces jointes (téléchargement / envoi) et avatars.
//
// Le champ « key » d'une pièce jointe (base64 dans le JSON) contient un petit
// JSON (mediaKey) : soit l'URL du CDN de Meta (fils normaux), soit de quoi
// télécharger et déchiffrer le fichier sur le serveur WhatsApp (fils chiffrés).

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
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	_ "golang.org/x/image/webp"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/msgconv/mediadl"
)

type mediaKey struct {
	// Fils normaux : l'URL (signée, elle expire au bout de quelques jours).
	URL  string `json:"url,omitempty"`
	Mime string `json:"mime,omitempty"`
	Msg  string `json:"msg,omitempty"`
	// Fils chiffrés : le fichier chiffré sur le serveur WhatsApp.
	WA        bool   `json:"wa,omitempty"`
	Type      string `json:"type,omitempty"`
	Key       []byte `json:"key,omitempty"`
	SHA256    []byte `json:"sha256,omitempty"`
	EncSHA256 []byte `json:"enc_sha256,omitempty"`
	Path      string `json:"path,omitempty"`
}

func removeFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Une pièce jointe, téléchargée (et déchiffrée) une seule fois.
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
		log.Warn().Err(err).Str("media", cmd.MediaID).Msg("téléchargement")
		emit("media", map[string]any{"media_id": cmd.MediaID, "error": err.Error()})
	}
	var k mediaKey
	if err := json.Unmarshal(cmd.Key, &k); err != nil {
		fail(errors.New("clé de pièce jointe illisible"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	var data []byte
	var err error
	mime := firstOf(cmd.Mime, k.Mime)
	if k.WA {
		data, err = p.downloadWA(ctx, &k)
	} else if k.URL != "" {
		var r io.ReadCloser
		_, r, err = mediadl.DownloadMedia(ctx, mime, k.URL, 200<<20)
		if err == nil {
			data, err = io.ReadAll(io.LimitReader(r, 200<<20))
			_ = r.Close()
		} else if errors.Is(err, mediadl.ErrForbidden) {
			err = errors.New("lien expiré sur le serveur de Meta (rouvre la conversation dans Messenger)")
		}
	} else {
		err = errors.New("pièce jointe sans adresse")
	}
	if err != nil {
		fail(err)
		return
	}
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(mime, data))
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	emit("media", map[string]any{"media_id": cmd.MediaID, "path": path})
}

func (p *Pont) downloadWA(ctx context.Context, k *mediaKey) ([]byte, error) {
	if err := p.waitE2EE(ctx, 30*time.Second); err != nil {
		return nil, err
	}
	wa := p.waClient()
	if wa == nil {
		return nil, errors.New("canal chiffré pas connecté")
	}
	return wa.DownloadFB(ctx, &waMediaTransport.WAMediaTransport_Integral{
		FileSHA256:    k.SHA256,
		MediaKey:      k.Key,
		FileEncSHA256: k.EncSHA256,
		DirectPath:    &k.Path,
	}, whatsmeow.MediaType(k.Type))
}

// ---- Avatars ----

func avatarFresh(id string) (string, bool) {
	dir := filepath.Join(mediaDir(), "avatars")
	matches, _ := filepath.Glob(filepath.Join(dir, hashName(id)+".*"))
	for _, m := range matches {
		if strings.HasSuffix(m, ".tmp") {
			continue
		}
		if st, err := os.Stat(m); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
			return m, true
		}
	}
	return "", false
}

func (p *Pont) fetchAvatar(id, url string) (string, error) {
	if path, ok := avatarFresh(id); ok {
		return path, nil
	}
	if url == "" {
		return "", errors.New("pas d'image")
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	data, err := mediadl.DownloadAvatar(ctx, url)
	if err != nil || len(data) == 0 {
		return "", fmt.Errorf("avatar : %w", err)
	}
	dir := filepath.Join(mediaDir(), "avatars")
	// Une seule image par personne/fil : on retire les anciennes.
	old, _ := filepath.Glob(filepath.Join(dir, hashName(id)+".*"))
	for _, o := range old {
		_ = os.Remove(o)
	}
	path := filepath.Join(dir, hashName(id)+extension("", data))
	if err := writeWhole(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// Les avatars des fils : la personne (1-à-1) ou l'image du groupe.
func (p *Pont) avatars(convs []string) {
	type demande struct{ conv, id, url string }
	var todo []demande
	p.mu.Lock()
	for _, id := range convs {
		c := p.convs[id]
		if c == nil {
			continue
		}
		if isGroupType(c.Type) {
			if c.Picture != "" {
				todo = append(todo, demande{id, "fil:" + id, c.Picture})
			}
			continue
		}
		if ct := p.contacts[c.Key]; ct != nil && ct.Avatar != "" {
			todo = append(todo, demande{id, "contact:" + idStr(c.Key), ct.Avatar})
		}
	}
	p.mu.Unlock()
	for _, d := range todo {
		if p.ctx.Err() != nil {
			return
		}
		path, err := p.fetchAvatar(d.id, d.url)
		if err != nil {
			log.Debug().Err(err).Str("fil", d.conv).Msg("avatar")
			continue
		}
		emit("avatar", map[string]any{"conv": d.conv, "path": path})
	}
}

func (p *Pont) myAvatarDownload() {
	p.mu.Lock()
	url, me := p.myAvatar, p.me
	if ct := p.contacts[me]; ct != nil && ct.Avatar != "" {
		url = firstOf(ct.Avatar, url)
	}
	p.mu.Unlock()
	if me == 0 {
		return
	}
	path, err := p.fetchAvatar("contact:"+idStr(me), url)
	if err != nil {
		log.Debug().Err(err).Msg("mon avatar")
		return
	}
	emit("avatar_moi", map[string]any{"service": "messenger", "path": path})
}

// ---- Envoi de fichiers ----

func readMedia(path, mime string) ([]byte, string, error) {
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
	if i := strings.IndexByte(mime, ';'); i > 0 && !strings.HasPrefix(mime, "audio/") {
		mime = strings.TrimSpace(mime[:i])
	}
	return data, mime, nil
}

func (p *Pont) sendMedia(cmd Cmd) {
	if _, err := os.Stat(cmd.Path); err != nil {
		emitErr("send", fmt.Errorf("fichier introuvable : %w", err))
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": cmd.TmpID, "ok": false})
		return
	}
	p.sendCommon(cmd, strings.TrimSpace(cmd.Text), cmd.Path, cmd.Mime)
}

// Fils normaux : envoi sur le serveur de Meta, qui rend un identifiant.
func (p *Pont) uploadFB(ctx context.Context, cli *messagix.Client, thread int64, path, mime string) (int64, []Attachment, error) {
	data, mime, err := readMedia(path, mime)
	if err != nil {
		return 0, nil, err
	}
	resp, err := cli.GetHTTP().SendMercuryUploadRequest(ctx, thread, &httpclient.MercuryUploadMedia{
		Filename:    filepath.Base(path),
		MimeType:    mime,
		MediaData:   data,
		IsVoiceClip: strings.HasPrefix(mime, "audio/"),
	})
	if err != nil {
		return 0, nil, err
	}
	id := resp.Payload.RealMetadata.GetFbId()
	if id == 0 {
		return 0, nil, errors.New("Meta n'a pas rendu d'identifiant pour le fichier")
	}
	return id, localAttachment(path, mime, data), nil
}

// La pièce jointe locale d'un envoi (déjà présente : pas besoin de la télécharger).
func localAttachment(path, mime string, data []byte) []Attachment {
	a := Attachment{MediaID: "local:" + hashName(path+time.Now().String()), Mime: mime, Name: filepath.Base(path), Size: int64(len(data))}
	if strings.HasPrefix(mime, "image/") {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			a.W, a.H = int64(cfg.Width), int64(cfg.Height)
		}
	}
	// Copie dans pieces/ : l'interface la trouve sans téléchargement.
	dst := filepath.Join(mediaDir(), "pieces", hashName(a.MediaID)+extension(mime, data))
	if writeWhole(dst, data) == nil {
		key, _ := json.Marshal(mediaKey{Mime: mime})
		a.Key = key
	}
	return []Attachment{a}
}
