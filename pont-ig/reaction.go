package main

// Les réactions envoyées d'ici (commande « react ») : une seule par personne
// et par message (Instagram remplace l'ancienne) ; un emoji vide retire la mienne.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
)

// Ma réaction actuelle sur un message ("" s'il n'y en a pas).
func myReaction(l []Reaction) string {
	for _, r := range l {
		if r.FromMe {
			return r.Emoji
		}
	}
	return ""
}

// Remplace ma réaction.
func setMyReaction(l []Reaction, actor int64, emoji string) []Reaction {
	out := make([]Reaction, 0, len(l)+1)
	for _, r := range l {
		if !r.FromMe && r.Actor != actor {
			out = append(out, r)
		}
	}
	if emoji != "" {
		out = append(out, Reaction{Emoji: emoji, Actor: actor, FromMe: true})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Deux listes de réactions identiques (sans regarder les noms affichés) ?
func sameReactions(a, b []Reaction) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Emoji != b[i].Emoji || a[i].Actor != b[i].Actor || a[i].FromMe != b[i].FromMe {
			return false
		}
	}
	return true
}

// Annonce un message à l'interface, sans condition.
func (p *Pont) emitNow(r *msgRow) {
	p.mu.Lock()
	out := p.msgOut(r)
	p.mu.Unlock()
	emit("message", map[string]any{"item": out})
}

func reactErr(cmd Cmd, err error) {
	log.Warn().Err(err).Str("fil", cmd.Conv).Str("msg", cmd.ID).Msg("réaction")
	emit("error", map[string]any{"where": "react", "text": err.Error(), "conv": cmd.Conv, "id": cmd.ID})
}

func (p *Pont) sendReaction(cmd Cmd) {
	emoji := strings.TrimSpace(cmd.Emoji)
	cli := p.client()
	p.mu.Lock()
	me := p.me
	p.mu.Unlock()
	if cli == nil || me == 0 {
		reactErr(cmd, errors.New("Instagram n'est pas connecté"))
		return
	}
	r := p.getMsg(cmd.ID)
	if r == nil || strings.HasPrefix(r.ID, "tmp_") {
		reactErr(cmd, fmt.Errorf("message inconnu : %s", cmd.ID))
		return
	}
	conv := r.Conv
	if cmd.Conv != "" && cmd.Conv != conv {
		reactErr(cmd, errors.New("le message n'est pas dans cette conversation"))
		return
	}
	c := p.conv(conv)
	if c == nil {
		reactErr(cmd, fmt.Errorf("conversation inconnue : %s", conv))
		return
	}
	old := myReaction(r.Reactions)
	if old == emoji {
		p.emitNow(r)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	igid, _, err := p.ensureIGID(ctx, cli, conv, c)
	if igid == "" {
		if err == nil {
			err = errors.New("identifiant du fil inconnu")
		}
		reactErr(cmd, explain(err))
		return
	}

	// Tout de suite à l'écran ; remis comme avant si Instagram refuse.
	r.Reactions = setMyReaction(r.Reactions, me, emoji)
	p.putMsg(r)
	p.emitNow(r)

	// Comme igconnector : « created » (remplace l'ancienne), ou « deleted »
	// avec l'emoji retiré.
	in := slidetypes.ReactionInput{
		Emoji:          emoji,
		MessageID:      r.ID,
		ReactionStatus: slidetypes.ReactionStatusCreated,
		ThreadID:       igid,
	}
	if emoji == "" {
		in.Emoji, in.ReactionStatus = old, slidetypes.ReactionStatusDeleted
	}
	if _, err := cli.SendReaction(ctx, &slidetypes.CreateReactionRequest{Input: in}); err != nil {
		if cur := p.getMsg(r.ID); cur != nil {
			cur.Reactions = setMyReaction(cur.Reactions, me, old)
			p.putMsg(cur)
			p.emitNow(cur)
		}
		reactErr(cmd, explain(err))
	}
}
