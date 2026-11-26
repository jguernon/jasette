package main

import (
	"errors"
	"fmt"
	"strings"
)

// Les règles communes de « create_group » (même fichier dans chaque pont
// qui sait créer un groupe) : membres nettoyés, nom par défaut.

// Un groupe créé par Jasette compte au plus ce nombre de personnes (moi en plus).
const maxMembresGroupe = 50

// membresGroupe : les identifiants demandés, sans espaces ni doublons, dans
// l'ordre. Un groupe réunit au moins deux autres personnes (avec une seule,
// c'est une conversation ordinaire : commande « new »).
func membresGroupe(brut []string) ([]string, error) {
	vus := map[string]bool{}
	out := []string{}
	for _, m := range brut {
		m = strings.TrimSpace(m)
		if m == "" || vus[m] {
			continue
		}
		vus[m] = true
		out = append(out, m)
	}
	if len(out) < 2 {
		return nil, errors.New("un groupe demande au moins deux personnes")
	}
	if len(out) > maxMembresGroupe {
		return nil, fmt.Errorf("un groupe compte au plus %d personnes", maxMembresGroupe)
	}
	return out, nil
}

// nomGroupe : le nom choisi, sinon les noms des membres (« Ana, Bob, Cy »),
// coupé à max caractères (0 : sans limite) pour les services qui exigent un nom.
func nomGroupe(nom string, noms []string, max int) string {
	nom = strings.Join(strings.Fields(nom), " ")
	if nom == "" {
		parts := []string{}
		for _, n := range noms {
			if n = strings.TrimSpace(n); n != "" {
				parts = append(parts, n)
			}
		}
		nom = strings.Join(parts, ", ")
	}
	if r := []rune(nom); max > 0 && len(r) > max {
		nom = strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return nom
}
