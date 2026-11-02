package outils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aloth-decideom/greffier/internal/systeme"
)

// UserAgent des requêtes HTTP (renseigné par main avec la version).
var UserAgent = "greffier"

// Fichier : un fichier téléchargeable à une version figée (modèle ou archive d'outil).
type Fichier struct {
	Nom, Somme, Depot string // nom, SHA-256, adresse du dossier (Depot+Nom)
	Taille            int64
}

// ErrSomme : le fichier téléchargé ne correspond pas à la somme de contrôle attendue.
var ErrSomme = errors.New("le fichier téléchargé ne correspond pas à la somme de contrôle attendue")

// Requete : GET url, en erreur si la réponse n'est pas 200.
func Requete(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
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

// TelechargerVers écrit url dans dest (créé avec mode), au plus limite octets, et vérifie son
// SHA-256 (ErrSomme sinon). progres (facultatif) suit le téléchargement, en pourcentage de limite.
// En cas d'erreur, dest peut rester incomplet : à l'appelant de le supprimer.
func TelechargerVers(ctx context.Context, url, dest string, mode os.FileMode, limite int64, somme string, progres func(int)) error {
	r, err := Requete(ctx, url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	h := sha256.New()
	w := io.MultiWriter(f, h)
	if progres != nil {
		w = io.MultiWriter(w, &compteur{total: limite, progres: progres})
	}
	n, err := io.Copy(w, io.LimitReader(r.Body, limite+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return err
	case n > limite:
		return errors.New("fichier anormalement gros")
	case hex.EncodeToString(h.Sum(nil)) != somme:
		return ErrSomme
	}
	return nil
}

// Telecharger écrit le fichier m dans chemin, en vérifiant son SHA-256 (rien n'est écrit en cas d'échec).
func Telecharger(ctx context.Context, m Fichier, chemin string, progres func(int)) error {
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		return err
	}
	systeme.Logf("Téléchargement de %s (%d Mo, une seule fois)…", m.Nom, max(m.Taille>>20, 1))
	part := chemin + ".part"
	if err := TelechargerVers(ctx, m.Depot+m.Nom, part, 0o644, m.Taille, m.Somme, progres); err != nil {
		os.Remove(part)
		return fmt.Errorf("téléchargement de %s : %w", m.Nom, err)
	}
	return os.Rename(part, chemin)
}

// enCache : le fichier m dans cache/greffier/dossier, téléchargé s'il manque.
func enCache(ctx context.Context, m Fichier, dossier string, progres func(int)) (string, error) {
	chemin, err := systeme.DossierCache(dossier, m.Nom)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(chemin); err == nil {
		return chemin, nil
	}
	return chemin, Telecharger(ctx, m, chemin, progres)
}

type compteur struct {
	n, total int64
	progres  func(int)
}

func (c *compteur) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	c.progres(int(c.n * 100 / c.total))
	return len(p), nil
}
