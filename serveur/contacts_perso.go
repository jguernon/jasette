// Le nom et la photo choisis à la main pour une personne : la marque
// `contacts_perso` (PROTOCOLE.md, §5.5, « Règle des contacts perso ») et le
// dossier de leurs photos, <médias>/contacts/ (§6.2, `put` avec
// "garder":"contact").
//
// Le serveur ne fait que garder, nettoyer, unir et diffuser la marque, comme
// les moods ; la photo est un chemin opaque pour lui (les clients la
// téléchargent par `get`, qui ne sert que les racines permises, §6.1).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	contactsPersoMax     = 5000    // entrées au plus
	tailleNomPersoMax    = 256     // octets au plus par nom
	tailleCheminPhotoMax = 1024    // octets au plus par chemin de photo
	taillePhotoPersoMax  = 8 << 20 // une photo gardée (déjà réduite à 512 px par le client)
	agePhotoOrpheline    = 24 * time.Hour
)

// ContactPerso : le nom et la photo d'une personne, choisis dans sa fiche.
// Nom vide : celui du service ; Photo vide : celle du service. Google : la
// personne a demandé de l'enregistrer aussi dans les Contacts Google (un
// téléphone jumelé qui en a la permission l'applique, une fois).
type ContactPerso struct {
	Nom    string `json:"nom,omitempty"`
	Photo  string `json:"photo,omitempty"`
	Google bool   `json:"google,omitempty"`
}

// nettoyerContactsPerso : jamais nul ; ids propres ; nom et chemin bornés et
// en UTF-8 (sinon vidés) ; une entrée sans nom ni photo disparaît ; 5000 au plus.
func nettoyerContactsPerso(m map[string]ContactPerso) map[string]ContactPerso {
	r := make(map[string]ContactPerso, len(m))
	for id, c := range m {
		if !idValide(id) {
			continue
		}
		c.Nom = strings.TrimSpace(c.Nom)
		if len(c.Nom) > tailleNomPersoMax || !utf8.ValidString(c.Nom) || strings.ContainsAny(c.Nom, "\x00\n\r") {
			c.Nom = ""
		}
		if len(c.Photo) > tailleCheminPhotoMax || !utf8.ValidString(c.Photo) || strings.ContainsRune(c.Photo, 0) {
			c.Photo = ""
		}
		if c.Nom == "" && c.Photo == "" {
			continue
		}
		if len(r) >= contactsPersoMax {
			break
		}
		r[id] = c
	}
	return r
}

// lireContactsPerso : la valeur de `contacts_perso` d'une trame, avec
// indulgence (une entrée mal formée est sautée). Faux si absente, nulle ou
// pas un objet : rien ne change.
func lireContactsPerso(brute json.RawMessage) (map[string]ContactPerso, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return nil, false
	}
	var o map[string]json.RawMessage
	if json.Unmarshal(brute, &o) != nil {
		return nil, false
	}
	r := make(map[string]ContactPerso, len(o))
	for id, e := range o {
		var c struct {
			Nom    json.RawMessage `json:"nom"`
			Photo  json.RawMessage `json:"photo"`
			Google json.RawMessage `json:"google"`
		}
		if json.Unmarshal(e, &c) != nil {
			continue
		}
		var p ContactPerso
		_ = json.Unmarshal(c.Nom, &p.Nom)
		_ = json.Unmarshal(c.Photo, &p.Photo)
		_ = json.Unmarshal(c.Google, &p.Google)
		r[id] = p
	}
	return nettoyerContactsPerso(r), true
}

func egalesContactsPerso(a, b map[string]ContactPerso) bool {
	if len(a) != len(b) {
		return false
	}
	for id, c := range a {
		if v, ok := b[id]; !ok || v != c {
			return false
		}
	}
	return true
}

// unirContactsPerso : les entrées de `dabord`, puis celles de `ensuite` pour
// les ids qu'il n'a pas (comme les moods).
func unirContactsPerso(dabord, ensuite map[string]ContactPerso) map[string]ContactPerso {
	r := make(map[string]ContactPerso, len(dabord)+len(ensuite))
	for id, c := range ensuite {
		r[id] = c
	}
	for id, c := range dabord {
		r[id] = c
	}
	return nettoyerContactsPerso(r)
}

// ---- Les photos ----

// photosContactsDir : <médias>/contacts, servi par `get` (sous les médias).
func photosContactsDir() string {
	d := filepath.Join(mediaDir(), "contacts")
	_ = os.MkdirAll(d, 0o700)
	return d
}

// extensionsPhotoPerso : les images qu'on garde comme photo de contact.
var extensionsPhotoPerso = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// garderPhotoContact : un dépôt terminé devient une photo de contact,
// <médias>/contacts/<sha256 (32 hex)>.<ext> (le même contenu, le même
// fichier). Le dépôt est effacé. Rend le chemin gardé.
func garderPhotoContact(depot string) (string, error) {
	defer os.RemoveAll(filepath.Dir(depot))
	ext := strings.ToLower(filepath.Ext(depot))
	if !extensionsPhotoPerso[ext] {
		return "", errors.New("pas une image (jpg, png ou webp)")
	}
	f, err := os.Open(depot)
	if err != nil {
		return "", errors.New("dépôt illisible")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", errors.New("dépôt illisible")
	}
	if st.Size() == 0 || st.Size() > taillePhotoPersoMax {
		return "", errors.New("photo trop grosse")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errors.New("dépôt illisible")
	}
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	dest := filepath.Join(photosContactsDir(), hex.EncodeToString(h.Sum(nil))[:32]+ext)
	if _, err := os.Stat(dest); err == nil {
		// Déjà gardée : rafraîchie, pour que le ménage ne l'emporte pas avant la marque.
		now := time.Now()
		_ = os.Chtimes(dest, now, now)
		return dest, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", errors.New("dépôt illisible")
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", errors.New("dépôt illisible")
	}
	if err := ecrireAtomique(dest, data); err != nil {
		return "", errors.New("photo impossible à garder")
	}
	return dest, nil
}

// menagePhotosContacts : efface les photos de <médias>/contacts qu'aucune
// entrée de `contacts_perso` ne nomme plus, si elles ont plus d'un jour (une
// photo tout juste téléversée attend encore sa marque).
func menagePhotosContacts(m map[string]ContactPerso) {
	dir := filepath.Join(mediaDir(), "contacts")
	entrees, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	nommees := map[string]bool{}
	for _, c := range m {
		if c.Photo != "" {
			nommees[filepath.Base(c.Photo)] = true
		}
	}
	for _, e := range entrees {
		if e.IsDir() || nommees[e.Name()] {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > agePhotoOrpheline {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
