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
	"encoding/json"
	"slices"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/graphql"
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

// Toutes les lignes de fils reçues depuis le lancement, d'où qu'elles
// viennent (réponses à nos demandes ou flux du socket) : de quoi voir, sans
// nom ni contenu, si les fils chiffrés arrivent et avec quelle date.
type compteurFils struct {
	Remplaces, MisAJour      int // LSDeleteThenInsertThread, LSUpdateOrInsertThread
	Chiffres, Normaux        int
	Nouveaux, Rajeunis       int // fil inconnu jusque-là ; date d'activité avancée
	ChiffresMois, ChiffresAn int // date de la ligne chiffrée : < 30 j, < 1 an
	ChiffresVieux, SansDate  int // plus vieille ; aucune date
	Douteux                  int // clé, type ou date invraisemblable (champs décalés ?)
}

const jourMs = int64(24 * 3600 * 1000)

// Note une ligne de fil. À appeler avec p.mu.
func (k *compteurFils) noter(remplace bool, cle int64, tt table.ThreadType, activite int64, nouveau, rajeuni bool, now int64) {
	if remplace {
		k.Remplaces++
	} else {
		k.MisAJour++
	}
	if nouveau {
		k.Nouveaux++
	}
	if rajeuni {
		k.Rajeunis++
	}
	if ligneDouteuse(cle, tt, activite, now) {
		k.Douteux++
	}
	if !tt.IsWhatsApp() {
		k.Normaux++
		return
	}
	k.Chiffres++
	switch {
	case activite <= 0:
		k.SansDate++
	case activite > now-30*jourMs:
		k.ChiffresMois++
	case activite > now-365*jourMs:
		k.ChiffresAn++
	default:
		k.ChiffresVieux++
	}
}

// Une ligne de fil qu'un décalage des champs aurait abîmée : clé nulle,
// type inconnu de messagix, date hors de 2004 .. demain.
func ligneDouteuse(cle int64, tt table.ThreadType, activite, now int64) bool {
	if cle <= 0 || tt <= table.UNKNOWN_THREAD_TYPE || tt > 1000 {
		return true
	}
	return activite != 0 && (activite < 1072915200000 || activite > now+jourMs)
}

func (p *Pont) logBilan(etape string) {
	p.mu.Lock()
	b := p.bilanListe(time.Now().UnixMilli())
	k := p.recus
	p.mu.Unlock()
	log.Info().Str("étape", etape).
		Int("reçus_chiffrés", k.Chiffres).Int("reçus_normaux", k.Normaux).
		Int("lignes_remplace", k.Remplaces).Int("lignes_maj", k.MisAJour).
		Int("nouveaux", k.Nouveaux).Int("rajeunis", k.Rajeunis).
		Int("chiffrés_30j", k.ChiffresMois).Int("chiffrés_1an", k.ChiffresAn).
		Int("chiffrés_vieux", k.ChiffresVieux).Int("sans_date", k.SansDate).
		Int("douteux", k.Douteux).
		Int("fils", b.Total).Int("chiffrés", b.Chiffres).Int("cachés", b.Caches).
		Int("montrés", b.Montres).Int("montrés_chiffrés", b.MontresChiffres).
		Int("récents_30j", b.Recents).Int("récents_chiffrés", b.RecentsChiffres).
		Int("sans_nom", b.SansNom).
		Msg("fils Messenger")
}

// ---- La première page des fils chiffrés ----

// Redemande la première page du groupe 95 (la réponse de messagix est
// perdue), et met à jour la plage pour la pagination qui suit.
func (p *Pont) premierePageChiffree(cli *messagix.Client) {
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
		log.Warn().Err(err).Msg("fils chiffrés : première page")
		return
	}
	recus := compteFils(tbl)
	p.handleTable(tbl, false)
	cli.PostHandlePublishResponse(tbl)
	// La plage arrive parfois à part, par le flux du groupe 95.
	ctx, annule = context.WithTimeout(p.ctx, 15*time.Second)
	plage := cli.WaitForThreadRange(ctx, groupeChiffre, gen) == nil
	annule()
	log.Info().Int("réponse_chiffrés", recus.Chiffres).Int("réponse_normaux", recus.Normaux).
		Bool("plage_reçue", plage).Msg("fils Messenger : première page du groupe 95")
}

// L'instantané du groupe 95 par GraphQL, comme au chargement de la page
// quand elle n'a pas de synchro (had_page_sync=false) : messagix le demande
// aussi, mais n'en garde que les liens « fil ouvert -> fil chiffré ». Une
// seule requête, sans toucher aux curseurs du flux.
func (p *Pont) instantaneChiffre(cli *messagix.Client) {
	version, params, ok := paramsSynchro(cli)
	for essai := 0; !ok && essai < 5 && p.ctx.Err() == nil; essai++ {
		// L'état n'est lisible qu'une fois le socket synchronisé.
		time.Sleep(2 * time.Second)
		version, params, ok = paramsSynchro(cli)
	}
	if !ok {
		log.Info().Msg("fils Messenger : instantané du groupe 95 impossible (pas de paramètres)")
		return
	}
	ctx, annule := context.WithTimeout(p.ctx, 45*time.Second)
	tbl, err := cli.GetHTTP().MakeLSRequest(ctx, &graphql.LSPlatformGraphQLLightspeedVariables{
		Database:          groupeChiffre,
		Version:           version,
		SyncParams:        &params,
		LastAppliedCursor: nil,
	}, 1)
	annule()
	if err != nil {
		log.Warn().Err(err).Msg("fils chiffrés : instantané")
		return
	}
	recus := compteFils(tbl)
	p.handleTable(tbl, false)
	log.Info().Int("réponse_chiffrés", recus.Chiffres).Int("réponse_normaux", recus.Normaux).
		Int("liens_chiffrés", len(tbl.LSCreateOpenToE2EEThreadLink)+len(tbl.LSVerifyHybridThreadExists)).
		Int("contacts", len(tbl.LSDeleteThenInsertContact)+len(tbl.LSVerifyContactRowExists)).
		Msg("fils Messenger : instantané du groupe 95")
}

// La version et les paramètres de synchro « contact » (ceux que messagix
// donne au groupe 95), lus dans l'état de messagix : rien n'est gardé ni
// écrit dans le journal.
func paramsSynchro(cli *messagix.Client) (int64, string, bool) {
	raw, err := cli.DumpState()
	if err != nil || raw == nil {
		return 0, "", false
	}
	var etat struct {
		Configs *struct {
			VersionID          int64
			BrowserConfigTable *struct {
				LSPlatformMessengerSyncParams struct{ Contact string }
			}
		}
	}
	if json.Unmarshal(raw, &etat) != nil || etat.Configs == nil || etat.Configs.BrowserConfigTable == nil {
		return 0, "", false
	}
	params := etat.Configs.BrowserConfigTable.LSPlatformMessengerSyncParams.Contact
	return etat.Configs.VersionID, params, etat.Configs.VersionID != 0 && params != ""
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
