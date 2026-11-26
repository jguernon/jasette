package main

// Pièces jointes (téléchargement / envoi) et avatars.
//
// Le champ « key » d'une pièce jointe (base64 dans le JSON) contient le
// « AttachmentPointer » de Signal en protobuf : où trouver le fichier chiffré
// sur le CDN de Signal, sa clé et son empreinte. Signal garde les pièces
// jointes environ 45 jours sur ses serveurs.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	mimepkg "mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-signal/pkg/signalmeow"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/protobuf/signalpb"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/types"
)

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
	var ptr signalpb.AttachmentPointer
	if len(cmd.Key) == 0 || proto.Unmarshal(cmd.Key, &ptr) != nil {
		fail(errors.New("clé de pièce jointe illisible"))
		return
	}
	if ptr.GetCdnKey() == "" && ptr.GetCdnId() == 0 {
		fail(errors.New("pièce jointe sans adresse"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 5*time.Minute)
	defer annule()
	data, err := signalmeow.DownloadAttachmentWithPointer(ctx, &ptr, nil, nil)
	if err != nil {
		if errors.Is(err, signalmeow.ErrAttachmentNotFound) {
			err = errors.New("pièce jointe expirée sur le serveur de Signal")
		}
		fail(err)
		return
	}
	mime := firstOf(cmd.Mime, ptr.GetContentType())
	path := filepath.Join(dir, hashName(cmd.MediaID)+extension(mime, data))
	if err := writeWhole(path, data); err != nil {
		fail(err)
		return
	}
	emit("media", map[string]any{"media_id": cmd.MediaID, "path": path})
}

// ---- Avatars ----

func avatarDir() string { return filepath.Join(mediaDir(), "avatars") }

// L'image gardée pour un identifiant, si elle est assez récente (maxAge 0 : toujours).
func avatarFresh(id string, maxAge time.Duration) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(avatarDir(), hashName(id)+".*"))
	for _, m := range matches {
		if strings.HasSuffix(m, ".tmp") {
			continue
		}
		if st, err := os.Stat(m); err == nil && (maxAge == 0 || time.Since(st.ModTime()) < maxAge) {
			return m, true
		}
	}
	return "", false
}

func removeAvatar(id string) error {
	old, _ := filepath.Glob(filepath.Join(avatarDir(), hashName(id)+".*"))
	var errs []error
	for _, o := range old {
		errs = append(errs, os.Remove(o))
	}
	return errors.Join(errs...)
}

// Une seule image par personne/fil : on retire les anciennes.
func saveAvatar(id string, data []byte) error {
	_, err := saveAvatarPath(id, data)
	return err
}

func saveAvatarPath(id string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("image vide")
	}
	_ = removeAvatar(id)
	path := filepath.Join(avatarDir(), hashName(id)+extension("", data))
	if err := writeWhole(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// Pas d'image de profil.
var errPasDImage = errors.New("pas d'image")

// L'image d'une personne : la photo du carnet d'adresses du téléphone si on
// l'a reçue, sinon la photo de son profil Signal (chiffrée avec sa clé de profil).
func (p *Pont) contactAvatar(ctx context.Context, cli *signalmeow.Client, aci string) (string, error) {
	if path, ok := avatarFresh("contact:"+aci, 0); ok {
		return path, nil
	}
	if path, ok := avatarFresh("profil:"+aci, 24*time.Hour); ok {
		return path, nil
	}
	id, err := uuid.Parse(aci)
	if err != nil {
		return "", err
	}
	r, err := cli.Store.RecipientStore.LoadAndUpdateRecipient(ctx, id, uuid.Nil, nil)
	if err != nil || r == nil {
		return "", fmt.Errorf("contact : %w", err)
	}
	if r.Profile.AvatarPath == "" || r.Profile.AvatarPath == "clear" {
		return "", errPasDImage
	}
	data, err := cli.DownloadUserAvatar(ctx, r.Profile.AvatarPath, r.Profile.Key)
	if err != nil {
		return "", fmt.Errorf("avatar : %w", err)
	}
	return saveAvatarPath("profil:"+aci, data)
}

// L'image d'un groupe (chiffrée avec la clé du groupe).
func (p *Pont) groupAvatar(ctx context.Context, cli *signalmeow.Client, gid, avatarPath string) (string, error) {
	if path, ok := avatarFresh("fil:"+gid, 7*24*time.Hour); ok {
		return path, nil
	}
	if avatarPath == "" {
		return "", errors.New("pas d'image")
	}
	mk, err := cli.Store.GroupStore.MasterKeyFromGroupIdentifier(ctx, types.GroupIdentifier(gid))
	if err != nil || mk == "" {
		return "", fmt.Errorf("clé du groupe : %w", err)
	}
	data, err := cli.DownloadGroupAvatar(ctx, avatarPath, mk)
	if err != nil {
		return "", fmt.Errorf("avatar du groupe : %w", err)
	}
	return saveAvatarPath("fil:"+gid, data)
}

// Les avatars des fils : la personne (1-à-1) ou l'image du groupe.
func (p *Pont) avatars(convs []string) {
	cli := p.client()
	if cli == nil {
		return
	}
	type demande struct{ conv, avatar string }
	var todo []demande
	p.mu.Lock()
	for _, id := range convs {
		c := p.convs[id]
		if c == nil {
			continue
		}
		if c.Group {
			if c.Avatar != "" {
				todo = append(todo, demande{id, c.Avatar})
			}
			continue
		}
		if isUUID(id) {
			todo = append(todo, demande{id, ""})
		}
	}
	p.mu.Unlock()
	for _, d := range todo {
		if p.ctx.Err() != nil || p.client() != cli {
			return
		}
		ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
		var path string
		var err error
		if isGroupID(d.conv) {
			path, err = p.groupAvatar(ctx, cli, d.conv, d.avatar)
		} else {
			path, err = p.contactAvatar(ctx, cli, d.conv)
		}
		annule()
		if err != nil {
			log.Debug().Err(err).Str("fil", d.conv).Msg("avatar")
			continue
		}
		emit("avatar", map[string]any{"conv": d.conv, "path": path})
	}
}

func (p *Pont) myAvatarDownload() {
	cli := p.client()
	if cli == nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	path, err := p.contactAvatar(ctx, cli, cli.Store.ACI.String())
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

// Un message vocal enregistré par l'interface (pw-record, en ogg).
func isVoiceNote(path, mime string) bool {
	return strings.HasPrefix(mime, "audio/") &&
		(strings.HasPrefix(filepath.Base(path), "vocal-") || mime == "audio/ogg" || mime == "audio/opus")
}

// Convertit un vocal en AAC (le format des vocaux de Signal), sans fichier
// temporaire : ffmpeg lit l'entrée standard et écrit la sortie standard.
func toAAC(ctx context.Context, data []byte) ([]byte, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("ffmpeg absent")
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-vn", "-c:a", "aac", "-b:a", "64k", "-f", "adts", "pipe:1")
	cmd.Stdin = bytes.NewReader(data)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg : %w (%s)", err, strings.TrimSpace(errOut.String()))
	}
	if out.Len() == 0 {
		return nil, errors.New("ffmpeg n'a rien produit")
	}
	return out.Bytes(), nil
}

// Chiffre et téléverse un fichier sur le CDN de Signal. Rend le pointeur à
// mettre dans le message, et la pièce jointe locale pour l'interface.
func (p *Pont) uploadMedia(ctx context.Context, cli *signalmeow.Client, path, mime string) (*signalpb.AttachmentPointer, []Attachment, error) {
	data, mime, err := readMedia(path, mime)
	if err != nil {
		return nil, nil, err
	}
	name := filepath.Base(path)
	voice := false
	if isVoiceNote(path, mime) {
		if conv, err := toAAC(ctx, data); err != nil {
			log.Warn().Err(err).Msg("conversion du message vocal (envoyé tel quel)")
		} else {
			data, mime, voice = conv, "audio/aac", true
			name = strings.TrimSuffix(name, filepath.Ext(name)) + ".aac"
		}
	}
	ptr, err := cli.UploadAttachment(ctx, data)
	if err != nil {
		return nil, nil, fmt.Errorf("téléversement : %w", err)
	}
	ptr.ContentType = proto.String(mime)
	ptr.FileName = proto.String(name)
	if voice {
		ptr.Flags = proto.Uint32(uint32(signalpb.AttachmentPointer_VOICE_MESSAGE))
	}
	if strings.HasPrefix(mime, "image/") {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			ptr.Width = proto.Uint32(uint32(cfg.Width))
			ptr.Height = proto.Uint32(uint32(cfg.Height))
		}
	}
	return ptr, localAttachment(path, mime, data, ptr), nil
}

// La pièce jointe locale d'un envoi (déjà présente : pas besoin de la télécharger).
func localAttachment(path, mime string, data []byte, ptr *signalpb.AttachmentPointer) []Attachment {
	a := Attachment{MediaID: "local:" + hashName(path+time.Now().String()), Mime: mime, Name: filepath.Base(path), Size: int64(len(data))}
	if ptr.GetFlags()&uint32(signalpb.AttachmentPointer_VOICE_MESSAGE) != 0 {
		a.Name = "message vocal"
	}
	a.W, a.H = int64(ptr.GetWidth()), int64(ptr.GetHeight())
	// Copie dans pieces/ : l'interface la trouve sans téléchargement. Le
	// pointeur reste dans « key » (pour la retélécharger au besoin).
	dst := filepath.Join(mediaDir(), "pieces", hashName(a.MediaID)+extension(mime, data))
	if writeWhole(dst, data) == nil {
		a.Key, _ = proto.Marshal(ptr)
	}
	return []Attachment{a}
}
