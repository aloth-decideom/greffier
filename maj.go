package main

// Avis de nouvelle version : au démarrage, une requête à l'API GitHub (dernière release).
// Hors ligne, en erreur ou pour une version de développement : rien n'est affiché, rien ne bloque.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"
)

var urlDerniereRelease = "https://api.github.com/repos/aloth-decideom/greffier/releases/latest"

// InfoVersion : réponse de /api/version.
type InfoVersion struct {
	Version        string `json:"version"`
	Derniere       string `json:"derniere,omitempty"`
	Nouvelle       bool   `json:"nouvelle"`
	Page           string `json:"page,omitempty"`
	Telechargement string `json:"telechargement,omitempty"`
}

type verificateurMaj struct {
	mu   sync.Mutex
	info InfoVersion
}

func (v *verificateurMaj) Info() InfoVersion {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.info
}

// Verifier interroge GitHub (4 s maximum) et mémorise le résultat.
func (v *verificateurMaj) Verifier(actuelle string) {
	if _, ok := parseVersion(actuelle); !ok {
		return // version de développement : pas de comparaison possible
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	info, err := derniereRelease(ctx, actuelle)
	if err != nil {
		return
	}
	v.mu.Lock()
	v.info = info
	v.mu.Unlock()
	if info.Nouvelle {
		logf("Nouvelle version %s disponible (tu as %s) : %s", info.Derniere, actuelle, info.Page)
	}
}

func derniereRelease(ctx context.Context, actuelle string) (InfoVersion, error) {
	info := InfoVersion{Version: actuelle}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlDerniereRelease, nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "greffier/"+actuelle)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return info, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return info, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Page   string `json:"html_url"`
		Assets []struct {
			Nom string `json:"name"`
			URL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
		return info, err
	}
	info.Derniere, info.Page = rel.Tag, rel.Page
	info.Nouvelle = plusRecente(rel.Tag, actuelle)
	nom := nomBinaire(runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		if a.Nom == nom {
			info.Telechargement = a.URL
		}
	}
	return info, nil
}

var reVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	m := reVersion.FindStringSubmatch(s)
	if m == nil {
		return v, false
	}
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

// plusRecente : a > b, pour des versions vX.Y.Z (sinon false).
func plusRecente(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// nomBinaire : nom du fichier de la release pour ce système (mêmes noms que compiler.sh).
func nomBinaire(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return "greffier-windows-x64.exe"
	case "windows/arm64":
		return "greffier-windows-arm64.exe"
	case "darwin/arm64":
		return "greffier-macos-apple-silicon"
	case "darwin/amd64":
		return "greffier-macos-intel"
	case "linux/amd64":
		return "greffier-linux-x64"
	}
	return ""
}
