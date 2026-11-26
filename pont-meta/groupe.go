package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

// createGroup : un groupe Messenger (CreateGroupTask, comme mautrix-meta),
// puis son nom s'il y en a un (RenameThreadTask). Le pont répond par la
// conversation (open), ou une erreur create_group.
func (p *Pont) createGroup(cmd Cmd) {
	membres, err := membresGroupe(cmd.Members)
	if err != nil {
		emitErr("create_group", err)
		return
	}
	ids := make([]int64, 0, len(membres))
	for _, m := range membres {
		id, err := strconv.ParseInt(strings.TrimPrefix(m, "+"), 10, 64)
		if err != nil || id <= 0 {
			emitErr("create_group", fmt.Errorf("identifiant Messenger invalide : %s", m))
			return
		}
		ids = append(ids, id)
	}
	cli := p.client()
	if cli == nil {
		emitErr("create_group", errors.New("Messenger n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	defer annule()
	fil := methods.GenerateEpochID()
	resp, err := cli.ExecuteTasks(ctx, &socket.CreateGroupTask{
		Participants: ids,
		SendPayload: socket.CreateGroupPayload{
			ThreadID: fil,
			OTID:     strconv.FormatInt(methods.GenerateEpochID(), 10),
			Source:   0,
			SendType: 8,
		},
	})
	switch {
	case err != nil:
		emitErr("create_group", fmt.Errorf("nouveau groupe : %w", explain(err)))
		return
	case len(resp.LSIssueNewError) > 0:
		e := resp.LSIssueNewError[0]
		emitErr("create_group", fmt.Errorf("nouveau groupe refusé par Messenger : %s", cmp.Or(e.ErrorMessage, e.ErrorTitle, "raison inconnue")))
		return
	case len(resp.LSReplaceOptimisticThread) == 0 || resp.LSReplaceOptimisticThread[0].ThreadKey1 != fil:
		emitErr("create_group", errors.New("nouveau groupe : réponse inattendue de Messenger"))
		return
	}
	cle := resp.LSReplaceOptimisticThread[0].ThreadKey2
	p.handleTable(resp, false)
	nom := nomGroupe(cmd.Name, nil, 0)
	if nom != "" {
		if r, err := cli.ExecuteTasks(ctx, &socket.RenameThreadTask{ThreadKey: cle, ThreadName: nom, SyncGroup: 1}); err != nil {
			log.Warn().Err(err).Msg("nom du nouveau groupe")
		} else if r != nil {
			p.handleTable(r, false)
		}
	}
	conv := idStr(cle)
	p.mu.Lock()
	c := p.getOrMakeConv(cle, table.GROUP_THREAD)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	if c.Name == "" {
		c.Name = nom
	}
	if len(c.Members) == 0 {
		c.Members = ids
	}
	c.Hidden = false
	p.saveConv(conv, c)
	item := p.convOut(conv, c)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{conv})
}
