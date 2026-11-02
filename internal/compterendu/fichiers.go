package compterendu

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aloth-decideom/greffier/internal/systeme"
)

// Fichier : le compte rendu de l'atelier dir (sorties/CR-<atelier>.md).
func Fichier(dir string) string {
	return filepath.Join(dir, "sorties", "CR-"+filepath.Base(dir)+".md")
}

// Copie garde une version du compte rendu dans sorties/historique/ (origine : avant-claude, navigateur…).
func Copie(dir, md, origine string) error {
	nom := fmt.Sprintf("CR-%s_%s_%s.md", filepath.Base(dir), time.Now().Format("2006-01-02_15h04m05"), systeme.Etiquette(origine))
	return systeme.EcrireAtomique(filepath.Join(dir, "sorties", "historique", nom), []byte(md+"\n"))
}

// MettreDeCote garde le compte rendu actuel (s'il existe) dans l'historique avant de le remplacer.
func MettreDeCote(dir, origine string) error {
	data, err := os.ReadFile(Fichier(dir))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return nil
	}
	return Copie(dir, strings.TrimSuffix(string(data), "\n"), origine)
}
