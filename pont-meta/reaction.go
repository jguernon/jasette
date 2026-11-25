package main

// Les réactions envoyées d'ici (commande « react ») : une seule par personne
// et par message (Messenger remplace l'ancienne) ; un emoji vide retire la mienne.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	waTypes "go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
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
		reactErr(cmd, errors.New("Messenger n'est pas connecté"))
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

	// Tout de suite à l'écran ; remis comme avant si Messenger refuse.
	r.Reactions = setMyReaction(r.Reactions, me, emoji)
	p.putMsg(r)
	p.emitNow(r)

	var err error
	if c.Type.IsWhatsApp() {
		err = p.reactE2EE(c, r, me, emoji)
	} else {
		err = p.reactFB(c.Key, r.ID, me, emoji)
	}
	if err != nil {
		if cur := p.getMsg(r.ID); cur != nil {
			cur.Reactions = setMyReaction(cur.Reactions, me, old)
			p.putMsg(cur)
			p.emitNow(cur)
		}
		reactErr(cmd, err)
	}
}

// Un fil normal : la tâche SendReactionTask (emoji vide = retrait), comme mautrix-meta.
func (p *Pont) reactFB(thread int64, msgID string, me int64, emoji string) error {
	cli := p.client()
	if cli == nil {
		return errors.New("Messenger n'est pas connecté")
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	if err := cli.WaitUntilCanSendMessages(ctx, 15*time.Second); err != nil {
		return explain(err)
	}
	resp, err := cli.ExecuteTasks(ctx, &socket.SendReactionTask{
		ThreadKey:       thread,
		TimestampMs:     time.Now().UnixMilli(),
		MessageID:       msgID,
		ActorID:         me,
		Reaction:        emoji,
		SyncGroup:       1,
		SendAttribution: table.MESSENGER_INBOX_IN_THREAD,
	})
	if err != nil {
		return explain(err)
	}
	if resp != nil {
		for _, f := range resp.LSHandleFailedTask {
			return fmt.Errorf("Messenger a refusé la réaction : %s", f.Message)
		}
		p.handleTable(resp, false)
	}
	return nil
}

// Un fil chiffré : un ReactionMessage par le canal chiffré (texte vide = retrait).
func (p *Pont) reactE2EE(c *convState, r *msgRow, me int64, emoji string) error {
	if err := p.waitE2EE(p.ctx, time.Minute); err != nil {
		return err
	}
	wa := p.waClient()
	if wa == nil {
		return errors.New("canal chiffré pas connecté")
	}
	chat := p.chatJID(c)
	key := &waCommon.MessageKey{
		RemoteJID: proto.String(chat.String()),
		ID:        proto.String(r.ID),
	}
	if r.FromMe {
		key.FromMe = proto.Bool(true)
	}
	if chat.Server != waTypes.MessengerServer && chat.Server != waTypes.DefaultUserServer {
		// Un groupe : l'auteur du message visé.
		sender := r.WASender
		switch {
		case r.FromMe:
			sender = waTypes.NewJID(idStr(me), waTypes.MessengerServer).String()
		case sender == "":
			sender = waTypes.NewJID(idStr(r.SenderID), waTypes.MessengerServer).String()
		}
		key.Participant = proto.String(sender)
	}
	msg := &waConsumerApplication.ConsumerApplication{
		Payload: &waConsumerApplication.ConsumerApplication_Payload{
			Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{
				Content: &waConsumerApplication.ConsumerApplication_Content{
					Content: &waConsumerApplication.ConsumerApplication_Content_ReactionMessage{
						ReactionMessage: &waConsumerApplication.ConsumerApplication_ReactionMessage{
							Key:               key,
							Text:              proto.String(emoji),
							SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
						},
					},
				},
			},
		},
	}
	ctx, annule := context.WithTimeout(p.ctx, time.Minute)
	defer annule()
	if _, err := wa.SendFBMessage(ctx, chat, msg, nil, whatsmeow.SendRequestExtra{ID: wa.GenerateMessageID()}); err != nil {
		return fmt.Errorf("réaction chiffrée : %w", err)
	}
	return nil
}
