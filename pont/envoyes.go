package main

// Les images et fichiers que j'envoie : Google renvoie ensuite le message avec
// seulement le nom du fichier (pas d'identifiant de média), et les autres
// appareils (le téléphone, en client du serveur) n'avaient qu'un nom à
// afficher. On garde ici, au moment de l'envoi, la pièce telle que Google l'a
// reçue (identifiant, clé, type, taille), avec une copie du fichier dans le
// cache des pièces : le message renvoyé la retrouve par son tmp_id ou par le
// nom du fichier.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

type envoye struct {
	Piece Attachment `json:"piece"`
	TmpID string     `json:"tmp_id,omitempty"`
	Quand int64      `json:"quand"`
}

var (
	envoyesMu sync.Mutex
	envoyes   map[string]envoye // nom du fichier -> pièce
)

const envoyesMax = 500

func cheminEnvoyes() string { return filepath.Join(dataDir(), "envoyes.json") }

func chargerEnvoyes() {
	if envoyes != nil {
		return
	}
	envoyes = map[string]envoye{}
	if b, err := os.ReadFile(cheminEnvoyes()); err == nil {
		_ = json.Unmarshal(b, &envoyes)
	}
}

// retenirEnvoye : appelé après UploadMedia, avant l'envoi du message.
func retenirEnvoye(media *gmproto.MediaContent, nom, mime, tmp string, data []byte) {
	if media == nil || media.GetMediaID() == "" || nom == "" {
		return
	}
	p := Attachment{
		MediaID: media.GetMediaID(),
		Key:     media.GetDecryptionKey(),
		Mime:    mime,
		Name:    nom,
		Size:    int64(len(data)),
		W:       int64(media.GetDimensions().GetWidth()),
		H:       int64(media.GetDimensions().GetHeight()),
	}
	// La copie locale : le téléchargement de cette pièce la trouve tout de suite.
	dir := filepath.Join(mediaDir(), "pieces")
	_ = os.MkdirAll(dir, 0o700)
	_ = writeWhole(filepath.Join(dir, hashName(p.MediaID)+extension(mime, data)), data)

	envoyesMu.Lock()
	defer envoyesMu.Unlock()
	chargerEnvoyes()
	envoyes[nom] = envoye{Piece: p, TmpID: tmp, Quand: time.Now().Unix()}
	// Les plus vieilles partent au-delà de la limite.
	for len(envoyes) > envoyesMax {
		var vieux string
		var t int64 = 1 << 62
		for k, e := range envoyes {
			if e.Quand < t {
				vieux, t = k, e.Quand
			}
		}
		delete(envoyes, vieux)
	}
	if b, err := json.Marshal(envoyes); err == nil {
		_ = writeWhole(cheminEnvoyes(), b)
		_ = os.Chmod(cheminEnvoyes(), 0o600)
	}
}

// pieceEnvoyee : la pièce gardée pour un message à moi renvoyé sans elle.
func pieceEnvoyee(nom, tmp string) (Attachment, bool) {
	envoyesMu.Lock()
	defer envoyesMu.Unlock()
	chargerEnvoyes()
	if e, ok := envoyes[nom]; ok && nom != "" {
		return e.Piece, true
	}
	if tmp != "" {
		for _, e := range envoyes {
			if e.TmpID == tmp {
				return e.Piece, true
			}
		}
	}
	return Attachment{}, false
}
