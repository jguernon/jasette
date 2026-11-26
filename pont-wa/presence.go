package main

// La présence en ligne des contacts (PROTOCOLE.md, §5.10).
//
// Réglage « Afficher qui est en ligne » : JASETTE_PRESENCE=1 au lancement,
// puis la commande {"cmd":"presence","on":true|false}. Désactivé (ou absent),
// le pont n'en fait rien : aucun abonnement, et l'appareil reste
// « indisponible » hors des moments d'activité (le téléphone garde ses
// notifications). Activé, l'appareil se déclare « disponible » en permanence
// (WhatsApp ne transmet la présence des autres qu'à un appareil disponible) et
// s'abonne à la présence des 50 conversations individuelles les plus récentes,
// à chaque connexion (un abonnement ne survit pas à une coupure).
//
// Les ids : les conversations sont rangées sous le numéro (…@s.whatsapp.net)
// quand on le connaît (canon), et WhatsApp désigne de plus en plus les
// personnes par leur LID (…@lid). L'abonnement vise le LID quand on le
// connaît (comme WhatsApp Web, et c'est sous le LID que whatsmeow garde le
// jeton de confidentialité « tctoken » joint à la demande), sinon le numéro ;
// la présence reçue est ramenée au numéro (canon) pour retrouver la
// conversation. Sans jeton, WhatsApp peut ignorer la demande : le journal
// compte les abonnements avec et sans jeton.

import (
	"cmp"
	"context"
	"errors"
	"os"
	"slices"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Combien de conversations individuelles suivre.
const presenceMax = 50

func presenceEnv() bool { return os.Getenv("JASETTE_PRESENCE") == "1" }

// Les compteurs du journal (sous p.mu) : présences reçues depuis le dernier
// bilan, et combien n'ont pas trouvé leur conversation.
type presenceStats struct {
	premiere   bool
	recues     int
	sansConv   int
	lidSeul    int // LID sans numéro connu
	depuis     time.Time
	dispoOK    bool // la dernière déclaration « disponible » a réussi
	dispoEchec error
}

// setPresence : la commande « presence ».
func (p *Pont) setPresence(on bool) {
	p.mu.Lock()
	was := p.presenceOn
	p.presenceOn = on
	cli := p.cli
	ready := p.ready
	if !on {
		p.subscribed = map[string]bool{}
	}
	p.mu.Unlock()
	if was == on || cli == nil || !ready {
		return
	}
	if on {
		p.presenceUp(cli)
	} else {
		// Plus de présence : le téléphone retrouve ses notifications.
		p.mu.Lock()
		p.online = false
		p.mu.Unlock()
		_ = cli.SendPresence(context.WithoutCancel(p.ctx), types.PresenceUnavailable)
	}
}

// presenceUp : se déclarer disponible et s'abonner aux conversations récentes.
// Après chaque connexion (les abonnements ne survivent pas à une coupure).
func (p *Pont) presenceUp(cli *whatsmeow.Client) {
	p.mu.Lock()
	p.online = true
	p.lastActive = time.Now()
	p.subscribed = map[string]bool{}
	p.mu.Unlock()
	err := cli.SendPresence(context.WithoutCancel(p.ctx), types.PresenceAvailable)
	p.mu.Lock()
	p.pstats.dispoOK, p.pstats.dispoEchec = err == nil, err
	p.mu.Unlock()
	if err != nil {
		// Sans « disponible », WhatsApp n'envoie la présence de personne.
		msg := "présence WhatsApp : impossible de se déclarer disponible, aucune présence ne viendra"
		if errors.Is(err, whatsmeow.ErrNoPushName) {
			msg += " (nom de profil pas encore reçu : nouvel essai à sa réception)"
		}
		log.Warn().Err(err).Msg(msg)
	} else {
		log.Info().Msg("présence WhatsApp : appareil déclaré disponible")
	}
	p.subscribeRecent(cli)
}

// presenceNomRecu : le nom de profil vient d'arriver ; si la déclaration
// « disponible » avait échoué faute de nom, on la refait.
func (p *Pont) presenceNomRecu(cli *whatsmeow.Client) {
	p.mu.Lock()
	refaire := p.presenceOn && p.ready && !p.pstats.dispoOK && errors.Is(p.pstats.dispoEchec, whatsmeow.ErrNoPushName)
	p.mu.Unlock()
	if refaire {
		p.presenceUp(cli)
	}
}

// subscribeRecent s'abonne aux conversations individuelles les plus récentes
// qui ne le sont pas encore.
func (p *Pont) subscribeRecent(cli *whatsmeow.Client) {
	p.mu.Lock()
	if !p.presenceOn {
		p.mu.Unlock()
		return
	}
	type c struct {
		jid string
		t   int64
	}
	var all []c
	for id, cv := range p.convs {
		if cv.Group || cv.Hidden || p.isMe(cli, id) {
			continue
		}
		all = append(all, c{id, max(cv.LastTime, cv.LastActivity)})
	}
	slices.SortFunc(all, func(a, b c) int { return cmp.Compare(b.t, a.t) })
	if len(all) > presenceMax {
		all = all[:presenceMax]
	}
	var todo []types.JID
	for _, x := range all {
		if p.subscribed[x.jid] {
			continue
		}
		j, err := types.ParseJID(x.jid)
		if err != nil || j.Server != types.DefaultUserServer && j.Server != types.HiddenUserServer {
			continue
		}
		p.subscribed[x.jid] = true
		todo = append(todo, j)
	}
	p.mu.Unlock()
	var nLID, nJeton, nEchecs int
	for _, j := range todo {
		cible := p.presenceCible(cli, j)
		if cible.Server == types.HiddenUserServer {
			nLID++
		}
		if tok, err := cli.Store.PrivacyTokens.GetPrivacyToken(p.ctx, cible); err == nil && tok != nil && len(tok.Token) > 0 {
			nJeton++
		}
		ctx, annule := context.WithTimeout(p.ctx, 15*time.Second)
		if err := cli.SubscribePresence(ctx, cible); err != nil {
			nEchecs++
			log.Debug().Err(err).Msg("abonnement à la présence")
		}
		annule()
	}
	if len(todo) > 0 {
		log.Info().Int("contacts", len(todo)).Int("par_lid", nLID).Int("avec_jeton", nJeton).Int("échecs", nEchecs).Msg("abonné à la présence")
	}
}

// presenceCible : le LID de la personne s'il est connu, sinon son JID.
func (p *Pont) presenceCible(cli *whatsmeow.Client, j types.JID) types.JID {
	if j.Server != types.DefaultUserServer {
		return j
	}
	lid, err := cli.Store.LIDs.GetLIDForPN(p.ctx, j)
	if err != nil || lid.IsEmpty() {
		return j
	}
	return lid.ToNonAD()
}

// handlePresence : un contact est passé en ligne ou hors ligne.
func (p *Pont) handlePresence(cli *whatsmeow.Client, evt *events.Presence) {
	p.mu.Lock()
	on := p.presenceOn
	p.mu.Unlock()
	if !on {
		return
	}
	jid := p.canon(cli, evt.From)
	if skipChat(jid) || jid.Server == types.GroupServer {
		return
	}
	conv := jid.String()
	p.mu.Lock()
	cv := p.convs[conv]
	me := p.isMe(cli, conv)
	p.presenceCompter(jid, cv != nil && !cv.Group)
	p.mu.Unlock()
	if cv == nil || cv.Group || me {
		return
	}
	emit("presence", map[string]any{"item": presenceItem(conv, evt)})
}

// presenceItem : l'item de l'événement (conv = user = la conversation).
func presenceItem(conv string, evt *events.Presence) map[string]any {
	var last int64
	if !evt.LastSeen.IsZero() {
		last = evt.LastSeen.UnixMilli()
	}
	if !evt.Unavailable {
		last = time.Now().UnixMilli()
	}
	return map[string]any{"conv": conv, "user": conv, "online": !evt.Unavailable, "last_active": last}
}

// presenceCompter : le journal, sans contenu privé. À appeler avec p.mu.
func (p *Pont) presenceCompter(jid types.JID, rattache bool) {
	s := &p.pstats
	s.recues++
	if !rattache {
		s.sansConv++
		if jid.Server == types.HiddenUserServer {
			s.lidSeul++
		}
	}
	if !s.premiere {
		s.premiere = true
		s.depuis = time.Now()
		log.Info().Bool("rattachée", rattache).Bool("lid", jid.Server == types.HiddenUserServer).Msg("présence WhatsApp reçue (première fois)")
		return
	}
	if time.Since(s.depuis) >= time.Hour {
		log.Info().Int("reçues", s.recues).Int("sans_conversation", s.sansConv).Int("lid_sans_numéro", s.lidSeul).Msg("présence WhatsApp : bilan de l'heure")
		s.recues, s.sansConv, s.lidSeul, s.depuis = 0, 0, 0, time.Now()
	}
}
