package main

// La recherche dans tous les messages gardés (commande « search_messages »).
//
// Le cache SQLite garde chaque message en JSON (jasette_msg.data) : la
// recherche lit son texte avec json_extract, le plie (sans accents ni
// majuscules) par une fonction SQL à nous, et le compare avec LIKE. Seuls les
// résultats (au plus « limit ») remontent en mémoire, les plus récents d'abord.
//
// Ce fichier est le même dans tous les ponts qui ont ce cache (pont-meta,
// pont-ig, pont-wa, pont-signal, pont-x) ; seul texteSQL change pour WhatsApp.

import (
	"database/sql"
	"database/sql/driver"
	"strings"
	"unicode"

	"modernc.org/sqlite"
)

// Le texte d'un message dans la base (le JSON est gardé en BLOB : on le relit
// en texte). Sur WhatsApp, « body » est le texte sans la citation, s'il y en a une.
const texteSQL = "COALESCE(NULLIF(json_extract(CAST(data AS TEXT), '$.body'), ''), json_extract(CAST(data AS TEXT), '$.text'))"

func init() {
	// jasette_plier(texte) : le texte sans accents ni majuscules, pour LIKE.
	_ = sqlite.RegisterDeterministicScalarFunction("jasette_plier", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		switch v := args[0].(type) {
		case string:
			return plier(v), nil
		case []byte:
			return plier(string(v)), nil
		}
		return nil, nil
	})
}

// plier : minuscules, sans accents (comme `plier` de l'interface).
func plier(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		r = unicode.ToLower(r)
		switch r {
		case 'à', 'â', 'ä', 'á', 'ã', 'å':
			r = 'a'
		case 'é', 'è', 'ê', 'ë':
			r = 'e'
		case 'î', 'ï', 'í', 'ì':
			r = 'i'
		case 'ô', 'ö', 'ó', 'ò', 'õ':
			r = 'o'
		case 'ù', 'û', 'ü', 'ú':
			r = 'u'
		case 'ç':
			r = 'c'
		case 'ñ':
			r = 'n'
		case 'ÿ', 'ý':
			r = 'y'
		case 'œ':
			b.WriteString("oe")
			continue
		case 'æ':
			b.WriteString("ae")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Échappe les jokers de LIKE (% et _) et le caractère d'échappement lui-même.
func motifLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// searchMessages répond « search_messages » : les messages dont le texte
// contient « text », les plus récents d'abord, au plus « limit ».
func (p *Pont) searchMessages(text string, limit int) {
	items := []map[string]any{}
	defer func() {
		emit("search_messages", map[string]any{"query": text, "items": items})
	}()
	q := plier(strings.TrimSpace(text))
	if q == "" || p.db == nil {
		return
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := p.db.QueryContext(p.ctx,
		"SELECT id, conv, time, "+texteSQL+", json_extract(CAST(data AS TEXT), '$.sender'), json_extract(CAST(data AS TEXT), '$.from_me') "+
			"FROM jasette_msg WHERE id NOT LIKE 'tmp_%' AND jasette_plier("+texteSQL+") LIKE $1 ESCAPE '\\' "+
			"ORDER BY time DESC LIMIT $2", motifLike(q), limit)
	if err != nil {
		log.Warn().Err(err).Msg("recherche dans les messages")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, conv string
		var t int64
		var texte, sender sql.NullString
		var moi sql.NullInt64
		if rows.Scan(&id, &conv, &t, &texte, &sender, &moi) != nil {
			continue
		}
		items = append(items, map[string]any{
			"conv": conv, "id": id, "text": texte.String, "sender": sender.String, "time": t,
			// « from_me » comme partout ailleurs ; « fromMe » tel que demandé par le protocole.
			"from_me": moi.Int64 != 0, "fromMe": moi.Int64 != 0,
		})
	}
}
