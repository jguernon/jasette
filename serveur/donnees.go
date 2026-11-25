// Les données du serveur : dossiers, certificat, appareils jumelés, code de
// jumelage en cours.
//
// Tout vit dans <données Jasette>/serveur (0700) : cert.pem, cle.pem,
// appareils.json (jetons hachés) et jumelage.json (le code en cours), tous
// en 0600. Les commandes en ligne (--jumeler, --appareils, --revoquer) et le
// serveur en marche se partagent ces fichiers, sous un verrou (flock).
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ---- Les dossiers (mêmes règles que les ponts) ----

func dataDir() string {
	if d := os.Getenv("JASETTE_DATA"); d != "" {
		return d
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "jasette")
}

func mediaDir() string {
	if d := os.Getenv("JASETTE_MEDIA"); d != "" {
		return d
	}
	return filepath.Join(dataDir(), "media")
}

func serveurDir() string {
	d := filepath.Join(dataDir(), "serveur")
	_ = os.MkdirAll(d, 0o700)
	_ = os.Chmod(d, 0o700)
	return d
}

// Les fichiers téléversés par les clients (pour send_media).
func depotsDir() string {
	d := filepath.Join(serveurDir(), "depots")
	_ = os.MkdirAll(d, 0o700)
	return d
}

// Écrit un fichier en entier, 0600, par renommage (jamais à moitié écrit).
func ecrireSecret(chemin string, data []byte) error {
	tmp := chemin + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	_ = os.Chmod(tmp, 0o600)
	return os.Rename(tmp, chemin)
}

// Le verrou partagé entre le serveur et les commandes en ligne.
func avecVerrou(f func() error) error {
	v, err := os.OpenFile(filepath.Join(serveurDir(), ".verrou"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer v.Close()
	if err := syscall.Flock(int(v.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(v.Fd()), syscall.LOCK_UN)
	return f()
}

// ---- Le certificat ----

// chargerCertificat lit (ou crée au premier lancement) le certificat
// auto-signé du serveur, et rend son empreinte : SHA-256 du certificat DER,
// en hexadécimal minuscule.
func chargerCertificat() (tls.Certificate, string, error) {
	dir := serveurDir()
	certPath, clePath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "cle.pem")
	var cert tls.Certificate
	err := avecVerrou(func() error {
		if _, err := os.Stat(certPath); errors.Is(err, os.ErrNotExist) {
			if err := genererCertificat(certPath, clePath); err != nil {
				return err
			}
			logf("nouveau certificat créé dans %s", dir)
		}
		var err error
		cert, err = tls.LoadX509KeyPair(certPath, clePath)
		return err
	})
	if err != nil {
		return cert, "", err
	}
	somme := sha256.Sum256(cert.Certificate[0])
	return cert, hex.EncodeToString(somme[:]), nil
}

func genererCertificat(certPath, clePath string) error {
	cle, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serie, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	modele := &x509.Certificate{
		SerialNumber:          serie,
		Subject:               pkix.Name{CommonName: "Jasette serveur"},
		DNSNames:              []string{"jasette-serveur"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, modele, modele, &cle.PublicKey, cle)
	if err != nil {
		return err
	}
	cleDer, err := x509.MarshalPKCS8PrivateKey(cle)
	if err != nil {
		return err
	}
	if err := ecrireSecret(clePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: cleDer})); err != nil {
		return err
	}
	return ecrireSecret(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// ---- Les appareils jumelés ----

type Appareil struct {
	Nom     string `json:"nom"`
	Hachage string `json:"jeton_sha256"` // SHA-256 du jeton, en hexadécimal ; le jeton lui-même n'est jamais gardé
	Cree    int64  `json:"cree"`
	Vu      int64  `json:"vu,omitempty"`
}

func appareilsPath() string { return filepath.Join(serveurDir(), "appareils.json") }

func lireAppareils() ([]Appareil, error) {
	data, err := os.ReadFile(appareilsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var liste []Appareil
	if err := json.Unmarshal(data, &liste); err != nil {
		return nil, fmt.Errorf("appareils.json illisible : %w", err)
	}
	return liste, nil
}

func ecrireAppareils(liste []Appareil) error {
	if liste == nil {
		liste = []Appareil{}
	}
	data, err := json.MarshalIndent(liste, "", "  ")
	if err != nil {
		return err
	}
	return ecrireSecret(appareilsPath(), data)
}

func hacherJeton(jeton string) string {
	s := sha256.Sum256([]byte(jeton))
	return hex.EncodeToString(s[:])
}

// nouvelAppareil crée un jeton pour `nom` (rendu unique) et le rend en clair :
// c'est la seule fois qu'il existe en clair de ce côté.
func nouvelAppareil(nom string) (jeton, nomFinal string, err error) {
	brut := make([]byte, 32)
	if _, err := rand.Read(brut); err != nil {
		return "", "", err
	}
	jeton = base64.RawURLEncoding.EncodeToString(brut)
	nom = nettoyerNom(nom)
	err = avecVerrou(func() error {
		liste, err := lireAppareils()
		if err != nil {
			return err
		}
		nomFinal = nom
		for i := 2; nomPris(liste, nomFinal); i++ {
			nomFinal = fmt.Sprintf("%s (%d)", nom, i)
		}
		liste = append(liste, Appareil{Nom: nomFinal, Hachage: hacherJeton(jeton), Cree: time.Now().Unix()})
		return ecrireAppareils(liste)
	})
	return jeton, nomFinal, err
}

func nomPris(liste []Appareil, nom string) bool {
	for _, a := range liste {
		if a.Nom == nom {
			return true
		}
	}
	return false
}

func nettoyerNom(nom string) string {
	nom = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(nom))
	if r := []rune(nom); len(r) > 60 {
		nom = string(r[:60])
	}
	if nom == "" {
		nom = "appareil"
	}
	return nom
}

// authentifier rend le nom de l'appareil dont c'est le jeton, et note l'heure.
func authentifier(jeton string) (string, bool) {
	if jeton == "" {
		return "", false
	}
	h := hacherJeton(jeton)
	nom, ok := "", false
	_ = avecVerrou(func() error {
		liste, err := lireAppareils()
		if err != nil {
			return err
		}
		for i := range liste {
			if hmac.Equal([]byte(liste[i].Hachage), []byte(h)) {
				nom, ok = liste[i].Nom, true
				liste[i].Vu = time.Now().Unix()
				return ecrireAppareils(liste)
			}
		}
		return nil
	})
	return nom, ok
}

func revoquer(nom string) (bool, error) {
	trouve := false
	err := avecVerrou(func() error {
		liste, err := lireAppareils()
		if err != nil {
			return err
		}
		garde := liste[:0]
		for _, a := range liste {
			if a.Nom == nom {
				trouve = true
				continue
			}
			garde = append(garde, a)
		}
		if !trouve {
			return nil
		}
		return ecrireAppareils(garde)
	})
	return trouve, err
}

// ---- Le jumelage ----
//
// Le code (6 chiffres) est gardé dans jumelage.json jusqu'à son expiration,
// son premier usage réussi, ou 5 essais ratés. Voir PROTOCOLE.md pour le
// calcul des preuves et du code de vérification.

const (
	dureeCode  = 10 * time.Minute
	essaisMax  = 5
	versionPro = "jasette/1"
)

type Jumelage struct {
	Code   string `json:"code"`
	Expire int64  `json:"expire"`
	Essais int    `json:"essais"`
}

func jumelagePath() string { return filepath.Join(serveurDir(), "jumelage.json") }

func nouveauCode() (Jumelage, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return Jumelage{}, err
	}
	j := Jumelage{Code: fmt.Sprintf("%06d", n.Int64()), Expire: time.Now().Add(dureeCode).Unix()}
	err = avecVerrou(func() error {
		data, _ := json.Marshal(j)
		return ecrireSecret(jumelagePath(), data)
	})
	return j, err
}

// codeVerification : 6 chiffres tirés du code et de l'empreinte. Le serveur
// l'affiche avec le code ; le client le calcule avec l'empreinte qu'il voit,
// avant de rien envoyer. S'ils diffèrent, quelqu'un est au milieu.
func codeVerification(code, empreinte string) string {
	s := sha256.Sum256([]byte(versionPro + "/verification|" + code + "|" + empreinte))
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(s[:4])%1_000_000)
}

func preuve(code, role, empreinte, nonceServeur, nonceClient string) []byte {
	m := hmac.New(sha256.New, []byte(code))
	m.Write([]byte(versionPro + "/" + role + "|" + empreinte + "|" + nonceServeur + "|" + nonceClient))
	return m.Sum(nil)
}

var errCode = errors.New("code de jumelage invalide ou expiré")

// verifierJumelage contrôle la preuve du client contre le code en cours.
// Réussite : le code est consommé, et on rend la preuve du serveur.
func verifierJumelage(empreinte, ns, nc, preuveHex string) ([]byte, error) {
	recue, err := hex.DecodeString(preuveHex)
	if err != nil || len(recue) != sha256.Size {
		recue = nil
	}
	var reponse []byte
	err = avecVerrou(func() error {
		data, err := os.ReadFile(jumelagePath())
		if err != nil {
			return errCode
		}
		var j Jumelage
		if json.Unmarshal(data, &j) != nil || time.Now().Unix() > j.Expire || j.Essais >= essaisMax {
			_ = os.Remove(jumelagePath())
			return errCode
		}
		if recue == nil || !hmac.Equal(recue, preuve(j.Code, "client", empreinte, ns, nc)) {
			j.Essais++
			if j.Essais >= essaisMax {
				logf("jumelage : trop d'essais ratés, code annulé")
				_ = os.Remove(jumelagePath())
			} else {
				data, _ := json.Marshal(j)
				_ = ecrireSecret(jumelagePath(), data)
			}
			return errCode
		}
		_ = os.Remove(jumelagePath())
		reponse = preuve(j.Code, "serveur", empreinte, ns, nc)
		return nil
	})
	return reponse, err
}
