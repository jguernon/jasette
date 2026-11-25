package main

// Pièces jointes (téléchargement / envoi) et photos de profil.
//
// Le champ « key » d'une pièce jointe (base64 dans le JSON) contient un petit
// JSON (mediaKey) : la partie média du message WhatsApp (protobuf, avec le
// chemin sur le CDN et les clés de déchiffrement), plus de quoi demander au
// téléphone de la renvoyer quand le CDN l'a oubliée.

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
	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type mediaKey struct {
	Kind  string `json:"kind,omitempty"`  // image, video, audio, document, sticker
	Proto []byte `json:"proto,omitempty"` // la partie média du message (sans vignette)
	Mime  string `json:"mime,omitempty"`
	// Pour redemander le média au téléphone.
	Chat   string `json:"chat,omitempty"`
	Sender string `json:"sender,omitempty"`
	ID     string `json:"id,omitempty"`
	FromMe bool   `json:"from_me,omitempty"`
}

// La clé d'une pièce jointe reçue.
func encodeMediaKey(kind string, msg proto.Message, mime string, info *types.MessageInfo) []byte {
	c := proto.Clone(msg)
	// Les vignettes ne servent pas au téléchargement : on ne les garde pas.
	switch x := c.(type) {
	case *waE2E.ImageMessage:
		x.JPEGThumbnail = nil
	case *waE2E.VideoMessage:
		x.JPEGThumbnail = nil
	case *waE2E.DocumentMessage:
		x.JPEGThumbnail = nil
	case *waE2E.StickerMessage:
		x.PngThumbnail = nil
	}
	b, err := proto.Marshal(c)
	if err != nil {
		return nil
	}
	k, _ := json.Marshal(mediaKey{
		Kind: kind, Proto: b, Mime: mime,
		Chat: info.Chat.ToNonAD().String(), Sender: info.Sender.ToNonAD().String(), ID: info.ID, FromMe: info.IsFromMe,
	})
	return k
}

// La partie média du message, prête pour whatsmeow.
func (k *mediaKey) message() (whatsmeow.DownloadableMessage, error) {
	var msg interface {
		proto.Message
		whatsmeow.DownloadableMessage
	}
	switch k.Kind {
	case "image":
		msg = &waE2E.ImageMessage{}
	case "video":
		msg = &waE2E.VideoMessage{}
	case "audio":
		msg = &waE2E.AudioMessage{}
	case "document":
		msg = &waE2E.DocumentMessage{}
	case "sticker":
		msg = &waE2E.StickerMessage{}
	default:
		return nil, fmt.Errorf("type de pièce jointe inconnu : %q", k.Kind)
	}
	if err := proto.Unmarshal(k.Proto, msg); err != nil {
		return nil, err
	}
	return msg, nil
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
	if len(k.Proto) == 0 {
		fail(errors.New("pièce jointe sans adresse"))
		return
	}
	msg, err := k.message()
	if err != nil {
		fail(err)
		return
	}
	cli, err := p.readyClient()
	if err != nil {
		fail(err)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	data, err := cli.Download(ctx, msg)
	if errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) || errors.Is(err, whatsmeow.ErrNoURLPresent) {
		// Le CDN l'a oubliée : on demande au téléphone de la renvoyer.
		if path, rerr := p.mediaRetry(ctx, cli, &k, msg.GetMediaKey()); rerr != nil {
			err = rerr
		} else {
			setDirectPath(msg, path)
			data, err = cli.Download(ctx, msg)
			if err == nil {
				p.updateMediaKey(k.ID, msg, &k)
			}
		}
	}
	if err != nil {
		fail(fmt.Errorf("téléchargement : %w", err))
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

func setDirectPath(msg whatsmeow.DownloadableMessage, path string) {
	switch x := msg.(type) {
	case *waE2E.ImageMessage:
		x.DirectPath, x.URL = &path, nil
	case *waE2E.VideoMessage:
		x.DirectPath, x.URL = &path, nil
	case *waE2E.AudioMessage:
		x.DirectPath, x.URL = &path, nil
	case *waE2E.DocumentMessage:
		x.DirectPath, x.URL = &path, nil
	case *waE2E.StickerMessage:
		x.DirectPath, x.URL = &path, nil
	}
}

// Garde le nouveau chemin du média dans le message (pour la prochaine fois).
func (p *Pont) updateMediaKey(id string, msg whatsmeow.DownloadableMessage, k *mediaKey) {
	r := p.getMsg(id)
	if r == nil {
		return
	}
	b, err := proto.Marshal(msg.(proto.Message))
	if err != nil {
		return
	}
	nk := *k
	nk.Proto = b
	key, _ := json.Marshal(nk)
	for i := range r.Attachments {
		r.Attachments[i].Key = key
	}
	p.putMsg(r)
}

// Demande au téléphone de renvoyer un média sur le CDN ; rend le nouveau chemin.
func (p *Pont) mediaRetry(ctx context.Context, cli *whatsmeow.Client, k *mediaKey, key []byte) (string, error) {
	if k.ID == "" || k.Chat == "" || len(key) == 0 {
		return "", errors.New("média expiré sur le serveur de WhatsApp")
	}
	chat, err := types.ParseJID(k.Chat)
	if err != nil {
		return "", err
	}
	sender, _ := types.ParseJID(k.Sender)
	ch := make(chan *events.MediaRetry, 1)
	p.mu.Lock()
	p.retryWait[k.ID] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.retryWait, k.ID)
		p.mu.Unlock()
	}()
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsFromMe: k.FromMe, IsGroup: chat.Server == types.GroupServer},
		ID:            k.ID,
	}
	if err := cli.SendMediaRetryReceipt(ctx, info, key); err != nil {
		return "", fmt.Errorf("demande au téléphone : %w", err)
	}
	wait, annule := context.WithTimeout(ctx, time.Minute)
	defer annule()
	select {
	case evt := <-ch:
		notif, err := whatsmeow.DecryptMediaRetryNotification(evt, key)
		if errors.Is(err, whatsmeow.ErrMediaNotAvailableOnPhone) {
			return "", errors.New("le média n'est plus sur le téléphone")
		} else if err != nil {
			return "", err
		}
		if notif.GetResult() != waMmsRetry.MediaRetryNotification_SUCCESS || notif.GetDirectPath() == "" {
			return "", fmt.Errorf("le téléphone n'a pas pu renvoyer le média (%s)", notif.GetResult())
		}
		return notif.GetDirectPath(), nil
	case <-wait.Done():
		return "", errors.New("le téléphone n'a pas renvoyé le média (est-il en ligne ?)")
	}
}

// ---- Photos de profil ----

func avatarName(id string) string { return hashName("avatar:" + id) }

func avatarFresh(id string) (string, bool) {
	dir := filepath.Join(mediaDir(), "avatars")
	matches, _ := filepath.Glob(filepath.Join(dir, avatarName(id)+".*"))
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

func removeAvatar(id string) {
	old, _ := filepath.Glob(filepath.Join(mediaDir(), "avatars", avatarName(id)+".*"))
	for _, o := range old {
		_ = os.Remove(o)
	}
}

// La photo d'une personne ou d'un groupe (JID texte) : gardée 24 h.
func (p *Pont) fetchPic(cli *whatsmeow.Client, id string) (string, error) {
	if path, ok := avatarFresh(id); ok {
		return path, nil
	}
	p.mu.Lock()
	since, sans := p.noPic[id]
	existing := p.picIDs[id]
	p.mu.Unlock()
	if sans && time.Since(since) < 6*time.Hour {
		return "", errors.New("pas de photo")
	}
	if cli == nil {
		return "", errors.New("WhatsApp n'est pas connecté")
	}
	jid, err := types.ParseJID(id)
	if err != nil {
		return "", err
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	// Avec l'identifiant de la photo déjà téléchargée : rien à refaire si elle n'a pas changé.
	if existing != "" {
		old, _ := filepath.Glob(filepath.Join(mediaDir(), "avatars", avatarName(id)+".*"))
		if len(old) == 0 {
			existing = ""
		}
	}
	info, err := cli.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{ExistingID: existing, IsCommunity: false})
	if errors.Is(err, whatsmeow.ErrProfilePictureNotSet) || errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized) {
		p.mu.Lock()
		p.noPic[id] = time.Now()
		p.mu.Unlock()
		return "", err
	} else if err != nil {
		return "", err
	}
	if info == nil {
		// Inchangée : on rafraîchit la date du fichier gardé.
		old, _ := filepath.Glob(filepath.Join(mediaDir(), "avatars", avatarName(id)+".*"))
		for _, o := range old {
			if !strings.HasSuffix(o, ".tmp") {
				now := time.Now()
				_ = os.Chtimes(o, now, now)
				return o, nil
			}
		}
		return "", errors.New("photo introuvable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("photo : HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil || len(data) == 0 {
		return "", fmt.Errorf("photo : %w", err)
	}
	removeAvatar(id)
	path := filepath.Join(mediaDir(), "avatars", avatarName(id)+extension("", data))
	if err := writeWhole(path, data); err != nil {
		return "", err
	}
	p.mu.Lock()
	p.picIDs[id] = info.ID
	delete(p.noPic, id)
	p.mu.Unlock()
	return path, nil
}

// Les photos des fils : la personne (1-à-1) ou l'image du groupe.
func (p *Pont) avatars(convs []string) {
	cli := p.client()
	for _, id := range convs {
		if p.ctx.Err() != nil {
			return
		}
		_, fresh := avatarFresh(id)
		path, err := p.fetchPic(cli, id)
		if err != nil {
			log.Debug().Err(err).Str("fil", id).Msg("photo")
			continue
		}
		emit("avatar", map[string]any{"conv": id, "path": path})
		if !fresh {
			// Doucement avec les serveurs de WhatsApp.
			time.Sleep(250 * time.Millisecond)
		}
	}
}

func (p *Pont) myAvatarDownload(cli *whatsmeow.Client) {
	if cli.Store.ID == nil {
		return
	}
	path, err := p.fetchPic(cli, cli.Store.ID.ToNonAD().String())
	if err != nil {
		log.Debug().Err(err).Msg("ma photo")
		return
	}
	emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
}

// Une photo a changé : on la retélécharge.
func (p *Pont) pictureChanged(cli *whatsmeow.Client, evt *events.Picture) {
	id := p.canon(cli, evt.JID).String()
	removeAvatar(id)
	p.mu.Lock()
	delete(p.picIDs, id)
	delete(p.noPic, id)
	me := p.isMe(cli, id)
	if evt.Remove {
		p.noPic[id] = time.Now()
	}
	p.mu.Unlock()
	if evt.Remove {
		return
	}
	if me {
		p.myAvatarDownload(cli)
		return
	}
	p.avatars([]string{id})
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

// Les pièces jointes qui portent une légende (les vocaux et autocollants non).
func captionable(mime string) bool {
	return !strings.HasPrefix(mime, "audio/") && mime != "image/webp"
}

// Envoie le fichier sur le CDN de WhatsApp (chiffré) et construit le message.
func (p *Pont) uploadMedia(ctx context.Context, cli *whatsmeow.Client, path, mime, caption string, reply *msgRow) (*waE2E.Message, []Attachment, error) {
	data, mime, err := readMedia(path, mime)
	if err != nil {
		return nil, nil, err
	}
	base := strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])
	var kind whatsmeow.MediaType
	switch {
	case base == "image/jpeg" || base == "image/png" || base == "image/webp":
		kind = whatsmeow.MediaImage
	case strings.HasPrefix(base, "video/"):
		kind = whatsmeow.MediaVideo
	case strings.HasPrefix(base, "audio/"):
		kind = whatsmeow.MediaAudio
	default:
		kind = whatsmeow.MediaDocument
	}
	up, err := cli.Upload(ctx, data, kind)
	if err != nil {
		return nil, nil, err
	}
	ci := replyContext(cli, reply)
	var capt *string
	if caption != "" {
		capt = proto.String(caption)
	}
	atts := localAttachment(path, mime, data)
	msg := &waE2E.Message{}
	switch kind {
	case whatsmeow.MediaImage:
		if base == "image/webp" {
			// Un WebP part comme autocollant (c'est ce que fait WhatsApp).
			msg.StickerMessage = &waE2E.StickerMessage{
				URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
				FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
				Mimetype: proto.String(base), ContextInfo: ci,
			}
			if len(atts) > 0 {
				msg.StickerMessage.Width, msg.StickerMessage.Height = proto.Uint32(uint32(atts[0].W)), proto.Uint32(uint32(atts[0].H))
				atts[0].Name = "autocollant"
			}
			break
		}
		msg.ImageMessage = &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(base), Caption: capt, ContextInfo: ci,
		}
		if len(atts) > 0 && atts[0].W > 0 {
			msg.ImageMessage.Width, msg.ImageMessage.Height = proto.Uint32(uint32(atts[0].W)), proto.Uint32(uint32(atts[0].H))
		}
	case whatsmeow.MediaVideo:
		msg.VideoMessage = &waE2E.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(base), Caption: capt, ContextInfo: ci,
		}
	case whatsmeow.MediaAudio:
		// Un Ogg/Opus part comme message vocal ; le reste comme fichier audio.
		ptt := base == "audio/ogg" || base == "audio/opus"
		m := mime
		if ptt {
			m = "audio/ogg; codecs=opus"
		}
		msg.AudioMessage = &waE2E.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(m), PTT: proto.Bool(ptt), ContextInfo: ci,
		}
		if ptt && len(atts) > 0 {
			atts[0].Name = "message vocal"
		}
	default:
		name := filepath.Base(path)
		msg.DocumentMessage = &waE2E.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String(base), FileName: proto.String(name), Title: proto.String(name), Caption: capt, ContextInfo: ci,
		}
	}
	return msg, atts, nil
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
