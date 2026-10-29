package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"runtime"
	"testing"
)

func TestInstallerOutil(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("cache de l'utilisateur déplacé par XDG_CACHE_HOME : Linux seulement")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	corps := "#!/bin/sh\necho ok\n"
	tw.WriteHeader(&tar.Header{Name: "outil-x64/outil", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(corps))})
	tw.Write([]byte(corps))
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	appels := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appels++
		w.Write(archive)
	}))
	defer srv.Close()
	somme := sha256.Sum256(archive)
	a := archiveOutil{modeleWhisper{"outil-x64.tar.gz", hex.EncodeToString(somme[:]), srv.URL + "/", int64(len(archive))}, "outil-x64/outil"}

	for range 2 { // la seconde fois : déjà dans le cache, rien n'est téléchargé
		exe, err := installerOutil(context.Background(), a, "outil-1.0", func(int) {})
		if err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(exe); err != nil || string(b) != corps {
			t.Fatalf("exécutable : %q %v", b, err)
		}
	}
	if appels != 1 {
		t.Errorf("%d téléchargements", appels)
	}

	a.Somme = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := installerOutil(context.Background(), a, "outil-2.0", func(int) {}); err == nil {
		t.Error("archive acceptée malgré une somme de contrôle fausse")
	}
	a = archiveOutil{modeleWhisper{"outil-x64.tar.gz", hex.EncodeToString(somme[:]), srv.URL + "/", int64(len(archive))}, "outil-x64/absent"}
	if _, err := installerOutil(context.Background(), a, "outil-3.0", func(int) {}); err == nil {
		t.Error("exécutable absent de l'archive non signalé")
	}
}

// Chaque archive téléchargeable doit avoir sa somme de contrôle et sa taille.
func TestArchivesOutils(t *testing.T) {
	hexa := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for nom, archives := range map[string]map[string]archiveOutil{"whisper": archivesWhisper, "nemo-speech": archivesNemo} {
		for systeme, a := range archives {
			if !hexa.MatchString(a.Somme) || a.Taille <= 0 || a.Exe == "" {
				t.Errorf("%s %s : somme, taille ou exécutable manquant (%+v)", nom, systeme, a)
			}
		}
	}
}
