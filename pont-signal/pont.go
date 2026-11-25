package main

// La connexion à Signal (signalmeow) : liaison comme appareil lié (code QR),
// connexion des sockets, synchro des contacts et des groupes, et toutes les
// commandes de l'interface.

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-signal/pkg/libsignalgo"
	"go.mau.fi/mautrix-signal/pkg/signalmeow"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/protobuf/signalpb"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/store"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/types"
)

// Le nom de l'appareil lié, tel que le téléphone le montre.
const deviceName = "Jasette"

// Signal Desktop redemande un code QR toutes les 45 s (le serveur ferme le
// socket de liaison après 60 s) ; on fait pareil, au plus 20 fois.
const (
	qrRefresh   = 45 * time.Second
	qrMaxRounds = 20
)

type Pont struct {
	ctx context.Context
	db  *sql.DB
	sm  *store.Container

	mu     sync.Mutex
	cli    *signalmeow.Client
	me     uuid.UUID // mon ACI
	myName string

	convs    map[string]*convState
	contacts map[string]*contact
	loaded   map[string]int64 // fil -> plus vieux message montré à l'interface
	typing   map[string]*time.Timer

	listDirty     bool
	listCount     int
	archiveWanted bool
	ready         bool
	connecting    bool
	synced        bool // la synchro d'après connexion est lancée (une fois par connexion)
	pairCancel    context.CancelFunc
	pairGen       int
	connCancel    context.CancelFunc
	lastGroupSync time.Time
}

func newPont(ctx context.Context) *Pont {
	p := &Pont{
		ctx:       ctx,
		convs:     map[string]*convState{},
		contacts:  map[string]*contact{},
		loaded:    map[string]int64{},
		typing:    map[string]*time.Timer{},
		listCount: 50,
	}
	// Les journaux de libsignal (Rust) passent par le nôtre.
	signalmeow.SetLogger(log.With().Str("c", "libsignal").Logger())
	go p.listLoop()
	return p
}

func (p *Pont) client() *signalmeow.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cli
}

func (p *Pont) myACI() uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.me
}

// ---- Démarrage, liaison et connexion ----

// Démarre avec l'appareil gardé, sinon dit à l'interface qu'il faut le lier.
func (p *Pont) start() {
	dev := p.savedDevice()
	p.mu.Lock()
	pairing := p.pairCancel != nil
	p.mu.Unlock()
	if pairing {
		// Une liaison a déjà commencé (commande « pair » arrivée tout de suite).
		return
	}
	if dev == nil {
		emitState("unpaired")
		return
	}
	p.connect(dev, false)
}

// L'appareil lié gardé par signalmeow (le premier encore valide).
func (p *Pont) savedDevice() *store.Device {
	devs, err := p.sm.GetAllDevices(p.ctx)
	if err != nil {
		log.Err(err).Msg("appareils gardés")
	}
	for _, d := range devs {
		if d.IsDeviceLoggedIn() {
			return d
		}
	}
	return nil
}

// La liaison : un code QR à scanner dans Signal sur le téléphone
// (Paramètres > Appareils liés > Lier un nouvel appareil).
func (p *Pont) pair() {
	p.mu.Lock()
	if p.pairCancel != nil {
		// Déjà en cours : on recommence avec un nouveau code.
		p.pairCancel()
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.pairCancel = cancel
	p.pairGen++
	gen := p.pairGen
	p.mu.Unlock()
	defer func() {
		cancel()
		p.mu.Lock()
		if p.pairGen == gen {
			p.pairCancel = nil
		}
		p.mu.Unlock()
	}()

	p.disconnect()
	emitState("pairing")
	for round := 0; round < qrMaxRounds; round++ {
		provCtx, provCancel := context.WithCancel(ctx)
		// allowBackup=false : pas de transfert d'historique (Signal ne
		// l'offre qu'aux appareils qui savent lire ses sauvegardes).
		ch := signalmeow.PerformProvisioning(provCtx, p.sm, deviceName, false)
		resp, ok := <-ch
		if !ok || resp.Err != nil || resp.State != signalmeow.StateProvisioningURLReceived {
			provCancel()
			if ctx.Err() != nil {
				return
			}
			err := resp.Err
			if err == nil {
				err = errors.New("le serveur de Signal n'a pas donné de code")
			}
			log.Err(err).Msg("liaison : code QR")
			emitErr("pair", explainLink(err))
			emitState("unpaired")
			return
		}
		log.Info().Int("tour", round).Msg("code QR prêt")
		emit("qr", map[string]any{"code": resp.ProvisioningURL, "data": resp.ProvisioningURL})

		var done *signalmeow.ProvisioningResponse
		select {
		case r, ok := <-ch:
			if ok {
				done = &r
			}
		case <-time.After(qrRefresh):
		case <-ctx.Done():
		}
		provCancel()
		// Vider le canal : signalmeow le ferme quand il a fini.
		go func() {
			for range ch {
			}
		}()
		if ctx.Err() != nil {
			return
		}
		if done == nil {
			continue // code expiré : un nouveau
		}
		if done.Err != nil || done.State != signalmeow.StateProvisioningDataReceived || done.ProvisioningData == nil || done.ProvisioningData.ACI == uuid.Nil {
			err := done.Err
			if err == nil {
				err = errors.New("le téléphone n'a pas envoyé l'identité du compte")
			}
			log.Err(err).Msg("liaison")
			emitErr("pair", explainLink(err))
			emitState("unpaired")
			return
		}
		p.linked(done.ProvisioningData)
		return
	}
	emitErr("pair", errors.New("le code QR n'a pas été scanné à temps : recommence la liaison"))
	emitState("unpaired")
}

// La liaison a réussi : signalmeow a gardé l'appareil, on se connecte.
func (p *Pont) linked(data *store.DeviceData) {
	log.Info().Stringer("aci", data.ACI).Msg("appareil lié")
	var old string
	p.loadKV("account", &old)
	if old != data.ACI.String() {
		// Un autre compte qu'avant : on repart d'un cache vide, et on oublie
		// les anciens appareils gardés par signalmeow.
		p.forgetAccount()
		if devs, err := p.sm.GetAllDevices(p.ctx); err == nil {
			for _, d := range devs {
				if d.ACI != data.ACI {
					_ = p.sm.DeleteDevice(p.ctx, &d.DeviceData)
				}
			}
		}
	}
	p.saveKV("account", data.ACI.String())
	dev, err := p.sm.DeviceByACI(p.ctx, data.ACI)
	if err != nil || dev == nil {
		if err == nil {
			err = errors.New("appareil introuvable dans la base")
		}
		emitErr("pair", fmt.Errorf("liaison : %w", err))
		emitState("unpaired")
		return
	}
	emit("paired", nil)
	p.connect(dev, true)
}

// Les erreurs de liaison, en français (mêmes cas que le connecteur de mautrix-signal).
func explainLink(err error) error {
	var linkErr signalmeow.DeviceLinkError
	if errors.As(err, &linkErr) {
		switch linkErr.StatusCode {
		case http.StatusConflict:
			return errors.New("Signal refuse la liaison : un autre appareil du compte demande une fonction que Jasette n'a pas encore ; réessaie plus tard")
		case http.StatusLengthRequired:
			return errors.New("le compte Signal a déjà le maximum d'appareils liés : retires-en un dans l'application, puis réessaie")
		case http.StatusForbidden:
			return errors.New("code QR invalide ou déjà utilisé : recommence la liaison")
		case http.StatusTooManyRequests:
			return errors.New("Signal limite les liaisons : attends quelques minutes, puis réessaie")
		}
		return fmt.Errorf("Signal a refusé la liaison (HTTP %d)", linkErr.StatusCode)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("Signal ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("liaison Signal : %w", err)
}

// Ouvre les sockets de Signal et suit leur état.
func (p *Pont) connect(dev *store.Device, justLinked bool) {
	p.mu.Lock()
	if p.connecting {
		p.mu.Unlock()
		return
	}
	p.connecting = true
	ctx, cancel := context.WithCancel(p.ctx)
	if p.connCancel != nil {
		p.connCancel()
	}
	p.connCancel = cancel
	cli := signalmeow.NewClient(dev, log.With().Str("c", "signalmeow").Logger(), func(evt eventAny) bool {
		return p.onEvent(evt)
	})
	p.cli = cli
	p.me = dev.ACI
	p.synced = false
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.connecting = false
		p.mu.Unlock()
	}()

	// Les contacts du téléphone (noms du carnet d'adresses, photos) : à la
	// liaison, puis tous les trois jours (comme le connecteur de mautrix).
	var lastSync time.Time
	p.loadKV("contacts_sync", &lastSync)
	cli.SyncContactsOnConnect = justLinked || time.Since(lastSync) > 72*time.Hour
	if cli.SyncContactsOnConnect {
		p.saveKV("contacts_sync", time.Now())
	}

	emitState("connecting")
	p.emitAccount()
	for essai := 0; ; essai++ {
		status, err := cli.StartReceiveLoops(ctx)
		if err == nil {
			go p.statusLoop(ctx, cli, status)
			break
		}
		log.Err(err).Int("essai", essai).Msg("connexion")
		emitState("reconnecting")
		select {
		case <-time.After(min(time.Duration(2<<essai)*time.Second, 150*time.Second)):
		case <-ctx.Done():
			return
		}
	}
}

// Le suivi des sockets : prêt, coupé, déconnecté par le téléphone.
func (p *Pont) statusLoop(ctx context.Context, cli *signalmeow.Client, ch chan signalmeow.SignalConnectionStatus) {
	for {
		var st signalmeow.SignalConnectionStatus
		var ok bool
		select {
		case st, ok = <-ch:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}
		if p.client() != cli {
			return
		}
		switch st.Event {
		case signalmeow.SignalConnectionEventConnected:
			log.Info().Msg("connecté à Signal")
			p.mu.Lock()
			p.ready = true
			first := !p.synced
			p.synced = true
			p.mu.Unlock()
			emitState("ready")
			p.emitList()
			if first {
				go p.afterConnect(ctx, cli)
			}
		case signalmeow.SignalConnectionEventDisconnected:
			log.Warn().Err(st.Err).Msg("socket Signal coupé")
			p.mu.Lock()
			p.ready = false
			p.mu.Unlock()
			emitState("reconnecting")
		case signalmeow.SignalConnectionEventLoggedOut:
			log.Warn().Err(st.Err).Msg("Signal : appareil délié")
			p.loggedOut()
			return
		case signalmeow.SignalConnectionEventError:
			log.Warn().Err(st.Err).Msg("Signal : erreur de connexion")
			emitState("reconnecting")
		case signalmeow.SignalConnectionEventFatalError:
			log.Err(st.Err).Msg("Signal : erreur fatale")
			if st.Err != nil {
				emitErr("connect", fmt.Errorf("Signal : %w", st.Err))
			}
			emitState("reconnecting")
		case signalmeow.SignalConnectionCleanShutdown:
			log.Info().Msg("Signal : sockets fermés")
		}
	}
}

// Le téléphone a délié Jasette (ou le compte a été réinscrit) : il faut
// recommencer la liaison. Le cache reste (même compte probable).
func (p *Pont) loggedOut() {
	p.mu.Lock()
	cli := p.cli
	p.cli = nil
	p.ready = false
	if p.connCancel != nil {
		p.connCancel()
		p.connCancel = nil
	}
	p.mu.Unlock()
	if cli != nil {
		if err := cli.ClearKeysAndDisconnect(context.WithoutCancel(p.ctx)); err != nil {
			log.Warn().Err(err).Msg("effacement des clés")
		}
	}
	emitErr("listen", errors.New("Signal a délié Jasette : refais la liaison avec le code QR"))
	emitState("unpaired")
}

// Une fois connecté : mon profil, le stockage (contacts et groupes), les groupes.
func (p *Pont) afterConnect(ctx context.Context, cli *signalmeow.Client) {
	go p.refreshMe(ctx, cli)
	if cli.Store.MasterKey != nil {
		// Sinon, la clé arrive du téléphone (demandée par signalmeow) et
		// signalmeow lance alors lui-même la synchro.
		cli.StorageSync(ctx)
	}
	p.refreshKnownContacts(ctx, cli)
	// Les groupes : tout de suite, puis encore un peu plus tard (la synchro du
	// stockage arrive après la clé envoyée par le téléphone), puis de temps en temps.
	for _, wait := range []time.Duration{0, 20 * time.Second, time.Minute, 3 * time.Minute} {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
		if p.client() != cli {
			return
		}
		p.syncGroups(ctx, cli, wait > 0)
	}
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			p.syncGroups(ctx, cli, false)
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pont) emitAccount() {
	p.mu.Lock()
	cli, name := p.cli, p.myName
	p.mu.Unlock()
	if cli == nil {
		return
	}
	emit("account", map[string]any{"service": serviceName(), "name": cmp.Or(name, cli.Store.Number, "Signal"), "id": cli.Store.ACI.String(), "email": ""})
}

// Mon nom et ma photo (mon propre profil Signal).
func (p *Pont) refreshMe(ctx context.Context, cli *signalmeow.Client) {
	rctx, annule := context.WithTimeout(ctx, 30*time.Second)
	defer annule()
	me, err := cli.ContactByACI(rctx, cli.Store.ACI)
	if err != nil || me == nil {
		log.Debug().Err(err).Msg("mon profil")
		p.emitAccount()
		return
	}
	p.mu.Lock()
	p.myName = cmp.Or(me.Profile.Name, me.E164, cli.Store.Number)
	p.mu.Unlock()
	p.updateContact(me)
	p.emitAccount()
	p.myAvatarDownload()
}

// Les noms des contacts déjà connus de signalmeow (carnet, profils).
func (p *Pont) refreshKnownContacts(ctx context.Context, cli *signalmeow.Client) {
	all, err := cli.Store.RecipientStore.LoadAllContacts(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("contacts")
		return
	}
	for _, r := range all {
		p.updateContact(r)
	}
	p.markDirty()
}

// À appeler sans p.mu : garde le nom et la photo d'un contact de signalmeow.
func (p *Pont) updateContact(r *types.Recipient) {
	if r == nil || r.ACI == uuid.Nil {
		return
	}
	id := r.ACI.String()
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.contacts[id]
	if c == nil {
		c = &contact{}
		p.contacts[id] = c
	}
	n := contactNameOf(r)
	changed := false
	set := func(dst *string, v string) {
		if v != "" && *dst != v {
			*dst, changed = v, true
		}
	}
	set(&c.Name, n)
	set(&c.Number, r.E164)
	if r.Profile.AvatarPath != "" && r.Profile.AvatarPath != "clear" {
		set(&c.Avatar, r.Profile.AvatarPath)
	} else if r.Profile.AvatarPath == "clear" && c.Avatar != "" {
		c.Avatar, changed = "", true
	}
	set(&c.AvatarHash, r.ContactAvatar.Hash)
	if changed {
		p.saveContact(id, c)
		p.listDirty = true
	}
}

// Le nom à montrer : surnom, carnet du téléphone, profil, numéro (comme mautrix-signal).
func contactNameOf(r *types.Recipient) string {
	return strings.TrimSpace(cmp.Or(r.Nickname, r.ContactName, r.Profile.Name, r.E164))
}

// Les groupes connus de signalmeow : leur nom, leurs membres et leur image.
func (p *Pont) syncGroups(ctx context.Context, cli *signalmeow.Client, onlyNew bool) {
	ids := p.knownGroups(cli.Store.ACI.String())
	n := 0
	for _, gid := range ids {
		if ctx.Err() != nil || p.client() != cli {
			return
		}
		p.mu.Lock()
		_, known := p.convs[gid]
		p.mu.Unlock()
		if onlyNew && known {
			continue
		}
		if p.refreshGroup(ctx, cli, gid, 0) {
			n++
		}
	}
	p.mu.Lock()
	p.lastGroupSync = time.Now()
	p.mu.Unlock()
	if n > 0 {
		log.Info().Int("groupes", n).Msg("groupes synchronisés")
		p.markDirty()
		p.emitList()
	}
}

// Relit un groupe chez Signal (nom, membres, image). Vrai s'il a pu être lu.
func (p *Pont) refreshGroup(ctx context.Context, cli *signalmeow.Client, gid string, revision uint32) bool {
	gctx, annule := context.WithTimeout(ctx, 30*time.Second)
	defer annule()
	g, _, err := cli.RetrieveGroupByID(gctx, types.GroupIdentifier(gid), revision)
	if err != nil || g == nil {
		log.Debug().Err(err).Str("groupe", gid).Msg("lecture du groupe")
		return false
	}
	me := cli.Store.ACI
	var members []string
	member := false
	for _, m := range g.Members {
		if m.ACI == me {
			member = true
		}
		members = append(members, m.ACI.String())
	}
	p.mu.Lock()
	c := p.convs[gid]
	if c == nil {
		c = &convState{}
		p.convs[gid] = c
		// Un groupe sans message encore : il apparaît dans la liste à partir
		// de maintenant (Signal ne donne pas l'ancien historique).
		c.LastActivity = time.Now().UnixMilli()
		c.ReadWM = c.LastActivity
	}
	c.Group = true
	c.Name = g.Title
	if g.AvatarPath != c.Avatar {
		c.Avatar = g.AvatarPath
		_ = removeAvatar("fil:" + gid)
	}
	c.Revision = max(c.Revision, g.Revision)
	c.Members = members
	c.ReadOnly = g.Terminated || !member
	p.saveConv(gid, c)
	p.listDirty = true
	p.mu.Unlock()
	// Les noms des membres que l'on ne connaît pas encore.
	go p.fetchUnknownNames(members)
	return true
}

// Demande les profils des personnes sans nom connu.
func (p *Pont) fetchUnknownNames(acis []string) {
	cli := p.client()
	if cli == nil {
		return
	}
	for _, a := range acis {
		p.mu.Lock()
		c := p.contacts[a]
		p.mu.Unlock()
		if c != nil && c.Name != "" {
			continue
		}
		id, err := uuid.Parse(a)
		if err != nil {
			continue
		}
		ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
		r, err := cli.ContactByACI(ctx, id)
		annule()
		if err != nil {
			log.Debug().Err(err).Str("aci", a).Msg("profil")
			continue
		}
		p.updateContact(r)
	}
}

func (p *Pont) disconnect() {
	p.mu.Lock()
	cli := p.cli
	p.cli = nil
	p.ready = false
	if p.connCancel != nil {
		p.connCancel()
		p.connCancel = nil
	}
	p.mu.Unlock()
	if cli != nil {
		if err := cli.StopReceiveLoops(); err != nil {
			log.Debug().Err(err).Msg("arrêt des sockets")
		}
	}
}

// ---- Les fils ----

func (p *Pont) conv(id string) *convState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.convs[id]
}

// Les identifiants de Signal : un groupe a un identifiant de 44 caractères
// (base64) ; une personne, son ACI (UUID), ou « PNI:<uuid> » si on ne connaît
// que son numéro.
func isGroupID(id string) bool { return len(id) == 44 }

// À appeler avec p.mu.
func (p *Pont) getOrMakeConv(id string) *convState {
	c := p.convs[id]
	if c == nil {
		c = &convState{Group: isGroupID(id)}
		if !c.Group {
			c.Members = []string{id}
		}
		p.convs[id] = c
	}
	return c
}

func (p *Pont) contactName(id string) string {
	if c := p.contacts[id]; c != nil {
		return c.Name
	}
	return ""
}

// À appeler avec p.mu.
func (p *Pont) convOut(id string, c *convState) Conversation {
	me := p.me.String()
	name := c.Name
	var members []string
	for _, m := range c.Members {
		if m == me {
			continue
		}
		if n := p.contactName(m); n != "" {
			members = append(members, n)
		}
	}
	if !c.Group {
		if n := p.contactName(id); n != "" {
			name = n
		} else if strings.HasPrefix(id, "PNI:") {
			name = c.Name
		}
		if id == me && name == "" {
			name = "Note à moi-même"
		}
		if len(members) == 0 && name != "" {
			members = []string{name}
		}
	}
	if name == "" && len(members) > 0 {
		name = strings.Join(members, ", ")
	}
	if name == "" {
		name = "Conversation Signal"
	}
	t := max(c.LastActivity, c.LastTime)
	unread := !c.LastFromMe && c.LastTime > c.ReadWM && c.LastTime > 0
	return Conversation{
		ID:       id,
		Name:     name,
		Preview:  c.LastText,
		FromMe:   c.LastFromMe,
		Time:     t,
		Unread:   unread,
		Group:    c.Group,
		Members:  orEmpty(members),
		ReadOnly: c.ReadOnly,
		E2EE:     true,
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (p *Pont) markDirty() {
	p.mu.Lock()
	p.listDirty = true
	p.mu.Unlock()
}

// Renvoie la liste à l'interface quand elle a changé (au plus toutes les 2 s).
func (p *Pont) listLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
			p.mu.Lock()
			dirty := p.listDirty && p.ready
			p.mu.Unlock()
			if dirty {
				p.emitList()
			}
		}
	}
}

func (p *Pont) emitList() {
	p.mu.Lock()
	n, arch := p.listCount, p.archiveWanted
	p.listDirty = false
	p.mu.Unlock()
	p.sendList(n, false, true)
	if arch {
		p.sendList(n, true, false)
	}
}

func (p *Pont) listConversations(n int, archive bool) {
	p.mu.Lock()
	if archive {
		p.archiveWanted = true
	} else {
		p.listCount = max(n, p.listCount)
	}
	p.mu.Unlock()
	p.sendList(n, archive, true)
}

func (p *Pont) sendList(n int, archive, avatars bool) {
	p.mu.Lock()
	items := []Conversation{}
	for id, c := range p.convs {
		if c.Hidden {
			continue
		}
		co := p.convOut(id, c)
		// Signal n'a pas d'archives synchronisées : la liste archivée reste vide.
		if co.Archived != archive || co.Time == 0 && co.Preview == "" {
			continue
		}
		items = append(items, co)
	}
	p.mu.Unlock()
	slices.SortStableFunc(items, func(a, b Conversation) int { return cmp.Compare(b.Time, a.Time) })
	if len(items) > n {
		items = items[:n]
	}
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.ID)
	}
	ev := map[string]any{"items": items}
	if archive {
		ev["folder"] = "archive"
	}
	emit("conversations", ev)
	if avatars {
		go p.avatars(keys)
	}
}

// ---- Les messages ----

// Tout est déjà dans le cache : Signal ne garde pas d'historique sur ses
// serveurs (et n'en donne pas aux appareils liés).
func (p *Pont) messages(cmd Cmd) {
	count := int(cmd.Count)
	if count <= 0 {
		count = 50
	}
	conv := cmd.Conv
	if p.conv(conv) == nil {
		emitErr("messages", fmt.Errorf("conversation inconnue : %s", conv))
		emit("messages", map[string]any{"conv": conv, "items": []Message{}, "older": cmd.Before != "", "more": false})
		return
	}
	rows := p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
	more := len(rows) >= count
	items := make([]Message, 0, len(rows))
	p.mu.Lock()
	for _, r := range rows {
		items = append(items, p.msgOut(r))
	}
	if len(items) > 0 {
		if old, ok := p.loaded[conv]; !ok || items[0].Time < old {
			p.loaded[conv] = items[0].Time
		}
	} else if _, ok := p.loaded[conv]; !ok {
		p.loaded[conv] = time.Now().UnixMilli()
	}
	p.mu.Unlock()
	emit("messages", map[string]any{"conv": conv, "items": items, "older": cmd.Before != "", "more": more})
}

// À appeler avec p.mu : le format de l'interface, avec les noms à jour.
func (p *Pont) msgOut(r *msgRow) Message {
	m := r.Message
	if r.SenderID != "" && !m.FromMe {
		if n := p.contactName(r.SenderID); n != "" {
			m.Sender = n
		}
	}
	m.Reactions = slices.Clone(m.Reactions)
	for i := range m.Reactions {
		if n := p.contactName(m.Reactions[i].Actor); n != "" {
			m.Reactions[i].Sender = n
		}
	}
	return m
}

// Le destinataire d'un fil : une personne ou un groupe.
func parseConv(conv string) (libsignalgo.ServiceID, types.GroupIdentifier, error) {
	if isGroupID(conv) {
		return libsignalgo.ServiceID{}, types.GroupIdentifier(conv), nil
	}
	sid, err := libsignalgo.ServiceIDFromString(conv)
	return sid, "", err
}

// Envoie un contenu dans un fil (personne ou groupe).
func (p *Pont) sendContent(ctx context.Context, cli *signalmeow.Client, conv string, content *signalpb.Content) error {
	sid, gid, err := parseConv(conv)
	if err != nil {
		return fmt.Errorf("fil invalide : %w", err)
	}
	if gid != "" {
		res, err := cli.SendGroupMessage(ctx, gid, content)
		if err != nil {
			return err
		}
		if len(res.SuccessfullySentTo) == 0 && len(res.FailedToSendTo) > 0 {
			return fmt.Errorf("aucun membre du groupe n'a reçu le message : %w", res.FailureError())
		}
		if len(res.FailedToSendTo) > 0 {
			log.Warn().Int("échecs", len(res.FailedToSendTo)).Int("reçus", len(res.SuccessfullySentTo)).Msg("envoi au groupe incomplet")
		}
		return nil
	}
	res := cli.SendMessage(ctx, sid, content)
	if !res.WasSuccessful {
		if res.Error != nil {
			return res.Error
		}
		return errors.New("échec de l'envoi")
	}
	return nil
}

// Les erreurs d'envoi, en français.
func explainSend(err error) error {
	switch {
	case errors.Is(err, signalmeow.ErrUnregisteredUser):
		return errors.New("cette personne n'est plus inscrite à Signal")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("Signal ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("Signal : %w", err)
}

func (p *Pont) send(cmd Cmd) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return
	}
	p.sendCommon(cmd, text, "", "")
}

// Envoi commun texte/fichier. L'horodatage du message est aussi son
// identifiant chez Signal (avec mon ACI).
func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	ts := uint64(time.Now().UnixMilli())
	tmp := cmp.Or(cmd.TmpID, fmt.Sprintf("tmp_%d", ts))
	fail := func(err error) {
		log.Warn().Err(err).Str("fil", cmd.Conv).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	c := p.conv(cmd.Conv)
	if c == nil {
		fail(fmt.Errorf("conversation inconnue : %s", cmd.Conv))
		return
	}
	cli := p.client()
	if cli == nil {
		fail(errors.New("Signal n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()

	dm := &signalpb.DataMessage{Timestamp: proto.Uint64(ts)}
	if text != "" {
		dm.Body = proto.String(text)
	}
	var atts []Attachment
	if path != "" {
		ptr, local, err := p.uploadMedia(ctx, cli, path, mime)
		if err != nil {
			fail(fmt.Errorf("envoi du fichier : %w", err))
			return
		}
		dm.Attachments = []*signalpb.AttachmentPointer{ptr}
		atts = local
	}
	if err := p.sendContent(ctx, cli, cmd.Conv, signalmeow.WrapDataMessage(dm)); err != nil {
		fail(explainSend(err))
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	me := cli.Store.ACI
	r := &msgRow{
		Message: Message{
			ID:          msgID(me, ts),
			Conv:        cmd.Conv,
			Text:        text,
			FromMe:      true,
			Time:        int64(ts),
			Status:      "OUTGOING_COMPLETE",
			Kind:        "signal",
			TmpID:       tmp,
			Attachments: atts,
		},
		SenderID: me.String(),
	}
	r.Media = mediaSummary(atts)
	p.mu.Lock()
	c.LastActivity = max(c.LastActivity, r.Time)
	c.ReadWM = max(c.ReadWM, r.Time)
	c.Hidden = false
	p.saveConv(cmd.Conv, c)
	p.mu.Unlock()
	p.putMsg(r)
	p.noteLast(cmd.Conv, r)
	p.mu.Lock()
	out := p.msgOut(r)
	p.mu.Unlock()
	emit("message", map[string]any{"item": out})
}

// Lu : les accusés de lecture partent vers les expéditeurs (et signalmeow
// prévient mes autres appareils).
func (p *Pont) markRead(cmd Cmd) {
	c := p.conv(cmd.Conv)
	if c == nil {
		return
	}
	cli := p.client()
	p.mu.Lock()
	since := c.ReceiptWM
	now := time.Now().UnixMilli()
	c.ReadWM = max(c.ReadWM, c.LastTime, now)
	if cli != nil {
		c.ReceiptWM = max(c.ReceiptWM, c.LastTime, now)
	}
	p.saveConv(cmd.Conv, c)
	p.listDirty = true
	p.mu.Unlock()
	if cli == nil {
		return
	}
	// Accusés de lecture désactivés dans les réglages de Signal : on n'en envoie pas.
	if ar := cli.Store.AccountRecord; ar != nil && !ar.GetReadReceipts() {
		return
	}
	bySender := map[uuid.UUID][]uint64{}
	for _, r := range p.incomingSince(cmd.Conv, since) {
		sender, ts, err := parseMsgID(r.ID)
		if err != nil || sender == cli.Store.ACI {
			continue
		}
		bySender[sender] = append(bySender[sender], ts)
	}
	for sender, tss := range bySender {
		ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
		res := cli.SendMessage(ctx, libsignalgo.NewACIServiceID(sender), signalmeow.ReadReceptMessageForTimestamps(tss))
		annule()
		if !res.WasSuccessful {
			log.Warn().Err(res.Error).Stringer("à", sender).Msg("accusé de lecture")
		}
	}
}

// « Est en train d'écrire » vers Signal (commande facultative de l'interface).
func (p *Pont) sendTyping(cmd Cmd) {
	cli := p.client()
	if cli == nil || p.conv(cmd.Conv) == nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
	defer annule()
	if err := p.sendContent(ctx, cli, cmd.Conv, signalmeow.TypingMessage(cmd.On)); err != nil {
		log.Debug().Err(err).Msg("saisie")
	}
}

var phoneChars = regexp.MustCompile(`[\s\-().]`)

// Nouvelle conversation : un numéro (+1…), un nom d'utilisateur Signal
// (nom.12), ou un identifiant ; sinon une recherche dans les contacts.
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimSpace(cmd.Number)
	if q == "" {
		q = strings.TrimSpace(cmd.Text)
	}
	if q == "" {
		return
	}
	cli := p.client()
	if cli == nil {
		emitErr("new", errors.New("Signal n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	var conv string
	digits := phoneChars.ReplaceAllString(q, "")
	switch {
	case isGroupID(q) && p.conv(q) != nil:
		conv = q
	case strings.HasPrefix(q, "PNI:") || isUUID(q):
		sid, err := libsignalgo.ServiceIDFromString(q)
		if err != nil {
			emitErr("new", fmt.Errorf("identifiant invalide : %w", err))
			return
		}
		conv = sid.String()
	case signalmeow.SignalUsernameRegex.MatchString(q):
		sid, err := cli.ResolveUsername(ctx, strings.TrimPrefix(q, "@"))
		if err != nil || sid.IsEmpty() {
			emitErr("new", fmt.Errorf("nom d'utilisateur Signal introuvable : %s", q))
			return
		}
		conv = sid.String()
	case isPhone(digits):
		sid, err := p.resolvePhone(ctx, cli, digits)
		if err != nil {
			emitErr("new", err)
			return
		}
		conv = sid.String()
	default:
		p.search(q)
		return
	}
	if sid, err := libsignalgo.ServiceIDFromString(conv); err == nil && sid.Type == libsignalgo.ServiceIDTypeACI {
		if r, err := cli.ContactByACI(ctx, sid.UUID); err == nil {
			p.updateContact(r)
		}
	}
	p.mu.Lock()
	c := p.getOrMakeConv(conv)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
		c.ReadWM = c.LastActivity
	}
	if !c.Group && strings.HasPrefix(conv, "PNI:") && c.Name == "" {
		c.Name = digits
	}
	c.Hidden = false
	p.saveConv(conv, c)
	item := p.convOut(conv, c)
	p.listDirty = true
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{conv})
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func isPhone(s string) bool {
	s = strings.TrimPrefix(s, "+")
	if len(s) < 7 || len(s) > 15 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Un numéro de téléphone vers un compte Signal : le carnet local d'abord,
// puis la recherche de contacts de Signal (CDSI).
func (p *Pont) resolvePhone(ctx context.Context, cli *signalmeow.Client, number string) (libsignalgo.ServiceID, error) {
	if !strings.HasPrefix(number, "+") {
		// Sans indicatif : numéro nord-américain à 10 chiffres.
		if len(number) == 10 {
			number = "1" + number
		}
		number = "+" + number
	}
	if r, err := cli.ContactByE164(ctx, number); err == nil && r != nil && r.ACI != uuid.Nil {
		return libsignalgo.NewACIServiceID(r.ACI), nil
	}
	var e164 uint64
	if _, err := fmt.Sscanf(strings.TrimPrefix(number, "+"), "%d", &e164); err != nil {
		return libsignalgo.ServiceID{}, fmt.Errorf("numéro invalide : %s", number)
	}
	resp, err := cli.LookupPhone(ctx, e164)
	if err != nil {
		return libsignalgo.ServiceID{}, fmt.Errorf("recherche du numéro chez Signal : %w", err)
	}
	found := resp[e164]
	if found.ACI == uuid.Nil && found.PNI == uuid.Nil {
		return libsignalgo.ServiceID{}, fmt.Errorf("%s n'est pas sur Signal", number)
	}
	if r, err := cli.Store.RecipientStore.UpdateRecipientE164(ctx, found.ACI, found.PNI, number); err == nil {
		p.updateContact(r)
	}
	if found.ACI != uuid.Nil {
		return libsignalgo.NewACIServiceID(found.ACI), nil
	}
	return libsignalgo.NewPNIServiceID(found.PNI), nil
}

// Recherche de personnes et de groupes, dans ce que le pont connaît :
// « search{items:[{id,name,avatar}]} ». Signal n'a pas d'annuaire public :
// pour une personne inconnue, la commande « new » avec son numéro suffit.
func (p *Pont) search(q string) {
	type item struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Avatar string `json:"avatar,omitempty"`
	}
	items := []item{}
	if q == "" {
		emit("search", map[string]any{"query": q, "items": items})
		return
	}
	lq := strings.ToLower(q)
	digits := phoneChars.ReplaceAllString(q, "")
	p.mu.Lock()
	me := p.me.String()
	for id, c := range p.contacts {
		if id == me || c.Name == "" {
			continue
		}
		if strings.Contains(strings.ToLower(c.Name), lq) || (digits != "" && strings.Contains(c.Number, strings.TrimPrefix(digits, "+"))) {
			name := c.Name
			if c.Number != "" && c.Number != c.Name {
				name = fmt.Sprintf("%s (%s)", c.Name, c.Number)
			}
			items = append(items, item{ID: id, Name: name})
		}
	}
	for id, c := range p.convs {
		if c.Group && strings.Contains(strings.ToLower(c.Name), lq) {
			items = append(items, item{ID: id, Name: c.Name})
		}
	}
	p.mu.Unlock()
	slices.SortFunc(items, func(a, b item) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	if len(items) > 50 {
		items = items[:50]
	}
	emit("search", map[string]any{"query": q, "items": items})
}

// Oublie le compte : cache.
func (p *Pont) forgetAccount() {
	p.mu.Lock()
	p.convs = map[string]*convState{}
	p.contacts = map[string]*contact{}
	p.loaded = map[string]int64{}
	p.myName = ""
	p.mu.Unlock()
	if err := p.wipeStore(); err != nil {
		log.Warn().Err(err).Msg("effacement du cache")
	}
}

// Délie Jasette : l'appareil est retiré du compte (comme dans Signal >
// Appareils liés), puis tout est oublié.
func (p *Pont) unpair() {
	p.mu.Lock()
	if p.pairCancel != nil {
		p.pairCancel()
		p.pairCancel = nil
	}
	cli := p.cli
	p.mu.Unlock()
	if cli != nil {
		ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
		if err := cli.Unlink(ctx); err != nil {
			log.Warn().Err(err).Msg("retrait de l'appareil lié")
		}
		annule()
	}
	p.disconnect()
	if devs, err := p.sm.GetAllDevices(p.ctx); err == nil {
		for _, d := range devs {
			if err := p.sm.DeleteDevice(context.WithoutCancel(p.ctx), &d.DeviceData); err != nil {
				log.Warn().Err(err).Msg("effacement de l'appareil")
			}
		}
	}
	p.forgetAccount()
	p.mu.Lock()
	p.me = uuid.Nil
	p.mu.Unlock()
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.mu.Lock()
	if p.pairCancel != nil {
		p.pairCancel()
	}
	p.mu.Unlock()
	p.disconnect()
	if p.db != nil {
		_ = p.db.Close()
	}
}
