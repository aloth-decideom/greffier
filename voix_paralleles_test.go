package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Segment stéréo : micro et visio passent dans nemo-speech en même temps.
func TestVoixDeuxVoiesEnMemeTemps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("faux nemo-speech en shell")
	}
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	modele := filepath.Join(dir, "cache", "greffier", "voix", modeleDiarisation.Nom)
	os.MkdirAll(filepath.Dir(modele), 0o755)
	os.WriteFile(modele, nil, 0o644)
	journal := filepath.Join(dir, "journal")
	faux := filepath.Join(dir, "nemo-speech")
	os.WriteFile(faux, []byte(`#!/bin/sh
while [ $# -gt 0 ]; do [ "$1" = -o ] && sortie=$2; shift; done
echo debut >> `+journal+`
sleep 1
echo fin >> `+journal+`
echo '{"segments":[{"start":0,"end":2,"speaker":0}]}' > "$sortie"
`), 0o755)
	t.Setenv("GREFFIER_NEMO", faux)

	sorties := filepath.Join(dir, "sorties")
	audio := dossierAudio(sorties)
	os.MkdirAll(audio, 0o755)
	seg := "seg-20261029-140000"
	n := 10 * frequenceAudio
	os.WriteFile(filepath.Join(audio, seg+".stereo"), nil, 0o644)
	os.WriteFile(filepath.Join(audio, seg+".wav"), append(enteteWAV(uint32(4*n), 2), make([]byte, 4*n)...), 0o644)
	ancien := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(audio, seg+".wav"), ancien, ancien)

	sp := &separateur{disque: &sync.Mutex{}}
	if err := sp.executer(context.Background(), sorties, []string{seg}, 10); err != nil {
		t.Fatal(err)
	}
	lignes, _ := os.ReadFile(journal)
	if got := strings.Join(strings.Fields(string(lignes)), " "); got != "debut debut fin fin" {
		t.Fatalf("les deux voies doivent tourner en même temps : %q", got)
	}
	v, ok := lireVoix(sorties)
	if !ok || len(v.Tours) != 2 || v.Tours[0].Voix != "0" || v.Tours[1].Voix != "v0" {
		t.Fatalf("un tour micro puis un tour visio attendus : %+v", v.Tours)
	}
}
