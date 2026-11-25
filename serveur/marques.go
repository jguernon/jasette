// Les marques : le Head, les favoris et les épinglées, partagés entre tous
// les appareils de l'utilisateur (voir PROTOCOLE.md, §5.5).
//
// Le serveur en garde la copie de référence dans <données>/serveur/marques.json
// (0600, écrit par renommage, avec une copie marques.json.bak de la version
// précédente). Chaque changement accepté incrémente `version` et est diffusé
// à tous les clients ; le rejeu en envoie l'état juste après {"auth":"ok"}.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

const (
	marquesParListe = 5000 // ids au plus par liste
	tailleIdMax     = 512  // octets au plus par id
)

// Marques : trois listes ordonnées d'ids de conversations (préfixés « m: »
// pour Messenger, « i: » pour Instagram, sans préfixe pour Google).
// Version 0 : le serveur n'a encore rien reçu.
type Marques struct {
	Head     []string `json:"head"`
	Favoris  []string `json:"favoris"`
	Epingles []string `json:"epingles"`
	Version  int64    `json:"version"`
}

func marquesPath() string { return filepath.Join(serveurDir(), "marques.json") }

// listes rend les trois listes, dans l'ordre (pour les boucles).
func (m *Marques) listes() []*[]string { return []*[]string{&m.Head, &m.Favoris, &m.Epingles} }

// normaliser : jamais de liste nulle (elle s'écrirait `null`), ids propres.
func (m *Marques) normaliser() {
	for _, l := range m.listes() {
		*l = nettoyerIds(*l)
	}
}

func (m *Marques) memesListes(n *Marques) bool {
	a, b := m.listes(), n.listes()
	for i := range a {
		if !egales(*a[i], *b[i]) {
			return false
		}
	}
	return true
}

// nettoyerIds : sans doublon, sans id vide ni démesuré, borné.
func nettoyerIds(l []string) []string {
	r := make([]string, 0, len(l))
	vus := map[string]bool{}
	for _, id := range l {
		if id == "" || len(id) > tailleIdMax || !utf8.ValidString(id) || vus[id] {
			continue
		}
		vus[id] = true
		r = append(r, id)
		if len(r) >= marquesParListe {
			break
		}
	}
	return r
}

func egales(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unir : `d'abord` dans son ordre, puis les ids de `ensuite` qui n'y sont pas.
func unir(dabord, ensuite []string) []string {
	r := append([]string{}, dabord...)
	vus := map[string]bool{}
	for _, id := range dabord {
		vus[id] = true
	}
	for _, id := range ensuite {
		if !vus[id] {
			vus[id] = true
			r = append(r, id)
		}
	}
	return nettoyerIds(r)
}

// lireMarquesFichier lit un fichier de marques ; erreur s'il est illisible.
func lireMarquesFichier(chemin string) (Marques, error) {
	var m Marques
	data, err := os.ReadFile(chemin)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Version < 0 {
		return m, errors.New("version négative")
	}
	m.normaliser()
	return m, nil
}

// chargerMarques : marques.json, sinon sa copie .bak. Un fichier illisible
// n'est jamais écrasé : il est mis de côté (marques.json.illisible-<date>).
func chargerMarques() Marques {
	chemin := marquesPath()
	m, err := lireMarquesFichier(chemin)
	if err == nil {
		return m
	}
	if !errors.Is(err, os.ErrNotExist) {
		cote := fmt.Sprintf("%s.illisible-%s", chemin, time.Now().Format("20060102-150405"))
		logf("marques.json illisible (%v) : mis de côté sous %s", err, filepath.Base(cote))
		_ = os.Rename(chemin, cote)
	}
	if b, errB := lireMarquesFichier(chemin + ".bak"); errB == nil {
		if !errors.Is(err, os.ErrNotExist) {
			logf("marques reprises de marques.json.bak (version %d)", b.Version)
		}
		return b
	}
	m = Marques{}
	m.normaliser()
	return m
}

// garderMarques écrit marques.json de façon atomique (fichier temporaire
// synchronisé, puis renommage), après avoir copié l'ancien, s'il est lisible,
// dans marques.json.bak.
func garderMarques(m Marques) error {
	chemin := marquesPath()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if _, err := lireMarquesFichier(chemin); err == nil {
		if ancien, err := os.ReadFile(chemin); err == nil {
			_ = ecrireAtomique(chemin+".bak", ancien)
		}
	}
	return ecrireAtomique(chemin, append(data, '\n'))
}

// ecrireAtomique : 0600, fichier temporaire synchronisé sur le disque, puis
// renommage (jamais un fichier à moitié écrit, même après une coupure).
func ecrireAtomique(chemin string, data []byte) error {
	tmp := chemin + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if errF := f.Close(); err == nil {
		err = errF
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, chemin); err != nil {
		return err
	}
	if d, err := os.Open(filepath.Dir(chemin)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// trameMarques : {"marques":{…}}, avec l'id de la requête et le refus pour
// le seul client qui l'a envoyée.
func trameMarques(m Marques, id json.RawMessage, refus string) []byte {
	corps := map[string]any{
		"head": m.Head, "favoris": m.Favoris, "epingles": m.Epingles, "version": m.Version,
	}
	if len(id) > 0 {
		corps["id"] = id
	}
	if refus != "" {
		corps["refus"] = refus
	}
	t, _ := json.Marshal(map[string]any{"marques": corps})
	return t
}

// recevoirMarques traite une trame {"marques":{…}} d'un client.
//
//   - version égale à celle du serveur : les listes sont prises telles
//     quelles (une liste vide l'est exprès) ;
//   - version différente (client en retard) : si elle viderait une liste
//     que le serveur a, refus, et le client reçoit les marques du serveur ;
//     sinon les listes du client, dans son ordre, plus les ids du serveur
//     qu'il n'a pas (personne ne perd rien).
//
// Un changement accepté est gardé, puis diffusé à tous avec version+1.
func (s *Serveur) recevoirMarques(c *Client, brute json.RawMessage) {
	var d struct {
		Head     *[]string       `json:"head"`
		Favoris  *[]string       `json:"favoris"`
		Epingles *[]string       `json:"epingles"`
		Version  int64           `json:"version"`
		ID       json.RawMessage `json:"id"`
	}
	if json.Unmarshal(brute, &d) != nil {
		return
	}
	if len(d.ID) > 64 {
		d.ID = nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.marques
	nouv := Marques{Head: cur.Head, Favoris: cur.Favoris, Epingles: cur.Epingles}
	// Une liste absente de la trame ne change pas.
	for i, l := range []*[]string{d.Head, d.Favoris, d.Epingles} {
		if l != nil {
			*nouv.listes()[i] = nettoyerIds(*l)
		}
	}
	repondre := func(m Marques, refus string) {
		select {
		case c.evs <- trameMarques(m, d.ID, refus):
		default:
		}
	}
	if d.Version != cur.Version {
		for i, l := range nouv.listes() {
			if len(*l) == 0 && len(*cur.listes()[i]) > 0 {
				logf("marques de %q refusées : listes vides, version %d < %d", c.nom, d.Version, cur.Version)
				repondre(cur, "version périmée : une liste du serveur serait vidée")
				return
			}
		}
		for i, l := range nouv.listes() {
			*l = unir(*l, *cur.listes()[i])
		}
	}
	nouv.normaliser()
	if nouv.memesListes(&cur) {
		// Rien ne change : le client reçoit seulement l'état courant.
		repondre(cur, "")
		return
	}
	nouv.Version = cur.Version + 1
	if err := garderMarques(nouv); err != nil {
		logf("marques : écriture impossible : %v", err)
		repondre(cur, "le serveur n'a pas pu enregistrer les marques")
		return
	}
	s.marques = nouv
	logf("marques version %d (de %q) : head %d, favoris %d, épinglées %d",
		nouv.Version, c.nom, len(nouv.Head), len(nouv.Favoris), len(nouv.Epingles))
	tous := trameMarques(nouv, nil, "")
	for autre := range s.clients {
		if autre == c {
			continue
		}
		select {
		case autre.evs <- tous:
		default:
			logf("client %q trop lent, débranché", autre.nom)
			autre.fermer()
		}
	}
	repondre(nouv, "")
}
