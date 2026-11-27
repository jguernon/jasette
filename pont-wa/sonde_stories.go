package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Sonde « stories actives » (étude de faisabilité) : désactivée par défaut.
//
// Active seulement si JASETTE_STORIES_SONDE=1 ou si le fichier
// <dossier du pont>/stories-sonde existe. Elle écrit, en 0600, dans
// <dossier du pont>/stories-sonde.log, une ligne JSON par observation :
// qui a une story active (ids, nom, heures), jamais le média ni le texte.
// Elle ne marque rien comme vu.

func sondeStoriesActive() bool {
	if os.Getenv("JASETTE_STORIES_SONDE") == "1" {
		return true
	}
	_, err := os.Stat(filepath.Join(waDir(), "stories-sonde"))
	return err == nil
}

var sondeStoriesMu sync.Mutex

func sondeStoriesNoter(sorte string, v any) {
	sondeStoriesMu.Lock()
	defer sondeStoriesMu.Unlock()
	b, err := json.Marshal(map[string]any{"t": time.Now().Format(time.RFC3339), "sorte": sorte, "v": v})
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(waDir(), "stories-sonde.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// sondeStatut : un statut reçu en direct (status@broadcast) : expéditeur,
// id, heure et sorte seulement. Aucun accusé de lecture n'est envoyé.
func (p *Pont) sondeStatut(cli *whatsmeow.Client, evt *events.Message) {
	exp := p.canon(cli, evt.Info.Sender)
	p.mu.Lock()
	nom := p.nameOf(exp.String())
	p.mu.Unlock()
	sorte := evt.Info.Type
	if evt.Message != nil && evt.Message.GetProtocolMessage() != nil {
		sorte = "protocole:" + evt.Message.GetProtocolMessage().GetType().String()
	}
	sondeStoriesNoter("whatsapp.statut", map[string]any{
		"id": evt.Info.ID, "expediteur": exp.String(), "brut": evt.Info.Sender.String(), "nom": nom,
		"pushname": evt.Info.PushName, "heure": evt.Info.Timestamp.Format(time.RFC3339),
		"sorte": sorte, "media": evt.Info.MediaType,
	})
}

// sondeStatutsHistorique : les statuts de l'historique (statusV3Messages).
func (p *Pont) sondeStatutsHistorique(cli *whatsmeow.Client, l []*waWeb.WebMessageInfo) {
	rows := []map[string]any{}
	for _, m := range l {
		k := m.GetKey()
		brut := m.GetParticipant()
		if brut == "" {
			brut = k.GetParticipant()
		}
		exp := brut
		nom := ""
		if j, err := types.ParseJID(brut); err == nil {
			exp = p.canon(cli, j).String()
		}
		p.mu.Lock()
		nom = p.nameOf(exp)
		p.mu.Unlock()
		rows = append(rows, map[string]any{
			"id": k.GetID(), "expediteur": exp, "nom": nom, "de_moi": k.GetFromMe(),
			"heure": time.Unix(int64(m.GetMessageTimestamp()), 0).Format(time.RFC3339),
		})
	}
	sondeStoriesNoter("whatsapp.historique", map[string]any{"n": len(l), "statuts": rows})
}
