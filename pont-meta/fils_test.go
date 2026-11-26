package main

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix/table"
)

func pontDeTest(t *testing.T) *Pont {
	t.Helper()
	t.Setenv("JASETTE_DATA", t.TempDir())
	p := &Pont{
		ctx:        context.Background(),
		convs:      map[string]*convState{},
		contacts:   map[int64]*contact{},
		threadMap:  map[int64]int64{},
		openThread: map[int64]int64{},
		presence:   map[int64]presenceRow{},
		me:         1,
	}
	if err := p.openStore(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.db.Close() })
	return p
}

func TestDossierNormal(t *testing.T) {
	cas := map[string]string{
		"inbox": "inbox", "": "", "archived": "archived", "pending": "pending",
		"e2ee_cutover": "inbox", "e2ee_cutover_archived": "archived",
		"e2ee_cutover_pending": "pending", "e2ee_cutover_other": "other",
	}
	for in, attendu := range cas {
		if d := dossierNormal(in); d != attendu {
			t.Errorf("%q : %q, attendu %q", in, d, attendu)
		}
	}
}

func TestCompteFils(t *testing.T) {
	tbl := &table.LSTable{
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{
			{ThreadKey: 2, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE},
			{ThreadKey: 3, ThreadType: table.ONE_TO_ONE},
		},
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 4, ThreadType: table.ENCRYPTED_OVER_WA_GROUP},
		},
	}
	if b := compteFils(tbl); b.Chiffres != 2 || b.Normaux != 1 {
		t.Errorf("bilan %+v", b)
	}
	if b := compteFils(nil); b != (bilanRecus{}) {
		t.Errorf("table vide : %+v", b)
	}
}

// Un fil chiffré connu seulement par sa ligne « hybride » (date de la
// bascule, vieille) prend la date de la première page du groupe 95, et
// passe devant les vieux fils ouverts.
func TestFilChiffreRemonte(t *testing.T) {
	p := pontDeTest(t)
	now := time.Now().UnixMilli()
	jour := int64(24 * 3600 * 1000)
	p.handleTable(&table.LSTable{
		LSVerifyHybridThreadExists: []*table.LSVerifyHybridThreadExists{
			{ThreadKey: 500, ThreadJID: 50, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, LastActivityTimestampMS: now - 900*jour},
		},
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{
			{ThreadKey: 60, ThreadType: table.ONE_TO_ONE, LastActivityTimestampMs: now - 200*jour, Snippet: "vieux", FolderName: "inbox"},
		},
	}, true)
	if c := p.convs["50"]; c == nil || c.LastActivity != now-900*jour {
		t.Fatalf("fil hybride : %+v", c)
	}
	// La première page des fils chiffrés (groupe 95), avec un nouveau fil
	// chiffré inconnu jusque-là et le dossier « e2ee_cutover ».
	p.handleTable(&table.LSTable{
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{
			{ThreadKey: 50, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, LastActivityTimestampMs: now - 7*jour, FolderName: "inbox"},
			{ThreadKey: 70, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, LastActivityTimestampMs: now - 4*jour, FolderName: "e2ee_cutover"},
		},
		LSVerifyContactRowExists: []*table.LSVerifyContactRowExists{{ContactId: 70, Name: "Personne Sept"}},
	}, false)
	c50, c70 := p.convs["50"], p.convs["70"]
	if c50.LastActivity != now-7*jour || c50.Hidden {
		t.Errorf("fil 50 : %+v", c50)
	}
	if c70 == nil || !c70.Type.IsWhatsApp() || c70.Folder != "inbox" {
		t.Fatalf("fil 70 : %+v", c70)
	}
	co := p.convOut("70", c70)
	if co.Archived || co.Name != "Personne Sept" || !co.E2EE || co.Time != now-4*jour {
		t.Errorf("fil 70 montré : %+v", co)
	}
	b := p.bilanListe(now)
	if b.Total != 3 || b.Chiffres != 2 || b.Montres != 3 || b.RecentsChiffres != 2 || b.Caches != 0 {
		t.Errorf("bilan %+v", b)
	}
}

// Une vieille ligne du fil ouvert remplacé ne prend pas le nom, l'aperçu ni
// le dossier du fil chiffré ; une ligne plus récente, si.
func TestAncienFilOuvertNeRemplacePas(t *testing.T) {
	p := pontDeTest(t)
	p.threadMap[500] = 50
	p.convs["50"] = &convState{Key: 50, Type: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, Folder: "inbox", LastActivity: 1000, Snippet: "récent"}
	p.handleTable(&table.LSTable{
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{
			{ThreadKey: 500, ThreadType: table.ONE_TO_ONE, LastActivityTimestampMs: 10, Snippet: "vieux", FolderName: "archived", LastReadWatermarkTimestampMs: 10},
		},
	}, false)
	c := p.convs["50"]
	if c.Snippet != "récent" || c.Folder != "inbox" || c.LastActivity != 1000 || !c.Type.IsWhatsApp() {
		t.Errorf("fil chiffré écrasé : %+v", c)
	}
	if _, ok := p.convs["500"]; ok {
		t.Error("le fil ouvert ne doit pas réapparaître à part")
	}
	p.handleTable(&table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 500, ThreadType: table.ONE_TO_ONE, LastActivityTimestampMs: 2000, Snippet: "plus récent"},
		},
	}, false)
	if c.Snippet != "plus récent" || c.LastActivity != 2000 {
		t.Errorf("mise à jour récente perdue : %+v", c)
	}
}

func TestContactsManquants(t *testing.T) {
	p := &Pont{convs: map[string]*convState{}, contacts: map[int64]*contact{}, me: 1}
	p.convs["10"] = &convState{Key: 10, Type: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, LastActivity: 500}
	p.convs["11"] = &convState{Key: 11, Type: table.ONE_TO_ONE, LastActivity: 400}
	p.convs["12"] = &convState{Key: 12, Type: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, LastActivity: 300}
	p.convs["13"] = &convState{Key: 13, Type: table.GROUP_THREAD, LastActivity: 900}
	p.convs["14"] = &convState{Key: 14, Type: table.ONE_TO_ONE, LastActivity: 800, Hidden: true}
	p.convs["15"] = &convState{Key: 15, Type: table.ONE_TO_ONE, LastActivity: 100}
	p.contacts[11] = &contact{Name: "Onze"}
	got := p.contactsManquants(3)
	if len(got) != 2 || got[0] != 10 || got[1] != 12 {
		t.Errorf("manquants : %v", got)
	}
}

// Toutes les lignes de fils comptent, celles de LSUpdateOrInsertThread
// aussi (le flux du socket en envoie surtout de cette sorte).
func TestCompteurFils(t *testing.T) {
	p := pontDeTest(t)
	now := time.Now().UnixMilli()
	p.convs["60"] = &convState{Key: 60, Type: table.ONE_TO_ONE, Folder: "inbox", LastActivity: now - 500*jourMs}
	p.handleTable(&table.LSTable{
		LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{
			{ThreadKey: 60, ThreadType: table.ONE_TO_ONE, LastActivityTimestampMs: now - 100*jourMs},
			{ThreadKey: 70, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, LastActivityTimestampMs: now - 4*jourMs},
			{ThreadKey: 71, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, LastActivityTimestampMs: now - 900*jourMs},
			{ThreadKey: 72, ThreadType: table.ENCRYPTED_OVER_WA_ONE_TO_ONE},
		},
		LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{
			{ThreadKey: 73, ThreadType: table.ENCRYPTED_OVER_WA_GROUP, LastActivityTimestampMs: now - 40*jourMs},
			{ThreadKey: 74, ThreadType: 9999, LastActivityTimestampMs: 5},
		},
	}, false)
	k := p.recus
	attendu := compteurFils{
		Remplaces: 2, MisAJour: 4, Chiffres: 4, Normaux: 2, Nouveaux: 5, Rajeunis: 5,
		ChiffresMois: 1, ChiffresAn: 1, ChiffresVieux: 1, SansDate: 1, Douteux: 1,
	}
	if k != attendu {
		t.Errorf("compteur\n %+v\nattendu\n %+v", k, attendu)
	}
	if c := p.convs["70"]; c == nil || c.LastActivity != now-4*jourMs || c.Hidden || !c.Type.IsWhatsApp() {
		t.Errorf("fil chiffré par LSUpdateOrInsertThread : %+v", c)
	}
}

func TestLigneDouteuse(t *testing.T) {
	now := time.Now().UnixMilli()
	cas := []struct {
		cle      int64
		tt       table.ThreadType
		activite int64
		douteuse bool
	}{
		{1, table.ONE_TO_ONE, now - jourMs, false},
		{1, table.ENCRYPTED_OVER_WA_ONE_TO_ONE, 0, false},
		{0, table.ONE_TO_ONE, now, true},
		{1, table.UNKNOWN_THREAD_TYPE, now, true},
		{1, 16384, now, true},
		{1, table.ONE_TO_ONE, 1234, true},
		{1, table.ONE_TO_ONE, now + 2*jourMs, true},
	}
	for _, c := range cas {
		if d := ligneDouteuse(c.cle, c.tt, c.activite, now); d != c.douteuse {
			t.Errorf("%+v : %v", c, d)
		}
	}
}

// Un fil chiffré sans aperçu de Meta : le dernier message connu ici,
// sinon un aperçu neutre ; un fil normal sans aperçu reste vide.
func TestApercuChiffre(t *testing.T) {
	p := &Pont{convs: map[string]*convState{}, contacts: map[int64]*contact{}, me: 1}
	c := &convState{Key: 70, Type: table.ENCRYPTED_OVER_WA_ONE_TO_ONE, Folder: "inbox", LastActivity: 100}
	if co := p.convOut("70", c); co.Preview != apercuChiffre || co.FromMe {
		t.Errorf("sans message : %+v", co)
	}
	c.LastText, c.LastTime, c.LastFromMe = "salut", 90, true
	if co := p.convOut("70", c); co.Preview != "salut" || !co.FromMe {
		t.Errorf("avec message : %+v", co)
	}
	n := &convState{Key: 71, Type: table.ONE_TO_ONE, Folder: "inbox", LastActivity: 100}
	if co := p.convOut("71", n); co.Preview != "" {
		t.Errorf("fil normal : %+v", co)
	}
}
