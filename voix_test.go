package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFluxVoix(t *testing.T) {
	sorties := t.TempDir()
	mono, stereo := "seg-20261029-090000", "seg-20261029-100000"
	if err := ajouterAudio(sorties, mono, bytes.Repeat([]byte{1, 0}, 2*frequenceAudio), 1); err != nil { // 2 s
		t.Fatal(err)
	}
	if err := ajouterAudio(sorties, stereo, bytes.Repeat([]byte{2, 0, 3, 0}, 3*frequenceAudio), 2); err != nil { // 3 s
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
	if len(micro) != 2 || micro[1] != (partFlux{stereo, 2 * frequenceAudio, 3 * frequenceAudio}) || len(visio) != 1 || visio[0].debut != 0 {
		t.Fatalf("plans : %+v %+v", micro, visio)
	}
	wav, _ := os.ReadFile(filepath.Join(dir, "0.wav"))
	donnees := wav[tailleEnteteWAV:]
	if len(donnees) != 5*octetsParSeconde || binary.LittleEndian.Uint32(wav[40:]) != 5*octetsParSeconde || donnees[0] != 1 || donnees[len(donnees)-2] != 2 {
		t.Errorf("flux du micro incorrect (%d octets)", len(donnees))
	}
	if wav, _ := os.ReadFile(filepath.Join(dir, "1.wav")); wav[tailleEnteteWAV] != 3 {
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
	if w, err := extraitAudio(sorties, stereo, 1, 2, 1); err != nil || len(w) != tailleEnteteWAV+octetsParSeconde || w[tailleEnteteWAV] != 3 {
		t.Errorf("extrait de la visio : %d octets, %v", len(w), err)
	}
	for _, p := range [][2]float64{{2, 1}, {0, 90}, {5, 6}} {
		if _, err := extraitAudio(sorties, stereo, p[0], p[1], 0); err == nil {
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
	if err := majVoix(dir, nil, nil, true, false); err == nil {
		t.Error("noms acceptés sans séparation")
	}
	os.MkdirAll(dossierAudio(sorties), 0o755)
	os.WriteFile(filepath.Join(dossierAudio(sorties), seg+".json"), []byte(`{"transcription":[
		{"offsets":{"from":0,"to":3000},"text":" Première phrase."},
		{"offsets":{"from":3000,"to":3500},"text":" Courte."},
		{"offsets":{"from":4000,"to":9000},"text":" Deuxième voix."}]}`), 0o644)
	v := voixAtelier{Segments: []string{seg}, Noms: map[string]string{}, Tours: []tourVoix{{seg, 0, 3.5, "1"}, {seg, 4, 9, "2"}, {seg, 20, 23, "2"}}}
	if err := ecrireVoix(sorties, v); err != nil {
		t.Fatal(err)
	}
	r := resumerVoix(sorties, v)
	if len(r) != 2 || r[0].ID != "2" || r[0].Secondes != 8 || len(r[0].Extraits) != 1 || r[0].Extraits[0].Texte != "Deuxième voix." ||
		r[1].Libelle != "Voix 1" || len(r[1].Extraits) != 1 {
		t.Fatalf("résumé : %+v", r)
	}
	if err := majVoix(dir, map[string]string{"1": " Julie ", "9": "inconnue"}, map[string]bool{"2": true, "9": true}, true, false); err != nil {
		t.Fatal(err)
	}
	v, _ = lireVoix(sorties)
	if !v.Utiliser || len(v.Noms) != 1 || v.Noms["1"] != "Julie" || len(v.Mixtes) != 1 || !v.Mixtes["2"] {
		t.Errorf("noms : %+v", v)
	}
	if v.Choisi {
		t.Error("découpage pas encore choisi à la main")
	}
	majVoix(dir, nil, nil, false, true) // « Sans découpage » coché à la main
	majVoix(dir, nil, nil, false, false)
	if v, _ = lireVoix(sorties); v.Utiliser || !v.Choisi {
		t.Errorf("choix à la main oublié : %+v", v)
	}
	v, _ = lireVoix(sorties)
	majVoix(dir, map[string]string{"1": "Julie"}, map[string]bool{"2": true}, true, false)
	v, _ = lireVoix(sorties)
	if v.nom("2") != "Voix 2 (plusieurs personnes)" || resumerVoix(sorties, v)[0].Mixte != true {
		t.Errorf("voix mixte : %q", v.nom("2"))
	}
}

func TestExtraireArchive(t *testing.T) {
	dir := t.TempDir()
	tgz := filepath.Join(dir, "a.tar.gz")
	f, _ := os.Create(tgz)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	ajout := func(h *tar.Header, corps string) {
		h.Size = int64(len(corps))
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(corps))
	}
	ajout(&tar.Header{Name: "nemo/bin/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	ajout(&tar.Header{Name: "nemo/bin/nemo-speech", Typeflag: tar.TypeReg, Mode: 0o755}, "#!/bin/sh\n")
	ajout(&tar.Header{Name: "nemo/lib/libggml.so.0.25.1", Typeflag: tar.TypeReg, Mode: 0o644}, "lib")
	ajout(&tar.Header{Name: "nemo/lib/libggml.so.0", Typeflag: tar.TypeSymlink, Linkname: "libggml.so.0.25.1"}, "")
	tw.Close()
	gz.Close()
	f.Close()
	dest := filepath.Join(dir, "cache")
	os.MkdirAll(dest, 0o755)
	if err := extraireArchive(tgz, dest); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dest, "nemo", "bin", "nemo-speech")); err != nil || st.Mode()&0o100 == 0 {
		t.Errorf("exécutable : %v %v", st, err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "nemo", "lib", "libggml.so.0")); err != nil || string(b) != "lib" {
		t.Errorf("lien : %q %v", b, err)
	}

	// entrées qui sortiraient du dossier : refusées
	for _, h := range []*tar.Header{
		{Name: "../evade", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "nemo/lien", Typeflag: tar.TypeSymlink, Linkname: "../../evade"},
		{Name: "nemo/abs", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		f, _ := os.Create(tgz)
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(h)
		tw.Close()
		gz.Close()
		f.Close()
		if err := extraireArchive(tgz, dest); err == nil {
			t.Errorf("entrée acceptée : %s -> %s", h.Name, h.Linkname)
		}
	}

	zp := filepath.Join(dir, "a.zip")
	f, _ = os.Create(zp)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("nemo-win/bin/nemo-speech.exe")
	w.Write([]byte("exe"))
	zw.Close()
	f.Close()
	if err := extraireArchive(zp, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "nemo-win", "bin", "nemo-speech.exe")); string(b) != "exe" {
		t.Error("zip non extrait")
	}
}
