// Les marques : le Head, les favoris, les épinglées, les archives, les
// privées, les moods, les emojis rapides, les personnes et les contacts perso (fiche de la
// personne, personnes.go), l'ordre des services et les réglages garder_archives et
// cacher_apres_jours de Jasette, partagés entre tous les appareils de l'utilisateur (voir
// PROTOCOLE.md, §5.5).
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
	tailleMoodMax   = 64   // octets au plus par nom de mood
	tailleEmojiMax  = 32   // octets au plus par emoji rapide (un graphème)
)

// Marques : cinq listes ordonnées d'ids de conversations (préfixés « m: »
// pour Messenger, « i: » pour Instagram, « w: », « s: », « x: », « t: »,
// « b: », « d: », « r: », « l: » pour WhatsApp, Signal, X, Telegram, Bluesky, Discord,
// Reddit et LinkedIn,
// sans préfixe pour Google).
// Version 0 : le serveur n'a encore rien reçu.
//
// Archives : l'archivage propre à Jasette, pour les services qui ne savent
// pas archiver (Messenger, Instagram, Signal, X). Un nouveau message reçu
// dans une de ces conversations l'en retire (desarchiverSiRecu), sauf si
// GarderArchives est vrai.
type Marques struct {
	Head     []string `json:"head"`
	Favoris  []string `json:"favoris"`
	Epingles []string `json:"epingles"`
	Archives []string `json:"archives"`
	// Privees : les conversations de la Liste privée (PROTOCOLE.md, §5.5) :
	// cachées partout ailleurs que dans la vue Privées des clients, et
	// exclues du MCP. Le code qui ouvre cette vue reste sur chaque appareil.
	Privees []string `json:"privees"`
	// Moods : le « Mood » choisi pour une conversation (couleurs des bulles et
	// fond du fil), {id: nom}. Le serveur ne connaît pas les noms : il les
	// garde tels quels ; un client ignore un nom qu'il ne connaît pas.
	Moods map[string]string `json:"moods"`
	// Emojis : l'« emoji rapide » d'une conversation, {id: emoji}, envoyé
	// d'un appui par le bouton à droite de la saisie. Absent : aucun.
	Emojis map[string]string `json:"emojis"`
	// GarderArchives : « Garder les conversations archivées hors de la boîte
	// de réception ». Vrai : un message reçu ne désarchive plus. Absent (ancien
	// fichier) : faux.
	GarderArchives bool `json:"garder_archives"`
	// CacherApresJours : « Cacher les conversations plus anciennes de la boîte
	// de réception », en jours (0 : tout afficher). Seuls les clients filtrent
	// (à l'affichage) ; le serveur ne fait que le garder et le partager.
	// Absent (ancien fichier) : 0.
	CacherApresJours int `json:"cacher_apres_jours"`
	// PresenceEnLigne : « Afficher qui est en ligne » (point vert, « Actif il
	// y a 12 min »). Vrai par défaut, et pour un ancien fichier sans ce champ.
	// Le serveur le transmet aux ponts (presence.go).
	PresenceEnLigne bool `json:"presence_en_ligne"`
	// SonsConversation : le jeu de sons des conversations, "bulle" (défaut),
	// "bois", "verre" ou "aucun" (jeuSons). Seuls les clients jouent.
	SonsConversation string `json:"sons_conversation"`
	// NotesContacts : « Afficher les notes de mes contacts » (bulles du Head,
	// PROTOCOLE.md §5.12). Vrai par défaut, et pour un ancien fichier. Le
	// serveur le transmet au pont Instagram (notes.go).
	NotesContacts bool `json:"notes_contacts"`
	// NotesMusique : l'appareil qui publie « ce que j'écoute » ("" : personne).
	NotesMusique string `json:"notes_musique"`
	// NotesMusiqueFormat : ce que dit la note musicale, "artiste" (défaut),
	// "genre" ou "titre" (formatMusique).
	NotesMusiqueFormat string `json:"notes_musique_format"`
	// NotesServices : où « Quoi de neuf ? » publie, ["tout"] ou une liste.
	NotesServices []string `json:"notes_services"`
	// OrdreServices : l'ordre des services choisi dans « Dans la boîte de
	// réception » (leurs clés : "google", "messenger"…), partagé comme
	// l'ordre des favoris. Vide : l'ordre par défaut de chaque client. Le
	// serveur ne connaît pas les noms : un client ignore une clé inconnue et
	// met à la fin un service absent. Absent (ancien fichier) : vide.
	OrdreServices []string `json:"ordre_services"`
	// Personnes : les conversations reliées à la main (fiche de la personne,
	// personnes.go). PersonnesRefus : les paires « Pas la même personne ».
	// Absents (ancien fichier) : vides.
	Personnes      []Personne `json:"personnes"`
	PersonnesRefus [][]string `json:"personnes_refus"`
	// ContactsPerso : le nom et la photo choisis à la main dans la fiche
	// (contacts_perso.go), {id: {nom, photo, google}}. Absent : vide.
	ContactsPerso map[string]ContactPerso `json:"contacts_perso"`
	Version       int64                   `json:"version"`
	// Propre au serveur, jamais envoyé aux clients : depuis quand (ms, horloge
	// du serveur) chaque id est dans Archives. Seul un message reçu après
	// cette heure ramène la conversation dans l'Inbox (une réaction ou une
	// mise à jour d'un vieux message, non).
	ArchivesDepuis map[string]int64 `json:"archives_depuis,omitempty"`
}

func marquesPath() string { return filepath.Join(serveurDir(), "marques.json") }

// listes rend les cinq listes, dans l'ordre (pour les boucles).
func (m *Marques) listes() []*[]string {
	return []*[]string{&m.Head, &m.Favoris, &m.Epingles, &m.Archives, &m.Privees}
}

// normaliser : jamais de liste nulle (elle s'écrirait `null`), ids propres,
// une heure d'archivage pour chaque archive (maintenant, si elle manque) et
// aucune pour un id qui n'y est plus.
func (m *Marques) normaliser() {
	for _, l := range m.listes() {
		*l = nettoyerIds(*l)
	}
	m.Moods = nettoyerMoods(m.Moods)
	m.Emojis = nettoyerEmojis(m.Emojis)
	m.CacherApresJours = bornerJours(m.CacherApresJours)
	m.SonsConversation = jeuSons(m.SonsConversation)
	m.NotesMusique = bornerAppareil(m.NotesMusique)
	m.NotesMusiqueFormat = formatMusique(m.NotesMusiqueFormat)
	m.NotesServices = nettoyerServicesNotes(m.NotesServices)
	m.OrdreServices = nettoyerOrdreServices(m.OrdreServices)
	m.Personnes = nettoyerPersonnes(m.Personnes)
	m.PersonnesRefus = nettoyerRefus(m.PersonnesRefus)
	m.ContactsPerso = nettoyerContactsPerso(m.ContactsPerso)
	depuis := make(map[string]int64, len(m.Archives))
	maintenant := time.Now().UnixMilli()
	for _, id := range m.Archives {
		if t, ok := m.ArchivesDepuis[id]; ok && t > 0 {
			depuis[id] = t
		} else {
			depuis[id] = maintenant
		}
	}
	m.ArchivesDepuis = depuis
}

func (m *Marques) memesListes(n *Marques) bool {
	a, b := m.listes(), n.listes()
	for i := range a {
		if !egales(*a[i], *b[i]) {
			return false
		}
	}
	return egalesMoods(m.Moods, n.Moods) && egalesMoods(m.Emojis, n.Emojis) && m.GarderArchives == n.GarderArchives &&
		m.CacherApresJours == n.CacherApresJours && m.PresenceEnLigne == n.PresenceEnLigne &&
		m.SonsConversation == n.SonsConversation && m.NotesContacts == n.NotesContacts &&
		m.NotesMusique == n.NotesMusique && m.NotesMusiqueFormat == n.NotesMusiqueFormat && egales(m.NotesServices, n.NotesServices) &&
		egales(m.OrdreServices, n.OrdreServices) &&
		egalesPersonnes(m.Personnes, n.Personnes) && egalesRefus(m.PersonnesRefus, n.PersonnesRefus) &&
		egalesContactsPerso(m.ContactsPerso, n.ContactsPerso)
}

// cacherApresJoursMax : au-delà de dix ans, autant tout afficher.
const cacherApresJoursMax = 3650

// bornerJours : cacher_apres_jours entre 0 (tout afficher) et
// cacherApresJoursMax ; négatif ou démesuré : 0.
func bornerJours(j int) int {
	if j < 0 || j > cacherApresJoursMax {
		return 0
	}
	return j
}

// nettoyerMoods : jamais nul, sans id ni nom vide ou démesuré, borné.
func nettoyerMoods(m map[string]string) map[string]string {
	return nettoyerObjet(m, tailleMoodMax)
}

// nettoyerEmojis : de même, un emoji de 32 octets au plus.
func nettoyerEmojis(m map[string]string) map[string]string {
	return nettoyerObjet(m, tailleEmojiMax)
}

// nettoyerObjet : {id: valeur}, jamais nul, sans id ni valeur vide ou
// démesurée (valeur de `max` octets au plus), borné.
func nettoyerObjet(m map[string]string, max int) map[string]string {
	r := make(map[string]string, len(m))
	for id, nom := range m {
		if id == "" || len(id) > tailleIdMax || !utf8.ValidString(id) ||
			nom == "" || len(nom) > max || !utf8.ValidString(nom) {
			continue
		}
		if len(r) >= marquesParListe {
			break
		}
		r[id] = nom
	}
	return r
}

func egalesMoods(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for id, nom := range a {
		if v, ok := b[id]; !ok || v != nom {
			return false
		}
	}
	return true
}

// unirMoods : les moods de `dabord`, puis ceux de `ensuite` pour les ids
// qu'il n'a pas.
func unirMoods(dabord, ensuite map[string]string) map[string]string {
	return nettoyerMoods(unirObjets(dabord, ensuite))
}

// unirEmojis : de même pour les emojis rapides.
func unirEmojis(dabord, ensuite map[string]string) map[string]string {
	return nettoyerEmojis(unirObjets(dabord, ensuite))
}

func unirObjets(dabord, ensuite map[string]string) map[string]string {
	r := make(map[string]string, len(dabord)+len(ensuite))
	for id, nom := range ensuite {
		r[id] = nom
	}
	for id, nom := range dabord {
		r[id] = nom
	}
	return r
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
	// Les réglages vrais par défaut : un fichier d'avant eux ne les a pas.
	m := Marques{PresenceEnLigne: true, NotesContacts: true}
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
	m = Marques{PresenceEnLigne: true, NotesContacts: true}
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
		"head": m.Head, "favoris": m.Favoris, "epingles": m.Epingles, "archives": m.Archives,
		"privees": m.Privees, "moods": m.Moods, "emojis": m.Emojis, "garder_archives": m.GarderArchives,
		"cacher_apres_jours": m.CacherApresJours, "presence_en_ligne": m.PresenceEnLigne,
		"sons_conversation": jeuSons(m.SonsConversation), "notes_contacts": m.NotesContacts,
		"notes_musique": m.NotesMusique, "notes_musique_format": formatMusique(m.NotesMusiqueFormat), "notes_services": nettoyerServicesNotes(m.NotesServices),
		"ordre_services": nettoyerOrdreServices(m.OrdreServices), "personnes": nettoyerPersonnes(m.Personnes), "personnes_refus": nettoyerRefus(m.PersonnesRefus),
		"contacts_perso": nettoyerContactsPerso(m.ContactsPerso), "version": m.Version,
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
		Head     *[]string          `json:"head"`
		Favoris  *[]string          `json:"favoris"`
		Epingles *[]string          `json:"epingles"`
		Archives *[]string          `json:"archives"`
		Privees  *[]string          `json:"privees"`
		Moods    *map[string]string `json:"moods"`
		Emojis   *map[string]string `json:"emojis"`
		Garder   *bool              `json:"garder_archives"`
		Cacher   *int               `json:"cacher_apres_jours"`
		Presence *bool              `json:"presence_en_ligne"`
		Sons     json.RawMessage    `json:"sons_conversation"`
		NotesC   *bool              `json:"notes_contacts"`
		NotesM   *string            `json:"notes_musique"`
		NotesF   *string            `json:"notes_musique_format"`
		NotesS   *[]string          `json:"notes_services"`
		Ordre    json.RawMessage    `json:"ordre_services"`
		Pers     json.RawMessage    `json:"personnes"`
		PersR    json.RawMessage    `json:"personnes_refus"`
		Perso    json.RawMessage    `json:"contacts_perso"`
		Version  int64              `json:"version"`
		ID       json.RawMessage    `json:"id"`
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
	// Une copie : chaque champ que la trame porte est remplacé ci-dessous,
	// jamais modifié sur place.
	nouv := cur
	// Une liste absente de la trame ne change pas (un ancien client, qui ne
	// connaît pas `archives` ou `privees`, ne les vide donc jamais).
	for i, l := range []*[]string{d.Head, d.Favoris, d.Epingles, d.Archives, d.Privees} {
		if l != nil {
			*nouv.listes()[i] = nettoyerIds(*l)
		}
	}
	// De même pour `moods` (un client d'avant les moods ne les efface pas).
	if d.Moods != nil {
		nouv.Moods = nettoyerMoods(*d.Moods)
	}
	// Et pour `emojis` (un client d'avant les emojis rapides ne les efface pas).
	if d.Emojis != nil {
		nouv.Emojis = nettoyerEmojis(*d.Emojis)
	}
	// Et pour `garder_archives` : absent, le réglage ne change pas (un client
	// d'avant ce réglage ne le remet jamais à faux). Présent, il est pris tel
	// quel, même d'un client en retard (un simple oui/non : rien à unir).
	if d.Garder != nil {
		nouv.GarderArchives = *d.Garder
	}
	// De même pour `cacher_apres_jours` (un nombre de jours, borné).
	if d.Cacher != nil {
		nouv.CacherApresJours = bornerJours(*d.Cacher)
	}
	// Et pour `presence_en_ligne` : absent, rien ne change (un client d'avant
	// ce réglage ne le désactive jamais).
	if d.Presence != nil {
		nouv.PresenceEnLigne = *d.Presence
	}
	// Et pour `sons_conversation` : absent (ou null), rien ne change ; un
	// booléen (premiers clients) : vrai "bulle", faux "aucun".
	if j, ok := lireJeuSons(d.Sons); ok {
		nouv.SonsConversation = j
	}
	// Les réglages de « Quoi de neuf ? » : absents, rien ne change ; présents,
	// pris tels quels (des réglages : rien à unir).
	if d.NotesC != nil {
		nouv.NotesContacts = *d.NotesC
	}
	if d.NotesM != nil {
		nouv.NotesMusique = *d.NotesM
	}
	if d.NotesF != nil {
		nouv.NotesMusiqueFormat = *d.NotesF
	}
	if d.NotesS != nil {
		nouv.NotesServices = *d.NotesS
	}
	// L'ordre des services : absent, null ou mal formé, rien ne change (un
	// client d'avant ce réglage ne l'efface pas) ; présent, c'est l'ordre
	// complet, sans union (un réglage). Vide d'un client en retard : ignoré
	// (il n'a sans doute jamais reçu celui du serveur).
	if l, ok := lireOrdreServices(d.Ordre); ok && (len(l) > 0 || d.Version == cur.Version) {
		nouv.OrdreServices = l
	}
	// Les personnes et leurs refus : absents (ou null), rien ne change (un
	// client d'avant la fiche de la personne ne les efface pas) ; présents,
	// c'est la liste complète.
	if l, ok := lirePersonnes(d.Pers); ok {
		nouv.Personnes = l
	}
	if l, ok := lireRefus(d.PersR); ok {
		nouv.PersonnesRefus = l
	}
	// Et les contacts perso : absents (ou null), rien ne change ; présents,
	// c'est l'objet complet (une clé absente : « Rétablir »).
	if m, ok := lireContactsPerso(d.Perso); ok {
		nouv.ContactsPerso = m
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
		if len(nouv.Moods) == 0 && len(cur.Moods) > 0 {
			logf("marques de %q refusées : moods vides, version %d < %d", c.nom, d.Version, cur.Version)
			repondre(cur, "version périmée : les moods du serveur seraient vidés")
			return
		}
		if len(nouv.Emojis) == 0 && len(cur.Emojis) > 0 {
			logf("marques de %q refusées : emojis vides, version %d < %d", c.nom, d.Version, cur.Version)
			repondre(cur, "version périmée : les emojis rapides du serveur seraient vidés")
			return
		}
		if len(nouv.Personnes) == 0 && len(cur.Personnes) > 0 {
			logf("marques de %q refusées : personnes vides, version %d < %d", c.nom, d.Version, cur.Version)
			repondre(cur, "version périmée : les personnes du serveur seraient vidées")
			return
		}
		if len(nouv.PersonnesRefus) == 0 && len(cur.PersonnesRefus) > 0 {
			logf("marques de %q refusées : refus vides, version %d < %d", c.nom, d.Version, cur.Version)
			repondre(cur, "version périmée : les refus de personnes du serveur seraient vidés")
			return
		}
		if len(nouv.ContactsPerso) == 0 && len(cur.ContactsPerso) > 0 {
			logf("marques de %q refusées : contacts perso vides, version %d < %d", c.nom, d.Version, cur.Version)
			repondre(cur, "version périmée : les contacts perso du serveur seraient vidés")
			return
		}
		for i, l := range nouv.listes() {
			*l = unir(*l, *cur.listes()[i])
		}
		nouv.ContactsPerso = unirContactsPerso(nouv.ContactsPerso, cur.ContactsPerso)
		nouv.Personnes = unirPersonnes(nouv.Personnes, cur.Personnes)
		nouv.PersonnesRefus = unirRefus(nouv.PersonnesRefus, cur.PersonnesRefus)
		nouv.Moods = unirMoods(nouv.Moods, cur.Moods)
		nouv.Emojis = unirEmojis(nouv.Emojis, cur.Emojis)
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
	logf("marques version %d (de %q) : head %d, favoris %d, épinglées %d, archives %d, privées %d, moods %d, emojis %d, garder_archives %v, cacher_apres_jours %d, presence_en_ligne %v, sons_conversation %s, notes_contacts %v, notes_services %v, ordre_services %v, personnes %d, refus %d, contacts perso %d",
		nouv.Version, c.nom, len(nouv.Head), len(nouv.Favoris), len(nouv.Epingles), len(nouv.Archives), len(nouv.Privees), len(nouv.Moods), len(nouv.Emojis), nouv.GarderArchives, nouv.CacherApresJours, nouv.PresenceEnLigne, nouv.SonsConversation, nouv.NotesContacts, nouv.NotesServices, nouv.OrdreServices, len(nouv.Personnes), len(nouv.PersonnesRefus), len(nouv.ContactsPerso))
	if nouv.PresenceEnLigne != cur.PresenceEnLigne {
		s.presenceChangee(nouv.PresenceEnLigne)
	}
	if nouv.NotesContacts != cur.NotesContacts {
		s.notesContactsChange(nouv.NotesContacts)
	}
	if !egalesContactsPerso(nouv.ContactsPerso, cur.ContactsPerso) {
		perso := nouv.ContactsPerso
		go menagePhotosContacts(perso)
	}
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

// Le préfixe des ids d'un service, tels que les clients les voient.
var prefixeService = map[string]string{
	"google": "", "messenger": "m:", "instagram": "i:", "whatsapp": "w:", "signal": "s:", "x": "x:", "telegram": "t:", "bluesky": "b:", "discord": "d:", "reddit": "r:", "linkedin": "l:",
}

// desarchiverSiRecu : un événement `message` d'un pont. Un message reçu (pas
// envoyé par moi) plus récent que l'archivage d'une conversation de
// `archives` l'en retire : nouvelles marques, gardées puis diffusées à tous,
// avant l'événement lui-même. Idempotent : une conversation qui n'est plus
// dans `archives` ne change rien. Avec `garder_archives`, rien ne sort des
// archives (les non-lus et les notifications des clients, eux, suivent leur
// cours). À appeler sous s.mu.
func (s *Serveur) desarchiverSiRecu(svc string, ligne []byte) {
	if len(s.marques.Archives) == 0 || s.marques.GarderArchives {
		return
	}
	var ev struct {
		Item struct {
			Conv   string `json:"conv"`
			FromMe bool   `json:"from_me"`
			Time   int64  `json:"time"`
		} `json:"item"`
	}
	if json.Unmarshal(ligne, &ev) != nil || ev.Item.Conv == "" || ev.Item.FromMe {
		return
	}
	prefixe, ok := prefixeService[svc]
	if !ok {
		return
	}
	id := prefixe + ev.Item.Conv
	depuis, archivee := s.marques.ArchivesDepuis[id]
	if !archivee || ev.Item.Time <= depuis {
		return
	}
	cur := s.marques
	// Tout sauf `archives`, refaite ci-dessous.
	nouv := cur
	nouv.Archives = nil
	for _, a := range cur.Archives {
		if a != id {
			nouv.Archives = append(nouv.Archives, a)
		}
	}
	nouv.normaliser()
	nouv.Version = cur.Version + 1
	if err := garderMarques(nouv); err != nil {
		logf("marques : écriture impossible : %v", err)
		return
	}
	s.marques = nouv
	logf("marques version %d : une conversation %s sort des archives (message reçu)", nouv.Version, svc)
	s.diffuserVerrouille(trameMarques(nouv, nil, ""))
}

// jeuSons : un jeu de sons connu, sinon "bulle" (le défaut).
func jeuSons(j string) string {
	switch j {
	case "bulle", "bois", "verre", "aucun":
		return j
	}
	return "bulle"
}

// lireJeuSons : la valeur de `sons_conversation` d'une trame. Faux si elle
// est absente ou nulle (rien ne change) ; un booléen (premiers clients) :
// vrai "bulle", faux "aucun" ; une chaîne inconnue : "bulle".
func lireJeuSons(brute json.RawMessage) (string, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return "", false
	}
	var b bool
	if json.Unmarshal(brute, &b) == nil {
		if b {
			return "bulle", true
		}
		return "aucun", true
	}
	var j string
	if json.Unmarshal(brute, &j) == nil {
		return jeuSons(j), true
	}
	return "", false
}

const (
	tailleAppareilMax = 128 // octets au plus pour notes_musique
	servicesNotesMax  = 32  // noms au plus dans notes_services
	tailleServiceMax  = 32  // octets au plus par nom de service
)

// bornerAppareil : le nom de l'appareil de notes_musique, valide et borné.
func bornerAppareil(a string) string {
	if !utf8.ValidString(a) {
		return ""
	}
	for len(a) > tailleAppareilMax {
		_, n := utf8.DecodeLastRuneInString(a)
		a = a[:len(a)-n]
	}
	return a
}

// nettoyerServicesNotes : ["tout"] (le défaut), ou une liste propre de noms
// de services. Vide, ou contenant « tout » : ["tout"].
func nettoyerServicesNotes(l []string) []string {
	r := make([]string, 0, len(l))
	vus := map[string]bool{}
	for _, svc := range l {
		if svc == "tout" {
			return []string{"tout"}
		}
		if svc == "" || len(svc) > tailleServiceMax || !utf8.ValidString(svc) || vus[svc] {
			continue
		}
		vus[svc] = true
		r = append(r, svc)
		if len(r) >= servicesNotesMax {
			break
		}
	}
	if len(r) == 0 {
		return []string{"tout"}
	}
	return r
}

// formatMusique : un format de note musicale connu, sinon "artiste".
func formatMusique(f string) string {
	switch f {
	case "artiste", "genre", "titre":
		return f
	}
	return "artiste"
}

// nettoyerOrdreServices : l'ordre des services, sans doublon ni nom vide,
// invalide ou démesuré (32 noms de 32 octets au plus) ; jamais nul.
func nettoyerOrdreServices(l []string) []string {
	r := make([]string, 0, len(l))
	vus := map[string]bool{}
	for _, svc := range l {
		if svc == "" || len(svc) > tailleServiceMax || !utf8.ValidString(svc) || vus[svc] {
			continue
		}
		vus[svc] = true
		r = append(r, svc)
		if len(r) >= servicesNotesMax {
			break
		}
	}
	return r
}

// lireOrdreServices : la valeur de `ordre_services` d'une trame, nettoyée.
// Faux si elle est absente, nulle ou n'est pas une liste de chaînes (rien ne
// change, le reste de la trame compte).
func lireOrdreServices(brute json.RawMessage) ([]string, bool) {
	if len(brute) == 0 || string(brute) == "null" {
		return nil, false
	}
	var l []string
	if json.Unmarshal(brute, &l) != nil {
		return nil, false
	}
	return nettoyerOrdreServices(l), true
}
