// Package ressources : fichiers intégrés à l'exécutable (les modifier nécessite de recompiler).
//   - app/            page d'atelier, page compte rendu, moteur de notes, charte, polices, logos
//   - lanceur.html    page d'accueil (choix de l'atelier)
//   - compte-rendu/   prompt commun et modèles du compte rendu (atelier, notes libres)
//   - marp/           thème des slides
package ressources

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed app lanceur.html compte-rendu marp
var FS embed.FS

// Lire : contenu d'un fichier intégré (chemin depuis ce dossier, séparateur « / »).
func Lire(nom string) ([]byte, error) { return FS.ReadFile(nom) }

// Copier écrit le dossier intégré racine (et ses sous-dossiers) dans dest.
func Copier(racine, dest string) error {
	return fs.WalkDir(FS, racine, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		cible := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(p, racine)))
		if d.IsDir() {
			return os.MkdirAll(cible, 0o755)
		}
		data, err := FS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(cible, data, 0o644)
	})
}
