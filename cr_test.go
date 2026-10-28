package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Compte rendu relu dans la page : lu et écrit sur le disque, versions écartées gardées dans historique/.
func TestCompteRenduDisque(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "atelier-01")
	s := NewServeur(8770)
	appel := func(methode, corps string) map[string]any {
		rec := httptest.NewRecorder()
		s.api(rec, httptest.NewRequest(methode, "/a/x/api/cr", strings.NewReader(corps)), dir, "cr")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s : statut %d (%s)", methode, rec.Code, rec.Body)
		}
		var res map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		return res
	}

	if res := appel("GET", ""); res["markdown"] != "" || res["modifie"] != float64(0) {
		t.Errorf("sans fichier : %v", res)
	}
	appel("PUT", `{"markdown":"# CR\n\nCorrigé"}`)
	data, err := os.ReadFile(filepath.Join(dir, "sorties", "CR-atelier-01.md"))
	if err != nil || string(data) != "# CR\n\nCorrigé\n" {
		t.Fatalf("fichier écrit : %q, %v", data, err)
	}
	if res := appel("GET", ""); res["markdown"] != "# CR\n\nCorrigé" || res["modifie"] == float64(0) {
		t.Errorf("relu : %v", res)
	}

	appel("PUT", `{"markdown":"# Version du navigateur","copie":"Navigateur/../x"}`)
	if err := mettreDeCoteCR(dir, "avant-claude"); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(dir, "sorties", "historique", "CR-atelier-01_*.md"))
	if len(copies) != 2 || !strings.HasSuffix(copies[0], "_avant-claude.md") && !strings.HasSuffix(copies[1], "_avant-claude.md") {
		t.Fatalf("copies : %v", copies)
	}
	for _, c := range copies {
		if strings.HasSuffix(c, "_navigateurx.md") {
			if b, _ := os.ReadFile(c); string(b) != "# Version du navigateur\n" {
				t.Errorf("copie du navigateur : %q", b)
			}
		} else if !strings.HasSuffix(c, "_avant-claude.md") {
			t.Errorf("nom de copie inattendu : %s", c)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "sorties", "CR-atelier-01.md")); string(b) != "# CR\n\nCorrigé\n" {
		t.Errorf("une copie ne doit pas toucher au compte rendu : %q", b)
	}
}

// Questions modifiées dans la page : questions.md réécrit, ancien gardé, adresse conservée si le client change.
func TestQuestionsModifiees(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "atelier-02")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "questions.md"), []byte("---\nclient: Ancien\nautre: garde\n---\n# P\n## A1. S\n- Q: q\n"), 0o644)
	avant, _, _ := LoadAtelier(dir)
	s := NewServeur(8770)
	appel := func(corps string) int {
		rec := httptest.NewRecorder()
		s.api(rec, httptest.NewRequest("PUT", "/a/x/api/questions", strings.NewReader(corps)), dir, "questions")
		return rec.Code
	}
	if c := appel(`{"meta":{"client":"Nouveau","slug":"pirate"},"parts":[{"title":"P","sections":[{"title":"S","minutes":5,"questions":[{"text":"q2","star":true,"relances":["r"]}]}]}]}`); c != http.StatusOK {
		t.Fatalf("statut %d", c)
	}
	apres, _, err := LoadAtelier(dir)
	if err != nil {
		t.Fatal(err)
	}
	q := apres.Parts[0].Sections[0].Questions[0]
	if apres.Slug != avant.Slug || apres.Meta["client"] != "Nouveau" || apres.Meta["autre"] != "garde" || q.Text != "q2" || !q.Star {
		t.Errorf("après modification : %+v", apres)
	}
	if copies, _ := filepath.Glob(filepath.Join(dir, "sorties", "historique", "questions_*.md")); len(copies) != 1 {
		t.Errorf("ancien questions.md non gardé : %v", copies)
	}
	if c := appel(`{"parts":`); c != http.StatusBadRequest {
		t.Errorf("corps illisible : statut %d", c)
	}
}
