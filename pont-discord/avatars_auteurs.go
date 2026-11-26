package main

// Les images des serveurs et des auteurs (onglet Groupes, PROTOCOLE.md §5.13).
//
//   - L'icône d'un serveur : {"ev":"avatar","conv":"g<guilde>","path":…},
//     une seule fois par adresse ; les clients la montrent aussi pour ses
//     salons qui n'ont pas d'image propre.
//   - La photo de l'auteur d'un message, dans un salon de serveur ou un
//     groupe privé : le message porte « sender_id », et
//     {"ev":"sender_avatar","service":"discord","id":<auteur>,"path":…}
//     donne sa photo. Paresseux : seulement pour les messages réellement
//     chargés ou reçus, une fois par adresse, au plus avatarsParMinute
//     téléchargements par minute (en plus du rythme du CDN). Une photo déjà
//     sur le disque est redonnée sans réseau avec chaque page d'historique.
//     L'avatar propre au serveur (member.avatar) d'abord, sinon celui du
//     compte ; sans avatar, rien : les clients mettent l'initiale.

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Au plus tant de photos d'auteurs téléchargées par minute.
const avatarsParMinute = 20

type demandeAuteur struct{ id, url string }

// auteurURL : la photo de l'auteur d'un message (128 px, statique).
func auteurURL(dm *discordgo.Message, guild string) string {
	if dm.Author == nil {
		return ""
	}
	if guild != "" && dm.Member != nil && dm.Member.Avatar != "" {
		return discordgo.EndpointGuildMemberAvatar(guild, dm.Author.ID, dm.Member.Avatar) + "?size=128"
	}
	return avatarURL(dm.Author)
}

func cheminAvatar(url string) string {
	return filepath.Join(mediaDir(), "avatars", hashName(url)+".png")
}

func surDisque(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// noteAuteur : un message d'un autre dans un groupe ou un salon. Pose
// sender_id, demande la photo si elle est nouvelle et rend celle déjà sur
// le disque (à redonner aux clients), ou "". À appeler avec p.mu.
func (p *Pont) noteAuteur(m *Message, dm *discordgo.Message, c *chanState) string {
	if m.FromMe || dm.Author == nil || c == nil || (c.guild == "" && (c.ch == nil || c.ch.Type != discordgo.ChannelTypeGroupDM)) {
		return ""
	}
	m.SenderID = dm.Author.ID
	url := auteurURL(dm, c.guild)
	if url == "" {
		return ""
	}
	if p.auteurs[dm.Author.ID] == url {
		if path := cheminAvatar(url); surDisque(path) {
			return path
		}
		return ""
	}
	p.auteurs[dm.Author.ID] = url
	select {
	case p.fileAuteurs <- demandeAuteur{dm.Author.ID, url}:
	default:
		// File pleine : on redemandera à la prochaine occasion.
		delete(p.auteurs, dm.Author.ID)
	}
	return ""
}

// emitAuteurs : les photos d'auteurs déjà sur le disque (id → chemin).
func emitAuteurs(connues map[string]string) {
	for id, path := range connues {
		emit("sender_avatar", map[string]any{"service": serviceName(), "id": id, "path": path})
	}
}

// boucleAuteurs : les photos demandées, une à la fois, au plus
// avatarsParMinute par minute ; celles déjà sur le disque sans attendre.
func (p *Pont) boucleAuteurs() {
	rythme := throttle{gap: time.Minute / avatarsParMinute}
	for {
		var d demandeAuteur
		select {
		case <-p.ctx.Done():
			return
		case d = <-p.fileAuteurs:
		}
		path := cheminAvatar(d.url)
		if !surDisque(path) {
			if rythme.wait(p.ctx) != nil {
				return
			}
			ctx, annule := context.WithTimeout(p.ctx, time.Minute)
			path = p.fetchImage(ctx, d.url)
			annule()
		}
		if path == "" {
			p.mu.Lock()
			if p.auteurs[d.id] == d.url {
				delete(p.auteurs, d.id) // un autre essai plus tard
			}
			p.mu.Unlock()
			continue
		}
		emitAuteurs(map[string]string{d.id: path})
	}
}

// guildIconURL : l'icône d'un serveur (128 px, statique), ou "".
func guildIconURL(gs *guildState) string {
	if gs.icon == "" {
		return ""
	}
	return discordgo.EndpointGuildIcon(gs.id, gs.icon) + "?size=128"
}

// fetchGuildIcons : l'icône de chaque serveur, une fois par adresse, au
// rythme du CDN (comme la colonne des serveurs du client web).
func (p *Pont) fetchGuildIcons() {
	p.mu.Lock()
	var todo []demandeAuteur
	for id, gs := range p.guilds {
		conv := "g" + id
		if url := guildIconURL(gs); url != "" && p.avatars[conv] != url {
			todo = append(todo, demandeAuteur{conv, url})
		}
	}
	p.mu.Unlock()
	ctx, annule := context.WithTimeout(p.ctx, 10*time.Minute)
	defer annule()
	for _, d := range todo {
		path := p.fetchImage(ctx, d.url)
		if path == "" {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		p.mu.Lock()
		p.avatars[d.id] = d.url
		p.mu.Unlock()
		emit("avatar", map[string]any{"conv": d.id, "path": path})
	}
}
