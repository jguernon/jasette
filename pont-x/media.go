package main

// Pièces jointes (téléchargement / envoi) et avatars.
//
// Le champ « key » d'une pièce jointe (base64 dans le JSON) contient un petit
// JSON (mediaKey) :
//   - média XChat : le fil, la « media hash key » et la version de la clé du
//     fil ; le fichier est chiffré (secretstream) sur le serveur de X ;
//   - média de l'ancienne API : l'URL (ton.twitter.com demande les cookies,
//     pbs/video.twimg.com sont publics).

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

	_ "golang.org/x/image/webp"

	"go.mau.fi/mautrix-twitter/pkg/twittermeow"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/crypto"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/data/payload"
)

const maxMedia = 200 << 20

type mediaKey struct {
	URL  string `json:"url,omitempty"`
	Mime string `json:"mime,omitempty"`
	// Média XChat chiffré.
	Conv string `json:"conv,omitempty"`
	Hash string `json:"hash,omitempty"`
	Ver  string `json:"ver,omitempty"`
}

// Une pièce jointe, téléchargée une seule fois.
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
	cli := p.client()
	if cli == nil {
		fail(errors.New("X n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	var data []byte
	err := errors.New("pièce jointe sans adresse")
	if k.Hash != "" && k.Conv != "" {
		var buf bytes.Buffer
		if _, err = cli.DownloadXChatMedia(ctx, k.Conv, k.Hash, k.Ver, &buf); err == nil {
			data = buf.Bytes()
		} else {
			log.Debug().Err(err).Msg("média XChat")
		}
	}
	if data == nil && k.URL != "" {
		data, err = fetchX(ctx, cli, k.URL)
	}
	if data == nil {
		fail(explain(err))
		return
	}
	mime := firstOf(cmd.Mime, k.Mime)
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(mime, data))
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	emit("media", map[string]any{"media_id": cmd.MediaID, "path": path})
}

// Télécharge une adresse de X : avec les cookies pour x.com / ton.twitter.com,
// sans rien pour les CDN publics.
func fetchX(ctx context.Context, cli *twittermeow.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	hc := http.DefaultClient
	if twittermeow.IsXURL(url) && !twittermeow.IsPublicCDNURL(url) {
		req.Header = cli.GetBaseHeaders()
		req.Header.Set("Cookie", cli.GetCookieString())
		hc = cli.HTTP
	} else {
		req.Header.Set("User-Agent", cli.GetUserAgent())
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("le serveur de X a répondu %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxMedia))
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

func forgetAvatar(id string) {
	old, _ := filepath.Glob(filepath.Join(mediaDir(), "avatars", hashName(id)+".*"))
	for _, o := range old {
		_ = os.Remove(o)
	}
}

// Une image de profil, en grand (« _normal » est une vignette de 48 px).
func bigAvatar(url string) string {
	return strings.Replace(url, "_normal.", "_400x400.", 1)
}

// Télécharge une image (en clair, ou chiffrée avec la clé d'un fil si « conv »).
func (p *Pont) fetchAvatar(id, url, conv, keyVersion string) (string, error) {
	if path, ok := avatarFresh(id); ok {
		return path, nil
	}
	if url == "" {
		return "", errors.New("pas d'image")
	}
	cli := p.client()
	if cli == nil {
		return "", errors.New("X n'est pas connecté")
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	var data []byte
	var err error
	if conv != "" && !twittermeow.IsPublicCDNURL(url) {
		data, err = p.fetchEncryptedImage(ctx, cli, url, conv, keyVersion)
	} else {
		data, err = fetchX(ctx, cli, url)
	}
	if err != nil || len(data) == 0 {
		return "", fmt.Errorf("avatar : %w", err)
	}
	forgetAvatar(id)
	path := filepath.Join(mediaDir(), "avatars", hashName(id)+extension("", data))
	if err := writeWhole(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// Les images de groupe XChat sont chiffrées avec la clé du fil.
func (p *Pont) fetchEncryptedImage(ctx context.Context, cli *twittermeow.Client, url, conv, keyVersion string) ([]byte, error) {
	var key *crypto.ConversationKey
	if keyVersion != "" {
		key, _ = p.ks.GetConversationKey(ctx, conv, keyVersion)
	}
	if key == nil {
		key, _ = p.ks.GetLatestConversationKey(ctx, conv)
	}
	if key == nil {
		return nil, errors.New("clé du fil inconnue")
	}
	resp, err := cli.FetchRaw(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("le serveur de X a répondu %s", resp.Status)
	}
	var buf bytes.Buffer
	if _, err := crypto.SecretstreamDecrypt(io.LimitReader(resp.Body, maxMedia), key.Key, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Les avatars des fils : la personne (1-à-1) ou l'image du groupe.
func (p *Pont) avatars(convs []string) {
	type demande struct{ conv, id, url, keyConv, keyVer string }
	var todo []demande
	p.mu.Lock()
	for _, id := range convs {
		c := p.convs[id]
		if c == nil {
			continue
		}
		if c.Group {
			if c.Picture != "" {
				kc := ""
				if c.XChat {
					kc = id
				}
				todo = append(todo, demande{id, "fil:" + id, c.Picture, kc, c.PicKey})
			}
			continue
		}
		other := p.otherOf(c)
		if ct := p.contacts[other]; ct != nil && ct.Avatar != "" {
			todo = append(todo, demande{id, "contact:" + other, bigAvatar(ct.Avatar), "", ""})
		}
	}
	p.mu.Unlock()
	for _, d := range todo {
		if p.ctx.Err() != nil {
			return
		}
		path, err := p.fetchAvatar(d.id, d.url, d.keyConv, d.keyVer)
		if err != nil {
			log.Debug().Err(err).Str("fil", d.conv).Msg("avatar")
			continue
		}
		emit("avatar", map[string]any{"conv": d.conv, "path": path})
	}
}

func (p *Pont) myAvatarDownload() {
	p.mu.Lock()
	me := p.me
	url := ""
	if p.sess != nil {
		url = p.sess.Avatar
	}
	if ct := p.contacts[me]; ct != nil {
		url = firstOf(url, ct.Avatar)
	}
	p.mu.Unlock()
	if me == "" || url == "" {
		return
	}
	path, err := p.fetchAvatar("contact:"+me, bigAvatar(url), "", "")
	if err != nil {
		log.Debug().Err(err).Msg("mon avatar")
		return
	}
	emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
}

// ---- Envoi de fichiers ----

func readMedia(path, mime string) ([]byte, string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if st.Size() > maxMedia {
		return nil, "", errors.New("fichier trop gros")
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

func filepathBase(path string) string { return filepath.Base(path) }

func (p *Pont) sendMedia(cmd Cmd) {
	if _, err := os.Stat(cmd.Path); err != nil {
		emitErr("send", fmt.Errorf("fichier introuvable : %w", err))
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": cmd.TmpID, "ok": false})
		return
	}
	p.sendCommon(cmd, strings.TrimSpace(cmd.Text), cmd.Path, cmd.Mime)
}

// Le type XChat d'un fichier.
func xchatMediaType(mime string) payload.MediaType {
	switch {
	case mime == "image/gif":
		return payload.MediaTypeGif
	case strings.HasPrefix(mime, "image/"):
		return payload.MediaTypeImage
	case strings.HasPrefix(mime, "video/"):
		return payload.MediaTypeVideo
	case strings.HasPrefix(mime, "audio/"):
		return payload.MediaTypeAudio
	}
	return payload.MediaTypeFile
}

// Envoie le fichier sur le serveur de X. Rend la pièce jointe XChat (modes
// XChat) ou l'identifiant du média (ancienne API).
func (p *Pont) uploadMedia(ctx context.Context, cli *twittermeow.Client, conv, msgID string, mode sendMode, name, mime string, data []byte, local []Attachment) (*payload.MessageAttachment, string, error) {
	typ := xchatMediaType(mime)
	if mode != sendEncrypted && (typ == payload.MediaTypeFile || typ == payload.MediaTypeAudio) {
		return nil, "", errors.New("X n'accepte les fichiers et messages vocaux que dans les conversations chiffrées (X Chat)")
	}
	size := int64(len(data))
	media := &payload.MediaAttachment{
		Type:          ptrTo(int32(typ)),
		FilesizeBytes: &size,
		Filename:      &name,
	}
	if len(local) > 0 && local[0].W > 0 {
		w, h := local[0].W, local[0].H
		media.Dimensions = &payload.MediaDimensions{Width: &w, Height: &h}
	}
	if mode == sendEncrypted {
		res, err := cli.UploadXChatMedia(ctx, conv, msgID, data)
		if errors.Is(err, twittermeow.ErrXChatMediaSizeUnsupported) {
			return nil, "", errors.New("fichier trop gros pour X")
		} else if err != nil {
			return nil, "", err
		}
		media.MediaHashKey = &res.MediaHashKey
		return &payload.MessageAttachment{Media: media}, "", nil
	}
	cat := payload.MEDIA_CATEGORY_DM_IMAGE
	switch {
	case typ == payload.MediaTypeGif:
		cat = payload.MEDIA_CATEGORY_DM_GIF
	case typ == payload.MediaTypeVideo:
		cat = payload.MEDIA_CATEGORY_DM_VIDEO
	}
	res, err := cli.UploadMedia(ctx, &payload.UploadMediaQuery{MediaType: mime, MediaCategory: cat}, data)
	if err != nil {
		return nil, "", err
	}
	if res == nil || res.MediaIDString == "" {
		return nil, "", errors.New("X n'a pas rendu d'identifiant pour le fichier")
	}
	if mode == sendREST {
		return nil, res.MediaIDString, nil
	}
	media.MediaHashKey = &res.MediaKey
	media.AttachmentId = &res.MediaIDString
	return &payload.MessageAttachment{Media: media}, "", nil
}

func ptrTo[T any](v T) *T { return &v }

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
