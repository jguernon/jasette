package main

// Le flux de présence de Meta (« Actif maintenant », « Actif il y a 12 min »),
// commun à Messenger et à Instagram. Ce fichier est copié tel quel dans
// pont-meta et pont-ig : le modifier partout à la fois.
//
// C'est la voie qu'emploient aujourd'hui les sites facebook.com et
// instagram.com (modules PresenceUnifiedClient, observés en novembre 2026) :
// un flux « PresenceUnifiedJSON » sur le socket DGW « streamcontroller » de la
// passerelle (wss://gateway.facebook.com ou gateway.instagram.com), à côté du
// socket principal que messagix ou instameow ouvrent déjà.
//
//   - Ouverture du flux : {"x-dgw-app-XRSS-method":"PresenceUnifiedJSON",…},
//     puis une trame de données (Thrift RequestStreamBody) :
//     {"appFamily":1|3,"pollingMode":1,"appId":"…","publishEncoding":2}.
//     Le site y ajoute « presenceReportingRequest » pour annoncer sa propre
//     disponibilité ; le pont ne le fait pas : il écoute sans se montrer actif.
//   - Le serveur publie ensuite (StreamResponse > ResponseData) :
//     {"publishType":1 (tout l'état) | 2 (des changements),
//     "presenceUpdates":[{"userId":"<id de messagerie>","presenceStatus":2|0|null,
//     "lastActiveTimeSeconds":<s>|null}]}. userId « 0 » : à ignorer.
//   - Pour des personnes hors de sa liste d'amis, le site demande leur
//     présence par un amendement {"payload":{"additionalContacts":
//     {"additionalContacts":["<id>",…]}}}, répété toutes les 2,5 minutes.
//     Le pont fait de même pour ses conversations individuelles récentes.
//
// Prudence : une seule connexion à la fois ; après une coupure, nouvel essai
// avec une attente qui double (5 s, 10 s… 5 min) ; après cinq échecs de suite
// (flux refusé, jamais démarré, ou fermé en moins d'une minute), pause de
// 30 minutes. Session refusée (401) : le flux s'arrête jusqu'à la prochaine
// connexion du pont. Rien ne tourne si le réglage est désactivé.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"go.mau.fi/mautrix-meta/pkg/instameow/thrift"
	"go.mau.fi/mautrix-meta/pkg/instameow/thrift/requeststream"
	"go.mau.fi/mautrix-meta/pkg/messagix/dgw"
)

const (
	fluxRafraichir = 150 * time.Second // le site redemande ses contacts toutes les 2,5 min
	fluxDemarrage  = 30 * time.Second  // délai pour que le serveur démarre le flux
	fluxStable     = time.Minute       // en deçà, une coupure compte comme un échec
	fluxEchecsMax  = 5
	fluxPause      = 30 * time.Minute
	fluxAttenteMax = 5 * time.Minute
	fluxPasPret    = 30 * time.Second // le pont n'est pas encore connecté
	fluxContacts   = 100              // au plus autant d'ids par amendement
)

// Une ligne de presenceUpdates.
type majPresence struct {
	ID       int64
	Actif    bool
	Derniere int64 // ms ; 0 = inconnue
}

// idSouple : un id donné en texte ou en nombre.
type idSouple string

func (i *idSouple) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*i = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*i = idSouple(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*i = idSouple(n.String())
	return nil
}

// parsePresence lit une publication du flux. complet : tout l'état (publishType 1).
func parsePresence(b []byte) (maj []majPresence, complet bool, err error) {
	var pub struct {
		PublishType     int `json:"publishType"`
		PresenceUpdates []struct {
			UserID                idSouple     `json:"userId"`
			PresenceStatus        *int         `json:"presenceStatus"`
			LastActiveTimeSeconds *json.Number `json:"lastActiveTimeSeconds"`
		} `json:"presenceUpdates"`
	}
	if err = json.Unmarshal(b, &pub); err != nil {
		return nil, false, err
	}
	for _, u := range pub.PresenceUpdates {
		id, e := strconv.ParseInt(string(u.UserID), 10, 64)
		if e != nil || id <= 0 {
			continue
		}
		m := majPresence{ID: id, Actif: u.PresenceStatus != nil && *u.PresenceStatus == 2}
		if u.LastActiveTimeSeconds != nil {
			if f, e := u.LastActiveTimeSeconds.Float64(); e == nil && f > 0 {
				m.Derniere = int64(f * 1000)
			}
		}
		maj = append(maj, m)
	}
	return maj, pub.PublishType == 1, nil
}

// Les paramètres d'ouverture du flux, et le corps de la première trame.
func fluxOuverture(referer string, appFamily int, appID string) (params, corps []byte) {
	params, _ = json.Marshal(map[string]string{
		"x-dgw-app-XRSS-method":       "PresenceUnifiedJSON",
		"x-dgw-app-xrs-body":          "true",
		"x-dgw-app-XRS-Accept-Ack":    "RSAck",
		"x-dgw-app-XRSS-http_referer": referer,
	})
	corps, _ = json.Marshal(map[string]any{
		"appFamily": appFamily, "pollingMode": 1, "appId": appID, "publishEncoding": 2,
	})
	return params, corps
}

// L'amendement qui demande la présence de ces personnes.
func fluxAmendement(ids []int64) []byte {
	l := make([]string, 0, len(ids))
	for _, id := range ids {
		l = append(l, strconv.FormatInt(id, 10))
	}
	b, _ := json.Marshal(map[string]any{"payload": map[string]any{
		"additionalContacts": map[string]any{"additionalContacts": l},
	}})
	return b
}

// fluxConfig : de quoi ouvrir le socket, tiré de l'état du client connecté.
type fluxConfig struct {
	WSURL, Origin, Referer string
	AppID, UserID          string
	DeviceID               string
	Facebook               bool // authentification « 1:0 » (Facebook) ou « 6:0 » (Instagram)
	AppFamily              int  // 1 Facebook, 3 Instagram
	Cookies                func() string
	Dial                   websocket.DialOptions
}

type fluxPresence struct {
	nom string // « Messenger », « Instagram »
	// config : nil, nil tant que le pont n'est pas prêt.
	config func() (*fluxConfig, error)
	// contacts : les personnes des conversations individuelles récentes.
	contacts func() []int64
	// recu : applique une publication ; rend combien d'ids ont une conversation.
	recu func(maj []majPresence, complet bool) int

	mu      sync.Mutex
	annule  context.CancelFunc
	tourne  bool
	premier bool // première présence déjà notée au journal

	// Compteurs pour le journal (au plus une ligne par heure).
	nPub, nIDs, nRattaches atomic.Int64
	depuis                 time.Time
}

// demarrer lance le flux s'il ne tourne pas déjà.
func (f *fluxPresence) demarrer(parent context.Context) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tourne {
		return
	}
	ctx, annule := context.WithCancel(parent)
	f.annule, f.tourne = annule, true
	if f.depuis.IsZero() {
		f.depuis = time.Now()
	}
	go func() {
		f.boucle(ctx)
		// Arrêté de lui-même (session refusée) : un prochain demarrer relance.
		f.mu.Lock()
		if ctx.Err() == nil {
			f.annule, f.tourne = nil, false
		}
		f.mu.Unlock()
		annule()
	}()
}

// arreter ferme le flux (réglage désactivé, pont déconnecté).
func (f *fluxPresence) arreter() {
	if f == nil {
		return
	}
	f.mu.Lock()
	annule := f.annule
	f.annule, f.tourne = nil, false
	f.mu.Unlock()
	if annule != nil {
		annule()
	}
}

func attendre(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (f *fluxPresence) boucle(ctx context.Context) {
	echecs := 0
	annonce := false
	for ctx.Err() == nil {
		cfg, err := f.config()
		if err != nil {
			log.Warn().Err(err).Msgf("présence %s : configuration du flux", f.nom)
		}
		if cfg == nil {
			if !attendre(ctx, fluxPasPret) {
				return
			}
			continue
		}
		if !annonce {
			annonce = true
			log.Info().Str("voie", "PresenceUnifiedJSON").Str("socket", cfg.WSURL).Msgf("présence %s : ouverture du flux", f.nom)
		}
		debut := time.Now()
		demarre, err := f.session(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		if dgw.IsUnauthorized(err) {
			log.Warn().Err(err).Msgf("présence %s : session refusée, flux arrêté jusqu'à la prochaine connexion", f.nom)
			return
		}
		if demarre && time.Since(debut) >= fluxStable {
			echecs = 0
		} else {
			echecs++
		}
		attente := min(5*time.Second<<echecs, fluxAttenteMax)
		if echecs >= fluxEchecsMax {
			log.Warn().Err(err).Msgf("présence %s indisponible : nouvel essai dans 30 min", f.nom)
			attente, echecs = fluxPause, 0
		} else {
			log.Info().Err(err).Int("échecs", echecs).Bool("avait_démarré", demarre).Dur("attente", attente).Msgf("présence %s : flux coupé", f.nom)
		}
		if !attendre(ctx, attente) {
			return
		}
	}
}

// session : une connexion du socket ; rend si le flux a démarré.
func (f *fluxPresence) session(ctx context.Context, cfg *fluxConfig) (bool, error) {
	sctx, fin := context.WithCancel(ctx)
	defer fin()
	var demarre atomic.Bool
	demarreCh := make(chan struct{})
	var once sync.Once
	var sock *dgw.Socket
	params, corps := fluxOuverture(cfg.Referer, cfg.AppFamily, cfg.AppID)

	onConnect := func(cctx context.Context, fatal func(error)) error {
		stream, err := sock.EstablishStream(cctx, dgw.StreamInit{
			Parameters: params,
			LogName:    "presence",
			FrameHandler: func(_ context.Context, b []byte) error {
				f.trame(b, fatal, func() {
					demarre.Store(true)
					once.Do(func() { close(demarreCh) })
				})
				return nil
			},
			OnClose: func() { fatal(errors.New("flux de présence fermé par le serveur")) },
		})
		if err != nil {
			return err
		}
		init, err := thrift.Marshal(&requeststream.Payload{RequestBody: &requeststream.RequestStreamBody{Body: corps}})
		if err != nil {
			return err
		}
		if err := stream.SendData(cctx, init); err != nil {
			return fmt.Errorf("demande de présence : %w", err)
		}
		go f.amendements(sctx, stream, demarreCh, fatal)
		return nil
	}
	sock = dgw.NewSocket(dgw.SocketOptions{
		GetCookies:     cfg.Cookies,
		OnConnect:      onConnect,
		Origin:         cfg.Origin,
		WSURL:          cfg.WSURL,
		DialOpts:       cfg.Dial,
		Log:            log.With().Str("socket", "présence").Logger(),
		Facebook:       cfg.Facebook,
		AppStreamGroup: "group1",
		AppID:          cfg.AppID,
		UserID:         cfg.UserID,
		DeviceID:       cfg.DeviceID,
	})
	err := sock.Connect(sctx)
	return demarre.Load(), err
}

// amendements : dès le démarrage, puis toutes les 2,5 min, les contacts voulus.
func (f *fluxPresence) amendements(ctx context.Context, stream *dgw.PersistentStream, demarre <-chan struct{}, fatal func(error)) {
	select {
	case <-ctx.Done():
		return
	case <-demarre:
	case <-time.After(fluxDemarrage):
		fatal(errors.New("le serveur n'a pas démarré le flux de présence"))
		return
	}
	var n int64
	for {
		ids := f.contacts()
		if len(ids) > fluxContacts {
			ids = ids[:fluxContacts]
		}
		if len(ids) > 0 {
			n++
			id := n
			b, err := thrift.Marshal(&requeststream.Payload{Amend: &requeststream.AmendStream{AmendmentID: &id, Amendment: fluxAmendement(ids)}})
			if err == nil {
				actx, annule := context.WithTimeout(ctx, 15*time.Second)
				err = stream.SendData(actx, b)
				annule()
			}
			if err != nil && ctx.Err() == nil {
				log.Debug().Err(err).Msgf("présence %s : demande des contacts", f.nom)
			} else if n == 1 {
				log.Info().Int("contacts", len(ids)).Msgf("présence %s : contacts demandés", f.nom)
			}
		}
		if !attendre(ctx, fluxRafraichir) {
			return
		}
	}
}

// trame : une trame du serveur sur le flux.
func (f *fluxPresence) trame(b []byte, fatal func(error), demarre func()) {
	var p requeststream.Payload
	if err := thrift.Unmarshal(b, &p); err != nil {
		log.Debug().Err(err).Msgf("présence %s : trame illisible", f.nom)
		return
	}
	if p.Response == nil {
		return
	}
	for _, d := range p.Response.Delta {
		switch {
		case d.FlowStatus != nil:
			if *d.FlowStatus == requeststream.FlowStatus_Started {
				log.Debug().Msgf("présence %s : flux démarré", f.nom)
				demarre()
			}
		case d.Termination != nil:
			t := d.Termination
			log.Warn().Str("raison", t.GetReason().String()).Str("message", t.GetMessage()).Msgf("présence %s : le serveur termine le flux", f.nom)
			fatal(fmt.Errorf("flux de présence terminé : %s", t.GetReason()))
		case d.Data != nil:
			f.publication(d.Data.Bytes)
		}
	}
}

func (f *fluxPresence) publication(b []byte) {
	maj, complet, err := parsePresence(b)
	if err != nil {
		log.Debug().Err(err).Int("octets", len(b)).Msgf("présence %s : publication illisible", f.nom)
		return
	}
	rattaches := f.recu(maj, complet)
	f.nPub.Add(1)
	f.nIDs.Add(int64(len(maj)))
	f.nRattaches.Add(int64(rattaches))
	f.mu.Lock()
	premier := !f.premier && len(maj) > 0
	if premier {
		f.premier = true
	}
	if f.depuis.IsZero() {
		f.depuis = time.Now()
	}
	bilan := time.Since(f.depuis) >= time.Hour
	if bilan {
		f.depuis = time.Now()
	}
	f.mu.Unlock()
	if premier {
		log.Info().Int("personnes", len(maj)).Int("rattachées", rattaches).Bool("complet", complet).Msgf("présence %s reçue (première fois)", f.nom)
	}
	if bilan {
		log.Info().Int64("publications", f.nPub.Swap(0)).Int64("personnes", f.nIDs.Swap(0)).Int64("rattachées", f.nRattaches.Swap(0)).Msgf("présence %s : bilan de l'heure", f.nom)
	}
}
