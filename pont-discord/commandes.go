package main

// Les commandes de l'interface : historique, envoi, réactions, lu, « écrit… ».
// Chacune est une action de l'utilisateur ; le pont n'en fait aucune de
// lui-même.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// referer : chaque requête porte l'adresse de la conversation ouverte, comme
// le client web (/channels/@me/<salon> ou /channels/<guilde>/<salon>).
func referer(conv string) discordgo.RequestOption {
	guild, channel := splitConv(conv)
	return discordgo.WithChannelReferer(guild, channel)
}

func (p *Pont) knownConv(conv string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chans[conv] != nil
}

// ---- L'historique ----

// messages : une page d'historique (jamais marquée comme lue : seul « read »
// le fait). Hors connexion : ce qu'on a en mémoire.
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
	sess, err := p.api()
	var page []*discordgo.Message
	if err == nil {
		ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
		if err = p.rest.wait(ctx); err == nil {
			page, err = sess.ChannelMessages(rawChannel(conv), count, cmd.Before, "", "", referer(conv), discordgo.WithContext(ctx))
		}
		annule()
		if err != nil {
			log.Warn().Str("fil", conv).Str("err", errorText(err)).Msg("historique")
		}
	}
	if err != nil {
		items, more := p.cachedPage(conv, cmd.Before, count)
		emit("messages", map[string]any{"conv": conv, "items": items, "older": older, "more": more})
		return
	}
	items := make([]Message, 0, len(page))
	p.mu.Lock()
	for i := len(page) - 1; i >= 0; i-- { // Discord rend du plus récent au plus vieux
		dm := page[i]
		if dm == nil {
			continue
		}
		dm.ChannelID = rawChannel(conv)
		if guild, _ := splitConv(conv); guild != "" {
			dm.GuildID = guild
		}
		if dm.Author != nil && dm.Author.Username != "" {
			p.users[dm.Author.ID] = dm.Author
		}
		m := convertMessage(dm, p)
		m.Conv = conv
		m.MentionsMe, m.ReplyToMe = p.mentionsMe(dm), p.replyToMe(dm, conv)
		if old := p.recalled(conv, m.ID); old != nil {
			m.TmpID = old.TmpID
			if len(old.Reactions) == len(m.Reactions) {
				m.Reactions = old.Reactions // celles qui savent qui
			}
		}
		p.remember(m)
		items = append(items, m)
	}
	changed := false
	if c := p.chans[conv]; c != nil && !older && len(items) > 0 {
		changed = p.noteLast(c, items[len(items)-1])
	}
	p.mu.Unlock()
	emit("messages", map[string]any{"conv": conv, "items": items, "older": older, "more": len(page) >= count})
	if changed {
		p.emitConv(conv, false)
	}
}

// cachedPage : une page tirée de la mémoire (hors connexion).
func (p *Pont) cachedPage(conv, before string, count int) ([]Message, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.msgs[conv]
	end := len(l)
	if before != "" {
		for end > 0 && compareIDs(l[end-1].ID, before) >= 0 {
			end--
		}
	}
	start := max(0, end-count)
	return append([]Message{}, l[start:end]...), start > 0
}

// ---- L'envoi ----

// explainSend : les refus courants de Discord, en clair.
func explainSend(err error) error {
	var re *discordgo.RESTError
	if errors.As(err, &re) {
		if re.Message != nil {
			switch re.Message.Code {
			case 50001, 50013:
				return errors.New("Discord refuse : vous n'avez pas le droit d'écrire dans ce salon")
			case 50007:
				return errors.New("Discord refuse : cette personne n'accepte pas vos messages privés")
			case 40002, 40003:
				return errors.New("Discord demande de vérifier le compte : ouvrez l'app Discord")
			case 50035:
				return fmt.Errorf("Discord a refusé le message : %s", re.Message.Message)
			case 40005:
				return errors.New("fichier trop gros pour Discord")
			case 20016, 20028:
				return errors.New("Discord limite les envois pour l'instant : réessayez plus tard")
			}
		}
		if strings.Contains(string(re.ResponseBody), "captcha_key") {
			return errors.New("Discord demande un captcha : envoyez ce message depuis l'app Discord")
		}
		if re.Response != nil && re.Response.StatusCode == http.StatusTooManyRequests {
			return errors.New("Discord limite les envois pour l'instant : réessayez plus tard")
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("Discord ne répond pas (délai dépassé)")
	}
	return fmt.Errorf("Discord : %s", errorText(err))
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

func (p *Pont) sendCommon(cmd Cmd, text, path, mime string) {
	tmp := firstOf(cmd.TmpID, fmt.Sprintf("tmp_%d", time.Now().UnixNano()))
	fail := func(err error) {
		log.Warn().Str("err", err.Error()).Str("fil", cmd.Conv).Msg("envoi")
		emitErr("send", err)
		emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": false})
	}
	if !p.knownConv(cmd.Conv) {
		fail(errors.New("conversation Discord inconnue"))
		return
	}
	sess, err := p.api()
	if err != nil {
		fail(err)
		return
	}
	if len([]rune(text)) > 2000 {
		fail(errors.New("Discord limite un message à 2000 caractères"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 5*time.Minute)
	defer annule()
	req := &discordgo.MessageSend{Content: text, Nonce: generateNonce()}
	if cmd.ReplyTo != "" {
		guild, channel := splitConv(cmd.Conv)
		req.Reference = &discordgo.MessageReference{MessageID: cmd.ReplyTo, ChannelID: channel, GuildID: guild}
	}
	var local []Attachment
	if path != "" {
		att, l, err := p.upload(ctx, sess, cmd.Conv, path, mime)
		if err != nil {
			fail(err)
			return
		}
		req.Attachments = []*discordgo.MessageAttachment{att}
		local = l
	}
	p.mu.Lock()
	p.pending[req.Nonce] = tmp
	p.mu.Unlock()
	if err := p.rest.wait(ctx); err != nil {
		fail(err)
		return
	}
	dm, err := sess.ChannelMessageSendComplex(rawChannel(cmd.Conv), req, referer(cmd.Conv), discordgo.WithContext(ctx))
	if err != nil {
		p.mu.Lock()
		delete(p.pending, req.Nonce)
		p.mu.Unlock()
		fail(explainSend(err))
		return
	}
	emit("sent", map[string]any{"conv": cmd.Conv, "tmp_id": tmp, "ok": true})
	dm.ChannelID = rawChannel(cmd.Conv)
	p.mu.Lock()
	m := convertMessage(dm, p)
	m.Conv = cmd.Conv
	m.TmpID = tmp
	if len(local) > 0 && len(m.Attachments) == len(local) {
		// La copie locale : l'interface l'a sans téléchargement.
		for i := range m.Attachments {
			m.Attachments[i].MediaID = local[i].MediaID
		}
	}
	delete(p.pending, req.Nonce)
	p.remember(m)
	changed := false
	if c := p.chans[cmd.Conv]; c != nil {
		changed = p.noteLast(c, m)
	}
	p.mu.Unlock()
	emit("message", map[string]any{"item": m})
	if changed {
		p.emitConv(cmd.Conv, false)
	}
}

// ---- Réactions ----

// react : une réaction à la fois, comme les autres services de Jasette :
// la nouvelle remplace la mienne ; vide, la mienne est retirée.
func (p *Pont) react(cmd Cmd) {
	fail := func(err error) {
		emit("error", map[string]any{"where": "react", "text": err.Error(), "conv": cmd.Conv, "id": cmd.ID})
	}
	if !p.knownConv(cmd.Conv) || cmd.ID == "" {
		fail(errors.New("message inconnu"))
		return
	}
	sess, err := p.api()
	if err != nil {
		fail(err)
		return
	}
	emoji := strings.TrimSpace(cmd.Emoji)
	var mine []string
	p.mu.Lock()
	if m := p.recalled(cmd.Conv, cmd.ID); m != nil {
		for _, r := range m.Reactions {
			if r.FromMe && r.Emoji != emoji && !strings.HasPrefix(r.Emoji, ":") {
				mine = append(mine, r.Emoji)
			}
		}
	}
	p.mu.Unlock()
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	for _, e := range mine {
		if err := p.rest.wait(ctx); err != nil {
			fail(err)
			return
		}
		if err := sess.MessageReactionRemoveUser("", rawChannel(cmd.Conv), cmd.ID, e, "@me", discordgo.WithContext(ctx)); err != nil {
			log.Warn().Str("err", errorText(err)).Msg("retrait de réaction")
		}
	}
	if emoji == "" {
		return
	}
	if err := p.rest.wait(ctx); err != nil {
		fail(err)
		return
	}
	if err := sess.MessageReactionAddUser("", rawChannel(cmd.Conv), cmd.ID, emoji, discordgo.WithContext(ctx)); err != nil {
		fail(explainSend(err))
	}
	// La passerelle renvoie MESSAGE_REACTION_ADD : l'interface se met à jour par là.
}

// ---- Lu ----

// markRead : marque le fil comme lu chez Discord (et donc sur les autres
// appareils). La seule commande qui le fait, sur une action de l'utilisateur.
func (p *Pont) markRead(cmd Cmd) {
	p.mu.Lock()
	c := p.chans[cmd.Conv]
	if c == nil || c.lastID == "" || compareIDs(c.lastID, c.readID) <= 0 {
		p.mu.Unlock()
		return
	}
	last := c.lastID
	c.readID = last
	c.mentions = 0
	p.mu.Unlock()
	p.emitConv(cmd.Conv, false)
	sess, err := p.api()
	if err != nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if p.rest.wait(ctx) != nil {
		return
	}
	if _, err := sess.ChannelMessageAckNoToken(rawChannel(cmd.Conv), last, referer(cmd.Conv), discordgo.WithContext(ctx)); err != nil {
		log.Warn().Str("err", errorText(err)).Str("fil", cmd.Conv).Msg("lu")
	}
}

// ---- « écrit… » ----

// sendTyping : Discord garde l'indicateur dix secondes ; on ne le renvoie
// pas plus d'une fois toutes les huit secondes.
func (p *Pont) sendTyping(cmd Cmd) {
	if !cmd.On || !p.knownConv(cmd.Conv) {
		return
	}
	p.mu.Lock()
	if time.Since(p.typed[cmd.Conv]) < 8*time.Second {
		p.mu.Unlock()
		return
	}
	p.typed[cmd.Conv] = time.Now()
	p.mu.Unlock()
	sess, err := p.api()
	if err != nil {
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
	defer annule()
	if p.rest.wait(ctx) != nil {
		return
	}
	_ = sess.ChannelTyping(rawChannel(cmd.Conv), referer(cmd.Conv), discordgo.WithContext(ctx))
}

// ---- Nouvelle conversation ----

// newChat : un message privé avec une personne (son id Discord, rendu par
// « search »). S'il existe déjà, il s'ouvre simplement.
func (p *Pont) newChat(cmd Cmd) {
	q := strings.TrimPrefix(strings.TrimSpace(firstOf(cmd.Number, cmd.Text, cmd.ID)), "d:")
	if q == "" {
		return
	}
	p.mu.Lock()
	if p.chans[q] != nil {
		p.mu.Unlock()
		p.emitConv(q, true)
		return
	}
	for id, c := range p.chans {
		if c.ch.Type == discordgo.ChannelTypeDM {
			for _, u := range p.recipients(c) {
				if u.ID == q {
					p.mu.Unlock()
					p.emitConv(id, true)
					return
				}
			}
		}
	}
	_, connu := p.users[q]
	p.mu.Unlock()
	if !connu || strings.Trim(q, "0123456789") != "" {
		p.search(q)
		return
	}
	sess, err := p.api()
	if err != nil {
		emitErr("new", err)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	if err := p.rest.wait(ctx); err != nil {
		emitErr("new", err)
		return
	}
	ch, err := sess.UserChannelCreate(q, discordgo.WithContext(ctx))
	if err != nil {
		emitErr("new", explainSend(err))
		return
	}
	p.mu.Lock()
	p.addChannel(ch)
	p.mu.Unlock()
	p.emitConv(ch.ID, true)
	go p.fetchAvatars([]string{ch.ID})
}

// Un groupe privé Discord : dix personnes au plus, moi compris.
const maxGroupe = 9

// createGroup : un groupe privé avec ces personnes (PROTOCOLE.md §5.6 bis),
// comme le client web : POST /users/@me/channels avec « recipients », puis
// le nom, s'il y en a un.
func (p *Pont) createGroup(cmd Cmd) {
	var ids []string
	vus := map[string]bool{}
	for _, m := range cmd.Members {
		m = strings.TrimPrefix(strings.TrimSpace(m), "d:")
		if m == "" || vus[m] {
			continue
		}
		if strings.Trim(m, "0123456789") != "" {
			emitErr("create_group", fmt.Errorf("personne introuvable : %s", m))
			return
		}
		vus[m] = true
		ids = append(ids, m)
	}
	if len(ids) < 2 {
		emitErr("create_group", errors.New("il faut au moins deux personnes"))
		return
	}
	if len(ids) > maxGroupe {
		emitErr("create_group", fmt.Errorf("un groupe Discord compte %d personnes au plus, toi compris", maxGroupe+1))
		return
	}
	sess, err := p.api()
	if err != nil {
		emitErr("create_group", err)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	if err := p.rest.wait(ctx); err != nil {
		emitErr("create_group", err)
		return
	}
	body, err := sess.RequestWithBucketID(http.MethodPost, discordgo.EndpointUserChannels("@me"),
		map[string]any{"recipients": ids}, discordgo.EndpointUserChannels(""), discordgo.WithContext(ctx))
	if err != nil {
		emitErr("create_group", explainSend(err))
		return
	}
	var ch discordgo.Channel
	if err := json.Unmarshal(body, &ch); err != nil || ch.ID == "" {
		emitErr("create_group", errors.New("réponse de Discord illisible"))
		return
	}
	if nom := strings.TrimSpace(cmd.Name); nom != "" {
		if len([]rune(nom)) > 100 {
			nom = string([]rune(nom)[:100])
		}
		if p.rest.wait(ctx) == nil {
			if edit, err := sess.ChannelEdit(ch.ID, &discordgo.ChannelEdit{Name: nom}, referer(ch.ID), discordgo.WithContext(ctx)); err == nil && edit != nil {
				ch.Name = edit.Name
			} else if err != nil {
				log.Warn().Err(err).Msg("nom du groupe")
			}
		}
	}
	p.mu.Lock()
	p.addChannel(&ch)
	p.mu.Unlock()
	p.emitConv(ch.ID, true)
}
