package main

// Tests de notes_commun.go (copié tel quel dans chaque pont qui publie une note).

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fauxService struct {
	publies  []string
	effaces  int
	echec    error
	ancienne string
}

func notesEssai(t *testing.T, f *fauxService, duree time.Duration) (*notesPont, *time.Time) {
	t.Helper()
	t.Setenv("JASETTE_DATA", t.TempDir())
	h := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	n := nouvellesNotes(opsNote{
		nom: "Essai", limite: 10, parHeure: 5, duree: duree,
		publier: func(_ context.Context, texte string, _ int, e *etatNote) (string, error) {
			if f.echec != nil {
				return "", f.echec
			}
			if e.Ancienne == nil {
				a := f.ancienne
				e.Ancienne = &a
			}
			f.publies = append(f.publies, texte)
			return "id-" + texte, nil
		},
		effacer: func(_ context.Context, e *etatNote) error {
			if f.echec != nil {
				return f.echec
			}
			f.effaces++
			return nil
		},
	})
	n.maintenant = func() time.Time { return h }
	return n, &h
}

func TestNoteSetLimitesEtDoublons(t *testing.T) {
	f := &fauxService{}
	n, _ := notesEssai(t, f, 0)
	ctx := context.Background()
	n.set(ctx, "", 0, "", "a", true)
	n.set(ctx, "beaucoup trop long", 0, "", "b", true)
	n.set(ctx, "salut", 0, "", "c", false)
	if len(f.publies) != 0 {
		t.Fatalf("publié malgré tout : %v", f.publies)
	}
	n.set(ctx, "salut", 0, "", "d", true)
	n.set(ctx, " salut ", 0, "", "e", true) // la même : rien à republier
	if len(f.publies) != 1 || n.etat.Text != "salut" || n.etat.ID != "id-salut" {
		t.Fatalf("publies %v, état %+v", f.publies, n.etat)
	}
	// Relu depuis le disque.
	m := nouvellesNotes(n.ops)
	m.mu.Lock()
	m.charger()
	m.mu.Unlock()
	if m.etat.Text != "salut" {
		t.Fatalf("pas gardé : %+v", m.etat)
	}
}

func TestNoteQuotaParHeure(t *testing.T) {
	f := &fauxService{}
	n, h := notesEssai(t, f, 0)
	ctx := context.Background()
	for _, s := range []string{"a", "b", "c", "d", "e", "f"} {
		n.set(ctx, s, 0, "", "", true)
	}
	if len(f.publies) != 5 {
		t.Fatalf("quota : %v", f.publies)
	}
	*h = h.Add(61 * time.Minute)
	n.set(ctx, "g", 0, "", "", true)
	if len(f.publies) != 6 {
		t.Fatalf("après une heure : %v", f.publies)
	}
}

func TestNoteErreurClaire(t *testing.T) {
	f := &fauxService{echec: errors.New("boum")}
	n, _ := notesEssai(t, f, 0)
	n.set(context.Background(), "x", 0, "", "", true)
	if n.etat.Text != "" {
		t.Fatal("échec gardé comme note")
	}
	if e := n.clair("set", f.echec).Error(); e != "La note Essai n'a pas pu être publiée : boum" {
		t.Fatal(e)
	}
	if e := n.clair("set", erreurNote("telle quelle")).Error(); e != "telle quelle" {
		t.Fatal(e)
	}
}

// La musique passe par-dessus la note écrite à la main, qui revient quand la
// musique s'efface (si elle a moins de 24 h) ; sinon, effacement.
func TestNoteMusiqueRemetLaNoteMain(t *testing.T) {
	f := &fauxService{}
	n, h := notesEssai(t, f, 24*time.Hour)
	ctx := context.Background()
	n.set(ctx, "chalet", 1, "main", "", true)
	*h = h.Add(10 * time.Minute)
	n.set(ctx, "🎵 Daft", 0, "musique", "", true)
	if n.etat.Source != "musique" || n.etat.MainText != "chalet" || n.etat.MainAudience != 1 {
		t.Fatalf("pas mise de côté : %+v", n.etat)
	}
	if ev := n.noteMoi(); ev["main_text"] != "chalet" {
		t.Fatalf("note_moi : %v", ev)
	}
	n.set(ctx, "🎵 Air", 0, "musique", "", true)
	if n.etat.MainText != "chalet" {
		t.Fatal("note main perdue au changement de musique")
	}
	*h = h.Add(30 * time.Minute)
	n.clear(ctx, "musique", "", true)
	if n.etat.Text != "chalet" || n.etat.Source != "main" || n.etat.MainText != "" || f.effaces != 0 ||
		f.publies[len(f.publies)-1] != "chalet" {
		t.Fatalf("pas remise : %+v %v", n.etat, f.publies)
	}
	// Un effacement musical ne touche pas une note écrite à la main.
	n.clear(ctx, "musique", "", true)
	if n.etat.Text != "chalet" {
		t.Fatal("note main effacée par la musique")
	}
	// Note main trop vieille : la musique s'efface pour de bon.
	*h = h.Add(2 * time.Hour)
	n.set(ctx, "🎵 Daft", 0, "musique", "", true)
	*h = h.Add(23 * time.Hour)
	n.clear(ctx, "musique", "", true)
	if n.etat.Text != "" || f.effaces != 1 {
		t.Fatalf("pas effacée : %+v", n.etat)
	}
}

func TestNoteEffacerRemetLaBio(t *testing.T) {
	f := &fauxService{ancienne: "ma bio"}
	n, _ := notesEssai(t, f, 0)
	ctx := context.Background()
	n.set(ctx, "note", 0, "", "", true)
	if n.etat.Ancienne == nil || *n.etat.Ancienne != "ma bio" {
		t.Fatalf("bio pas gardée : %+v", n.etat)
	}
	n.clear(ctx, "", "", true)
	if f.effaces != 1 || n.etat.Ancienne != nil || n.etat.Text != "" {
		t.Fatalf("après effacement : %+v", n.etat)
	}
	n.clear(ctx, "", "", true) // rien à effacer : aucune requête
	if f.effaces != 1 {
		t.Fatal("effacement inutile")
	}
}

func TestNoteEchue(t *testing.T) {
	f := &fauxService{}
	n, h := notesEssai(t, f, time.Hour)
	n.set(context.Background(), "x", 0, "", "", true)
	*h = h.Add(2 * time.Hour)
	if ev := n.noteMoi(); ev["text"] != "" {
		t.Fatalf("échue encore annoncée : %v", ev)
	}
}
