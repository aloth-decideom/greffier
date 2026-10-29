package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Une fois l'enregistrement terminé, plusieurs morceaux sont transcrits en même temps.
func TestTranscriptionPlusieursMorceaux(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.NumCPU() < 8 {
		t.Skip("faux whisper-cli en shell, au moins 2 morceaux à la fois")
	}
	dir := t.TempDir()
	journal := filepath.Join(dir, "journal")
	faux := filepath.Join(dir, "whisper-cli")
	os.WriteFile(faux, []byte(`#!/bin/sh
[ "$1" = --help ] && exit 0
while [ $# -gt 0 ]; do [ "$1" = -of ] && sortie=$2; shift; done
echo debut >> `+journal+`
sleep 1
echo fin >> `+journal+`
echo '{"transcription":[{"offsets":{"from":0,"to":1000},"text":" Bonjour."}]}' > "$sortie.json"
`), 0o755)
	modele := filepath.Join(dir, "modele.bin")
	os.WriteFile(modele, nil, 0o644)
	t.Setenv("GREFFIER_WHISPER", faux)
	t.Setenv("GREFFIER_MODELE_WHISPER", modele)

	audio := filepath.Join(dir, "sorties", "audio")
	os.MkdirAll(audio, 0o755)
	n := 7 * 60 * frequenceAudio // 4 morceaux de 2 min au plus
	wav := filepath.Join(audio, "seg-20261029-140000.wav")
	os.WriteFile(wav, append(enteteWAV(uint32(2*n), 1), make([]byte, 2*n)...), 0o644)
	ancien := time.Now().Add(-time.Hour)
	os.Chtimes(wav, ancien, ancien)

	tr := &transcripteur{}
	if err := tr.executer(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	lignes, _ := os.ReadFile(journal)
	actifs, pic := 0, 0
	for _, l := range strings.Fields(string(lignes)) {
		if l == "debut" {
			actifs++
			pic = max(pic, actifs)
		} else {
			actifs--
		}
	}
	if strings.Count(string(lignes), "debut") != 4 || pic < 2 {
		t.Fatalf("4 morceaux attendus, plusieurs à la fois : %q (pic %d)", lignes, pic)
	}
	if _, err := os.Stat(filepath.Join(dir, "sorties", "transcription.md")); err != nil {
		t.Fatal(err)
	}
}
