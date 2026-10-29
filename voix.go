package main

// Séparation des voix (diarisation), une fois la réunion terminée, en complément de la
// transcription faite au fil de l'enregistrement : NeMo-Speech.cpp (nemo-speech) avec le modèle
// Nemotron 3 Diarization de NVIDIA, téléchargés au premier usage (nemo.go).
// Rien ne quitte l'ordinateur. Les segments sont mis bout à bout et analysés en une passe, pour
// qu'une même personne garde son numéro d'une partie à l'autre ; avec une visio, le micro et le
// son de l'ordinateur sont analysés séparément (voix « 1 », « 2 »… et « v1 », « v2 »…).
// Résultat : sorties/voix.json, complété des noms donnés à l'écoute dans la page compte rendu.
// La transcription n'est découpée par voix que si l'utilisateur le choisit (Utiliser).

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
)

// cheminNemo : GREFFIER_NEMO, sinon le PATH, sinon à côté de Greffier ou aux emplacements habituels
// (archive de NeMo-Speech.cpp décompressée : nemo-speech/bin/nemo-speech).
func cheminNemo() string {
	exe := "nemo-speech"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	var candidats []string
	if p, err := executable(); err == nil {
		d := filepath.Dir(p)
		candidats = append(candidats, filepath.Join(d, "nemo-speech", "bin", exe), filepath.Join(d, exe))
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		candidats = append(candidats, filepath.Join(os.Getenv("LOCALAPPDATA"), "nemo-speech", "bin", exe))
	} else {
		candidats = append(candidats, "/opt/homebrew/bin/"+exe, "/usr/local/bin/"+exe, filepath.Join(home, ".local", "bin", exe),
			filepath.Join(home, ".local", "nemo-speech", "bin", exe))
	}
	return trouverOutil("GREFFIER_NEMO", []string{"nemo-speech"}, candidats)
}

// tourVoix : la voix parle de Debut à Fin (secondes depuis le début du segment).
type tourVoix struct {
	Seg   string  `json:"seg"`
	Debut float64 `json:"debut"`
	Fin   float64 `json:"fin"`
	Voix  string  `json:"voix"`
}

type voixAtelier struct {
	Date     string            `json:"date"`
	Segments []string          `json:"segments"` // segments analysés
	Tours    []tourVoix        `json:"tours"`
	Noms     map[string]string `json:"noms"`     // voix → nom donné à l'écoute
	Mixtes   map[string]bool   `json:"mixtes"`   // voix où le modèle a regroupé plusieurs personnes
	Utiliser bool              `json:"utiliser"` // transcription découpée par voix
}

func fichierVoix(sorties string) string { return filepath.Join(sorties, "voix.json") }

func lireVoix(sorties string) (voixAtelier, bool) {
	var v voixAtelier
	raw, err := os.ReadFile(fichierVoix(sorties))
	if err != nil || json.Unmarshal(raw, &v) != nil {
		return voixAtelier{}, false
	}
	return v, true
}

func ecrireVoix(sorties string, v voixAtelier) error {
	raw, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return writeAtomic(fichierVoix(sorties), raw)
}

// libelleVoix : nom par défaut d'une voix non identifiée.
func libelleVoix(id string) string {
	if n, ok := strings.CutPrefix(id, "v"); ok {
		return "Visio " + n
	}
	return "Voix " + id
}

// Suffixe d'une voix qui regroupe plusieurs personnes (que le modèle n'a pas su séparer).
const suffixeMixte = " (plusieurs personnes)"

func (v voixAtelier) nom(id string) string {
	n := strings.TrimSpace(v.Noms[id])
	if n == "" {
		n = libelleVoix(id)
	}
	if v.Mixtes[id] {
		n += suffixeMixte
	}
	return n
}

// voixParNom : temps de parole de chaque nom (deux voix de même nom sont réunies) entre a et b.
func (v voixAtelier) voixParNom(seg string, a, b float64, cle func(id string) string) map[string]float64 {
	c := map[string]float64{}
	for _, t := range v.Tours {
		if t.Seg == seg {
			if o := min(b, t.Fin) - max(a, t.Debut); o > 0 {
				c[cle(t.Voix)] += o
			}
		}
	}
	return c
}

// majoritaire : la clé qui parle au moins part du temps couvert, ou "" (personne, ou plusieurs voix).
func majoritaire(c map[string]float64, part float64) string {
	total, meilleur, cle := 0.0, 0.0, ""
	for k, d := range c {
		total += d
		if d > meilleur || (d == meilleur && k < cle) {
			meilleur, cle = d, k
		}
	}
	if total == 0 || meilleur <= part*total {
		return ""
	}
	return cle
}

// attribuerVoix remplace le « qui parle » de chaque phrase des segments analysés par le nom de
// la voix qui y parle plus de la moitié du temps (« ? » si aucune, ou si personne n'y parle).
func attribuerVoix(segs []segmentTranscrit, v voixAtelier) {
	for i := range segs {
		if !contient(v.Segments, segs[i].ID) {
			continue
		}
		for j := range segs[i].Morceaux {
			m := &segs[i].Morceaux[j]
			qui := majoritaire(v.voixParNom(segs[i].ID, float64(m.DebutMs)/1000, float64(m.FinMs)/1000, v.nom), 0.5)
			if qui == "" {
				qui = "?"
			}
			m.Qui = qui
		}
	}
}

func contient(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- analyse

// partFlux : le segment seg occupe n échantillons à partir de debut dans le flux analysé.
type partFlux struct {
	seg      string
	debut, n int
}

// extraireVoie : la voie (0 ou 1) d'un PCM 16 bits stéréo entrelacé.
func extraireVoie(data []byte, voie int) []byte {
	out := make([]byte, len(data)/2)
	for i, j := voie*2, 0; i+1 < len(data); i, j = i+4, j+2 {
		out[j], out[j+1] = data[i], data[i+1]
	}
	return out
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
	if _, err := w.Write(make([]byte, tailleEnteteWAV)); err != nil { // en-tête écrit à la fin
		return nil, err
	}
	var plan []partFlux
	total := 0
	for _, seg := range segs {
		voies := voiesSegment(sorties, seg)
		if voie >= voies {
			continue
		}
		n := echantillons(sorties, seg)
		for d := 0; d < n; d += 60 * frequenceAudio {
			data, err := lirePlage(sorties, seg, d, min(d+60*frequenceAudio, n))
			if err != nil {
				return nil, err
			}
			if voies == 2 {
				data = extraireVoie(data, voie)
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
	if _, err := f.WriteAt(enteteWAV(uint32(total*2), 1), 0); err != nil {
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
			d0, d1 := float64(p.debut)/frequenceAudio, float64(p.debut+p.n)/frequenceAudio
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

// separateur : une séparation des voix à la fois, en tâche de fond (même état que la transcription).
type separateur struct {
	mu      sync.Mutex
	dossier string
	etat    EtatTranscription
	disque  *sync.Mutex // verrou des écritures sur disque du serveur
}

func (sp *separateur) Etat(dir string) EtatTranscription {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.dossier != dir {
		return EtatTranscription{}
	}
	return sp.etat
}

func (sp *separateur) maj(f func(e *EtatTranscription)) {
	sp.mu.Lock()
	f(&sp.etat)
	sp.mu.Unlock()
}

// Lancer démarre la séparation des voix de l'atelier dir, une fois la réunion terminée.
func (sp *separateur) Lancer(dir string, arret <-chan struct{}, transcr *transcripteur) error {
	if !nemoDisponible() {
		return fmt.Errorf("séparation des voix indisponible sur ce système (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}
	if transcr.occupe() {
		return errors.New("attendre la fin de la transcription")
	}
	sorties := filepath.Join(dir, "sorties")
	var segs []string
	total := 0.0
	for _, s := range listerSegments(sorties) {
		if s.EnCours {
			return errors.New("enregistrement en cours : séparer les voix une fois la réunion terminée")
		}
		segs = append(segs, s.ID)
		total += s.Secondes // micro et visio analysés en même temps
	}
	if len(segs) == 0 {
		return errors.New("aucun enregistrement à analyser")
	}
	sp.mu.Lock()
	if sp.etat.EnCours {
		sp.mu.Unlock()
		return errors.New("séparation des voix déjà en cours")
	}
	sp.dossier, sp.etat = dir, EtatTranscription{EnCours: true, Etape: "Préparation"}
	sp.mu.Unlock()

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
		err := sp.executer(ctx, sorties, segs, total)
		cancel()
		sp.maj(func(e *EtatTranscription) {
			*e = EtatTranscription{Etape: "Séparation des voix terminée", Pourcent: 100}
			if err != nil {
				*e = EtatTranscription{Erreur: err.Error()}
			}
		})
		if err != nil {
			logf("⚠ Séparation des voix : %v", err)
		} else {
			logf("Séparation des voix terminée (%s) : %s", time.Since(debut).Round(time.Second), fichierVoix(sorties))
		}
	}()
	return nil
}

func (sp *separateur) executer(ctx context.Context, sorties string, segs []string, total float64) error {
	nemo, modele, err := assurerNemo(ctx, func(etape string, p int) {
		sp.maj(func(e *EtatTranscription) { e.Etape, e.Pourcent = etape, p })
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
				sp.maj(func(e *EtatTranscription) {
					if e.EnCours {
						e.Etape = fmt.Sprintf("Séparation des voix : %s sur environ %s", dureeLisible(ecoule), dureeLisible(estime))
						e.Pourcent = min(99, int(ecoule*100/estime))
					}
				})
			}
		}
	}()
	sp.maj(func(e *EtatTranscription) { e.Etape, e.Pourcent = "Séparation des voix", 0 })
	sp.disque.Lock()
	for _, seg := range segs {
		if err := finaliserWAV(sorties, seg); err != nil {
			sp.disque.Unlock()
			return err
		}
	}
	sp.disque.Unlock()
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
			wav := filepath.Join(dossierAudio(sorties), fmt.Sprintf("voix-%d.wav", voie))
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
	sp.disque.Lock()
	defer sp.disque.Unlock()
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

// ---------------------------------------------------------------- page compte rendu

// extraitVoix : un passage à écouter (secondes depuis le début du segment, voie à faire entendre).
type extraitVoix struct {
	Seg   string  `json:"seg"`
	Debut float64 `json:"debut"`
	Fin   float64 `json:"fin"`
	Voie  int     `json:"voie"`
	Texte string  `json:"texte,omitempty"`
}

type resumeVoix struct {
	ID       string        `json:"id"`
	Libelle  string        `json:"libelle"`
	Nom      string        `json:"nom"`
	Mixte    bool          `json:"mixte"`
	Secondes float64       `json:"secondes"`
	Extraits []extraitVoix `json:"extraits"`
}

const extraitsParVoix = 8

// resumerVoix : chaque voix trouvée, avec son temps de parole et des phrases à écouter, réparties
// sur l'enregistrement (phrases de la transcription où elle parle seule, sinon ses plus longs tours).
func resumerVoix(sorties string, v voixAtelier) []resumeVoix {
	par := map[string]*resumeVoix{}
	candidats := map[string][]extraitVoix{}
	for _, t := range v.Tours {
		r := par[t.Voix]
		if r == nil {
			r = &resumeVoix{ID: t.Voix, Libelle: libelleVoix(t.Voix), Nom: v.Noms[t.Voix], Mixte: v.Mixtes[t.Voix], Extraits: []extraitVoix{}}
			par[t.Voix] = r
		}
		r.Secondes += t.Fin - t.Debut
	}
	voie := func(id string) int {
		if strings.HasPrefix(id, "v") {
			return 1
		}
		return 0
	}
	for _, seg := range v.Segments {
		ms, err := lireSortieWhisper(filepath.Join(dossierAudio(sorties), seg+".json"))
		if err != nil {
			continue
		}
		for _, m := range ms {
			a, b := float64(m.DebutMs)/1000, float64(m.FinMs)/1000
			texte := strings.TrimSpace(m.Texte)
			if b-a < 1.5 || texte == "" || reParasite.MatchString(texte) {
				continue
			}
			if id := majoritaire(v.voixParNom(seg, a, b, func(id string) string { return id }), 0.8); id != "" {
				candidats[id] = append(candidats[id], extraitVoix{seg, a, b, voie(id), texte})
			}
		}
	}
	secours := map[string][]extraitVoix{} // voix sans phrase transcrite : ses tours de parole
	for _, t := range v.Tours {
		if t.Fin-t.Debut >= 1.5 {
			secours[t.Voix] = append(secours[t.Voix], extraitVoix{t.Seg, t.Debut, min(t.Fin, t.Debut+20), voie(t.Voix), ""})
		}
	}
	liste := []resumeVoix{}
	for id, r := range par {
		c := candidats[id]
		if len(c) == 0 {
			c = secours[id]
		}
		// une phrase, la plus longue, par tranche de l'enregistrement
		for k := range extraitsParVoix {
			lot := c[k*len(c)/extraitsParVoix : (k+1)*len(c)/extraitsParVoix]
			if len(lot) == 0 {
				continue
			}
			best := lot[0]
			for _, e := range lot[1:] {
				if e.Fin-e.Debut > best.Fin-best.Debut {
					best = e
				}
			}
			r.Extraits = append(r.Extraits, best)
		}
		liste = append(liste, *r)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].Secondes > liste[j].Secondes })
	return liste
}

// majVoix enregistre les noms, les voix qui regroupent plusieurs personnes et le choix de découper
// la transcription par voix, puis réécrit transcription.md en conséquence.
func majVoix(dir string, noms map[string]string, mixtes map[string]bool, utiliser bool) error {
	sorties := filepath.Join(dir, "sorties")
	v, ok := lireVoix(sorties)
	if !ok {
		return errors.New("voix pas encore séparées")
	}
	v.Noms, v.Mixtes = map[string]string{}, map[string]bool{}
	for _, t := range v.Tours {
		if n := strings.TrimSpace(noms[t.Voix]); n != "" {
			v.Noms[t.Voix] = n
		}
		if mixtes[t.Voix] {
			v.Mixtes[t.Voix] = true
		}
	}
	v.Utiliser = utiliser
	if err := ecrireVoix(sorties, v); err != nil {
		return err
	}
	if _, err := assemblerTranscription(dir); err != nil && !strings.Contains(err.Error(), "aucun enregistrement transcrit") {
		return err
	}
	return nil
}

// Écoute d'un passage depuis la page compte rendu : 60 s au plus.
const dureeMaxExtrait = 60

// extraitAudio : le passage [debut, fin] (secondes) du segment, en WAV mono (la voie demandée d'un segment stéréo).
func extraitAudio(sorties, seg string, debut, fin float64, voie int) ([]byte, error) {
	if !reSegment.MatchString(seg) {
		return nil, errors.New("nom de segment invalide")
	}
	if fin-debut > dureeMaxExtrait {
		return nil, errors.New("passage trop long")
	}
	d, f := max(int(debut*frequenceAudio), 0), min(int(fin*frequenceAudio), echantillons(sorties, seg))
	if f <= d {
		return nil, errors.New("passage invalide")
	}
	data, err := lirePlage(sorties, seg, d, f)
	if err != nil {
		return nil, err
	}
	if voiesSegment(sorties, seg) == 2 {
		data = extraireVoie(data, min(max(voie, 0), 1))
	}
	return append(enteteWAV(uint32(len(data)), 1), data...), nil
}
