package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptMarkdown(t *testing.T) {
	segs := []segmentTranscrit{
		{ID: "seg-20261012-140000", Morceaux: []morceau{
			{0, 2000, " Bonjour à tous."},
			{2000, 4000, " On commence par la vision."},
			{4000, 5000, " [Musique]"},
			{9500, 12000, " La DSI veut un entrepôt."},  // repère B1-1 à 10 s, tolérance 0,5 s
			{12000, 14000, " La DSI veut un entrepôt."}, // répétition de Whisper
			{14000, 15000, " Sous-titrage ST' 501"},     // texte parasite
			{30000, 32000, " Retour à la vision."},      // retour sur A1-1, nouveau titre
		}},
		{ID: "seg-20261012-150000", Morceaux: []morceau{
			{1000, 3000, " Deuxième partie."},
		}},
	}
	reperes := []repere{
		{Seg: "seg-20261012-140000", T: 0, ID: "A1-1"},
		{Seg: "seg-20261012-140000", T: 10, ID: "B1-1"},
		{Seg: "seg-20261012-140000", T: 29, ID: "A1-1"},
		{Seg: "seg-20261012-150000", T: 0, ID: "P2"},
		{Seg: "autre", T: 0, ID: "Z9-9"},
	}
	titres := map[string]string{"A1-1": "Quelle vision ?", "B1-1": "Quel existant ?", "P2": "Budget"}
	md := transcriptMarkdown(segs, reperes, titres)

	attendus := []string{
		"**Enregistrement 1 — 12/10/2026 à 14:00**",
		"## A1-1 · Quelle vision ?\n\n[14:00:00] Bonjour à tous. On commence par la vision.\n",
		"## B1-1 · Quel existant ?\n\n[14:00:09] La DSI veut un entrepôt.\n",
		"## A1-1 · Quelle vision ?\n\n[14:00:30] Retour à la vision.\n",
		"**Enregistrement 2 — 12/10/2026 à 15:00**",
		"## P2 · Budget\n\n[15:00:01] Deuxième partie.\n",
	}
	pos := 0
	for _, a := range attendus {
		i := strings.Index(md[pos:], a)
		if i < 0 {
			t.Fatalf("absent ou dans le désordre : %q\n---\n%s", a, md)
		}
		pos += i + len(a)
	}
	for _, interdit := range []string{"Musique", "Sous-titrage", "entrepôt. La DSI", "Z9-9"} {
		if strings.Contains(md, interdit) {
			t.Errorf("ne devrait pas contenir %q :\n%s", interdit, md)
		}
	}
}

func TestTranscriptSansRepere(t *testing.T) {
	segs := []segmentTranscrit{{ID: "nom-libre", Morceaux: []morceau{
		{0, 1000, " Un."}, {1200, 2000, " Deux."}, {5000, 6000, " Trois, après un silence."},
	}}}
	md := transcriptMarkdown(segs, nil, nil)
	if strings.Contains(md, "## ") {
		t.Errorf("pas de titre attendu sans repère :\n%s", md)
	}
	if !strings.Contains(md, "[00:00:00] Un. Deux.\n") || !strings.Contains(md, "[00:00:05] Trois, après un silence.") {
		t.Errorf("paragraphes ou horodatage relatif incorrects :\n%s", md)
	}
}

// Notes libres : titres des pages lus dans session.json au moment de l'assemblage
// (une page renommée après l'enregistrement prend son nouveau titre, une page sans titre « Page n »).
func TestAssemblerTranscriptionNotesLibres(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "questions.md"), []byte("---\natelier: Réunion\nmode: libre\n---\n"), 0o644)
	sorties := filepath.Join(dir, "sorties")
	os.MkdirAll(dossierAudio(sorties), 0o755)
	os.WriteFile(filepath.Join(sorties, "session.json"), []byte(`{"pages":[{"id":"P1","title":""},{"id":"P2","title":"Budget 2027"}],
		"audio":{"reperes":[{"seg":"seg-20261012-090000","t":0,"id":"P1"},{"seg":"seg-20261012-090000","t":60,"id":"P2"}]}}`), 0o644)
	seg := filepath.Join(dossierAudio(sorties), "seg-20261012-090000")
	os.WriteFile(seg+".wav", enteteWAV(0), 0o644)
	os.WriteFile(seg+".json", []byte(`{"transcription":[
		{"offsets":{"from":0,"to":2000},"text":" Ordre du jour."},
		{"offsets":{"from":61000,"to":63000},"text":" Le budget augmente."}]}`), 0o644)

	chemin, err := assemblerTranscription(dir)
	if err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(chemin)
	for _, a := range []string{"## P1 · Page 1\n\n[09:00:00] Ordre du jour.", "## P2 · Budget 2027\n\n[09:01:01] Le budget augmente."} {
		if !strings.Contains(string(md), a) {
			t.Errorf("absent : %q\n---\n%s", a, md)
		}
	}
}

func TestMorceauxAudio(t *testing.T) {
	const s = frequenceAudio
	reperes := []repere{
		{Seg: "seg", T: 0, ID: "A"},    // début : pas de coupe
		{Seg: "seg", T: 10, ID: "B"},   // coupe
		{Seg: "seg", T: 10.2, ID: "C"}, // trop près de la précédente
		{Seg: "autre", T: 20, ID: "Z"}, // autre segment
		{Seg: "seg", T: 5, ID: "A2"},   // dans le désordre
		{Seg: "seg", T: 29.8, ID: "D"}, // trop près de la fin
	}
	got := morceauxAudio(30*s, reperes, "seg")
	want := [][2]int{{0, 5 * s}, {5 * s, 10 * s}, {10 * s, 30 * s}}
	if len(got) != len(want) {
		t.Fatalf("morceaux : %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("morceau %d : %v, attendu %v", i, got[i], want[i])
		}
	}
	if m := morceauxAudio(3*s, nil, "seg"); len(m) != 1 || m[0] != [2]int{0, 3 * s} {
		t.Errorf("sans repère : %v", m)
	}
}
