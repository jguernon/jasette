package main

import (
	"errors"
	"fmt"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// createGroup : un groupe avec plusieurs numéros (RCS si tous l'ont, sinon
// MMS), comme mautrix-gmessages. Le nom ne vaut que pour un groupe RCS.
// Le pont répond par la conversation (open), ou une erreur create_group.
func (p *Pont) createGroup(cmd Cmd) {
	membres, err := membresGroupe(cmd.Members)
	if err != nil {
		emitErr("create_group", err)
		return
	}
	cli := p.client()
	if cli == nil {
		emitErr("create_group", errors.New("Google Messages n'est pas connecté"))
		return
	}
	req := &gmproto.GetOrCreateConversationRequest{}
	for _, n := range membres {
		req.Numbers = append(req.Numbers, &gmproto.ContactNumber{MysteriousInt: 2, Number: n, Number2: n})
	}
	if nom := nomGroupe(cmd.Name, nil, 0); nom != "" {
		req.RCSGroupName = &nom
	}
	resp, err := cli.GetOrCreateConversation(p.ctx, req)
	if err == nil && resp.GetStatus() == gmproto.GetOrCreateConversationResponse_CREATE_RCS {
		vrai := true
		req.CreateRCSGroup = &vrai
		if req.RCSGroupName == nil {
			vide := ""
			req.RCSGroupName = &vide
		}
		resp, err = cli.GetOrCreateConversation(p.ctx, req)
	}
	if err != nil || resp.GetConversation().GetConversationID() == "" {
		if err == nil {
			err = errors.New("groupe refusé par le téléphone")
		}
		emitErr("create_group", fmt.Errorf("nouveau groupe : %w", err))
		return
	}
	c := resp.GetConversation()
	p.remember(c)
	emit("conversation", map[string]any{"item": convOut(c), "open": true})
}
