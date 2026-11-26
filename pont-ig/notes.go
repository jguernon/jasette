package main

// « Quoi de neuf ? » sur Instagram (PROTOCOLE.md, §5.12) : ma note de la
// boîte de réception, et les notes de mes contacts (bulles du Head).
//
// Voie web seulement (POST https://www.instagram.com/api/graphql, avec les
// jetons de la page : fb_dtsg, lsd…, comme les autres requêtes d'instameow) :
//   - IGDInboxTrayQuery : le plateau des notes, une par auteur, sans variable ;
//     réponse dans data.response.inbox_tray_items ;
//   - usePolarisCreateInboxTrayItemSubmitMutation : poser ma note (elle
//     remplace la précédente), en-tête X-Root-Field-Name
//     xdt_create_inbox_tray_item, réponse dans
//     data.xdt_create_inbox_tray_item.inbox_tray_item ;
//   - usePolarisDeleteInboxTrayItemSubmitMutation : l'effacer ; réponse
//     data.xdt_delete_inbox_tray_item, null en cas de succès.
// Formes tirées de dumpsta-gram (engine/dumpstagram/_private/web/…/notes.py)
// et d'OpenCLI (clis/instagram/note.js).
//
// Les doc_id ont des valeurs par défaut ici ; <données>/instagram/notes-docs.json
// ({"nom":"doc_id"}) les remplace sans recompiler. Chaque requête part une
// seule fois (MakeRequestOnce) : jamais de double publication.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-querystring/query"

	"go.mau.fi/mautrix-meta/pkg/messagix/types"
)

const (
	docTrayIG   = "IGDInboxTrayQuery"
	docCreateIG = "usePolarisCreateInboxTrayItemSubmitMutation"
	docDeleteIG = "usePolarisDeleteInboxTrayItemSubmitMutation"

	// Lecture du plateau : au plus toutes les 15 minutes.
	intervalleNotes = 15 * time.Minute
)

var docsNotesIG = map[string]string{
	docTrayIG:   "29231580869776032",
	docCreateIG: "28592645767037889",
	docDeleteIG: "28419182984337833",
}

// docNote : le doc_id d'une requête, notes-docs.json d'abord.
func docNote(nom string) string {
	if b, err := os.ReadFile(filepath.Join(igDir(), "notes-docs.json")); err == nil {
		var m map[string]string
		if json.Unmarshal(b, &m) == nil && strings.TrimSpace(m[nom]) != "" {
			return strings.TrimSpace(m[nom])
		}
	}
	return docsNotesIG[nom]
}

// errPerimee : la requête n'est plus reconnue (doc_id périmé, réponse d'une
// autre forme).
var errPerimee = errors.New("requête périmée")

// reponseGraphQL : l'enveloppe d'une réponse GraphQL de Meta.
type reponseGraphQL struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message     string `json:"message"`
		Description string `json:"description"`
		Summary     string `json:"summary"`
	} `json:"errors"`
	Error        int    `json:"error"`
	ErrorSummary string `json:"errorSummary"`
}

// lireGraphQL : la réponse brute (sans le préfixe « for (;;); »), ou une
// erreur lisible. Un document inconnu ou une réponse sans données : errPerimee.
func lireGraphQL(corps []byte, racine string) (json.RawMessage, error) {
	corps = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(corps), []byte("for (;;);")))
	var r reponseGraphQL
	if err := json.Unmarshal(corps, &r); err != nil {
		return nil, errPerimee
	}
	if r.Error != 0 {
		return nil, fmt.Errorf("%s (code %d)", strings.TrimSpace(cmpOrNote(r.ErrorSummary, "refusée")), r.Error)
	}
	brut, ok := r.Data[racine]
	if len(r.Errors) > 0 && (!ok || string(brut) == "null") {
		// Des erreurs sans données : refusée. (Des erreurs à côté de
		// données sont des avertissements.)
		m := strings.TrimSpace(cmpOrNote(r.Errors[0].Message, cmpOrNote(r.Errors[0].Summary, r.Errors[0].Description)))
		bas := strings.ToLower(m)
		if m == "" || strings.Contains(bas, "persisted") || strings.Contains(bas, "doc_id") ||
			strings.Contains(bas, "document") || strings.Contains(bas, "not found") {
			return nil, errPerimee
		}
		return nil, errors.New(m)
	}
	if !ok {
		return nil, errPerimee
	}
	return brut, nil
}

// identiteIG : mon actor_id (côté Facebook, RelayAPIConfigDefaults.actorID ;
// sinon GetOwnFBID) et mon id Instagram (PolarisViewer.id), lus dans l'état
// d'instameow (jamais dans les cookies).
func (p *Pont) identiteIG() (acteur, igid string) {
	cli := p.client()
	if cli == nil {
		return "", ""
	}
	if brut, err := cli.DumpState(); err == nil && brut != nil {
		var d struct {
			Configs struct {
				BrowserConfigTable struct {
					RelayAPIConfigDefaults struct {
						ActorID string `json:"actorID"`
					}
					PolarisViewer struct {
						ID string `json:"id"`
					}
				}
			}
		}
		if json.Unmarshal(brut, &d) == nil {
			acteur = d.Configs.BrowserConfigTable.RelayAPIConfigDefaults.ActorID
			igid = d.Configs.BrowserConfigTable.PolarisViewer.ID
		}
	}
	if acteur == "" || acteur == "0" {
		if f := cli.GetOwnFBID(); f != 0 {
			acteur = strconv.FormatInt(f, 10)
		}
	}
	return acteur, igid
}

// graphQLNote : une requête GraphQL web d'Instagram, envoyée une seule fois,
// avec l'en-tête X-Root-Field-Name si `racine` est donnée.
func (p *Pont) graphQLNote(ctx context.Context, nom, racine string, variables any) ([]byte, error) {
	cli := p.client()
	if cli == nil || cli.GetHTTP() == nil {
		return nil, erreurNote("Instagram n'est pas connecté.")
	}
	h := cli.GetHTTP()
	v, err := json.Marshal(variables)
	if err != nil {
		return nil, err
	}
	q := h.NewHTTPQuery()
	q.FbAPICallerClass = "RelayModern"
	q.FbAPIReqFriendlyName = nom
	q.Variables = string(v)
	q.ServerTimestamps = "true"
	q.DocID = docNote(nom)
	form, err := query.Values(q)
	if err != nil {
		return nil, err
	}
	en := h.BuildHeaders(true, false)
	en.Set("x-fb-friendly-name", nom)
	if racine != "" {
		en.Set("x-root-field-name", racine)
	}
	en.Set("sec-fetch-dest", "empty")
	en.Set("sec-fetch-mode", "cors")
	en.Set("sec-fetch-site", "same-origin")
	en.Set("origin", cli.GetEndpoint("base_url"))
	en.Set("referer", "https://www.instagram.com/direct/inbox/")
	resp, corps, err := h.MakeRequestOnce(ctx, cli.GetEndpoint("graphql"), "POST", en, []byte(form.Encode()), types.FORM)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		cli.GetCookies().UpdateFromResponse(resp)
		if resp.StatusCode == 400 || resp.StatusCode == 404 {
			return nil, errPerimee
		}
	}
	return corps, nil
}

func idMutation() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// erreurIG : le message montré à l'utilisateur.
func erreurIG(op string, err error) error {
	var e erreurNote
	if errors.As(err, &e) {
		return e
	}
	verbe := "publiée"
	if op == "clear" {
		verbe = "effacée"
	}
	if errors.Is(err, errPerimee) {
		return erreurNote(fmt.Sprintf("La note Instagram n'a pas pu être %s (requête périmée).", verbe))
	}
	return erreurNote(fmt.Sprintf("La note Instagram n'a pas pu être %s (%v).", verbe, err))
}

// analyserNoteCreee : l'id de la note dans la réponse d'une création.
func analyserNoteCreee(corps []byte) (string, error) {
	brut, err := lireGraphQL(corps, "xdt_create_inbox_tray_item")
	if err != nil {
		return "", err
	}
	var r struct {
		Item *struct {
			ID   string `json:"inbox_tray_item_id"`
			ID2  string `json:"id"`
			Note *struct {
				Text string `json:"text"`
			} `json:"note_dict"`
		} `json:"inbox_tray_item"`
	}
	if json.Unmarshal(brut, &r) != nil || r.Item == nil || cmpOrNote(r.Item.ID, r.Item.ID2) == "" {
		return "", errPerimee
	}
	return cmpOrNote(r.Item.ID, r.Item.ID2), nil
}

// analyserNoteEffacee : succès si la racine est là (null : succès).
func analyserNoteEffacee(corps []byte) error {
	_, err := lireGraphQL(corps, "xdt_delete_inbox_tray_item")
	return err
}

func (p *Pont) publierNoteIG(ctx context.Context, texte string, audience int, _ *etatNote) (string, error) {
	acteur, _ := p.identiteIG()
	if acteur == "" {
		return "", erreurNote("La note Instagram n'a pas pu être publiée (identifiant du compte introuvable).")
	}
	vars := map[string]any{"input": map[string]any{
		"actor_id":             acteur,
		"client_mutation_id":   idMutation(),
		"inbox_tray_item_type": "note",
		"audience":             audience,
		"additional_params": map[string]any{
			"note_create_params": map[string]any{"text": texte, "note_style": 0},
		},
	}}
	corps, err := p.graphQLNote(ctx, docCreateIG, "xdt_create_inbox_tray_item", vars)
	if err != nil {
		return "", erreurIG("set", err)
	}
	id, err := analyserNoteCreee(corps)
	if err != nil {
		return "", erreurIG("set", err)
	}
	return id, nil
}

func (p *Pont) effacerNoteIG(ctx context.Context, e *etatNote) error {
	id := e.ID
	if id == "" {
		// Une note posée ailleurs : son id vient de la dernière lecture du plateau.
		p.mu.Lock()
		id = p.maNoteTray
		p.mu.Unlock()
	}
	if id == "" {
		return erreurNote("Aucune note Instagram connue à effacer.")
	}
	corps, err := p.graphQLNote(ctx, docDeleteIG, "xdt_delete_inbox_tray_item", map[string]any{"inbox_tray_item_id": id})
	if err != nil {
		return erreurIG("clear", err)
	}
	if err := analyserNoteEffacee(corps); err != nil {
		return erreurIG("clear", err)
	}
	return nil
}

// ---- Les notes des contacts ----

// noteContact : une note du plateau.
type noteContact struct {
	ItemID   string
	AuthorID string
	FBID     int64 // interop_messaging_user_fbid de l'auteur, s'il est donné
	Username string
	Text     string
	Time     int64 // ms
}

// analyserPlateau : les notes du plateau (IGDInboxTrayQuery), dans l'ordre ;
// les éléments qui ne sont pas des notes (ambient_data…) sont sautés.
func analyserPlateau(corps []byte) ([]noteContact, error) {
	brut, err := lireGraphQL(corps, "response")
	if err != nil {
		return nil, err
	}
	var r struct {
		Items *[]struct {
			ID   string `json:"inbox_tray_item_id"`
			Type string `json:"inbox_tray_item_type"`
			Pog  struct {
				Users []struct {
					ID       string      `json:"id"`
					Username string      `json:"username"`
					FBID     json.Number `json:"interop_messaging_user_fbid"`
				} `json:"pog_users"`
			} `json:"pog_info"`
			Note *struct {
				Text      string      `json:"text"`
				AuthorID  json.Number `json:"author_id"`
				CreatedAt json.Number `json:"created_at"`
			} `json:"note_dict"`
		} `json:"inbox_tray_items"`
	}
	if err := json.Unmarshal(brut, &r); err != nil || r.Items == nil {
		return nil, errPerimee
	}
	var out []noteContact
	for _, it := range *r.Items {
		if it.Type != "note" || it.Note == nil || strings.TrimSpace(it.Note.Text) == "" {
			continue
		}
		n := noteContact{ItemID: it.ID, AuthorID: it.Note.AuthorID.String(), Text: it.Note.Text}
		if s, err := it.Note.CreatedAt.Int64(); err == nil {
			n.Time = s * 1000
		}
		for _, u := range it.Pog.Users {
			if u.ID == n.AuthorID {
				n.Username = u.Username
				n.FBID, _ = u.FBID.Int64()
			}
		}
		out = append(out, n)
	}
	return out, nil
}

func notesEnv() bool { return os.Getenv("JASETTE_NOTES") == "1" }

// setNotesContacts : la commande notes_contacts.
func (p *Pont) setNotesContacts(on bool) {
	p.mu.Lock()
	p.notesOn = on
	if !on {
		p.notesLues = time.Time{}
	}
	p.mu.Unlock()
	if on {
		p.demanderNotes(false)
	}
}

// demanderNotes : réveille la boucle ; `force` (notes_list) relit si la
// dernière lecture a plus de 15 minutes, sinon redonne la dernière.
func (p *Pont) demanderNotes(force bool) {
	if force {
		p.mu.Lock()
		derniere, recente := p.notesDerniere, time.Since(p.notesLues) < intervalleNotes
		p.mu.Unlock()
		if recente && derniere != nil {
			copie := make(map[string]any, len(derniere))
			for k, v := range derniere {
				copie[k] = v
			}
			emit("notes", copie)
			return
		}
	}
	select {
	case p.notesNow <- struct{}{}:
	default:
	}
}

// notesLoop : le plateau au plus toutes les 15 minutes, seulement si le
// réglage est activé et le pont prêt. Trois échecs de suite : une heure de pause.
func (p *Pont) notesLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	echecs := 0
	var pause time.Time
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
		case <-p.notesNow:
		}
		p.mu.Lock()
		on, ready, lues := p.notesOn, p.ready, p.notesLues
		p.mu.Unlock()
		if !on || !ready || time.Now().Before(pause) || time.Since(lues) < intervalleNotes {
			continue
		}
		p.mu.Lock()
		p.notesLues = time.Now()
		p.mu.Unlock()
		if err := p.lirePlateau(); err != nil {
			echecs++
			log.Warn().Err(err).Int("échecs", echecs).Msg("notes Instagram")
			if echecs >= 3 {
				pause = time.Now().Add(time.Hour)
				echecs = 0
			}
		} else {
			echecs = 0
		}
	}
}

func (p *Pont) lirePlateau() error {
	corps, err := p.graphQLNote(p.ctx, docTrayIG, "", map[string]any{})
	if err != nil {
		return err
	}
	notes, err := analyserPlateau(corps)
	if err != nil {
		return err
	}
	_, moi := p.identiteIG()
	items := []map[string]any{}
	p.mu.Lock()
	if !p.notesOn {
		p.mu.Unlock()
		return nil
	}
	p.maNoteTray = ""
	for _, n := range notes {
		fbid := p.byUserIGID[n.AuthorID]
		if fbid == 0 {
			fbid = n.FBID
		}
		if (moi != "" && n.AuthorID == moi) || (fbid != 0 && fbid == p.me) {
			p.maNoteTray = n.ItemID
			continue
		}
		it := map[string]any{"user": n.AuthorID, "username": n.Username, "text": n.Text, "time": n.Time}
		if fbid != 0 {
			if conv := p.convOfContact(fbid); conv != "" {
				it["conv"] = conv
			}
		}
		items = append(items, it)
	}
	ev := map[string]any{"items": items, "time": time.Now().UnixMilli()}
	p.notesDerniere = map[string]any{"items": items, "time": ev["time"]}
	p.mu.Unlock()
	emit("notes", ev)
	return nil
}

func (p *Pont) estPret() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready && p.cli != nil
}
