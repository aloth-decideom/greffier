package main

// NeMo-Speech.cpp (séparation des voix, voix.go) sans installation : au premier usage, Greffier
// télécharge la version officielle pour le système (GitHub de NVIDIA, 5 à 7 Mo) et le modèle
// Nemotron 3 Diarization (Hugging Face, 107 Mo), sommes de contrôle vérifiées, dans le cache de
// l'utilisateur. Une version installée à la main (GREFFIER_NEMO, PATH, à côté de Greffier) reste prioritaire.

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

const versionNemo = "v0.2.0"

// Archives publiées par NVIDIA (CPU ; Metal sur Mac Apple Silicon). Windows ARM exécute la version x64.
var archivesNemo = map[string]modeleWhisper{
	"linux/amd64":   {"nemo-speech-0.2.0-linux-x86_64-cpu.tar.gz", "f396057150f1b774935c7414fd32ebc04195a795a34120d78d8c9ecefa1b7507", depotNemo, 6683477},
	"linux/arm64":   {"nemo-speech-0.2.0-linux-aarch64-cpu.tar.gz", "ac50694d4729f9eae390a9851d38f51269d9af9829b535325539090a3b95c787", depotNemo, 6309769},
	"darwin/arm64":  {"nemo-speech-0.2.0-macos-aarch64-metal.tar.gz", "5cb02ba7c04f0b5585ce5cde9c830be5f0b7dfb4c83083c500109c381b4f2da9", depotNemo, 5031510},
	"darwin/amd64":  {"nemo-speech-0.2.0-macos-x86_64-cpu.tar.gz", "d52c318d8197511d4f7eee02bcf05511f7e91b300555a130b562e571695a0f39", depotNemo, 5256846},
	"windows/amd64": {"nemo-speech-0.2.0-windows-x86_64-cpu.zip", "82c80451086e86194aba9af19f800da77cd66d1c28d993d0efceffd6cdb49bb8", depotNemo, 5528418},
	"windows/arm64": {"nemo-speech-0.2.0-windows-x86_64-cpu.zip", "82c80451086e86194aba9af19f800da77cd66d1c28d993d0efceffd6cdb49bb8", depotNemo, 5528418},
}

const depotNemo = "https://github.com/NVIDIA/NeMo-Speech.cpp/releases/download/" + versionNemo + "/"

// Modèle de séparation des voix (révision figée du dépôt Hugging Face de NVIDIA).
var modeleDiarisation = modeleWhisper{"Nemotron-3-Diarization.q8_0.gguf", "08456d9e22cd9a323c0364d98375f3746d6e68507ebb705cd46438c534c7a3a1",
	"https://huggingface.co/nvidia/Nemotron-3-Diarization/resolve/f667ed73aee57d40cc39428eb768b4fd87a0a29e/", 107012128}

// nemoDisponible : nemo-speech installé, ou téléchargeable pour ce système.
func nemoDisponible() bool {
	_, ok := archivesNemo[runtime.GOOS+"/"+runtime.GOARCH]
	return ok || cheminNemo() != ""
}

// nemoTelecharge : l'exécutable de la version téléchargée par Greffier (dans le cache), qu'il existe ou non.
func nemoTelecharge() (string, error) {
	a, ok := archivesNemo[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("NeMo-Speech.cpp n'existe pas pour ce système (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	nom := strings.TrimSuffix(strings.TrimSuffix(a.Nom, ".zip"), ".tar.gz")
	exe := "nemo-speech"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	return filepath.Join(cache, "greffier", nom, "bin", exe), nil
}

// assurerNemo : nemo-speech et le modèle de séparation des voix, téléchargés s'ils manquent.
// etape(libellé, pourcent) suit les téléchargements.
func assurerNemo(ctx context.Context, etape func(string, int)) (nemo, modele string, err error) {
	if nemo = cheminNemo(); nemo == "" {
		if nemo, err = nemoTelecharge(); err != nil {
			return "", "", err
		}
		if _, err := os.Stat(nemo); err != nil {
			a := archivesNemo[runtime.GOOS+"/"+runtime.GOARCH]
			racine := filepath.Dir(filepath.Dir(filepath.Dir(nemo))) // cache/greffier
			archive := filepath.Join(racine, a.Nom)
			err := telechargerModele(ctx, a, archive, func(p int) { etape("Téléchargement de NeMo-Speech (une seule fois)", p) })
			if err == nil {
				err = extraireArchive(archive, racine)
			}
			os.Remove(archive)
			if err != nil {
				return "", "", fmt.Errorf("installation de NeMo-Speech : %w", err)
			}
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	modele = filepath.Join(cache, "greffier", "voix", modeleDiarisation.Nom)
	if _, err := os.Stat(modele); err != nil {
		err := telechargerModele(ctx, modeleDiarisation, modele, func(p int) {
			etape("Téléchargement du modèle de séparation des voix (107 Mo, une seule fois)", p)
		})
		if err != nil {
			return "", "", err
		}
	}
	return nemo, modele, nil
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
