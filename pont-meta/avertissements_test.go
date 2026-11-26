package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// Les avertissements de décodage sont comptés, pas écrits ; le reste passe.
func TestFiltreBruit(t *testing.T) {
	var sortie bytes.Buffer
	f := &filtreBruit{sortie: &sortie, comptes: map[string]int{}}
	l := zerolog.New(f)
	for range 3 {
		l.Warn().Any("val", 0).Int("field_index", 102).Str("field_name", "IsReadReceiptsDisabled").Str("struct_name", "LSUpdateOrInsertThread").Msg("Failed to set bool")
	}
	l.Warn().Str("reference_name", "setBotResponseInfo").Msg("Skipping dependency with no reference")
	l.Warn().Str("dependency", "fetchBanners").Msg("Unknown dependency in sp")
	l.Warn().Msg("Sync failures found")
	if got := sortie.String(); !strings.Contains(got, "Sync failures found") || strings.Contains(got, "Failed to set") || strings.Contains(got, "dependency") {
		t.Errorf("sortie : %q", got)
	}
	n, detail := f.resume()
	if n != 5 || !strings.HasPrefix(detail, "LSUpdateOrInsertThread.IsReadReceiptsDisabled×3") ||
		!strings.Contains(detail, "procédure inconnue×1") || !strings.Contains(detail, "dépendance inconnue×1") {
		t.Errorf("résumé %d %q", n, detail)
	}
	if n, _ := f.resume(); n != 0 {
		t.Errorf("résumé remis à zéro : %d", n)
	}
}
