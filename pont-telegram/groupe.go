package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gotd/td/tg"
)

// Telegram coupe le titre d'un groupe à 128 caractères.
const maxNomGroupeTG = 128

// createGroup : un groupe simple Telegram (messages.createChat). Telegram
// exige un titre : sans nom choisi, ceux des membres. Le pont répond par la
// conversation (open), ou une erreur create_group.
func (p *Pont) createGroup(cmd Cmd) {
	membres, err := membresGroupe(cmd.Members)
	if err != nil {
		emitErr("create_group", err)
		return
	}
	api := p.tgAPI()
	if api == nil {
		emitErr("create_group", errors.New("Telegram n'est pas connecté"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	defer annule()
	users := make([]tg.InputUserClass, 0, len(membres))
	noms := []string{}
	for _, m := range membres {
		k, err := p.cleMembre(ctx, api, m)
		if err != nil {
			emitErr("create_group", err)
			return
		}
		ip, err := p.inputPeer(k)
		if err != nil {
			emitErr("create_group", err)
			return
		}
		u, ok := ip.(*tg.InputPeerUser)
		if !ok {
			emitErr("create_group", fmt.Errorf("pas une personne : %s", m))
			return
		}
		users = append(users, &tg.InputUser{UserID: u.UserID, AccessHash: u.AccessHash})
		p.mu.Lock()
		if f := strings.Fields(p.peerName(k)); len(f) > 0 {
			noms = append(noms, f[0])
		}
		p.mu.Unlock()
	}
	titre := nomGroupe(cmd.Name, noms, maxNomGroupeTG)
	if titre == "" {
		titre = "Groupe"
	}
	res, err := api.MessagesCreateChat(ctx, &tg.MessagesCreateChatRequest{Users: users, Title: titre})
	if err != nil {
		emitErr("create_group", fmt.Errorf("nouveau groupe : %w", err))
		return
	}
	conv := ""
	var chats []tg.ChatClass
	var gens []tg.UserClass
	switch u := res.Updates.(type) {
	case *tg.Updates:
		chats, gens = u.Chats, u.Users
	case *tg.UpdatesCombined:
		chats, gens = u.Chats, u.Users
	}
	p.noteEntities(gens, chats)
	for _, c := range chats {
		if ch, ok := c.(*tg.Chat); ok {
			conv = keyOf('g', ch.ID)
			break
		}
	}
	if conv == "" {
		emitErr("create_group", errors.New("nouveau groupe créé, mais introuvable : il paraîtra à la prochaine liste"))
		return
	}
	p.mu.Lock()
	c := p.getOrMakeConv(conv)
	if c.LastActivity == 0 {
		c.LastActivity = time.Now().UnixMilli()
	}
	c.Hidden = false
	p.saveConv(conv, c)
	item := p.convOut(conv, c)
	p.listDirty = true
	p.mu.Unlock()
	emit("conversation", map[string]any{"item": item, "open": true})
	go p.avatars([]string{conv})
}

// cleMembre : la clé (u<n>) d'une personne, donnée telle quelle (résultat de
// « search ») ou par son numéro (contacts.resolvePhone). Jamais un groupe.
func (p *Pont) cleMembre(ctx context.Context, api *tg.Client, m string) (string, error) {
	if kind, _, ok := splitKey(m); ok {
		if kind != 'u' {
			return "", errors.New("un groupe ou un canal ne peut pas être membre d'un groupe")
		}
		if !p.peerKnown(m) {
			return "", fmt.Errorf("personne inconnue : %s", m)
		}
		return m, nil
	}
	digits := phoneChars.ReplaceAllString(m, "")
	if !isPhone(digits) {
		return "", fmt.Errorf("identifiant Telegram invalide : %s", m)
	}
	res, err := api.ContactsResolvePhone(ctx, strings.TrimPrefix(digits, "+"))
	if err != nil {
		return "", fmt.Errorf("%s n'est pas joignable sur Telegram", m)
	}
	p.noteEntities(res.Users, res.Chats)
	k := peerKey(res.Peer)
	if !strings.HasPrefix(k, "u") {
		return "", fmt.Errorf("%s n'est pas une personne sur Telegram", m)
	}
	return k, nil
}
