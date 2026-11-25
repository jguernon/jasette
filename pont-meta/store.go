package main

// La session (session.json) et la base SQLite (messenger.db) :
// les clés de whatsmeow et le cache local des fils, contacts et messages.
// Meta pousse les données plutôt que de les servir à la demande : on garde
// donc tout ce qui arrive, et l'interface lit dans ce cache.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite" // SQLite en pur Go (pilote « sqlite »)

	"go.mau.fi/mautrix-meta/pkg/messagix/cookies"
	"go.mau.fi/mautrix-meta/pkg/messagix/securehistory"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

// ---- La session ----

type Session struct {
	Platform types.Platform   `json:"platform"`
	Cookies  *cookies.Cookies `json:"cookies"`
	UserID   int64            `json:"user_id"`
	Name     string           `json:"name,omitempty"`
	// L'appareil « WhatsApp » enregistré pour les conversations chiffrées.
	WADeviceID uint16 `json:"wa_device_id,omitempty"`
	// L'appareil d'historique sécurisé (après le NIP à 6 chiffres).
	HistoryDevice   *securehistory.Device `json:"history_device,omitempty"`
	HistoryDeclined bool                  `json:"history_declined,omitempty"`
}

func sessionPath() string { return filepath.Join(metaDir(), "session.json") }

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
	s.Cookies.Platform = s.Platform
	return &s
}

// La session gardée, si elle permet de se connecter.
func loadSession() *Session {
	s := loadSessionAny()
	if s == nil || !s.Platform.IsValid() || !s.Cookies.IsLoggedIn() {
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

func dbPath() string { return filepath.Join(metaDir(), "messenger.db") }

const schema = `
CREATE TABLE IF NOT EXISTS jasette_conv (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_contact (id INTEGER PRIMARY KEY, data BLOB NOT NULL);
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
	ctx := p.ctx
	if _, err = db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return err
	}
	container := sqlstore.NewWithDB(db, "sqlite", waLog.Zerolog(log.With().Str("c", "whatsmeow-store").Logger()))
	if err = container.Upgrade(ctx); err != nil {
		_ = db.Close()
		return err
	}
	p.db = db
	p.waStore = container
	p.loadCache()
	return nil
}

// ---- Le cache en mémoire (fils et contacts), écrit aussi dans la base ----

type convState struct {
	Key          int64            `json:"key"`
	Type         table.ThreadType `json:"type"`
	Name         string           `json:"name,omitempty"` // nom donné au fil (groupes)
	Snippet      string           `json:"snippet,omitempty"`
	SnippetFrom  int64            `json:"snippet_from,omitempty"`
	Picture      string           `json:"picture,omitempty"`
	Folder       string           `json:"folder,omitempty"`
	Members      []int64          `json:"members,omitempty"`
	LastActivity int64            `json:"last_activity,omitempty"`
	ReadWM       int64            `json:"read_wm,omitempty"`
	Hidden       bool             `json:"hidden,omitempty"`
	ReadOnly     bool             `json:"read_only,omitempty"`
	MoreBefore   *bool            `json:"more_before,omitempty"`
	// Le dernier message connu localement (pour l'aperçu des fils chiffrés).
	LastText   string `json:"last_text,omitempty"`
	LastFromMe bool   `json:"last_from_me,omitempty"`
	LastTime   int64  `json:"last_time,omitempty"`
}

type contact struct {
	Name   string `json:"name"`
	First  string `json:"first,omitempty"`
	Avatar string `json:"avatar,omitempty"`
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
			var id int64
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
	var raw []byte
	if p.db.QueryRowContext(p.ctx, "SELECT v FROM jasette_kv WHERE k='thread_map'").Scan(&raw) == nil {
		_ = json.Unmarshal(raw, &p.threadMap)
	}
	if p.db.QueryRowContext(p.ctx, "SELECT v FROM jasette_kv WHERE k='open_threads'").Scan(&raw) == nil {
		_ = json.Unmarshal(raw, &p.openThread)
	}
	log.Info().Int("fils", len(p.convs)).Int("contacts", len(p.contacts)).Msg("cache chargé")
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
	if _, err := p.db.ExecContext(p.ctx, "INSERT INTO jasette_kv (k, v) VALUES ($1, $2) ON CONFLICT (k) DO UPDATE SET v=excluded.v", k, b); err != nil {
		log.Warn().Err(err).Msg("cache : kv")
	}
}

// ---- Les messages ----

// Ce qui est gardé pour un message : le format de l'interface, plus de quoi
// recalculer l'expéditeur et répondre aux accusés de lecture.
type msgRow struct {
	Message
	SenderID int64  `json:"sender_id,omitempty"`
	WASender string `json:"wa_sender,omitempty"` // JID de l'expéditeur (fils chiffrés)
	Read     bool   `json:"read,omitempty"`      // accusé de lecture déjà envoyé
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

func (p *Pont) delMsg(id string) {
	_, _ = p.db.ExecContext(p.ctx, "DELETE FROM jasette_msg WHERE id=$1", id)
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
	// Du plus vieux au plus récent.
	for i, j := 0, len(res)-1; i < j; i, j = i+1, j-1 {
		res[i], res[j] = res[j], res[i]
	}
	return res
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

// Mes messages reçus, pas encore marqués lus (fils chiffrés).
func (p *Pont) unreadIncoming(conv string, upTo int64) []*msgRow {
	rows := p.queryMsgs(conv, upTo, "", 50)
	var res []*msgRow
	for _, r := range rows {
		if !r.FromMe && !r.Read && r.Kind == "e2ee" {
			res = append(res, r)
		}
	}
	return res
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

func idStr(i int64) string { return strconv.FormatInt(i, 10) }
