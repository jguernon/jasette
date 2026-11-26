package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
)

// createGroup : un groupe Instagram (CreateGroup d'instameow, comme
// mautrix-meta), puis le fil relu. Instagram ne prend pas de nom à la
// création. Le pont répond par la conversation (open), ou une erreur create_group.
func (p *Pont) createGroup(cmd Cmd) {
	membres, err := membresGroupe(cmd.Members)
	if err != nil {
		emitErr("create_group", err)
		return
	}
	for _, m := range membres {
		if id, err := strconv.ParseInt(strings.TrimPrefix(m, "+"), 10, 64); err != nil || id <= 0 {
			emitErr("create_group", fmt.Errorf("identifiant Instagram invalide : %s", m))
			return
		}
	}
	cli := p.client()
	if cli == nil {
		emitErr("create_group", errors.New("Instagram n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	defer annule()
	resp, err := cli.CreateGroup(ctx, &slidetypes.CreateGroupRequest{
		RecipientFBIDs:     membres,
		OfflineThreadingID: strconv.FormatInt(methods.GenerateEpochID(), 10),
	})
	if err != nil {
		emitErr("create_group", fmt.Errorf("nouveau groupe : %w", explain(err)))
		return
	}
	if resp == nil || resp.Data.ID == "" {
		emitErr("create_group", errors.New("nouveau groupe : réponse inattendue d'Instagram"))
		return
	}
	fil, err := cli.GetThread(ctx, slidetypes.MakeGetThreadInfoRequest(resp.Data.ID))
	if err != nil {
		emitErr("create_group", fmt.Errorf("nouveau groupe créé, mais illisible : %w", explain(err)))
		return
	}
	conv := p.upsertThread(fil.ThreadInfo.AsIGDirectThread, true)
	p.mu.Lock()
	c := p.convs[conv]
	if conv == "" || c == nil {
		p.mu.Unlock()
		emitErr("create_group", errors.New("nouveau groupe créé, mais introuvable : il paraîtra à la prochaine liste"))
		return
	}
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	c.Hidden = false
	p.saveConv(conv, c)
	item := p.convOut(conv, c)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{conv})
}
