package main

// La connexion à WhatsApp (whatsmeow) : association en appareil lié,
// connexion, événements du socket, et toutes les commandes de l'interface.

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Pont struct {
	ctx     context.Context
	db      *sql.DB
	waStore *sqlstore.Container

	mu    sync.Mutex
	cli   *whatsmeow.Client
	convs map[string]*convState
	names map[string]string // JID -> nom (carnet, nom public, entreprise)
	lidPN map[string]string // LID -> JID du numéro

	pending   map[string]string // id du message envoyé -> tmp_id de l'interface
	loaded    map[string]int64  // fil -> plus vieux message montré à l'interface
	fetching  map[string]bool
	histWait  map[string][]chan struct{}         // fil -> attentes d'une page d'historique
	histAsked map[string]time.Time               // fil -> dernière demande d'historique
	retryWait map[string]chan *events.MediaRetry // message -> attente d'un nouveau lien de média
	noPic     map[string]time.Time               // JID sans photo (pour ne pas redemander sans arrêt)
	picIDs    map[string]string                  // JID -> identifiant de la photo téléchargée

	listDirty     bool
	listCount     int
	archiveWanted bool
	ready         bool
	connecting    bool
	pairing       bool
	pairCancel    context.CancelFunc
	phoneCode     bool // un code d'association par numéro a été donné : plus de QR
	online        bool
	lastActive    time.Time

	// La présence des contacts (presence.go) : réglage, et abonnements de
	// cette connexion.
	presenceOn bool
	subscribed map[string]bool
	pstats     presenceStats

	// « Quoi de neuf ? » (notes.go).
	notes *notesPont
}

func newPont(ctx context.Context) *Pont {
	// Le nom montré dans « Appareils connectés » du téléphone.
	store.SetOSInfo("Jasette", [3]uint32{1, 0, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	p := &Pont{
		ctx:        ctx,
		convs:      map[string]*convState{},
		subscribed: map[string]bool{},
		presenceOn: presenceEnv(),
		names:      map[string]string{},
		lidPN:      map[string]string{},
		pending:    map[string]string{},
		loaded:     map[string]int64{},
		fetching:   map[string]bool{},
		histWait:   map[string][]chan struct{}{},
		histAsked:  map[string]time.Time{},
		retryWait:  map[string]chan *events.MediaRetry{},
		noPic:      map[string]time.Time{},
		picIDs:     map[string]string{},
		listCount:  50,
	}
	p.notes = nouvellesNotesWA(p)
	go p.listLoop()
	return p
}

func (p *Pont) client() *whatsmeow.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cli
}

// Le client, s'il est associé et connecté.
func (p *Pont) readyClient() (*whatsmeow.Client, error) {
	cli := p.client()
	if cli == nil || cli.Store.ID == nil {
		return nil, errors.New("WhatsApp n'est pas associé")
	}
	if !cli.IsConnected() {
		return nil, errors.New("WhatsApp n'est pas connecté")
	}
	return cli, nil
}

func (p *Pont) newClient(dev *store.Device) *whatsmeow.Client {
	cli := whatsmeow.NewClient(dev, waLog.Zerolog(log.With().Str("c", "whatsmeow").Logger()))
	cli.EnableAutoReconnect = true
	cli.AutomaticMessageRerequestFromPhone = true
	cli.AddEventHandler(func(evt any) { p.onEvent(cli, evt) })
	p.mu.Lock()
	p.cli = cli
	p.mu.Unlock()
	return cli
}

// ---- Démarrage, association et connexion ----

// Démarre avec l'appareil gardé, sinon dit à l'interface qu'il faut s'associer.
func (p *Pont) start() {
	dev, err := p.waStore.GetFirstDevice(p.ctx)
	if err != nil {
		log.Err(err).Msg("appareil lié")
		emitErr("store", fmt.Errorf("appareil WhatsApp illisible : %w", err))
		emitState("unpaired")
		return
	}
	if dev.ID == nil {
		emitState("unpaired")
		return
	}
	p.connect(p.newClient(dev))
}

// Connecte un client déjà associé (avec de nouveaux essais si le réseau manque).
func (p *Pont) connect(cli *whatsmeow.Client) {
	p.mu.Lock()
	if p.connecting {
		p.mu.Unlock()
		return
	}
	p.connecting = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.connecting = false
		p.mu.Unlock()
	}()
	emitState("connecting")
	for essai := 0; ; essai++ {
		err := cli.Connect()
		if err == nil || errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			return
		}
		log.Err(err).Int("essai", essai).Msg("connexion")
		if essai == 2 {
			emitErr("connect", fmt.Errorf("WhatsApp : %w", err))
		}
		emitState("reconnecting")
		select {
		case <-time.After(time.Duration(min(2<<essai, 120)) * time.Second):
		case <-p.ctx.Done():
			return
		}
		if p.client() != cli {
			return
		}
	}
}

func (p *Pont) disconnect() {
	p.mu.Lock()
	cli := p.cli
	p.cli = nil
	p.ready = false
	p.online = false
	if p.pairCancel != nil {
		p.pairCancel()
		p.pairCancel = nil
	}
	p.mu.Unlock()
	if cli != nil {
		cli.RemoveEventHandlers()
		cli.Disconnect()
	}
}

// Association en appareil lié. Sans numéro : codes QR (« qr ») ; avec un
// numéro : un code à taper sur le téléphone (« pair_code »).
func (p *Pont) pair(phone string) {
	p.mu.Lock()
	cli := p.cli
	if cli != nil && cli.Store.ID != nil {
		p.mu.Unlock()
		emitErr("pair", errors.New("WhatsApp est déjà associé (dissocie d'abord)"))
		return
	}
	if p.pairing && cli != nil {
		// Association déjà en cours : on donne seulement le code demandé.
		p.mu.Unlock()
		if phone != "" {
			p.pairPhone(cli, phone)
		}
		return
	}
	p.pairing = true
	p.phoneCode = false
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.pairing = false
		p.mu.Unlock()
	}()

	p.disconnect()
	cli = p.newClient(p.waStore.NewDevice())
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	p.mu.Lock()
	p.pairCancel = annule
	p.mu.Unlock()
	qrs, err := cli.GetQRChannel(ctx)
	if err != nil {
		emitErr("pair", fmt.Errorf("association : %w", err))
		emitState("unpaired")
		return
	}
	if err := cli.Connect(); err != nil {
		emitErr("pair", fmt.Errorf("WhatsApp injoignable : %w", err))
		emitState("unpaired")
		return
	}
	emitState("pairing")
	first := true
	for item := range qrs {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			if first && phone != "" {
				go p.pairPhone(cli, phone)
			}
			first = false
			p.mu.Lock()
			showQR := !p.phoneCode
			p.mu.Unlock()
			if showQR {
				// « code » (ce pont) et « data » (comme le pont Google Messages).
				emit("qr", map[string]any{"code": item.Code, "data": item.Code})
			}
		case whatsmeow.QRChannelSuccess.Event:
			log.Info().Msg("association réussie")
			return
		case whatsmeow.QRChannelTimeout.Event:
			emitErr("pair", errors.New("association : délai dépassé, aucun code n'a été scanné"))
		case whatsmeow.QRChannelEventError:
			emitErr("pair", fmt.Errorf("association : %w", item.Error))
		case whatsmeow.QRChannelClientOutdated.Event:
			emitErr("pair", errors.New("association refusée : la version de WhatsApp du pont est trop vieille (mettre à jour whatsmeow)"))
		case whatsmeow.QRChannelEventPasskeyResponse:
			// Confirmation demandée par le téléphone : on la donne.
			if err := cli.SendPasskeyConfirmation(ctx); err != nil {
				emitErr("pair", fmt.Errorf("association : %w", err))
			}
		case whatsmeow.QRChannelEventPasskeyRequest:
			emitErr("pair", errors.New("le téléphone demande une clé d'accès (passkey) : ce pont ne sait pas la donner, utilise le code QR"))
		case whatsmeow.QRChannelScannedWithoutMultidevice.Event:
			emitErr("pair", errors.New("active les appareils liés sur le téléphone, puis réessaie"))
			continue
		default:
			emitErr("pair", fmt.Errorf("association : %s", item.Event))
		}
	}
	// Fin sans succès (délai, erreur ou annulation).
	if p.client() == cli && cli.Store.ID == nil {
		p.disconnect()
		emitState("unpaired")
	}
}

// Le code d'association par numéro (huit caractères, « XXXX-XXXX »).
func (p *Pont) pairPhone(cli *whatsmeow.Client, phone string) {
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	code, err := cli.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		switch {
		case errors.Is(err, whatsmeow.ErrPhoneNumberTooShort):
			err = errors.New("numéro trop court (avec l'indicatif du pays, ex. +1 514…)")
		case errors.Is(err, whatsmeow.ErrPhoneNumberIsNotInternational):
			err = errors.New("le numéro doit commencer par l'indicatif du pays (ex. +1 514…)")
		}
		emitErr("pair_phone", fmt.Errorf("code d'association : %w", err))
		return
	}
	p.mu.Lock()
	p.phoneCode = true
	p.mu.Unlock()
	emit("pair_code", map[string]any{"code": code})
}

// L'appareil a été délié (depuis le téléphone, ou refusé par WhatsApp).
func (p *Pont) loggedOut(cli *whatsmeow.Client) {
	if p.client() != cli {
		return
	}
	p.disconnect()
	if cli.Store.ID != nil {
		if err := cli.Store.Delete(context.WithoutCancel(p.ctx)); err != nil {
			log.Warn().Err(err).Msg("effacement de l'appareil")
		}
	}
	p.forgetAccount()
	emitState("unpaired")
}

// Le nom et l'identifiant du compte.
func (p *Pont) emitAccount(cli *whatsmeow.Client) {
	if cli.Store.ID == nil {
		return
	}
	me := cli.Store.ID.ToNonAD()
	p.mu.Lock()
	name := cmp.Or(cli.Store.PushName, p.names[me.String()], "+"+me.User)
	p.mu.Unlock()
	emit("account", map[string]any{"service": serviceName(), "name": name, "id": me.User, "email": ""})
}

// Juste après chaque connexion : carnet, groupes, photo, liste.
func (p *Pont) afterConnect(cli *whatsmeow.Client) {
	p.reloadContacts(cli)
	p.emitAccount(cli)
	ctx, annule := context.WithTimeout(p.ctx, 60*time.Second)
	groups, err := cli.GetJoinedGroups(ctx)
	annule()
	if err != nil {
		log.Warn().Err(err).Msg("groupes")
	} else {
		for _, g := range groups {
			p.upsertGroup(cli, g)
		}
	}
	p.mu.Lock()
	presence := p.presenceOn
	p.mu.Unlock()
	if presence {
		// « Afficher qui est en ligne » : disponible, et abonné aux contacts
		// récents (WhatsApp ne donne la présence qu'à un appareil disponible).
		go p.presenceUp(cli)
	} else {
		// Hors ligne par défaut : le téléphone garde ses notifications.
		_ = cli.SendPresence(context.WithoutCancel(p.ctx), types.PresenceUnavailable)
	}
	p.emitList()
	go p.myAvatarDownload(cli)
}

// ---- Les événements de whatsmeow ----

func (p *Pont) onEvent(cli *whatsmeow.Client, raw any) {
	defer func() {
		if v := recover(); v != nil {
			log.Error().Any("panique", v).Type("type", raw).Msg("événement")
		}
	}()
	if p.client() != cli {
		return
	}
	switch evt := raw.(type) {
	case *events.QR:
		// Géré par le canal QR (pair).
	case *events.PairSuccess:
		log.Info().Str("jid", evt.ID.String()).Str("plateforme", evt.Platform).Msg("appareil associé")
		// Un autre compte qu'avant : on repart d'un cache vide.
		var old string
		_ = jsonUnmarshal(p.loadKV("account"), &old)
		if old != evt.ID.User {
			p.forgetAccount()
		}
		p.saveKV("account", evt.ID.User)
		emit("paired", nil)
		emitState("connecting")
	case *events.PairError:
		emitErr("pair", fmt.Errorf("association : %w", evt.Error))
	case *events.Connected:
		log.Info().Msg("connecté à WhatsApp")
		p.mu.Lock()
		p.ready = true
		p.mu.Unlock()
		emitState("ready")
		go p.notes.annoncer()
		go p.afterConnect(cli)
	case *events.Disconnected:
		p.mu.Lock()
		was := p.ready
		p.ready = false
		p.online = false
		p.mu.Unlock()
		// Juste après l'association, WhatsApp coupe et reconnecte : pas de
		// « reconnexion » à montrer tant qu'on n'a pas été prêt.
		if cli.Store.ID != nil && was {
			emitState("reconnecting")
		}
	case *events.KeepAliveTimeout:
		log.Warn().Int("échecs", evt.ErrorCount).Msg("WhatsApp ne répond plus")
	case *events.KeepAliveRestored:
		log.Info().Msg("WhatsApp répond de nouveau")
	case *events.LoggedOut:
		log.Warn().Bool("au_démarrage", evt.OnConnect).Str("raison", evt.Reason.String()).Msg("appareil délié")
		emitErr("listen", errors.New("WhatsApp a délié cet appareil : associe-le de nouveau"))
		go p.loggedOut(cli)
	case *events.StreamReplaced:
		emitErr("listen", errors.New("une autre copie de Jasette s'est connectée à ce compte WhatsApp"))
		emitState("reconnecting")
	case *events.TemporaryBan:
		emitErr("listen", fmt.Errorf("WhatsApp bloque temporairement le compte : %s", evt.String()))
	case *events.ConnectFailure:
		emitErr("listen", fmt.Errorf("WhatsApp refuse la connexion : %s %s", evt.Reason.String(), evt.Message))
	case *events.ClientOutdated:
		emitErr("listen", errors.New("WhatsApp refuse cette version du pont : mettre à jour whatsmeow (pont-wa)"))
	case *events.HistorySync:
		p.handleHistory(cli, evt)
	case *events.Message:
		p.handleMessage(cli, evt, true)
	case *events.UndecryptableMessage:
		p.handleUndecryptable(cli, evt)
	case *events.Receipt:
		p.handleReceipt(cli, evt)
	case *events.ChatPresence:
		p.handleTyping(cli, evt)
	case *events.Presence:
		p.handlePresence(cli, evt)
	case *events.Archive:
		p.setConvFlag(cli, evt.JID, func(c *convState) { c.Archived = evt.Action.GetArchived() })
	case *events.MarkChatAsRead:
		read := evt.Action.GetRead()
		p.setConvFlag(cli, evt.JID, func(c *convState) {
			if read {
				c.Unread, c.MarkedUnread = 0, false
				c.ReadWM = max(c.ReadWM, c.LastTime)
			} else {
				c.MarkedUnread = true
			}
		})
	case *events.DeleteChat:
		p.setConvFlag(cli, evt.JID, func(c *convState) { c.Hidden = true })
	case *events.Contact:
		go p.refreshName(cli, evt.JID)
	case *events.PushName:
		go p.refreshName(cli, evt.JID)
	case *events.BusinessName:
		go p.refreshName(cli, evt.JID)
	case *events.PushNameSetting:
		go p.emitAccount(cli)
		go p.presenceNomRecu(cli)
	case *events.AppStateSyncComplete:
		go func() {
			p.reloadContacts(cli)
			p.emitAccount(cli)
			p.markDirty()
		}()
	case *events.OfflineSyncCompleted:
		p.markDirty()
	case *events.Picture:
		go p.pictureChanged(cli, evt)
	case *events.GroupInfo:
		p.handleGroupInfo(cli, evt)
	case *events.JoinedGroup:
		p.upsertGroup(cli, &evt.GroupInfo)
		go p.avatars([]string{evt.JID.String()})
	case *events.MediaRetry:
		p.mu.Lock()
		ch := p.retryWait[evt.MessageID]
		p.mu.Unlock()
		if ch != nil {
			select {
			case ch <- evt:
			default:
			}
		}
	default:
		log.Debug().Type("type", raw).Msg("événement whatsmeow ignoré")
	}
}

func jsonUnmarshal(b []byte, v any) error {
	if b == nil {
		return errors.New("vide")
	}
	return jsonDecode(b, v)
}

// ---- Les identifiants ----

// Le JID d'un fil, tel que donné à l'interface : sans appareil, et pour une
// personne, le numéro plutôt que le LID quand WhatsApp le connaît.
func (p *Pont) canon(cli *whatsmeow.Client, jid types.JID) types.JID {
	jid = jid.ToNonAD()
	if jid.Server != types.HiddenUserServer {
		return jid
	}
	key := jid.String()
	p.mu.Lock()
	pn := p.lidPN[key]
	p.mu.Unlock()
	if pn != "" {
		if j, err := types.ParseJID(pn); err == nil {
			return j
		}
	}
	if cli == nil {
		return jid
	}
	got, err := cli.Store.LIDs.GetPNForLID(p.ctx, jid)
	if err != nil || got.IsEmpty() {
		return jid
	}
	got = got.ToNonAD()
	p.mu.Lock()
	p.lidPN[key] = got.String()
	p.mu.Unlock()
	return got
}

// Les fils que l'interface ne montre pas : statuts, chaînes, listes de diffusion.
func skipChat(jid types.JID) bool {
	switch jid.Server {
	case types.BroadcastServer, types.NewsletterServer:
		return true
	}
	return jid.IsEmpty() || jid.User == ""
}

// ---- Les noms ----

func bestName(c types.ContactInfo) string {
	return cmp.Or(c.FullName, c.FirstName, c.BusinessName, c.PushName)
}

func (p *Pont) reloadContacts(cli *whatsmeow.Client) {
	all, err := cli.Store.Contacts.GetAllContacts(p.ctx)
	if err != nil {
		log.Warn().Err(err).Msg("carnet")
		return
	}
	p.mu.Lock()
	for jid, c := range all {
		if n := bestName(c); n != "" {
			p.names[jid.String()] = n
		}
	}
	p.listDirty = true
	p.mu.Unlock()
	log.Info().Int("contacts", len(all)).Msg("carnet chargé")
}

func (p *Pont) refreshName(cli *whatsmeow.Client, jid types.JID) {
	c, err := cli.Store.Contacts.GetContact(p.ctx, jid.ToNonAD())
	if err != nil {
		return
	}
	if n := bestName(c); n != "" {
		p.mu.Lock()
		p.names[jid.ToNonAD().String()] = n
		p.listDirty = true
		p.mu.Unlock()
	}
}

// À appeler avec p.mu : le nom d'une personne (JID texte).
func (p *Pont) nameOf(jid string) string {
	if n := p.names[jid]; n != "" {
		return n
	}
	if pn := p.lidPN[jid]; pn != "" {
		if n := p.names[pn]; n != "" {
			return n
		}
		jid = pn
	}
	if user, server, ok := strings.Cut(jid, "@"); ok && server == types.DefaultUserServer {
		return "+" + user
	}
	return ""
}

// À appeler avec p.mu.
func (p *Pont) isMe(cli *whatsmeow.Client, jid string) bool {
	if cli == nil || cli.Store.ID == nil || jid == "" {
		return false
	}
	return jid == cli.Store.ID.ToNonAD().String() || (!cli.Store.LID.IsEmpty() && jid == cli.Store.LID.ToNonAD().String())
}

// ---- Les fils ----

func (p *Pont) conv(id string) *convState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.convs[id]
}

// À appeler avec p.mu.
func (p *Pont) getOrMakeConv(jid types.JID) (string, *convState) {
	id := jid.String()
	c := p.convs[id]
	if c == nil {
		c = &convState{JID: id, Group: jid.Server == types.GroupServer}
		p.convs[id] = c
	}
	return id, c
}

func (p *Pont) setConvFlag(cli *whatsmeow.Client, jid types.JID, f func(c *convState)) {
	jid = p.canon(cli, jid)
	if skipChat(jid) {
		return
	}
	p.mu.Lock()
	id, c := p.getOrMakeConv(jid)
	f(c)
	p.saveConv(id, c)
	p.listDirty = true
	p.mu.Unlock()
}

// Un groupe (liste des groupes, ajout à un groupe).
func (p *Pont) upsertGroup(cli *whatsmeow.Client, g *types.GroupInfo) {
	if g == nil || g.JID.IsEmpty() {
		return
	}
	var members []string
	for _, m := range g.Participants {
		j := m.JID
		if !m.PhoneNumber.IsEmpty() {
			j = m.PhoneNumber
			if !m.LID.IsEmpty() {
				p.mu.Lock()
				p.lidPN[m.LID.ToNonAD().String()] = m.PhoneNumber.ToNonAD().String()
				p.mu.Unlock()
			}
		}
		members = append(members, j.ToNonAD().String())
	}
	p.mu.Lock()
	id, c := p.getOrMakeConv(g.JID)
	c.Group = true
	if g.Name != "" {
		c.Name = g.Name
	}
	if len(members) > 0 {
		c.Members = members
	}
	c.ReadOnly = g.IsAnnounce && !p.amAdmin(cli, g)
	if c.LastActivity == 0 && !g.GroupCreated.IsZero() {
		c.LastActivity = g.GroupCreated.UnixMilli()
	}
	p.saveConv(id, c)
	p.listDirty = true
	p.mu.Unlock()
}

// À appeler avec p.mu : suis-je administrateur du groupe ?
func (p *Pont) amAdmin(cli *whatsmeow.Client, g *types.GroupInfo) bool {
	for _, m := range g.Participants {
		if (m.IsAdmin || m.IsSuperAdmin) && (p.isMe(cli, m.JID.ToNonAD().String()) || p.isMe(cli, m.PhoneNumber.ToNonAD().String()) || p.isMe(cli, m.LID.ToNonAD().String())) {
			return true
		}
	}
	return false
}

func (p *Pont) handleGroupInfo(cli *whatsmeow.Client, evt *events.GroupInfo) {
	p.mu.Lock()
	id, c := p.getOrMakeConv(evt.JID)
	c.Group = true
	if evt.Name != nil && evt.Name.Name != "" {
		c.Name = evt.Name.Name
	}
	for _, j := range evt.Join {
		s := j.ToNonAD().String()
		if pn := p.lidPN[s]; pn != "" {
			s = pn
		}
		if !slices.Contains(c.Members, s) {
			c.Members = append(c.Members, s)
		}
	}
	for _, j := range evt.Leave {
		s := j.ToNonAD().String()
		pn := p.lidPN[s]
		c.Members = slices.DeleteFunc(c.Members, func(m string) bool { return m == s || (pn != "" && m == pn) })
		if p.isMe(cli, s) || p.isMe(cli, pn) {
			c.ReadOnly = true
		}
	}
	if evt.Announce != nil {
		c.ReadOnly = evt.Announce.IsAnnounce
	}
	p.saveConv(id, c)
	p.listDirty = true
	p.mu.Unlock()
	if evt.Announce != nil {
		// Les administrateurs peuvent quand même écrire : on redemande le groupe.
		go func() {
			ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
			defer annule()
			if g, err := cli.GetGroupInfo(ctx, evt.JID); err == nil {
				p.upsertGroup(cli, g)
			}
		}()
	}
}

// À appeler avec p.mu.
func (p *Pont) convOut(id string, c *convState) Conversation {
	cli := p.cli
	name := c.Name
	var members []string
	for _, m := range c.Members {
		if p.isMe(cli, m) {
			continue
		}
		if n := p.nameOf(m); n != "" {
			members = append(members, n)
		}
		if len(members) >= 30 {
			break
		}
	}
	if !c.Group {
		if n := p.nameOf(id); n != "" && (name == "" || !strings.HasPrefix(n, "+")) {
			name = n
		}
		if name == "" {
			name = p.nameOf(id)
		}
		if name != "" {
			members = []string{name}
		}
	}
	if name == "" && len(members) > 0 {
		name = strings.Join(members, ", ")
	}
	if name == "" {
		name = "Conversation WhatsApp"
	}
	return Conversation{
		ID:       id,
		Name:     name,
		Preview:  c.LastText,
		FromMe:   c.LastFromMe,
		Time:     max(c.LastActivity, c.LastTime),
		Unread:   c.MarkedUnread || c.Unread > 0,
		Group:    c.Group,
		Members:  orEmpty(members),
		ReadOnly: c.ReadOnly,
		Archived: c.Archived,
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

// Renvoie la liste à l'interface quand elle a changé (au plus toutes les 2 s),
// et repasse « hors ligne » après trois minutes sans activité.
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
			// Avec la présence, on reste disponible.
			idle := p.online && !p.presenceOn && time.Since(p.lastActive) > 3*time.Minute
			if idle {
				p.online = false
			}
			cli := p.cli
			p.mu.Unlock()
			if dirty {
				p.emitList()
			}
			if idle && cli != nil {
				_ = cli.SendPresence(p.ctx, types.PresenceUnavailable)
			}
		}
	}
}

// L'utilisateur est là : « en ligne », pour recevoir les « en train d'écrire »
// (WhatsApp ne les envoie qu'aux appareils en ligne).
func (p *Pont) activity() {
	p.mu.Lock()
	cli := p.cli
	was := p.online
	p.lastActive = time.Now()
	ok := p.ready && cli != nil
	if ok {
		p.online = true
	}
	p.mu.Unlock()
	if ok && !was {
		if err := cli.SendPresence(p.ctx, types.PresenceAvailable); err != nil {
			log.Debug().Err(err).Msg("présence")
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

func (p *Pont) messages(cmd Cmd) {
	p.activity()
	count := int(cmd.Count)
	if count <= 0 {
		count = 50
	}
	conv := cmd.Conv
	c := p.conv(conv)
	if c == nil {
		emitErr("messages", fmt.Errorf("conversation inconnue : %s", conv))
		emit("messages", map[string]any{"conv": conv, "items": []Message{}, "older": cmd.Before != "", "more": false})
		return
	}
	rows := p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
	more := len(rows) >= count
	p.mu.Lock()
	noMore := c.NoMore
	p.mu.Unlock()
	if len(rows) < count && !noMore && len(rows) > 0 {
		// Plus vieux que ce qui est gardé : on le demande au téléphone.
		if p.requestHistory(conv) {
			rows = p.queryMsgs(conv, cmd.BeforeT, cmd.Before, count)
			p.mu.Lock()
			noMore = c.NoMore
			p.mu.Unlock()
			more = len(rows) >= count || (!noMore && len(rows) > 0)
		}
	}
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

// Demande au téléphone les messages plus vieux que le plus vieux gardé, et
// attend sa réponse (un « history sync » à la demande). Vrai si elle est venue.
func (p *Pont) requestHistory(conv string) bool {
	cli, err := p.readyClient()
	if err != nil {
		return false
	}
	p.mu.Lock()
	if p.fetching[conv] || time.Since(p.histAsked[conv]) < 30*time.Second {
		p.mu.Unlock()
		return false
	}
	p.fetching[conv] = true
	p.histAsked[conv] = time.Now()
	ch := make(chan struct{}, 1)
	p.histWait[conv] = append(p.histWait[conv], ch)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.fetching, conv)
		p.histWait[conv] = slices.DeleteFunc(p.histWait[conv], func(x chan struct{}) bool { return x == ch })
		p.mu.Unlock()
	}()
	oldest := p.oldestMsg(conv)
	if oldest == nil {
		return false
	}
	chat, err := types.ParseJID(cmp.Or(oldest.ChatJID, conv))
	if err != nil {
		return false
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, IsFromMe: oldest.FromMe, IsGroup: chat.Server == types.GroupServer},
		ID:            oldest.ID,
		Timestamp:     time.UnixMilli(oldest.Time),
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if _, err := cli.SendPeerMessage(ctx, cli.BuildHistorySyncRequest(info, 50)); err != nil {
		log.Warn().Err(err).Str("fil", conv).Msg("historique : demande au téléphone")
		return false
	}
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		log.Info().Str("fil", conv).Msg("historique : pas de réponse du téléphone")
		return false
	}
}

// À appeler avec p.mu : le format de l'interface, avec les noms à jour.
func (p *Pont) msgOut(r *msgRow) Message {
	m := r.Message
	if r.SenderJID != "" && !m.FromMe {
		if n := p.nameOf(r.SenderJID); n != "" {
			m.Sender = n
		}
	}
	m.Reactions = slices.Clone(m.Reactions)
	for i := range m.Reactions {
		if n := p.nameOf(m.Reactions[i].Actor); n != "" && !m.Reactions[i].FromMe {
			m.Reactions[i].Sender = n
		}
	}
	for _, j := range r.Readers {
		m.ReadBy = append(m.ReadBy, firstOf(p.nameOf(j), strings.SplitN(j, "@", 2)[0]))
	}
	return m
}

func tmpOr(tmp, id string) string {
	if tmp != "" {
		return tmp
	}
	return "tmp_" + id
}

// Un message envoyé : WhatsApp ne renvoie pas d'écho à l'appareil qui l'a
// envoyé, on l'annonce donc nous-mêmes.
func (p *Pont) echoSent(cli *whatsmeow.Client, conv, chat, id, tmp, text string, ts int64, atts []Attachment, reply *msgRow) {
	me := ""
	if cli.Store.ID != nil {
		me = cli.Store.ID.ToNonAD().String()
	}
	r := &msgRow{
		Message:   Message{ID: id, Conv: conv, Text: text, FromMe: true, Time: ts, Kind: "whatsapp", Status: "OUTGOING_COMPLETE", TmpID: tmp, Attachments: atts},
		SenderJID: me,
		ChatJID:   chat,
		Body:      text,
	}
	r.Media = mediaSummary(atts)
	if reply != nil {
		p.mu.Lock()
		who := "moi"
		if !reply.FromMe {
			who = cmp.Or(p.nameOf(reply.SenderJID), reply.Sender)
		}
		p.mu.Unlock()
		setReply(r, reply.ID, who, snippet(cmp.Or(reply.Body, reply.Text), reply.Attachments))
	}
	p.putMsg(r)
	p.noteLast(conv, r)
	p.mu.Lock()
	if c := p.convs[conv]; c != nil {
		c.Hidden = false
		c.Unread, c.MarkedUnread = 0, false
		c.LastActivity = max(c.LastActivity, ts)
		p.saveConv(conv, c)
	}
	out := p.msgOut(r)
	p.mu.Unlock()
	emit("message", map[string]any{"item": out})
}

func (p *Pont) send(cmd Cmd) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return
	}
	p.sendCommon(cmd, text, "", "")
}

// Envoi commun texte/fichier.
func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	p.activity()
	tmp := cmd.TmpID
	fail := func(err error) {
		log.Warn().Err(err).Str("fil", cmd.Conv).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	cli, err := p.readyClient()
	if err != nil {
		fail(err)
		return
	}
	id := cli.GenerateMessageID()
	tmp = tmpOr(tmp, id)
	c := p.conv(cmd.Conv)
	if c == nil {
		fail(fmt.Errorf("conversation inconnue : %s", cmd.Conv))
		return
	}
	jid, err := types.ParseJID(cmd.Conv)
	if err != nil {
		fail(fmt.Errorf("conversation illisible : %s", cmd.Conv))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 3*time.Minute)
	defer annule()

	var reply *msgRow
	if cmd.ReplyTo != "" {
		if r := p.getMsg(cmd.ReplyTo); r != nil && r.Conv == cmd.Conv {
			reply = r
		}
	}
	var atts []Attachment
	var msg = textMessage(cli, text, reply)
	if path != "" {
		msg, atts, err = p.uploadMedia(ctx, cli, path, mime, text, reply)
		if err != nil {
			fail(fmt.Errorf("envoi du fichier : %w", err))
			return
		}
	}
	p.mu.Lock()
	p.pending[id] = tmp
	p.mu.Unlock()
	resp, err := cli.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id})
	if err != nil {
		fail(fmt.Errorf("WhatsApp : %w", err))
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	ts := resp.Timestamp.UnixMilli()
	if resp.Timestamp.IsZero() {
		ts = time.Now().UnixMilli()
	}
	shown := text
	if path != "" && len(atts) > 0 && !captionable(atts[0].Mime) {
		shown = "" // vocal ou autocollant : la légende n'est pas partie
	}
	p.echoSent(cli, cmd.Conv, jid.String(), id, tmp, shown, ts, atts, reply)
	if path != "" && text != "" && shown == "" {
		// La légende part en message séparé.
		p.sendCommon(Cmd{Conv: cmd.Conv}, text, "", "")
	}
}

// Accusé de lecture : les messages reçus depuis la dernière lecture.
func (p *Pont) markRead(cmd Cmd) {
	p.activity()
	c := p.conv(cmd.Conv)
	if c == nil {
		return
	}
	p.mu.Lock()
	wm := c.ReadWM
	wasMarked := c.MarkedUnread
	c.Unread, c.MarkedUnread = 0, false
	p.saveConv(cmd.Conv, c)
	p.listDirty = true
	p.mu.Unlock()
	cli, err := p.readyClient()
	if err != nil {
		return
	}
	rows := p.queryMsgs(cmd.Conv, 0, "", 60)
	type cle struct{ chat, sender string }
	groupes := map[cle][]string{}
	var last *msgRow
	newWM := wm
	for _, r := range rows {
		if r.FromMe || r.Kind == "admin" || strings.HasPrefix(r.ID, "tmp_") {
			continue
		}
		last = r
		if r.Time <= wm {
			continue
		}
		k := cle{cmp.Or(r.ChatJID, cmd.Conv), r.SenderJID}
		groupes[k] = append(groupes[k], r.ID)
		newWM = max(newWM, r.Time)
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	for k, ids := range groupes {
		chat, err := types.ParseJID(k.chat)
		if err != nil {
			continue
		}
		var sender types.JID
		if chat.Server == types.GroupServer && k.sender != "" {
			sender, _ = types.ParseJID(k.sender)
		}
		if err := cli.MarkRead(ctx, ids, time.Now(), chat, sender); err != nil {
			log.Warn().Err(err).Msg("lu")
		}
	}
	p.mu.Lock()
	c.ReadWM = max(c.ReadWM, newWM, c.LastTime)
	p.saveConv(cmd.Conv, c)
	p.mu.Unlock()
	if wasMarked && last != nil {
		// Marqué « non lu » sur le téléphone : on enlève la marque partout.
		chat, _ := types.ParseJID(cmp.Or(last.ChatJID, cmd.Conv))
		sender, _ := types.ParseJID(last.SenderJID)
		patch := appstate.BuildMarkChatAsRead(chat, true, time.UnixMilli(last.Time), cli.BuildMessageKey(chat, sender, last.ID))
		if err := cli.SendAppState(ctx, patch); err != nil {
			log.Warn().Err(err).Msg("lu : état des applis")
		}
	}
}

// Archive ou désarchive un fil (état des applis, comme sur le téléphone).
func (p *Pont) archive(cmd Cmd) {
	c := p.conv(cmd.Conv)
	if c == nil {
		emitErr("archive", fmt.Errorf("conversation inconnue : %s", cmd.Conv))
		return
	}
	cli, err := p.readyClient()
	if err != nil {
		emitErr("archive", fmt.Errorf("archivage : %w", err))
		return
	}
	chat, err := types.ParseJID(cmd.Conv)
	if err != nil {
		emitErr("archive", fmt.Errorf("conversation illisible : %s", cmd.Conv))
		return
	}
	var ts time.Time
	var key = (*waCommonKey)(nil)
	if rows := p.queryMsgs(cmd.Conv, 0, "", 5); len(rows) > 0 {
		last := rows[len(rows)-1]
		if !strings.HasPrefix(last.ID, "tmp_") {
			if j, err := types.ParseJID(last.ChatJID); err == nil && last.ChatJID != "" {
				chat = j
			}
			sender, _ := types.ParseJID(last.SenderJID)
			ts = time.UnixMilli(last.Time)
			key = cli.BuildMessageKey(chat, sender, last.ID)
		}
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	if err := cli.SendAppState(ctx, appstate.BuildArchive(chat, cmd.Archived, ts, key)); err != nil {
		emitErr("archive", fmt.Errorf("archivage : WhatsApp a refusé (%w)", err))
		return
	}
	p.mu.Lock()
	c.Archived = cmd.Archived
	p.saveConv(cmd.Conv, c)
	item := p.convOut(cmd.Conv, c)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item})
	p.markDirty()
}

// Nouvelle conversation : un numéro (vérifié sur WhatsApp), sinon une recherche.
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimSpace(cmd.Number)
	if q == "" {
		q = strings.TrimSpace(cmd.Text)
	}
	if q == "" {
		return
	}
	digits := onlyDigits(q)
	if len(digits) < 7 || len(digits) < len(strings.Join(strings.Fields(q), ""))/2 {
		p.search(q)
		return
	}
	cli, err := p.readyClient()
	if err != nil {
		emitErr("new", err)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	resp, err := cli.IsOnWhatsApp(ctx, []string{"+" + digits})
	if err != nil {
		emitErr("new", fmt.Errorf("vérification du numéro : %w", err))
		return
	}
	var jid types.JID
	for _, r := range resp {
		if r.IsIn {
			jid = r.JID
			if r.VerifiedName != nil && r.VerifiedName.Details != nil {
				p.mu.Lock()
				if p.names[r.JID.String()] == "" {
					p.names[r.JID.String()] = r.VerifiedName.Details.GetVerifiedName()
				}
				p.mu.Unlock()
			}
		}
	}
	if jid.IsEmpty() {
		emitErr("new", fmt.Errorf("le +%s n'est pas sur WhatsApp", digits))
		return
	}
	jid = p.canon(cli, jid)
	p.mu.Lock()
	id, c := p.getOrMakeConv(jid)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	c.Hidden = false
	p.saveConv(id, c)
	item := p.convOut(id, c)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{id})
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Recherche parmi les contacts et groupes connus : « search{items:[{id,name,avatar}]} ».
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
	needle := fold(q)
	digits := onlyDigits(q)
	vus := map[string]bool{}
	p.mu.Lock()
	add := func(id, name string) {
		if vus[id] || len(items) >= 40 {
			return
		}
		vus[id] = true
		it := item{ID: id, Name: name}
		if path, ok := avatarFresh(id); ok {
			it.Avatar = path
		}
		items = append(items, it)
	}
	// Les groupes et fils connus.
	for id, c := range p.convs {
		if c.Hidden {
			continue
		}
		co := p.convOut(id, c)
		if strings.Contains(fold(co.Name), needle) || (len(digits) >= 3 && strings.Contains(id, digits)) {
			add(id, co.Name)
		}
	}
	// Le carnet (et les noms publics) : les personnes seulement.
	for jid, name := range p.names {
		if !strings.HasSuffix(jid, "@"+types.DefaultUserServer) && !strings.HasSuffix(jid, "@"+types.HiddenUserServer) {
			continue
		}
		id := jid
		if pn := p.lidPN[jid]; pn != "" {
			id = pn
		}
		if strings.Contains(fold(name), needle) || (len(digits) >= 3 && strings.Contains(id, digits)) {
			add(id, name)
		}
	}
	p.mu.Unlock()
	slices.SortStableFunc(items, func(a, b item) int { return cmp.Compare(fold(a.Name), fold(b.Name)) })
	emit("search", map[string]any{"query": q, "items": items})
}

// Minuscules sans accents (pour la recherche).
func fold(s string) string {
	s = strings.ToLower(s)
	r := strings.NewReplacer("à", "a", "â", "a", "ä", "a", "á", "a", "ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i", "í", "i", "ô", "o", "ö", "o", "ó", "o", "ù", "u", "û", "u", "ü", "u", "ú", "u", "ñ", "n")
	return r.Replace(s)
}

// Oublie le compte : cache et état.
func (p *Pont) forgetAccount() {
	p.mu.Lock()
	p.convs = map[string]*convState{}
	p.names = map[string]string{}
	p.lidPN = map[string]string{}
	p.loaded = map[string]int64{}
	p.picIDs = map[string]string{}
	p.noPic = map[string]time.Time{}
	p.mu.Unlock()
	if err := p.wipeStore(); err != nil {
		log.Warn().Err(err).Msg("effacement du cache")
	}
}

func (p *Pont) unpair() {
	cli := p.client()
	if cli != nil && cli.Store.ID != nil {
		ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
		err := cli.Logout(ctx)
		annule()
		if err != nil {
			// Hors ligne : on oublie l'appareil ici (il restera listé sur le
			// téléphone jusqu'à ce qu'on l'y retire).
			log.Warn().Err(err).Msg("dissociation")
			if derr := cli.Store.Delete(context.WithoutCancel(p.ctx)); derr != nil {
				log.Warn().Err(derr).Msg("effacement de l'appareil")
			}
		}
	}
	p.disconnect()
	p.forgetAccount()
	emitState("unpaired")
}

func (p *Pont) shutdown() {
	p.disconnect()
	if p.db != nil {
		_ = p.db.Close()
	}
}
