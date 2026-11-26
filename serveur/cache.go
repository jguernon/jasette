// Le cache du serveur : les dernières listes de conversations et les
// derniers messages de chaque conversation, par service (PROTOCOLE.md, §5.8).
//
//   - Les listes `conversations` (boîte de réception et archives) sont
//     rejouées à chaque client qui se branche, après l'état du service.
//   - Les messages : au plus cacheConvsMax conversations × cacheMsgsMax
//     messages, tenus à jour par les événements des ponts (`messages`,
//     `message` — réactions, modifications, retraits et accusés compris —,
//     `deleted`). Une demande `messages` d'un client (sans `before`) reçoit
//     aussitôt la copie du cache, marquée `"cache":true`, puis la réponse
//     du pont, comme avant.
//   - Le préchargement (prechargeur) demande lui-même les messages des
//     conversations récentes et marquées, une à la fois. Ses réponses ne
//     vont qu'au cache, jamais aux clients ; aucun pont ne marque une
//     conversation comme lue sur une commande `messages` (seulement sur
//     `read`, que le serveur n'envoie jamais de lui-même).
//
// Sur le disque : <données>/serveur/cache-<service>.json (0600, écrit par
// renommage, au plus toutes les 5 secondes). Jamais servi par `get`.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	cacheConvsMax    = 200 // conversations gardées par service
	cacheMsgsMax     = 60  // messages gardés par conversation
	prechargeNombre  = 30  // les conversations les plus récentes
	prechargeCompte  = 40  // messages demandés par conversation
	prechargePause   = 1500 * time.Millisecond
	prechargeDelai   = 45 * time.Second // attente maximale d'une réponse
	prechargeAttente = 3 * time.Second  // avant une passe : le pont s'installe
)

// Discord interdit les « self-bots » : son pont précharge moins, et plus
// lentement, comme quelqu'un qui ouvre quelques conversations à la main.
var prechargeParService = map[string]struct {
	nombre, compte, total int
	pause                 time.Duration
}{
	"discord": {nombre: 8, compte: 30, total: 15, pause: 4 * time.Second},
}

// reglagesPrecharge : nombre de conversations récentes, messages par
// conversation, total au plus (marquées comprises, 0 = sans limite) et pause.
func reglagesPrecharge(svc string) (nombre, compte, total int, pause time.Duration) {
	if r, ok := prechargeParService[svc]; ok {
		return r.nombre, r.compte, r.total, r.pause
	}
	return prechargeNombre, prechargeCompte, 0, prechargePause
}

const (
	demandeDuree  = 90 * time.Second // une demande d'un client sans réponse est oubliée
	cacheEcriture = 5 * time.Second
)

// Un message gardé : la ligne du pont telle quelle, et de quoi la ranger.
type msgCache struct {
	id   string
	tmp  string
	time int64
	brut json.RawMessage
}

type convCache struct {
	items []msgCache // du plus vieux au plus récent
	more  bool
	maj   int64 // ms : dernière mise à jour (pour l'éviction)
	// Une page a été reçue du pont depuis son lancement : la copie est à
	// jour (les événements la suivent ensuite). Pas gardé sur le disque.
	session bool
}

type Cache struct {
	mu     sync.Mutex
	svc    string
	chemin string
	// La dernière ligne `conversations` du pont : "inbox" et "archive".
	listes map[string]json.RawMessage
	convs  map[string]*convCache
	sale   bool
	// Quand le pont a envoyé la boîte de réception pour la dernière fois
	// (pas gardé sur le disque) : le MCP la redemande si elle est vieille.
	listeMaj time.Time
}

type fichierCache struct {
	Version int                        `json:"version"`
	Listes  map[string]json.RawMessage `json:"listes"`
	Convs   map[string]convFichier     `json:"convs"`
}

type convFichier struct {
	More  bool              `json:"more"`
	Maj   int64             `json:"maj"`
	Items []json.RawMessage `json:"items"`
}

// teteMsg : ce qu'il faut d'un message pour le ranger.
type teteMsg struct {
	ID    string `json:"id"`
	TmpID string `json:"tmp_id"`
	Conv  string `json:"conv"`
	Time  int64  `json:"time"`
}

func lireMsg(brut json.RawMessage) (msgCache, string, bool) {
	var t teteMsg
	if json.Unmarshal(brut, &t) != nil || t.ID == "" {
		return msgCache{}, "", false
	}
	return msgCache{id: t.ID, tmp: t.TmpID, time: t.Time, brut: brut}, t.Conv, true
}

func cachePath(svc string) string { return filepath.Join(serveurDir(), "cache-"+svc+".json") }

// chargerCache lit le cache d'un service (vide s'il manque ou est illisible)
// et lance son écriture périodique.
func chargerCache(svc string) *Cache {
	c := &Cache{svc: svc, chemin: cachePath(svc), listes: map[string]json.RawMessage{}, convs: map[string]*convCache{}}
	if data, err := os.ReadFile(c.chemin); err == nil {
		var f fichierCache
		if json.Unmarshal(data, &f) == nil && f.Version == 1 {
			for k, v := range f.Listes {
				if k == "inbox" || k == "archive" {
					c.listes[k] = v
				}
			}
			for id, e := range f.Convs {
				cc := &convCache{more: e.More, maj: e.Maj}
				for _, b := range e.Items {
					if m, _, ok := lireMsg(b); ok {
						cc.items = append(cc.items, m)
					}
				}
				trierMsgs(cc.items)
				c.convs[id] = cc
			}
			c.borner()
		} else {
			logf("[%s] cache illisible : ignoré", svc)
		}
	}
	go c.ecrivain()
	return c
}

func (c *Cache) ecrivain() {
	for range time.Tick(cacheEcriture) {
		c.garder()
	}
}

// garder écrit le fichier s'il a changé.
func (c *Cache) garder() {
	c.mu.Lock()
	if !c.sale {
		c.mu.Unlock()
		return
	}
	c.sale = false
	f := fichierCache{Version: 1, Listes: map[string]json.RawMessage{}, Convs: map[string]convFichier{}}
	for k, v := range c.listes {
		f.Listes[k] = v
	}
	for id, e := range c.convs {
		cf := convFichier{More: e.more, Maj: e.maj, Items: make([]json.RawMessage, 0, len(e.items))}
		for _, m := range e.items {
			cf.Items = append(cf.Items, m.brut)
		}
		f.Convs[id] = cf
	}
	data, err := json.Marshal(f)
	c.mu.Unlock()
	if err != nil {
		return
	}
	if err := ecrireAtomique(c.chemin, data); err != nil {
		logf("[%s] cache : écriture impossible : %v", c.svc, err)
	}
}

func trierMsgs(l []msgCache) {
	sort.SliceStable(l, func(i, j int) bool { return l[i].time < l[j].time })
}

// borner : au plus cacheConvsMax conversations (les moins récemment mises à
// jour partent). À appeler sous c.mu.
func (c *Cache) borner() {
	if len(c.convs) <= cacheConvsMax {
		return
	}
	type paire struct {
		id  string
		maj int64
	}
	var l []paire
	for id, e := range c.convs {
		l = append(l, paire{id, e.maj})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].maj > l[j].maj })
	for _, p := range l[cacheConvsMax:] {
		delete(c.convs, p.id)
	}
}

// tronquer : les cacheMsgsMax plus récents ; vrai s'il en manque maintenant.
func tronquer(l []msgCache) ([]msgCache, bool) {
	if len(l) <= cacheMsgsMax {
		return l, false
	}
	return append([]msgCache(nil), l[len(l)-cacheMsgsMax:]...), true
}

// ---- Les événements des ponts (sous srv.mu, puis c.mu) ----

// liste : une ligne `conversations` du pont (boîte de réception ou archives).
func (c *Cache) liste(ligne []byte, archive bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cle := "inbox"
	if archive {
		cle = "archive"
	}
	c.listes[cle] = append(json.RawMessage(nil), ligne...)
	if !archive {
		c.listeMaj = time.Now()
	}
	c.sale = true
}

// majConv : un événement `conversation` (une conversation nouvelle ou
// changée) met à jour la liste gardée, pour le rejeu.
func (c *Cache) majConv(ligne []byte) {
	var ev struct {
		Item json.RawMessage `json:"item"`
	}
	if json.Unmarshal(ligne, &ev) != nil || len(ev.Item) == 0 {
		return
	}
	var t struct {
		ID       string `json:"id"`
		Time     int64  `json:"time"`
		Archived bool   `json:"archived"`
	}
	if json.Unmarshal(ev.Item, &t) != nil || t.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cle := range []string{"inbox", "archive"} {
		dedans := (cle == "archive") == t.Archived
		c.remplacerDansListe(cle, t.ID, ev.Item, t.Time, dedans)
	}
}

// remplacerDansListe retire `id` de la liste gardée `cle` et, si `ajouter`,
// l'y remet à sa place (du plus récent au plus vieux). Sous c.mu.
func (c *Cache) remplacerDansListe(cle, id string, item json.RawMessage, temps int64, ajouter bool) {
	brute, ok := c.listes[cle]
	if !ok {
		return // pas de liste : le pont l'enverra entière
	}
	var ev map[string]json.RawMessage
	if json.Unmarshal(brute, &ev) != nil {
		return
	}
	var items []json.RawMessage
	_ = json.Unmarshal(ev["items"], &items)
	garde := make([]json.RawMessage, 0, len(items)+1)
	place := -1
	for _, it := range items {
		var t struct {
			ID   string `json:"id"`
			Time int64  `json:"time"`
		}
		_ = json.Unmarshal(it, &t)
		if t.ID == id {
			continue
		}
		if ajouter && place < 0 && t.Time < temps {
			place = len(garde)
			garde = append(garde, item)
		}
		garde = append(garde, it)
	}
	if ajouter && place < 0 {
		garde = append(garde, item)
	}
	ev["items"], _ = json.Marshal(garde)
	if b, err := json.Marshal(ev); err == nil {
		c.listes[cle] = b
		c.sale = true
	}
}

// page : une réponse `messages` du pont. La page la plus récente remplace
// ce qu'on avait dans sa plage ; on garde ce qui est plus récent qu'elle
// (arrivé entre-temps) et ce qui la prolonge vers le passé (si elle touche
// la copie). Une page plus ancienne (`older`) est ignorée.
func (c *Cache) page(ligne []byte) {
	var ev struct {
		Conv  string            `json:"conv"`
		Items []json.RawMessage `json:"items"`
		Older bool              `json:"older"`
		More  bool              `json:"more"`
	}
	if json.Unmarshal(ligne, &ev) != nil || ev.Conv == "" {
		return
	}
	var nouveaux []msgCache
	for _, b := range ev.Items {
		if m, _, ok := lireMsg(b); ok {
			nouveaux = append(nouveaux, m)
		}
	}
	trierMsgs(nouveaux)
	c.mu.Lock()
	defer c.mu.Unlock()
	anc := c.convs[ev.Conv]
	if ev.Older {
		// Une page plus ancienne : la copie garde les plus récents, elle n'y
		// gagnerait rien (et pourrait y laisser un trou).
		return
	}
	var l []msgCache
	if anc != nil {
		l = fusionnerPage(anc.items, nouveaux)
	} else {
		l = nouveaux
	}
	l, coupe := tronquer(l)
	if anc == nil {
		anc = &convCache{}
		c.convs[ev.Conv] = anc
	}
	anc.items, anc.more, anc.maj, anc.session = l, ev.More || coupe, time.Now().UnixMilli(), true
	c.sale = true
	c.borner()
}

// fusionnerPage : `page` (triée) fait foi dans sa plage ; de `anc`, on garde
// ce qui est plus récent qu'elle et, si elle touche `anc` (son plus vieux
// message y est), ce qui est plus vieux. Sans doublon d'id.
func fusionnerPage(anc, page []msgCache) []msgCache {
	if len(page) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(page))
	for _, m := range page {
		ids[m.id] = true
	}
	premier, dernier := page[0], page[len(page)-1]
	var avant, apres []msgCache
	touche := false
	for _, m := range anc {
		if m.id == premier.id {
			touche = true
		}
	}
	for _, m := range anc {
		if ids[m.id] {
			continue
		}
		switch {
		case m.time > dernier.time:
			apres = append(apres, m)
		case touche && m.time < premier.time:
			avant = append(avant, m)
		}
	}
	r := make([]msgCache, 0, len(avant)+len(page)+len(apres))
	r = append(append(append(r, avant...), page...), apres...)
	trierMsgs(r)
	return r
}

// message : un événement `message` (nouveau, réaction, modification,
// retrait, accusé) met à jour une conversation déjà gardée. Une
// conversation absente du cache n'y entre pas par un seul message : elle
// serait incomplète.
func (c *Cache) message(ligne []byte) {
	var ev struct {
		Item json.RawMessage `json:"item"`
	}
	if json.Unmarshal(ligne, &ev) != nil {
		return
	}
	m, conv, ok := lireMsg(ev.Item)
	if !ok || conv == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.convs[conv]
	if e == nil {
		return
	}
	trouve := false
	for i, x := range e.items {
		if x.id == m.id || (m.tmp != "" && x.tmp == m.tmp) {
			e.items[i] = m
			trouve = true
			break
		}
	}
	if !trouve {
		e.items = append(e.items, m)
	}
	trierMsgs(e.items)
	var coupe bool
	e.items, coupe = tronquer(e.items)
	e.more = e.more || coupe
	e.maj = time.Now().UnixMilli()
	c.sale = true
}

// oublier : une conversation supprimée (`deleted`).
func (c *Cache) oublier(conv string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.convs, conv)
	for _, cle := range []string{"inbox", "archive"} {
		c.remplacerDansListe(cle, conv, nil, 0, false)
	}
	c.sale = true
}

// vider : le compte est dissocié (`unpaired`) ; rien de lui ne reste.
func (c *Cache) vider() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.convs) == 0 && len(c.listes) == 0 {
		return
	}
	c.convs = map[string]*convCache{}
	c.listes = map[string]json.RawMessage{}
	c.sale = true
}

// nouvelleSession : le pont a été relancé ; les copies devront être
// redemandées avant d'être données pour à jour.
func (c *Cache) nouvelleSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.convs {
		e.session = false
	}
}

// ---- Pour les clients ----

// listesRejeu : les lignes `conversations` gardées (boîte, puis archives).
func (c *Cache) listesRejeu() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	var r [][]byte
	for _, cle := range []string{"inbox", "archive"} {
		if l, ok := c.listes[cle]; ok {
			r = append(r, l)
		}
	}
	return r
}

// reponse : la ligne `messages` à donner tout de suite à un client, marquée
// "cache":true, ou nil. Seulement une copie à jour (une page reçue du pont
// depuis son lancement, suivie ensuite par les événements).
func (c *Cache) reponse(conv string, count int) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.convs[conv]
	if e == nil || !e.session {
		return nil
	}
	items := e.items
	more := e.more
	if count > 0 && len(items) > count {
		items = items[len(items)-count:]
		more = true
	}
	bruts := make([]json.RawMessage, 0, len(items))
	for _, m := range items {
		bruts = append(bruts, m.brut)
	}
	t, err := json.Marshal(map[string]any{
		"ev": "messages", "conv": conv, "items": bruts, "older": false, "more": more, "cache": true,
	})
	if err != nil {
		return nil
	}
	return t
}

// ---- Le préchargement ----

type candidat struct {
	conv string
	time int64
}

// candidats : les conversations à précharger, dans l'ordre : les
// prechargeNombre plus récentes de la boîte de réception, puis les marquées
// (`marquees`, ids sans préfixe) présentes dans les listes. Seulement celles
// dont la copie manque ou n'est plus à jour.
func (c *Cache) candidats(marquees []string, nombre, total int) []candidat {
	c.mu.Lock()
	defer c.mu.Unlock()
	connues := map[string]int64{}
	var boite []candidat
	for _, cle := range []string{"inbox", "archive"} {
		var ev struct {
			Items []struct {
				ID   string `json:"id"`
				Time int64  `json:"time"`
			} `json:"items"`
		}
		if json.Unmarshal(c.listes[cle], &ev) != nil {
			continue
		}
		for _, it := range ev.Items {
			if it.ID == "" {
				continue
			}
			connues[it.ID] = it.Time
			if cle == "inbox" {
				boite = append(boite, candidat{it.ID, it.Time})
			}
		}
	}
	sort.SliceStable(boite, func(i, j int) bool { return boite[i].time > boite[j].time })
	if len(boite) > nombre {
		boite = boite[:nombre]
	}
	for _, id := range marquees {
		if t, ok := connues[id]; ok {
			boite = append(boite, candidat{id, t})
		}
	}
	var r []candidat
	vus := map[string]bool{}
	for _, k := range boite {
		if vus[k.conv] {
			continue
		}
		vus[k.conv] = true
		if !c.aJourVerrouille(k.conv, k.time) {
			r = append(r, k)
		}
	}
	if total > 0 && len(r) > total {
		r = r[:total]
	}
	return r
}

func (c *Cache) aJour(conv string, temps int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.aJourVerrouille(conv, temps)
}

// aJourVerrouille : copie reçue depuis le lancement du pont et qui a le
// dernier message annoncé par la liste.
func (c *Cache) aJourVerrouille(conv string, temps int64) bool {
	e := c.convs[conv]
	if e == nil || !e.session {
		return false
	}
	if len(e.items) == 0 {
		return true
	}
	return e.items[len(e.items)-1].time >= temps
}

// idsDuService : les ids d'une liste de marques qui sont à ce service, sans
// leur préfixe (Google : ceux qui n'ont aucun préfixe connu).
func idsDuService(liste []string, svc string) []string {
	prefixe := prefixeService[svc]
	var r []string
	for _, id := range liste {
		if prefixe != "" {
			if strings.HasPrefix(id, prefixe) {
				r = append(r, strings.TrimPrefix(id, prefixe))
			}
			continue
		}
		autre := false
		for s, p := range prefixeService {
			if s != svc && p != "" && strings.HasPrefix(id, p) {
				autre = true
				break
			}
		}
		if !autre {
			r = append(r, id)
		}
	}
	return r
}

// declencher : une passe de préchargement est à faire (sans bloquer).
func (p *Pont) declencher() {
	select {
	case p.aPrecharger <- struct{}{}:
	default:
	}
}

// prechargeur : le fil du préchargement d'un pont. Une passe à la fois ;
// une conversation à la fois, avec une pause entre deux.
func (p *Pont) prechargeur() {
	for range p.aPrecharger {
		time.Sleep(prechargeAttente)
		select { // les déclenchements arrivés pendant l'attente : une seule passe
		case <-p.aPrecharger:
		default:
		}
		p.passePrecharge()
	}
}

func (p *Pont) passePrecharge() {
	srv := p.srv
	srv.mu.Lock()
	pret := p.pret
	var marquees []string
	for _, l := range [][]string{srv.marques.Head, srv.marques.Favoris, srv.marques.Epingles} {
		marquees = append(marquees, idsDuService(l, p.svc)...)
	}
	srv.mu.Unlock()
	if !pret {
		return
	}
	nombre, compte, total, pause := reglagesPrecharge(p.svc)
	liste := p.cache.candidats(marquees, nombre, total)
	if len(liste) == 0 {
		return
	}
	logf("[%s] préchargement de %d conversation(s)", p.svc, len(liste))
	faits := 0
	for _, k := range liste {
		// Un client a pu l'ouvrir entre-temps.
		if p.cache.aJour(k.conv, k.time) {
			continue
		}
		srv.mu.Lock()
		if !p.pret {
			srv.mu.Unlock()
			logf("[%s] préchargement interrompu (service pas prêt)", p.svc)
			return
		}
		select { // une fin restée d'une demande précédente
		case <-p.prechargeFini:
		default:
		}
		p.precharge = k.conv
		srv.mu.Unlock()
		cmd, _ := json.Marshal(map[string]any{"cmd": "messages", "conv": k.conv, "count": compte, "auto": true})
		if err := p.envoyer(cmd); err != nil {
			srv.mu.Lock()
			p.precharge = ""
			srv.mu.Unlock()
			return
		}
		select {
		case <-p.prechargeFini:
			faits++
		case <-time.After(prechargeDelai):
			srv.mu.Lock()
			if p.precharge == k.conv {
				p.precharge = ""
			}
			srv.mu.Unlock()
			logf("[%s] préchargement : pas de réponse", p.svc)
		}
		time.Sleep(pause)
	}
	logf("[%s] préchargement fini (%d/%d)", p.svc, faits, len(liste))
}

// finPrecharge : la réponse (ou l'erreur) du préchargement en cours est
// arrivée. Sous srv.mu.
func (p *Pont) finPrecharge() {
	p.precharge = ""
	select {
	case p.prechargeFini <- struct{}{}:
	default:
	}
}

// noterDemande : un client attend la réponse `messages` de `conv`. Sous srv.mu.
func (p *Pont) noterDemande(conv string) {
	d := p.demandes[conv]
	if time.Since(d.depuis) > demandeDuree {
		d.n = 0
	}
	d.n++
	d.depuis = time.Now()
	p.demandes[conv] = d
}

// cleAncien : la clé, dans p.demandes, d'une page plus ancienne demandée
// par un client (sa réponse va toujours aux clients ; on la note pour ne pas
// prendre son éventuelle erreur pour celle du préchargement).
func cleAncien(conv string) string { return conv + "\x00ancien" }

// pourUnClient : la réponse `messages` (pas `older`) de `conv` est-elle
// attendue par un client ? Sinon, et si c'est celle du préchargement, elle
// ne va qu'au cache. Sous srv.mu.
func (p *Pont) pourUnClient(conv string) bool {
	if p.prendreDemande(conv) {
		return true
	}
	if p.precharge != "" && p.precharge == conv {
		p.finPrecharge()
		return false
	}
	return true
}

// prendreDemande : un client attend-il cette réponse (clé de p.demandes) ?
// Si oui, elle lui revient (une de moins à attendre). Sous srv.mu.
func (p *Pont) prendreDemande(cle string) bool {
	d, ok := p.demandes[cle]
	if !ok {
		return false
	}
	if d.n > 0 && time.Since(d.depuis) <= demandeDuree {
		d.n--
		if d.n == 0 {
			delete(p.demandes, cle)
		} else {
			p.demandes[cle] = d
		}
		return true
	}
	delete(p.demandes, cle)
	return false
}

// demandesEnCours : un client attend-il une réponse `messages` ? Sous srv.mu.
func (p *Pont) demandesEnCours() bool {
	for _, d := range p.demandes {
		if d.n > 0 && time.Since(d.depuis) <= demandeDuree {
			return true
		}
	}
	return false
}
