package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
		fmt.Fprintf(w, `{"tag_name":"v1.2.0","html_url":"https://exemple/rel","assets":[{"name":"autre","browser_download_url":"x"},{"name":%q,"browser_download_url":"https://exemple/bin"}]}`, nom)
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
