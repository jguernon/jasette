package main

// « Quoi de neuf ? » : la partie commune à tous les ponts (PROTOCOLE.md, §5.12).
// Ce fichier est copié tel quel dans chaque pont qui publie une note
// (pont-ig, pont-meta, pont-wa, pont-telegram, pont-bluesky) : le modifier
// partout à la fois.
//
// Le pont garde ma note actuelle dans <données>/<service>/notes-etat.json
// (0600) : texte, date, échéance, source (« main » ou « musique »), la note
// écrite à la main mise de côté par la musique, l'ancienne bio (services où
// la note est une bio) et les dates des dernières publications (garde-fou :
// au plus parHeure publications par heure glissante).
//
// Une commande = au plus une requête au service : jamais de nouvel essai,
// jamais de boucle. Le texte de la note actuelle n'est pas republié.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// etatNote : ce que le pont sait de ma note sur son service.
type etatNote struct {
	Text     string `json:"text"`
	Time     int64  `json:"time"`    // ms
	Expires  int64  `json:"expires"` // ms, 0 : sans échéance
	Source   string `json:"source"`
	Audience int    `json:"audience"`
	// L'id de la note chez le service (Instagram : inbox_tray_item_id ;
	// Messenger : rich_status_id), pour l'effacer.
	ID string `json:"id,omitempty"`

	// La note écrite à la main que la musique a remplacée.
	MainText     string `json:"main_text,omitempty"`
	MainTime     int64  `json:"main_time,omitempty"`
	MainAudience int    `json:"main_audience,omitempty"`

	// Ancienne : la bio d'avant la première note (WhatsApp, Telegram,
	// Bluesky), remise à l'effacement. nil : rien de gardé.
	Ancienne *string `json:"ancienne,omitempty"`

	// Les dates (ms) des publications de la dernière heure.
	Publications []int64 `json:"publications,omitempty"`
}

// erreurNote : un message déjà clair pour l'utilisateur, montré tel quel.
type erreurNote string

func (e erreurNote) Error() string { return string(e) }

// opsNote : ce que chaque service sait faire.
type opsNote struct {
	nom      string        // « Instagram », pour les messages
	limite   int           // caractères (points de code) au plus
	parHeure int           // publications au plus par heure glissante
	duree    time.Duration // durée de vie d'une note chez le service (0 : aucune)
	// publier pose `texte` ; rend l'id de la note chez le service (ou "").
	// Il peut remplir e.Ancienne (bio d'avant) si elle est nil.
	publier func(ctx context.Context, texte string, audience int, e *etatNote) (string, error)
	// effacer retire la note (ou remet e.Ancienne).
	effacer func(ctx context.Context, e *etatNote) error
}

type notesPont struct {
	mu   sync.Mutex
	ops  opsNote
	etat etatNote
	lu   bool
	// maintenant : l'horloge (remplacée dans les tests).
	maintenant func() time.Time
}

const dureeNoteMain = 24 * time.Hour

func nouvellesNotes(ops opsNote) *notesPont {
	return &notesPont{ops: ops, maintenant: time.Now}
}

func cheminEtatNote() string {
	return filepath.Join(dataDir(), serviceName(), "notes-etat.json")
}

// charger : à appeler sous n.mu.
func (n *notesPont) charger() {
	if n.lu {
		return
	}
	n.lu = true
	if b, err := os.ReadFile(cheminEtatNote()); err == nil {
		_ = json.Unmarshal(b, &n.etat)
	}
}

func (n *notesPont) garder() {
	b, err := json.MarshalIndent(&n.etat, "", "  ")
	if err != nil {
		return
	}
	if err := writeWhole(cheminEtatNote(), append(b, '\n')); err != nil {
		log.Warn().Err(err).Msg("notes-etat.json")
	}
}

// echue : la note actuelle a dépassé son échéance (le service l'a retirée).
// À appeler sous n.mu.
func (n *notesPont) echue(ms int64) bool {
	return n.etat.Text != "" && n.etat.Expires > 0 && ms >= n.etat.Expires
}

// noteMoi : l'événement note_moi de l'état. À appeler sous n.mu.
func (n *notesPont) noteMoi() map[string]any {
	ms := n.maintenant().UnixMilli()
	if n.echue(ms) {
		n.etat.Text, n.etat.ID = "", ""
		n.etat.Expires = 0
		n.etat.Source = "main"
	}
	f := map[string]any{
		"text": n.etat.Text, "time": n.etat.Time, "expires": n.etat.Expires,
		"source": cmpOrNote(n.etat.Source, "main"), "audience": n.etat.Audience,
	}
	if n.mainValide(ms) {
		f["main_text"] = n.etat.MainText
		f["main_time"] = n.etat.MainTime
	}
	return f
}

// mainValide : une note écrite à la main est de côté et a moins de 24 h.
func (n *notesPont) mainValide(ms int64) bool {
	return n.etat.MainText != "" && ms-n.etat.MainTime < dureeNoteMain.Milliseconds()
}

// annoncer : ma note actuelle (quand le pont est prêt).
func (n *notesPont) annoncer() {
	n.mu.Lock()
	n.charger()
	f := n.noteMoi()
	n.mu.Unlock()
	emit("note_moi", f)
}

func resultatNote(op, req string, err error) {
	f := map[string]any{"op": op, "ok": err == nil}
	if req != "" {
		f["req"] = req
	}
	if err != nil {
		f["error"] = err.Error()
	}
	emit("note_result", f)
}

// clair : l'erreur à montrer, en français.
func (n *notesPont) clair(op string, err error) error {
	var e erreurNote
	if errors.As(err, &e) {
		return e
	}
	verbe := "publiée"
	if op == "clear" {
		verbe = "effacée"
	}
	return erreurNote(fmt.Sprintf("La note %s n'a pas pu être %s : %v", n.ops.nom, verbe, err))
}

// quota : vrai si une publication de plus est permise. À appeler sous n.mu.
func (n *notesPont) quota(ms int64) bool {
	garde := n.etat.Publications[:0]
	for _, t := range n.etat.Publications {
		if ms-t < time.Hour.Milliseconds() {
			garde = append(garde, t)
		}
	}
	n.etat.Publications = garde
	return len(garde) < n.ops.parHeure
}

func (n *notesPont) tropDePublications() error {
	return erreurNote(fmt.Sprintf("Trop de publications sur %s en une heure : réessaie plus tard.", n.ops.nom))
}

// set : note_set. Une seule requête au service, au plus.
func (n *notesPont) set(ctx context.Context, texte string, audience int, source, req string, pret bool) {
	texte = strings.TrimSpace(texte)
	if source != "musique" {
		source = "main"
	}
	if audience != 1 {
		audience = 0
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.charger()
	ms := n.maintenant().UnixMilli()
	switch {
	case texte == "":
		resultatNote("set", req, erreurNote("La note est vide."))
		return
	case !utf8.ValidString(texte) || utf8.RuneCountInString(texte) > n.ops.limite:
		resultatNote("set", req, erreurNote(fmt.Sprintf("%s : %d caractères au plus.", n.ops.nom, n.ops.limite)))
		return
	case !pret:
		resultatNote("set", req, erreurNote(fmt.Sprintf("%s n'est pas connecté.", n.ops.nom)))
		return
	}
	if n.echue(ms) {
		n.etat.Text, n.etat.ID = "", ""
	}
	if n.etat.Text == texte && cmpOrNote(n.etat.Source, "main") == source && n.etat.Audience == audience {
		// Déjà la note actuelle : rien à publier.
		resultatNote("set", req, nil)
		return
	}
	if !n.quota(ms) {
		resultatNote("set", req, n.tropDePublications())
		return
	}
	n.etat.Publications = append(n.etat.Publications, ms)
	id, err := n.ops.publier(ctx, texte, audience, &n.etat)
	if err != nil {
		n.garder()
		log.Warn().Err(err).Msg("note : publication")
		resultatNote("set", req, n.clair("set", err))
		return
	}
	if source == "musique" {
		// La musique passe par-dessus une note écrite à la main : on la met
		// de côté (pour la remettre quand la musique s'arrête).
		if n.etat.Text != "" && cmpOrNote(n.etat.Source, "main") == "main" {
			n.etat.MainText, n.etat.MainTime, n.etat.MainAudience = n.etat.Text, n.etat.Time, n.etat.Audience
		}
	} else {
		n.etat.MainText, n.etat.MainTime, n.etat.MainAudience = "", 0, 0
	}
	n.etat.Text, n.etat.Time, n.etat.Source, n.etat.Audience, n.etat.ID = texte, ms, source, audience, id
	n.etat.Expires = 0
	if n.ops.duree > 0 {
		n.etat.Expires = ms + n.ops.duree.Milliseconds()
	}
	n.garder()
	f := n.noteMoi()
	emit("note_moi", f)
	resultatNote("set", req, nil)
}

// clear : note_clear. Source « musique » : seulement une note musicale ; la
// note écrite à la main mise de côté revient si elle a moins de 24 h (une
// seule requête : elle remplace la note musicale).
func (n *notesPont) clear(ctx context.Context, source, req string, pret bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.charger()
	ms := n.maintenant().UnixMilli()
	if n.echue(ms) {
		n.etat.Text, n.etat.ID = "", ""
	}
	musique := source == "musique"
	if musique && (n.etat.Text == "" || n.etat.Source != "musique") {
		// Rien de musical à retirer : une note écrite à la main reste.
		resultatNote("clear", req, nil)
		return
	}
	if n.etat.Text == "" && n.etat.Ancienne == nil {
		n.etat.MainText = ""
		n.garder()
		resultatNote("clear", req, nil)
		return
	}
	if !pret {
		resultatNote("clear", req, erreurNote(fmt.Sprintf("%s n'est pas connecté.", n.ops.nom)))
		return
	}
	if !n.quota(ms) {
		resultatNote("clear", req, n.tropDePublications())
		return
	}
	n.etat.Publications = append(n.etat.Publications, ms)
	if musique && n.mainValide(ms) {
		id, err := n.ops.publier(ctx, n.etat.MainText, n.etat.MainAudience, &n.etat)
		if err != nil {
			n.garder()
			resultatNote("clear", req, n.clair("clear", err))
			return
		}
		n.etat.Text, n.etat.Time, n.etat.Source, n.etat.Audience, n.etat.ID =
			n.etat.MainText, n.etat.MainTime, "main", n.etat.MainAudience, id
		n.etat.MainText, n.etat.MainTime, n.etat.MainAudience = "", 0, 0
		n.etat.Expires = 0
		if n.ops.duree > 0 {
			n.etat.Expires = ms + n.ops.duree.Milliseconds()
		}
	} else {
		if err := n.ops.effacer(ctx, &n.etat); err != nil {
			n.garder()
			resultatNote("clear", req, n.clair("clear", err))
			return
		}
		n.etat.Text, n.etat.ID, n.etat.Source, n.etat.Expires = "", "", "main", 0
		n.etat.Time = ms
		n.etat.MainText, n.etat.MainTime, n.etat.MainAudience = "", 0, 0
		n.etat.Ancienne = nil
	}
	n.garder()
	emit("note_moi", n.noteMoi())
	resultatNote("clear", req, nil)
}

func cmpOrNote(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
