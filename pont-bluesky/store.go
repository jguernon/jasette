package main

// La base SQLite (bluesky.db) : le cache des messages vus (pour la
// recherche « search_messages » et les réactions) et quelques valeurs.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // SQLite en pur Go (pilote « sqlite »)
)

func dbPath() string { return filepath.Join(bsDir(), "bluesky.db") }

const schema = `
CREATE TABLE IF NOT EXISTS jasette_msg (id TEXT PRIMARY KEY, conv TEXT NOT NULL, time INTEGER NOT NULL, data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS jasette_msg_conv_time ON jasette_msg (conv, time);
CREATE TABLE IF NOT EXISTS jasette_kv (k TEXT PRIMARY KEY, v BLOB NOT NULL);
`

func (p *Pont) openStore() error {
	path := dbPath()
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if fh, err := os.OpenFile(f, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
			_ = fh.Close()
		}
		_ = os.Chmod(f, 0o600)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(p.ctx, schema); err != nil {
		_ = db.Close()
		return err
	}
	p.db = db
	return nil
}

func (p *Pont) putMsg(m Message) {
	b, _ := json.Marshal(m)
	if _, err := p.db.ExecContext(p.ctx, "INSERT OR REPLACE INTO jasette_msg (id, conv, time, data) VALUES ($1, $2, $3, $4)", m.ID, m.Conv, m.Time, b); err != nil {
		log.Warn().Err(err).Msg("cache des messages")
	}
}

func (p *Pont) getMsg(id string) *Message {
	var b []byte
	if p.db.QueryRowContext(p.ctx, "SELECT data FROM jasette_msg WHERE id = $1", id).Scan(&b) != nil {
		return nil
	}
	var m Message
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

func (p *Pont) saveKV(k string, v any) {
	b, _ := json.Marshal(v)
	_, _ = p.db.ExecContext(p.ctx, "INSERT OR REPLACE INTO jasette_kv (k, v) VALUES ($1, $2)", k, b)
}

func (p *Pont) loadKV(k string, v any) bool {
	var b []byte
	if p.db.QueryRowContext(p.ctx, "SELECT v FROM jasette_kv WHERE k = $1", k).Scan(&b) != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func (p *Pont) wipeStore() {
	_, _ = p.db.ExecContext(p.ctx, "DELETE FROM jasette_msg; DELETE FROM jasette_kv;")
}
