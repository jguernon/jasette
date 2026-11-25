// Les aperçus de liens fournis par le service (PROTOCOLE.md, §5.9) : titre,
// description, site et vignette d'une adresse du message, tels que le service
// les envoie avec lui. L'interface les montre en priorité sur ceux qu'elle
// irait chercher elle-même.
package main

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
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

// apercuIG : l'aperçu d'un partage d'Instagram qui mène à une adresse web
// (titre, légende, auteur) ; nil sinon.
func apercuIG(x *slidetypes.XMAContent) *LinkPreview {
	if x == nil {
		return nil
	}
	lien := xmaLink(x)
	if lien == "" {
		return nil
	}
	titre := cmp.Or(x.TitleText, x.XMATitle)
	site := ""
	if x.HeaderTitleText != "" && x.HeaderTitleText != titre {
		site = x.HeaderTitleText
	}
	return nouvelApercu(lien, cmp.Or(titre, x.HeaderTitleText), cmp.Or(x.SubtitleText, x.CaptionBodyText), site)
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
