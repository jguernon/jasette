package main

// La base SQLite (whatsapp.db) : les tables de whatsmeow (clés de l'appareil
// lié, sessions Signal, contacts, état des applis) et le cache de Jasette
// (fils et messages). L'interface lit dans ce cache ; le téléphone le remplit
// (history sync) et le socket le tient à jour.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // SQLite en pur Go (pilote « sqlite »)

	waLog "go.mau.fi/whatsmeow/util/log"

	"go.mau.fi/whatsmeow/store/sqlstore"
)

func dbPath() string { return filepath.Join(waDir(), "whatsapp.db") }

const schema = `
CREATE TABLE IF NOT EXISTS jasette_conv (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_msg (id TEXT PRIMARY KEY, conv TEXT NOT NULL, time INTEGER NOT NULL, data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS jasette_msg_conv_time ON jasette_msg (conv, time);
CREATE TABLE IF NOT EXISTS jasette_kv (k TEXT PRIMARY KEY, v BLOB NOT NULL);
`

func (p *Pont) openStore() error {
	path := dbPath()
	// Créé d'avance en 600 : SQLite garde ensuite ces droits (et pour -wal / -shm).
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		_ = f.Close()
	}
	_ = os.Chmod(path, 0o600)
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(p.ctx, schema); err != nil {
		_ = db.Close()
		return err
	}
	container := sqlstore.NewWithDB(db, "sqlite", waLog.Zerolog(log.With().Str("c", "whatsmeow-store").Logger()))
	if err = container.Upgrade(p.ctx); err != nil {
		_ = db.Close()
		return err
	}
	p.db = db
	p.waStore = container
	p.loadCache()
	return nil
}

// ---- Le cache en mémoire des fils, écrit aussi dans la base ----

type convState struct {
	JID          string   `json:"jid"` // l'identifiant donné à l'interface
	Group        bool     `json:"group,omitempty"`
	Name         string   `json:"name,omitempty"` // nom du groupe (ou donné par l'historique)
	Archived     bool     `json:"archived,omitempty"`
	Unread       int      `json:"unread,omitempty"`
	MarkedUnread bool     `json:"marked_unread,omitempty"`
	ReadWM       int64    `json:"read_wm,omitempty"` // jusqu'où j'ai lu (ms)
	ReadOnly     bool     `json:"read_only,omitempty"`
	Hidden       bool     `json:"hidden,omitempty"`
	Members      []string `json:"members,omitempty"` // JID des membres (groupes)
	LastActivity int64    `json:"last_activity,omitempty"`
	NoMore       bool     `json:"no_more,omitempty"` // le début du fil est atteint
	// Le dernier message connu (pour l'aperçu).
	LastText   string `json:"last_text,omitempty"`
	LastFromMe bool   `json:"last_from_me,omitempty"`
	LastTime   int64  `json:"last_time,omitempty"`
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
	log.Info().Int("fils", len(p.convs)).Msg("cache chargé")
}

func (p *Pont) saveConv(id string, c *convState) {
	b, _ := json.Marshal(c)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_conv (id, data) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET data=excluded.data", id, b); err != nil {
		log.Warn().Err(err).Msg("cache : fil")
	}
}

func (p *Pont) saveKV(k string, v any) {
	b, _ := json.Marshal(v)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_kv (k, v) VALUES ($1, $2) ON CONFLICT (k) DO UPDATE SET v=excluded.v", k, b); err != nil {
		log.Warn().Err(err).Msg("cache : kv")
	}
}

func (p *Pont) loadKV(k string) []byte {
	var raw []byte
	if p.db.QueryRowContext(p.ctx, "SELECT v FROM jasette_kv WHERE k=$1", k).Scan(&raw) != nil {
		return nil
	}
	return raw
}

// ---- Les messages ----

// Ce qui est gardé pour un message : le format de l'interface, plus de quoi
// recalculer l'expéditeur, répondre aux accusés et redemander l'historique.
type msgRow struct {
	Message
	SenderJID string   `json:"sender_jid,omitempty"`
	ChatJID   string   `json:"chat_jid,omitempty"` // le JID d'origine du fil (peut être un LID)
	Body      string   `json:"body,omitempty"`     // le texte sans la citation (aperçu)
	Readers   []string `json:"readers,omitempty"`  // groupes : JID de ceux qui ont lu (mes messages)
}

func (p *Pont) putMsg(r *msgRow) {
	b, _ := json.Marshal(r)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_msg (id, conv, time, data) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET conv=excluded.conv, time=excluded.time, data=excluded.data", r.ID, r.Conv, r.Time, b); err != nil {
		log.Warn().Err(err).Msg("cache : message")
	}
}

func (p *Pont) getMsg(id string) *msgRow {
	var data []byte
	if id == "" || p.db.QueryRowContext(p.ctx, "SELECT data FROM jasette_msg WHERE id=$1", id).Scan(&data) != nil {
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

// Le plus vieux message gardé d'un fil (pour redemander l'historique au téléphone).
func (p *Pont) oldestMsg(conv string) *msgRow {
	rows, err := p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv=$1 AND id NOT LIKE 'tmp_%' ORDER BY time ASC LIMIT 1", conv)
	if err != nil {
		return nil
	}
	res := scanRows(rows)
	if len(res) == 0 {
		return nil
	}
	return res[0]
}

func (p *Pont) wipeStore() error {
	var errs []error
	for _, q := range []string{"DELETE FROM jasette_msg", "DELETE FROM jasette_conv", "DELETE FROM jasette_kv"} {
		if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), q); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
