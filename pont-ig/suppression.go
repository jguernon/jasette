package main

// Supprimer une conversation (commande « delete »), comme « Supprimer la
// discussion » d'Instagram : instameow DeleteThread (la même mutation
// GraphQL que instagram.com, « ig_direct_hide_thread »), avec l'identifiant
// court du fil, comme igconnector (HandleMatrixDeleteChat). Le fil disparaît
// de mes boîtes ; les autres participants le gardent. Pour un groupe, on ne
// le quitte pas (igconnector le quitte d'abord, parce qu'une salle Matrix
// supprimée vaut un départ ; ici, non).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow"
	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
)

// deleteConv. Réussite : la conversation est cachée ici (comme à la
// réception d'un DeleteThreadEvent), l'événement {"ev":"deleted","conv":"<id>"}
// part, puis la liste renouvelée. Échec : {"ev":"error","where":"delete",…}
// et la liste renouvelée (la conversation y revient si l'interface l'avait
// retirée).
func (p *Pont) deleteConv(cmd Cmd) {
	cli := p.client()
	c := p.conv(cmd.Conv)
	if cmd.Conv == "" {
		return
	}
	echec := func(err error) {
		emitErr("delete", err)
		p.emitList()
	}
	if cli == nil {
		echec(errors.New("suppression impossible : Instagram n'est pas connecté"))
		return
	}
	if c == nil {
		echec(errors.New("suppression impossible : conversation Instagram inconnue"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
	defer annule()
	igid, _, err := p.ensureIGID(ctx, cli, cmd.Conv, c)
	// Fil introuvable chez Instagram : déjà supprimé, comme pour igconnector.
	if err != nil && !errors.Is(err, instameow.ErrThreadNotFound) || err == nil && igid == "" {
		if err == nil {
			err = errors.New("identifiant du fil introuvable")
		}
		echec(fmt.Errorf("suppression : %w", explain(err)))
		return
	}
	if err == nil {
		if _, err := cli.DeleteThread(ctx, &slidetypes.DeleteThreadRequest{ThreadID: igid, MarkAsSpam: false}); err != nil {
			echec(fmt.Errorf("suppression : %w", explain(err)))
			return
		}
	}
	p.mu.Lock()
	c.Hidden = true
	p.saveConv(cmd.Conv, c)
	p.mu.Unlock()
	emit("deleted", map[string]any{"conv": cmd.Conv})
	p.emitList()
}
