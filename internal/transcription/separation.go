package transcription

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aloth-decideom/greffier/internal/audio"
	"github.com/aloth-decideom/greffier/internal/outils"
	"github.com/aloth-decideom/greffier/internal/systeme"
)

// partFlux : le segment seg occupe n échantillons à partir de debut dans le flux analysé.
type partFlux struct {
	seg      string
	debut, n int
}

// ecrireFlux met bout à bout, dans le WAV mono chemin, la voie (0 : micro ou segment mono,
// 1 : son de la visio) de chaque segment. Renvoie la place de chaque segment (aucune : rien à analyser).
func ecrireFlux(sorties string, segs []string, voie int, chemin string) ([]partFlux, error) {
	f, err := os.Create(chemin)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	if _, err := w.Write(make([]byte, audio.TailleEnteteWAV)); err != nil { // en-tête écrit à la fin
		return nil, err
	}
	var plan []partFlux
	total := 0
	for _, seg := range segs {
		voies := audio.Voies(sorties, seg)
		if voie >= voies {
			continue
		}
		n := audio.Echantillons(sorties, seg)
		for d := 0; d < n; d += 60 * audio.Frequence {
			data, err := audio.LirePlage(sorties, seg, d, min(d+60*audio.Frequence, n))
			if err != nil {
				return nil, err
			}
			if voies == 2 {
				data = audio.ExtraireVoie(data, voie)
			}
			if _, err := w.Write(data); err != nil {
				return nil, err
			}
		}
		plan = append(plan, partFlux{seg, total, n})
		total += n
	}
	if int64(total)*2 > 0xFFFFFFFF-36 {
		return nil, errors.New("enregistrement trop long pour être analysé en une fois")
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	if _, err := f.WriteAt(audio.EnteteWAV(uint32(total*2), 1), 0); err != nil {
		return nil, err
	}
	return plan, f.Close()
}

// toursDepuisFlux ramène les tours de parole trouvés dans le flux à leurs segments
// (un tour à cheval sur deux segments est coupé en deux).
func toursDepuisFlux(plan []partFlux, segments []struct {
	Start, End float64
	Speaker    int
}, prefixe string) []tourVoix {
	var tours []tourVoix
	for _, s := range segments {
		for _, p := range plan {
			d0, d1 := float64(p.debut)/audio.Frequence, float64(p.debut+p.n)/audio.Frequence
			if a, b := max(s.Start, d0), min(s.End, d1); b > a {
				tours = append(tours, tourVoix{p.seg, a - d0, b - d0, prefixe + strconv.Itoa(s.Speaker)})
			}
		}
	}
	return tours
}

// diariser passe un WAV mono à nemo-speech et renvoie ses tours de parole.
func diariser(ctx context.Context, nemo, modele, wav string) ([]struct {
	Start, End float64
	Speaker    int
}, error) {
	sortie := strings.TrimSuffix(wav, ".wav") + ".json"
	defer os.Remove(sortie)
	cmd := exec.CommandContext(ctx, nemo, "diarize", wav, "--model", modele, "--preset", "v3-offline", "--format", "json", "-o", sortie, "--force")
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("séparation des voix interrompue")
		}
		lignes := strings.Split(strings.TrimSpace(string(out)), "\n")
		return nil, fmt.Errorf("nemo-speech a échoué (%v) : %s", err, strings.Join(lignes[max(0, len(lignes)-6):], "\n"))
	}
	raw, err := os.ReadFile(sortie)
	if err != nil {
		return nil, err
	}
	var r struct {
		Segments []struct {
			Start, End float64
			Speaker    int
		} `json:"segments"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("sortie de nemo-speech illisible : %w", err)
	}
	return r.Segments, nil
}

// Temps de calcul observé sur processeur (Core Ultra 7, 4 cœurs occupés) : ~0,2 s par seconde d'audio.
const facteurSeparation = 0.21

// Separateur : une séparation des voix à la fois, en tâche de fond (même état que la transcription).
type Separateur struct{ tache }

// Lancer démarre la séparation des voix de l'atelier dir, une fois la réunion terminée.
func (sp *Separateur) Lancer(dir string, arret <-chan struct{}, transcr *Transcripteur) error {
	if !outils.NemoDisponible() {
		return fmt.Errorf("séparation des voix indisponible sur ce système (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}
	if transcr.Occupe() {
		return errors.New("attendre la fin de la transcription")
	}
	sorties := filepath.Join(dir, "sorties")
	var segs []string
	total := 0.0
	for _, s := range audio.Lister(sorties) {
		if s.EnCours {
			return errors.New("enregistrement en cours : séparer les voix une fois la réunion terminée")
		}
		segs = append(segs, s.ID)
		total += s.Secondes // micro et visio analysés en même temps
	}
	if len(segs) == 0 {
		return errors.New("aucun enregistrement à analyser")
	}
	if _, ok := sp.demarrer(dir); !ok {
		return errors.New("séparation des voix déjà en cours")
	}
	sp.enFond(arret, "Séparation des voix terminée", func(ctx context.Context) error { return sp.executer(ctx, sorties, segs, total) },
		func(err error, duree time.Duration) {
			if err != nil {
				systeme.Logf("⚠ Séparation des voix : %v", err)
			} else {
				systeme.Logf("Séparation des voix terminée (%s) : %s", duree.Round(time.Second), fichierVoix(sorties))
			}
		})
	return nil
}

func (sp *Separateur) executer(ctx context.Context, sorties string, segs []string, total float64) error {
	nemo, modele, err := outils.AssurerNemo(ctx, func(etape string, p int) {
		sp.maj(func(e *Etat) { e.Etape, e.Pourcent = etape, p })
	})
	if err != nil {
		return err
	}
	debut, estime := time.Now(), max(total*facteurSeparation, 30)
	go func() { // nemo-speech ne dit pas où il en est : avancement estimé
		tic := time.NewTicker(2 * time.Second)
		defer tic.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tic.C:
				ecoule := time.Since(debut).Seconds()
				sp.maj(func(e *Etat) {
					if e.EnCours {
						e.Etape = fmt.Sprintf("Séparation des voix : %s sur environ %s", dureeLisible(ecoule), dureeLisible(estime))
						e.Pourcent = min(99, int(ecoule*100/estime))
					}
				})
			}
		}
	}()
	sp.maj(func(e *Etat) { e.Etape, e.Pourcent = "Séparation des voix", 0 })
	deverrouiller := sp.verrou()
	for _, seg := range segs {
		if err := audio.Finaliser(sorties, seg); err != nil {
			deverrouiller()
			return err
		}
	}
	deverrouiller()
	v := voixAtelier{Date: time.Now().Format(time.RFC3339), Segments: segs, Tours: []tourVoix{}, Noms: map[string]string{}}
	// micro et visio en même temps : nemo-speech n'occupe que 4 threads (fixé dans NeMo-Speech.cpp)
	sctx, arreter := context.WithCancel(ctx)
	defer arreter()
	prefixes := []string{"", "v"}
	resultats := make([][]tourVoix, len(prefixes))
	var mu sync.Mutex
	var premiere error // la vraie cause, pas l'interruption de l'autre voie qui s'ensuit
	var wg sync.WaitGroup
	for voie, prefixe := range prefixes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wav := filepath.Join(audio.Dossier(sorties), fmt.Sprintf("voix-%d.wav", voie))
			defer os.Remove(wav)
			plan, err := ecrireFlux(sorties, segs, voie, wav)
			if err == nil && len(plan) > 0 {
				var tours []struct {
					Start, End float64
					Speaker    int
				}
				if tours, err = diariser(sctx, nemo, modele, wav); err == nil {
					resultats[voie] = toursDepuisFlux(plan, tours, prefixe)
				}
			}
			if err != nil {
				mu.Lock()
				if premiere == nil {
					premiere = err
				}
				mu.Unlock()
				arreter() // inutile de finir l'autre voie
			}
		}()
	}
	wg.Wait()
	if premiere != nil {
		return premiere
	}
	v.Tours = append(sansEcho(resultats[0], resultats[1]), resultats[1]...)
	defer sp.verrou()()
	return ecrireVoix(sorties, v) // nouvelle analyse : numéros changés, donc noms et choix remis à zéro
}

// Part de son temps au-delà de laquelle une voix du micro, qui ne parle que quand la visio parle,
// est le son de la visio entendu par le micro (haut-parleurs). Mesuré le 29/10 : 24 % pour la
// personne au micro, 100 % pour les échos.
const partEcho = 0.6

// sansEcho retire des tours du micro les voix qui ne sont que l'écho de la visio.
func sansEcho(micro, visio []tourVoix) []tourVoix {
	if len(visio) == 0 {
		return micro
	}
	parSeg := map[string][][2]float64{} // temps de parole de la visio, sans chevauchement
	for _, t := range visio {
		parSeg[t.Seg] = append(parSeg[t.Seg], [2]float64{t.Debut, t.Fin})
	}
	for seg, l := range parSeg {
		sort.Slice(l, func(i, j int) bool { return l[i][0] < l[j][0] })
		u := l[:1]
		for _, x := range l[1:] {
			if d := &u[len(u)-1]; x[0] <= d[1] {
				d[1] = max(d[1], x[1])
			} else {
				u = append(u, x)
			}
		}
		parSeg[seg] = u
	}
	duree, couvert := map[string]float64{}, map[string]float64{}
	for _, t := range micro {
		duree[t.Voix] += t.Fin - t.Debut
		for _, x := range parSeg[t.Seg] {
			couvert[t.Voix] += max(0, min(t.Fin, x[1])-max(t.Debut, x[0]))
		}
	}
	var garde []tourVoix
	for _, t := range micro {
		if couvert[t.Voix] < partEcho*duree[t.Voix] {
			garde = append(garde, t)
		}
	}
	return garde
}
