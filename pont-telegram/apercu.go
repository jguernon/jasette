// Les aperçus de liens fournis par le service (PROTOCOLE.md, §5.9) : titre,
// description, site et vignette d'une adresse du message, tels que le service
// les envoie avec lui. L'interface les montre en priorité sur ceux qu'elle
// irait chercher elle-même.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotd/td/tg"
)

// LinkPreview : le champ « link_preview » d'un message (absent s'il n'y en a pas).
type LinkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Site        string `json:"site,omitempty"`
	// La vignette : un fichier écrit par le pont (dossier des médias),
	// l'adresse https d'une image que l'interface ira chercher, ou une pièce
	// à demander au pont (commande « download », comme une pièce jointe).
	Image      string      `json:"image,omitempty"`
	ImageURL   string      `json:"image_url,omitempty"`
	ImageMedia *Attachment `json:"image_media,omitempty"`
}

// nouvelApercu : nil s'il n'y a ni adresse, ni rien à montrer.
func nouvelApercu(url, titre, description, site string) *LinkPreview {
	url, titre, description = strings.TrimSpace(url), strings.TrimSpace(titre), strings.TrimSpace(description)
	if url == "" || titre == "" && description == "" {
		return nil
	}
	return &LinkPreview{URL: url, Title: titre, Description: description, Site: strings.TrimSpace(site)}
}

// vignetteApercu : écrit une vignette (JPEG, PNG…) dans <médias>/apercus,
// nommée d'après son contenu ; rend son chemin, ou "" en cas d'échec.
func vignetteApercu(data []byte) string {
	if len(data) == 0 || len(data) > 4<<20 {
		return ""
	}
	h := sha256.Sum256(data)
	dir := filepath.Join(mediaDir(), "apercus")
	chemin := filepath.Join(dir, hex.EncodeToString(h[:16])+".jpg")
	if _, err := os.Stat(chemin); err == nil {
		return chemin
	}
	if os.MkdirAll(dir, 0o700) != nil {
		return ""
	}
	tmp := chemin + ".tmp"
	if os.WriteFile(tmp, data, 0o600) != nil || os.Rename(tmp, chemin) != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return chemin
}

// apercuTG : la page web que Telegram joint au message (titre, description,
// site), et sa photo, à demander au pont comme une pièce jointe.
func apercuTG(conv string, num int, media tg.MessageMediaClass) *LinkPreview {
	mw, ok := media.(*tg.MessageMediaWebPage)
	if !ok {
		return nil
	}
	wp, ok := mw.Webpage.(*tg.WebPage)
	if !ok {
		return nil
	}
	lp := nouvelApercu(wp.URL, wp.Title, wp.Description, wp.SiteName)
	if lp == nil {
		return nil
	}
	if pc, ok := wp.GetPhoto(); ok {
		if ph, ok := pc.(*tg.Photo); ok {
			typ, w, h, n := bestSize(ph.Sizes)
			key, _ := json.Marshal(mediaKey{Kind: "photo", ID: ph.ID, Hash: ph.AccessHash, Ref: ph.FileReference, Thumb: typ, Conv: conv, Num: num})
			lp.ImageMedia = &Attachment{MediaID: fmt.Sprintf("photo:%d:%s", ph.ID, typ), Key: key, Mime: "image/jpeg", Name: "apercu.jpg", Size: int64(n), W: int64(w), H: int64(h)}
		}
	}
	return lp
}
