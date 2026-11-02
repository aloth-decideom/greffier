package maj

// Mise à jour depuis l'icône ou la page d'accueil : téléchargement du binaire de la dernière
// release à côté de l'exécutable, vérification de son SHA-256 (SHA256SUMS.txt de la release),
// remplacement de l'exécutable, puis relance une fois l'arrêt propre terminé.
//   - Linux, macOS : le nouveau fichier est renommé par-dessus l'ancien (permis pendant qu'il
//     tourne ; sous macOS il ne faut surtout pas réécrire le fichier en place).
//   - Windows : un .exe lancé ne peut être ni écrasé ni supprimé, mais il peut être renommé :
//     greffier.exe → greffier.exe.old (supprimé au démarrage suivant), puis le nouveau à sa place.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/aloth-decideom/greffier/internal/outils"
	"github.com/aloth-decideom/greffier/internal/systeme"
)

const tailleMaxBinaire = 200 << 20

// peutSeRemplacer : le dossier de l'exécutable est-il modifiable ? (non pour Program Files,
// /usr/local/bin, ou un binaire macOS lancé depuis Téléchargements, déplacé en lecture seule)
func peutSeRemplacer() bool {
	exe, err := systeme.Executable()
	if err != nil {
		return false
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".greffier-essai-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// NettoyerAncienneVersion : supprime ce qu'une mise à jour a laissé (Windows : l'ancien .exe).
func NettoyerAncienneVersion() {
	if exe, err := systeme.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
		_ = os.Remove(fichierTemporaire(exe))
	}
}

func fichierTemporaire(exe string) string {
	return filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".nouveau")
}

// Installer télécharge et vérifie la nouvelle version, puis remplace exe.
func Installer(ctx context.Context, info InfoVersion, exe string) error {
	if info.Telechargement == "" || info.Sommes == "" {
		return errors.New("la release ne contient pas de binaire pour ce système")
	}
	attendu, err := sommeAttendue(ctx, info.Sommes, nomBinaire(runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return fmt.Errorf("sommes de contrôle : %w", err)
	}
	tmp := fichierTemporaire(exe)
	if err := telecharger(ctx, info.Telechargement, tmp, attendu); err != nil {
		os.Remove(tmp)
		return err
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("remplacement impossible : %w", err)
		}
		if err := os.Rename(tmp, exe); err != nil {
			_ = os.Rename(old, exe)
			os.Remove(tmp)
			return fmt.Errorf("remplacement impossible : %w", err)
		}
		return nil
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("remplacement impossible : %w", err)
	}
	return nil
}

// sommeAttendue lit SHA256SUMS.txt (format de sha256sum) et renvoie la somme du fichier nom.
func sommeAttendue(ctx context.Context, url, nom string) (string, error) {
	r, err := outils.Requete(ctx, url)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(r.Body, 1<<16))
	for sc.Scan() {
		somme, fichier, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if ok && strings.TrimLeft(strings.TrimSpace(fichier), "*") == nom && len(somme) == 64 {
			return strings.ToLower(somme), nil
		}
	}
	return "", fmt.Errorf("%s absent", nom)
}

// telecharger écrit url dans dest et vérifie son SHA-256.
func telecharger(ctx context.Context, url, dest, attendu string) error {
	err := outils.TelechargerVers(ctx, url, dest, 0o755, tailleMaxBinaire, attendu, nil)
	switch {
	case errors.Is(err, outils.ErrSomme):
		return errors.New("le fichier téléchargé ne correspond pas à la somme de contrôle de la release : mise à jour annulée")
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("écriture impossible à côté de l'application : %w", err)
	case err != nil:
		return fmt.Errorf("téléchargement : %w", err)
	}
	return nil
}

// Relancer démarre la nouvelle version avec les mêmes options, sans rouvrir le navigateur
// (la page ouverte se recharge seule). Elle attend que le port soit libre (GREFFIER_RELANCE).
func Relancer() error {
	exe, err := systeme.Executable()
	if err != nil {
		return err
	}
	args := os.Args[1:]
	if !slices.Contains(args, "--no-open") && !slices.Contains(args, "-no-open") {
		args = append([]string{"--no-open"}, args...)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "GREFFIER_RELANCE=1")
	systeme.Detacher(cmd)
	return cmd.Start()
}

// EcouterApresRelance : juste après une mise à jour, l'ancienne version libère le port.
func EcouterApresRelance(ecouter func() error) error {
	err := ecouter()
	if os.Getenv("GREFFIER_RELANCE") == "" {
		return err
	}
	os.Unsetenv("GREFFIER_RELANCE")
	for i := 0; err != nil && i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		err = ecouter()
	}
	return err
}
