package main

// Les réactions envoyées d'ici (commande « react ») : une seule par personne
// et par message ; un emoji vide retire la mienne. X garderait plusieurs
// réactions d'une même personne : pour en changer, on retire d'abord l'ancienne.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-twitter/pkg/twittermeow"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/crypto"
	"go.mau.fi/mautrix-twitter/pkg/twittermeow/data/payload"
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
	me, _ := strconv.ParseInt(p.myID(), 10, 64)
	if cli == nil || me == 0 {
		reactErr(cmd, errors.New("X n'est pas connecté"))
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
	if p.conv(conv) == nil {
		reactErr(cmd, fmt.Errorf("conversation inconnue : %s", conv))
		return
	}
	old := myReaction(r.Reactions)
	if old == emoji {
		p.emitNow(r)
		return
	}

	// Tout de suite à l'écran ; remis comme avant si X refuse.
	r.Reactions = setMyReaction(r.Reactions, me, emoji)
	p.putMsg(r)
	p.emitNow(r)

	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	var err error
	if old != "" {
		err = p.xReact(ctx, cli, conv, r.ID, old, true)
	}
	if err == nil && emoji != "" {
		err = p.xReact(ctx, cli, conv, r.ID, emoji, false)
	}
	if err != nil {
		if cur := p.getMsg(r.ID); cur != nil {
			// Si l'ancienne a été retirée mais la nouvelle refusée, il n'en reste plus.
			cur.Reactions = setMyReaction(cur.Reactions, me, old)
			p.putMsg(cur)
			p.emitNow(cur)
		}
		reactErr(cmd, explain(err))
	}
}

// Une réaction (ajout ou retrait), par XChat ou par l'ancienne API selon le
// fil, comme doHandleMatrixReaction du pont officiel.
func (p *Pont) xReact(ctx context.Context, cli *twittermeow.Client, conv, msgID, emoji string, remove bool) error {
	mode := p.modeOf(conv)
	if mode != sendREST {
		resp, err := cli.SendXChatReaction(ctx, conv, msgID, emoji, remove, mode == sendEncrypted)
		switch {
		case errors.Is(err, crypto.ErrKeyNotFound):
			// Pas de clé finalement : l'ancienne API, comme pour l'envoi.
		case err != nil:
			return err
		case resp != nil && len(resp.Errors) > 0:
			return fmt.Errorf("X a refusé la réaction : %s", resp.Errors[0].Message)
		default:
			return nil
		}
	}
	resp, err := cli.React(ctx, &payload.ReactionActionPayload{
		ConversationID: restID(conv),
		MessageID:      msgID,
		ReactionTypes:  []string{"Emoji"},
		EmojiReactions: []string{emoji},
	}, remove)
	if err != nil {
		return err
	}
	if !remove && resp.Data.CreateDmReaction.Typename == "CreateDMReactionFailure" ||
		remove && resp.Data.DeleteDmReaction.Typename == "DeleteDMReactionFailure" {
		return errors.New("X a refusé la réaction")
	}
	return nil
}
