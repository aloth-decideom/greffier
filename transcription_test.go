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
			{0, 2000, " Bonjour à tous.", ""},
			{2000, 4000, " On commence par la vision.", ""},
			{4000, 5000, " [Musique]", ""},
			{9500, 12000, " La DSI veut un entrepôt.", ""},  // repère B1-1 à 10 s, tolérance 0,5 s
			{12000, 14000, " La DSI veut un entrepôt.", ""}, // répétition de Whisper
			{14000, 15000, " Sous-titrage ST' 501", ""},     // texte parasite
			{30000, 32000, " Retour à la vision.", ""},      // retour sur A1-1, nouveau titre
		}},
		{ID: "seg-20261012-150000", Morceaux: []morceau{
			{1000, 3000, " Deuxième partie.", ""},
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
		{0, 1000, " Un.", ""}, {1200, 2000, " Deux.", ""}, {5000, 6000, " Trois, après un silence.", ""},
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
	os.WriteFile(seg+".wav", enteteWAV(0, 1), 0o644)
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
	got := morceauxAudio(30*s, reperes, "seg", true)
	want := [][2]int{{0, 5 * s}, {5 * s, 10 * s}, {10 * s, 30 * s}}
	if len(got) != len(want) {
		t.Fatalf("morceaux : %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("morceau %d : %v, attendu %v", i, got[i], want[i])
		}
	}
	if m := morceauxAudio(3*s, nil, "seg", true); len(m) != 1 || m[0] != [2]int{0, 3 * s} {
		t.Errorf("sans repère : %v", m)
	}
}

// Pendant l'enregistrement : seuls les morceaux terminés, avec des bornes qui ne bougent plus.
func TestMorceauxAudioEnCours(t *testing.T) {
	const s = frequenceAudio
	reperes := []repere{{Seg: "seg", T: 0, ID: "A"}, {Seg: "seg", T: 30, ID: "B"}}
	// 30 s sur A, puis 4 min 10 s sur B : coupé toutes les 2 min, le reste est en cours
	got := morceauxAudio(280*s, reperes, "seg", false)
	want := [][2]int{{0, 30 * s}, {30 * s, 150 * s}, {150 * s, 270 * s}}
	if len(got) != len(want) {
		t.Fatalf("morceaux : %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("morceau %d : %v, attendu %v", i, got[i], want[i])
		}
	}
	// plus tard, un repère C à 300 s puis la fin : les morceaux déjà rendus sont inchangés
	reperes = append(reperes, repere{Seg: "seg", T: 300, ID: "C"})
	fin := morceauxAudio(310*s, reperes, "seg", true)
	for i := range want {
		if fin[i] != want[i] {
			t.Errorf("morceau %d modifié : %v", i, fin[i])
		}
	}
	if fin[3] != [2]int{270 * s, 300 * s} || fin[4] != [2]int{300 * s, 310 * s} || len(fin) != 5 {
		t.Errorf("fin : %v", fin)
	}
	if m := morceauxAudio(20*s, nil, "seg", false); len(m) != 0 {
		t.Errorf("rien de terminé attendu : %v", m)
	}
}

func TestLirePlage(t *testing.T) {
	sorties := t.TempDir()
	seg := "seg-20261012-100000"
	ech := func(a, b int) []byte { // échantillons a..b-1, valeur = numéro
		var out []byte
		for i := a; i < b; i++ {
			out = append(out, byte(i), byte(i>>8))
		}
		return out
	}
	if err := ajouterAudio(sorties, seg, ech(0, 100), 1); err != nil {
		t.Fatal(err)
	}
	if err := finaliserWAV(sorties, seg); err != nil {
		t.Fatal(err)
	}
	if err := ajouterAudio(sorties, seg, ech(100, 160), 1); err != nil { // reprise après plantage : .wav + .pcm
		t.Fatal(err)
	}
	if n := echantillons(sorties, seg); n != 160 {
		t.Fatalf("%d échantillons", n)
	}
	for _, p := range [][2]int{{0, 10}, {90, 120}, {100, 160}, {130, 140}} {
		got, err := lirePlage(sorties, seg, p[0], p[1])
		if err != nil || string(got) != string(ech(p[0], p[1])) {
			t.Errorf("plage %v : %v, %v", p, got, err)
		}
	}
	if _, err := lirePlage(sorties, seg, 150, 170); err == nil {
		t.Error("plage au-delà de la fin acceptée")
	}
}

func TestTranscriptVoies(t *testing.T) {
	segs := []segmentTranscrit{{ID: "seg-20261029-100000", Morceaux: []morceau{
		{0, 2000, " Bonjour, vous m'entendez ?", "0"},
		{2000, 3000, " Oui, très bien.", "1"},
		{3000, 4000, " Parfait.", "1"},
		{4000, 5000, " On parle ensemble.", "?"},
	}}}
	md := transcriptMarkdown(segs, nil, nil)
	for _, a := range []string{
		"**Micro** = voix captées",
		"[10:00:00] **Micro** : Bonjour, vous m'entendez ?\n",
		"[10:00:02] **Visio** : Oui, très bien. Parfait.\n",
		"[10:00:04] **Micro ou visio** : On parle ensemble.\n",
	} {
		if !strings.Contains(md, a) {
			t.Errorf("absent : %q\n---\n%s", a, md)
		}
	}
	if md := transcriptMarkdown([]segmentTranscrit{{ID: "seg-20261029-100000", Morceaux: []morceau{{0, 1000, " Seul.", ""}}}}, nil, nil); strings.Contains(md, "**Micro**") {
		t.Errorf("enregistrement mono : pas de voies\n%s", md)
	}
}
