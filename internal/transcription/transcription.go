// Package transcription : transcription locale de l'enregistrement (whisper.cpp) et séparation
// des voix après la réunion (NeMo-Speech.cpp). Rien ne quitte l'ordinateur.
package transcription

// Transcription locale de l'enregistrement avec whisper.cpp (whisper-cli, téléchargé au premier
// usage s'il n'est pas installé : package outils) : rien ne quitte l'ordinateur. Le modèle est téléchargé une fois dans le cache de l'utilisateur.
// Chaque segment donne audio/<segment>.json ; sorties/transcription.md les assemble, découpée
// par question ou par page grâce aux repères posés par la page pendant l'enregistrement.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

var reProgres = regexp.MustCompile(`progress\s*=\s*(\d+)%`)

// GREFFIER_SANS_TRANSCRIPTION_DIRECTE=1 : ne transcrire qu'à la demande (bouton « Transcrire »),
// par exemple sur un poste trop lent pour transcrire pendant l'atelier.
var DirecteDesactivee = os.Getenv("GREFFIER_SANS_TRANSCRIPTION_DIRECTE") != ""

// Un long passage sur la même question est coupé toutes les 2 min, pour être transcrit pendant
// que l'enregistrement continue.
const dureeMaxMorceau = 120 * audio.Frequence

// morceauxAudio : bornes (en échantillons) des morceaux d'un segment de n échantillons, coupé à
// chaque repère. Ainsi chaque passage transcrit appartient sans ambiguïté à sa question, même
// quand la détection des silences rend l'horodatage de Whisper approximatif.
// Segment en cours d'enregistrement (complet = false) : seuls les morceaux terminés sont
// renvoyés ; leurs bornes ne changent plus quand l'enregistrement s'allonge.
func morceauxAudio(n int, reperes []repere, seg string, complet bool) [][2]int {
	coupes := []int{0}
	var ts []float64
	for _, r := range reperes {
		if r.Seg == seg {
			ts = append(ts, r.T)
		}
	}
	sort.Float64s(ts)
	for _, t := range ts {
		c := int(t * audio.Frequence)
		if c-coupes[len(coupes)-1] >= audio.Frequence/2 && n-c >= audio.Frequence/2 { // morceaux d'au moins 0,5 s
			coupes = append(coupes, c)
		}
	}
	var bornes [][2]int
	for i, c := range coupes {
		fin := n
		if i+1 < len(coupes) {
			fin = coupes[i+1]
		}
		for d := c; d < fin; d += dureeMaxMorceau {
			f := min(d+dureeMaxMorceau, fin)
			if !complet && i+1 == len(coupes) && f < d+dureeMaxMorceau {
				break // dernier morceau, encore en train de s'enregistrer
			}
			bornes = append(bornes, [2]int{d, f})
		}
	}
	return bornes
}

// Les morceaux transcrits sont gardés dans audio/<seg>.morceaux/<début>-<fin>.json
// (en échantillons) : rien n'est transcrit deux fois.
func cheminMorceau(sorties, seg string, b [2]int) string {
	return filepath.Join(audio.Dossier(sorties), seg+".morceaux", fmt.Sprintf("%d-%d", b[0], b[1]))
}

// transcrireMorceau passe un morceau du segment à whisper-cli (sauf s'il est déjà transcrit).
func (t *Transcripteur) transcrireMorceau(ctx context.Context, whisper, modele, vad string, threads int, sorties, seg string, b [2]int, progres func(int)) error {
	f := cheminMorceau(sorties, seg, b)
	if _, err := os.Stat(f + ".json"); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	deverrouiller := t.verrou()
	donnees, err := audio.LirePlage(sorties, seg, b[0], b[1])
	voies := audio.Voies(sorties, seg)
	deverrouiller()
	if err != nil {
		return err
	}
	if err := os.WriteFile(f+".wav", append(audio.EnteteWAV(uint32(len(donnees)), voies), donnees...), 0o644); err != nil {
		return err
	}
	defer os.Remove(f + ".wav")
	tmp := f + ".en-cours"
	args := []string{"-m", modele, "-f", f + ".wav", "-l", "fr", "-t", strconv.Itoa(threads), "-pp", "-oj", "-of", tmp}
	if vad != "" {
		args = append(args, "--vad", "-vm", vad)
	}
	if voies == 2 {
		args = append(args, "--diarize") // chaque passage : voie la plus forte, "0" micro, "1" visio, "?" les deux
	}
	if err := lancerWhisper(ctx, whisper, args, progres); err != nil {
		os.Remove(tmp + ".json")
		return err
	}
	return os.Rename(tmp+".json", f+".json")
}

// morceauxTranscrits : tous les morceaux du segment ont leur transcription.
func morceauxTranscrits(sorties, seg string, bornes [][2]int) bool {
	for _, b := range bornes {
		if _, err := os.Stat(cheminMorceau(sorties, seg, b) + ".json"); err != nil {
			return false
		}
	}
	return true
}

// assemblerSegment réunit les morceaux transcrits d'un segment terminé dans audio/<seg>.json
// (format de whisper-cli, décalages en ms depuis le début du segment).
func assemblerSegment(sorties, seg string, bornes [][2]int) error {
	type sortie struct {
		Offsets struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"offsets"`
		Text    string `json:"text"`
		Speaker string `json:"speaker,omitempty"`
	}
	tout := []sortie{}
	for _, b := range bornes {
		raw, err := os.ReadFile(cheminMorceau(sorties, seg, b) + ".json")
		if err != nil {
			return err
		}
		var w struct{ Transcription []sortie }
		if err := json.Unmarshal(raw, &w); err != nil {
			return fmt.Errorf("sortie de whisper-cli illisible : %w", err)
		}
		debut, fin := b[0]*1000/audio.Frequence, b[1]*1000/audio.Frequence
		for _, t := range w.Transcription {
			// décalages ramenés au segment, et bornés au morceau (la détection des silences les rend approximatifs)
			t.Offsets.From = min(max(debut+t.Offsets.From, debut), fin-1)
			t.Offsets.To = min(max(debut+t.Offsets.To, t.Offsets.From), fin)
			tout = append(tout, t)
		}
	}
	res, err := json.Marshal(map[string]any{"transcription": tout})
	if err != nil {
		return err
	}
	return systeme.EcrireAtomique(filepath.Join(audio.Dossier(sorties), seg+".json"), res)
}

// lancerWhisper exécute whisper-cli et suit sa progression (option -pp, sur stderr).
func lancerWhisper(ctx context.Context, whisper string, args []string, progres func(int)) error {
	cmd := exec.CommandContext(ctx, whisper, args...)
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var fin []string // dernières lignes, pour expliquer un échec
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		ligne := sc.Text()
		if m := reProgres.FindStringSubmatch(ligne); m != nil {
			p, _ := strconv.Atoi(m[1])
			progres(p)
		}
		if fin = append(fin, ligne); len(fin) > 8 {
			fin = fin[1:]
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return errors.New("transcription interrompue")
		}
		return fmt.Errorf("whisper-cli a échoué (%v) : %s", err, strings.TrimSpace(strings.Join(fin, "\n")))
	}
	return nil
}

// ---------------------------------------------------------------- tâche de fond

// Transcripteur : une transcription à la fois (elle occupe le processeur). Démarrée par le
// premier morceau d'audio reçu, elle suit l'enregistrement : chaque question terminée (ou toutes
// les 2 min) est transcrite pendant l'atelier, avec moins de threads pour ne pas gêner le poste.
// Quand plus rien ne s'enregistre (pause comprise), elle transcrit le reste, plusieurs morceaux
// à la fois, et écrit transcription.md.
type Transcripteur struct {
	tache
	reveil chan struct{}
}

// Reveiller : un enregistrement vient d'être mis en pause, inutile d'attendre la prochaine vérification.
func (t *Transcripteur) Reveiller() {
	select {
	case t.canalReveil() <- struct{}{}:
	default: // un réveil est déjà en attente
	}
}

func (t *Transcripteur) canalReveil() chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reveil == nil {
		t.reveil = make(chan struct{}, 1)
	}
	return t.reveil
}

// Lancer démarre la transcription de l'atelier dir (sans effet si elle tourne déjà pour lui).
// arret : fermé à l'arrêt de l'application, qui interrompt whisper-cli.
func (t *Transcripteur) Lancer(dir string, arret <-chan struct{}) error {
	if !outils.WhisperDisponible() {
		return errors.New("whisper.cpp n'existe pas pour ce système : l'installer à la main (voir le README)")
	}
	if autre, ok := t.demarrer(dir); !ok {
		if autre != dir {
			return errors.New("la transcription d'un autre atelier est en cours")
		}
		return nil // déjà lancée pour cet atelier
	}
	t.enFond(arret, "Transcription terminée", func(ctx context.Context) error { return t.executer(ctx, dir) },
		func(err error, duree time.Duration) {
			if err != nil {
				systeme.Logf("⚠ Transcription : %v", err)
			} else {
				systeme.Logf("Transcription terminée (%s après son démarrage) : %s", duree.Round(time.Second), filepath.Join(dir, "sorties", "transcription.md"))
			}
		})
	return nil
}

func (t *Transcripteur) executer(ctx context.Context, dir string) error {
	sorties := filepath.Join(dir, "sorties")
	whisper, err := outils.AssurerWhisper(ctx, func(p int) {
		t.maj(func(e *Etat) {
			e.Etape, e.Pourcent = "Téléchargement de whisper.cpp (une seule fois)", p
		})
	})
	if err != nil {
		return fmt.Errorf("installation de whisper.cpp : %w", err)
	}
	t.maj(func(e *Etat) { e.Etape = "Modèle de transcription" })
	modele, err := outils.AssurerModele(ctx, func(p int) {
		t.maj(func(e *Etat) {
			e.Etape, e.Pourcent = "Téléchargement du modèle de transcription (une seule fois)", p
		})
	})
	if err != nil {
		return err
	}
	t.maj(func(e *Etat) { e.Etape, e.Pourcent = "Détection des silences", 0 })
	vad := outils.AssurerVAD(ctx, whisper)
	threadsDirect := max(1, min(runtime.NumCPU()/2, 6))
	paralleles := max(1, min(runtime.NumCPU()/4, 3)) // whisper-cli gagne peu au-delà de 8 threads : plusieurs morceaux à la fois
	threads := max(1, min(runtime.NumCPU()/paralleles, 8))

	for {
		reperes := lireSessionAudio(sorties).Audio.Reperes
		segments := audio.Lister(sorties)
		enDirect := false
		total, fait := 0.0, 0.0
		type tache struct {
			seg    audio.InfoSegment
			bornes [][2]int
		}
		var taches []tache
		for _, sg := range segments {
			total += sg.Secondes
			if sg.Transcrit {
				fait += sg.Secondes
				continue
			}
			enDirect = enDirect || sg.EnCours
			deverrouiller := t.verrou()
			var err error
			if !sg.EnCours {
				err = audio.Finaliser(sorties, sg.ID)
			}
			n := audio.Echantillons(sorties, sg.ID)
			deverrouiller()
			if err != nil {
				return err
			}
			bornes := morceauxAudio(n, reperes, sg.ID, !sg.EnCours)
			for _, b := range bornes {
				if _, err := os.Stat(cheminMorceau(sorties, sg.ID, b) + ".json"); err == nil {
					fait += float64(b[1]-b[0]) / audio.Frequence
				}
			}
			taches = append(taches, tache{sg, bornes})
		}
		libelle := "Transcription"
		if enDirect {
			libelle = "Transcription au fil de l'enregistrement"
		}
		var mu sync.Mutex // fait et partiel, partagés par les morceaux transcrits en même temps
		partiel := map[string]float64{}
		etat := func() {
			f := fait
			for _, p := range partiel {
				f += p
			}
			t.maj(func(e *Etat) {
				e.Etape = fmt.Sprintf("%s : %s sur %s", libelle, dureeLisible(f), dureeLisible(total))
				e.Pourcent = int(f * 100 / max(total, 1))
			})
		}
		etat()
		type travail struct {
			seg string
			b   [2]int
		}
		var travaux []travail
		for _, ta := range taches {
			for _, b := range ta.bornes {
				if _, err := os.Stat(cheminMorceau(sorties, ta.seg.ID, b) + ".json"); err != nil {
					travaux = append(travaux, travail{ta.seg.ID, b})
				}
			}
		}
		n, p := threads, paralleles
		if enDirect {
			// un morceau par passage, sans gêner le poste : la pause est vue dès le morceau suivant
			n, p, travaux = threadsDirect, 1, travaux[:min(len(travaux), 1)]
		}
		sctx, arreter := context.WithCancel(ctx)
		file := make(chan travail)
		erreurs := make(chan error, p)
		var wg sync.WaitGroup
		for range p {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for tr := range file {
					cle := cheminMorceau(sorties, tr.seg, tr.b)
					d := float64(tr.b[1]-tr.b[0]) / audio.Frequence
					err := t.transcrireMorceau(sctx, whisper, modele, vad, n, sorties, tr.seg, tr.b, func(pc int) {
						mu.Lock()
						partiel[cle] = d * float64(pc) / 100
						etat()
						mu.Unlock()
					})
					mu.Lock()
					delete(partiel, cle)
					if err == nil {
						fait += d
					}
					etat()
					mu.Unlock()
					if err != nil {
						erreurs <- err
						arreter()
						return
					}
				}
			}()
		}
	distribuer:
		for _, tr := range travaux {
			select {
			case file <- tr:
			case <-sctx.Done():
				break distribuer
			}
		}
		close(file)
		wg.Wait()
		arreter()
		select {
		case err := <-erreurs:
			return err
		default:
		}
		for _, ta := range taches {
			if ta.seg.EnCours || !morceauxTranscrits(sorties, ta.seg.ID, ta.bornes) {
				continue
			}
			if err := assemblerSegment(sorties, ta.seg.ID, ta.bornes); err != nil {
				return err
			}
		}
		if !enDirect {
			break
		}
		if len(travaux) > 0 {
			continue // d'autres morceaux attendent peut-être déjà
		}
		select { // l'enregistrement continue : on revient voir ce qui s'est terminé
		case <-ctx.Done():
			return errors.New("transcription interrompue")
		case <-t.canalReveil():
		case <-time.After(15 * time.Second):
		}
	}
	_, err = assemblerTranscription(dir)
	return err
}
