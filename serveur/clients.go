// Les clients : connexion TLS, jumelage ou authentification, puis échange
// de lignes JSON (commandes vers les ponts, événements, fichiers, ping).
// Le détail des trames est dans PROTOCOLE.md.
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	tailleLigneMax  = 4 << 20   // une trame, morceaux de fichiers compris
	tailleDepotMax  = 200 << 20 // un fichier téléversé
	tailleMorceau   = 192 << 10 // morceau de fichier envoyé (avant base64)
	intervallePing  = 25 * time.Second
	delaiSilence    = 90 * time.Second // sans rien recevoir : client perdu
	delaiAccueil    = 20 * time.Second // poignée de main TLS + auth/jumelage
	dureeDepots     = time.Hour
	getsSimultanes  = 2
	depotsSimultane = 8
)

type racine struct {
	chemin          string
	mediasSeulement bool // dans les données : seulement les images, sons, vidéos, PDF
}

type Serveur struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
	ponts   map[string]*Pont
	marques Marques // sous mu ; voir marques.go

	empreinte string
	racines   []racine
	interdit  string // le dossier du serveur lui-même : jamais servi
	depots    string
	limiteur  *Limiteur
	mcp       *ServiceMCP // le serveur MCP (mcp.go)

	// Sous mu : le pont d'où vient chaque fichier annoncé par `media` (trace).
	origines map[string]string
}

type Client struct {
	srv     *Serveur
	conn    net.Conn // *tls.Conn ; une autre connexion dans les tests
	nom     string
	hachage string
	adresse string

	// Les files d'envoi (envoi.go) : evs et reponses passent avant fichiers.
	evs      chan []byte // événements diffusés : jamais bloquant
	reponses chan envoi  // réponses à ce client seul : bloquant
	fichiers chan envoi  // morceaux de fichiers : bloquant, débit plafonné
	fini     chan struct{}
	unique   sync.Once

	filesGets                  // les `get` en attente (envoi.go)
	depots    map[int64]*depot // lu et écrit par le seul fil de lecture
}

type depot struct {
	f      *os.File
	chemin string
	taille int64
}

// ---- Diffusion ----

// diffuserVerrouille envoie une trame à tous les clients (sous srv.mu).
// Un client trop lent pour suivre est débranché ; il se reconnectera.
func (s *Serveur) diffuserVerrouille(trame []byte) {
	for c := range s.clients {
		select {
		case c.evs <- trame:
		default:
			logf("client %q trop lent, débranché", c.nom)
			c.fermer()
		}
	}
}

// inscrire ajoute un client et lui rejoue les marques, puis l'état de
// chaque pont, sans trou ni doublon avec la diffusion.
func (s *Serveur) inscrire(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.evs <- trameMarques(s.marques, nil, "")
	for _, svc := range s.servicesTries() {
		for _, t := range s.ponts[svc].rejeu() {
			c.evs <- t
		}
	}
	s.clients[c] = struct{}{}
}

func (s *Serveur) rejouer(c *Client, svc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.ponts[svc]; p != nil {
		for _, t := range p.rejeu() {
			select {
			case c.evs <- t:
			default:
			}
		}
	}
}

func (s *Serveur) desinscrire(c *Client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
}

func (s *Serveur) servicesTries() []string {
	var r []string
	for _, svc := range ordreServices {
		if s.ponts[svc] != nil {
			r = append(r, svc)
		}
	}
	return r
}

// surveillerRevocations débranche les appareils retirés de appareils.json
// (par --revoquer, pendant que le serveur tourne).
func (s *Serveur) surveillerRevocations() {
	var vu time.Time
	for range time.Tick(3 * time.Second) {
		st, err := os.Stat(appareilsPath())
		var mod time.Time
		if err == nil {
			mod = st.ModTime()
		}
		if mod.Equal(vu) {
			continue
		}
		vu = mod
		liste, err := lireAppareils()
		if err != nil {
			continue
		}
		valides := map[string]bool{}
		for _, a := range liste {
			valides[a.Hachage] = true
		}
		s.mu.Lock()
		for c := range s.clients {
			if !valides[c.hachage] {
				logf("appareil %q révoqué : débranché", c.nom)
				c.fermer()
			}
		}
		s.mu.Unlock()
	}
}

// nettoyerDepots efface les téléversements de plus d'une heure.
func (s *Serveur) nettoyerDepots() {
	for {
		entrees, _ := os.ReadDir(s.depots)
		for _, e := range entrees {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > dureeDepots {
				_ = os.RemoveAll(filepath.Join(s.depots, e.Name()))
			}
		}
		time.Sleep(10 * time.Minute)
	}
}

// ---- Accueil d'une connexion ----

type accueil struct {
	Jumeler *struct {
		Nom    string `json:"nom"`
		Nonce  string `json:"nonce"`
		Preuve string `json:"preuve"`
	} `json:"jumeler"`
	Auth *struct {
		Jeton string `json:"jeton"`
	} `json:"auth"`
}

func (s *Serveur) servir(brute net.Conn) {
	conn := brute.(*tls.Conn)
	adresse := hoteDe(brute.RemoteAddr())
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(delaiAccueil))
	if err := conn.Handshake(); err != nil {
		return
	}
	lu := bufio.NewReaderSize(conn, 64<<10)

	nonce := aleatoire(32)
	if ecrireJSON(conn, map[string]any{"jasette": "serveur", "version": 1, "nonce": nonce}) != nil {
		return
	}
	ligne, err := lireLigne(lu, 64<<10)
	if err != nil {
		return
	}
	var a accueil
	if json.Unmarshal(ligne, &a) != nil {
		_ = ecrireJSON(conn, map[string]any{"erreur": "trame", "texte": "trame illisible"})
		return
	}
	switch {
	case a.Jumeler != nil:
		if !s.limiteur.permis("j" + adresse) {
			logf("jumelage refusé pour %s : trop d'essais", adresse)
			_ = ecrireJSON(conn, map[string]any{"erreur": "limite", "texte": "trop d'essais, réessayez dans quelques minutes"})
			return
		}
		srvPreuve, err := verifierJumelage(s.empreinte, nonce, a.Jumeler.Nonce, a.Jumeler.Preuve)
		if err != nil || len(a.Jumeler.Nonce) < 16 {
			logf("jumelage raté depuis %s", adresse)
			time.Sleep(time.Second)
			_ = ecrireJSON(conn, map[string]any{"erreur": "code", "texte": errCode.Error()})
			return
		}
		jeton, nom, err := nouvelAppareil(a.Jumeler.Nom)
		if err != nil {
			logf("jumelage : %v", err)
			_ = ecrireJSON(conn, map[string]any{"erreur": "interne", "texte": "impossible d'enregistrer l'appareil"})
			return
		}
		logf("appareil %q jumelé depuis %s", nom, adresse)
		_ = ecrireJSON(conn, map[string]any{"jumele": map[string]any{
			"jeton": jeton, "nom": nom, "preuve": hex.EncodeToString(srvPreuve),
		}})
		return
	case a.Auth != nil:
		if !s.limiteur.permis("a" + adresse) {
			_ = ecrireJSON(conn, map[string]any{"erreur": "limite", "texte": "trop d'essais"})
			return
		}
		nom, ok := authentifier(a.Auth.Jeton)
		if !ok {
			logf("jeton refusé depuis %s", adresse)
			time.Sleep(time.Second)
			_ = ecrireJSON(conn, map[string]any{"erreur": "jeton", "texte": "appareil inconnu ou révoqué : jumelez-le de nouveau"})
			return
		}
		s.limiteur.oublier("a" + adresse)
		c := nouveauClient(s, conn, nom, hacherJeton(a.Auth.Jeton), adresse)
		reglerTampon(conn)
		services := s.servicesTries()
		if ecrireJSON(conn, map[string]any{"auth": "ok", "nom": nom, "services": services}) != nil {
			return
		}
		_ = conn.SetDeadline(time.Time{})
		c.boucle(lu)
	default:
		_ = ecrireJSON(conn, map[string]any{"erreur": "trame", "texte": "attendu : jumeler ou auth"})
	}
}

func nouveauClient(s *Serveur, conn net.Conn, nom, hachage, adresse string) *Client {
	c := &Client{
		srv: s, conn: conn, nom: nom, hachage: hachage, adresse: adresse,
		evs: make(chan []byte, 8192), reponses: make(chan envoi, 256), fichiers: make(chan envoi, 2),
		fini: make(chan struct{}), depots: map[int64]*depot{},
	}
	c.signalGets = make(chan struct{}, 1)
	return c
}

func (c *Client) fermer() {
	c.unique.Do(func() {
		close(c.fini)
		_ = c.conn.Close()
	})
}

func (c *Client) boucle(lu *bufio.Reader) {
	logf("appareil %q branché depuis %s", c.nom, c.adresse)
	c.srv.inscrire(c)
	go c.ecrivain()
	for range getsSimultanes {
		go c.ouvrierGets()
	}
	defer func() {
		c.srv.desinscrire(c)
		c.fermer()
		for _, d := range c.depots {
			_ = d.f.Close()
			_ = os.RemoveAll(filepath.Dir(d.chemin))
		}
		logf("appareil %q débranché", c.nom)
	}()
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(delaiSilence))
		ligne, err := lireLigne(lu, tailleLigneMax)
		if err != nil {
			return
		}
		if len(ligne) > 0 {
			c.trame(ligne)
		}
	}
}

// envoyerA : une réponse pour ce client seulement (bloque si sa file est pleine).
func (c *Client) envoyerA(v any) {
	t, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.reponses <- envoi{t, "réponses"}:
	case <-c.fini:
	}
}

type trame struct {
	Svc     string          `json:"svc"`
	Cmd     json.RawMessage `json:"cmd"`
	Ping    json.RawMessage `json:"ping"`
	Pong    json.RawMessage `json:"pong"`
	Rejouer string          `json:"rejouer"`
	Marques json.RawMessage `json:"marques"`
	Mcp     *struct {
		ID     int64  `json:"id"`
		Action string `json:"action"`
		Envoi  *bool  `json:"envoi"`
	} `json:"mcp"`
	Apercu *struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	} `json:"apercu"`
	Get *struct {
		ID     int64  `json:"id"`
		Chemin string `json:"chemin"`
		Sha256 string `json:"sha256"`
	} `json:"get"`
	Put *struct {
		ID      int64  `json:"id"`
		Nom     string `json:"nom"`
		Donnees string `json:"donnees"`
		Fin     bool   `json:"fin"`
	} `json:"put"`
}

func (c *Client) trame(ligne []byte) {
	var t trame
	if err := json.Unmarshal(ligne, &t); err != nil {
		return
	}
	switch {
	case t.Cmd != nil:
		c.commande(t.Svc, t.Cmd)
	case t.Ping != nil:
		c.envoyerA(map[string]json.RawMessage{"pong": t.Ping})
	case t.Pong != nil:
		// signe de vie : le délai de lecture est déjà repoussé
	case t.Marques != nil:
		c.srv.recevoirMarques(c, t.Marques)
	case t.Mcp != nil:
		m := *t.Mcp
		go c.trameMCP(m.ID, m.Action, m.Envoi)
	case t.Apercu != nil:
		a := *t.Apercu
		go c.trameApercu(a.ID, a.URL)
	case t.Rejouer != "":
		c.srv.rejouer(c, t.Rejouer)
	case t.Get != nil:
		// En file, sans bloquer la lecture : une commande (`send`…) ne
		// doit jamais attendre derrière des fichiers.
		c.demanderGet(t.Get.ID, t.Get.Chemin, t.Get.Sha256)
	case t.Put != nil:
		c.deposer(t.Put.ID, t.Put.Nom, t.Put.Donnees, t.Put.Fin)
	}
}

func (c *Client) erreurSvc(svc, texte string) {
	ev, _ := json.Marshal(map[string]string{"ev": "error", "where": "serveur", "text": texte})
	c.envoyerA(json.RawMessage(emballer(svc, ev)))
}

func (c *Client) commande(svc string, brute json.RawMessage) {
	p := c.srv.ponts[svc]
	if p == nil {
		c.erreurSvc(svc, "service non offert par ce serveur : "+svc)
		return
	}
	var tete struct {
		Cmd    string          `json:"cmd"`
		Path   string          `json:"path"`
		Conv   string          `json:"conv"`
		Before json.RawMessage `json:"before"`
		Count  int             `json:"count"`
		Media  string          `json:"media_id"`
	}
	if json.Unmarshal(brute, &tete) != nil {
		return
	}
	switch tete.Cmd {
	case "messages":
		if tete.Conv == "" {
			break
		}
		ancien := len(tete.Before) > 0 && string(tete.Before) != `""` && string(tete.Before) != "null"
		if !ancien {
			// Réponse immédiate depuis le cache, à ce client seul, avant
			// celle du pont (même file : l'ordre est garanti).
			if t := p.cache.reponse(tete.Conv, tete.Count); t != nil {
				select {
				case c.evs <- emballer(svc, t):
				default:
				}
			}
		}
		c.srv.mu.Lock()
		if ancien {
			p.noterDemande(cleAncien(tete.Conv))
		} else {
			p.noterDemande(tete.Conv)
		}
		c.srv.mu.Unlock()
	case "download":
		// La réponse `media` reviendra à cet appareil seulement (envoi.go).
		c.srv.mu.Lock()
		p.noterTelechargement(tete.Media, c.hachage)
		c.srv.mu.Unlock()
	case "quit":
		// Le pont appartient au serveur : un client qui part ne l'arrête pas.
		return
	case "send_media":
		// Le pont lira ce fichier : seulement ce qu'un client a téléversé.
		if !sousRacine(c.srv.depots, tete.Path) {
			c.erreurSvc(svc, "send_media : téléversez d'abord le fichier (put)")
			return
		}
	}
	if err := p.envoyer(compacter(brute)); err != nil {
		c.erreurSvc(svc, err.Error())
	}
}

// compacter : une seule ligne, quoi qu'ait envoyé le client.
func compacter(b []byte) []byte {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	out, _ := json.Marshal(v)
	return out
}

// ---- Fichiers ----

var extensionsMedias = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".heic": true, ".bmp": true,
	".mp4": true, ".mov": true, ".webm": true, ".3gp": true, ".mkv": true,
	".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".amr": true, ".wav": true,
	".pdf": true, ".vcf": true,
}

// cheminPermis : le chemin réel (liens résolus) d'un fichier qu'on peut
// servir, ou une erreur. Seulement sous les racines des médias et des
// données, jamais le dossier du serveur, et dans les données seulement des
// médias.
func (s *Serveur) cheminPermis(demande string) (string, error) {
	refus := errors.New("chemin refusé")
	if demande == "" || !filepath.IsAbs(demande) || strings.ContainsRune(demande, 0) {
		return "", refus
	}
	for _, morceau := range strings.Split(demande, "/") {
		if morceau == ".." {
			return "", refus
		}
	}
	reel, err := filepath.EvalSymlinks(filepath.Clean(demande))
	if err != nil {
		return "", errors.New("fichier introuvable")
	}
	if s.interdit != "" && sousRacine(s.interdit, reel) {
		return "", refus
	}
	for _, r := range s.racines {
		if !sousRacine(r.chemin, reel) {
			continue
		}
		if r.mediasSeulement && !extensionsMedias[strings.ToLower(filepath.Ext(reel))] {
			continue
		}
		return reel, nil
	}
	return "", refus
}

// sousRacine : `chemin` (déjà nettoyé, liens résolus si possible) est-il
// strictement sous `racine` ?
func sousRacine(racine, chemin string) bool {
	if racine == "" || chemin == "" || !filepath.IsAbs(chemin) {
		return false
	}
	if r, err := filepath.EvalSymlinks(chemin); err == nil {
		chemin = r
	}
	rel, err := filepath.Rel(racine, filepath.Clean(chemin))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func (c *Client) servirFichier(id int64, demande, shaConnu string) {
	echec := func(texte string) {
		c.envoyerA(map[string]any{"fichier": map[string]any{"id": id, "erreur": texte, "fin": true}})
	}
	reel, err := c.srv.cheminPermis(demande)
	if err != nil {
		echec(err.Error())
		return
	}
	// O_NOFOLLOW : le dernier élément ne peut pas devenir un lien entre-temps.
	f, err := os.OpenFile(reel, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		echec("fichier illisible")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		echec("pas un fichier")
		return
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		echec("fichier illisible")
		return
	}
	somme := hex.EncodeToString(h.Sum(nil))
	if shaConnu != "" && strings.EqualFold(shaConnu, somme) {
		c.envoyerA(map[string]any{"fichier": map[string]any{"id": id, "inchange": true, "taille": st.Size(), "sha256": somme, "fin": true}})
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		echec("fichier illisible")
		return
	}
	sorte := c.srv.origine(demande)
	buf := make([]byte, tailleMorceau)
	premier := true
	for {
		n, err := io.ReadFull(f, buf)
		fin := err != nil
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			echec("lecture interrompue")
			return
		}
		m := map[string]any{"id": id, "donnees": base64.StdEncoding.EncodeToString(buf[:n]), "fin": fin}
		if premier {
			m["taille"], m["sha256"] = st.Size(), somme
			premier = false
		}
		t, _ := json.Marshal(map[string]any{"fichier": m})
		if !c.envoyerFichier(t, sorte) || fin {
			return
		}
		select {
		case <-c.fini:
			return
		default:
		}
	}
}

func (c *Client) deposer(id int64, nom, donnees string, fin bool) {
	echec := func(texte string) {
		if d := c.depots[id]; d != nil {
			_ = d.f.Close()
			_ = os.RemoveAll(filepath.Dir(d.chemin))
			delete(c.depots, id)
		}
		c.envoyerA(map[string]any{"depose": map[string]any{"id": id, "erreur": texte}})
	}
	d := c.depots[id]
	if d == nil {
		if len(c.depots) >= depotsSimultane {
			echec("trop de téléversements en même temps")
			return
		}
		dossier := filepath.Join(c.srv.depots, hex.EncodeToString([]byte(aleatoireBrut(12))))
		if err := os.MkdirAll(dossier, 0o700); err != nil {
			echec("dossier de dépôt impossible")
			return
		}
		chemin := filepath.Join(dossier, nomDeFichier(nom))
		f, err := os.OpenFile(chemin, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			echec("fichier de dépôt impossible")
			return
		}
		d = &depot{f: f, chemin: chemin}
		c.depots[id] = d
	}
	brut, err := base64.StdEncoding.DecodeString(donnees)
	if err != nil {
		echec("base64 illisible")
		return
	}
	d.taille += int64(len(brut))
	if d.taille > tailleDepotMax {
		echec("fichier trop gros")
		return
	}
	if _, err := d.f.Write(brut); err != nil {
		echec("écriture impossible")
		return
	}
	if fin {
		if err := d.f.Close(); err != nil {
			echec("écriture impossible")
			return
		}
		delete(c.depots, id)
		c.envoyerA(map[string]any{"depose": map[string]any{"id": id, "chemin": d.chemin, "taille": d.taille}})
	}
}

func nomDeFichier(nom string) string {
	nom = filepath.Base(strings.ReplaceAll(nom, "\\", "/"))
	nom = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '/' {
			return '_'
		}
		return r
	}, nom)
	nom = strings.TrimLeft(nom, ".")
	if nom == "" {
		nom = "fichier"
	}
	if len(nom) > 120 {
		nom = nom[len(nom)-120:]
	}
	return nom
}

// ---- Outils ----

func lireLigne(lu *bufio.Reader, max int) ([]byte, error) {
	var ligne []byte
	for {
		morceau, estPrefixe, err := lu.ReadLine()
		if err != nil {
			return nil, err
		}
		ligne = append(ligne, morceau...)
		if len(ligne) > max {
			return nil, errors.New("trame trop longue")
		}
		if !estPrefixe {
			return ligne, nil
		}
	}
}

func ecrireJSON(w io.Writer, v any) error {
	t, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(t, '\n'))
	return err
}

func aleatoireBrut(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return string(b)
}

func aleatoire(n int) string { return base64.StdEncoding.EncodeToString([]byte(aleatoireBrut(n))) }

func hoteDe(a net.Addr) string {
	if h, _, err := net.SplitHostPort(a.String()); err == nil {
		return h
	}
	return a.String()
}

// ---- Limiteur d'essais (anti force brute) ----

// Limiteur : au plus `max` essais par clé (adresse) sur une fenêtre glissante.
type Limiteur struct {
	mu      sync.Mutex
	max     int
	fenetre time.Duration
	essais  map[string][]time.Time
}

func nouveauLimiteur(max int, fenetre time.Duration) *Limiteur {
	return &Limiteur{max: max, fenetre: fenetre, essais: map[string][]time.Time{}}
}

func (l *Limiteur) permis(cle string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	garde := l.essais[cle][:0]
	for _, t := range l.essais[cle] {
		if now.Sub(t) < l.fenetre {
			garde = append(garde, t)
		}
	}
	if len(garde) >= l.max {
		l.essais[cle] = garde
		return false
	}
	l.essais[cle] = append(garde, now)
	if len(l.essais) > 10000 { // ménage
		for k, v := range l.essais {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > l.fenetre {
				delete(l.essais, k)
			}
		}
	}
	return true
}

// depasse : la clé a-t-elle déjà épuisé ses essais (sans en compter un) ?
func (l *Limiteur) depasse(cle string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, t := range l.essais[cle] {
		if time.Since(t) < l.fenetre {
			n++
		}
	}
	return n >= l.max
}

func (l *Limiteur) oublier(cle string) {
	l.mu.Lock()
	delete(l.essais, cle)
	l.mu.Unlock()
}
