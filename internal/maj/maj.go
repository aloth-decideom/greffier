// Package maj : avis de nouvelle version (GitHub) et mise à jour automatique de l'exécutable.
package maj

// Avis de nouvelle version : une requête à l'API GitHub (dernière release) au démarrage, puis
// toutes les 4 h, pour une application laissée ouverte plusieurs jours.
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

	"github.com/aloth-decideom/greffier/internal/systeme"
)

var urlDerniereRelease = "https://api.github.com/repos/aloth-decideom/greffier/releases/latest"

// InfoVersion : réponse de /api/version.
type InfoVersion struct {
	Version        string `json:"version"`
	Derniere       string `json:"derniere,omitempty"`
	Nouvelle       bool   `json:"nouvelle"`
	Page           string `json:"page,omitempty"`
	Telechargement string `json:"telechargement,omitempty"`
	Sommes         string `json:"-"`    // SHA256SUMS.txt de la release
	Auto           bool   `json:"auto"` // mise à jour possible sans passer par le navigateur (installation.go)
}

// Verificateur : dernier avis de version connu, revérifié toutes les 4 h.
type Verificateur struct {
	mu       sync.Mutex
	info     InfoVersion
	pret     chan struct{} // fermé quand la vérification est terminée (réussie ou non)
	pretOnce sync.Once
	change   chan struct{} // signalé quand une vérification annonce autre chose qu'avant
	echec    bool          // mise à jour automatique ratée pour cette version (sansAuto)
}

const intervalleMaj = 4 * time.Hour

// NouveauVerificateur : avant la première vérification, l'avis ne donne que la version actuelle.
func NouveauVerificateur(actuelle string) *Verificateur {
	return &Verificateur{info: InfoVersion{Version: actuelle}}
}

// Surveiller vérifie tout de suite, puis toutes les 4 h (ne rend pas la main).
func (v *Verificateur) Surveiller(actuelle string) {
	v.Verifier(actuelle)
	for range time.Tick(intervalleMaj) {
		v.Verifier(actuelle)
	}
}

// Changements : canal signalé à chaque changement de l'avis de version (icône Greffier).
func (v *Verificateur) Changements() <-chan struct{} {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.change == nil {
		v.change = make(chan struct{}, 1)
	}
	return v.change
}

// Pret : canal fermé une fois la vérification terminée.
func (v *Verificateur) Pret() <-chan struct{} {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.pret == nil {
		v.pret = make(chan struct{})
	}
	return v.pret
}

func (v *Verificateur) Info() InfoVersion {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.info
}

// SansAuto : après un échec, on ne propose plus que le téléchargement à la main.
func (v *Verificateur) SansAuto() {
	v.mu.Lock()
	v.info.Auto = false
	v.echec = true
	v.mu.Unlock()
}

// Verifier interroge GitHub (4 s maximum) et mémorise le résultat.
func (v *Verificateur) Verifier(actuelle string) {
	v.Pret()
	defer v.pretOnce.Do(func() { close(v.pret) })
	if _, ok := parseVersion(actuelle); !ok {
		return // version de développement : pas de comparaison possible
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	info, err := derniereRelease(ctx, actuelle)
	if err != nil {
		return
	}
	info.Auto = info.Nouvelle && info.Telechargement != "" && info.Sommes != "" && peutSeRemplacer()
	v.Changements() // crée le canal
	v.mu.Lock()
	change := v.change
	if info.Derniere != v.info.Derniere {
		v.echec = false // nouvelle version : on retente l'installation automatique
	}
	info.Auto = info.Auto && !v.echec
	nouveau := info != v.info
	v.info = info
	v.mu.Unlock()
	if !nouveau {
		return
	}
	select {
	case change <- struct{}{}:
	default: // déjà signalé, pas encore lu
	}
	if info.Nouvelle {
		systeme.Logf("Nouvelle version %s disponible (tu as %s) : %s", info.Derniere, actuelle, info.Page)
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
		switch a.Nom {
		case nom:
			info.Telechargement = a.URL
		case "SHA256SUMS.txt":
			info.Sommes = a.URL
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

// nomBinaire : nom du fichier de la release pour ce système (mêmes noms que scripts/compiler.sh).
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
