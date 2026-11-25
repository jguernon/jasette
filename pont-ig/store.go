package main

// La session (session.json) et la base SQLite (instagram.db) : l'état
// d'instameow, la position dans le flux de deltas, et le cache local des fils,
// contacts et messages. L'interface lit dans ce cache ; Instagram le complète
// à la demande (pages de messages) et en direct (deltas du socket).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite" // SQLite en pur Go (pilote « sqlite »)

	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

// ---- La session ----

type Session struct {
	Cookies *cookies.Cookies `json:"cookies"`
	// L'identifiant « Facebook » du compte (interop), celui des messages.
	UserID int64 `json:"user_id"`
	// L'identifiant Instagram du compte (ds_user_id).
	IGID     string `json:"igid,omitempty"`
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
}

func sessionPath() string { return filepath.Join(igDir(), "session.json") }

// La session gardée, même si ses cookies ne marchent plus.
func loadSessionAny() *Session {
	b, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		log.Warn().Err(err).Msg("session illisible")
		return nil
	}
	if s.Cookies == nil {
		s.Cookies = &cookies.Cookies{}
	}
	s.Cookies.Platform = types.Instagram
	return &s
}

// La session gardée, si elle permet de se connecter.
func loadSession() *Session {
	s := loadSessionAny()
	if s == nil || !s.Cookies.IsLoggedIn() {
		return nil
	}
	return s
}

func saveSession(s *Session) {
	if s == nil {
		return
	}
	b, err := json.Marshal(s)
	if err != nil {
		log.Err(err).Msg("session : encodage")
		return
	}
	if err := writeWhole(sessionPath(), b); err != nil {
		log.Err(err).Msg("session : écriture")
	}
}

// ---- La base ----

func dbPath() string { return filepath.Join(igDir(), "instagram.db") }

const schema = `
CREATE TABLE IF NOT EXISTS jasette_conv (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_contact (id INTEGER PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_msg (id TEXT PRIMARY KEY, conv TEXT NOT NULL, time INTEGER NOT NULL, data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS jasette_msg_conv_time ON jasette_msg (conv, time);
CREATE TABLE IF NOT EXISTS jasette_kv (k TEXT PRIMARY KEY, v BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_reaction (log_id TEXT PRIMARY KEY, msg TEXT NOT NULL, sender INTEGER NOT NULL);
`

func (p *Pont) openStore() error {
	path := dbPath()
	// Créé d'avance en 600 : SQLite garde ensuite ces droits (et pour -wal / -shm).
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		_ = f.Close()
	}
	_ = os.Chmod(path, 0o600)
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
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

// ---- Le cache en mémoire (fils et contacts), écrit aussi dans la base ----

type convState struct {
	Key int64 `json:"key"` // la clé du fil (FBID) : l'identifiant donné à l'interface
	// L'identifiant court du fil Instagram (« thread_fbid ») : pages de
	// messages, envoi, réactions.
	IGID string `json:"igid,omitempty"`
	// L'identifiant long (« thread_id ») : accusés de lecture, saisie.
	IGThreadID   string  `json:"ig_thread_id,omitempty"`
	Group        bool    `json:"group,omitempty"`
	Name         string  `json:"name,omitempty"` // nom donné au fil (groupes)
	Picture      string  `json:"picture,omitempty"`
	Folder       string  `json:"folder,omitempty"`
	Members      []int64 `json:"members,omitempty"`
	LastActivity int64   `json:"last_activity,omitempty"`
	ReadWM       int64   `json:"read_wm,omitempty"`
	MarkedUnread bool    `json:"marked_unread,omitempty"`
	Hidden       bool    `json:"hidden,omitempty"`
	ReadOnly     bool    `json:"read_only,omitempty"`
	NoMore       bool    `json:"no_more,omitempty"` // le début du fil est atteint
	// Le dernier message connu (pour l'aperçu).
	LastText   string `json:"last_text,omitempty"`
	LastFromMe bool   `json:"last_from_me,omitempty"`
	LastTime   int64  `json:"last_time,omitempty"`
}

type contact struct {
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	IGID     string `json:"igid,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
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
					p.indexConv(id, &c)
				}
			}
		}
		_ = rows.Close()
	}
	rows, err = p.db.QueryContext(p.ctx, "SELECT id, data FROM jasette_contact")
	if err == nil {
		for rows.Next() {
			var id int64
			var data []byte
			if rows.Scan(&id, &data) == nil {
				var c contact
				if json.Unmarshal(data, &c) == nil {
					p.contacts[id] = &c
					if c.IGID != "" {
						p.byUserIGID[c.IGID] = id
					}
				}
			}
		}
		_ = rows.Close()
	}
	log.Info().Int("fils", len(p.convs)).Int("contacts", len(p.contacts)).Msg("cache chargé")
}

// À appeler avec p.mu : les tables de correspondance des identifiants d'un fil.
func (p *Pont) indexConv(id string, c *convState) {
	if c.IGID != "" {
		p.byIGID[c.IGID] = id
	}
	if c.IGThreadID != "" {
		p.byThreadID[c.IGThreadID] = id
	}
}

func (p *Pont) saveConv(id string, c *convState) {
	b, _ := json.Marshal(c)
	if _, err := p.db.ExecContext(p.ctx, "INSERT INTO jasette_conv (id, data) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET data=excluded.data", id, b); err != nil {
		log.Warn().Err(err).Msg("cache : fil")
	}
}

func (p *Pont) saveContact(id int64, c *contact) {
	b, _ := json.Marshal(c)
	if _, err := p.db.ExecContext(p.ctx, "INSERT INTO jasette_contact (id, data) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET data=excluded.data", id, b); err != nil {
		log.Warn().Err(err).Msg("cache : contact")
	}
}

func (p *Pont) saveKV(k string, v any) {
	b, _ := json.Marshal(v)
	p.saveKVRaw(k, b)
}

func (p *Pont) saveKVRaw(k string, b []byte) {
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

func (p *Pont) delKV(k string) {
	_, _ = p.db.ExecContext(context.WithoutCancel(p.ctx), "DELETE FROM jasette_kv WHERE k=$1", k)
}

// La position dans le flux de deltas (« seq id ») et quand elle a été prise.
type seqState struct {
	SeqID int64     `json:"seq_id"`
	TS    time.Time `json:"ts"`
}

// ---- Les réactions (pour retrouver la cible d'un retrait) ----

func (p *Pont) putReaction(logID, msg string, sender int64) {
	if logID == "" {
		return
	}
	_, _ = p.db.ExecContext(p.ctx, "INSERT INTO jasette_reaction (log_id, msg, sender) VALUES ($1, $2, $3) ON CONFLICT (log_id) DO UPDATE SET msg=excluded.msg, sender=excluded.sender", logID, msg, sender)
}

func (p *Pont) getReaction(logID string) (string, int64) {
	var msg string
	var sender int64
	if logID == "" || p.db.QueryRowContext(p.ctx, "SELECT msg, sender FROM jasette_reaction WHERE log_id=$1", logID).Scan(&msg, &sender) != nil {
		return "", 0
	}
	return msg, sender
}

// ---- Les messages ----

// Ce qui est gardé pour un message : le format de l'interface, plus de quoi
// recalculer l'expéditeur.
type msgRow struct {
	Message
	SenderID int64 `json:"sender_id,omitempty"`
}

func (p *Pont) putMsg(r *msgRow) {
	b, _ := json.Marshal(r)
	if _, err := p.db.ExecContext(p.ctx, "INSERT INTO jasette_msg (id, conv, time, data) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET conv=excluded.conv, time=excluded.time, data=excluded.data", r.ID, r.Conv, r.Time, b); err != nil {
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

// Le message qui suit « t » dans un fil (pour redemander une page autour d'un message).
func (p *Pont) nextMsg(conv string, t int64, id string) *msgRow {
	rows, err := p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv=$1 AND time>=$2 AND id<>$3 AND id NOT LIKE 'tmp_%' ORDER BY time ASC LIMIT 1", conv, t, id)
	if err != nil {
		return nil
	}
	res := scanRows(rows)
	if len(res) == 0 {
		return nil
	}
	return res[0]
}

// Mes messages envoyés récents d'un fil (pour les accusés de lecture).
func (p *Pont) myRecentMsgs(conv string, upTo int64, limit int) []*msgRow {
	rows := p.queryMsgs(conv, upTo, "", limit)
	var res []*msgRow
	for _, r := range rows {
		if r.FromMe {
			res = append(res, r)
		}
	}
	return res
}

// Le dernier message reçu d'un fil (pour l'accusé de lecture).
func (p *Pont) lastIncoming(conv string) *msgRow {
	rows := p.queryMsgs(conv, 0, "", 30)
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].FromMe && rows[i].Kind != "admin" {
			return rows[i]
		}
	}
	return nil
}

func (p *Pont) wipeStore() error {
	var errs []error
	for _, q := range []string{"DELETE FROM jasette_msg", "DELETE FROM jasette_conv", "DELETE FROM jasette_contact", "DELETE FROM jasette_kv", "DELETE FROM jasette_reaction"} {
		if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), q); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func idStr(i int64) string { return strconv.FormatInt(i, 10) }

func removeFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
