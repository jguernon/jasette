package main

// Les réactions de Google Messages (RCS) : celles reçues avec chaque message
// (libgm renvoie le message entier quand elles changent, donc l'événement
// « message » est réémis tout seul), et la commande « react » (une seule
// réaction par personne et par message ; un emoji vide retire la mienne).

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

type Reaction struct {
	Emoji  string `json:"emoji"`
	Sender string `json:"sender"`
	FromMe bool   `json:"from_me"`
	// L'identifiant du participant chez Google Messages.
	Actor string `json:"actor"`
}

// Garde le dernier état connu d'un message (pour « react »). À appeler avec p.mu.
func (p *Pont) keepMsg(m *gmproto.Message) {
	if m.GetMessageID() == "" {
		return
	}
	if len(p.msgs) > 20000 {
		clear(p.msgs)
	}
	p.msgs[m.GetMessageID()] = m
}

// Mes identifiants de participant dans une conversation.
func myIDs(conv *gmproto.Conversation) []string {
	var ids []string
	if id := conv.GetDefaultOutgoingID(); id != "" {
		ids = append(ids, id)
	}
	for _, part := range conv.GetParticipants() {
		if id := part.GetID().GetParticipantID(); part.GetIsMe() && id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// L'emoji d'une réaction, comme le pont mautrix ("" si on ne sait pas l'afficher).
func emojiOf(d *gmproto.ReactionData) string {
	switch d.GetType() {
	case gmproto.EmojiType_EMOTIFY:
		return ""
	case gmproto.EmojiType_CUSTOM:
		return d.GetUnicode()
	}
	if e := d.GetType().Unicode(); e != "" {
		return e
	}
	return d.GetUnicode()
}

func reactionsOut(m *gmproto.Message, conv *gmproto.Conversation, names map[string]string) []Reaction {
	if len(m.GetReactions()) == 0 {
		return nil
	}
	mine := myIDs(conv)
	var out []Reaction
	for _, re := range m.GetReactions() {
		emoji := emojiOf(re.GetData())
		if emoji == "" {
			continue
		}
		for _, pid := range re.GetParticipantIDs() {
			out = append(out, Reaction{Emoji: emoji, Sender: names[pid], FromMe: slices.Contains(mine, pid), Actor: pid})
		}
	}
	return out
}

func reactErr(cmd Cmd, err error) {
	log.Warn().Err(err).Str("fil", cmd.Conv).Str("msg", cmd.ID).Msg("réaction")
	emit("error", map[string]any{"where": "react", "text": err.Error(), "conv": cmd.Conv, "id": cmd.ID})
}

func (p *Pont) sendReaction(cmd Cmd) {
	emoji := strings.TrimSpace(cmd.Emoji)
	cli := p.client()
	if cli == nil {
		reactErr(cmd, errors.New("Google Messages n'est pas connecté"))
		return
	}
	p.mu.Lock()
	m := p.msgs[cmd.ID]
	p.mu.Unlock()
	if m == nil {
		reactErr(cmd, fmt.Errorf("message inconnu : %s", cmd.ID))
		return
	}
	convID := m.GetConversationID()
	if cmd.Conv != "" && cmd.Conv != convID {
		reactErr(cmd, errors.New("le message n'est pas dans cette conversation"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	p.mu.Lock()
	conv := p.convs[convID]
	p.mu.Unlock()
	if conv == nil {
		var err error
		if conv, err = cli.GetConversation(ctx, convID); err != nil {
			reactErr(cmd, err)
			return
		}
		p.remember(conv)
	}
	mine := myIDs(conv)
	if len(mine) == 0 {
		reactErr(cmd, errors.New("mon identifiant dans cette conversation est inconnu"))
		return
	}
	// Ma réaction actuelle.
	var old *gmproto.ReactionData
	for _, re := range m.GetReactions() {
		for _, pid := range re.GetParticipantIDs() {
			if slices.Contains(mine, pid) {
				old = re.GetData()
			}
		}
	}
	oldEmoji := emojiOf(old)
	if old != nil && oldEmoji == emoji {
		emit("message", map[string]any{"item": p.msgOut(m)})
		return
	}
	req := &gmproto.SendReactionRequest{
		MessageID:  m.GetMessageID(),
		SIMPayload: conv.GetSimCard().GetSIMData().GetSIMPayload(),
	}
	switch {
	case emoji == "" && old == nil:
		emit("message", map[string]any{"item": p.msgOut(m)})
		return
	case emoji == "":
		req.Action, req.ReactionData = gmproto.SendReactionRequest_REMOVE, old
	case old == nil:
		req.Action, req.ReactionData = gmproto.SendReactionRequest_ADD, gmproto.MakeReactionData(emoji)
	default:
		req.Action, req.ReactionData = gmproto.SendReactionRequest_SWITCH, gmproto.MakeReactionData(emoji)
	}

	// Tout de suite à l'écran (une copie du message avec ma réaction) ; le
	// téléphone renverra ensuite le message à jour.
	opt := proto.Clone(m).(*gmproto.Message)
	var entries []*gmproto.ReactionEntry
	placed := false
	for _, re := range opt.GetReactions() {
		re.ParticipantIDs = slices.DeleteFunc(re.ParticipantIDs, func(pid string) bool { return slices.Contains(mine, pid) })
		if emoji != "" && emojiOf(re.GetData()) == emoji {
			re.ParticipantIDs = append(re.ParticipantIDs, mine[0])
			placed = true
		}
		if len(re.ParticipantIDs) > 0 {
			entries = append(entries, re)
		}
	}
	if emoji != "" && !placed {
		entries = append(entries, &gmproto.ReactionEntry{Data: gmproto.MakeReactionData(emoji), ParticipantIDs: []string{mine[0]}})
	}
	opt.Reactions = entries
	emit("message", map[string]any{"item": p.msgOut(opt)})

	resp, err := cli.SendReaction(ctx, req)
	if err == nil && !resp.GetSuccess() {
		err = errors.New("le téléphone a refusé la réaction")
	}
	if err != nil {
		// Remis comme avant, sauf si le téléphone a déjà renvoyé le message.
		p.mu.Lock()
		still := p.msgs[m.GetMessageID()] == opt
		p.mu.Unlock()
		if still {
			emit("message", map[string]any{"item": p.msgOut(m)})
		}
		reactErr(cmd, err)
	}
}
