// Les ponts : un processus enfant par service, relancé s'il meurt.
//
// Chaque ligne de sa sortie standard est un événement JSON, emballé en
// {"svc":...,"ev":...} et envoyé à tous les clients branchés. On garde le
// dernier état connu (hello, state, account, avatar_moi, qr, pair_code,
// history_pin) pour le rejouer aux clients qui arrivent, puis les listes de
// conversations et les derniers messages (cache.go, PROTOCOLE.md §5.8).
//
// Les services google, messenger et instagram sont les services de base :
// sans leur pont, le serveur le signale. Les services whatsapp, signal et x
// sont facultatifs : sans leur binaire, ils sont simplement omis.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type Pont struct {
	svc string
	bin string
	env []string
	srv *Serveur

	muEntree sync.Mutex
	entree   io.WriteCloser // nil quand le pont est arrêté

	// Sous srv.mu : le dernier événement de chaque sorte, déjà emballé.
	dernier map[string][]byte

	// Le cache des listes et des messages (cache.go), et le préchargement.
	cache         *Cache
	pret          bool               // sous srv.mu : dernier état « ready »
	demandes      map[string]demande // sous srv.mu : réponses `messages` attendues par un client
	precharge     string             // sous srv.mu : la conversation en préchargement
	prechargeFini chan struct{}
	aPrecharger   chan struct{}
}

// demande : des clients attendent la réponse `messages` d'une conversation.
type demande struct {
	n      int
	depuis time.Time
}

// Ordre du rejeu : d'abord qui l'on est, ensuite où l'on en est.
// « pair_code » : le code d'association à huit caractères de WhatsApp.
var sortesRejouees = []string{"hello", "state", "account", "avatar_moi", "qr", "pair_code", "history_pin"}

// Tous les services connus, dans l'ordre où on les présente aux clients.
var ordreServices = []string{"google", "messenger", "instagram", "whatsapp", "signal", "x"}

// Les services facultatifs : binaire, variable d'environnement qui peut le
// désigner, et dossier de données (sous <données>) que le pont utilise.
var pontsFacultatifs = map[string]struct{ bin, variable, dossier string }{
	"whatsapp": {"jasette-wa", "JASETTE_WA", "whatsapp"},
	"signal":   {"jasette-signal", "JASETTE_SIGNAL", "signal"},
	"x":        {"jasette-x", "JASETTE_X", "x"},
}

// errAbsent : le binaire d'un service facultatif n'est pas là ; ce n'est pas
// une erreur, le service est omis.
var errAbsent = errors.New("binaire absent")

// trouverPont cherche le programme d'un service : variable d'environnement,
// dossier --bin, à côté de jasette-serveur, puis le PATH.
func trouverPont(nom, variable, binDir string) string {
	if p := os.Getenv(variable); p != "" {
		return p
	}
	var essais []string
	if binDir != "" {
		essais = append(essais, filepath.Join(binDir, nom))
	}
	if exe, err := os.Executable(); err == nil {
		essais = append(essais, filepath.Join(filepath.Dir(exe), nom))
	}
	for _, e := range essais {
		if st, err := os.Stat(e); err == nil && st.Mode().IsRegular() {
			return e
		}
	}
	if p, err := exec.LookPath(nom); err == nil {
		return p
	}
	return ""
}

// nouveauPont prépare le pont d'un service, comme src/pont.rs le ferait.
func nouveauPont(srv *Serveur, svc, binDir string) (*Pont, error) {
	p := &Pont{
		svc: svc, srv: srv, dernier: map[string][]byte{}, demandes: map[string]demande{},
		prechargeFini: make(chan struct{}, 1), aPrecharger: make(chan struct{}, 1),
	}
	switch svc {
	case "google":
		p.bin = trouverPont("jasette-pont", "JASETTE_PONT", binDir)
	case "messenger":
		p.bin = trouverPont("jasette-meta", "JASETTE_META", binDir)
		p.env = append(p.env, "JASETTE_META_SERVICE=messenger")
	case "instagram":
		// Instagram a son propre pont s'il est là, sinon le pont Meta.
		if b := trouverPont("jasette-ig", "JASETTE_IG", binDir); b != "" {
			p.bin = b
		} else {
			p.bin = trouverPont("jasette-meta", "JASETTE_META", binDir)
		}
		p.env = append(p.env, "JASETTE_META_SERVICE=instagram")
	case "whatsapp", "signal", "x":
		f := pontsFacultatifs[svc]
		p.bin = trouverPont(f.bin, f.variable, binDir)
		if p.bin == "" {
			return nil, errAbsent
		}
		// Le pont crée lui-même <données>/<dossier> en 0700 ; on le prépare
		// aussi ici, pour que les droits soient bons dès le départ.
		d := filepath.Join(dataDir(), f.dossier)
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("dossier de données de %s : %w", svc, err)
		}
		_ = os.Chmod(d, 0o700)
	default:
		return nil, errors.New("service inconnu : " + svc)
	}
	if p.bin == "" {
		return nil, errors.New("pont introuvable pour " + svc)
	}
	p.cache = chargerCache(svc)
	return p, nil
}

// boucle lance le pont et le relance quand il meurt, avec une attente qui
// double à chaque échec rapproché (1 s → 60 s).
func (p *Pont) boucle() {
	attente := time.Second
	for {
		debut := time.Now()
		err := p.executer()
		if err != nil {
			logf("[%s] pont arrêté : %v", p.svc, err)
		} else {
			logf("[%s] pont arrêté", p.svc)
		}
		p.signalerArret()
		if time.Since(debut) > time.Minute {
			attente = time.Second
		}
		logf("[%s] relance dans %s", p.svc, attente)
		time.Sleep(attente)
		attente = min(attente*2, time.Minute)
	}
}

func (p *Pont) executer() error {
	cmd := exec.Command(p.bin)
	cmd.Env = append(os.Environ(), p.env...)
	entree, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	sortie, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	erreurs, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	logf("[%s] pont lancé (%s, pid %d)", p.svc, filepath.Base(p.bin), cmd.Process.Pid)
	p.muEntree.Lock()
	p.entree = entree
	p.muEntree.Unlock()

	// Les journaux du pont, préfixés du service.
	go func() {
		lu := bufio.NewScanner(erreurs)
		lu.Buffer(make([]byte, 64<<10), 1<<20)
		for lu.Scan() {
			logf("[%s] %s", p.svc, lu.Text())
		}
	}()

	lu := bufio.NewReader(sortie)
	for {
		ligne, err := lu.ReadBytes('\n')
		if len(ligne) > 0 {
			p.recu(ligne)
		}
		if err != nil {
			break
		}
	}
	p.muEntree.Lock()
	p.entree = nil
	p.muEntree.Unlock()
	_ = entree.Close()
	return cmd.Wait()
}

// recu traite un événement du pont : mise à jour de l'état gardé, puis envoi
// à tous les clients.
func (p *Pont) recu(ligne []byte) {
	ligne = trimEspaces(ligne)
	if len(ligne) == 0 {
		return
	}
	var tete struct {
		Ev     string `json:"ev"`
		State  string `json:"state"`
		Needed bool   `json:"needed"`
		Folder string `json:"folder"`
		Conv   string `json:"conv"`
		Older  bool   `json:"older"`
		Where  string `json:"where"`
	}
	if err := json.Unmarshal(ligne, &tete); err != nil {
		logf("[%s] ligne illisible du pont : %v", p.svc, err)
		return
	}
	trame := emballer(p.svc, ligne)
	srv := p.srv
	srv.mu.Lock()
	defer srv.mu.Unlock()
	switch tete.Ev {
	case "hello", "account", "avatar_moi", "qr":
		p.dernier[tete.Ev] = trame
	case "pair_code":
		// WhatsApp : avec un code d'association, le pont cesse de montrer
		// des QR ; l'ancien QR ne doit plus être rejoué.
		p.dernier["pair_code"] = trame
		delete(p.dernier, "qr")
	case "state":
		p.dernier["state"] = trame
		if tete.State == "ready" || tete.State == "unpaired" {
			// Associé, ou association abandonnée : plus de code à montrer.
			delete(p.dernier, "qr")
			delete(p.dernier, "pair_code")
		}
		avant := p.pret
		p.pret = tete.State == "ready"
		if p.pret && !avant {
			p.declencher()
		}
		if tete.State == "unpaired" {
			// Plus de compte : ses listes et ses messages partent.
			p.cache.vider()
		}
	case "paired":
		delete(p.dernier, "qr")
		delete(p.dernier, "pair_code")
	case "history_pin":
		if tete.Needed {
			p.dernier["history_pin"] = trame
		} else {
			delete(p.dernier, "history_pin")
		}
	case "message":
		// Archives de Jasette : un message reçu ramène la conversation dans
		// l'Inbox (les marques partent avant le message).
		srv.desarchiverSiRecu(p.svc, ligne)
		p.cache.message(ligne)
	case "conversations":
		archive := tete.Folder == "archive"
		p.cache.liste(ligne, archive)
		if !archive && p.pret {
			p.declencher()
		}
	case "conversation":
		p.cache.majConv(ligne)
	case "deleted":
		if tete.Conv != "" {
			p.cache.oublier(tete.Conv)
		}
	case "messages":
		p.cache.page(ligne)
		if tete.Older {
			delete(p.demandes, cleAncien(tete.Conv))
		} else if !p.pourUnClient(tete.Conv) {
			// La réponse au préchargement ne va qu'au cache.
			return
		}
	case "error":
		// L'échec d'un préchargement non plus (la commande n'est pas d'un client).
		if tete.Where == "messages" && p.precharge != "" && !p.demandesEnCours() {
			logf("[%s] préchargement : erreur du pont", p.svc)
			p.finPrecharge()
			return
		}
	}
	srv.diffuserVerrouille(trame)
}

// signalerArret : le pont est mort ; les clients voient « reconnecting »
// jusqu'à ce que le nouveau dise où il en est.
func (p *Pont) signalerArret() {
	trame := emballer(p.svc, []byte(`{"ev":"state","state":"reconnecting"}`))
	p.srv.mu.Lock()
	defer p.srv.mu.Unlock()
	delete(p.dernier, "qr")
	delete(p.dernier, "pair_code")
	delete(p.dernier, "history_pin")
	p.dernier["state"] = trame
	p.pret = false
	p.demandes = map[string]demande{}
	if p.precharge != "" {
		p.finPrecharge()
	}
	p.cache.nouvelleSession()
	p.srv.diffuserVerrouille(trame)
}

// rejeu : à appeler sous srv.mu. L'état, puis les listes gardées
// (boîte de réception, archives).
func (p *Pont) rejeu() [][]byte {
	var r [][]byte
	for _, s := range sortesRejouees {
		if t, ok := p.dernier[s]; ok {
			r = append(r, t)
		}
	}
	for _, l := range p.cache.listesRejeu() {
		r = append(r, emballer(p.svc, l))
	}
	return r
}

var errPontArrete = errors.New("le pont redémarre")

func (p *Pont) envoyer(cmd []byte) error {
	p.muEntree.Lock()
	defer p.muEntree.Unlock()
	if p.entree == nil {
		return errPontArrete
	}
	_, err := p.entree.Write(append(append([]byte{}, cmd...), '\n'))
	return err
}

func emballer(svc string, ev []byte) []byte {
	s, _ := json.Marshal(svc)
	t := make([]byte, 0, len(ev)+len(s)+16)
	t = append(t, `{"svc":`...)
	t = append(t, s...)
	t = append(t, `,"ev":`...)
	t = append(t, ev...)
	t = append(t, '}')
	return t
}

func trimEspaces(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
