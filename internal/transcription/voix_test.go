package transcription

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aloth-decideom/greffier/internal/audio"
)

func TestFluxVoix(t *testing.T) {
	sorties := t.TempDir()
	mono, stereo := "seg-20261029-090000", "seg-20261029-100000"
	if err := audio.Ajouter(sorties, mono, bytes.Repeat([]byte{1, 0}, 2*audio.Frequence), 1); err != nil { // 2 s
		t.Fatal(err)
	}
	if err := audio.Ajouter(sorties, stereo, bytes.Repeat([]byte{2, 0, 3, 0}, 3*audio.Frequence), 2); err != nil { // 3 s
		t.Fatal(err)
	}
	dir := t.TempDir()
	micro, err := ecrireFlux(sorties, []string{mono, stereo}, 0, filepath.Join(dir, "0.wav"))
	if err != nil {
		t.Fatal(err)
	}
	visio, err := ecrireFlux(sorties, []string{mono, stereo}, 1, filepath.Join(dir, "1.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(micro) != 2 || micro[1] != (partFlux{stereo, 2 * audio.Frequence, 3 * audio.Frequence}) || len(visio) != 1 || visio[0].debut != 0 {
		t.Fatalf("plans : %+v %+v", micro, visio)
	}
	wav, _ := os.ReadFile(filepath.Join(dir, "0.wav"))
	donnees := wav[audio.TailleEnteteWAV:]
	if len(donnees) != 5*audio.OctetsParSeconde || binary.LittleEndian.Uint32(wav[40:]) != 5*audio.OctetsParSeconde || donnees[0] != 1 || donnees[len(donnees)-2] != 2 {
		t.Errorf("flux du micro incorrect (%d octets)", len(donnees))
	}
	if wav, _ := os.ReadFile(filepath.Join(dir, "1.wav")); wav[audio.TailleEnteteWAV] != 3 {
		t.Error("flux de la visio : voie droite attendue")
	}

	// tour à cheval sur les deux segments : coupé en deux
	tours := toursDepuisFlux(micro, []struct {
		Start, End float64
		Speaker    int
	}{{1.5, 2.5, 1}, {3, 4, 2}}, "")
	attendu := []tourVoix{{mono, 1.5, 2, "1"}, {stereo, 0, 0.5, "1"}, {stereo, 1, 2, "2"}}
	if len(tours) != len(attendu) {
		t.Fatalf("tours : %+v", tours)
	}
	for i := range attendu {
		if tours[i] != attendu[i] {
			t.Errorf("tour %d : %+v, attendu %+v", i, tours[i], attendu[i])
		}
	}

	// écoute d'un passage : la voie demandée seulement
	if w, err := audio.Extrait(sorties, stereo, 1, 2, 1); err != nil || len(w) != audio.TailleEnteteWAV+audio.OctetsParSeconde || w[audio.TailleEnteteWAV] != 3 {
		t.Errorf("extrait de la visio : %d octets, %v", len(w), err)
	}
	for _, p := range [][2]float64{{2, 1}, {0, 90}, {5, 6}} {
		if _, err := audio.Extrait(sorties, stereo, p[0], p[1], 0); err == nil {
			t.Errorf("passage %v accepté", p)
		}
	}
}

func TestAttribuerVoix(t *testing.T) {
	seg := "seg-20261029-100000"
	v := voixAtelier{Segments: []string{seg}, Noms: map[string]string{"1": "Baptiste", "3": "Baptiste", "2": "Antoine, Anaïs"}, Mixtes: map[string]bool{"2": true}, Utiliser: true, Tours: []tourVoix{
		{seg, 0, 4, "1"}, {seg, 4, 8, "2"}, {seg, 8, 10, "3"}, {seg, 10, 12, "v1"}, {seg, 12, 13, "2"},
	}}
	segs := []segmentTranscrit{
		{ID: seg, Morceaux: []morceau{
			{0, 3000, " Bonjour.", "Micro"},
			{4500, 7500, " Merci.", ""},
			{7000, 10000, " Même personne, deux voix.", ""}, // voix 2 puis 3 (« Baptiste » aussi)
			{10000, 12000, " À distance.", ""},
			{11000, 13000, " Moitié-moitié.", ""},
			{20000, 21000, " Silence pour le modèle.", ""},
		}},
		{ID: "seg-20261029-110000", Morceaux: []morceau{{0, 1000, " Pas analysé.", "Micro"}}},
	}
	attribuerVoix(segs, v)
	var qui []string
	for _, m := range segs[0].Morceaux {
		qui = append(qui, m.Qui)
	}
	if got, want := strings.Join(qui, "|"), "Baptiste|Antoine, Anaïs (plusieurs personnes)|Baptiste|Visio 1|?|?"; got != want {
		t.Errorf("voix : %s, attendu %s", got, want)
	}
	if segs[1].Morceaux[0].Qui != "Micro" {
		t.Error("segment non analysé : étiquette gardée")
	}
	md := transcriptMarkdown(segs[:1], nil, nil, legendeVoix)
	for _, a := range []string{"Voix séparées automatiquement", "**Baptiste** : Bonjour.\n", "**Antoine, Anaïs (plusieurs personnes)** : Merci.\n", "**Visio 1** : À distance.\n"} {
		if !strings.Contains(md, a) {
			t.Errorf("absent : %q\n%s", a, md)
		}
	}
}

func TestVoixAtelier(t *testing.T) {
	dir := t.TempDir()
	sorties := filepath.Join(dir, "sorties")
	seg := "seg-20261029-100000"
	if err := MajVoix(dir, nil, nil, true, false); err == nil {
		t.Error("noms acceptés sans séparation")
	}
	os.MkdirAll(audio.Dossier(sorties), 0o755)
	os.WriteFile(filepath.Join(audio.Dossier(sorties), seg+".json"), []byte(`{"transcription":[
		{"offsets":{"from":0,"to":3000},"text":" Première phrase."},
		{"offsets":{"from":3000,"to":3500},"text":" Courte."},
		{"offsets":{"from":4000,"to":9000},"text":" Deuxième voix."}]}`), 0o644)
	v := voixAtelier{Segments: []string{seg}, Noms: map[string]string{}, Tours: []tourVoix{{seg, 0, 3.5, "1"}, {seg, 4, 9, "2"}, {seg, 20, 23, "2"}}}
	if err := ecrireVoix(sorties, v); err != nil {
		t.Fatal(err)
	}
	r := ResumerVoix(sorties, v)
	if len(r) != 2 || r[0].ID != "2" || r[0].Secondes != 8 || len(r[0].Extraits) != 1 || r[0].Extraits[0].Texte != "Deuxième voix." ||
		r[1].Libelle != "Voix 1" || len(r[1].Extraits) != 1 {
		t.Fatalf("résumé : %+v", r)
	}
	if err := MajVoix(dir, map[string]string{"1": " Julie ", "9": "inconnue"}, map[string]bool{"2": true, "9": true}, true, false); err != nil {
		t.Fatal(err)
	}
	v, _ = LireVoix(sorties)
	if !v.Utiliser || len(v.Noms) != 1 || v.Noms["1"] != "Julie" || len(v.Mixtes) != 1 || !v.Mixtes["2"] {
		t.Errorf("noms : %+v", v)
	}
	if v.Choisi {
		t.Error("découpage pas encore choisi à la main")
	}
	MajVoix(dir, nil, nil, false, true) // « Sans découpage » coché à la main
	MajVoix(dir, nil, nil, false, false)
	if v, _ = LireVoix(sorties); v.Utiliser || !v.Choisi {
		t.Errorf("choix à la main oublié : %+v", v)
	}
	v, _ = LireVoix(sorties)
	MajVoix(dir, map[string]string{"1": "Julie"}, map[string]bool{"2": true}, true, false)
	v, _ = LireVoix(sorties)
	if v.nom("2") != "Voix 2 (plusieurs personnes)" || ResumerVoix(sorties, v)[0].Mixte != true {
		t.Errorf("voix mixte : %q", v.nom("2"))
	}
}
