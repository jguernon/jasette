package main

// Les commandes de l'interface : historique, envoi, réactions, lu,
// demandes de message. Chacune est une action de l'utilisateur ; le pont
// n'en fait aucune de lui-même.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func (p *Pont) knownConv(conv string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.salons[conv] != nil
}

// ---- L'historique ----

type pageMessages struct {
	Chunk []mxEvent `json:"chunk"`
	State []mxEvent `json:"state"`
	Start string    `json:"start"`
	End   string    `json:"end"`
}

// Le filtre du client web : sans les événements de modération, membres
// chargés au besoin.
const filtreHistorique = `{"not_types":["com.reddit.review_open","com.reddit.review_close"],"lazy_load_members":true}`

// pageHistorique : une page de /messages, du plus récent vers le plus vieux.
func (p *Pont) pageHistorique(ctx context.Context, cli *mautrix.Client, conv, from string, limit int) (*pageMessages, error) {
	q := map[string]string{"dir": "b", "limit": itoa(limit), "filter": filtreHistorique}
	if from != "" {
		q["from"] = from
	}
	u := cli.BuildURLWithQuery(mautrix.ClientURLPath{"v3", "rooms", conv, "messages"}, q)
	var r pageMessages
	_, err := cli.MakeFullRequest(ctx, mautrix.FullRequest{Method: "GET", URL: u, ResponseJSON: &r, MaxAttempts: 2})
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// finHistorique : plus rien avant (pas de jeton, ou le jeton terminal de Reddit).
func finHistorique(end string) bool { return end == "" || end == "t0_0" }

// messages : une page d'historique (jamais marquée comme lue : seul « read »
// le fait). Une seule page par demande (pas de rattrapage en masse). Hors
// connexion : ce qu'on a en mémoire.
func (p *Pont) messages(cmd Cmd) {
	conv := cmd.Conv
	count := int(cmd.Count)
	if count <= 0 {
		count = 30
	}
	count = min(count, maxPage)
	older := cmd.Before != ""
	if !p.knownConv(conv) {
		emit("messages", map[string]any{"conv": conv, "items": []Message{}, "older": older, "more": false})
		return
	}
	from := ""
	if older {
		p.mu.Lock()
		from = p.older[conv]
		p.mu.Unlock()
		if from == "" {
			items, more := p.cachedPage(conv, cmd.Before, count)
			emit("messages", map[string]any{"conv": conv, "items": items, "older": older, "more": more})
			return
		}
	}
	cli, err := p.api()
	var page *pageMessages
	if err == nil {
		ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
		page, err = p.pageHistorique(ctx, cli, conv, from, count)
		annule()
		if err != nil {
			log.Warn().Str("fil", conv).Str("err", erreurCourte(err)).Msg("historique")
		}
	}
	if err != nil {
		items, more := p.cachedPage(conv, cmd.Before, count)
		emit("messages", map[string]any{"conv": conv, "items": items, "older": older, "more": more})
		return
	}
	items := p.appliquerPage(conv, page, older)
	emit("messages", map[string]any{"conv": conv, "items": items, "older": older, "more": !finHistorique(page.End) && len(page.Chunk) > 0})
	go p.auteursDe(conv, items)
	if !older && len(items) > 0 {
		p.emitConv(conv)
	}
}

// appliquerPage : les messages d'une page /messages, du plus vieux au plus
// récent, gardés en mémoire. Les réactions de l'historique ne viennent pas
// (/messages les omet ; les demander message par message serait une rafale).
func (p *Pont) appliquerPage(conv string, page *pageMessages, older bool) []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.salons[conv]
	if s == nil {
		return []Message{}
	}
	for _, ev := range page.State {
		if s.noteState(ev, p.me) {
			p.sale = true
		}
	}
	p.older[conv] = ""
	if !finHistorique(page.End) {
		p.older[conv] = page.End
	}
	// Les profils et réactions visent des messages : les messages d'abord.
	evs := append([]mxEvent(nil), page.Chunk...)
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].TS < evs[j].TS })
	items := []Message{}
	ids := map[string]int{}
	for _, ev := range evs {
		if !estMessage(ev.Type) {
			continue
		}
		m, ok := convertir(ev, conv, p.me, func(uid string) string { return p.nomDe(s, uid) })
		if !ok {
			continue
		}
		p.completer(s, &m)
		p.remember(m)
		if old := p.recalled(conv, m.ID); old != nil {
			m = *old
		}
		ids[m.ID] = len(items)
		items = append(items, m)
	}
	for _, ev := range evs {
		var m Message
		var ok bool
		switch ev.Type {
		case "com.reddit.profile":
			m, ok, _ = p.profil(s, ev)
		case "m.reaction":
			m, ok, _ = p.reaction(s, ev)
		case "m.room.redaction":
			m, ok, _ = p.redaction(s, ev)
		}
		if i, dans := ids[m.ID]; ok && dans {
			items[i] = m
		}
	}
	if !older && len(items) > 0 {
		p.noteLast(s, items[len(items)-1])
	}
	return items
}

// cachedPage : une page tirée de la mémoire (hors connexion).
func (p *Pont) cachedPage(conv, before string, count int) ([]Message, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.msgs[conv]
	end := len(l)
	if before != "" {
		for i, m := range l {
			if m.ID == before {
				end = i
				break
			}
		}
	}
	start := max(0, end-count)
	return append([]Message{}, l[start:end]...), start > 0
}

// ---- L'envoi ----

// explainSend : les refus courants de Reddit, en clair.
func explainSend(err error) error {
	var he mautrix.HTTPError
	if errors.As(err, &he) && he.RespError != nil {
		re := he.RespError
		msg := re.Err
		if m, ok := re.ExtraData["com.reddit.error.msg"].(string); ok && m != "" {
			msg = m
		}
		switch {
		case re.ErrCode == "M_LIMIT_EXCEEDED" || re.StatusCode == http.StatusTooManyRequests:
			return errors.New("Reddit limite les envois pour l'instant : réessayez plus tard")
		case strings.Contains(msg, "reaction key"):
			return errors.New("Reddit n'accepte que les réactions de sa palette")
		case re.ErrCode == "M_FORBIDDEN":
			return fmt.Errorf("Reddit refuse : %s", firstOf(msg, "action interdite dans ce salon"))
		case re.ErrCode == "M_TOO_LARGE":
			return errors.New("fichier trop gros pour Reddit")
		}
		if msg != "" {
			return fmt.Errorf("Reddit : %s", msg)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("Reddit ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("Reddit : %s", erreurCourte(err))
}

func (p *Pont) send(cmd Cmd) {
	text := strings.TrimSpace(cmd.Text)
	if text == "" {
		return
	}
	p.sendCommon(cmd, text, "", "")
}

func (p *Pont) sendMedia(cmd Cmd) {
	p.sendCommon(cmd, strings.TrimSpace(cmd.Text), cmd.Path, cmd.Mime)
}

// lireImage : le fichier à envoyer, s'il est une image que Reddit accepte.
func lireImage(path string) ([]byte, string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if st.Size() > maxGIF {
		return nil, "", errors.New("fichier trop gros pour Reddit (100 Mo au plus pour un GIF, 20 Mo pour une image)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	mime := http.DetectContentType(data)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	lim := plafondImage(mime)
	if lim == 0 {
		return nil, "", errors.New("Reddit n'accepte que les images (JPEG, PNG, GIF, WebP) : ni vidéo, ni son, ni fichier")
	}
	if int64(len(data)) > lim {
		if mime == "image/gif" {
			return nil, "", errors.New("GIF trop gros pour Reddit (100 Mo au plus)")
		}
		return nil, "", errors.New("image trop grosse pour Reddit (20 Mo au plus)")
	}
	return data, mime, nil
}

// relationReponse : la réponse, comme le client de Reddit (un fil m.thread
// dont la racine est le message cité, ou la racine du fil où il est).
func relationReponse(cible, racine string) map[string]any {
	if racine == "" {
		racine = cible
	}
	rel := map[string]any{
		"rel_type": "m.thread", "event_id": racine,
		"is_falling_back": cible == racine,
		"m.in_reply_to":   map[string]any{"event_id": cible},
	}
	return rel
}

func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	tmp := firstOf(cmd.TmpID, fmt.Sprintf("tmp_%d", time.Now().UnixNano()))
	fail := func(err error) {
		log.Warn().Str("err", err.Error()).Str("fil", cmd.Conv).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	if !p.knownConv(cmd.Conv) {
		fail(errors.New("conversation Reddit inconnue"))
		return
	}
	if texteTropLong(text) {
		fail(errors.New("Reddit limite un message à 10 000 caractères"))
		return
	}
	cli, err := p.api()
	if err != nil {
		fail(err)
		return
	}
	var data []byte
	if path != "" {
		if data, mime, err = lireImage(path); err != nil {
			fail(err)
			return
		}
	}
	ctx, annule := context.WithTimeout(p.ctx, 5*time.Minute)
	defer annule()
	// Répondre à une demande de message, c'est l'accepter (le geste de
	// l'utilisateur) : Reddit refuse d'écrire dans un salon pas rejoint.
	p.mu.Lock()
	demande := false
	if s := p.salons[cmd.Conv]; s != nil {
		demande = s.Invite
	}
	racine := ""
	if cmd.ReplyTo != "" {
		if old := p.recalled(cmd.Conv, cmd.ReplyTo); old != nil {
			racine = old.racine // la racine de son fil, s'il est dans un fil
		}
	}
	p.mu.Unlock()
	if demande {
		if err := p.rejoindre(ctx, cli, cmd.Conv); err != nil {
			fail(explainSend(err))
			return
		}
	}
	var contents []map[string]any
	if data != nil {
		up, err := cli.UploadBytes(ctx, data, mime)
		if err != nil {
			fail(explainSend(err))
			return
		}
		info := map[string]any{"mimetype": mime, "size": len(data)}
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			info["w"], info["h"] = cfg.Width, cfg.Height
		}
		contents = append(contents, map[string]any{"msgtype": "m.image", "body": "Image", "url": up.ContentURI.String(), "info": info})
	}
	// La légende d'une image : un message à part (Reddit ne l'affiche pas).
	if text != "" {
		contents = append(contents, map[string]any{"msgtype": "m.text", "body": text})
	}
	if cmd.ReplyTo != "" && len(contents) > 0 {
		contents[0]["m.relates_to"] = relationReponse(cmd.ReplyTo, racine)
	}
	for i, c := range contents {
		txn := tmp
		if i > 0 {
			txn = fmt.Sprintf("%s.%d", tmp, i)
		}
		resp, err := cli.SendMessageEvent(ctx, id.RoomID(cmd.Conv), event.EventMessage, c, mautrix.ReqSendEvent{TransactionID: txn})
		if err != nil {
			fail(explainSend(err))
			return
		}
		raw, _ := json.Marshal(c)
		ev := mxEvent{Type: "m.room.message", Sender: cli.UserID.String(), ID: resp.EventID.String(), TS: time.Now().UnixMilli(), Content: raw}
		ev.Unsigned.TxnID = txn
		p.mu.Lock()
		s := p.salons[cmd.Conv]
		var m Message
		var ok, changed bool
		if s != nil {
			m, ok = convertir(ev, cmd.Conv, p.me, func(u string) string { return p.nomDe(s, u) })
			if ok {
				if i == 0 {
					m.TmpID = tmp
				}
				if data != nil && i == 0 && len(m.Attachments) == 1 {
					// La copie locale : l'interface l'a sans téléchargement.
					m.Attachments[0].Name = "image" + extension(mime, data)
				}
				p.completer(s, &m)
				p.remember(m)
				changed = p.noteLast(s, m)
			}
		}
		p.mu.Unlock()
		if i == 0 {
			emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
		}
		if ok {
			emit("message", map[string]any{"item": m})
		}
		if changed {
			p.emitConv(cmd.Conv)
		}
	}
}

// ---- Réactions ----

// react : une réaction à la fois, comme les autres services de Jasette : la
// nouvelle remplace la mienne ; vide, la mienne est retirée. Seules les
// clés de la palette de Reddit passent.
func (p *Pont) react(cmd Cmd) {
	fail := func(err error) {
		emit("error", map[string]any{"where": "react", "text": err.Error(), "conv": cmd.Conv, "id": cmd.ID})
	}
	if !p.knownConv(cmd.Conv) || cmd.ID == "" {
		fail(errors.New("message inconnu"))
		return
	}
	cle := ""
	if strings.TrimSpace(cmd.Emoji) != "" {
		if cle = cleReaction(cmd.Emoji); cle == "" {
			fail(errors.New("Reddit n'a pas cette réaction : sa palette est fixe (👍 👎 😂 😍 😢 😮 😡 🤔 😎 👋…)"))
			return
		}
	}
	cli, err := p.api()
	if err != nil {
		fail(err)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	var miennes []string
	p.mu.Lock()
	for evID, r := range p.reacts {
		if r.conv == cmd.Conv && r.target == cmd.ID && r.sender == p.me && r.key != cle {
			miennes = append(miennes, evID)
		}
	}
	p.mu.Unlock()
	for _, evID := range miennes {
		if _, err := cli.RedactEvent(ctx, id.RoomID(cmd.Conv), id.EventID(evID)); err != nil {
			log.Warn().Str("err", erreurCourte(err)).Msg("retrait de réaction")
		}
	}
	if cle == "" {
		return
	}
	c := map[string]any{"m.relates_to": map[string]any{"rel_type": "m.annotation", "event_id": cmd.ID, "key": cle}}
	if _, err := cli.SendMessageEvent(ctx, id.RoomID(cmd.Conv), event.EventReaction, c); err != nil {
		fail(explainSend(err))
	}
	// /sync renvoie la réaction : l'interface se met à jour par là.
}

// ---- Lu ----

// markRead : un accusé m.read sur le dernier message. La seule commande qui
// marque lu, sur une action de l'utilisateur. Rien pour une demande pas
// acceptée (l'expéditeur le verrait).
func (p *Pont) markRead(cmd Cmd) {
	p.mu.Lock()
	s := p.salons[cmd.Conv]
	if s == nil || s.Invite || s.LastID == "" || (!s.unread() && s.Mentions == 0) {
		p.mu.Unlock()
		return
	}
	last := s.LastID
	s.ReadTS = s.LastTS
	s.Unread, s.Mentions = 0, 0
	p.sale = true
	p.mu.Unlock()
	p.emitConv(cmd.Conv)
	cli, err := p.api()
	if err != nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if err := cli.SendReceipt(ctx, id.RoomID(cmd.Conv), id.EventID(last), event.ReceiptTypeRead, nil); err != nil {
		log.Warn().Str("err", erreurCourte(err)).Str("fil", cmd.Conv).Msg("lu")
	}
}

// ---- Demandes de message ----

func (p *Pont) rejoindre(ctx context.Context, cli *mautrix.Client, conv string) error {
	if _, err := cli.JoinRoom(ctx, conv, nil); err != nil {
		return err
	}
	p.mu.Lock()
	if s := p.salons[conv]; s != nil {
		s.Invite = false
		s.Hidden = false
		p.sale = true
	}
	p.mu.Unlock()
	return nil
}

// acceptRequest : « Accepter » (rejoindre le salon ; l'expéditeur le voit).
func (p *Pont) acceptRequest(cmd Cmd) {
	cli, err := p.api()
	if err == nil && !p.knownConv(cmd.Conv) {
		err = errors.New("conversation Reddit inconnue")
	}
	if err == nil {
		ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
		err = p.rejoindre(ctx, cli, cmd.Conv)
		annule()
	}
	if err != nil {
		emitErr("accept", explainSend(err))
		return
	}
	p.emitConv(cmd.Conv)
}

// ignoreRequest : « Ignorer », comme Reddit : la conversation à deux est
// cachée (com.reddit.hidden_chat, sans rien dire à l'expéditeur) ; une
// invitation à un groupe est refusée (on quitte le salon).
func (p *Pont) ignoreRequest(cmd Cmd) {
	cli, err := p.api()
	p.mu.Lock()
	s := p.salons[cmd.Conv]
	groupe := s != nil && s.sorte() != sorteDirect
	p.mu.Unlock()
	if err == nil && s == nil {
		err = errors.New("conversation Reddit inconnue")
	}
	if err == nil {
		ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
		if groupe {
			_, err = cli.LeaveRoom(ctx, id.RoomID(cmd.Conv))
		} else {
			err = cli.SetRoomAccountData(ctx, id.RoomID(cmd.Conv), "com.reddit.hidden_chat", map[string]any{"hidden": true})
		}
		annule()
	}
	if err != nil {
		emitErr("ignore", explainSend(err))
		return
	}
	p.mu.Lock()
	if s := p.salons[cmd.Conv]; s != nil {
		s.Hidden = true
		p.sale = true
	}
	p.mu.Unlock()
	emit("deleted", map[string]any{"conv": cmd.Conv})
}

// ---- Les salons que /sync n'a pas donnés ----

// Au plus tant de salons lus à la main après la première synchronisation.
const maxReconcilier = 60

// reconcilier : /joined_rooms, puis l'état des salons inconnus (type, nom,
// membres, dernier message), un à la fois et au rythme REST.
func (p *Pont) reconcilier(ctx context.Context, gen int, fromLogin bool) {
	cli, err := p.api()
	if err != nil {
		return
	}
	cctx, annule := context.WithTimeout(ctx, 30*time.Second)
	jr, err := cli.JoinedRooms(cctx)
	annule()
	if err != nil {
		log.Warn().Str("err", erreurCourte(err)).Msg("joined_rooms")
		return
	}
	p.mu.Lock()
	var manquants []string
	for _, r := range jr.JoinedRooms {
		s := p.salons[r.String()]
		if s == nil || (s.Type == "" && s.LastID == "") {
			manquants = append(manquants, r.String())
		}
	}
	p.mu.Unlock()
	if len(manquants) == 0 {
		return
	}
	log.Info().Int("salons", len(manquants)).Msg("salons absents de /sync : lecture de leur état")
	if len(manquants) > maxReconcilier {
		manquants = manquants[:maxReconcilier]
	}
	for _, rid := range manquants {
		if ctx.Err() != nil {
			return
		}
		p.lireSalon(ctx, cli, rid)
	}
	p.mu.Lock()
	cur := p.gen == gen
	salons := p.salons
	saveSalons(salons)
	p.sale = false
	p.mu.Unlock()
	if cur {
		p.listConversations(false)
	}
}

// lireSalon : l'état d'un salon par lectures séparées (/state entier est
// refusé par Reddit), puis son dernier message.
func (p *Pont) lireSalon(ctx context.Context, cli *mautrix.Client, rid string) {
	cctx, annule := context.WithTimeout(ctx, time.Minute)
	defer annule()
	lire := func(t, cle string) map[string]any {
		var c map[string]any
		u := cli.BuildClientURL("v3", "rooms", rid, "state", t, cle)
		if _, err := cli.MakeFullRequest(cctx, mautrix.FullRequest{Method: "GET", URL: u, ResponseJSON: &c, MaxAttempts: 1}); err != nil {
			return nil
		}
		return c
	}
	evs := []mxEvent{}
	ajouter := func(t string, c map[string]any) {
		if c == nil {
			return
		}
		raw, _ := json.Marshal(c)
		vide := ""
		evs = append(evs, mxEvent{Type: t, StateKey: &vide, Content: raw})
	}
	ajouter("com.reddit.chat.type", lire("com.reddit.chat.type", ""))
	ajouter("m.room.name", lire("m.room.name", ""))
	var membres struct {
		Chunk []mxEvent `json:"chunk"`
	}
	u := cli.BuildURLWithQuery(mautrix.ClientURLPath{"v3", "rooms", rid, "members"}, map[string]string{"membership": "join"})
	_, _ = cli.MakeFullRequest(cctx, mautrix.FullRequest{Method: "GET", URL: u, ResponseJSON: &membres, MaxAttempts: 1})
	page, err := p.pageHistorique(cctx, cli, rid, "", 5)
	p.mu.Lock()
	s := p.salonDe(rid)
	for _, ev := range append(evs, membres.Chunk...) {
		s.noteState(ev, p.me)
	}
	p.noterType(s)
	p.mu.Unlock()
	if err == nil {
		p.appliquerPage(rid, page, false)
	}
}

// ---- Recherche ----

// search : parmi les personnes des conversations connues (rien n'est
// demandé à Reddit). Jasette ne commence pas de conversation Reddit : les
// résultats ouvrent la conversation existante.
func (p *Pont) search(q string) {
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	items := []item{}
	defer func() { emit("search", map[string]any{"query": q, "items": items}) }()
	lq := plier(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(q), "u/"), "@"))
	if lq == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.salons {
		if s.sorte() != sorteDirect || s.Hidden {
			continue
		}
		c := s.convOut(p.me, p.noms)
		if strings.Contains(plier(c.Name), lq) {
			items = append(items, item{ID: s.ID, Name: c.Name})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	if len(items) > 30 {
		items = items[:30]
	}
}

// searchMessages : les messages en mémoire dont le texte contient « text ».
func (p *Pont) searchMessages(text string, limit int) {
	items := []map[string]any{}
	defer func() {
		emit("search_messages", map[string]any{"query": text, "items": items})
	}()
	q := plier(strings.TrimSpace(text))
	if q == "" {
		return
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	p.mu.Lock()
	var hits []Message
	for _, l := range p.msgs {
		for _, m := range l {
			if m.Kind != "admin" && strings.Contains(plier(m.Text), q) {
				hits = append(hits, m)
			}
		}
	}
	p.mu.Unlock()
	sort.Slice(hits, func(i, j int) bool { return hits[i].Time > hits[j].Time })
	for i, m := range hits {
		if i >= limit {
			break
		}
		items = append(items, map[string]any{
			"conv": m.Conv, "id": m.ID, "text": m.Text, "sender": m.Sender, "time": m.Time,
			"from_me": m.FromMe, "fromMe": m.FromMe,
		})
	}
}

// plier : minuscules, sans accents (comme `plier` de l'interface).
func plier(s string) string {
	r := strings.NewReplacer("à", "a", "â", "a", "ä", "a", "á", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ç", "c", "œ", "oe", "æ", "ae")
	return r.Replace(strings.ToLower(s))
}

