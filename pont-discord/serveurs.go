package main

// Les serveurs Discord (guildes) : leurs salons texte et d'annonces, pour
// l'onglet Groupes (PROTOCOLE.md, §5.13).
//
// Prudence : rien ici ne parle au réseau. Les serveurs, leurs salons, mes
// rôles et les compteurs de non-lus et de mentions viennent de READY (et
// des événements qui suivent) ; aucune requête REST de plus, aucun
// abonnement « lazy guild » (op 14). Un salon n'a d'historique que si
// l'utilisateur l'ouvre (commande « messages », une page de 50 au plus).
// Les messages que la passerelle pousse d'elle-même pour ces salons sont
// acceptés ; Discord ne les pousse pas tous pour les très gros serveurs
// (il faudrait l'op 14, qu'on n'envoie pas) : ceux-là se mettent à jour
// à l'ouverture du salon.
//
// Les ids : un salon de serveur est « g<guilde>:<salon> » (l'interface y
// ajoute « d: ») ; un message privé garde l'id nu de son salon.

import (
	"sort"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Au plus tant de salons de serveurs dans la liste (en plus des messages
// privés), et tant par serveur.
const (
	maxSalons           = 2000
	maxSalonsParServeur = 150
)

type guildState struct {
	id      string
	name    string
	ownerID string
	roles   []*discordgo.Role
	myRoles []string
	member  bool // mes rôles sont connus (merged_members ou événement)
	cats    map[string]*discordgo.Channel
	chans   map[string]*discordgo.Channel // tous les salons texte et d'annonces, visibles ou non
}

// guildConvID : l'id de conversation d'un salon de serveur.
func guildConvID(guild, channel string) string { return "g" + guild + ":" + channel }

// splitConv : la guilde (vide pour un message privé) et le salon nu.
func splitConv(conv string) (guild, channel string) {
	if strings.HasPrefix(conv, "g") {
		if g, c, ok := strings.Cut(conv[1:], ":"); ok {
			return g, c
		}
	}
	return "", conv
}

// rawChannel : le salon nu, pour les requêtes à Discord.
func rawChannel(conv string) string {
	_, c := splitConv(conv)
	return c
}

// convFor : la conversation d'un salon nu (événements de la passerelle qui
// n'ont pas l'id de la guilde). À appeler avec p.mu.
func (p *Pont) convFor(channel string) string {
	if conv, ok := p.guildChan[channel]; ok {
		return conv
	}
	return channel
}

// guildProps : avec client_state_v2, le nom et le propriétaire sont dans
// « properties ».
func guildProps(g *discordgo.Guild) (name, owner string) {
	name, owner = g.Name, g.OwnerID
	if g.Properties != nil {
		name = firstOf(g.Properties.Name, name)
		owner = firstOf(g.Properties.OwnerID, owner)
	}
	return
}

// channelPerms : mes permissions dans ce salon (règles de Discord :
// @everyone, mes rôles, puis les exceptions du salon).
func channelPerms(g *guildState, ch *discordgo.Channel, me string) int64 {
	if me != "" && me == g.ownerID {
		return discordgo.PermissionAll
	}
	var perms int64
	for _, r := range g.roles {
		if r != nil && r.ID == g.id {
			perms |= r.Permissions
		}
	}
	mine := map[string]bool{}
	for _, id := range g.myRoles {
		mine[id] = true
	}
	for _, r := range g.roles {
		if r != nil && mine[r.ID] {
			perms |= r.Permissions
		}
	}
	if perms&discordgo.PermissionAdministrator != 0 {
		return discordgo.PermissionAll
	}
	for _, o := range ch.PermissionOverwrites {
		if o != nil && o.ID == g.id {
			perms &^= o.Deny
			perms |= o.Allow
		}
	}
	var deny, allow int64
	for _, o := range ch.PermissionOverwrites {
		if o != nil && o.Type == discordgo.PermissionOverwriteTypeRole && mine[o.ID] {
			deny |= o.Deny
			allow |= o.Allow
		}
	}
	perms = perms&^deny | allow
	for _, o := range ch.PermissionOverwrites {
		if o != nil && o.Type == discordgo.PermissionOverwriteTypeMember && o.ID == me {
			perms &^= o.Deny
			perms |= o.Allow
		}
	}
	return perms
}

func textLike(t discordgo.ChannelType) bool {
	return t == discordgo.ChannelTypeGuildText || t == discordgo.ChannelTypeGuildNews
}

// addGuild : un serveur de READY ou de GUILD_CREATE. « member » : mes
// rôles, s'ils sont connus. À appeler avec p.mu.
func (p *Pont) addGuild(g *discordgo.Guild, member *discordgo.Member) {
	if g == nil || g.ID == "" || g.Unavailable {
		return
	}
	gs := p.guilds[g.ID]
	if gs == nil {
		gs = &guildState{id: g.ID}
		p.guilds[g.ID] = gs
	}
	name, owner := guildProps(g)
	if name != "" {
		gs.name = name
	}
	if owner != "" {
		gs.ownerID = owner
	}
	if g.Roles != nil {
		gs.roles = g.Roles
	}
	if member != nil {
		gs.myRoles, gs.member = member.Roles, true
	}
	if g.Channels != nil {
		gs.cats = map[string]*discordgo.Channel{}
		gs.chans = map[string]*discordgo.Channel{}
		for _, ch := range g.Channels {
			p.noteGuildChannel(gs, ch)
		}
	}
	p.refreshGuild(gs)
}

// noteGuildChannel : garde un salon (catégorie, texte ou annonces). À
// appeler avec p.mu.
func (p *Pont) noteGuildChannel(gs *guildState, ch *discordgo.Channel) {
	if ch == nil {
		return
	}
	ch.GuildID = gs.id
	switch {
	case ch.Type == discordgo.ChannelTypeGuildCategory:
		gs.cats[ch.ID] = ch
	case textLike(ch.Type):
		gs.chans[ch.ID] = ch
	}
}

// refreshGuild : met p.chans d'accord avec les salons que je peux voir.
// À appeler avec p.mu.
func (p *Pont) refreshGuild(gs *guildState) {
	me := p.meID()
	visible := map[string]bool{}
	var ids []string
	for id, ch := range gs.chans {
		if channelPerms(gs, ch, me)&discordgo.PermissionViewChannel == 0 {
			continue
		}
		ids = append(ids, id)
	}
	// Au plus maxSalonsParServeur : les plus récents.
	sort.Slice(ids, func(i, j int) bool {
		return compareIDs(gs.chans[ids[i]].LastMessageID, gs.chans[ids[j]].LastMessageID) > 0
	})
	if len(ids) > maxSalonsParServeur {
		ids = ids[:maxSalonsParServeur]
	}
	for _, id := range ids {
		ch := gs.chans[id]
		conv := guildConvID(gs.id, id)
		visible[conv] = true
		c := p.chans[conv]
		if c == nil {
			c = &chanState{guild: gs.id}
			// Sans état de lecture connu : lu (sinon chaque salon jamais
			// ouvert paraîtrait non lu).
			c.readID = ch.LastMessageID
			p.chans[conv] = c
		}
		c.ch = ch
		c.canSend = channelPerms(gs, ch, me)&discordgo.PermissionSendMessages != 0
		if compareIDs(ch.LastMessageID, c.lastID) > 0 {
			c.lastID = ch.LastMessageID
			c.known = false
		}
		p.guildChan[id] = conv
	}
	prefix := "g" + gs.id + ":"
	for conv := range p.chans {
		if strings.HasPrefix(conv, prefix) && !visible[conv] {
			delete(p.chans, conv)
			delete(p.msgs, conv)
			delete(p.guildChan, rawChannel(conv))
		}
	}
}

// removeGuild : un serveur quitté (ou devenu indisponible). Rend les
// conversations retirées. À appeler avec p.mu.
func (p *Pont) removeGuild(id string) []string {
	delete(p.guilds, id)
	var gone []string
	prefix := "g" + id + ":"
	for conv := range p.chans {
		if strings.HasPrefix(conv, prefix) {
			gone = append(gone, conv)
			delete(p.chans, conv)
			delete(p.msgs, conv)
			delete(p.guildChan, rawChannel(conv))
		}
	}
	return gone
}

// guildFields : les champs §5.13 d'un salon de serveur. À appeler avec p.mu.
func (p *Pont) guildFields(out *Conversation, c *chanState) {
	gs := p.guilds[c.guild]
	out.Group, out.Large, out.Kind = true, true, "server"
	out.Announce = c.ch.Type == discordgo.ChannelTypeGuildNews
	out.Parent = "g" + c.guild
	out.Order = c.ch.Position
	out.Mentions = c.mentions
	out.ReadOnly = !c.canSend
	if gs != nil {
		out.ParentName = gs.name
		if cat := gs.cats[c.ch.ParentID]; cat != nil {
			out.Section, out.SectionOrder = cat.Name, cat.Position
		}
	}
}

// mentionsMe : le message me mentionne (@moi, @everyone, @here ou un de
// mes rôles). À appeler avec p.mu.
func (p *Pont) mentionsMe(dm *discordgo.Message) bool {
	me := p.meID()
	if dm.Author != nil && dm.Author.ID == me {
		return false
	}
	if dm.MentionEveryone {
		return true
	}
	for _, u := range dm.Mentions {
		if u != nil && u.ID == me {
			return true
		}
	}
	if gs := p.guilds[dm.GuildID]; gs != nil && len(dm.MentionRoles) > 0 {
		for _, r := range dm.MentionRoles {
			for _, mine := range gs.myRoles {
				if r == mine {
					return true
				}
			}
		}
	}
	return false
}

// replyToMe : le message répond à un des miens. À appeler avec p.mu.
func (p *Pont) replyToMe(dm *discordgo.Message, conv string) bool {
	me := p.meID()
	if dm.Author != nil && dm.Author.ID == me {
		return false
	}
	if ref := dm.ReferencedMessage; ref != nil && ref.Author != nil {
		return ref.Author.ID == me
	}
	if dm.MessageReference != nil && dm.MessageReference.MessageID != "" {
		if old := p.recalled(conv, dm.MessageReference.MessageID); old != nil {
			return old.FromMe
		}
	}
	return false
}

// Les événements des serveurs : rejoints, quittés, modifiés ; salons créés,
// modifiés, supprimés ; mes rôles changés.
func (p *Pont) onGuildCreate(g *discordgo.Guild) {
	p.mu.Lock()
	before := p.guildConvs(g.ID)
	p.addGuild(g, p.myMember(g))
	after := p.guildConvs(g.ID)
	p.mu.Unlock()
	p.announceGuildDiff(before, after)
}

func (p *Pont) onGuildDelete(g *discordgo.Guild) {
	if g == nil {
		return
	}
	p.mu.Lock()
	gone := p.removeGuild(g.ID)
	p.mu.Unlock()
	for _, conv := range gone {
		emit("deleted", map[string]any{"conv": conv})
	}
}

func (p *Pont) onGuildChannel(ch *discordgo.Channel, deleted bool) {
	p.mu.Lock()
	gs := p.guilds[ch.GuildID]
	if gs == nil {
		p.mu.Unlock()
		return
	}
	before := p.guildConvs(gs.id)
	if deleted {
		delete(gs.cats, ch.ID)
		delete(gs.chans, ch.ID)
	} else {
		if gs.cats == nil {
			gs.cats, gs.chans = map[string]*discordgo.Channel{}, map[string]*discordgo.Channel{}
		}
		p.noteGuildChannel(gs, ch)
	}
	p.refreshGuild(gs)
	after := p.guildConvs(gs.id)
	p.mu.Unlock()
	only := guildConvID(gs.id, ch.ID)
	if ch.Type == discordgo.ChannelTypeGuildCategory {
		// Le nom ou la position d'une catégorie : tous ses salons changent.
		only = ""
	}
	p.announceGuildDiffOnly(before, after, only)
}

func (p *Pont) onMyRoles(m *discordgo.Member) {
	if m == nil || m.User == nil {
		return
	}
	p.mu.Lock()
	gs := p.guilds[m.GuildID]
	if gs == nil || m.User.ID != p.meID() {
		p.mu.Unlock()
		return
	}
	before := p.guildConvs(gs.id)
	gs.myRoles, gs.member = m.Roles, true
	p.refreshGuild(gs)
	after := p.guildConvs(gs.id)
	p.mu.Unlock()
	p.announceGuildDiff(before, after)
}

// myMember : mes rôles dans un serveur, s'ils sont dans l'événement.
func (p *Pont) myMember(g *discordgo.Guild) *discordgo.Member {
	me := p.meID()
	for _, m := range g.Members {
		if m != nil && (m.UserID == me || (m.User != nil && m.User.ID == me)) {
			return m
		}
	}
	return nil
}

// guildConvs : les conversations d'un serveur (vrai = déjà connue). À
// appeler avec p.mu.
func (p *Pont) guildConvs(guild string) map[string]bool {
	out := map[string]bool{}
	prefix := "g" + guild + ":"
	for conv := range p.chans {
		if strings.HasPrefix(conv, prefix) {
			out[conv] = true
		}
	}
	return out
}

func (p *Pont) announceGuildDiff(before, after map[string]bool) {
	p.announceGuildDiffOnly(before, after, "")
}

// announceGuildDiffOnly : les conversations retirées, les nouvelles, et
// « only » (toutes celles du serveur si vide).
func (p *Pont) announceGuildDiffOnly(before, after map[string]bool, only string) {
	for conv := range before {
		if !after[conv] {
			emit("deleted", map[string]any{"conv": conv})
		}
	}
	if !p.isReady() {
		return
	}
	for conv := range after {
		if only == "" || conv == only || !before[conv] {
			p.emitConv(conv, false)
		}
	}
}

func (p *Pont) isReady() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}
