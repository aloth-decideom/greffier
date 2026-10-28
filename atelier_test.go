package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
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
- Q: * Troisième, prioritaire avec une astérisque ?
- Q: *Quatrième, astérisque collée ?
- Q: **Cinquième** en gras, non prioritaire
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
	for i, want := range []struct {
		star bool
		text string
	}{{true, "Troisième, prioritaire avec une astérisque ?"}, {true, "Quatrième, astérisque collée ?"}, {false, "**Cinquième** en gras, non prioritaire"}} {
		if q := a1.Questions[2+i]; q.Star != want.star || q.Text != want.text {
			t.Errorf("marqueur de priorité mal lu : %+v (attendu %+v)", q, want)
		}
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

func TestModeLibre(t *testing.T) {
	meta, parts, _, err := ParseQuestions("---\natelier: Réunion\nmode: libre\n---\n")
	if err != nil || len(parts) != 0 || !estLibre(meta) {
		t.Fatalf("notes libres sans partie refusées : %v", err)
	}
	if _, _, _, err := ParseQuestions("---\natelier: Réunion\n---\n"); err == nil {
		t.Error("un atelier sans partie ni « mode: libre » devrait être refusé")
	}
	if !strings.Contains(buildPrompt(meta), "Sujets abordés") || strings.Contains(buildPrompt(meta), "rapport d'étonnement)") {
		t.Error("le prompt des notes libres doit utiliser le modèle libre")
	}
	if !strings.Contains(buildPrompt(map[string]string{}), "rapport d'étonnement") {
		t.Error("le prompt d'un atelier doit utiliser le modèle d'atelier")
	}
}

func TestCreerNotesLibres(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 10, 9, 14, 30, 0, 0, time.Local)
	dir, err := CreerNotesLibres(base, "Point  hebdo\n--- DSI", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "2026-10-09_14h30-point-hebdo-dsi" {
		t.Errorf("nom de dossier inattendu : %s", filepath.Base(dir))
	}
	a, _, err := LoadAtelier(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.Mode != "libre" || a.Meta["atelier"] != "Point hebdo — DSI" || !strings.HasPrefix(a.Slug, "notes-") {
		t.Errorf("atelier inattendu : mode=%q titre=%q slug=%q", a.Mode, a.Meta["atelier"], a.Slug)
	}
	// Même titre, même minute : un nouveau dossier, jamais d'écrasement.
	dir2, err := CreerNotesLibres(base, "Point hebdo DSI", "", now)
	if err != nil || dir2 == dir {
		t.Errorf("deuxième dossier : %s (%v)", dir2, err)
	}
	// Sans titre : titre par défaut daté.
	dir3, _ := CreerNotesLibres(base, "  ", "ACME", now)
	if a3, _, _ := LoadAtelier(dir3); a3 == nil || a3.Meta["atelier"] != "Notes du 09/10/2026" || a3.Meta["client"] != "ACME" {
		t.Errorf("titre par défaut inattendu : %+v", a3)
	}
	// Pas de slides Marp pour des notes libres.
	if _, err := a.Generate(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marp")); err == nil {
		t.Error("marp/ ne devrait pas être généré pour des notes libres")
	}
}

func TestFormatQuestionsAllerRetour(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("exemple", "atelier-exemple", "questions.md"))
	if err != nil {
		t.Fatal(err)
	}
	meta, parts, _, err := ParseQuestions(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	texte, err := FormatQuestions(meta, parts)
	if err != nil {
		t.Fatal(err)
	}
	meta2, parts2, warnings, err := ParseQuestions(texte)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("relecture : %v %v\n%s", err, warnings, texte)
	}
	if !reflect.DeepEqual(meta, meta2) || !reflect.DeepEqual(parts, parts2) {
		t.Errorf("aller-retour différent :\n%s", texte)
	}
}

func TestFormatQuestionsRenumerote(t *testing.T) {
	parts := []Part{
		{Title: "", Sections: []Section{
			{Code: "B3", Title: "Ma | section", Questions: []Question{
				{Text: "* pas prioritaire"}, {Text: "  "}, {Text: "Deuxième\nligne", Star: true, Relances: []string{"", "relance"}},
			}},
		}},
		{Title: "Partie deux", Sections: []Section{{Code: "A1", Title: "Autre"}, {Code: "A7", Title: "Encore"}}},
	}
	texte, err := FormatQuestions(map[string]string{"client": "X\n--- Y", "atelier": "T"}, parts)
	if err != nil {
		t.Fatal(err)
	}
	meta, got, _, err := ParseQuestions(texte)
	if err != nil {
		t.Fatalf("%v\n%s", err, texte)
	}
	if meta["client"] != "X — Y" {
		t.Errorf("client : %q", meta["client"])
	}
	if got[0].Title != "Partie A" || got[0].Sections[0].Code != "A1" || got[0].Sections[0].Title != "Ma / section" ||
		got[1].Sections[0].Code != "B1" || got[1].Sections[1].Code != "B2" {
		t.Errorf("codes ou titres incorrects :\n%s", texte)
	}
	qs := got[0].Sections[0].Questions
	if len(qs) != 2 || qs[0].Star || qs[0].Text != "pas prioritaire" || qs[1].ID != "A1-2" || !qs[1].Star ||
		qs[1].Text != "Deuxième ligne" || len(qs[1].Relances) != 1 {
		t.Errorf("questions incorrectes : %+v", qs)
	}
}

func TestSlugFixe(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "atelier-1")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "questions.md"), []byte("---\nclient: Nouveau nom\nslug: ancien-atelier-1\n---\n# Partie\n## A1. S\n- Q: q\n"), 0o644)
	a, _, err := LoadAtelier(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != "ancien-atelier-1" || a.StorageKey != "decideom-ancien-atelier-1" {
		t.Errorf("slug : %q", a.Slug)
	}
}
