// Package compterendu : le compte rendu d'un atelier, rédigé par Claude Code puis relu et corrigé
// dans la page cr.html (sorties/CR-<atelier>.md, versions précédentes dans sorties/historique/).
package compterendu

// Compte rendu rédigé par Claude Code en mode non interactif (claude -p), avec l'abonnement
// de la personne connectée dans Claude Code : pas de clé API. Claude ne reçoit que l'outil
// Read, et travaille dans sorties/ pour pouvoir regarder les photos (règle 9 du prompt).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aloth-decideom/greffier/internal/systeme"
)

const dureeMaxCR = 10 * time.Minute

// consigneClaude : ajoutée après les notes, propre à l'exécution par Claude Code.
const consigneClaude = "\n\n---\n\nLe dossier courant contient le dossier `photos/` des notes : " +
	"ouvre chaque photo citée avec l'outil Read pour la décrire (règle 9). " +
	"Ta réponse finale est uniquement le Markdown du compte rendu."

// CheminClaude : GREFFIER_CLAUDE, sinon le PATH, sinon les emplacements d'installation habituels.
func CheminClaude() string {
	home, _ := os.UserHomeDir()
	candidats := []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	}
	if runtime.GOOS == "windows" {
		candidats = []string{
			filepath.Join(home, ".local", "bin", "claude.exe"),
			filepath.Join(os.Getenv("APPDATA"), "npm", "claude.cmd"),
		}
	}
	return systeme.TrouverOutil("GREFFIER_CLAUDE", []string{"claude"}, candidats)
}

// Rediger envoie prompt + notes à Claude Code et renvoie le Markdown du compte rendu.
func Rediger(ctx context.Context, dossier, texte string) (string, error) {
	claude := CheminClaude()
	if claude == "" {
		return "", errors.New("Claude Code introuvable : l'installer et s'y connecter (ou indiquer son chemin dans GREFFIER_CLAUDE)")
	}
	ctx, cancel := context.WithTimeout(ctx, dureeMaxCR)
	defer cancel()
	cmd := exec.CommandContext(ctx, claude, "-p", "--tools", "Read", "--no-session-persistence", "--output-format", "text")
	cmd.Dir = dossier
	cmd.Stdin = strings.NewReader(texte + consigneClaude)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	md := strings.TrimSpace(out.String())
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return "", fmt.Errorf("Claude n'a pas répondu en %v", dureeMaxCR)
	case ctx.Err() != nil:
		return "", errors.New("rédaction annulée")
	case err != nil:
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = md
		}
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		return "", fmt.Errorf("Claude Code a échoué (%v) : %s", err, msg)
	case md == "":
		return "", errors.New("Claude Code n'a rien renvoyé")
	}
	return md, nil
}
