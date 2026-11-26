package main

// La photo des auteurs dans les groupes (PROTOCOLE.md §5.13), avec prudence.
// Même fichier dans pont-wa, pont-signal et pont-telegram.
//
// Chaque message d'un autre dans un groupe porte « sender_id », et
// {"ev":"sender_avatar","service":…,"id":<sender_id>,"path":…} donne sa photo.
// Les règles, demandées par Jonathan :
//   - paresseux : seulement pour les auteurs des messages réellement chargés
//     ou reçus dans un groupe (noter) ;
//   - une fois par personne et par jour, avec un état sur le disque
//     (<données>/<service>/auteurs.json) : une photo déjà connue est redonnée
//     sans réseau ;
//   - au plus auteursParMinute requêtes par minute, étalées (une toutes les
//     six secondes), et auteursParJour par jour ;
//   - jamais de rafale : une seule file, petite (auteursFile) ; ce qui n'y
//     entre pas attend de revoir l'auteur ;
//   - un refus du service (limite, confidentialité, pas de photo) garde les
//     initiales, sans nouvel essai avant le lendemain.

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
)

const (
	auteursParMinute = 10
	auteursParJour   = 200
	auteursFile      = 32
)

// Ce que l'on sait d'un auteur : sa photo sur le disque (vide : les
// initiales) et le jour de la dernière demande.
type entreeAuteur struct {
	Path  string `json:"path,omitempty"`
	Jour  string `json:"jour"`
	Refus bool   `json:"refus,omitempty"`
}

// L'état gardé sur le disque : le compte du jour et chaque auteur.
type etatAuteurs struct {
	Jour   string                  `json:"jour"`
	Compte int                     `json:"compte"`
	Gens   map[string]entreeAuteur `json:"gens"`
}

type photosAuteurs struct {
	mu       sync.Mutex
	fichier  string
	etat     etatAuteurs
	annonces map[string]string // id → chemin déjà annoncé aux clients (cette session)
	enFile   map[string]bool
	file     chan string
	dernier  time.Time

	// charger : la requête au service (télécharge et rend le chemin ; ""
	// sans erreur : pas de photo). refus : l'erreur est un refus du service
	// (rien avant demain) plutôt qu'une panne passagère.
	charger func(ctx context.Context, id string) (string, error)
	refus   func(error) bool
	// Remplaçables par les essais.
	horloge  func() time.Time
	attendre func(ctx context.Context, d time.Duration) error
	emettre  func(id, path string)
}

func nouvellesPhotosAuteurs(fichier string, charger func(context.Context, string) (string, error), refus func(error) bool) *photosAuteurs {
	a := &photosAuteurs{
		fichier:  fichier,
		annonces: map[string]string{},
		enFile:   map[string]bool{},
		file:     make(chan string, auteursFile),
		charger:  charger,
		refus:    refus,
		horloge:  time.Now,
		attendre: dormir,
		emettre: func(id, path string) {
			emit("sender_avatar", map[string]any{"service": serviceName(), "id": id, "path": path})
		},
	}
	if b, err := os.ReadFile(fichier); err == nil {
		_ = json.Unmarshal(b, &a.etat)
	}
	if a.etat.Gens == nil {
		a.etat.Gens = map[string]entreeAuteur{}
	}
	return a
}

func dormir(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *photosAuteurs) jour() string { return a.horloge().Format("2006-01-02") }

func surDisque(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// noter : un message d'un autre dans un groupe, chargé ou reçu. Redonne sa
// photo connue (une fois par session) et la demande au plus une fois par
// jour. Ne bloque jamais.
func (a *photosAuteurs) noter(id string) {
	if a == nil || id == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.etat.Gens[id]
	if ok && e.Path != "" && a.annonces[id] != e.Path && surDisque(e.Path) {
		a.annonces[id] = e.Path
		a.emettre(id, e.Path)
	}
	if (ok && e.Jour == a.jour()) || a.enFile[id] {
		return
	}
	select {
	case a.file <- id:
		a.enFile[id] = true
	default:
		// File pleine : on redemandera en revoyant l'auteur.
	}
}

// connue : la photo d'un auteur déjà sur le disque, sans requête ("" sinon).
func (a *photosAuteurs) connue(id string) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e := a.etat.Gens[id]; e.Path != "" && surDisque(e.Path) {
		return e.Path
	}
	return ""
}

// boucle : les demandes, une à la fois.
func (a *photosAuteurs) boucle(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-a.file:
			if a.traiter(ctx, id) != nil {
				return
			}
		}
	}
}

// traiter : une demande, si le jour et les plafonds le permettent. Rend
// l'erreur du contexte seulement (fin du pont).
func (a *photosAuteurs) traiter(ctx context.Context, id string) error {
	a.mu.Lock()
	delete(a.enFile, id)
	jour := a.jour()
	if a.etat.Gens[id].Jour == jour {
		a.mu.Unlock()
		return nil
	}
	if a.etat.Jour != jour {
		a.etat.Jour, a.etat.Compte = jour, 0
	}
	if a.etat.Compte >= auteursParJour {
		a.mu.Unlock()
		return nil // plafond du jour : on redemandera demain, en revoyant l'auteur
	}
	attente := time.Minute/auteursParMinute - a.horloge().Sub(a.dernier)
	a.mu.Unlock()
	if !a.dernier.IsZero() && attente > 0 {
		if err := a.attendre(ctx, attente); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.dernier = a.horloge()
	a.etat.Compte++
	a.mu.Unlock()

	path, err := a.charger(ctx, id)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil && !a.refus(err) {
		log.Debug().Err(err).Str("auteur", id).Msg("photo d'un auteur : panne passagère")
		a.garder()
		return nil
	}
	e := entreeAuteur{Path: path, Jour: jour, Refus: err != nil}
	if err != nil {
		e.Path = "" // refus : les initiales jusqu'à demain
	}
	a.etat.Gens[id] = e
	a.garder()
	if e.Path != "" && a.annonces[id] != e.Path {
		a.annonces[id] = e.Path
		a.emettre(id, e.Path)
	}
	return nil
}

// garder : l'état sur le disque (avec a.mu).
func (a *photosAuteurs) garder() {
	if a.fichier == "" {
		return
	}
	b, err := json.Marshal(&a.etat)
	if err == nil {
		err = writeWhole(a.fichier, b)
	}
	if err != nil {
		log.Debug().Err(err).Msg("état des photos d'auteurs")
	}
}
