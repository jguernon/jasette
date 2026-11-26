// « Quoi de neuf ? » : les notes et statuts (PROTOCOLE.md, §5.12).
//
// Les commandes note_set, note_clear et notes_list passent telles quelles au
// pont du service choisi par le client (une trame par service) ; le serveur
// garde le dernier note_moi de chaque pont et la dernière liste `notes`
// d'Instagram pour les rejouer (ponts.go). Ici : le réglage notes_contacts,
// transmis au pont Instagram comme presence_en_ligne.
package main

import (
	"encoding/json"
	"errors"
)

// Le pont qui lit les notes des contacts.
const serviceNotesContacts = "instagram"

func envNotes(on bool) string {
	if on {
		return "JASETTE_NOTES=1"
	}
	return "JASETTE_NOTES=0"
}

// notesContactsChange : la marque notes_contacts vient de changer. Le pont
// Instagram reçoit la commande (sans redémarrer) ; désactivée, la liste gardée
// est oubliée. À appeler sous s.mu.
func (s *Serveur) notesContactsChange(on bool) {
	p := s.ponts[serviceNotesContacts]
	if p == nil {
		return
	}
	if !on {
		p.notes = nil
	}
	cmd, _ := json.Marshal(map[string]any{"cmd": "notes_contacts", "on": on})
	go func() {
		if err := p.envoyer(cmd); err != nil && !errors.Is(err, errPontArrete) {
			logf("[%s] notes_contacts : %v", p.svc, err)
		}
	}()
}
