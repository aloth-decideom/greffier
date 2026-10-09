package main

import (
	"regexp"
	"strings"
	"testing"
)

const exemple = "\uFEFF---\r\nclient: Exemple SA\r\natelier: Test : essai\r\n---\r\n" + `
<!-- commentaire ignoré
## Z9. Pas une vraie section
-->
# Partie A — Cadrage
## A1. Vision | 15 min | cible: CODIR | étape: 2
- Q: ★ Première question ?
  - relance: Une relance
  - relance: Une autre
- Q: Deuxième question ?
## A2. Ancien séparateur · 10 min · cible: DSI
- Q: Troisième ?
# Partie B
## B1. Sans durée
- Q: Quatrième ?
texte libre ignoré
`

func TestParseQuestions(t *testing.T) {
	meta, parts, warnings, err := ParseQuestions(exemple)
	if err != nil {
		t.Fatal(err)
	}
	if meta["client"] != "Exemple SA" || meta["atelier"] != "Test : essai" {
		t.Errorf("méta incorrecte : %v", meta)
	}
	if len(parts) != 2 || len(parts[0].Sections) != 2 || len(parts[1].Sections) != 1 {
		t.Fatalf("structure incorrecte : %+v", parts)
	}
	a1 := parts[0].Sections[0]
	if a1.Code != "A1" || a1.Title != "Vision" || a1.Minutes != 15 || a1.Cible != "CODIR" || a1.Etape != "2" {
		t.Errorf("section A1 incorrecte : %+v", a1)
	}
	q := a1.Questions[0]
	if q.ID != "A1-1" || !q.Star || q.Text != "Première question ?" || len(q.Relances) != 2 {
		t.Errorf("question A1-1 incorrecte : %+v", q)
	}
	if a1.Questions[1].ID != "A1-2" || a1.Questions[1].Star {
		t.Errorf("question A1-2 incorrecte : %+v", a1.Questions[1])
	}
	if a2 := parts[0].Sections[1]; a2.Minutes != 10 || a2.Cible != "DSI" {
		t.Errorf("ancien séparateur « · » mal lu : %+v", a2)
	}
	if b1 := parts[1].Sections[0]; b1.Minutes != 0 || b1.Questions[0].ID != "B1-1" {
		t.Errorf("section sans durée mal lue : %+v", b1)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "texte libre") {
		t.Errorf("avertissements inattendus : %v", warnings)
	}
}

func TestParseErreurs(t *testing.T) {
	cas := map[string]string{
		"## A1. Sans partie\n":             "section sans partie",
		"# P\n## Vision sans code\n":       "en-tête de section invalide",
		"# P\n- Q: Question orpheline ?\n": "question hors section",
		"# P\n## A1. S\n  - relance: x\n":  "relance sans question",
		"juste du texte\n":                 "aucune partie",
	}
	for src, attendu := range cas {
		if _, _, _, err := ParseQuestions(src); err == nil || !strings.Contains(err.Error(), attendu) {
			t.Errorf("%q : erreur %v, attendu « %s »", src, err, attendu)
		}
	}
}

func TestSlug(t *testing.T) {
	if got := slugify("Exemple SA-Atelier n°1 Été"); got != "exemple-sa-atelier-n-1-ete" {
		t.Errorf("slugify = %q", got)
	}
}

// L'atelier d'exemple livré avec le dépôt doit rester lisible.
func TestExemple(t *testing.T) {
	a, warnings, err := LoadAtelier("exemple/atelier-exemple")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) > 0 {
		t.Errorf("lignes ignorées dans l'exemple : %v", warnings)
	}
	if a.StorageKey != "decideom-exemple-sa-atelier-exemple" {
		t.Errorf("clé de stockage inattendue : %s", a.StorageKey)
	}
	if _, n := a.QuestionCount(); n < 15 {
		t.Errorf("seulement %d questions", n)
	}
	// Les {{MAJUSCULES}} sont remplacées par Greffier ; les {{minuscules}} du modèle sont pour l'IA.
	if m := regexp.MustCompile(`\{\{[A-Z]+\}\}`).FindString(a.PromptCR); m != "" {
		t.Errorf("balise non remplacée dans le prompt : %s", m)
	}
}
