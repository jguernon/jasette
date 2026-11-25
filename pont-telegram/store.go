package main

// La base SQLite (telegram.db) : le cache local de Jasette (tables
// « jasette_* ») : fils, personnes et groupes connus (avec leur access_hash,
// sans lequel Telegram refuse de les adresser) et messages. La session
// MTProto elle-même est dans session.json (gotd), à part.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // SQLite en pur Go (pilote « sqlite »)
)

func dbPath() string { return filepath.Join(tgDir(), "telegram.db") }

const schema = `
CREATE TABLE IF NOT EXISTS jasette_conv (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_peer (id TEXT PRIMARY KEY, data BLOB NOT NULL);
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
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)&_txlock=immediate")
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(p.ctx, schema); err != nil {
		_ = db.Close()
		return err
	}
	p.db = db
	p.loadCache()
	return nil
}

// ---- Le cache en mémoire (fils et personnes), écrit aussi dans la base ----

type convState struct {
	// Le dernier message connu (pour l'aperçu).
	LastText   string `json:"last_text,omitempty"`
	LastFromMe bool   `json:"last_from_me,omitempty"`
	LastTime   int64  `json:"last_time,omitempty"`
	TopID      int    `json:"top_id,omitempty"`
	// Lu jusqu'à (identifiants de messages de Telegram) : chez moi, et chez
	// l'autre (mes messages lus).
	ReadInbox  int  `json:"read_inbox,omitempty"`
	ReadOutbox int  `json:"read_outbox,omitempty"`
	Unread     int  `json:"unread,omitempty"`
	UnreadMark bool `json:"unread_mark,omitempty"`
	Archived   bool `json:"archived,omitempty"`
	Pinned     bool `json:"pinned,omitempty"`
	// Vu dans la liste des fils de Telegram (sinon : ouvert d'ici, sans message).
	InDialogs    bool  `json:"in_dialogs,omitempty"`
	LastActivity int64 `json:"last_activity,omitempty"`
	Hidden       bool  `json:"hidden,omitempty"`
}

// Une personne, un groupe ou un canal connu.
type peer struct {
	Kind       byte   `json:"kind"` // 'u', 'g' (groupe simple) ou 'c' (canal, supergroupe)
	ID         int64  `json:"id"`
	Hash       int64  `json:"hash,omitempty"`
	Name       string `json:"name,omitempty"`
	Username   string `json:"username,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Photo      int64  `json:"photo,omitempty"`
	Self       bool   `json:"self,omitempty"`
	Bot        bool   `json:"bot,omitempty"`
	Broadcast  bool   `json:"broadcast,omitempty"` // canal (pas un groupe)
	CanPost    bool   `json:"can_post,omitempty"`  // canal : je peux y publier
	Left       bool   `json:"left,omitempty"`
	Members    int    `json:"members,omitempty"`
	ContactOf  bool   `json:"contact,omitempty"`
	Deleted    bool   `json:"deleted,omitempty"`
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
	rows, err = p.db.QueryContext(p.ctx, "SELECT id, data FROM jasette_peer")
	if err == nil {
		for rows.Next() {
			var id string
			var data []byte
			if rows.Scan(&id, &data) == nil {
				var c peer
				if json.Unmarshal(data, &c) == nil {
					p.peers[id] = &c
				}
			}
		}
		_ = rows.Close()
	}
	log.Info().Int("fils", len(p.convs)).Int("personnes", len(p.peers)).Msg("cache chargé")
}

func (p *Pont) saveConv(id string, c *convState) {
	b, _ := json.Marshal(c)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_conv (id, data) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET data=excluded.data", id, b); err != nil {
		log.Warn().Err(err).Msg("cache : fil")
	}
}

func (p *Pont) savePeer(id string, c *peer) {
	b, _ := json.Marshal(c)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_peer (id, data) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET data=excluded.data", id, b); err != nil {
		log.Warn().Err(err).Msg("cache : personne")
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
// recalculer l'expéditeur et la citation.
type msgRow struct {
	Message
	Body     string `json:"body,omitempty"`      // le texte sans la citation
	SenderID string `json:"sender_id,omitempty"` // « u123 » ou « c123 »
	Num      int    `json:"num,omitempty"`       // l'identifiant de Telegram dans le fil
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

// Les messages d'un fil gardés ici, du plus vieux au plus récent, avant
// « before » (ms) si non nul.
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
	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	return res
}

// Les messages des fils non-canaux qui portent ce numéro (Telegram ne dit
// pas de quel fil vient une suppression hors canal : les numéros y sont
// uniques pour tout le compte).
func (p *Pont) msgsByNum(num int) []*msgRow {
	rows, err := p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE id LIKE $1 AND conv NOT LIKE 'c%'", "%/"+itoa(num))
	if err != nil {
		return nil
	}
	return scanRows(rows)
}

func (p *Pont) wipeStore() error {
	var errs []error
	for _, q := range []string{"DELETE FROM jasette_msg", "DELETE FROM jasette_conv", "DELETE FROM jasette_peer", "DELETE FROM jasette_kv"} {
		if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), q); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
