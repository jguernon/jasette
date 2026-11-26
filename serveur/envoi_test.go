package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Un message diffusé passe devant 10 Mo de fichiers en cours d'envoi : au
// plus un morceau déjà parti le précède.
func TestMessageAvantFichiers(t *testing.T) {
	dossier := t.TempDir()
	gros := filepath.Join(dossier, "gros.png")
	if err := os.WriteFile(gros, bytes.Repeat([]byte{7}, 10<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Serveur{clients: map[*Client]struct{}{}, racines: []racine{{chemin: dossier}}}
	serveurCote, clientCote := net.Pipe()
	defer clientCote.Close()
	c := nouveauClient(s, serveurCote, "essai", "h", "local")
	defer c.fermer()
	go c.ecrivain()
	for range getsSimultanes {
		go c.ouvrierGets()
	}
	// Plusieurs fichiers demandés, le même gros fichier chaque fois.
	for i := range 5 {
		c.demanderGet(int64(i+1), gros, "")
	}
	lu := bufio.NewReaderSize(clientCote, 1<<20)
	_ = clientCote.SetReadDeadline(time.Now().Add(10 * time.Second))
	premiere, err := lu.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(premiere, []byte(`{"fichier"`)) {
		t.Fatalf("première trame : %.60s", premiere)
	}
	message := emballer("messenger", []byte(`{"ev":"message","item":{"id":"m1"}}`))
	c.evs <- message
	morceaux, octets := 0, 0
	for {
		l, err := lu.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(bytes.TrimSpace(l), message) {
			break
		}
		if !bytes.HasPrefix(l, []byte(`{"fichier"`)) {
			continue // un ping
		}
		morceaux++
		octets += len(l)
	}
	if morceaux > 1 || octets > 300<<10 {
		t.Fatalf("le message a attendu %d morceaux (%d octets)", morceaux, octets)
	}
}

// Les réponses (pong, erreurs) passent aussi avant les fichiers.
func TestReponseAvantFichiers(t *testing.T) {
	s := &Serveur{clients: map[*Client]struct{}{}}
	serveurCote, clientCote := net.Pipe()
	defer clientCote.Close()
	c := nouveauClient(s, serveurCote, "essai", "h", "local")
	defer c.fermer()
	// Deux morceaux en file avant l'écrivain, puis une réponse.
	c.fichiers <- envoi{[]byte(`{"fichier":{"id":1}}`), "fichiers"}
	c.fichiers <- envoi{[]byte(`{"fichier":{"id":1}}`), "fichiers"}
	c.envoyerA(map[string]any{"pong": 1})
	go c.ecrivain()
	lu := bufio.NewReader(clientCote)
	_ = clientCote.SetReadDeadline(time.Now().Add(5 * time.Second))
	l, err := lu.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(l), `{"pong"`) {
		t.Fatalf("première trame : %s", l)
	}
}

// Une file de `get` trop longue : les plus vieux sont abandonnés, et le
// client le sait (erreur, fin).
func TestGetsAbandonnes(t *testing.T) {
	s := &Serveur{clients: map[*Client]struct{}{}}
	c := nouveauClient(s, nil, "essai", "h", "local")
	for i := range getsEnAttenteMax + 5 {
		c.demanderGet(int64(i+1), "/x", "")
	}
	if len(c.getsAttente) != getsEnAttenteMax {
		t.Fatalf("en attente : %d", len(c.getsAttente))
	}
	if c.getsAttente[0].id != 6 {
		t.Fatalf("le plus vieux gardé : %d", c.getsAttente[0].id)
	}
	if len(c.reponses) != 5 {
		t.Fatalf("abandons signalés : %d", len(c.reponses))
	}
	e := <-c.reponses
	var r struct {
		Fichier struct {
			ID     int64  `json:"id"`
			Erreur string `json:"erreur"`
			Fin    bool   `json:"fin"`
		} `json:"fichier"`
	}
	if json.Unmarshal(e.t, &r) != nil || r.Fichier.ID != 1 || r.Fichier.Erreur == "" || !r.Fichier.Fin {
		t.Fatalf("abandon : %s", e.t)
	}
	// Une demande trop vieille n'est pas servie.
	c.getsAttente[0].quand = time.Now().Add(-2 * getAgeMax)
	g, ok := c.prochainGet()
	if !ok || g.id != 7 {
		t.Fatalf("prochain : %v %v", g, ok)
	}
}

// La réponse `media` ne va qu'aux appareils qui ont demandé le fichier ;
// sans demande connue, elle part à tous (comme avant).
func TestMediaRoutee(t *testing.T) {
	s := &Serveur{clients: map[*Client]struct{}{}}
	pc := &Client{srv: s, nom: "pc", hachage: "a", evs: make(chan []byte, 4)}
	pc2 := &Client{srv: s, nom: "pc (2)", hachage: "a", evs: make(chan []byte, 4)}
	fold := &Client{srv: s, nom: "fold", hachage: "b", evs: make(chan []byte, 4)}
	for _, c := range []*Client{pc, pc2, fold} {
		s.clients[c] = struct{}{}
	}
	p := &Pont{svc: "discord", srv: s, dernier: map[string][]byte{}}
	s.ponts = map[string]*Pont{"discord": p}
	s.mu.Lock()
	p.noterTelechargement("dc:1", "a")
	s.mu.Unlock()
	p.recu([]byte(`{"ev":"media","media_id":"dc:1","path":"/m/pieces/x.webp"}`))
	if len(pc.evs) != 1 || len(pc2.evs) != 1 || len(fold.evs) != 0 {
		t.Fatalf("routée : pc %d, pc2 %d, fold %d", len(pc.evs), len(pc2.evs), len(fold.evs))
	}
	if s.origine("/m/pieces/x.webp") != "fichiers/discord" {
		t.Fatal("origine du fichier oubliée")
	}
	// Une seconde réponse (personne ne l'attend plus) : à tous.
	p.recu([]byte(`{"ev":"media","media_id":"dc:1","path":"/m/pieces/x.webp"}`))
	if len(pc.evs) != 2 || len(fold.evs) != 1 {
		t.Fatalf("diffusée : pc %d, fold %d", len(pc.evs), len(fold.evs))
	}
}

// La trace : une ligne quand le débit dépasse le seuil, avec la sorte
// dominante, puis plus rien pendant une minute.
func TestTraceDebit(t *testing.T) {
	var d compteurDebit
	t0 := time.Unix(1000, 0)
	d.ajouter("fold", "discord/message", 100, t0)
	var ligne string
	for i := 1; i <= 100; i++ {
		if l := d.ajouter("fold", "fichiers/discord", 256<<10, t0.Add(time.Duration(i)*100*time.Millisecond)); l != "" {
			ligne = l
		}
	}
	if !strings.Contains(ligne, `"fold"`) || !strings.Contains(ligne, "fichiers/discord 99 %") || !strings.Contains(ligne, "Mo/s") {
		t.Fatalf("trace : %q", ligne)
	}
	for i := 101; i <= 400; i++ {
		if l := d.ajouter("fold", "fichiers/discord", 256<<10, t0.Add(time.Duration(i)*100*time.Millisecond)); l != "" {
			t.Fatalf("deux lignes en moins d'une minute : %q", l)
		}
	}
	if l := d.ajouter("fold", "fichiers/discord", 50<<20, t0.Add(80*time.Second)); l == "" {
		t.Fatal("rien après la pause")
	}
	// Un débit faible : rien.
	var calme compteurDebit
	for i := range 100 {
		if l := calme.ajouter("pc", "google/message", 1000, t0.Add(time.Duration(i)*time.Second)); l != "" {
			t.Fatalf("débit faible signalé : %q", l)
		}
	}
}

func TestSorteDe(t *testing.T) {
	cas := map[string]string{
		string(emballer("discord", []byte(`{"ev":"media","media_id":"dc:1"}`))):        "discord/media",
		string(emballer("google", []byte(`{"conv":"c1","ev":"messages","items":[]}`))): "google/messages",
		`{"marques":{}}`: "marques",
	}
	for trame, voulu := range cas {
		if s := sorteDe([]byte(trame)); s != voulu {
			t.Errorf("%s : %q, voulu %q", trame, s, voulu)
		}
	}
}

// Le seau : une avance, puis le débit.
func TestSeau(t *testing.T) {
	t0 := time.Unix(0, 0)
	s := nouveauSeau(1<<20, 512<<10)
	if s.attente(t0) != 0 {
		t.Fatal("seau plein : attente")
	}
	s.prendre(1<<20, t0)
	if d := s.attente(t0); d < 400*time.Millisecond || d > 600*time.Millisecond {
		t.Fatalf("attente : %v", d)
	}
	if s.attente(t0.Add(time.Second)) != 0 {
		t.Fatal("pas rempli après une seconde")
	}
}
