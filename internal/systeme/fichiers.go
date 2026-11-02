package systeme

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var reEtiquette = regexp.MustCompile(`[^a-z0-9-]`)

// Etiquette : texte réduit à [a-z0-9-], pour une partie de nom de fichier.
func Etiquette(s string) string { return reEtiquette.ReplaceAllString(strings.ToLower(s), "") }

// Executable : chemin réel de l'exécutable en cours (liens symboliques résolus).
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// DossierCache : cache/greffier/<sous-dossiers> dans le cache de l'utilisateur (outils et modèles téléchargés).
func DossierCache(sous ...string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{cache, "greffier"}, sous...)...), nil
}

// TrouverOutil : la variable d'environnement env, sinon le PATH, sinon les candidats
// (lancé par double-clic, Greffier n'a pas toujours le PATH du terminal).
func TrouverOutil(env string, noms, candidats []string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	for _, n := range noms {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	for _, p := range candidats {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// EcrireAtomique écrit dans un fichier temporaire puis remplace : jamais de fichier à moitié écrit.
func EcrireAtomique(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}
