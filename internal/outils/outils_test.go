package outils

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	a := archiveOutil{Fichier{"outil-x64.tar.gz", hex.EncodeToString(somme[:]), srv.URL + "/", int64(len(archive))}, "outil-x64/outil"}

	for range 2 { // la seconde fois : déjà dans le cache, rien n'est téléchargé
		exe, err := installer(context.Background(), a, "outil-1.0", func(int) {})
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
	if _, err := installer(context.Background(), a, "outil-2.0", func(int) {}); err == nil {
		t.Error("archive acceptée malgré une somme de contrôle fausse")
	}
	a = archiveOutil{Fichier{"outil-x64.tar.gz", hex.EncodeToString(somme[:]), srv.URL + "/", int64(len(archive))}, "outil-x64/absent"}
	if _, err := installer(context.Background(), a, "outil-3.0", func(int) {}); err == nil {
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
