package transcription

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Etat : réponse de api/audio (transcription) et api/voix (séparation des voix), pendant et après la tâche.
type Etat struct {
	EnCours  bool   `json:"enCours"`
	Etape    string `json:"etape,omitempty"`
	Pourcent int    `json:"pourcent"`
	Erreur   string `json:"erreur,omitempty"`
}

// tache : une tâche de fond (transcription ou séparation des voix) à la fois, pour un atelier.
type tache struct {
	mu      sync.Mutex
	dossier string
	etat    Etat
	Disque  *sync.Mutex // verrou des écritures sur disque du serveur (ajouts d'audio, finalisation)
}

// Etat : où en est la tâche pour l'atelier dir (vide si elle concerne un autre atelier).
func (t *tache) Etat(dir string) Etat {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dossier != dir {
		return Etat{}
	}
	return t.etat
}

// Occupe : la tâche est en cours (pour n'importe quel atelier).
func (t *tache) Occupe() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.etat.EnCours
}

func (t *tache) maj(f func(e *Etat)) {
	t.mu.Lock()
	f(&t.etat)
	t.mu.Unlock()
}

// verrou prend le verrou des écritures sur disque et renvoie de quoi le rendre.
func (t *tache) verrou() func() {
	if t.Disque == nil {
		return func() {}
	}
	t.Disque.Lock()
	return t.Disque.Unlock
}

// demarrer marque la tâche en cours pour l'atelier dir. Si elle l'est déjà : false, et l'atelier concerné.
func (t *tache) demarrer(dir string) (enCoursPour string, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.etat.EnCours {
		return t.dossier, false
	}
	t.dossier, t.etat = dir, Etat{EnCours: true, Etape: "Préparation"}
	return "", true
}

// enFond exécute f en tâche de fond ; la fermeture d'arret (arrêt de l'application) l'interrompt.
// L'état final est termine (100 %) ou l'erreur de f ; compteRendu reçoit l'erreur et la durée.
func (t *tache) enFond(arret <-chan struct{}, termine string, f func(ctx context.Context) error, compteRendu func(err error, duree time.Duration)) {
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			select {
			case <-arret:
				cancel()
			case <-ctx.Done():
			}
		}()
		debut := time.Now()
		err := f(ctx)
		cancel()
		t.maj(func(e *Etat) {
			*e = Etat{Etape: termine, Pourcent: 100}
			if err != nil {
				*e = Etat{Erreur: err.Error()}
			}
		})
		compteRendu(err, time.Since(debut))
	}()
}

func dureeLisible(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%d s", int(s))
	}
	m := int(s / 60)
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %02d", m/60, m%60)
}
