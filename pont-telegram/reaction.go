package main

// Les réactions envoyées d'ici (commande « react ») : une seule par
// personne et par message ; un emoji vide retire la mienne.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func myReaction(l []Reaction) string {
	for _, r := range l {
		if r.FromMe {
			return r.Emoji
		}
	}
	return ""
}

func setMyReaction(l []Reaction, actor, emoji string) []Reaction {
	out := make([]Reaction, 0, len(l)+1)
	for _, r := range l {
		if !r.FromMe && (r.Actor == "" || r.Actor != actor) {
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

func reactErr(cmd Cmd, err error) {
	log.Warn().Err(err).Str("fil", cmd.Conv).Str("msg", cmd.ID).Msg("réaction")
	emit("error", map[string]any{"where": "react", "text": err.Error(), "conv": cmd.Conv, "id": cmd.ID})
}

func (p *Pont) sendReaction(cmd Cmd) {
	emoji := strings.TrimSpace(cmd.Emoji)
	api := p.tgAPI()
	if api == nil {
		reactErr(cmd, errors.New("Telegram n'est pas connecté"))
		return
	}
	r := p.getMsg(cmd.ID)
	if r == nil || r.Num == 0 {
		reactErr(cmd, fmt.Errorf("message inconnu : %s", cmd.ID))
		return
	}
	if cmd.Conv != "" && cmd.Conv != r.Conv {
		reactErr(cmd, errors.New("le message n'est pas dans cette conversation"))
		return
	}
	ip, err := p.inputPeer(r.Conv)
	if err != nil {
		reactErr(cmd, err)
		return
	}
	p.mu.Lock()
	me := keyOf('u', p.me)
	p.mu.Unlock()
	old := myReaction(r.Reactions)
	if old == emoji {
		p.emitNow(r)
		return
	}
	// Tout de suite à l'écran ; remis comme avant si Telegram refuse.
	r.Reactions = setMyReaction(r.Reactions, me, emoji)
	p.putMsg(r)
	p.emitNow(r)

	req := &tg.MessagesSendReactionRequest{Peer: ip, MsgID: r.Num}
	if emoji != "" {
		req.SetReaction([]tg.ReactionClass{&tg.ReactionEmoji{Emoticon: emoji}})
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	if _, err := api.MessagesSendReaction(ctx, req); err != nil {
		if cur := p.getMsg(r.ID); cur != nil {
			cur.Reactions = setMyReaction(cur.Reactions, me, old)
			p.putMsg(cur)
			p.emitNow(cur)
		}
		if tgerr.Is(err, "REACTION_INVALID") {
			err = errors.New("cette réaction n'est pas permise dans cette conversation")
		} else {
			err = explainSend(err)
		}
		reactErr(cmd, err)
	}
}
