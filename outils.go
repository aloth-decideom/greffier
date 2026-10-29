package main

// Outils externes téléchargés par Greffier au premier usage (whisper-cli, nemo-speech) : une
// version figée, publiée en archive, vérifiée (SHA-256) puis décompressée dans le cache de
// l'utilisateur. Rien à installer ; une version installée à la main reste prioritaire.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// archiveOutil : l'archive d'un outil pour un système, et le chemin de son exécutable dans l'archive.
type archiveOutil struct {
	modeleWhisper        // nom, somme SHA-256, adresse, taille
	Exe           string // exécutable dans l'archive (séparateur « / »)
}

// pourCeSysteme : l'archive de l'outil pour le système en cours, s'il en existe une.
func pourCeSysteme(archives map[string]archiveOutil) (archiveOutil, bool) {
	a, ok := archives[runtime.GOOS+"/"+runtime.GOARCH]
	return a, ok
}

// installerOutil : l'exécutable de l'outil, décompressé dans cache/greffier/dossier (téléchargé
// s'il manque). progres suit le téléchargement.
func installerOutil(ctx context.Context, a archiveOutil, dossier string, progres func(int)) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	racine := filepath.Join(cache, "greffier")
	dest := filepath.Join(racine, dossier)
	exe := filepath.Join(dest, filepath.FromSlash(a.Exe))
	if _, err := os.Stat(exe); err == nil {
		return exe, nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	archive := filepath.Join(racine, a.Nom)
	err = telechargerModele(ctx, a.modeleWhisper, archive, progres)
	if err == nil {
		err = extraireArchive(archive, dest)
	}
	os.Remove(archive)
	if err == nil {
		if _, err = os.Stat(exe); err != nil {
			err = fmt.Errorf("%s absent de l'archive", a.Exe)
		}
	}
	return exe, err
}

// extraireArchive décompresse une archive .tar.gz ou .zip dans dest. L'archive est d'abord
// décompressée dans un dossier temporaire, puis son dossier de premier niveau est mis en place :
// une extraction interrompue ne laisse pas d'installation incomplète.
func extraireArchive(archive, dest string) error {
	tmp, err := os.MkdirTemp(dest, ".extraction-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if strings.HasSuffix(archive, ".zip") {
		err = extraireZip(archive, tmp)
	} else {
		err = extraireTarGz(archive, tmp)
	}
	if err != nil {
		return err
	}
	entrees, err := os.ReadDir(tmp)
	if err != nil {
		return err
	}
	for _, e := range entrees {
		final := filepath.Join(dest, e.Name())
		os.RemoveAll(final)
		if err := os.Rename(filepath.Join(tmp, e.Name()), final); err != nil {
			return err
		}
	}
	return nil
}

// cheminSur : la destination d'une entrée d'archive, refusée si elle sort de dest.
func cheminSur(dest, nom string) (string, error) {
	p := filepath.Join(dest, filepath.FromSlash(nom))
	if p != dest && !strings.HasPrefix(p, dest+string(filepath.Separator)) {
		return "", fmt.Errorf("chemin refusé dans l'archive : %s", nom)
	}
	return p, nil
}

func ecrireFichier(p string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func extraireTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		p, err := cheminSur(dest, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(p, 0o755)
		case tar.TypeReg:
			err = ecrireFichier(p, tr, os.FileMode(h.Mode)&0o755|0o600)
		case tar.TypeSymlink: // bibliothèques : libggml.so.0 -> libggml.so.0.25.1, dans le même dossier
			if _, err = cheminSur(dest, filepath.Join(filepath.Dir(h.Name), h.Linkname)); err == nil && !filepath.IsAbs(h.Linkname) {
				if err = os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
					err = os.Symlink(h.Linkname, p)
				}
			} else if err == nil {
				err = fmt.Errorf("lien refusé dans l'archive : %s", h.Name)
			}
		}
		if err != nil {
			return err
		}
	}
}

func extraireZip(archive, dest string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, e := range z.File {
		p, err := cheminSur(dest, e.Name)
		if err != nil {
			return err
		}
		if e.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		r, err := e.Open()
		if err != nil {
			return err
		}
		err = ecrireFichier(p, r, 0o755)
		r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
