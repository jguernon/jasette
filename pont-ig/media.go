package main

// Pièces jointes (téléchargement / envoi) et avatars.
//
// Le champ « key » d'une pièce jointe (base64 dans le JSON) contient un petit
// JSON (mediaKey) : l'URL signée du CDN d'Instagram, plus de quoi la
// redemander quand elle a expiré (le message et la place de la pièce).

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

	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/msgconv/mediadl"
)

type mediaKey struct {
	URL  string `json:"url,omitempty"`
	Mime string `json:"mime,omitempty"`
	// Pour redemander l'URL : le message, son fil, la pièce jointe.
	Msg   string `json:"msg,omitempty"`
	Conv  string `json:"conv,omitempty"`
	FBID  string `json:"fbid,omitempty"`
	Index int    `json:"index,omitempty"`
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
	if k.URL == "" {
		fail(errors.New("pièce jointe sans adresse"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()
	mime := firstOf(cmd.Mime, k.Mime)
	data, err := fetchURL(ctx, mime, k.URL)
	if errors.Is(err, mediadl.ErrForbidden) || errors.Is(err, mediadl.ErrURLNotFound) {
		// L'URL signée a expiré : on redemande le message à Instagram.
		if fresh, rerr := p.refreshMediaURL(ctx, &k); rerr != nil {
			log.Debug().Err(rerr).Msg("nouvelle URL de pièce jointe")
			err = errors.New("lien expiré sur le serveur d'Instagram")
		} else {
			data, err = fetchURL(ctx, mime, fresh)
		}
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

func fetchURL(ctx context.Context, mime, url string) ([]byte, error) {
	_, r, err := mediadl.DownloadMedia(ctx, mime, url, 200<<20)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 200<<20))
}

// Redemande le message (et ses voisins) pour avoir une URL fraîche, comme le
// fait igconnector : la page des messages plus vieux que le suivant, ou la
// page la plus récente s'il n'y a pas de suivant. Les messages relus vont
// aussi dans le cache (leurs URL sont fraîches).
func (p *Pont) refreshMediaURL(ctx context.Context, k *mediaKey) (string, error) {
	cli := p.client()
	if cli == nil {
		return "", errors.New("Instagram n'est pas connecté")
	}
	if k.Msg == "" || k.Conv == "" {
		return "", errors.New("pièce jointe sans message")
	}
	c := p.conv(k.Conv)
	if c == nil {
		return "", errors.New("fil inconnu")
	}
	igid, _, err := p.ensureIGID(ctx, cli, k.Conv, c)
	if igid == "" {
		return "", fmt.Errorf("identifiant du fil : %w", err)
	}
	var edges []slidetypes.Node[*slidetypes.Message]
	orig := p.getMsg(k.Msg)
	var next *msgRow
	if orig != nil {
		next = p.nextMsg(k.Conv, orig.Time, orig.ID)
	}
	if next != nil {
		resp, err := cli.PaginateMessages(ctx, &slidetypes.PaginateMessagesRequest{
			OlderThanMessageID:      &next.ID,
			ThreadID:                igid,
			FirstN:                  20,
			InitialMessagePageCount: 20,
		})
		if err != nil {
			return "", err
		}
		if t := resp.ThreadInfo.AsIGDirectThread; t != nil && t.Messages != nil {
			edges = t.Messages.Edges
		}
	} else {
		resp, err := cli.GetThread(ctx, slidetypes.MakeGetThreadInfoRequest(igid))
		if err != nil {
			return "", err
		}
		if t := resp.ThreadInfo.AsIGDirectThread; t != nil && t.SlideMessages != nil {
			edges = t.SlideMessages.Edges
		}
	}
	fresh := ""
	for _, e := range edges {
		r := p.igRow(k.Conv, e.Node)
		if r == nil {
			continue
		}
		p.storeRow(r)
		if r.ID != k.Msg {
			continue
		}
		for i, a := range r.Attachments {
			var nk mediaKey
			if json.Unmarshal(a.Key, &nk) != nil {
				continue
			}
			if (k.FBID != "" && nk.FBID == k.FBID) || (k.FBID == "" && i == k.Index) {
				fresh = nk.URL
			}
		}
	}
	if fresh == "" {
		return "", errors.New("pièce jointe introuvable dans la page relue")
	}
	return fresh, nil
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

// Le nombre de membres d'une mosaïque (comme les groupes Google).
const mosaiqueMax = 4

// Les membres montrés dans la mosaïque d'un groupe sans image : les
// premiers autres que moi, ceux qui ont une photo d'abord. À appeler avec p.mu.
func (p *Pont) membresMosaique(c *convState) []int64 {
	var avec, sans []int64
	for _, m := range c.Members {
		if m == p.me || m == 0 || containsID(avec, m) || containsID(sans, m) {
			continue
		}
		if ct := p.contacts[m]; ct != nil && ct.Avatar != "" {
			avec = append(avec, m)
		} else {
			sans = append(sans, m)
		}
	}
	l := append(avec, sans...)
	if len(l) > mosaiqueMax {
		l = l[:mosaiqueMax]
	}
	return l
}

// Les avatars des fils : la personne (1-à-1), l'image du groupe, sinon
// (groupe sans image) la mosaïque de ses premiers membres, événement
// « avatars_groupe » comme le pont Google. Les photos des membres passent
// par le même cache que celles des fils à deux (une fois par personne).
func (p *Pont) avatars(convs []string) {
	type demande struct{ conv, id, url string }
	type membre struct {
		id       int64
		nom, url string
	}
	type mosaique struct {
		conv    string
		membres []membre
	}
	var todo []demande
	var mosaiques []mosaique
	p.mu.Lock()
	for _, id := range convs {
		c := p.convs[id]
		if c == nil {
			continue
		}
		if c.Group {
			if c.Picture != "" {
				todo = append(todo, demande{id, "fil:" + id, c.Picture})
			} else if ms := p.membresMosaique(c); len(ms) >= 2 {
				mo := mosaique{conv: id}
				for _, m := range ms {
					mb := membre{id: m}
					if ct := p.contacts[m]; ct != nil {
						mb.nom, mb.url = ct.Name, ct.Avatar
					}
					mo.membres = append(mo.membres, mb)
				}
				mosaiques = append(mosaiques, mo)
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
	for _, mo := range mosaiques {
		liste := make([]map[string]any, 0, len(mo.membres))
		photos := 0
		for _, m := range mo.membres {
			if p.ctx.Err() != nil {
				return
			}
			path := ""
			if m.url != "" {
				if pa, err := p.fetchAvatar("contact:"+idStr(m.id), m.url); err == nil {
					path = pa
					photos++
				} else {
					log.Debug().Err(err).Str("fil", mo.conv).Msg("avatar d'un membre")
				}
			}
			liste = append(liste, map[string]any{"name": m.nom, "color": "", "path": path})
		}
		if photos > 0 {
			emit("avatars_groupe", map[string]any{"conv": mo.conv, "members": liste})
		}
	}
}

func (p *Pont) myAvatarDownload() {
	p.mu.Lock()
	url, me := p.myAvatar, p.me
	if ct := p.contacts[me]; ct != nil && ct.Avatar != "" {
		url = firstOf(url, ct.Avatar)
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

// Envoi sur le serveur de Meta (le même « mercury » que le web d'Instagram),
// qui rend l'identifiant de la pièce jointe.
func (p *Pont) uploadMedia(ctx context.Context, cli *instameow.Client, thread int64, path, mime string) (int64, []Attachment, error) {
	data, mime, err := readMedia(path, mime)
	if err != nil {
		return 0, nil, err
	}
	media := &httpclient.MercuryUploadMedia{
		Filename:    filepath.Base(path),
		MimeType:    mime,
		MediaData:   data,
		IsVoiceClip: strings.HasPrefix(mime, "audio/"),
	}
	resp, err := cli.GetHTTP().SendMercuryUploadRequest(ctx, thread, media)
	if err != nil {
		return 0, nil, err
	}
	id := resp.Payload.RealMetadata.GetFbId()
	if id == 0 {
		return 0, nil, errors.New("Instagram n'a pas rendu d'identifiant pour le fichier")
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
