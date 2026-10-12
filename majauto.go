package main

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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const tailleMaxBinaire = 200 << 20

// executable : chemin réel de l'exécutable en cours.
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// peutSeRemplacer : le dossier de l'exécutable est-il modifiable ? (non pour Program Files,
// /usr/local/bin, ou un binaire macOS lancé depuis Téléchargements, déplacé en lecture seule)
func peutSeRemplacer() bool {
	exe, err := executable()
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

// nettoyerAncienneVersion : supprime ce qu'une mise à jour a laissé (Windows : l'ancien .exe).
func nettoyerAncienneVersion() {
	if exe, err := executable(); err == nil {
		_ = os.Remove(exe + ".old")
		_ = os.Remove(fichierTemporaire(exe))
	}
}

func fichierTemporaire(exe string) string {
	return filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".nouveau")
}

// installerMaj télécharge et vérifie la nouvelle version, puis remplace exe.
func installerMaj(ctx context.Context, info InfoVersion, exe string) error {
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

func requete(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "greffier/"+version)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if r.StatusCode != http.StatusOK {
		r.Body.Close()
		return nil, fmt.Errorf("HTTP %d pour %s", r.StatusCode, url)
	}
	return r, nil
}

// sommeAttendue lit SHA256SUMS.txt (format de sha256sum) et renvoie la somme du fichier nom.
func sommeAttendue(ctx context.Context, url, nom string) (string, error) {
	r, err := requete(ctx, url)
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
	r, err := requete(ctx, url)
	if err != nil {
		return fmt.Errorf("téléchargement : %w", err)
	}
	defer r.Body.Close()
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("écriture impossible à côté de l'application : %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, tailleMaxBinaire+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("téléchargement : %w", err)
	}
	if n > tailleMaxBinaire {
		return errors.New("téléchargement : fichier anormalement gros")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != attendu {
		return errors.New("le fichier téléchargé ne correspond pas à la somme de contrôle de la release : mise à jour annulée")
	}
	return nil
}

// relancer démarre la nouvelle version avec les mêmes options, sans rouvrir le navigateur
// (la page ouverte se recharge seule). Elle attend que le port soit libre (GREFFIER_RELANCE).
func relancer() error {
	exe, err := executable()
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
	detacher(cmd)
	return cmd.Start()
}

// ecouterApresRelance : juste après une mise à jour, l'ancienne version libère le port.
func ecouterApresRelance(ecouter func() error) error {
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
