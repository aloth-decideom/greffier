package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPlusRecente(t *testing.T) {
	cas := []struct {
		a, b    string
		attendu bool
	}{
		{"v1.0.1", "v1.0.0", true},
		{"v1.1.0", "v1.0.9", true},
		{"v2.0.0", "v1.10.0", true},
		{"v1.10.0", "v1.9.0", true},
		{"1.2.0", "v1.1.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v1.1.0", "dev", false},
		{"v1.1.0", "dev-3d56307", false},
		{"v1.1.0-rc1", "v1.0.0", false},
	}
	for _, c := range cas {
		if got := plusRecente(c.a, c.b); got != c.attendu {
			t.Errorf("plusRecente(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestDerniereRelease(t *testing.T) {
	nom := nomBinaire(runtime.GOOS, runtime.GOARCH)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "greffier/") {
			t.Error("User-Agent manquant (exigé par l'API GitHub)")
		}
		fmt.Fprintf(w, `{"tag_name":"v1.2.0","html_url":"https://exemple/rel","assets":[{"name":"autre","browser_download_url":"x"},{"name":%q,"browser_download_url":"https://exemple/bin"},{"name":"SHA256SUMS.txt","browser_download_url":"https://exemple/sommes"}]}`, nom)
	}))
	defer srv.Close()
	ancienne := urlDerniereRelease
	urlDerniereRelease = srv.URL
	defer func() { urlDerniereRelease = ancienne }()

	info, err := derniereRelease(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Nouvelle || info.Derniere != "v1.2.0" || info.Page != "https://exemple/rel" {
		t.Errorf("info inattendue : %+v", info)
	}
	if nom != "" && info.Telechargement != "https://exemple/bin" {
		t.Errorf("lien de téléchargement inattendu : %q", info.Telechargement)
	}
	if info.Sommes != "https://exemple/sommes" {
		t.Errorf("lien des sommes de contrôle inattendu : %q", info.Sommes)
	}
	if info, _ := derniereRelease(context.Background(), "v1.2.0"); info.Nouvelle {
		t.Error("même version : pas de nouvelle version")
	}

	// Pas de release (404) ou hors ligne : erreur silencieuse, rien à signaler.
	srv404 := httptest.NewServer(http.NotFoundHandler())
	defer srv404.Close()
	urlDerniereRelease = srv404.URL
	var v verificateurMaj
	v.info = InfoVersion{Version: "v1.0.0"}
	v.Verifier("v1.0.0")
	if v.Info().Nouvelle {
		t.Error("404 : aucune nouvelle version ne doit être signalée")
	}
}

// Les noms de nomBinaire doivent être ceux produits par compiler.sh (et donc publiés dans la release).
func TestNomBinaireCompiler(t *testing.T) {
	script, err := os.ReadFile("compiler.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"windows/amd64", "windows/arm64", "darwin/arm64", "darwin/amd64", "linux/amd64"} {
		goos, goarch, _ := strings.Cut(c, "/")
		if n := nomBinaire(goos, goarch); n == "" || !strings.Contains(string(script), c+"/"+n) {
			t.Errorf("%s : %q absent de compiler.sh", c, n)
		}
	}
}

func TestInstallerMaj(t *testing.T) {
	nom := nomBinaire(runtime.GOOS, runtime.GOARCH)
	if nom == "" {
		t.Skip("pas de binaire publié pour ce système")
	}
	nouveau := []byte("nouvelle version")
	somme := sha256.Sum256(nouveau)
	sommes := fmt.Sprintf("%x  autre-fichier\n%x  %s\n", sha256.Sum256([]byte("x")), somme, nom)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bin":
			_, _ = w.Write(nouveau)
		case "/sommes":
			fmt.Fprint(w, sommes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	info := InfoVersion{Telechargement: srv.URL + "/bin", Sommes: srv.URL + "/sommes"}

	installer := func(t *testing.T, info InfoVersion) (string, error) {
		exe := filepath.Join(t.TempDir(), "greffier")
		if err := os.WriteFile(exe, []byte("ancienne version"), 0o755); err != nil {
			t.Fatal(err)
		}
		return exe, installerMaj(context.Background(), info, exe)
	}

	t.Run("somme correcte", func(t *testing.T) {
		exe, err := installer(t, info)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(exe); string(got) != string(nouveau) {
			t.Errorf("exécutable non remplacé : %q", got)
		}
		if st, _ := os.Stat(exe); runtime.GOOS != "windows" && st.Mode()&0o100 == 0 {
			t.Error("le nouvel exécutable doit être exécutable")
		}
		if _, err := os.Stat(fichierTemporaire(exe)); err == nil {
			t.Error("fichier temporaire laissé sur place")
		}
	})

	t.Run("somme fausse : rien ne change", func(t *testing.T) {
		sommes = fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("autre chose")), nom)
		defer func() { sommes = fmt.Sprintf("%x  %s\n", somme, nom) }()
		exe, err := installer(t, info)
		if err == nil || !strings.Contains(err.Error(), "somme de contrôle") {
			t.Fatalf("erreur attendue, obtenu %v", err)
		}
		if got, _ := os.ReadFile(exe); string(got) != "ancienne version" {
			t.Errorf("l'ancienne version doit rester : %q", got)
		}
		if _, err := os.Stat(fichierTemporaire(exe)); err == nil {
			t.Error("fichier temporaire laissé sur place")
		}
	})

	t.Run("binaire absent des sommes", func(t *testing.T) {
		sommes = "abc  autre\n"
		defer func() { sommes = fmt.Sprintf("%x  %s\n", somme, nom) }()
		if _, err := installer(t, info); err == nil {
			t.Fatal("erreur attendue")
		}
	})

	t.Run("release sans binaire", func(t *testing.T) {
		if _, err := installer(t, InfoVersion{}); err == nil {
			t.Fatal("erreur attendue")
		}
	})
}

// Revérification : une release publiée après le démarrage est signalée ; un échec d'installation
// automatique reste retenu pour cette version, pas pour la suivante.
func TestVerifierRevérification(t *testing.T) {
	tag := "v1.0.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://exemple/rel"}`, tag)
	}))
	defer srv.Close()
	ancienne := urlDerniereRelease
	urlDerniereRelease = srv.URL
	defer func() { urlDerniereRelease = ancienne }()

	var v verificateurMaj
	change := v.Changements()
	v.Verifier("v1.0.0")
	<-change // premier avis (aucune nouvelle version)
	if v.Info().Nouvelle {
		t.Fatal("même version : pas de nouvelle version")
	}
	tag = "v1.1.0"
	v.Verifier("v1.0.0")
	select {
	case <-change:
	default:
		t.Fatal("nouvelle version publiée : changement non signalé")
	}
	if i := v.Info(); !i.Nouvelle || i.Derniere != "v1.1.0" {
		t.Fatalf("info inattendue : %+v", i)
	}
	v.Verifier("v1.0.0")
	select {
	case <-change:
		t.Error("rien de nouveau : aucun changement à signaler")
	default:
	}
	v.sansAuto()
	v.Verifier("v1.0.0")
	if !v.echec {
		t.Error("même version : l'échec de l'installation automatique doit rester retenu")
	}
	tag = "v1.2.0"
	v.Verifier("v1.0.0")
	if v.echec {
		t.Error("version suivante : l'installation automatique doit être retentée")
	}
}
