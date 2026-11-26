package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Un banc d'essai : horloge à la main, attentes notées, requêtes comptées.
type banc struct {
	a          *photosAuteurs
	maintenant time.Time
	attentes   []time.Duration
	requetes   []string
	annonces   map[string]string
	reponse    func(id string) (string, error)
}

var errRefus = errors.New("refus")

func nouveauBanc(t *testing.T) *banc {
	t.Helper()
	dir := t.TempDir()
	photo := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(photo, []byte("jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := &banc{maintenant: time.Date(2026, 11, 26, 10, 0, 0, 0, time.Local), annonces: map[string]string{}}
	b.reponse = func(string) (string, error) { return photo, nil }
	b.a = nouvellesPhotosAuteurs(filepath.Join(dir, "auteurs.json"),
		func(_ context.Context, id string) (string, error) {
			b.requetes = append(b.requetes, id)
			return b.reponse(id)
		},
		func(err error) bool { return errors.Is(err, errRefus) })
	b.a.horloge = func() time.Time { return b.maintenant }
	b.a.attendre = func(_ context.Context, d time.Duration) error {
		b.attentes = append(b.attentes, d)
		b.maintenant = b.maintenant.Add(d)
		return nil
	}
	b.a.emettre = func(id, path string) { b.annonces[id] = path }
	return b
}

// vider : traite toute la file.
func (b *banc) vider(t *testing.T) {
	t.Helper()
	for {
		select {
		case id := <-b.a.file:
			if err := b.a.traiter(context.Background(), id); err != nil {
				t.Fatal(err)
			}
		default:
			return
		}
	}
}

func TestAuteurUneFoisParJour(t *testing.T) {
	b := nouveauBanc(t)
	b.a.noter("alice")
	b.a.noter("alice") // déjà en file
	b.vider(t)
	if len(b.requetes) != 1 || b.annonces["alice"] == "" {
		t.Fatalf("requêtes %v, annonces %v", b.requetes, b.annonces)
	}
	b.maintenant = b.maintenant.Add(3 * time.Hour)
	b.a.noter("alice")
	b.vider(t)
	if len(b.requetes) != 1 {
		t.Fatalf("redemandée le même jour : %v", b.requetes)
	}
	// Le lendemain : une nouvelle demande.
	b.maintenant = b.maintenant.Add(24 * time.Hour)
	b.a.noter("alice")
	b.vider(t)
	if len(b.requetes) != 2 {
		t.Fatalf("pas redemandée le lendemain : %v", b.requetes)
	}
}

func TestAuteurCacheSurDisque(t *testing.T) {
	b := nouveauBanc(t)
	b.a.noter("alice")
	b.vider(t)
	// Une nouvelle session (pont relancé) le même jour : l'état est relu,
	// la photo est redonnée sans réseau.
	a2 := nouvellesPhotosAuteurs(b.a.fichier, func(context.Context, string) (string, error) {
		t.Fatal("requête alors que la photo du jour est connue")
		return "", nil
	}, func(error) bool { return true })
	a2.horloge = b.a.horloge
	vues := map[string]string{}
	a2.emettre = func(id, path string) { vues[id] = path }
	a2.noter("alice")
	a2.noter("alice")
	if vues["alice"] == "" || len(a2.file) != 0 {
		t.Fatalf("photo connue non redonnée (%v) ou redemandée (%d en file)", vues, len(a2.file))
	}
	if a2.connue("alice") == "" || a2.connue("bob") != "" {
		t.Fatal("connue()")
	}
}

func TestAuteurPlafondParMinute(t *testing.T) {
	b := nouveauBanc(t)
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		b.a.noter(id)
	}
	debut := b.maintenant
	b.vider(t)
	if len(b.requetes) != 12 {
		t.Fatalf("%d requêtes", len(b.requetes))
	}
	// Étalées : une toutes les six secondes, jamais deux d'un coup.
	for _, d := range b.attentes {
		if d < 6*time.Second-time.Millisecond {
			t.Fatalf("attente trop courte : %v", d)
		}
	}
	if ecoule := b.maintenant.Sub(debut); ecoule < 66*time.Second {
		t.Fatalf("12 requêtes en %v : plus de 10 par minute", ecoule)
	}
}

func TestAuteurPasDeRafale(t *testing.T) {
	b := nouveauBanc(t)
	// Un gros groupe ouvert : 500 auteurs d'un coup. La file reste petite ;
	// noter ne bloque jamais.
	for i := range 500 {
		b.a.noter(string(rune('A'+i%26)) + string(rune('a'+i/26)))
	}
	if n := len(b.a.file); n != auteursFile {
		t.Fatalf("file : %d", n)
	}
}

func TestAuteurPlafondParJour(t *testing.T) {
	b := nouveauBanc(t)
	b.a.etat.Jour, b.a.etat.Compte = b.a.jour(), auteursParJour
	b.a.noter("alice")
	b.vider(t)
	if len(b.requetes) != 0 {
		t.Fatalf("plafond du jour dépassé : %v", b.requetes)
	}
	// Le lendemain, le compte repart à zéro.
	b.maintenant = b.maintenant.Add(24 * time.Hour)
	b.a.noter("alice")
	b.vider(t)
	if len(b.requetes) != 1 || b.a.etat.Compte != 1 {
		t.Fatalf("requêtes %v, compte %d", b.requetes, b.a.etat.Compte)
	}
}

func TestAuteurRefus(t *testing.T) {
	b := nouveauBanc(t)
	b.reponse = func(string) (string, error) { return "", errRefus }
	b.a.noter("bob")
	b.vider(t)
	b.a.noter("bob")
	b.vider(t)
	if len(b.requetes) != 1 || b.annonces["bob"] != "" || !b.a.etat.Gens["bob"].Refus {
		t.Fatalf("refus : requêtes %v, annonces %v", b.requetes, b.annonces)
	}
	// Une panne passagère n'est pas notée : on réessaiera en revoyant l'auteur.
	b.reponse = func(string) (string, error) { return "", errors.New("réseau") }
	b.a.noter("carole")
	b.vider(t)
	if _, ok := b.a.etat.Gens["carole"]; ok {
		t.Fatal("panne passagère notée comme refus")
	}
	b.maintenant = b.maintenant.Add(24 * time.Hour)
	b.reponse = func(string) (string, error) { return "", nil }
	b.a.noter("bob")
	b.vider(t)
	if len(b.requetes) != 3 {
		t.Fatalf("pas de nouvel essai le lendemain : %v", b.requetes)
	}
}
