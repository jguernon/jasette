package main

// Pièces jointes (téléchargement / envoi) et avatars.
//
// Le champ « key » d'une pièce jointe (base64 dans le JSON) contient un petit
// JSON (mediaKey) : de quoi redemander le fichier à Telegram (identifiant,
// access_hash, file_reference) et, si la référence a expiré, le message qui
// le contient (fil et numéro) pour en obtenir une nouvelle.

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
	mimepkg "mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/telegram/downloader/fileref"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

type mediaKey struct {
	Kind  string `json:"k"` // "photo" ou "doc"
	ID    int64  `json:"id"`
	Hash  int64  `json:"h"`
	Ref   []byte `json:"r"`
	Thumb string `json:"t,omitempty"`
	Conv  string `json:"c"`
	Num   int    `json:"n"`
}

func (k mediaKey) location() tg.InputFileLocationClass {
	if k.Kind == "photo" {
		return &tg.InputPhotoFileLocation{ID: k.ID, AccessHash: k.Hash, FileReference: k.Ref, ThumbSize: k.Thumb}
	}
	return &tg.InputDocumentFileLocation{ID: k.ID, AccessHash: k.Hash, FileReference: k.Ref}
}

// La plus grande taille d'une photo.
func bestSize(sizes []tg.PhotoSizeClass) (string, int, int, int) {
	typ, w, h, n := "", 0, 0, 0
	for _, s := range sizes {
		switch v := s.(type) {
		case *tg.PhotoSize:
			if v.W*v.H > w*h {
				typ, w, h, n = v.Type, v.W, v.H, v.Size
			}
		case *tg.PhotoSizeProgressive:
			if v.W*v.H > w*h {
				typ, w, h = v.Type, v.W, v.H
				if len(v.Sizes) > 0 {
					n = v.Sizes[len(v.Sizes)-1]
				}
			}
		}
	}
	return typ, w, h, n
}

// À appeler avec p.mu : les pièces jointes d'un message, et le texte à
// ajouter pour ce qui n'est pas un fichier (position, contact, sondage).
func (p *Pont) attachmentsOf(conv string, num int, media tg.MessageMediaClass) ([]Attachment, string) {
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		ph, ok := m.Photo.(*tg.Photo)
		if !ok {
			return nil, "📷 Photo (expirée)"
		}
		typ, w, h, n := bestSize(ph.Sizes)
		key, _ := json.Marshal(mediaKey{Kind: "photo", ID: ph.ID, Hash: ph.AccessHash, Ref: ph.FileReference, Thumb: typ, Conv: conv, Num: num})
		return []Attachment{{MediaID: fmt.Sprintf("photo:%d:%s", ph.ID, typ), Key: key, Mime: "image/jpeg", Name: "photo.jpg", Size: int64(n), W: int64(w), H: int64(h)}}, ""
	case *tg.MessageMediaDocument:
		doc, ok := m.Document.(*tg.Document)
		if !ok {
			return nil, "📎 Fichier (expiré)"
		}
		a := Attachment{MediaID: fmt.Sprintf("doc:%d", doc.ID), Mime: doc.MimeType, Size: doc.Size}
		for _, at := range doc.Attributes {
			switch v := at.(type) {
			case *tg.DocumentAttributeFilename:
				if a.Name == "" {
					a.Name = v.FileName
				}
			case *tg.DocumentAttributeAudio:
				if v.Voice {
					a.Name = "message vocal"
				}
			case *tg.DocumentAttributeSticker:
				a.Name = "autocollant"
			case *tg.DocumentAttributeVideo:
				a.W, a.H = int64(v.W), int64(v.H)
			case *tg.DocumentAttributeImageSize:
				a.W, a.H = int64(v.W), int64(v.H)
			}
		}
		if m.Voice {
			a.Name = "message vocal"
		}
		if a.Mime == "" {
			a.Mime = "application/octet-stream"
		}
		key, _ := json.Marshal(mediaKey{Kind: "doc", ID: doc.ID, Hash: doc.AccessHash, Ref: doc.FileReference, Conv: conv, Num: num})
		a.Key = key
		return []Attachment{a}, ""
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return nil, "📍 Position"
	case *tg.MessageMediaContact:
		return nil, strings.TrimSpace("👤 " + m.FirstName + " " + m.LastName + " " + m.PhoneNumber)
	case *tg.MessageMediaPoll:
		return nil, "📊 " + m.Poll.Question.Text
	case *tg.MessageMediaDice:
		return nil, m.Emoticon
	}
	return nil, ""
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
	if len(cmd.Key) == 0 || json.Unmarshal(cmd.Key, &k) != nil || k.ID == 0 {
		fail(errors.New("clé de pièce jointe illisible"))
		return
	}
	p.mu.Lock()
	client, ok := p.client, p.authed
	p.mu.Unlock()
	if client == nil || !ok {
		fail(errors.New("Telegram n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	loc := k.location()
	b := client.Download(loc)
	if ip, err := p.inputPeer(k.Conv); err == nil && k.Num > 0 {
		b = b.WithRefresh(fileref.Message(client.API(), ip, k.Num, loc))
	}
	var buf bytes.Buffer
	if _, err := b.Stream(ctx, &buf); err != nil {
		fail(fmt.Errorf("Telegram : %w", err))
		return
	}
	data := buf.Bytes()
	mime := cmd.Mime
	if k.Kind == "photo" {
		mime = "image/jpeg"
	}
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(mime, data))
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	emit("media", map[string]any{"media_id": cmd.MediaID, "path": path})
}

// ---- Avatars ----

func avatarDir() string { return filepath.Join(mediaDir(), "avatars") }

func avatarFresh(id string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(avatarDir(), hashName(id)+".*"))
	for _, m := range matches {
		if !strings.HasSuffix(m, ".tmp") {
			return m, true
		}
	}
	return "", false
}

func saveAvatarPath(id string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("image vide")
	}
	path := filepath.Join(avatarDir(), hashName(id)+extension("", data))
	if err := writeWhole(path, data); err != nil {
		return "", err
	}
	return path, nil
}

var (
	errPasDImage = errors.New("pas d'image")
	errHorsLigne = errors.New("hors ligne")
)

// La photo d'un fil (personne, groupe, canal) : la petite version.
func (p *Pont) peerAvatar(ctx context.Context, k string) (string, error) {
	p.mu.Lock()
	c := p.peers[k]
	client := p.client
	p.mu.Unlock()
	if c == nil || c.Photo == 0 {
		return "", errPasDImage
	}
	id := fmt.Sprintf("avatar:%s:%d", k, c.Photo)
	if path, ok := avatarFresh(id); ok {
		return path, nil
	}
	if client == nil {
		return "", errHorsLigne
	}
	ip, err := p.inputPeer(k)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := client.Download(&tg.InputPeerPhotoFileLocation{Peer: ip, PhotoID: c.Photo}).Stream(ctx, &buf); err != nil {
		return "", err
	}
	path, err := saveAvatarPath(id, buf.Bytes())
	if err != nil {
		return "", err
	}
	// Une seule image par fil : l'ancienne photo s'en va.
	var old string
	if p.loadKV("avatar:"+k, &old) && old != "" && old != path {
		_ = os.Remove(old)
	}
	p.saveKV("avatar:"+k, path)
	return path, nil
}

// Les avatars des fils.
func (p *Pont) avatars(convs []string) {
	for _, k := range convs {
		if p.ctx.Err() != nil || p.tgAPI() == nil {
			return
		}
		ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
		path, err := p.peerAvatar(ctx, k)
		annule()
		if err != nil {
			continue
		}
		emit("avatar", map[string]any{"conv": k, "path": path})
	}
}

func (p *Pont) myAvatarDownload() {
	p.mu.Lock()
	me := keyOf('u', p.me)
	p.mu.Unlock()
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	path, err := p.peerAvatar(ctx, me)
	if err != nil {
		log.Debug().Err(err).Msg("mon avatar")
		return
	}
	emit("avatar_moi", map[string]any{"service": serviceName(), "path": path})
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
	if i := strings.IndexByte(mime, ';'); i > 0 {
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

// Un message vocal enregistré par l'interface (pw-record, en ogg/opus) :
// c'est déjà le format des vocaux de Telegram.
func isVoiceNote(path, mime string) bool {
	return strings.HasPrefix(mime, "audio/") &&
		(strings.HasPrefix(filepath.Base(path), "vocal-") || mime == "audio/ogg" || mime == "audio/opus")
}

// Téléverse un fichier chez Telegram. Rend le média à envoyer, et la pièce
// jointe locale pour l'interface.
func (p *Pont) uploadMedia(ctx context.Context, api *tg.Client, path, mime string) (tg.InputMediaClass, []Attachment, error) {
	data, mime, err := readMedia(path, mime)
	if err != nil {
		return nil, nil, err
	}
	name := filepath.Base(path)
	f, err := uploader.NewUploader(api).FromBytes(ctx, name, data)
	if err != nil {
		return nil, nil, fmt.Errorf("téléversement : %w", err)
	}
	a := Attachment{MediaID: "local:" + hashName(path+time.Now().String()), Mime: mime, Name: name, Size: int64(len(data))}
	var media tg.InputMediaClass
	switch {
	case mime == "image/jpeg" || mime == "image/png":
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			a.W, a.H = int64(cfg.Width), int64(cfg.Height)
		}
		media = &tg.InputMediaUploadedPhoto{File: f}
	default:
		attrs := []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: name}}
		switch {
		case isVoiceNote(path, mime):
			attrs = append(attrs, &tg.DocumentAttributeAudio{Voice: true})
			mime, a.Name = "audio/ogg", "message vocal"
		case strings.HasPrefix(mime, "video/"):
			attrs = append(attrs, &tg.DocumentAttributeVideo{SupportsStreaming: true})
		}
		media = &tg.InputMediaUploadedDocument{File: f, MimeType: mime, Attributes: attrs}
	}
	a.Mime = mime
	// Copie dans pieces/ : l'interface la trouve sans téléchargement.
	dst := filepath.Join(mediaDir(), "pieces", hashName(a.MediaID)+extension(mime, data))
	_ = writeWhole(dst, data)
	return media, []Attachment{a}, nil
}
