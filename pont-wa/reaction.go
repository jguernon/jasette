package main

// Les réactions envoyées d'ici (commande « react ») : une seule par personne
// et par message ; un emoji vide retire la mienne.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
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

// Remplace ma réaction (toutes celles marquées « moi », quel que soit
// l'identifiant sous lequel elles sont arrivées).
func setMyReaction(l []Reaction, actor, emoji string) []Reaction {
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
	p.activity()
	emoji := strings.TrimSpace(cmd.Emoji)
	cli, err := p.readyClient()
	if err != nil {
		reactErr(cmd, err)
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
	chat, err := types.ParseJID(conv)
	if err != nil {
		reactErr(cmd, fmt.Errorf("conversation illisible : %s", conv))
		return
	}
	me := cli.Store.ID.ToNonAD()
	old := myReaction(r.Reactions)
	if old == emoji {
		p.emitNow(r)
		return
	}
	// L'expéditeur du message visé (moi pour mes messages).
	sender := me
	if !r.FromMe {
		if sender, err = types.ParseJID(r.SenderJID); err != nil || r.SenderJID == "" {
			sender = chat // conversation à deux : l'autre personne
		}
	}

	// Tout de suite à l'écran ; remis comme avant si WhatsApp refuse.
	r.Reactions = setMyReaction(r.Reactions, me.String(), emoji)
	p.putMsg(r)
	p.emitNow(r)

	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	// Un emoji vide retire la réaction ; un autre remplace l'ancienne.
	_, err = cli.SendMessage(ctx, chat, cli.BuildReaction(chat, sender, r.ID, emoji))
	if err != nil {
		if cur := p.getMsg(r.ID); cur != nil {
			cur.Reactions = setMyReaction(cur.Reactions, me.String(), old)
			p.putMsg(cur)
			p.emitNow(cur)
		}
		reactErr(cmd, fmt.Errorf("WhatsApp : %w", err))
	}
}

// Ajoute quelqu'un à la liste de ceux qui ont lu un de mes messages (groupes).
func addReader(l []string, who string) ([]string, bool) {
	if who == "" {
		return l, false
	}
	for _, x := range l {
		if x == who {
			return l, false
		}
	}
	return append(l, who), true
}
