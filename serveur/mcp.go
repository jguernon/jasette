// Le serveur MCP de Jasette : Claude (claude.ai, Claude Code) lit les
// messageries réunies par ce serveur (PROTOCOLE.md, §9).
//
//   - Fermé par défaut. Il s'ouvre par Réglages > Serveur Jasette > « Ouvrir
//     ma connexion MCP » (trame `mcp` du protocole), ou `--mcp ouvrir`.
//   - Transport Streamable HTTP, sur son propre port (47810 par défaut,
//     --mcp-port), sur les mêmes interfaces que le serveur (toutes, sauf
//     --ecoute). Chaque requête exige le jeton : en-tête
//     « Authorization: Bearer <jeton> », ou l'URL secrète /mcp/<jeton> pour
//     les clients qui ne savent pas envoyer d'en-tête (claude.ai).
//   - Le jeton (32 octets aléatoires) est gardé dans <données>/serveur/mcp.json
//     (0600) ; jamais écrit dans les journaux. Fermer le révoque ;
//     « Régénérer » le remplace.
//   - Outils en lecture seulement : lister_conversations, lire_messages,
//     chercher_messages, etat_services. Lire ne marque rien comme lu : le
//     serveur n'envoie jamais `read` aux ponts, et leur commande `messages`
//     ne marque rien. Les réponses des ponts aux demandes du MCP ne vont
//     qu'au MCP (et au cache), jamais aux appareils : leur interface ne les
//     prend donc pas pour une conversation ouverte.
//   - envoyer_message : seulement si l'utilisateur l'a permis (case à part,
//     désactivée par défaut, remise à non à la fermeture), limité en débit.
//   - Chaque appel d'outil est journalisé (nom, service, conversation, durée,
//     résultat), jamais le contenu des messages ni le texte cherché.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpPortParDefaut  = 47810
	mcpChemin         = "/mcp"
	mcpDelaiPont      = 30 * time.Second
	mcpDelaiRecherche = 15 * time.Second
	mcpDelaiEnvoi     = 60 * time.Second
	mcpTexteMax       = 4000
)

// ---- La configuration (mcp.json) ----

type ConfigMCP struct {
	Ouvert bool   `json:"ouvert"`
	Jeton  string `json:"jeton,omitempty"`
	Envoi  bool   `json:"envoi"`
	Depuis int64  `json:"depuis,omitempty"` // le jeton courant a été créé à cette heure (s)
}

func mcpPath() string { return filepath.Join(serveurDir(), "mcp.json") }

func lireConfigMCP() ConfigMCP {
	var c ConfigMCP
	if data, err := os.ReadFile(mcpPath()); err == nil {
		if json.Unmarshal(data, &c) != nil {
			logf("mcp : mcp.json illisible, connexion MCP fermée")
			return ConfigMCP{}
		}
	}
	if c.Jeton == "" {
		c.Ouvert = false
	}
	if !c.Ouvert {
		c.Jeton, c.Envoi = "", false
	}
	return c
}

func nouveauJetonMCP() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

var errMCPFerme = errors.New("la connexion MCP est fermée")

// actionMCP applique une action à mcp.json, sous le verrou du serveur.
// Actions : etat, ouvrir, fermer, regenerer, envoi (avec `envoi`).
func actionMCP(action string, envoi *bool) (ConfigMCP, error) {
	var c ConfigMCP
	err := avecVerrou(func() error {
		c = lireConfigMCP()
		avant := c
		switch action {
		case "etat":
		case "ouvrir":
			if !c.Ouvert {
				c = ConfigMCP{Ouvert: true, Jeton: nouveauJetonMCP(), Depuis: time.Now().Unix()}
			}
		case "fermer":
			c = ConfigMCP{}
		case "regenerer":
			if !c.Ouvert {
				return errMCPFerme
			}
			c.Jeton, c.Depuis = nouveauJetonMCP(), time.Now().Unix()
		case "envoi":
			if envoi == nil {
				return errors.New("envoi : valeur manquante")
			}
			if !c.Ouvert {
				return errMCPFerme
			}
			c.Envoi = *envoi
		default:
			return fmt.Errorf("action MCP inconnue : %q", action)
		}
		if c == avant {
			return nil
		}
		data, _ := json.MarshalIndent(c, "", "  ")
		return ecrireSecret(mcpPath(), data)
	})
	return c, err
}

// cmdMCP : `jasette-serveur --mcp <action>` (le serveur en marche voit le
// changement dans les 2 secondes).
func cmdMCP(action string, port int) int {
	var envoi *bool
	switch action {
	case "envoi-oui", "envoi-non":
		v := action == "envoi-oui"
		envoi, action = &v, "envoi"
	}
	c, err := actionMCP(action, envoi)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !c.Ouvert {
		fmt.Println("Connexion MCP : fermée")
		return 0
	}
	fmt.Println("Connexion MCP : ouverte")
	for _, a := range adressesLocales() {
		fmt.Printf("URL   : http://%s%s\n", net.JoinHostPort(a, strconv.Itoa(port)), mcpChemin)
	}
	fmt.Printf("Jeton : %s\n", c.Jeton)
	if c.Envoi {
		fmt.Println("Envoi de messages : permis")
	} else {
		fmt.Println("Envoi de messages : non permis")
	}
	return 0
}

// adressesLocales : les adresses IPv4 de cette machine (sans Docker), puis 127.0.0.1.
func adressesLocales() []string {
	var r []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Les ponts de Docker et des machines virtuelles ne mènent pas ici.
		if n := i.Name; strings.HasPrefix(n, "docker") || strings.HasPrefix(n, "br-") ||
			strings.HasPrefix(n, "veth") || strings.HasPrefix(n, "virbr") {
			continue
		}
		adrs, _ := i.Addrs()
		for _, a := range adrs {
			n, ok := a.(*net.IPNet)
			if ok && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
				r = append(r, n.IP.String())
			}
		}
	}
	return append(r, "127.0.0.1")
}

// ---- Le service HTTP ----

type ServiceMCP struct {
	srv     *Serveur
	adresse string // hôte:port d'écoute
	port    int

	mu      sync.Mutex
	cfg     ConfigMCP
	http    *http.Server
	handler http.Handler // le serveur MCP du jeton et du réglage d'envoi courants
	erreur  string       // la dernière erreur d'écoute

	refus      *Limiteur // jetons faux, par adresse
	envoisMin  *Limiteur
	envoisHeur *Limiteur
}

func nouveauServiceMCP(srv *Serveur, hote string, port int) *ServiceMCP {
	return &ServiceMCP{
		srv: srv, adresse: net.JoinHostPort(hote, strconv.Itoa(port)), port: port,
		refus:      nouveauLimiteur(20, 10*time.Minute),
		envoisMin:  nouveauLimiteur(5, time.Minute),
		envoisHeur: nouveauLimiteur(30, time.Hour),
	}
}

// surveiller : applique mcp.json au lancement, puis à chaque changement
// (par --mcp, pendant que le serveur tourne).
func (m *ServiceMCP) surveiller() {
	var vu time.Time
	for {
		var mod time.Time
		if st, err := os.Stat(mcpPath()); err == nil {
			mod = st.ModTime()
		}
		if !mod.Equal(vu) || vu.IsZero() {
			vu = mod
			if vu.IsZero() {
				vu = time.Unix(1, 0)
			}
			m.appliquer(lireConfigMCP())
		}
		time.Sleep(2 * time.Second)
	}
}

// appliquer : ouvre ou ferme l'écoute, refait le serveur MCP si le jeton ou
// le réglage d'envoi a changé (les sessions de l'ancien jeton tombent).
func (m *ServiceMCP) appliquer(c ConfigMCP) {
	m.mu.Lock()
	defer m.mu.Unlock()
	avant := m.cfg
	m.cfg = c
	if !c.Ouvert {
		m.handler = nil
		if m.http != nil {
			_ = m.http.Close()
			m.http = nil
			logf("mcp : connexion fermée")
		}
		return
	}
	if m.handler == nil || c.Jeton != avant.Jeton || c.Envoi != avant.Envoi {
		m.handler = m.construire(c.Envoi)
		if avant.Ouvert && c.Jeton != avant.Jeton {
			logf("mcp : nouveau jeton")
		}
		if c.Envoi != avant.Envoi && avant.Ouvert {
			logf("mcp : envoi de messages %s", map[bool]string{true: "permis", false: "retiré"}[c.Envoi])
		}
	}
	if m.http == nil {
		ln, err := net.Listen("tcp", m.adresse)
		if err != nil {
			m.erreur = err.Error()
			logf("mcp : écoute impossible sur %s : %v", m.adresse, err)
			return
		}
		m.erreur = ""
		hs := &http.Server{
			Handler:           m,
			ReadHeaderTimeout: 15 * time.Second,
			IdleTimeout:       2 * time.Minute,
			MaxHeaderBytes:    64 << 10,
			ErrorLog:          nil,
		}
		m.http = hs
		go func() {
			if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logf("mcp : %v", err)
			}
		}()
		logf("mcp : connexion ouverte, http://%s%s (envoi %s)", ln.Addr(), mcpChemin,
			map[bool]string{true: "permis", false: "non permis"}[c.Envoi])
	}
}

// etat : ce qu'on répond à la trame `mcp`.
func (m *ServiceMCP) etat() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := map[string]any{
		"ouvert": m.cfg.Ouvert, "envoi": m.cfg.Envoi, "port": m.port, "chemin": mcpChemin,
		"adresses": adressesLocales(), "ecoute": m.http != nil,
	}
	if m.cfg.Ouvert {
		r["jeton"] = m.cfg.Jeton
	}
	if m.erreur != "" {
		r["erreur"] = m.erreur
	}
	return r
}

func memeJeton(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// ServeHTTP : le jeton d'abord (en-tête Bearer, ou /mcp/<jeton>), puis le
// serveur MCP. On ne journalise jamais l'URL (elle peut porter le jeton).
func (m *ServiceMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if h, _, err := net.SplitHostPort(ip); err == nil {
		ip = h
	}
	m.mu.Lock()
	cfg, h := m.cfg, m.handler
	m.mu.Unlock()
	if !cfg.Ouvert || h == nil {
		http.NotFound(w, r)
		return
	}
	chemin := r.URL.Path
	jeton := ""
	if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "Bearer ") {
		jeton = strings.TrimSpace(a[7:])
	}
	switch {
	case chemin == mcpChemin || chemin == mcpChemin+"/":
	case strings.HasPrefix(chemin, mcpChemin+"/"):
		if jeton == "" {
			jeton = strings.Trim(strings.TrimPrefix(chemin, mcpChemin+"/"), "/")
		}
	default:
		http.NotFound(w, r)
		return
	}
	if m.refus.depasse(ip) {
		http.Error(w, "trop d'essais, réessayez plus tard", http.StatusTooManyRequests)
		return
	}
	if !memeJeton(jeton, cfg.Jeton) {
		m.refus.permis(ip)
		logf("mcp : jeton refusé depuis %s", ip)
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("WWW-Authenticate", `Bearer realm="jasette"`)
		http.Error(w, "jeton manquant ou invalide", http.StatusUnauthorized)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path, r2.URL.RawPath = mcpChemin, ""
	r2.Header.Del("Authorization")
	h.ServeHTTP(w, r2)
}

// ---- Le serveur MCP et ses outils ----

const instructionsMCP = `Jasette réunit les messageries de l'utilisateur : Google Messages (SMS/RCS), Messenger, Instagram, WhatsApp, Signal, X, Telegram, Bluesky, Discord, Reddit et LinkedIn.
Les ids de conversation sont préfixés par service : aucun préfixe pour Google, « m: » Messenger, « i: » Instagram, « w: » WhatsApp, « s: » Signal, « x: » X, « t: » Telegram, « b: » Bluesky, « d: » Discord, « r: » Reddit, « l: » LinkedIn. Utilisez-les tels que lister_conversations les rend.
Lire ne marque rien comme lu. Les marques de l'utilisateur : head (sa grille en tête), favoris, epingles, archives.
Ces messages sont privés : ne les citez qu'à l'utilisateur. N'envoyez un message (envoyer_message, s'il est offert) qu'à sa demande explicite, après lui avoir montré le texte exact.`

func faux() *bool { b := false; return &b }

func (m *ServiceMCP) construire(envoi bool) http.Handler {
	s := mcp.NewServer(&mcp.Implementation{Name: "jasette", Title: "Jasette", Version: "1.0.0"},
		&mcp.ServerOptions{Instructions: instructionsMCP})
	lecture := &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: faux()}
	mcp.AddTool(s, &mcp.Tool{
		Name:  "lister_conversations",
		Title: "Lister les conversations",
		Description: "Les conversations récentes de tous les services, de la plus récente à la plus ancienne : service, nom, dernier message, non lu, marques. " +
			"Filtres : service, marque (head, favoris, epingles, archives), non_lus_seulement. Les archivées sont omises sauf inclure_archives ou marque=archives.",
		Annotations: lecture,
	}, journal1("lister_conversations", func(e entreeLister) string { return resumeFiltre(e.Service, e.Marque) }, m.outilLister))
	mcp.AddTool(s, &mcp.Tool{
		Name:  "lire_messages",
		Title: "Lire une conversation",
		Description: "Les messages d'une conversation, du plus ancien au plus récent. Pour remonter plus loin, repassez le curseur « suite » dans « avant ». " +
			"Ne marque rien comme lu.",
		Annotations: lecture,
	}, journal1("lire_messages", func(e entreeLire) string { return " " + e.Conversation }, m.outilLire))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "chercher_messages",
		Title:       "Chercher dans les messages",
		Description: "Cherche un texte (sans tenir compte des accents ni de la casse) dans les messages gardés par Jasette et par les ponts. Les plus récents d'abord.",
		Annotations: lecture,
	}, journal1("chercher_messages", func(e entreeChercher) string {
		return fmt.Sprintf("%s (%d caractères)", resumeFiltre(e.Service, ""), utf8.RuneCountInString(e.Texte))
	}, m.outilChercher))
	mcp.AddTool(s, &mcp.Tool{
		Name:        "etat_services",
		Title:       "État des services",
		Description: "L'état de chaque service (prêt, en connexion, non associé…), le compte, le nombre de conversations et de non lues.",
		Annotations: lecture,
	}, journal1("etat_services", func(struct{}) string { return "" }, m.outilEtat))
	if envoi {
		mcp.AddTool(s, &mcp.Tool{
			Name:  "envoyer_message",
			Title: "Envoyer un message",
			Description: "Envoie un message texte dans une conversation existante, au nom de l'utilisateur. Seulement à sa demande explicite, avec le texte exact qu'il a approuvé. " +
				"Limité à 5 envois par minute et 30 par heure.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: faux(), OpenWorldHint: func() *bool { b := true; return &b }()},
		}, journal1("envoyer_message", func(e entreeEnvoyer) string {
			return fmt.Sprintf(" %s (%d caractères)", e.Conversation, utf8.RuneCountInString(e.Texte))
		}, m.outilEnvoyer))
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		SessionTimeout: 30 * time.Minute,
		// Le jeton protège déjà chaque requête ; derrière un tunnel ou un
		// mandataire (claude.ai), les requêtes arrivent sur 127.0.0.1 avec le
		// nom public dans Host : la protection de l'hôte local les refuserait.
		DisableLocalhostProtection: true,
	})
}

func resumeFiltre(svc, marque string) string {
	r := ""
	if svc != "" {
		r += " service=" + svc
	}
	if marque != "" {
		r += " marque=" + marque
	}
	return r
}

// journal1 : chaque appel d'outil va au journal (sans contenu de message).
func journal1[In, Out any](nom string, resume func(In) string, f mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		debut := time.Now()
		r, o, err := f(ctx, req, in)
		statut := "ok"
		if err != nil {
			statut = "erreur"
		}
		logf("mcp : %s%s → %s (%d ms)", nom, resume(in), statut, time.Since(debut).Milliseconds())
		return r, o, err
	}
}

// ---- Les données des ponts, telles que gardées ----

type convPont struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Preview  string   `json:"preview"`
	FromMe   bool     `json:"from_me"`
	Time     int64    `json:"time"`
	Unread   bool     `json:"unread"`
	Group    bool     `json:"group"`
	Archived bool     `json:"archived"`
	Members  []string `json:"members"`
	ReadOnly bool     `json:"read_only"`
}

type msgPont struct {
	ID          string `json:"id"`
	Conv        string `json:"conv"`
	Text        string `json:"text"`
	Media       string `json:"media"`
	FromMe      bool   `json:"from_me"`
	Sender      string `json:"sender"`
	Time        int64  `json:"time"`
	Attachments []struct {
		Name string `json:"name"`
		Mime string `json:"mime"`
	} `json:"attachments"`
	Reactions []struct {
		Emoji  string `json:"emoji"`
		Sender string `json:"sender"`
		FromMe bool   `json:"from_me"`
	} `json:"reactions"`
}

func heure(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func apercuMsg(m msgPont) string {
	if strings.TrimSpace(m.Text) != "" {
		return m.Text
	}
	if len(m.Attachments) > 0 {
		return "[pièce jointe : " + cmpOr(m.Attachments[0].Name, m.Attachments[0].Mime, "fichier") + "]"
	}
	if m.Media != "" {
		return "[pièce jointe : " + m.Media + "]"
	}
	return ""
}

func cmpOr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// svcDeId : le service et l'id du pont d'un id préfixé.
func svcDeId(id string) (string, string) {
	for svc, p := range prefixeService {
		if p != "" && strings.HasPrefix(id, p) {
			return svc, strings.TrimPrefix(id, p)
		}
	}
	return "google", id
}

// convsGardees : les conversations des listes gardées d'un service
// (boîte de réception, puis archives, marquées Archived).
func (c *Cache) convsGardees() []convPont {
	c.mu.Lock()
	listes := map[string]json.RawMessage{}
	for k, v := range c.listes {
		listes[k] = v
	}
	c.mu.Unlock()
	var r []convPont
	vus := map[string]bool{}
	for _, cle := range []string{"inbox", "archive"} {
		var ev struct {
			Items []convPont `json:"items"`
		}
		if json.Unmarshal(listes[cle], &ev) != nil {
			continue
		}
		for _, it := range ev.Items {
			if it.ID == "" || vus[it.ID] {
				continue
			}
			vus[it.ID] = true
			if cle == "archive" {
				it.Archived = true
			}
			r = append(r, it)
		}
	}
	return r
}

// listeFraiche : la boîte de réception a-t-elle été reçue du pont depuis moins de `age` ?
func (c *Cache) listeFraiche(age time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.listeMaj.IsZero() && time.Since(c.listeMaj) < age
}

// rafraichirListe : si le service est prêt et que sa liste gardée manque ou
// date de plus d'une minute, la redemande au pont (10 s au plus ; sinon la
// liste gardée sert telle quelle).
func (m *ServiceMCP) rafraichirListe(ctx context.Context, p *Pont) {
	if !m.etatDe(p).pret || p.cache.listeFraiche(time.Minute) {
		return
	}
	_, _ = m.demanderPont(ctx, p, "conversations", "inbox", map[string]any{"cmd": "conversations", "count": 100}, 10*time.Second)
}

// rafraichirListes : rafraichirListe pour plusieurs services, en parallèle.
func (m *ServiceMCP) rafraichirListes(ctx context.Context, svcs []string) {
	var wg sync.WaitGroup
	for _, svc := range svcs {
		p := m.srv.ponts[svc]
		wg.Add(1)
		go func() { defer wg.Done(); m.rafraichirListe(ctx, p) }()
	}
	wg.Wait()
}

func (c *Cache) aDesListes() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.listes["inbox"]
	return ok
}

// dernierGarde : le dernier message gardé d'une conversation.
func (c *Cache) dernierGarde(conv string) (msgPont, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.convs[conv]
	if e == nil || len(e.items) == 0 {
		return msgPont{}, false
	}
	var m msgPont
	if json.Unmarshal(e.items[len(e.items)-1].brut, &m) != nil {
		return msgPont{}, false
	}
	return m, true
}

// copieGardee : les messages gardés d'une conversation (du plus vieux au
// plus récent), s'il en reste d'autres, et si la copie est à jour.
func (c *Cache) copieGardee(conv string) (items []json.RawMessage, more, ajour, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.convs[conv]
	if e == nil {
		return nil, false, false, false
	}
	for _, m := range e.items {
		items = append(items, m.brut)
	}
	return items, e.more, e.session, true
}

func (c *Cache) nbCopies() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.convs)
}

// chercherGarde : les messages gardés dont le texte contient q (déjà plié).
func (c *Cache) chercherGarde(q string) []msgPont {
	c.mu.Lock()
	defer c.mu.Unlock()
	var r []msgPont
	for conv, e := range c.convs {
		for _, x := range e.items {
			var m msgPont
			if json.Unmarshal(x.brut, &m) != nil || m.Text == "" || strings.HasPrefix(m.ID, "tmp_") {
				continue
			}
			if strings.Contains(plierTexte(m.Text), q) {
				if m.Conv == "" {
					m.Conv = conv
				}
				r = append(r, m)
			}
		}
	}
	return r
}

var plieur = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "á", "a", "ã", "a", "å", "a",
	"ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ñ", "n",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y",
	"œ", "oe", "æ", "ae", "’", "'",
)

func plierTexte(s string) string { return plieur.Replace(strings.ToLower(s)) }

// ---- Les demandes du MCP aux ponts ----

// attenteMCP : une réponse de pont attendue par le MCP. Sous srv.mu.
type attenteMCP struct {
	sorte, cle string
	ch         chan reponseMCP
	erreur     string // une erreur du pont vue en attendant (envoi)
}

type reponseMCP struct {
	ligne  []byte
	erreur string
}

func cleMessages(conv string, ancien bool) string {
	if ancien {
		return conv + "\x00ancien"
	}
	return conv
}

// livrerMCP : donne la ligne à la première attente de cette sorte et clé.
// Sous srv.mu.
func (p *Pont) livrerMCP(sorte, cle string, ligne []byte) bool {
	for i, a := range p.mcpAttentes {
		if a.sorte == sorte && a.cle == cle {
			a.ch <- reponseMCP{ligne: append([]byte(nil), ligne...), erreur: a.erreur}
			p.mcpAttentes = append(p.mcpAttentes[:i:i], p.mcpAttentes[i+1:]...)
			return true
		}
	}
	return false
}

// erreurMCP : une erreur du pont pour la première attente de cette sorte.
// Sous srv.mu.
func (p *Pont) erreurMCP(sorte, texte string) bool {
	for i, a := range p.mcpAttentes {
		if a.sorte == sorte {
			a.ch <- reponseMCP{erreur: texte}
			p.mcpAttentes = append(p.mcpAttentes[:i:i], p.mcpAttentes[i+1:]...)
			return true
		}
	}
	return false
}

func (p *Pont) attendMCP(sorte string) *attenteMCP {
	for _, a := range p.mcpAttentes {
		if a.sorte == sorte {
			return a
		}
	}
	return nil
}

// demanderPont envoie une commande au pont et attend la réponse que recu()
// lui remet (sorte, clé).
func (m *ServiceMCP) demanderPont(ctx context.Context, p *Pont, sorte, cle string, cmd map[string]any, delai time.Duration) (reponseMCP, error) {
	a := &attenteMCP{sorte: sorte, cle: cle, ch: make(chan reponseMCP, 1)}
	srv := m.srv
	srv.mu.Lock()
	p.mcpAttentes = append(p.mcpAttentes, a)
	srv.mu.Unlock()
	defer func() {
		srv.mu.Lock()
		for i, x := range p.mcpAttentes {
			if x == a {
				p.mcpAttentes = append(p.mcpAttentes[:i:i], p.mcpAttentes[i+1:]...)
				break
			}
		}
		srv.mu.Unlock()
	}()
	b, _ := json.Marshal(cmd)
	if err := p.envoyer(b); err != nil {
		return reponseMCP{}, fmt.Errorf("%s : %v", p.svc, err)
	}
	select {
	case r := <-a.ch:
		return r, nil
	case <-ctx.Done():
		return reponseMCP{}, ctx.Err()
	case <-time.After(delai):
		srv.mu.Lock()
		e := a.erreur
		srv.mu.Unlock()
		if e != "" {
			return reponseMCP{}, fmt.Errorf("%s : %s", p.svc, e)
		}
		return reponseMCP{}, fmt.Errorf("%s : pas de réponse du service", p.svc)
	}
}

// ---- Les outils ----

type etatPont struct {
	pret   bool
	etat   string
	compte string
}

func (m *ServiceMCP) etatDe(p *Pont) etatPont {
	m.srv.mu.Lock()
	st, ac, pret := p.dernier["state"], p.dernier["account"], p.pret
	m.srv.mu.Unlock()
	e := etatPont{pret: pret, etat: "inconnu"}
	var s struct {
		Ev struct {
			State string `json:"state"`
		} `json:"ev"`
	}
	if json.Unmarshal(st, &s) == nil && s.Ev.State != "" {
		e.etat = s.Ev.State
	}
	var a struct {
		Ev map[string]any `json:"ev"`
	}
	if json.Unmarshal(ac, &a) == nil {
		for _, k := range []string{"name", "email", "username", "number"} {
			if v, ok := a.Ev[k].(string); ok && v != "" {
				e.compte = v
				break
			}
		}
	}
	return e
}

func (m *ServiceMCP) services(filtre string) ([]string, error) {
	tous := m.srv.servicesTries()
	if filtre == "" {
		return tous, nil
	}
	filtre = strings.ToLower(strings.TrimSpace(filtre))
	for _, s := range tous {
		if s == filtre {
			return []string{s}, nil
		}
	}
	return nil, fmt.Errorf("service inconnu ou non offert : %q (offerts : %s)", filtre, strings.Join(tous, ", "))
}

// privees : les conversations de la Liste privée (marque `privees`, §5.5).
// Le MCP ne les montre jamais : ni liste, ni lecture, ni recherche, ni
// compte, ni envoi ; une conversation privée y est « inconnue ».
func (m *ServiceMCP) privees() map[string]bool {
	m.srv.mu.Lock()
	defer m.srv.mu.Unlock()
	r := make(map[string]bool, len(m.srv.marques.Privees))
	for _, id := range m.srv.marques.Privees {
		r[id] = true
	}
	return r
}

func (m *ServiceMCP) marquesDe() map[string][]string {
	m.srv.mu.Lock()
	defer m.srv.mu.Unlock()
	r := map[string][]string{}
	for nom, l := range map[string][]string{
		"head": m.srv.marques.Head, "favoris": m.srv.marques.Favoris,
		"epingles": m.srv.marques.Epingles, "archives": m.srv.marques.Archives,
	} {
		for _, id := range l {
			r[id] = append(r[id], nom)
		}
	}
	for _, l := range r {
		sort.Strings(l)
	}
	return r
}

// -- lister_conversations

type entreeLister struct {
	Service         string `json:"service,omitempty" jsonschema:"un seul service : google, messenger, instagram, whatsapp, signal, x, telegram, bluesky, discord, reddit ou linkedin"`
	Marque          string `json:"marque,omitempty" jsonschema:"seulement les conversations de cette marque : head, favoris, epingles ou archives"`
	NonLusSeulement bool   `json:"non_lus_seulement,omitempty" jsonschema:"seulement les conversations non lues"`
	InclureArchives bool   `json:"inclure_archives,omitempty" jsonschema:"inclure les conversations archivées"`
	Limite          int    `json:"limite,omitempty" jsonschema:"nombre maximal de conversations (50 par défaut, 200 au plus)"`
}

type ConvMCP struct {
	ID             string   `json:"id" jsonschema:"id préfixé, pour lire_messages"`
	Service        string   `json:"service"`
	Nom            string   `json:"nom"`
	Groupe         bool     `json:"groupe,omitempty"`
	Membres        []string `json:"membres,omitempty"`
	DernierMessage string   `json:"dernier_message"`
	DernierDeMoi   bool     `json:"dernier_de_moi"`
	Heure          string   `json:"heure"`
	NonLu          bool     `json:"non_lu"`
	Marques        []string `json:"marques,omitempty"`
	Archivee       bool     `json:"archivee,omitempty"`
	LectureSeule   bool     `json:"lecture_seule,omitempty"`
	temps          int64
}

type ListeMCP struct {
	Conversations []ConvMCP `json:"conversations"`
	Total         int       `json:"total" jsonschema:"nombre de conversations qui répondent aux filtres, avant la limite"`
	NonLues       int       `json:"non_lues"`
	Remarques     []string  `json:"remarques,omitempty"`
}

var marquesConnues = map[string]bool{"head": true, "favoris": true, "epingles": true, "archives": true}

func (m *ServiceMCP) outilLister(ctx context.Context, _ *mcp.CallToolRequest, e entreeLister) (*mcp.CallToolResult, ListeMCP, error) {
	out := ListeMCP{Conversations: []ConvMCP{}}
	svcs, err := m.services(e.Service)
	if err != nil {
		return nil, out, err
	}
	marque := strings.ToLower(strings.TrimSpace(e.Marque))
	if marque == "épingles" || marque == "epinglees" || marque == "épinglées" {
		marque = "epingles"
	}
	if marque != "" && !marquesConnues[marque] {
		return nil, out, fmt.Errorf("marque inconnue : %q (head, favoris, epingles, archives)", e.Marque)
	}
	limite := e.Limite
	if limite <= 0 {
		limite = 50
	}
	limite = min(limite, 200)
	m.rafraichirListes(ctx, svcs)
	marques := m.marquesDe()
	privees := m.privees()
	var toutes []ConvMCP
	for _, svc := range svcs {
		p := m.srv.ponts[svc]
		if !p.cache.aDesListes() {
			out.Remarques = append(out.Remarques, svc+" : pas encore de liste ("+m.etatDe(p).etat+")")
			continue
		}
		for _, c := range p.cache.convsGardees() {
			id := prefixeService[svc] + c.ID
			if privees[id] {
				continue
			}
			mq := marques[id]
			x := ConvMCP{
				ID: id, Service: svc, Nom: c.Name, Groupe: c.Group, Membres: c.Members,
				DernierMessage: c.Preview, DernierDeMoi: c.FromMe, NonLu: c.Unread, temps: c.Time,
				Marques: mq, LectureSeule: c.ReadOnly,
				Archivee: c.Archived || contient(mq, "archives"),
			}
			if d, ok := p.cache.dernierGarde(c.ID); ok && d.Time > c.Time {
				x.DernierMessage, x.DernierDeMoi, x.temps = apercuMsg(d), d.FromMe, d.Time
				if !d.FromMe {
					x.NonLu = true
				}
			}
			x.Heure = heure(x.temps)
			switch {
			case marque == "archives":
				if !x.Archivee {
					continue
				}
			case marque != "":
				if !contient(mq, marque) {
					continue
				}
			case !e.InclureArchives && x.Archivee:
				continue
			}
			if e.NonLusSeulement && !x.NonLu {
				continue
			}
			toutes = append(toutes, x)
		}
	}
	sort.SliceStable(toutes, func(i, j int) bool { return toutes[i].temps > toutes[j].temps })
	out.Total = len(toutes)
	for _, x := range toutes {
		if x.NonLu {
			out.NonLues++
		}
	}
	if len(toutes) > limite {
		toutes = toutes[:limite]
	}
	out.Conversations = append(out.Conversations, toutes...)
	return nil, out, nil
}

func contient(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// -- lire_messages

type entreeLire struct {
	Conversation string `json:"conversation" jsonschema:"l'id de la conversation, tel que rendu par lister_conversations (ex. m:123)"`
	Nombre       int    `json:"nombre,omitempty" jsonschema:"nombre de messages (30 par défaut, 100 au plus)"`
	Avant        string `json:"avant,omitempty" jsonschema:"le curseur « suite » d'une réponse précédente, pour des messages plus anciens"`
}

type MsgMCP struct {
	ID            string   `json:"id"`
	De            string   `json:"de"`
	DeMoi         bool     `json:"de_moi"`
	Texte         string   `json:"texte"`
	Heure         string   `json:"heure"`
	PiecesJointes []string `json:"pieces_jointes,omitempty"`
	Reactions     []string `json:"reactions,omitempty"`
}

type MessagesMCP struct {
	Conversation string   `json:"conversation"`
	Service      string   `json:"service"`
	Nom          string   `json:"nom,omitempty"`
	Messages     []MsgMCP `json:"messages"`
	PlusAnciens  bool     `json:"plus_anciens" jsonschema:"il y a des messages plus anciens (voir suite)"`
	Suite        string   `json:"suite,omitempty" jsonschema:"à passer dans « avant » pour les messages plus anciens"`
	Source       string   `json:"source" jsonschema:"copie du serveur, ou service"`
	Remarque     string   `json:"remarque,omitempty"`
}

func versMsgMCP(brut json.RawMessage) (MsgMCP, int64, bool) {
	var m msgPont
	if json.Unmarshal(brut, &m) != nil || m.ID == "" {
		return MsgMCP{}, 0, false
	}
	x := MsgMCP{ID: m.ID, DeMoi: m.FromMe, Texte: m.Text, Heure: heure(m.Time), De: m.Sender}
	if m.FromMe {
		x.De = "moi"
	}
	for _, a := range m.Attachments {
		x.PiecesJointes = append(x.PiecesJointes, cmpOr(a.Name, a.Mime, "fichier"))
	}
	if len(m.Attachments) == 0 && m.Media != "" {
		x.PiecesJointes = append(x.PiecesJointes, m.Media)
	}
	for _, r := range m.Reactions {
		qui := r.Sender
		if r.FromMe || qui == "" {
			qui = "moi"
		}
		x.Reactions = append(x.Reactions, r.Emoji+" "+qui)
	}
	return x, m.Time, true
}

func (m *ServiceMCP) nomConv(p *Pont, brut string) string {
	for _, c := range p.cache.convsGardees() {
		if c.ID == brut {
			return c.Name
		}
	}
	return ""
}

func (m *ServiceMCP) outilLire(ctx context.Context, _ *mcp.CallToolRequest, e entreeLire) (*mcp.CallToolResult, MessagesMCP, error) {
	out := MessagesMCP{Conversation: e.Conversation, Messages: []MsgMCP{}}
	id := strings.TrimSpace(e.Conversation)
	if id == "" {
		return nil, out, errors.New("conversation : id manquant")
	}
	svc, brut := svcDeId(id)
	p := m.srv.ponts[svc]
	if p == nil {
		return nil, out, fmt.Errorf("service non offert par ce serveur : %s", svc)
	}
	if m.privees()[prefixeService[svc]+brut] {
		return nil, out, errors.New("conversation inconnue")
	}
	if !p.cache.aDesListes() {
		m.rafraichirListe(ctx, p)
	}
	out.Service, out.Nom = svc, m.nomConv(p, brut)
	nombre := e.Nombre
	if nombre <= 0 {
		nombre = 30
	}
	nombre = min(nombre, 100)
	pret := m.etatDe(p).pret

	var items []json.RawMessage
	var more bool
	if e.Avant == "" {
		copie, cMore, ajour, ok := p.cache.copieGardee(brut)
		switch {
		case ok && ajour && (len(copie) >= nombre || !cMore):
			items, more, out.Source = copie, cMore, "copie du serveur"
		case pret:
			r, err := m.demanderPont(ctx, p, "messages", cleMessages(brut, false),
				map[string]any{"cmd": "messages", "conv": brut, "count": nombre, "auto": true}, mcpDelaiPont)
			if err == nil && r.erreur != "" {
				err = fmt.Errorf("%s : %s", svc, r.erreur)
			}
			if err != nil {
				if !ok {
					return nil, out, err
				}
				items, more, out.Source = copie, cMore, "copie du serveur"
				out.Remarque = "le service n'a pas répondu : copie gardée par le serveur, peut-être incomplète"
				break
			}
			items, more = lirePage(r.ligne)
			out.Source = "service"
		case ok:
			items, more, out.Source = copie, cMore, "copie du serveur"
			out.Remarque = svc + " n'est pas prêt : copie gardée par le serveur, peut-être pas à jour"
		default:
			return nil, out, fmt.Errorf("%s n'est pas prêt (%s) et le serveur n'a pas de copie de cette conversation", svc, m.etatDe(p).etat)
		}
	} else {
		avantID, t, _ := strings.Cut(e.Avant, "|")
		avantT, _ := strconv.ParseInt(t, 10, 64)
		if avantID == "" {
			return nil, out, errors.New("avant : curseur illisible (utilisez « suite »)")
		}
		if !pret {
			return nil, out, fmt.Errorf("%s n'est pas prêt (%s) : impossible de remonter plus loin", svc, m.etatDe(p).etat)
		}
		r, err := m.demanderPont(ctx, p, "messages", cleMessages(brut, true),
			map[string]any{"cmd": "messages", "conv": brut, "count": nombre, "before": avantID, "before_time": avantT, "auto": true}, mcpDelaiPont)
		if err == nil && r.erreur != "" {
			err = fmt.Errorf("%s : %s", svc, r.erreur)
		}
		if err != nil {
			return nil, out, err
		}
		items, more = lirePage(r.ligne)
		out.Source = "service"
	}
	if len(items) > nombre {
		items, more = items[len(items)-nombre:], true
	}
	var premierT int64
	for _, b := range items {
		if x, t, ok := versMsgMCP(b); ok {
			if len(out.Messages) == 0 {
				premierT = t
			}
			out.Messages = append(out.Messages, x)
		}
	}
	if more && len(out.Messages) > 0 {
		out.PlusAnciens = true
		out.Suite = out.Messages[0].ID + "|" + strconv.FormatInt(premierT, 10)
	}
	return nil, out, nil
}

func lirePage(ligne []byte) ([]json.RawMessage, bool) {
	var ev struct {
		Items []json.RawMessage `json:"items"`
		More  bool              `json:"more"`
	}
	_ = json.Unmarshal(ligne, &ev)
	var l []json.RawMessage
	type paire struct {
		b json.RawMessage
		t int64
	}
	var ps []paire
	for _, b := range ev.Items {
		var t teteMsg
		_ = json.Unmarshal(b, &t)
		ps = append(ps, paire{b, t.Time})
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].t < ps[j].t })
	for _, p := range ps {
		l = append(l, p.b)
	}
	return l, ev.More
}

// -- chercher_messages

type entreeChercher struct {
	Texte   string `json:"texte" jsonschema:"le texte à chercher (2 caractères au moins)"`
	Service string `json:"service,omitempty" jsonschema:"un seul service : google, messenger, instagram, whatsapp, signal, x, telegram, bluesky, discord, reddit ou linkedin"`
	Limite  int    `json:"limite,omitempty" jsonschema:"nombre maximal de résultats (30 par défaut, 100 au plus)"`
}

type TrouveMCP struct {
	Conversation    string `json:"conversation"`
	Service         string `json:"service"`
	NomConversation string `json:"nom_conversation,omitempty"`
	ID              string `json:"id"`
	De              string `json:"de"`
	DeMoi           bool   `json:"de_moi"`
	Texte           string `json:"texte"`
	Heure           string `json:"heure"`
	temps           int64
}

type RechercheMCP struct {
	Resultats []TrouveMCP `json:"resultats"`
	Remarques []string    `json:"remarques,omitempty"`
}

func (m *ServiceMCP) outilChercher(ctx context.Context, _ *mcp.CallToolRequest, e entreeChercher) (*mcp.CallToolResult, RechercheMCP, error) {
	out := RechercheMCP{Resultats: []TrouveMCP{}}
	texte := strings.TrimSpace(e.Texte)
	if utf8.RuneCountInString(texte) < 2 {
		return nil, out, errors.New("texte : 2 caractères au moins")
	}
	svcs, err := m.services(e.Service)
	if err != nil {
		return nil, out, err
	}
	limite := e.Limite
	if limite <= 0 {
		limite = 30
	}
	limite = min(limite, 100)
	q := plierTexte(texte)
	m.rafraichirListes(ctx, svcs)

	var mu sync.Mutex
	vus := map[string]bool{}
	privees := m.privees()
	ajouter := func(svc string, noms map[string]string, l []msgPont) {
		mu.Lock()
		defer mu.Unlock()
		for _, x := range l {
			cle := svc + "\x00" + x.Conv + "\x00" + x.ID
			if vus[cle] || x.Conv == "" || privees[prefixeService[svc]+x.Conv] {
				continue
			}
			vus[cle] = true
			de := x.Sender
			if x.FromMe {
				de = "moi"
			}
			out.Resultats = append(out.Resultats, TrouveMCP{
				Conversation: prefixeService[svc] + x.Conv, Service: svc, NomConversation: noms[x.Conv],
				ID: x.ID, De: de, DeMoi: x.FromMe, Texte: x.Text, Heure: heure(x.Time), temps: x.Time,
			})
		}
	}
	var wg sync.WaitGroup
	for _, svc := range svcs {
		p := m.srv.ponts[svc]
		noms := map[string]string{}
		for _, c := range p.cache.convsGardees() {
			noms[c.ID] = c.Name
		}
		ajouter(svc, noms, p.cache.chercherGarde(q))
		// Google : son pont n'a pas de recherche ; les autres cherchent dans leur base.
		if svc == "google" || !m.etatDe(p).pret {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := m.demanderPont(ctx, p, "search", texte,
				map[string]any{"cmd": "search_messages", "text": texte, "limit": limite}, mcpDelaiRecherche)
			if err != nil || r.erreur != "" {
				mu.Lock()
				out.Remarques = append(out.Remarques, svc+" : recherche du service sans réponse ; seulement la copie du serveur")
				mu.Unlock()
				return
			}
			var ev struct {
				Items []msgPont `json:"items"`
			}
			_ = json.Unmarshal(r.ligne, &ev)
			ajouter(svc, noms, ev.Items)
		}()
	}
	wg.Wait()
	sort.SliceStable(out.Resultats, func(i, j int) bool { return out.Resultats[i].temps > out.Resultats[j].temps })
	if len(out.Resultats) > limite {
		out.Resultats = out.Resultats[:limite]
	}
	return nil, out, nil
}

// -- etat_services

type EtatServiceMCP struct {
	Service       string `json:"service"`
	Etat          string `json:"etat" jsonschema:"ready (prêt), connecting, reconnecting, pairing, unpaired (non associé)…"`
	Pret          bool   `json:"pret"`
	Compte        string `json:"compte,omitempty"`
	Conversations int    `json:"conversations"`
	NonLues       int    `json:"non_lues"`
	EnCopie       int    `json:"conversations_en_copie" jsonschema:"conversations dont le serveur garde les derniers messages"`
}

type EtatsMCP struct {
	Services     []EtatServiceMCP `json:"services"`
	NonOfferts   []string         `json:"non_offerts,omitempty"`
	EnvoiPermis  bool             `json:"envoi_permis"`
	HeureServeur string           `json:"heure_serveur"`
}

func (m *ServiceMCP) outilEtat(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, EtatsMCP, error) {
	out := EtatsMCP{Services: []EtatServiceMCP{}, HeureServeur: time.Now().Format(time.RFC3339)}
	m.mu.Lock()
	out.EnvoiPermis = m.cfg.Envoi
	m.mu.Unlock()
	m.rafraichirListes(ctx, m.srv.servicesTries())
	marques := m.marquesDe()
	privees := m.privees()
	for _, svc := range ordreServices {
		p := m.srv.ponts[svc]
		if p == nil {
			out.NonOfferts = append(out.NonOfferts, svc)
			continue
		}
		e := m.etatDe(p)
		x := EtatServiceMCP{Service: svc, Etat: e.etat, Pret: e.pret, Compte: e.compte, EnCopie: p.cache.nbCopies()}
		for _, c := range p.cache.convsGardees() {
			if c.Archived || contient(marques[prefixeService[svc]+c.ID], "archives") || privees[prefixeService[svc]+c.ID] {
				continue
			}
			x.Conversations++
			if c.Unread {
				x.NonLues++
			}
		}
		out.Services = append(out.Services, x)
	}
	return nil, out, nil
}

// -- envoyer_message

type entreeEnvoyer struct {
	Conversation string `json:"conversation" jsonschema:"l'id de la conversation, tel que rendu par lister_conversations"`
	Texte        string `json:"texte" jsonschema:"le texte exact approuvé par l'utilisateur (4000 caractères au plus)"`
}

type EnvoiMCP struct {
	Envoye       bool   `json:"envoye"`
	Conversation string `json:"conversation"`
	Nom          string `json:"nom,omitempty"`
	Heure        string `json:"heure"`
}

func (m *ServiceMCP) outilEnvoyer(ctx context.Context, _ *mcp.CallToolRequest, e entreeEnvoyer) (*mcp.CallToolResult, EnvoiMCP, error) {
	out := EnvoiMCP{Conversation: e.Conversation}
	m.mu.Lock()
	permis := m.cfg.Envoi
	m.mu.Unlock()
	if !permis {
		return nil, out, errors.New("l'envoi de messages n'est pas permis (Réglages > Serveur Jasette > Permettre à Claude d'envoyer des messages)")
	}
	texte := strings.TrimSpace(e.Texte)
	if texte == "" {
		return nil, out, errors.New("texte vide")
	}
	if utf8.RuneCountInString(texte) > mcpTexteMax {
		return nil, out, fmt.Errorf("texte trop long (%d caractères au plus)", mcpTexteMax)
	}
	svc, brut := svcDeId(strings.TrimSpace(e.Conversation))
	p := m.srv.ponts[svc]
	if p == nil {
		return nil, out, fmt.Errorf("service non offert par ce serveur : %s", svc)
	}
	m.rafraichirListe(ctx, p)
	var conv *convPont
	for _, c := range p.cache.convsGardees() {
		if c.ID == brut && !m.privees()[prefixeService[svc]+brut] {
			conv = &c
			break
		}
	}
	if conv == nil {
		return nil, out, errors.New("conversation inconnue : seulement une conversation existante (lister_conversations)")
	}
	if conv.ReadOnly {
		return nil, out, errors.New("conversation en lecture seule")
	}
	if !m.etatDe(p).pret {
		return nil, out, fmt.Errorf("%s n'est pas prêt", svc)
	}
	if !m.envoisMin.permis("envoi") || !m.envoisHeur.permis("envoi") {
		return nil, out, errors.New("trop d'envois : 5 par minute et 30 par heure au plus")
	}
	tmp := "mcp_" + hex.EncodeToString([]byte(aleatoireBrut(8)))
	r, err := m.demanderPont(ctx, p, "sent", tmp,
		map[string]any{"cmd": "send", "conv": brut, "text": texte, "tmp_id": tmp, "auto": true}, mcpDelaiEnvoi)
	if err != nil {
		return nil, out, err
	}
	var ev struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(r.ligne, &ev)
	if !ev.OK {
		if r.erreur != "" {
			return nil, out, fmt.Errorf("le service a refusé l'envoi : %s", r.erreur)
		}
		return nil, out, errors.New("le service a refusé l'envoi")
	}
	out.Envoye, out.Nom, out.Heure = true, conv.Name, time.Now().Format(time.RFC3339)
	return nil, out, nil
}

// ---- La trame `mcp` des clients (PROTOCOLE.md, §9.3) ----

func (c *Client) trameMCP(id int64, action string, envoi *bool) {
	m := c.srv.mcp
	r := map[string]any{}
	cfg, err := actionMCP(action, envoi)
	if err == nil {
		m.appliquer(cfg)
		if action != "etat" {
			logf("mcp : « %s » demandé par l'appareil %q", action, c.nom)
		}
	}
	for k, v := range m.etat() {
		r[k] = v
	}
	if err != nil {
		r["erreur"] = err.Error()
	}
	r["id"] = id
	c.envoyerA(map[string]any{"mcp": r})
}
