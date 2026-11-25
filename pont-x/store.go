package main

// La session (session.json) et la base SQLite (x.db) : les clés des
// conversations XChat, la position dans le flux XChat, et le cache local des
// fils, contacts et messages. L'interface lit dans ce cache ; X le complète à
// la demande (pages d'historique) et en direct (socket XChat, API REST).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite" // SQLite en pur Go (pilote « sqlite »)
)

// ---- La session ----

type Session struct {
	// Les cookies de x.com, au format d'un en-tête « Cookie ».
	Cookies  string `json:"cookies"`
	UserID   string `json:"user_id"`
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	// Les clés XChat du compte, récupérées avec le code d'accès (login.go) :
	// SecretKey déchiffre les clés des conversations, SigningKey signe les
	// messages envoyés. Base64 de scalaires P-256 de 32 octets.
	SecretKey         string `json:"secret_key,omitempty"`
	SigningKey        string `json:"signing_key,omitempty"`
	SigningKeyVersion string `json:"signing_key_version,omitempty"`
}

func (s *Session) hasKeys() bool {
	return s != nil && s.SecretKey != ""
}

func (s *Session) loggedIn() bool {
	return s != nil && s.Cookies != "" && s.UserID != "" && s.hasKeys()
}

func sessionPath() string { return filepath.Join(xDir(), "session.json") }

// La session gardée, même si elle ne permet plus de se connecter.
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
	return &s
}

// La session gardée, si elle permet de se connecter.
func loadSession() *Session {
	s := loadSessionAny()
	if !s.loggedIn() {
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

func dbPath() string { return filepath.Join(xDir(), "x.db") }

const schema = `
CREATE TABLE IF NOT EXISTS jasette_conv (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_contact (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jasette_msg (id TEXT PRIMARY KEY, conv TEXT NOT NULL, time INTEGER NOT NULL, data BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS jasette_msg_conv_time ON jasette_msg (conv, time);
CREATE TABLE IF NOT EXISTS jasette_kv (k TEXT PRIMARY KEY, v BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS x_conv_key (conv TEXT NOT NULL, version TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY (conv, version));
CREATE TABLE IF NOT EXISTS x_conv_token (conv TEXT PRIMARY KEY, token TEXT NOT NULL);
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
	ID      string   `json:"id"` // l'identifiant XChat : « a:b » (1-à-1) ou « g… » (groupe)
	Group   bool     `json:"group,omitempty"`
	Name    string   `json:"name,omitempty"`    // nom donné au fil (groupes)
	Picture string   `json:"picture,omitempty"` // image du groupe (chiffrée si pas sur twimg.com)
	PicKey  string   `json:"pic_key,omitempty"` // version de la clé qui chiffre l'image
	Members []string `json:"members,omitempty"`
	// Demande de message pas encore acceptée (XChat « non fiable » ou REST).
	Request bool `json:"request,omitempty"`
	Muted   bool `json:"muted,omitempty"`
	// Le fil vit dans XChat (jeton ou clé connus) ; sinon, seulement dans
	// l'ancienne API REST (demandes de messages, vieux groupes).
	XChat        bool  `json:"xchat,omitempty"`
	LastActivity int64 `json:"last_activity,omitempty"`
	ReadWM       int64 `json:"read_wm,omitempty"`
	Hidden       bool  `json:"hidden,omitempty"`
	ReadOnly     bool  `json:"read_only,omitempty"`
	NoMore       bool  `json:"no_more,omitempty"` // le début du fil est atteint
	// Le dernier message connu (pour l'aperçu).
	LastText   string `json:"last_text,omitempty"`
	LastFromMe bool   `json:"last_from_me,omitempty"`
	LastTime   int64  `json:"last_time,omitempty"`
}

type contact struct {
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
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
					c.ID = id
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

// Lit une valeur gardée dans « v ». Faux si elle n'existe pas.
func (p *Pont) loadKV(k string, v any) bool {
	var raw []byte
	if p.db.QueryRowContext(context.WithoutCancel(p.ctx), "SELECT v FROM jasette_kv WHERE k=$1", k).Scan(&raw) != nil {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

func (p *Pont) delKV(k string) {
	_, _ = p.db.ExecContext(context.WithoutCancel(p.ctx), "DELETE FROM jasette_kv WHERE k=$1", k)
}

// ---- Les messages ----

// Ce qui est gardé pour un message : le format de l'interface, plus de quoi
// recalculer l'expéditeur et retrouver l'envoi d'origine.
type msgRow struct {
	Message
	SenderID  string `json:"sender_id,omitempty"`
	RequestID string `json:"request_id,omitempty"` // l'identifiant donné à l'envoi (UUID)
}

func (p *Pont) putMsg(r *msgRow) {
	b, _ := json.Marshal(r)
	if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), "INSERT INTO jasette_msg (id, conv, time, data) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET conv=excluded.conv, time=excluded.time, data=excluded.data", r.ID, r.Conv, r.Time, b); err != nil {
		log.Warn().Err(err).Msg("cache : message")
	}
}

func (p *Pont) getMsg(id string) *msgRow {
	var data []byte
	if err := p.db.QueryRowContext(context.WithoutCancel(p.ctx), "SELECT data FROM jasette_msg WHERE id=$1", id).Scan(&data); err != nil {
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

// Le plus vieux message d'un fil dont l'identifiant est un numéro de séquence
// XChat (ou un identifiant REST) : le repère pour demander l'historique.
func (p *Pont) oldestServerMsg(conv string) *msgRow {
	rows, err := p.db.QueryContext(p.ctx, "SELECT data FROM jasette_msg WHERE conv=$1 ORDER BY time ASC LIMIT 50", conv)
	if err != nil {
		return nil
	}
	for _, r := range scanRows(rows) {
		if isDigits(r.ID) {
			return r
		}
	}
	return nil
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

// Le dernier message reçu d'un fil avec un identifiant du serveur (pour l'accusé de lecture).
func (p *Pont) lastIncoming(conv string) *msgRow {
	rows := p.queryMsgs(conv, 0, "", 30)
	for i := len(rows) - 1; i >= 0; i-- {
		if !rows[i].FromMe && rows[i].Kind != "admin" && isDigits(rows[i].ID) {
			return rows[i]
		}
	}
	return nil
}

func (p *Pont) wipeStore() error {
	var errs []error
	for _, q := range []string{"DELETE FROM jasette_msg", "DELETE FROM jasette_conv", "DELETE FROM jasette_contact", "DELETE FROM jasette_kv", "DELETE FROM x_conv_key", "DELETE FROM x_conv_token"} {
		if _, err := p.db.ExecContext(context.WithoutCancel(p.ctx), q); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseID(s string) int64 {
	i, _ := strconv.ParseInt(s, 10, 64)
	return i
}

func removeFile(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
