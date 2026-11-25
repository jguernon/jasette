// Les aperçus de liens fournis par le service (PROTOCOLE.md, §5.9) : titre,
// description, site et vignette d'une adresse du message, tels que le service
// les envoie avec lui. L'interface les montre en priorité sur ceux qu'elle
// irait chercher elle-même.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
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

// apercuXMA : l'aperçu d'un lien partagé (pièce « XMA » de Messenger ou
// d'Instagram) : nil si ce n'est pas un lien web avec un titre.
func apercuXMA(titre, sousTitre, action string, cta *table.LSInsertAttachmentCta) *LinkPreview {
	lien := action
	if cta != nil {
		if strings.HasPrefix(cta.Type_, "xma_poll_") {
			return nil
		}
		lien = firstOf(lien, cta.ActionUrl, cta.NativeUrl)
	}
	lien = deballerLien(lien)
	if !strings.HasPrefix(lien, "http") {
		return nil
	}
	return nouvelApercu(lien, titre, sousTitre, "")
}

// deballerLien : l.facebook.com/l.php?u=… et l.instagram.com/?u=… mènent à u.
func deballerLien(lien string) string {
	u, err := url.Parse(lien)
	if err != nil {
		return lien
	}
	h := strings.ToLower(u.Hostname())
	if h == "l.facebook.com" || h == "lm.facebook.com" || h == "l.messenger.com" || h == "l.instagram.com" {
		if v := u.Query().Get("u"); strings.HasPrefix(v, "http") {
			return v
		}
	}
	return lien
}

// contientLien : le texte a-t-il déjà une adresse web ?
func contientLien(texte string) bool {
	t := strings.ToLower(texte)
	return strings.Contains(t, "http://") || strings.Contains(t, "https://") || strings.Contains(t, "www.")
}

func joindre(texte, ajout string) string {
	if texte == "" {
		return ajout
	}
	return texte + "\n\n" + ajout
}
