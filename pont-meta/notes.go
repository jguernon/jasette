package main

// « Quoi de neuf ? » sur Messenger (PROTOCOLE.md, §5.12) : ma note de la
// boîte de réception (24 h, privacy FRIENDS, 60 caractères), comme le site.
//
// Voie web de la session du pont : POST <domaine>/api/graphql/ (le domaine
// de la session : www.facebook.com pour une session Facebook ; messenger.com
// est fermé depuis avril 2026). Requêtes, d'après fbchat-v2
// (_createNotes.py), ws3-fca (notes.js) et FB-Notes-Extended :
//   - useMWInboxTrayCreateNoteMutation (repli :
//     MWInboxTrayNoteCreationDialogCreationStepContentMutation, seulement si
//     Facebook ne connaît pas la première) : {"input":{client_mutation_id,
//     actor_id, description, duration, note_type, privacy, session_id}},
//     réponse data.xfb_rich_status_create.status ;
//   - useMWInboxTrayDeleteNoteMutation : {"input":{client_mutation_id,
//     actor_id, rich_status_id}}, réponse data.xfb_rich_status_delete ;
//   - MWInboxTrayNoteCreationDialogQuery ({"scale":2}) : ma note actuelle,
//     data.viewer.actor.msgr_user_rich_status (pour l'effacer quand elle a été
//     posée ailleurs).
// Les doc_id : valeurs par défaut ici, remplaçables par
// <données>/messenger/notes-docs.json. Une requête part une seule fois.

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
	docLireFB    = "MWInboxTrayNoteCreationDialogQuery"
	docCreerFB   = "useMWInboxTrayCreateNoteMutation"
	docCreerFB2  = "MWInboxTrayNoteCreationDialogCreationStepContentMutation"
	docEffacerFB = "useMWInboxTrayDeleteNoteMutation"
)

var docsNotesFB = map[string]string{
	docLireFB:    "30899655739648624", // ou 26067429279547490
	docCreerFB:   "25742693715382390",
	docCreerFB2:  "24060573783603122",
	docEffacerFB: "9532619970198958",
}

func docNote(nom string) string {
	if b, err := os.ReadFile(filepath.Join(metaDir(), "notes-docs.json")); err == nil {
		var m map[string]string
		if json.Unmarshal(b, &m) == nil && strings.TrimSpace(m[nom]) != "" {
			return strings.TrimSpace(m[nom])
		}
	}
	return docsNotesFB[nom]
}

var errPerimee = errors.New("requête périmée")

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

// lireGraphQL : la racine demandée, ou une erreur (errPerimee si le document
// n'est pas reconnu ou si la réponse n'a pas la forme attendue).
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
		m := strings.TrimSpace(cmpOrNote(r.Errors[0].Message, cmpOrNote(r.Errors[0].Summary, r.Errors[0].Description)))
		bas := strings.ToLower(m)
		if m == "" || strings.Contains(bas, "persisted") || strings.Contains(bas, "doc_id") ||
			strings.Contains(bas, "document") || strings.Contains(bas, "not found") {
			return nil, errPerimee
		}
		return nil, errors.New(m)
	}
	if !ok || string(brut) == "null" {
		return nil, errPerimee
	}
	return brut, nil
}

// graphQLNote : une requête GraphQL web, envoyée une seule fois.
func (p *Pont) graphQLNote(ctx context.Context, nom string, variables any) ([]byte, error) {
	cli := p.client()
	if cli == nil || cli.GetHTTP() == nil {
		return nil, erreurNote("Messenger n'est pas connecté.")
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
	en.Set("sec-fetch-dest", "empty")
	en.Set("sec-fetch-mode", "cors")
	en.Set("sec-fetch-site", "same-origin")
	en.Set("origin", cli.GetEndpoint("base_url"))
	en.Set("referer", cli.GetEndpoint("messages"))
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

func idAleatoire() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func erreurFB(op string, err error) error {
	var e erreurNote
	if errors.As(err, &e) {
		return e
	}
	verbe := "publiée"
	if op == "clear" {
		verbe = "effacée"
	}
	if errors.Is(err, errPerimee) {
		return erreurNote(fmt.Sprintf("La note Messenger n'a pas pu être %s (requête périmée).", verbe))
	}
	return erreurNote(fmt.Sprintf("La note Messenger n'a pas pu être %s (%v).", verbe, err))
}

// analyserStatut : l'id d'une note (xfb_rich_status_create.status, ou
// viewer.actor.msgr_user_rich_status).
func analyserNoteCreee(corps []byte) (string, error) {
	brut, err := lireGraphQL(corps, "xfb_rich_status_create")
	if err != nil {
		return "", err
	}
	var r struct {
		Status *struct {
			ID  string `json:"id"`
			ID2 string `json:"rich_status_id"`
		} `json:"status"`
	}
	if json.Unmarshal(brut, &r) != nil || r.Status == nil {
		return "", errPerimee
	}
	return cmpOrNote(r.Status.ID, r.Status.ID2), nil
}

func analyserNoteActuelle(corps []byte) (id, texte string, err error) {
	brut, err := lireGraphQL(corps, "viewer")
	if err != nil {
		return "", "", err
	}
	var r struct {
		Actor *struct {
			Statut *struct {
				ID          string `json:"id"`
				ID2         string `json:"rich_status_id"`
				Description string `json:"description"`
			} `json:"msgr_user_rich_status"`
		} `json:"actor"`
	}
	if json.Unmarshal(brut, &r) != nil || r.Actor == nil {
		return "", "", errPerimee
	}
	if r.Actor.Statut == nil {
		return "", "", nil
	}
	return cmpOrNote(r.Actor.Statut.ID, r.Actor.Statut.ID2), r.Actor.Statut.Description, nil
}

func analyserNoteEffacee(corps []byte) error {
	_, err := lireGraphQL(corps, "xfb_rich_status_delete")
	return err
}

func (p *Pont) acteurFB() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.me == 0 {
		return ""
	}
	return strconv.FormatInt(p.me, 10)
}

func (p *Pont) publierNoteFB(ctx context.Context, texte string, _ int, _ *etatNote) (string, error) {
	if serviceName() != "messenger" {
		return "", erreurNote("Les notes Instagram passent par le pont Instagram (jasette-ig).")
	}
	acteur := p.acteurFB()
	if acteur == "" {
		return "", erreurNote("La note Messenger n'a pas pu être publiée (identifiant du compte introuvable).")
	}
	vars := map[string]any{"input": map[string]any{
		"client_mutation_id": idAleatoire(),
		"actor_id":           acteur,
		"description":        texte,
		"duration":           86400,
		"note_type":          "TEXT_NOTE",
		"privacy":            "FRIENDS",
		"session_id":         idAleatoire(),
	}}
	corps, err := p.graphQLNote(ctx, docCreerFB, vars)
	var id string
	if err == nil {
		id, err = analyserNoteCreee(corps)
	}
	if errors.Is(err, errPerimee) {
		// Facebook ne reconnaît pas la première mutation : rien n'a été créé,
		// on essaie (une fois) l'autre forme connue.
		log.Warn().Msg("note Messenger : première mutation périmée, repli")
		corps, err = p.graphQLNote(ctx, docCreerFB2, vars)
		if err == nil {
			id, err = analyserNoteCreee(corps)
		}
	}
	if err != nil {
		return "", erreurFB("set", err)
	}
	return id, nil
}

func (p *Pont) effacerNoteFB(ctx context.Context, e *etatNote) error {
	if serviceName() != "messenger" {
		return erreurNote("Les notes Instagram passent par le pont Instagram (jasette-ig).")
	}
	acteur := p.acteurFB()
	id := e.ID
	if id == "" {
		// Posée ailleurs (ou id inconnu) : on lit la note actuelle.
		corps, err := p.graphQLNote(ctx, docLireFB, map[string]any{"scale": 2})
		if err == nil {
			id, _, err = analyserNoteActuelle(corps)
		}
		if err != nil {
			return erreurFB("clear", err)
		}
		if id == "" {
			return nil // plus de note chez Facebook
		}
	}
	vars := map[string]any{"input": map[string]any{
		"client_mutation_id": idAleatoire(), "actor_id": acteur, "rich_status_id": id,
	}}
	corps, err := p.graphQLNote(ctx, docEffacerFB, vars)
	if err == nil {
		err = analyserNoteEffacee(corps)
	}
	if err != nil {
		return erreurFB("clear", err)
	}
	return nil
}

func (p *Pont) estPret() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready && p.cli != nil
}

func nouvellesNotesFB(p *Pont) *notesPont {
	return nouvellesNotes(opsNote{
		nom: "Messenger", limite: 60, parHeure: 6, duree: 24 * time.Hour,
		publier: p.publierNoteFB, effacer: p.effacerNoteFB,
	})
}
