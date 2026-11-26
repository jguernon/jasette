package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// WhatsApp refuse un nom de groupe de plus de 25 caractères (whatsmeow).
const maxNomGroupeWA = 25

// createGroup : un groupe WhatsApp (CreateGroup de whatsmeow). WhatsApp
// exige un nom : sans nom choisi, ceux des membres. Le pont répond par la
// conversation (open), ou une erreur create_group.
func (p *Pont) createGroup(cmd Cmd) {
	membres, err := membresGroupe(cmd.Members)
	if err != nil {
		emitErr("create_group", err)
		return
	}
	cli, err := p.readyClient()
	if err != nil {
		emitErr("create_group", err)
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	defer annule()
	jids := make([]types.JID, 0, len(membres))
	noms := make([]string, 0, len(membres))
	for _, m := range membres {
		jid, err := p.jidMembre(ctx, cli, m)
		if err != nil {
			emitErr("create_group", err)
			return
		}
		jids = append(jids, jid)
		p.mu.Lock()
		noms = append(noms, prenom(p.names[jid.String()]))
		p.mu.Unlock()
	}
	nom := nomGroupe(cmd.Name, noms, maxNomGroupeWA)
	if nom == "" {
		nom = "Groupe"
	}
	g, err := cli.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: nom, Participants: jids})
	if err != nil {
		emitErr("create_group", fmt.Errorf("nouveau groupe : %w", err))
		return
	}
	p.upsertGroup(cli, g)
	p.mu.Lock()
	id, c := p.getOrMakeConv(g.JID)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	c.Hidden = false
	p.saveConv(id, c)
	item := p.convOut(id, c)
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{id})
}

// jidMembre : le JID d'une personne, donné tel quel (résultat de « search »)
// ou par son numéro (vérifié chez WhatsApp). Jamais un groupe.
func (p *Pont) jidMembre(ctx context.Context, cli *whatsmeow.Client, m string) (types.JID, error) {
	if strings.Contains(m, "@") {
		jid, err := types.ParseJID(m)
		if err != nil || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
			return types.JID{}, fmt.Errorf("pas une personne WhatsApp : %s", m)
		}
		return jid.ToNonAD(), nil
	}
	digits := onlyDigits(m)
	if len(digits) < 7 {
		return types.JID{}, fmt.Errorf("numéro WhatsApp invalide : %s", m)
	}
	resp, err := cli.IsOnWhatsApp(ctx, []string{"+" + digits})
	if err != nil {
		return types.JID{}, fmt.Errorf("vérification du numéro : %w", err)
	}
	for _, r := range resp {
		if r.IsIn {
			return r.JID.ToNonAD(), nil
		}
	}
	return types.JID{}, fmt.Errorf("le +%s n'est pas sur WhatsApp", digits)
}

// prenom : le premier mot d'un nom (pour le nom de groupe par défaut, court).
func prenom(nom string) string {
	if f := strings.Fields(nom); len(f) > 0 {
		return f[0]
	}
	return ""
}
