package main

// La liste des fils au démarrage, fils chiffrés compris.
//
// Les conversations à deux récentes de Messenger sont presque toutes
// chiffrées (ENCRYPTED_OVER_WA_ONE_TO_ONE) et vivent dans le groupe de
// synchro 95. messagix demande bien leur première page en ouvrant le socket
// (sendInitialThreadFetch), mais jette la réponse : ni les fils ni la plage
// qui permet de paginer n'arrivent. Le pont ne connaissait donc les fils
// chiffrés que par les lignes « hybrides » des fils ouverts d'avant (avec
// la date de la bascule, souvent vieille de plusieurs années), ou par un
// message reçu en direct. On redemande ici cette première page nous-mêmes,
// comme le site web, puis les pages plus anciennes au rythme habituel.

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

// Le groupe de synchro des fils chiffrés.
const groupeChiffre = 95

// Les dossiers « e2ee_cutover* » : la place d'un fil ouvert passé au
// chiffré (mautrix-meta les traite comme le dossier d'origine).
func dossierNormal(f string) string {
	switch f {
	case "e2ee_cutover":
		return "inbox"
	case "e2ee_cutover_archived":
		return "archived"
	case "e2ee_cutover_pending":
		return "pending"
	case "e2ee_cutover_other":
		return "other"
	}
	return f
}

// ---- Diagnostic (sans nom ni contenu) ----

type bilanRecus struct {
	Chiffres, Normaux int
}

// Les lignes de fils d'une table, chiffrées ou non.
func compteFils(tbl *table.LSTable) bilanRecus {
	var b bilanRecus
	if tbl == nil {
		return b
	}
	compte := func(tt table.ThreadType) {
		if tt.IsWhatsApp() {
			b.Chiffres++
		} else {
			b.Normaux++
		}
	}
	for _, th := range tbl.LSDeleteThenInsertThread {
		compte(th.ThreadType)
	}
	for _, th := range tbl.LSUpdateOrInsertThread {
		compte(th.ThreadType)
	}
	return b
}

type bilanListe struct {
	Total, Chiffres, Caches, Montres, MontresChiffres, Recents, RecentsChiffres, SansNom int
}

// L'état de la liste : combien de fils, chiffrés, cachés, montrés, et
// combien des fils montrés datent de moins de 30 jours. À appeler avec p.mu.
func (p *Pont) bilanListe(now int64) bilanListe {
	var b bilanListe
	for _, c := range p.convs {
		b.Total++
		chiffre := c.Type.IsWhatsApp()
		if chiffre {
			b.Chiffres++
		}
		if c.Hidden || hiddenFolders[c.Folder] {
			b.Caches++
			continue
		}
		b.Montres++
		if chiffre {
			b.MontresChiffres++
		}
		if t := max(c.LastActivity, c.LastTime); t > now-30*24*3600*1000 {
			b.Recents++
			if chiffre {
				b.RecentsChiffres++
			}
		}
		if !isGroupType(c.Type) && p.contactName(c.Key) == "" && c.Name == "" {
			b.SansNom++
		}
	}
	return b
}

func (p *Pont) logBilan(etape string, recus bilanRecus) {
	p.mu.Lock()
	b := p.bilanListe(time.Now().UnixMilli())
	p.mu.Unlock()
	log.Info().Str("étape", etape).
		Int("reçus_chiffrés", recus.Chiffres).Int("reçus_normaux", recus.Normaux).
		Int("fils", b.Total).Int("chiffrés", b.Chiffres).Int("cachés", b.Caches).
		Int("montrés", b.Montres).Int("montrés_chiffrés", b.MontresChiffres).
		Int("récents_30j", b.Recents).Int("récents_chiffrés", b.RecentsChiffres).
		Int("sans_nom", b.SansNom).
		Msg("fils Messenger")
}

// ---- La première page des fils chiffrés ----

// Redemande la première page du groupe 95 (la réponse de messagix est
// perdue), et met à jour la plage pour la pagination qui suit.
func (p *Pont) premierePageChiffree(cli *messagix.Client) (bilanRecus, error) {
	gen := cli.ThreadRangeGeneration(groupeChiffre)
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	tbl, err := cli.ExecuteTasks(ctx, &socket.FetchThreadsTask{
		IsAfter:                    0,
		ParentThreadKey:            -1,
		ReferenceThreadKey:         0,
		ReferenceActivityTimestamp: 9999999999999,
		AdditionalPagesToFetch:     0,
		SyncGroup:                  groupeChiffre,
	})
	annule()
	if err != nil {
		return bilanRecus{}, err
	}
	recus := compteFils(tbl)
	p.handleTable(tbl, false)
	cli.PostHandlePublishResponse(tbl)
	// La plage arrive parfois à part, par le flux du groupe 95.
	ctx, annule = context.WithTimeout(p.ctx, 15*time.Second)
	_ = cli.WaitForThreadRange(ctx, groupeChiffre, gen)
	annule()
	return recus, nil
}

// ---- Les noms qui manquent ----

// Les personnes des fils à deux montrés (les n plus récents) dont on n'a
// ni le nom ni un nom de fil. À appeler avec p.mu.
func (p *Pont) contactsManquants(n int) []int64 {
	type fil struct {
		key int64
		t   int64
	}
	var fils []fil
	for _, c := range p.convs {
		if c.Hidden || hiddenFolders[c.Folder] || isGroupType(c.Type) || c.Key == 0 || c.Key == p.me {
			continue
		}
		fils = append(fils, fil{c.Key, max(c.LastActivity, c.LastTime)})
	}
	slices.SortFunc(fils, func(a, b fil) int { return cmp.Compare(b.t, a.t) })
	if len(fils) > n {
		fils = fils[:n]
	}
	var ids []int64
	for _, f := range fils {
		ct := p.contacts[f.key]
		if ct == nil || strings.TrimSpace(ct.Name) == "" {
			ids = append(ids, f.key)
		}
	}
	return ids
}

// Demande, une à une et sans se presser, la fiche des personnes sans nom
// (souvent des fils chiffrés avec des gens qui ne sont pas des amis).
func (p *Pont) completerContacts(cli *messagix.Client, n, plafond int) {
	p.mu.Lock()
	ids := p.contactsManquants(n)
	p.mu.Unlock()
	if len(ids) > plafond {
		ids = ids[:plafond]
	}
	trouves := 0
	for _, id := range ids {
		if p.client() != cli || p.ctx.Err() != nil {
			return
		}
		ctx, annule := context.WithTimeout(p.ctx, 30*time.Second)
		tbl, err := cli.ExecuteTasks(ctx, &socket.GetContactsFullTask{ContactID: id})
		annule()
		if err != nil {
			log.Debug().Err(err).Msg("fiche d'un contact")
		} else if tbl != nil {
			if len(tbl.LSDeleteThenInsertContact) > 0 || len(tbl.LSVerifyContactRowExists) > 0 {
				trouves++
			}
			p.handleTable(tbl, false)
		}
		time.Sleep(time.Second)
	}
	if len(ids) > 0 {
		log.Info().Int("demandés", len(ids)).Int("trouvés", trouves).Msg("fiches de contacts sans nom")
		p.markDirty()
	}
}

// Une ligne du fil ouvert d'avant (clé Facebook rangée sous le fil chiffré
// qui l'a remplacé) plus vieille que ce que l'on sait déjà du fil chiffré.
func ancienFilOuvert(cleRecue, cle, activite int64, c *convState) bool {
	return cleRecue != cle && c.Type.IsWhatsApp() && activite < c.LastActivity
}
