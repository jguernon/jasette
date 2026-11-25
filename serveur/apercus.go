// Les aperçus de liens (PROTOCOLE.md, §5.9) : le serveur va chercher la page
// d'une adresse vue dans un message, en lit les balises OpenGraph et Twitter
// card (titre, description, image, nom du site) et garde le résultat sur le
// disque. Ainsi, chaque appareil n'a pas à aller chercher la page lui-même.
//
// Garde-fous : 5 s par requête, 2 Mio de HTML lus au plus, 5 redirections,
// et jamais d'adresse privée ou locale (vérifiée à la connexion même, après
// la résolution du nom : un nom qui pointe vers 127.0.0.1 est refusé aussi).
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const (
	apercuDelai        = 5 * time.Second
	apercuHTMLMax      = 2 << 20 // HTML lu au plus
	apercuImageMax     = 8 << 20 // image téléchargée au plus
	apercuLargeur      = 640     // l'image est réduite à cette largeur
	apercuRedirections = 5
	apercuDureeOK      = 7 * 24 * time.Hour
	apercuDureeEchec   = 6 * time.Hour
	apercuSimultanes   = 4
	// User-Agent d'un navigateur courant : bien des sites ne donnent leurs
	// balises qu'à un navigateur.
	apercuUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// Apercu : ce qu'on montre sous le message. Image : chemin d'un fichier du
// dossier des médias (servi par `get`, §6.1), vide s'il n'y en a pas.
type Apercu struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Site        string `json:"site,omitempty"`
	Image       string `json:"image,omitempty"`
	// Pour le cache : quand, et pourquoi il n'y en a pas.
	Quand  int64  `json:"quand,omitempty"`
	Erreur string `json:"erreur,omitempty"`
}

var (
	errAdresseInterdite = errors.New("adresse privée ou locale refusée")
	apercusEnCours      = map[string]chan struct{}{}
	apercusMu           sync.Mutex
	apercusJeton        = make(chan struct{}, apercuSimultanes)
)

// ipInterdite : les adresses qu'un aperçu ne joint jamais (réseau local,
// boucle, lien local, réservées…).
func ipInterdite(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, p := range plagesInterdites {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

var plagesInterdites = func() []netip.Prefix {
	var l []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32",
		"64:ff9b::/96", "64:ff9b:1::/48", "2001:db8::/32", "fec0::/10", "::/128",
	} {
		l = append(l, netip.MustParsePrefix(s))
	}
	return l
}()

// clientApercus : un client HTTP qui refuse de se connecter à une adresse
// interdite (contrôle fait sur l'adresse réellement jointe, après DNS).
var clientApercus = func() *http.Client {
	dial := &net.Dialer{
		Timeout: apercuDelai,
		Control: func(_, adresse string, _ syscall.RawConn) error {
			hote, _, err := net.SplitHostPort(adresse)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(hote)
			if err != nil || ipInterdite(ip) {
				return errAdresseInterdite
			}
			return nil
		},
	}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dial.DialContext,
		TLSHandshakeTimeout:   apercuDelai,
		ResponseHeaderTimeout: apercuDelai,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   apercuDelai,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > apercuRedirections {
				return errors.New("trop de redirections")
			}
			return adresseAcceptable(req.URL)
		},
	}
}()

// adresseAcceptable : http(s), un nom d'hôte, pas d'identifiants, et pas
// d'adresse IP interdite écrite en clair (le reste est vérifié à la connexion).
func adresseAcceptable(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("seulement http et https")
	}
	if u.User != nil || u.Hostname() == "" {
		return errors.New("adresse refusée")
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || !strings.Contains(h, ".") && !strings.Contains(h, ":") {
		return errAdresseInterdite
	}
	if ip, err := netip.ParseAddr(h); err == nil && ipInterdite(ip) {
		return errAdresseInterdite
	}
	return nil
}

// normaliserURL : « www.exemple.com » devient « https://www.exemple.com ».
func normaliserURL(brute string) (*url.URL, error) {
	brute = strings.TrimSpace(brute)
	if len(brute) > 2048 {
		return nil, errors.New("adresse trop longue")
	}
	if !strings.Contains(brute, "://") {
		brute = "https://" + brute
	}
	u, err := url.Parse(brute)
	if err != nil {
		return nil, err
	}
	u.Fragment = ""
	return u, adresseAcceptable(u)
}

func apercusDir() string    { return filepath.Join(serveurDir(), "apercus") }
func imagesApercus() string { return filepath.Join(mediaDir(), "apercus") }

func cleApercu(u string) string {
	h := sha256.Sum256([]byte(u))
	return hex.EncodeToString(h[:16])
}

// apercuEnCache : l'aperçu gardé, s'il est encore frais.
func apercuEnCache(cle string) *Apercu {
	b, err := os.ReadFile(filepath.Join(apercusDir(), cle+".json"))
	if err != nil {
		return nil
	}
	var a Apercu
	if json.Unmarshal(b, &a) != nil {
		return nil
	}
	duree := apercuDureeOK
	if a.Erreur != "" {
		duree = apercuDureeEchec
	}
	if time.Since(time.UnixMilli(a.Quand)) > duree {
		return nil
	}
	if a.Image != "" {
		if _, err := os.Stat(a.Image); err != nil {
			return nil
		}
	}
	return &a
}

func garderApercu(cle string, a *Apercu) {
	_ = os.MkdirAll(apercusDir(), 0o700)
	if b, err := json.Marshal(a); err == nil {
		_ = ecrireSecret(filepath.Join(apercusDir(), cle+".json"), b)
	}
}

// apercuDe : l'aperçu d'une adresse, du cache ou du réseau. Deux demandes de
// la même adresse en même temps n'en font qu'une.
func apercuDe(brute string) *Apercu {
	u, err := normaliserURL(brute)
	if err != nil {
		return &Apercu{URL: brute, Erreur: err.Error()}
	}
	cle := cleApercu(u.String())
	for {
		if a := apercuEnCache(cle); a != nil {
			a.URL = brute
			return a
		}
		apercusMu.Lock()
		attente, encours := apercusEnCours[cle]
		if !encours {
			attente = make(chan struct{})
			apercusEnCours[cle] = attente
		}
		apercusMu.Unlock()
		if encours {
			<-attente
			continue
		}
		apercusJeton <- struct{}{}
		a := chercherApercu(u, cle)
		<-apercusJeton
		a.URL = brute
		a.Quand = time.Now().UnixMilli()
		garderApercu(cle, a)
		apercusMu.Lock()
		delete(apercusEnCours, cle)
		close(attente)
		apercusMu.Unlock()
		return a
	}
}

func requeteApercu(ctx context.Context, u string, accepte string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", apercuUA)
	req.Header.Set("Accept", accepte)
	req.Header.Set("Accept-Language", "fr-CA,fr;q=0.9,en;q=0.8")
	return clientApercus.Do(req)
}

// chercherApercu : la page, ses balises, puis l'image (réduite).
func chercherApercu(u *url.URL, cle string) *Apercu {
	ctx, annule := context.WithTimeout(context.Background(), apercuDelai)
	defer annule()
	rep, err := requeteApercu(ctx, u.String(), "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	if err != nil {
		return &Apercu{Erreur: resumerErreur(err)}
	}
	defer rep.Body.Close()
	if rep.StatusCode/100 != 2 {
		return &Apercu{Erreur: fmt.Sprintf("HTTP %d", rep.StatusCode)}
	}
	ct, _, _ := mime.ParseMediaType(rep.Header.Get("Content-Type"))
	if ct != "" && ct != "text/html" && ct != "application/xhtml+xml" {
		return &Apercu{Erreur: "pas une page : " + ct}
	}
	corps, err := io.ReadAll(io.LimitReader(rep.Body, apercuHTMLMax))
	if err != nil && len(corps) == 0 {
		return &Apercu{Erreur: resumerErreur(err)}
	}
	a := lireBalises(corps, rep.Header.Get("Content-Type"), rep.Request.URL)
	if a.Title == "" && a.Description == "" {
		return &Apercu{Erreur: "aucune balise"}
	}
	if a.Image != "" {
		a.Image = telechargerImage(a.Image, cle)
	}
	return a
}

func resumerErreur(err error) string {
	if errors.Is(err, errAdresseInterdite) {
		return errAdresseInterdite.Error()
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Timeout() {
		return "délai dépassé"
	}
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

// lireBalises : OpenGraph d'abord, puis Twitter card, puis <title> et la
// description ordinaire. Image : adresse absolue (relative à la page).
func lireBalises(corps []byte, typeContenu string, base *url.URL) *Apercu {
	var r io.Reader = bytes.NewReader(corps)
	if lr, err := charset.NewReader(r, typeContenu); err == nil {
		r = lr
	}
	z := html.NewTokenizer(r)
	m := map[string]string{}
	titre := ""
	dansTitre := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		nom, attrs := z.TagName()
		tag := string(nom)
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			if tag == "body" {
				// Les balises utiles sont dans <head> ; certains sites les
				// mettent plus loin, mais on s'arrête au premier <body> qui
				// suit des balises déjà trouvées.
				if len(m) > 0 {
					goto fini
				}
			}
			if tag == "title" && titre == "" {
				dansTitre = true
			}
			if tag != "meta" || !attrs {
				continue
			}
			var prop, contenu string
			for {
				k, v, encore := z.TagAttr()
				switch strings.ToLower(string(k)) {
				case "property", "name", "itemprop":
					if prop == "" {
						prop = strings.ToLower(strings.TrimSpace(string(v)))
					}
				case "content":
					contenu = strings.TrimSpace(string(v))
				}
				if !encore {
					break
				}
			}
			if prop != "" && contenu != "" {
				if _, deja := m[prop]; !deja {
					m[prop] = contenu
				}
			}
		case html.TextToken:
			if dansTitre {
				titre += string(z.Text())
			}
		case html.EndTagToken:
			if tag == "title" {
				dansTitre = false
			}
			if tag == "head" && len(m) > 0 {
				goto fini
			}
		}
	}
fini:
	premier := func(cles ...string) string {
		for _, k := range cles {
			if v := strings.TrimSpace(m[k]); v != "" {
				return v
			}
		}
		return ""
	}
	a := &Apercu{
		Title:       premier("og:title", "twitter:title"),
		Description: premier("og:description", "twitter:description", "description"),
		Site:        premier("og:site_name", "application-name"),
		Image:       premier("og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src", "image"),
	}
	if a.Title == "" {
		a.Title = strings.TrimSpace(strings.Join(strings.Fields(titre), " "))
	}
	if a.Site == "" && base != nil {
		a.Site = strings.TrimPrefix(base.Hostname(), "www.")
	}
	a.Title = couper(html.UnescapeString(a.Title), 300)
	a.Description = couper(html.UnescapeString(a.Description), 500)
	a.Site = couper(html.UnescapeString(a.Site), 100)
	if a.Image != "" && base != nil {
		if iu, err := base.Parse(a.Image); err == nil && (iu.Scheme == "https" || iu.Scheme == "http") {
			a.Image = iu.String()
		} else {
			a.Image = ""
		}
	}
	return a
}

func couper(s string, n int) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// telechargerImage : l'image de l'aperçu, réduite à 640 px de large, en JPEG,
// dans <médias>/apercus. Rend son chemin, ou "" si elle n'a pas pu être lue.
func telechargerImage(adresse, cle string) string {
	u, err := url.Parse(adresse)
	if err != nil || adresseAcceptable(u) != nil {
		return ""
	}
	ctx, annule := context.WithTimeout(context.Background(), apercuDelai)
	defer annule()
	rep, err := requeteApercu(ctx, u.String(), "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8")
	if err != nil {
		return ""
	}
	defer rep.Body.Close()
	if rep.StatusCode/100 != 2 {
		return ""
	}
	brut, err := io.ReadAll(io.LimitReader(rep.Body, apercuImageMax+1))
	if err != nil || len(brut) > apercuImageMax {
		return ""
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(brut))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 40_000_000 {
		return ""
	}
	img, _, err := image.Decode(bytes.NewReader(brut))
	if err != nil {
		return ""
	}
	img = reduire(img, apercuLargeur)
	var sortie bytes.Buffer
	if jpeg.Encode(&sortie, img, &jpeg.Options{Quality: 82}) != nil {
		return ""
	}
	dir := imagesApercus()
	_ = os.MkdirAll(dir, 0o700)
	chemin := filepath.Join(dir, cle+".jpg")
	if ecrireSecret(chemin, sortie.Bytes()) != nil {
		return ""
	}
	return chemin
}

// reduire : au plus `largeur` px de large (et 2 × largeur de haut), sur fond
// blanc (le JPEG n'a pas de transparence).
func reduire(img image.Image, largeur int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	k := 1.0
	if w > largeur {
		k = float64(largeur) / float64(w)
	}
	if float64(h)*k > float64(2*largeur) {
		k = float64(2*largeur) / float64(h)
	}
	nw, nh := max(1, int(float64(w)*k)), max(1, int(float64(h)*k))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// trameApercu : la réponse à {"apercu":{"id","url"}}, à ce client seul.
func (c *Client) trameApercu(id int64, adresse string) {
	a := apercuDe(adresse)
	r := map[string]any{"id": id, "url": adresse}
	if a.Erreur != "" {
		r["erreur"] = a.Erreur
	} else {
		r["title"] = a.Title
		r["description"] = a.Description
		r["site"] = a.Site
		if a.Image != "" {
			r["image"] = a.Image
		}
	}
	c.envoyerA(map[string]any{"apercu": r})
}

// cmdApercu : jasette-serveur --apercu URL (essai en ligne de commande).
func cmdApercu(adresse string) int {
	a := apercuDe(adresse)
	b, _ := json.MarshalIndent(a, "", "  ")
	fmt.Println(string(b))
	if a.Erreur != "" {
		return 1
	}
	return 0
}
