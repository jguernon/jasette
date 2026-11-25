package main

// Les réactions envoyées d'ici (commande « react ») : une seule par personne
// et par message ; un emoji vide retire la mienne.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-signal/pkg/signalmeow"
	"go.mau.fi/mautrix-signal/pkg/signalmeow/protobuf/signalpb"
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
	emoji := strings.TrimSpace(cmd.Emoji)
	cli := p.client()
	if cli == nil {
		reactErr(cmd, errors.New("Signal n'est pas connecté"))
		return
	}
	r := p.getMsg(cmd.ID)
	if r == nil {
		reactErr(cmd, fmt.Errorf("message inconnu : %s", cmd.ID))
		return
	}
	conv := r.Conv
	if cmd.Conv != "" && cmd.Conv != conv {
		reactErr(cmd, errors.New("le message n'est pas dans cette conversation"))
		return
	}
	// L'identifiant donne l'auteur et l'horodatage du message visé.
	author, ts, err := parseMsgID(r.ID)
	if err != nil {
		reactErr(cmd, errors.New("on ne peut pas réagir à ce message"))
		return
	}
	me := cli.Store.ACI.String()
	old := myReaction(r.Reactions)
	if old == emoji {
		p.emitNow(r)
		return
	}

	// Tout de suite à l'écran ; remis comme avant si Signal refuse.
	r.Reactions = setMyReaction(r.Reactions, me, emoji)
	p.putMsg(r)
	p.emitNow(r)

	// Une nouvelle réaction remplace l'ancienne ; pour retirer, Signal veut
	// l'emoji retiré.
	re := &signalpb.DataMessage_Reaction{
		Emoji:                 proto.String(emoji),
		Remove:                proto.Bool(false),
		TargetAuthorAciBinary: author[:],
		TargetSentTimestamp:   proto.Uint64(ts),
	}
	if emoji == "" {
		re.Emoji, re.Remove = proto.String(old), proto.Bool(true)
	}
	dm := &signalpb.DataMessage{
		Timestamp:               proto.Uint64(uint64(time.Now().UnixMilli())),
		RequiredProtocolVersion: proto.Uint32(uint32(signalpb.DataMessage_REACTIONS)),
		Reaction:                re,
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	if err := p.sendContent(ctx, cli, conv, signalmeow.WrapDataMessage(dm)); err != nil {
		if cur := p.getMsg(r.ID); cur != nil {
			cur.Reactions = setMyReaction(cur.Reactions, me, old)
			p.putMsg(cur)
			p.emitNow(cur)
		}
		reactErr(cmd, explainSend(err))
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
