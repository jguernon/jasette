package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/mautrix-signal/pkg/libsignalgo"
	"go.mau.fi/mautrix-signal/pkg/signalmeow"
)

// Signal coupe le nom d'un groupe à 32 caractères.
const maxNomGroupeSignal = 32

// createGroup : un groupe Signal (CreateGroup de signalmeow, comme
// mautrix-signal) : moi administrateur, les membres connus par leur ACI, les
// autres (PNI) invités. Sans nom choisi, ceux des membres. Le pont répond par
// la conversation (open), ou une erreur create_group.
func (p *Pont) createGroup(cmd Cmd) {
	membres, err := membresGroupe(cmd.Members)
	if err != nil {
		emitErr("create_group", err)
		return
	}
	cli := p.client()
	if cli == nil {
		emitErr("create_group", errors.New("Signal n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 60*time.Second)
	defer annule()
	g := &signalmeow.Group{
		Members: []*signalmeow.GroupMember{{ACI: cli.Store.ACI, Role: signalmeow.GroupMember_ADMINISTRATOR}},
		AccessControl: &signalmeow.GroupAccessControl{
			Members:           signalmeow.AccessControl_MEMBER,
			AddFromInviteLink: signalmeow.AccessControl_UNSATISFIABLE,
			Attributes:        signalmeow.AccessControl_ADMINISTRATOR,
		},
	}
	noms := []string{}
	maintenant := uint64(time.Now().UnixMilli())
	for _, m := range membres {
		sid, err := p.sidMembre(ctx, cli, m)
		if err != nil {
			emitErr("create_group", err)
			return
		}
		p.mu.Lock()
		noms = append(noms, strings.TrimSpace(strings.SplitN(p.contactName(sid.String()), " ", 2)[0]))
		p.mu.Unlock()
		if sid.Type == libsignalgo.ServiceIDTypeACI {
			g.Members = append(g.Members, &signalmeow.GroupMember{ACI: sid.UUID, Role: signalmeow.GroupMember_DEFAULT})
		} else {
			g.PendingMembers = append(g.PendingMembers, &signalmeow.PendingMember{
				ServiceID: sid, Role: signalmeow.GroupMember_DEFAULT, AddedByUserID: cli.Store.ACI, Timestamp: maintenant,
			})
		}
	}
	g.Title = nomGroupe(cmd.Name, noms, maxNomGroupeSignal)
	if _, err := signalmeow.PrepareGroupCreation(g); err != nil {
		emitErr("create_group", fmt.Errorf("nouveau groupe : %w", err))
		return
	}
	cree, err := cli.CreateGroup(ctx, g)
	if err != nil {
		emitErr("create_group", fmt.Errorf("nouveau groupe : %w", err))
		return
	}
	gid := string(cree.GroupIdentifier)
	if !p.refreshGroup(ctx, cli, gid, 0) {
		// Relu plus tard ; d'ici là, ce que l'on sait déjà.
		p.mu.Lock()
		c := p.getOrMakeConv(gid)
		c.Group, c.Name = true, cree.Title
		p.saveConv(gid, c)
		p.mu.Unlock()
	}
	p.mu.Lock()
	c := p.getOrMakeConv(gid)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
		c.ReadWM = c.LastActivity
	}
	c.Hidden = false
	p.saveConv(gid, c)
	item := p.convOut(gid, c)
	p.listDirty = true
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{gid})
}

// sidMembre : l'identifiant Signal d'une personne, donné tel quel (résultat
// de « search ») ou par son numéro. Jamais un groupe.
func (p *Pont) sidMembre(ctx context.Context, cli *signalmeow.Client, m string) (libsignalgo.ServiceID, error) {
	if isGroupID(m) {
		return libsignalgo.ServiceID{}, errors.New("un groupe ne peut pas être membre d'un groupe")
	}
	if strings.HasPrefix(m, "PNI:") || isUUID(m) {
		sid, err := libsignalgo.ServiceIDFromString(m)
		if err != nil {
			return libsignalgo.ServiceID{}, fmt.Errorf("identifiant Signal invalide : %s", m)
		}
		return sid, nil
	}
	digits := phoneChars.ReplaceAllString(m, "")
	if !isPhone(digits) {
		return libsignalgo.ServiceID{}, fmt.Errorf("identifiant Signal invalide : %s", m)
	}
	return p.resolvePhone(ctx, cli, digits)
}
