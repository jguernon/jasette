package main

// Supprimer une conversation (commande « delete »), comme « Supprimer la
// discussion » de Messenger : le fil disparaît de mes boîtes, sur tous mes
// appareils ; les autres participants le gardent. Pour un groupe, on ne le
// quitte pas (mautrix-meta le quitte d'abord, parce qu'une salle Matrix
// supprimée vaut un départ ; ici, non).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
)

// Le fil Facebook d'origine d'un fil chiffré « hybride » (0 s'il n'y en a pas).
// À appeler avec p.mu.
func (p *Pont) fbKeyOf(cle int64) int64 {
	for fb, chiffre := range p.threadMap {
		if chiffre == cle && fb != cle {
			return fb
		}
	}
	return 0
}

// deleteConv : DeleteThreadTask, avec les valeurs de mautrix-meta
// (connector/handlematrix.go, HandleMatrixDeleteChat) : RemoveType 0 ; groupe
// de synchronisation 1 pour un fil ordinaire, 95 avec la clé du fil Facebook
// d'origine pour un fil chiffré (canal WhatsApp). Un fil chiffré sans fil
// Facebook connu ne peut pas être supprimé par ce canal : erreur claire.
//
// Réussite : la conversation est cachée ici (comme à la réception d'un
// LSDeleteThread), l'événement {"ev":"deleted","conv":"<id>"} part, puis la
// liste renouvelée. Échec : {"ev":"error","where":"delete",…} et la liste
// renouvelée (la conversation y revient si l'interface l'avait retirée).
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
		echec(errors.New("suppression impossible : Messenger n'est pas connecté"))
		return
	}
	if c == nil {
		echec(errors.New("suppression impossible : conversation Messenger inconnue"))
		return
	}
	p.mu.Lock()
	cle, groupe := c.Key, int64(1)
	if c.Type.IsWhatsApp() {
		if fb := p.fbKeyOf(c.Key); fb != 0 {
			cle, groupe = fb, 95
		} else {
			cle = 0
		}
	}
	p.mu.Unlock()
	if cle == 0 {
		echec(errors.New("Messenger ne permet pas de supprimer ce fil chiffré depuis Jasette : supprime-le dans l'app Messenger"))
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 20*time.Second)
	defer annule()
	if _, err := cli.ExecuteTasks(ctx, &socket.DeleteThreadTask{ThreadKey: cle, RemoveType: 0, SyncGroup: groupe}); err != nil {
		echec(fmt.Errorf("suppression : %w", explain(err)))
		return
	}
	p.mu.Lock()
	c.Hidden = true
	p.saveConv(cmd.Conv, c)
	p.mu.Unlock()
	emit("deleted", map[string]any{"conv": cmd.Conv})
	p.emitList()
}
