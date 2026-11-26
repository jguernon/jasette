package main

// Le direct : le flux « realtime » de linkedin.com (messages, conversations,
// « écrit », lu, réactions). Aucun accusé de réception n'est renvoyé.

import (
	"time"

	"go.mau.fi/mautrix-linkedin/pkg/linkedingo"
)

func (p *Pont) evenement(gen int, de *linkedingo.DecoratedEvent) {
	d := de.Payload.Data
	switch de.Topic.NthPrefixPart(2) {
	case linkedingo.RealtimeEventTopicConversations, linkedingo.RealtimeEventTopicTabBadgeUpdate:
		if d.DecoratedConversation != nil && !d.DecoratedConversation.Result.EntityURN.IsEmpty() {
			_, me, err := p.client()
			if err == nil {
				p.appliquerConvs([]linkedingo.Conversation{d.DecoratedConversation.Result}, me, false)
			}
		} else {
			p.planDelta(gen)
		}
	case linkedingo.RealtimeEventTopicConversationDelete:
		if d.DecoratedConversationDelete != nil {
			p.mu.Lock()
			id := court(d.DecoratedConversationDelete.Result.EntityURN, p.me)
			p.mu.Unlock()
			if id != "" {
				p.supprimee(id)
			}
		}
	case linkedingo.RealtimeEventTopicMessages:
		if d.DecoratedMessage != nil {
			p.messageDirect(gen, d.DecoratedMessage.Result)
		}
	case linkedingo.RealtimeEventTopicTypingIndicators:
		if d.DecoratedTypingIndicator != nil {
			p.ecrit(d.DecoratedTypingIndicator.Result)
		}
	case linkedingo.RealtimeEventTopicMessageSeenReceipts:
		if d.DecoratedSeenReceipt != nil {
			p.vu(d.DecoratedSeenReceipt.Result)
		}
	case linkedingo.RealtimeEventTopicMessageReactionSummaries:
		if d.DecoratedReactionSummary != nil {
			p.reactionDirect(d.DecoratedReactionSummary.Result)
		}
	}
}

// messageDirect : un message nouveau, modifié ou retiré.
func (p *Pont) messageDirect(gen int, lm linkedingo.Message) {
	p.mu.Lock()
	me := p.me
	cid := court(convDe(lm), me)
	if cid == "" || court(lm.EntityURN, me) == "" {
		p.mu.Unlock()
		return
	}
	c := p.convs[cid]
	inconnue := c == nil
	if inconnue {
		c = &conv{ID: cid}
		p.convs[cid] = c
	}
	if len(lm.Conversation.ConversationParticipants) > 0 {
		appliquerConv(c, lm.Conversation, me)
	} else if inconnue {
		defer p.planDelta(gen)
	}
	m := convertir(lm, cid, me, p.nomDe(c))
	if old := p.trouver(cid, m.ID); old != nil && lm.MessageBodyRenderFormat == linkedingo.MessageBodyRenderFormatRecalled {
		m = retire(*old)
	}
	neuf := p.trouver(cid, m.ID) == nil
	if m.FromMe {
		// Le retour d'un message envoyé d'ici : le même tmp_id.
		l := p.envois[cid]
		for i, e := range l {
			if e.texte == m.corps || (e.texte == "" && len(m.Attachments) > 0) {
				m.TmpID = e.tmp
				p.envois[cid] = append(l[:i], l[i+1:]...)
				break
			}
		}
	}
	p.remember(m)
	if mm := p.trouver(cid, m.ID); mm != nil {
		m = *mm
	}
	p.noteLast(c, m)
	if neuf && !m.FromMe && m.Kind != "admin" {
		c.Unread = true
		// Un nouveau message des autres fait revenir une conversation cachée.
		c.Ignoree = false
	}
	if m.FromMe {
		c.Unread = false
	}
	p.sale = true
	p.mu.Unlock()
	emit("message", map[string]any{"item": m})
	p.emitConv(cid)
	if !m.FromMe && m.SenderID != "" {
		go p.auteursDe(cid, []Message{m})
	}
}

// ecrit : quelqu'un écrit (LinkedIn n'envoie pas la fin : le client
// l'oublie après quelques secondes).
func (p *Pont) ecrit(t linkedingo.RealtimeTypingIndicator) {
	p.mu.Lock()
	me := p.me
	qui := t.TypingParticipant.EntityURN.ID()
	cid := court(t.Conversation.EntityURN, me)
	c := p.convs[cid]
	nom := nomParticipant(t.TypingParticipant)
	if nom == "" && c != nil {
		nom = p.nomDe(c)(qui)
	}
	p.mu.Unlock()
	if cid == "" || qui == "" || qui == me || c == nil {
		return
	}
	ev := map[string]any{"conv": cid, "on": true}
	if nom != "" {
		ev["name"] = nom
	}
	emit("typing", ev)
}

// vu : un accusé de lecture. Le mien (lu ailleurs) : la conversation est
// lue. Celui d'un autre : mes messages jusque-là passent à « lu »
// (conversation à deux), ou son nom s'ajoute aux lecteurs (groupe).
func (p *Pont) vu(r linkedingo.SeenReceipt) {
	p.mu.Lock()
	me := p.me
	qui := r.SeenByParticipant.EntityURN.ID()
	mid := court(r.Message.EntityURN, me)
	var cid string
	for c, l := range p.msgs {
		for i := len(l) - 1; i >= 0; i-- {
			if l[i].ID == mid {
				cid = c
				break
			}
		}
		if cid != "" {
			break
		}
	}
	c := p.convs[cid]
	if c == nil || qui == "" {
		p.mu.Unlock()
		return
	}
	if qui == me {
		chg := c.Unread
		c.Unread = false
		p.sale = p.sale || chg
		p.mu.Unlock()
		if chg {
			p.emitConv(cid)
		}
		return
	}
	cible := p.trouver(cid, mid)
	var aEmettre []Message
	if c.Groupe {
		nom := firstOf(nomParticipant(r.SeenByParticipant), p.nomDe(c)(qui))
		if cible != nil && cible.FromMe && nom != "" && !contient(cible.ReadBy, nom) {
			cible.ReadBy = append(cible.ReadBy, nom)
			aEmettre = append(aEmettre, *cible)
		}
	} else if cible != nil {
		l := p.msgs[cid]
		for i := len(l) - 1; i >= 0 && len(aEmettre) < 30; i-- {
			if l[i].FromMe && l[i].Time <= cible.Time && l[i].Kind != "admin" && l[i].Status != "OUTGOING_READ" {
				l[i].Status = "OUTGOING_READ"
				aEmettre = append(aEmettre, l[i])
			}
		}
	}
	p.mu.Unlock()
	for _, m := range aEmettre {
		emit("message", map[string]any{"item": m})
	}
}

// reactionDirect : une réaction ajoutée ou retirée (LinkedIn dit qui).
func (p *Pont) reactionDirect(s linkedingo.RealtimeReactionSummary) {
	p.mu.Lock()
	me := p.me
	mid := court(s.Message.EntityURN, me)
	m := p.trouver("", mid)
	emoji := s.ReactionSummary.Emoji
	if m == nil || emoji == "" {
		p.mu.Unlock()
		return
	}
	qui := s.Actor.EntityURN.ID()
	nom := ""
	if c := p.convs[m.Conv]; c != nil {
		nom = firstOf(nomParticipant(s.Actor), p.nomDe(c)(qui))
	}
	m.Reactions = appliquerReaction(m.Reactions, emoji, qui, nom, qui == me, s.ReactionAdded, s.ReactionSummary.Count)
	out := *m
	p.mu.Unlock()
	emit("message", map[string]any{"item": out})
}

// appliquerReaction : une réaction nommée ajoutée ou retirée ; les
// anonymes de cet émoji complètent jusqu'au compte que LinkedIn donne.
func appliquerReaction(l []Reaction, emoji, qui, nom string, moi, ajout bool, compte int) []Reaction {
	var r []Reaction
	for _, x := range l {
		if x.Emoji == emoji && ((moi && x.FromMe) || (!moi && qui != "" && x.Actor == qui)) {
			continue
		}
		r = append(r, x)
	}
	if ajout {
		x := Reaction{Emoji: emoji, FromMe: moi}
		if !moi {
			x.Actor, x.Sender = qui, nom
		}
		r = append(r, x)
	}
	// Le compte de cet émoji : les anonymes s'ajustent.
	nommes := 0
	for _, x := range r {
		if x.Emoji == emoji && (x.FromMe || x.Actor != "") {
			nommes++
		}
	}
	garder := max(0, compte-nommes)
	var fin []Reaction
	for _, x := range r {
		if x.Emoji == emoji && !x.FromMe && x.Actor == "" {
			if garder == 0 {
				continue
			}
			garder--
		}
		fin = append(fin, x)
	}
	for ; garder > 0; garder-- {
		fin = append(fin, Reaction{Emoji: emoji})
	}
	if len(fin) == 0 {
		return nil
	}
	return fin
}

// nettoyerEnvois : les envois trop vieux pour revenir (à appeler avec p.mu).
func (p *Pont) nettoyerEnvois(cid string) {
	l := p.envois[cid][:0]
	for _, e := range p.envois[cid] {
		if time.Since(e.quand) < 2*time.Minute {
			l = append(l, e)
		}
	}
	if len(l) == 0 {
		delete(p.envois, cid)
		return
	}
	p.envois[cid] = l
}
