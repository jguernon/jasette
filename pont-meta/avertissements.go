package main

// Les avertissements de décodage de messagix (journal global de zerolog) :
// des centaines par démarrage (« Failed to set int64 » quand Meta ajoute une
// colonne à une table, « Skipping dependency… » pour les procédures que
// messagix ne connaît pas). Ils ne disent rien de neuf d'une fois à l'autre :
// on les compte, et une ligne de résumé les remplace.

import (
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

var bruitMessagix = map[string]bool{
	"Failed to set int64":                        true,
	"Failed to set string":                       true,
	"Failed to set bool":                         true,
	"Failed to set int":                          true,
	"Failed to set float64":                      true,
	"Unknown dependency in sp":                   true,
	"Skipping dependency with no reference":      true,
	"Skipping dependency with unrecognized type": true,
	"Struct has more fields than the data slice": true,
}

// filtreBruit laisse passer le reste du journal global tel quel.
type filtreBruit struct {
	sortie io.Writer
	mu     sync.Mutex
	// « table.champ » (ou la procédure) -> nombre, depuis le dernier résumé.
	comptes map[string]int
	total   int
}

func (f *filtreBruit) Write(b []byte) (int, error) {
	var ligne struct {
		Message    string `json:"message"`
		Struct     string `json:"struct_name"`
		Champ      string `json:"field_name"`
		Index      *int   `json:"field_index"`
		Reference  string `json:"reference_name"`
		Dependance string `json:"dependency"`
	}
	if json.Unmarshal(b, &ligne) == nil && bruitMessagix[ligne.Message] {
		cle := ligne.Message
		switch {
		case ligne.Struct != "" && ligne.Champ != "":
			cle = ligne.Struct + "." + ligne.Champ
		case ligne.Struct != "":
			cle = ligne.Struct
		case ligne.Reference != "":
			cle = "procédure inconnue"
		case ligne.Dependance != "":
			cle = "dépendance inconnue"
		}
		f.mu.Lock()
		f.comptes[cle]++
		f.total++
		f.mu.Unlock()
		return len(b), nil
	}
	return f.sortie.Write(b)
}

// Le résumé depuis le dernier appel : total, et les cinq clés les plus vues.
func (f *filtreBruit) resume() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	total, comptes := f.total, f.comptes
	f.total, f.comptes = 0, map[string]int{}
	cles := slices.Collect(maps.Keys(comptes))
	slices.SortFunc(cles, func(a, b string) int {
		if comptes[a] != comptes[b] {
			return comptes[b] - comptes[a]
		}
		return strings.Compare(a, b)
	})
	var parts []string
	for i, k := range cles {
		if i == 5 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, k+"×"+itoa(comptes[k]))
	}
	return total, strings.Join(parts, ", ")
}

func itoa(n int) string { return idStr(int64(n)) }

var bruit *filtreBruit

// Branche le filtre sur le journal global (celui de messagix).
func filtrerBruitMessagix(sortie io.Writer) {
	bruit = &filtreBruit{sortie: sortie, comptes: map[string]int{}}
	zlog.Logger = zerolog.New(bruit).With().Timestamp().Logger()
}

func (p *Pont) resumeAvertissements() {
	if bruit == nil {
		return
	}
	if n, detail := bruit.resume(); n > 0 {
		log.Info().Int("tus", n).Str("principaux", detail).Msg("avertissements de décodage messagix")
	}
}
