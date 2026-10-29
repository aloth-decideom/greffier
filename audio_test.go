package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnteteWAV(t *testing.T) {
	h := enteteWAV(32000, 1)
	if len(h) != tailleEnteteWAV {
		t.Fatalf("en-tête de %d octets", len(h))
	}
	le := binary.LittleEndian
	if string(h[0:4]) != "RIFF" || string(h[8:16]) != "WAVEfmt " || string(h[36:40]) != "data" {
		t.Errorf("balises incorrectes : %q", h)
	}
	if le.Uint32(h[4:]) != 36+32000 || le.Uint32(h[24:]) != 16000 || le.Uint32(h[28:]) != 32000 || le.Uint32(h[40:]) != 32000 {
		t.Errorf("tailles ou fréquence incorrectes")
	}
}

func TestSegmentsAudio(t *testing.T) {
	sorties := t.TempDir()
	seg := "seg-20261012-140500"
	for _, mauvais := range []string{"../x", "seg-1", "seg-20261012-140500.wav", ""} {
		if ajouterAudio(sorties, mauvais, []byte{0, 0}, 1) == nil {
			t.Errorf("nom accepté : %q", mauvais)
		}
	}
	if ajouterAudio(sorties, seg, []byte{1}, 1) == nil {
		t.Error("morceau de longueur impaire accepté")
	}
	un := bytes.Repeat([]byte{1, 0}, frequenceAudio) // 1 s
	for range 2 {
		if err := ajouterAudio(sorties, seg, un, 1); err != nil {
			t.Fatal(err)
		}
	}
	l := listerSegments(sorties)
	if len(l) != 1 || l[0].Secondes != 2 || !l[0].EnCours || l[0].Transcrit {
		t.Fatalf("segments : %+v", l)
	}
	if err := finaliserWAV(sorties, seg); err != nil {
		t.Fatal(err)
	}
	// reprise du même segment après finalisation (plantage puis rattrapage) : données ajoutées
	if err := ajouterAudio(sorties, seg, un, 1); err != nil {
		t.Fatal(err)
	}
	if err := finaliserWAV(sorties, seg); err != nil {
		t.Fatal(err)
	}
	wav, err := os.ReadFile(filepath.Join(dossierAudio(sorties), seg+".wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(wav) != tailleEnteteWAV+3*octetsParSeconde || binary.LittleEndian.Uint32(wav[40:]) != 3*octetsParSeconde {
		t.Errorf("WAV de %d octets", len(wav))
	}
	if _, err := os.Stat(filepath.Join(dossierAudio(sorties), seg+".pcm")); err == nil {
		t.Error("le .pcm aurait dû disparaître")
	}

	// transcrit seulement si le .json est plus récent que l'audio
	js := filepath.Join(dossierAudio(sorties), seg+".json")
	os.WriteFile(js, []byte("{}"), 0o644)
	if l := listerSegments(sorties); !l[0].Transcrit || l[0].EnCours || l[0].Secondes != 3 {
		t.Errorf("après transcription : %+v", l)
	}
	ancien := time.Now().Add(-time.Hour)
	os.Chtimes(js, ancien, ancien)
	if l := listerSegments(sorties); l[0].Transcrit {
		t.Error("audio plus récent que la transcription : à refaire")
	}

	if err := supprimerAudio(sorties); err != nil {
		t.Fatal(err)
	}
	if l := listerSegments(sorties); len(l) != 0 {
		t.Errorf("audio encore listé : %+v", l)
	}
	if _, err := os.Stat(filepath.Join(dossierAudio(sorties), "supprimes", seg+".wav")); err != nil {
		t.Error("audio non déplacé dans supprimes/")
	}
	if _, err := os.Stat(js); err != nil {
		t.Error("la transcription du segment doit rester")
	}
}

func TestSegmentStereo(t *testing.T) {
	sorties := t.TempDir()
	seg := "seg-20261029-100000"
	un := bytes.Repeat([]byte{1, 0, 2, 0}, frequenceAudio) // 1 s, micro + visio
	if ajouterAudio(sorties, seg, un[:6], 2) == nil {
		t.Error("morceau stéréo incomplet accepté")
	}
	for range 2 {
		if err := ajouterAudio(sorties, seg, un, 2); err != nil {
			t.Fatal(err)
		}
	}
	if ajouterAudio(sorties, seg, un[:4], 1) == nil {
		t.Error("morceau mono accepté dans un segment stéréo")
	}
	if l := listerSegments(sorties); len(l) != 1 || l[0].Secondes != 2 {
		t.Fatalf("segments : %+v", l)
	}
	if n := echantillons(sorties, seg); n != 2*frequenceAudio {
		t.Errorf("%d échantillons par voie", n)
	}
	if err := finaliserWAV(sorties, seg); err != nil {
		t.Fatal(err)
	}
	wav, _ := os.ReadFile(filepath.Join(dossierAudio(sorties), seg+".wav"))
	le := binary.LittleEndian
	if le.Uint16(wav[22:]) != 2 || le.Uint32(wav[28:]) != 2*octetsParSeconde || le.Uint16(wav[32:]) != 4 || len(wav) != tailleEnteteWAV+2*len(un) {
		t.Errorf("en-tête stéréo incorrect : %v", wav[:tailleEnteteWAV])
	}
	if p, err := lirePlage(sorties, seg, 10, 12); err != nil || !bytes.Equal(p, []byte{1, 0, 2, 0, 1, 0, 2, 0}) {
		t.Errorf("plage : %v %v", p, err)
	}
	if err := supprimerAudio(sorties); err != nil {
		t.Fatal(err)
	}
	if voiesSegment(sorties, seg) != 1 {
		t.Error("marque stéréo laissée en place")
	}
}
