// Jasette — un pont factice, pour essayer les clients (l'app iOS) contre un
// jasette-serveur local SANS aucun compte réel. Il parle comme les vrais ponts
// (une ligne JSON par commande et par événement) et invente quelques
// conversations, messages, avatars et une photo.
//
//	JASETTE_PONT=…/pont-factice JASETTE_META=…/pont-factice \
//	JASETTE_DATA=…/donnees jasette-serveur --port 47900 --ecoute 127.0.0.1 \
//	    --services google,messenger,instagram
//
// Service : JASETTE_META_SERVICE (messenger, instagram), sinon google.
// Rien ne sort de la machine : « send » répond lui-même, comme un écho.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type conv struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Preview string   `json:"preview"`
	FromMe  bool     `json:"from_me"`
	Time    int64    `json:"time"`
	Unread  bool     `json:"unread"`
	Group   bool     `json:"group"`
	Color   string   `json:"color"`
	Members []string `json:"members"`
}

type piece struct {
	MediaID string `json:"media_id"`
	Key     []byte `json:"key"`
	Mime    string `json:"mime"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	W       int64  `json:"w"`
	H       int64  `json:"h"`
}

type message struct {
	ID          string  `json:"id"`
	Conv        string  `json:"conv"`
	Text        string  `json:"text"`
	FromMe      bool    `json:"from_me"`
	Sender      string  `json:"sender"`
	Time        int64   `json:"time"`
	Status      string  `json:"status"`
	Kind        string  `json:"kind"`
	TmpID       string  `json:"tmp_id,omitempty"`
	Attachments []piece `json:"attachments,omitempty"`
}

type cmd struct {
	Cmd     string `json:"cmd"`
	Conv    string `json:"conv"`
	Text    string `json:"text"`
	TmpID   string `json:"tmp_id"`
	Before  string `json:"before"`
	MediaID string `json:"media_id"`
}

var (
	mu       sync.Mutex
	sortie   = json.NewEncoder(os.Stdout)
	convs    []*conv
	fils     = map[string][]message{}
	service  = "google"
	medias   string
	compteur int
)

func emit(ev string, f map[string]any) {
	if f == nil {
		f = map[string]any{}
	}
	f["ev"] = ev
	_ = sortie.Encode(f)
}

func maintenant() int64 { return time.Now().UnixMilli() }

func main() {
	if s := os.Getenv("JASETTE_META_SERVICE"); s != "" {
		service = s
	}
	medias = os.Getenv("JASETTE_MEDIA")
	if medias == "" {
		medias = filepath.Join(os.Getenv("JASETTE_DATA"), "media")
	}
	_ = os.MkdirAll(filepath.Join(medias, "avatars"), 0o700)
	_ = os.MkdirAll(filepath.Join(medias, "pieces"), 0o700)
	inventer()

	mu.Lock()
	emit("hello", map[string]any{"version": "factice"})
	emit("state", map[string]any{"state": "connecting"})
	emit("account", map[string]any{"name": "Compte d'essai (" + service + ")", "email": "essai@example.invalid"})
	emit("state", map[string]any{"state": "ready"})
	envoyerListe()
	for i, c := range convs {
		if p := avatar(c.ID, i); p != "" {
			emit("avatar", map[string]any{"conv": c.ID, "path": p})
		}
	}
	mu.Unlock()

	lecteur := bufio.NewScanner(os.Stdin)
	lecteur.Buffer(make([]byte, 1<<20), 16<<20)
	for lecteur.Scan() {
		var c cmd
		if json.Unmarshal(lecteur.Bytes(), &c) != nil {
			continue
		}
		mu.Lock()
		traiter(c)
		mu.Unlock()
	}
}

func envoyerListe() {
	emit("conversations", map[string]any{"items": convs})
}

func traiter(c cmd) {
	switch c.Cmd {
	case "conversations":
		envoyerListe()
	case "messages":
		if c.Before != "" {
			emit("messages", map[string]any{"conv": c.Conv, "items": []message{}, "older": true, "more": false})
			return
		}
		emit("messages", map[string]any{"conv": c.Conv, "items": fils[c.Conv], "older": false, "more": false})
	case "read":
		for _, v := range convs {
			if v.ID == c.Conv && v.Unread {
				v.Unread = false
				emit("conversation", map[string]any{"item": v})
			}
		}
	case "send":
		compteur++
		m := message{ID: fmt.Sprintf("%s-envoi-%d", service, compteur), Conv: c.Conv, Text: c.Text, FromMe: true,
			Time: maintenant(), Status: "SENT", Kind: service, TmpID: c.TmpID}
		fils[c.Conv] = append(fils[c.Conv], m)
		emit("message", map[string]any{"item": m})
		emit("sent", map[string]any{"conv": c.Conv, "tmp_id": c.TmpID, "ok": true})
		for _, v := range convs {
			if v.ID == c.Conv {
				v.Preview, v.FromMe, v.Time = c.Text, true, m.Time
				emit("conversation", map[string]any{"item": v})
			}
		}
	case "download":
		p := filepath.Join(medias, "pieces", "photo-"+service+".jpg")
		if err := photo(p); err != nil {
			emit("media", map[string]any{"media_id": c.MediaID, "error": err.Error()})
			return
		}
		emit("media", map[string]any{"media_id": c.MediaID, "path": p})
	}
}

// inventer : quelques conversations par service, toutes fictives.
func inventer() {
	t := maintenant()
	min := int64(60_000)
	ajouter := func(c *conv, msgs ...message) {
		convs = append(convs, c)
		for i := range msgs {
			msgs[i].Conv = c.ID
			msgs[i].ID = fmt.Sprintf("%s-%s-%d", service, c.ID, i)
			msgs[i].Status = "READ"
			msgs[i].Kind = service
		}
		fils[c.ID] = msgs
		if n := len(msgs); n > 0 {
			c.Time, c.Preview, c.FromMe = msgs[n-1].Time, msgs[n-1].Text, msgs[n-1].FromMe
			if c.Preview == "" {
				c.Preview = "📷 Photo"
			}
		}
	}
	img := piece{MediaID: "photo-1", Key: []byte("essai"), Mime: "image/jpeg", Name: "photo.jpg", W: 800, H: 600}
	switch service {
	case "google":
		ajouter(&conv{ID: "101", Name: "Camille Tremblay", Color: "#e67e22", Unread: true},
			message{Text: "Salut ! Tu viens toujours samedi ?", Sender: "Camille Tremblay", Time: t - 26*60*min},
			message{Text: "Oui, j'apporte le dessert 🍰", FromMe: true, Time: t - 25*60*min},
			message{Text: "Parfait. Regarde la vue d'ici :", Sender: "Camille Tremblay", Time: t - 12*min},
			message{Sender: "Camille Tremblay", Time: t - 11*min, Attachments: []piece{img}},
			message{Text: "On se rejoint à 18 h ?", Sender: "Camille Tremblay", Time: t - 10*min})
		ajouter(&conv{ID: "102", Name: "Équipe de soccer", Group: true, Color: "#16a34a", Members: []string{"Sam", "Noé", "Moi"}},
			message{Text: "Match déplacé à 19 h", Sender: "Sam Bouchard", Time: t - 3*60*min},
			message{Text: "Noté !", Sender: "Noé Pelletier", Time: t - 2*60*min},
			message{Text: "Je serai là", FromMe: true, Time: t - 90*min})
		ajouter(&conv{ID: "103", Name: "Pharmacie du coin", Color: "#0a7fc4"},
			message{Text: "Votre commande est prête.", Sender: "Pharmacie du coin", Time: t - 3*24*60*min})
	case "messenger":
		ajouter(&conv{ID: "201", Name: "Alex Gagnon", Color: "#8b5cf6", Unread: true},
			message{Text: "As-tu vu le nouveau film ?", Sender: "Alex Gagnon", Time: t - 40*min},
			message{Text: "Pas encore, ce week-end peut-être", FromMe: true, Time: t - 35*min},
			message{Text: "Il est super, tu vas aimer 🎬", Sender: "Alex Gagnon", Time: t - 5*min})
		ajouter(&conv{ID: "202", Name: "Souper du vendredi", Group: true, Color: "#db2f74"},
			message{Text: "Qui réserve le resto ?", Sender: "Julie Morin", Time: t - 30*60*min},
			message{Text: "Je m'en occupe", FromMe: true, Time: t - 29*60*min})
	case "instagram":
		ajouter(&conv{ID: "301", Name: "Léa Roy", Color: "#e0552b"},
			message{Text: "Trop belle ta photo !", Sender: "Léa Roy", Time: t - 2*24*60*min},
			message{Text: "Merci 😊", FromMe: true, Time: t - 2*24*60*min + 5*min})
	}
}

// avatar : un disque dégradé, gardé sous <médias>/avatars/.
func avatar(id string, i int) string {
	p := filepath.Join(medias, "avatars", fmt.Sprintf("%s-%s.png", service, id))
	if _, err := os.Stat(p); err == nil {
		return p
	}
	teintes := [][3]uint8{{240, 138, 108}, {47, 107, 255}, {22, 163, 74}, {139, 92, 246}, {219, 47, 116}}
	a := teintes[i%len(teintes)]
	b := teintes[(i+2)%len(teintes)]
	const n = 160
	im := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			f := float64(x+y) / float64(2*n)
			im.Set(x, y, color.RGBA{mix(a[0], b[0], f), mix(a[1], b[1], f), mix(a[2], b[2], f), 255})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	if png.Encode(f, im) != nil {
		return ""
	}
	return p
}

// photo : un paysage de synthèse (ciel, soleil, collines), 800 × 600.
func photo(p string) error {
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	const w, h = 800, 600
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			f := float64(y) / h
			c := color.RGBA{mix(255, 120, f), mix(170, 80, f), mix(120, 160, f), 255}
			dx, dy := float64(x-560), float64(y-260)
			if dx*dx+dy*dy < 60*60 {
				c = color.RGBA{255, 230, 160, 255}
			}
			colline := 420 + 40*math.Sin(float64(x)/90)
			if float64(y) > colline {
				c = color.RGBA{30, mix(70, 40, f), 60, 255}
			}
			im.Set(x, y, c)
		}
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, im, &jpeg.Options{Quality: 85})
}

func mix(a, b uint8, f float64) uint8 { return uint8(float64(a)*(1-f) + float64(b)*f) }
