package main

// Les personnes, groupes et canaux : leur clé dans Jasette (« u123 »,
// « g123 » pour un groupe simple, « c123 » pour un canal ou un supergroupe),
// leur nom, et l'access_hash sans lequel Telegram refuse de les adresser.

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

func itoa(n int) string { return strconv.Itoa(n) }

func keyOf(kind byte, id int64) string { return string(kind) + strconv.FormatInt(id, 10) }

// La clé d'un pair de Telegram.
func peerKey(pc tg.PeerClass) string {
	switch v := pc.(type) {
	case *tg.PeerUser:
		return keyOf('u', v.UserID)
	case *tg.PeerChat:
		return keyOf('g', v.ChatID)
	case *tg.PeerChannel:
		return keyOf('c', v.ChannelID)
	}
	return ""
}

// Découpe une clé : sa sorte et son numéro.
func splitKey(k string) (byte, int64, bool) {
	if len(k) < 2 {
		return 0, 0, false
	}
	id, err := strconv.ParseInt(k[1:], 10, 64)
	if err != nil || (k[0] != 'u' && k[0] != 'g' && k[0] != 'c') {
		return 0, 0, false
	}
	return k[0], id, true
}

// L'identifiant d'un message dans Jasette : « <fil>/<numéro> ».
func msgKey(conv string, num int) string { return conv + "/" + itoa(num) }

func parseMsgKey(id string) (string, int, bool) {
	i := strings.LastIndexByte(id, '/')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(id[i+1:])
	return id[:i], n, err == nil
}

func userName(u *tg.User) string {
	if u.Deleted {
		return "Compte supprimé"
	}
	n := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if n == "" && u.Username != "" {
		n = "@" + u.Username
	}
	if n == "" && u.Phone != "" {
		n = "+" + u.Phone
	}
	return n
}

// À appeler avec p.mu : garde une personne (ses noms, sa photo, son hash).
func (p *Pont) noteUser(u *tg.User) {
	if u == nil {
		return
	}
	k := keyOf('u', u.ID)
	c := p.peers[k]
	if c == nil {
		c = &peer{Kind: 'u', ID: u.ID}
		p.peers[k] = c
	}
	old := *c
	// Une version « min » n'a pas un hash utilisable : on garde l'ancien.
	if !u.Min || c.Hash == 0 {
		if u.AccessHash != 0 {
			c.Hash = u.AccessHash
		}
	}
	if n := userName(u); n != "" {
		c.Name = n
	}
	if !u.Min {
		c.Username = u.Username
		if u.Phone != "" {
			c.Phone = u.Phone
		}
		c.ContactOf = u.Contact
	}
	if ph, ok := u.Photo.(*tg.UserProfilePhoto); ok {
		c.Photo = ph.PhotoID
	} else if u.Photo != nil && !u.Min {
		c.Photo = 0
	}
	c.Self, c.Bot, c.Deleted = u.Self, u.Bot, u.Deleted
	if !u.Min && !u.Self && !u.Bot {
		p.notePresence(k, u.Status)
	}
	if *c != old {
		p.savePeer(k, c)
		p.listDirty = true
	}
}

func chatPhoto(ph tg.ChatPhotoClass) int64 {
	if v, ok := ph.(*tg.ChatPhoto); ok {
		return v.PhotoID
	}
	return 0
}

func (p *Pont) noteChat(ch tg.ChatClass) {
	var k string
	var c *peer
	switch v := ch.(type) {
	case *tg.Chat:
		k = keyOf('g', v.ID)
		c = p.peers[k]
		if c == nil {
			c = &peer{Kind: 'g', ID: v.ID}
			p.peers[k] = c
		}
		old := *c
		c.Name, c.Photo, c.Left, c.Members = v.Title, chatPhoto(v.Photo), v.Left || v.Deactivated, v.ParticipantsCount
		c.CanPost = !c.Left
		if *c != old {
			p.savePeer(k, c)
			p.listDirty = true
		}
	case *tg.ChatForbidden:
		k = keyOf('g', v.ID)
		c = p.peers[k]
		if c == nil {
			c = &peer{Kind: 'g', ID: v.ID}
			p.peers[k] = c
		}
		c.Name, c.Left, c.CanPost = v.Title, true, false
		p.savePeer(k, c)
	case *tg.Channel:
		k = keyOf('c', v.ID)
		c = p.peers[k]
		if c == nil {
			c = &peer{Kind: 'c', ID: v.ID}
			p.peers[k] = c
		}
		old := *c
		if !v.Min || c.Hash == 0 {
			if v.AccessHash != 0 {
				c.Hash = v.AccessHash
			}
		}
		c.Name, c.Username, c.Photo = v.Title, v.Username, chatPhoto(v.Photo)
		c.Broadcast = v.Broadcast
		if !v.Min {
			c.Left = v.Left
			c.Members = cmp.Or(v.ParticipantsCount, c.Members)
			_, admin := v.GetAdminRights()
			c.CanPost = !v.Left && (!v.Broadcast || v.Creator || admin)
		}
		if *c != old {
			p.savePeer(k, c)
			p.listDirty = true
		}
	case *tg.ChannelForbidden:
		k = keyOf('c', v.ID)
		c = p.peers[k]
		if c == nil {
			c = &peer{Kind: 'c', ID: v.ID}
			p.peers[k] = c
		}
		c.Name, c.Hash, c.Left, c.CanPost, c.Broadcast = v.Title, v.AccessHash, true, false, v.Broadcast
		p.savePeer(k, c)
	}
}

// Garde tout ce qu'une réponse de Telegram fait connaître.
func (p *Pont) noteEntities(users []tg.UserClass, chats []tg.ChatClass) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range users {
		if v, ok := u.(*tg.User); ok {
			p.noteUser(v)
		}
	}
	for _, c := range chats {
		p.noteChat(c)
	}
}

func (p *Pont) noteEnt(e tg.Entities) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range e.Users {
		p.noteUser(u)
	}
	for _, c := range e.Chats {
		p.noteChat(c)
	}
	for _, c := range e.Channels {
		p.noteChat(c)
	}
}

// À appeler avec p.mu.
func (p *Pont) peerName(k string) string {
	if c := p.peers[k]; c != nil {
		return c.Name
	}
	return ""
}

// Le pair d'une clé, pour les appels à Telegram.
func (p *Pont) inputPeer(k string) (tg.InputPeerClass, error) {
	kind, id, ok := splitKey(k)
	if !ok {
		return nil, fmt.Errorf("conversation invalide : %s", k)
	}
	p.mu.Lock()
	c := p.peers[k]
	me := p.me
	p.mu.Unlock()
	switch kind {
	case 'u':
		if id == me {
			return &tg.InputPeerSelf{}, nil
		}
		if c == nil {
			return nil, fmt.Errorf("personne inconnue : %s", k)
		}
		return &tg.InputPeerUser{UserID: id, AccessHash: c.Hash}, nil
	case 'g':
		return &tg.InputPeerChat{ChatID: id}, nil
	default:
		if c == nil {
			return nil, fmt.Errorf("canal inconnu : %s", k)
		}
		return &tg.InputPeerChannel{ChannelID: id, AccessHash: c.Hash}, nil
	}
}

func (p *Pont) inputChannel(k string) (*tg.InputChannel, error) {
	ip, err := p.inputPeer(k)
	if err != nil {
		return nil, err
	}
	ch, ok := ip.(*tg.InputPeerChannel)
	if !ok {
		return nil, errors.New("pas un canal")
	}
	return &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}, nil
}
