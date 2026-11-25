// Les aperçus de liens fournis par le service (PROTOCOLE.md, §5.9) : titre,
// description, site et vignette d'une adresse du message, tels que le service
// les envoie avec lui. L'interface les montre en priorité sur ceux qu'elle
// irait chercher elle-même.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-signal/pkg/signalmeow"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/protobuf/signalpb"
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

// apercuSignal : le premier aperçu que Signal envoie avec le message. Sa
// vignette est une pièce jointe chiffrée : téléchargée ici (petite, 10 s au
// plus), une seule fois.
func (p *Pont) apercuSignal(dm *signalpb.DataMessage) *LinkPreview {
	for _, pv := range dm.GetPreview() {
		lp := nouvelApercu(pv.GetUrl(), pv.GetTitle(), pv.GetDescription(), "")
		if lp == nil {
			continue
		}
		if a := pv.GetImage(); a != nil && (a.GetCdnKey() != "" || a.GetCdnId() != 0) && a.GetSize() < 4<<20 {
			brut, _ := proto.Marshal(a)
			h := sha256.Sum256(brut)
			chemin := filepath.Join(mediaDir(), "apercus", "sg-"+hex.EncodeToString(h[:16])+".jpg")
			if _, err := os.Stat(chemin); err == nil {
				lp.Image = chemin
			} else {
				ctx, annule := context.WithTimeout(p.ctx, 10*time.Second)
				data, err := signalmeow.DownloadAttachmentWithPointer(ctx, a, nil, nil)
				annule()
				if err == nil && len(data) > 0 && os.MkdirAll(filepath.Dir(chemin), 0o700) == nil &&
					os.WriteFile(chemin+".tmp", data, 0o600) == nil && os.Rename(chemin+".tmp", chemin) == nil {
					lp.Image = chemin
				} else if err != nil {
					log.Warn().Err(err).Msg("vignette d'aperçu")
				}
			}
		}
		return lp
	}
	return nil
}
