// L'envoi vers un client : trois files, un seul écrivain.
//
//   - evs : les événements diffusés (messages, `sent`, états, listes…) ;
//     jamais bloquant pour la diffusion ;
//   - reponses : les réponses à ce client seul (pong, erreurs, dépôts, MCP,
//     aperçus, fichiers refusés ou inchangés) ;
//   - fichiers : les morceaux de fichiers (`get`).
//
// Les deux premières passent toujours avant les fichiers, et les fichiers
// ont un débit plafonné : un message n'attend jamais derrière des
// mégaoctets d'images (au plus un morceau déjà parti, et le petit tampon
// d'envoi TCP). Les demandes `get` attendent dans une file bornée ; au-delà,
// les plus vieilles sont abandonnées (le client redemandera).
package main

import (
	"bytes"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// Le débit des morceaux de fichiers, par client, et l'avance permise.
	debitFichiers = 4 << 20
	seauFichiers  = 512 << 10
	// Le tampon d'envoi TCP d'un client (le noyau le double) : ce qui est
	// parti dans la socket ne peut plus être doublé par un message.
	tamponEnvoi = 256 << 10
	// Les demandes `get` en attente d'un client, au plus, et leur âge maximal.
	getsEnAttenteMax = 48
	getAgeMax        = 90 * time.Second
	// La trace du débit : mesuré sur dix secondes, signalé au-delà du seuil,
	// une ligne par minute au plus.
	traceFenetre = 10 * time.Second
	traceSeuil   = 1 << 20 // octets par seconde
	tracePause   = time.Minute
)

// envoi : une trame à écrire, et sa sorte pour la trace.
type envoi struct {
	t     []byte
	sorte string
}

// demandeGet : un `get` en attente.
type demandeGet struct {
	id     int64
	chemin string
	sha    string
	quand  time.Time
}

// ---- Le seau du débit des fichiers ----

type seau struct {
	debit, capacite float64
	niveau          float64
	vu              time.Time
}

func nouveauSeau(debit, capacite int) *seau {
	return &seau{debit: float64(debit), capacite: float64(capacite), niveau: float64(capacite)}
}

// attente : combien attendre avant de pouvoir envoyer (0 : tout de suite).
func (s *seau) attente(now time.Time) time.Duration {
	s.remplir(now)
	if s.niveau > 0 {
		return 0
	}
	return time.Duration(-s.niveau / s.debit * float64(time.Second))
}

func (s *seau) prendre(n int, now time.Time) {
	s.remplir(now)
	s.niveau -= float64(n)
}

func (s *seau) remplir(now time.Time) {
	if !s.vu.IsZero() {
		s.niveau = min(s.capacite, s.niveau+now.Sub(s.vu).Seconds()*s.debit)
	}
	s.vu = now
}

// ---- La trace du débit ----

// compteurDebit : les octets écrits vers un client, par sorte, sur une
// fenêtre ; une ligne au journal (sans contenu) quand c'est trop.
type compteurDebit struct {
	debut   time.Time
	total   int
	sortes  map[string]int
	dernier time.Time // la dernière ligne au journal
}

// ajouter compte n octets ; rend la ligne à journaliser, ou "".
func (d *compteurDebit) ajouter(nom, sorte string, n int, now time.Time) string {
	if d.debut.IsZero() {
		d.debut = now
	}
	if d.sortes == nil {
		d.sortes = map[string]int{}
	}
	d.total += n
	d.sortes[sorte] += n
	duree := now.Sub(d.debut)
	if duree < traceFenetre {
		return ""
	}
	ligne := ""
	debit := float64(d.total) / duree.Seconds()
	if debit > traceSeuil && (d.dernier.IsZero() || now.Sub(d.dernier) >= tracePause) {
		type part struct {
			sorte string
			n     int
		}
		var parts []part
		for s, k := range d.sortes {
			parts = append(parts, part{s, k})
		}
		sort.Slice(parts, func(i, j int) bool {
			return parts[i].n > parts[j].n || parts[i].n == parts[j].n && parts[i].sorte < parts[j].sorte
		})
		var detail []string
		for i, p := range parts {
			if i == 3 {
				break
			}
			detail = append(detail, fmt.Sprintf("%s %d %%", p.sorte, p.n*100/max(d.total, 1)))
		}
		ligne = fmt.Sprintf("appareil %q : %.1f Mo/s envoyés sur %d s (%s)", nom, debit/(1<<20), int(duree.Seconds()), strings.Join(detail, ", "))
		d.dernier = now
	}
	d.debut, d.total, d.sortes = now, 0, map[string]int{}
	return ligne
}

// sorteDe : « service/événement » d'une trame diffusée ({"svc":…,"ev":{"ev":…}}),
// lue dans son début seulement (rien du contenu).
func sorteDe(t []byte) string {
	tete := t[:min(len(t), 160)]
	if !bytes.HasPrefix(tete, []byte(`{"svc":"`)) {
		if bytes.HasPrefix(tete, []byte(`{"marques"`)) {
			return "marques"
		}
		return "autre"
	}
	reste := tete[len(`{"svc":"`):]
	i := bytes.IndexByte(reste, '"')
	if i < 0 {
		return "autre"
	}
	svc := string(reste[:i])
	ev := "?"
	if j := bytes.Index(reste, []byte(`"ev":"`)); j >= 0 {
		r := reste[j+len(`"ev":"`):]
		if k := bytes.IndexByte(r, '"'); k >= 0 && k < 40 {
			ev = string(r[:k])
		}
	}
	return svc + "/" + ev
}

// ---- L'écrivain ----

// ecrivain : seul à écrire sur la connexion. Les événements et les réponses
// passent avant les morceaux de fichiers, qui ont un débit plafonné.
func (c *Client) ecrivain() {
	ping := time.NewTicker(intervallePing)
	defer ping.Stop()
	debit := nouveauSeau(debitFichiers, seauFichiers)
	var trace compteurDebit
	ecrire := func(e envoi) bool {
		_ = c.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := c.conn.Write(append(e.t, '\n')); err != nil {
			c.fermer()
			return false
		}
		if l := trace.ajouter(c.nom, e.sorte, len(e.t)+1, time.Now()); l != "" {
			logf("%s", l)
		}
		return true
	}
	diffusee := func(t []byte) envoi { return envoi{t, sorteDe(t)} }
	var minuterie *time.Timer
	defer func() {
		if minuterie != nil {
			minuterie.Stop()
		}
	}()
	for {
		// D'abord tout ce qui est prioritaire.
		select {
		case t := <-c.evs:
			if !ecrire(diffusee(t)) {
				return
			}
			continue
		case e := <-c.reponses:
			if !ecrire(e) {
				return
			}
			continue
		case <-c.fini:
			return
		default:
		}
		// Puis ce qui vient : un fichier seulement si le débit le permet.
		var fichiers <-chan envoi
		var reveil <-chan time.Time
		if d := debit.attente(time.Now()); d <= 0 {
			fichiers = c.fichiers
		} else {
			if minuterie == nil {
				minuterie = time.NewTimer(d)
			} else {
				minuterie.Reset(d)
			}
			reveil = minuterie.C
		}
		select {
		case t := <-c.evs:
			if !ecrire(diffusee(t)) {
				return
			}
		case e := <-c.reponses:
			if !ecrire(e) {
				return
			}
		case e := <-fichiers:
			debit.prendre(len(e.t)+1, time.Now())
			if !ecrire(e) {
				return
			}
		case <-reveil:
		case <-ping.C:
			if !ecrire(envoi{fmt.Appendf(nil, `{"ping":%d}`, time.Now().UnixMilli()), "ping"}) {
				return
			}
		case <-c.fini:
			return
		}
		if minuterie != nil && reveil != nil && !minuterie.Stop() {
			select {
			case <-minuterie.C:
			default:
			}
		}
	}
}

// reglerTampon : un petit tampon d'envoi TCP, pour que la priorité des
// événements ne soit pas perdue dans la file du noyau.
func reglerTampon(brute net.Conn) {
	type avecNetConn interface{ NetConn() net.Conn }
	if a, ok := brute.(avecNetConn); ok {
		brute = a.NetConn()
	}
	if tc, ok := brute.(*net.TCPConn); ok {
		_ = tc.SetWriteBuffer(tamponEnvoi)
	}
}

// ---- Les demandes de fichiers ----

// demanderGet : met un `get` en file (sans bloquer la lecture des commandes).
// File trop longue : les plus vieilles demandes sont abandonnées.
func (c *Client) demanderGet(id int64, chemin, sha string) {
	var abandons []int64
	c.muGets.Lock()
	c.getsAttente = append(c.getsAttente, demandeGet{id, chemin, sha, time.Now()})
	for len(c.getsAttente) > getsEnAttenteMax {
		abandons = append(abandons, c.getsAttente[0].id)
		c.getsAttente = c.getsAttente[1:]
	}
	c.muGets.Unlock()
	for _, a := range abandons {
		c.abandonnerGet(a)
	}
	select {
	case c.signalGets <- struct{}{}:
	default:
	}
}

func (c *Client) abandonnerGet(id int64) {
	c.envoyerA(map[string]any{"fichier": map[string]any{"id": id, "erreur": "abandonné : trop de fichiers en attente", "fin": true}})
}

// prochainGet : la plus vieille demande encore fraîche (les trop vieilles
// sont abandonnées), ou faux s'il n'y en a pas.
func (c *Client) prochainGet() (demandeGet, bool) {
	var abandons []int64
	defer func() {
		for _, a := range abandons {
			c.abandonnerGet(a)
		}
	}()
	c.muGets.Lock()
	defer c.muGets.Unlock()
	for len(c.getsAttente) > 0 {
		g := c.getsAttente[0]
		c.getsAttente = c.getsAttente[1:]
		if time.Since(g.quand) > getAgeMax {
			abandons = append(abandons, g.id)
			continue
		}
		if len(c.getsAttente) > 0 {
			// Il en reste : un autre ouvrier peut s'y mettre.
			select {
			case c.signalGets <- struct{}{}:
			default:
			}
		}
		return g, true
	}
	return demandeGet{}, false
}

// ouvrierGets : sert les `get` en file, l'un après l'autre (getsSimultanes
// ouvriers par client).
func (c *Client) ouvrierGets() {
	for {
		g, ok := c.prochainGet()
		if !ok {
			select {
			case <-c.signalGets:
				continue
			case <-c.fini:
				return
			}
		}
		c.servirFichier(g.id, g.chemin, g.sha)
	}
}

// envoyerFichier : un morceau de fichier (bloque tant que la file est pleine).
func (c *Client) envoyerFichier(t []byte, sorte string) bool {
	select {
	case c.fichiers <- envoi{t, sorte}:
		return true
	case <-c.fini:
		return false
	}
}

// ---- L'origine des fichiers (pour la trace) ----

const originesMax = 4000

// noterOrigine : ce fichier vient du pont `svc` (événement `media`). Sous s.mu.
func (s *Serveur) noterOrigine(chemin, svc string) {
	if chemin == "" {
		return
	}
	if s.origines == nil || len(s.origines) >= originesMax {
		s.origines = map[string]string{}
	}
	s.origines[chemin] = svc
}

func (s *Serveur) origine(chemin string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc := s.origines[chemin]; svc != "" {
		return "fichiers/" + svc
	}
	return "fichiers"
}

// ---- Les téléchargements : la réponse `media` à qui l'a demandée ----

// dureeTelechargement : au-delà, une demande `download` est oubliée (la
// réponse `media` repart alors à tous, comme avant).
const dureeTelechargement = 15 * time.Minute

// noterTelechargement : l'appareil `hachage` attend `media` pour ce média.
// Sous s.mu.
func (p *Pont) noterTelechargement(mediaID, hachage string) {
	if mediaID == "" {
		return
	}
	if p.telechargements == nil {
		p.telechargements = map[string]map[string]time.Time{}
	}
	if len(p.telechargements) > 2000 {
		for id, qui := range p.telechargements {
			for h, t := range qui {
				if time.Since(t) > dureeTelechargement {
					delete(qui, h)
				}
			}
			if len(qui) == 0 {
				delete(p.telechargements, id)
			}
		}
	}
	qui := p.telechargements[mediaID]
	if qui == nil {
		qui = map[string]time.Time{}
		p.telechargements[mediaID] = qui
	}
	qui[hachage] = time.Now()
}

// routerMedia : la réponse `media` ne va qu'aux appareils qui l'ont demandée
// (tous leurs branchements). Faux si personne ne l'attend : elle part alors
// à tous. Sous s.mu.
func (p *Pont) routerMedia(mediaID string, trame []byte) bool {
	qui := p.telechargements[mediaID]
	delete(p.telechargements, mediaID)
	voulu := map[string]bool{}
	for h, t := range qui {
		if time.Since(t) <= dureeTelechargement {
			voulu[h] = true
		}
	}
	if len(voulu) == 0 {
		return false
	}
	for c := range p.srv.clients {
		if !voulu[c.hachage] {
			continue
		}
		select {
		case c.evs <- trame:
		default:
			logf("client %q trop lent, débranché", c.nom)
			c.fermer()
		}
	}
	return true
}

// mutex pour les champs des gets (déclaré ici pour garder Client lisible).
type filesGets struct {
	muGets      sync.Mutex
	getsAttente []demandeGet
	signalGets  chan struct{}
}
