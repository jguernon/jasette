package main

// La base SQLite (signal.db) : l'état de signalmeow (ses propres tables
// « signalmeow_* », gérées par sa bibliothèque) et le cache local de Jasette
// (tables « jasette_* ») : fils, contacts et messages. L'interface lit dans
// ce cache ; Signal le complète en direct (les messages arrivent tous par le
// socket, il n'y a pas d'historique à demander au serveur).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"go.mau.fi/util/dbutil"
	_ "modernc.org/sqlite" // SQLite en pur Go (pilote « sqlite »)

	"go.mau.fi/mautrix-signal/pkg/signalmeow/store"
)

func dbPath() string { return filepath.Join(sgDir(), "signal.db") }

const schema = `
CREATE TABLE IF NOT EXISTS jasette_conv (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_contact (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_msg (id TEXT PRIMARY KEY, conv TEXT NOT NULL, time INTEGER NOT NULL, data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS jasette_msg_conv_time ON jasette_msg (conv, time);
CREATE TABLE IF NOT EXISTS jasette_kv (k TEXT PRIMARY KEY, v BLOB NOT NULL);
`

func (p *Pont) openStore() error {
	path := dbPath()
	// Créés d'avance en 600 : SQLite garde ensuite ces droits.
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if fh, err := os.OpenFile(f, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
			_ = fh.Close()
		}
		_ = os.Chmod(f, 0o600)
	}
	// _txlock=immediate : les transactions de signalmeow prennent le verrou
	// d'écriture dès le début (pas d'impasse entre deux connexions).
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(p.ctx, schema); err != nil {
		_ = db.Close()
		return err
	}
	wrapped, err := dbutil.NewWithDB(db, "sqlite")
	if err != nil {
		_ = db.Close()
		return err
	}
	sm := store.NewStore(wrapped, dbutil.ZeroLogger(log.With().Str("c", "signalmeow-db").Logger()))
	if err = sm.Upgrade(p.ctx); err != nil {
		_ = db.Close()
		return err
	}
	p.db = db
	p.sm = sm
	p.loadCache()
	return nil
}

// ---- Le cache en mémoire (fils et contacts), écrit aussi dans la base ----

type convState struct {
	Group bool `json:"group,omitempty"`
	// Groupes : le nom, l'image (chemin chiffré sur le CDN de Signal), la
	// révision connue, les membres (ACI).
	Name     string   `json:"name,omitempty"`
	Avatar   string   `json:"avatar,omitempty"`
	Revision uint32   `json:"revision,omitempty"`
	Members  []string `json:"members,omitempty"`
	// Le groupe est terminé, ou je n'en fais plus partie.
	ReadOnly     bool  `json:"read_only,omitempty"`
	LastActivity int64 `json:"last_activity,omitempty"`
	ReadWM       int64 `json:"read_wm,omitempty"`
	// Jusqu'où les accusés de lecture sont partis (ou ont été lus ailleurs).
	ReceiptWM int64 `json:"receipt_wm,omitempty"`
	Hidden    bool  `json:"hidden,omitempty"`
	// Le dernier message connu (pour l'aperçu).
	LastText   string `json:"last_text,omitempty"`
	LastFromMe bool   `json:"last_from_me,omitempty"`
	LastTime   int64  `json:"last_time,omitempty"`
}

type contact struct {
	Name   string `json:"name,omitempty"`
	Number string `json:"number,omitempty"`
	// La photo de profil (chemin chiffré sur le CDN de Signal), ou l'empreinte
	// de la photo du carnet d'adresses du téléphone.
	Avatar     string `json:"avatar,omitempty"`
	AvatarHash string `json:"avatar_hash,omitempty"`
}

func (p *Pont) loadCache() {
	rows, err := p.db.QueryContext(p.ctx, "SELECT id, data FROM jasette_conv")
	if err == nil {
		for rows.Next() {
			var id string
			var data []byte
			if rows.Scan(&id, &data) == nil {
				var c convState
				if json.Unmarshal(data, &c) == nil {
					p.convs[id] = &c
				}
			}
		}
		_ = rows.Close()
	}
	rows, err = p.db.QueryContext(p.ctx, "SELECT id, data FROM jasette_contact")
	if err == nil {
		for rows.Next() {
			var id string
			var data []byte
			if rows.Scan(&id, &data) == nil {
				var c contact
				if json.Unmarshal(data, &c) == nil {
					p.contacts[id] = &c
				}
			}
		}
		_ = rows.Close()
	}
	log.Info().Int("fils", len(p.convs)).Int("contacts", len(p.contacts)).Msg("cache chargé")
}

func (p *Pont) saveConv(id string, c *convState) {
	b, _ := json.Marshal(c)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_conv (id, data) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET data=excluded.data", id, b); err != nil {
		log.Warn().Err(err).Msg("cache : fil")
	}
}

func (p *Pont) delConv(id string) {
	_, _ = p.db.ExecContext(context.WithoutCancel(p.ctx), "DELETE FROM jasette_conv WHERE id=$1", id)
}

func (p *Pont) saveContact(id string, c *contact) {
	b, _ := json.Marshal(c)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_contact (id, data) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET data=excluded.data", id, b); err != nil {
		log.Warn().Err(err).Msg("cache : contact")
	}
}

func (p *Pont) saveKV(k string, v any) {
	b, _ := json.Marshal(v)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_kv (k, v) VALUES ($1, $2) ON CONFLICT (k) DO UPDATE SET v=excluded.v", k, b); err != nil {
		log.Warn().Err(err).Msg("cache : kv")
	}
}

func (p *Pont) loadKV(k string, into any) bool {
	var raw []byte
	if p.db.QueryRowContext(p.ctx, "SELECT v FROM jasette_kv WHERE k=$1", k).Scan(&raw) != nil {
		return false
	}
	return json.Unmarshal(raw, into) == nil
}

// ---- Les messages ----

// Ce qui est gardé pour un message : le format de l'interface, plus de quoi
// recalculer l'expéditeur.
type msgRow struct {
	Message
	SenderID string   `json:"sender_id,omitempty"`
	Readers  []string `json:"readers,omitempty"` // groupes : ACI de ceux qui ont lu (mes messages)
}

func (p *Pont) putMsg(r *msgRow) {
	b, _ := json.Marshal(r)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_msg (id, conv, time, data) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET conv=excluded.conv, time=excluded.time, data=excluded.data", r.ID, r.Conv, r.Time, b); err != nil {
		log.Warn().Err(err).Msg("cache : message")
	}
}

func (p *Pont) getMsg(id string) *msgRow {
	var data []byte
	if err := p.db.QueryRowContext(p.ctx, "SELECT data FROM jasette_msg WHERE id=$1", id).Scan(&data); err != nil {
		return nil
	}
	var r msgRow
	if json.Unmarshal(data, &r) != nil {
		return nil
	}
	return &r
}

func scanRows(rows *sql.Rows) []*msgRow {
	defer rows.Close()
	var res []*msgRow
	for rows.Next() {
		var data []byte
		if rows.Scan(&data) != nil {
			continue
		}
		var r msgRow
		if json.Unmarshal(data, &r) == nil {
			res = append(res, &r)
		}
	}
	return res
}

// Les messages d'un fil, du plus vieux au plus récent, avant « before » (ms) si non nul.
func (p *Pont) queryMsgs(conv string, before int64, beforeID string, count int) []*msgRow {
	var rows *sql.Rows
	var err error
	if before > 0 {
		rows, err = p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv=$1 AND time<=$2 AND id<>$3 ORDER BY time DESC LIMIT $4", conv, before, beforeID, count)
	} else {
		rows, err = p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv=$1 ORDER BY time DESC LIMIT $2", conv, count)
	}
	if err != nil {
		log.Warn().Err(err).Msg("cache : lecture")
		return nil
	}
	res := scanRows(rows)
	// Du plus vieux au plus récent.
	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	return res
}

// Les messages reçus d'un fil depuis « after » (ms), pour les accusés de lecture.
func (p *Pont) incomingSince(conv string, after int64) []*msgRow {
	rows, err := p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv=$1 AND time>$2 ORDER BY time ASC LIMIT 500", conv, after)
	if err != nil {
		return nil
	}
	var res []*msgRow
	for _, r := range scanRows(rows) {
		if !r.FromMe && r.Kind != "admin" && r.SenderID != "" {
			res = append(res, r)
		}
	}
	return res
}

// Déplace les messages d'un fil vers un autre (un numéro devenu un compte connu).
func (p *Pont) moveMsgs(from, to string) {
	rows, err := p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv=$1", from)
	if err != nil {
		return
	}
	for _, r := range scanRows(rows) {
		r.Conv = to
		p.putMsg(r)
	}
}

func (p *Pont) wipeStore() error {
	var errs []error
	for _, q := range []string{"DELETE FROM jasette_msg", "DELETE FROM jasette_conv", "DELETE FROM jasette_contact", "DELETE FROM jasette_kv"} {
		if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), q); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Les groupes dont signalmeow connaît la clé (synchro du stockage, messages reçus).
func (p *Pont) knownGroups(aci string) []string {
	rows, err := p.db.QueryContext(p.ctx, "SELECT group_identifier FROM signalmeow_groups WHERE account_id=$1", aci)
	if err != nil {
		log.Debug().Err(err).Msg("liste des groupes")
		return nil
	}
	defer rows.Close()
	var res []string
	for rows.Next() {
		var g string
		if rows.Scan(&g) == nil && g != "" {
			res = append(res, g)
		}
	}
	return res
}
