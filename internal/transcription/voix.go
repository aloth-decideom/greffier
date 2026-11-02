package transcription

// Séparation des voix (diarisation), une fois la réunion terminée, en complément de la
// transcription faite au fil de l'enregistrement : NeMo-Speech.cpp (nemo-speech) avec le modèle
// Nemotron 3 Diarization de NVIDIA, téléchargés au premier usage (package outils).
// Rien ne quitte l'ordinateur. Les segments sont mis bout à bout et analysés en une passe, pour
// qu'une même personne garde son numéro d'une partie à l'autre ; avec une visio, le micro et le
// son de l'ordinateur sont analysés séparément (voix « 1 », « 2 »… et « v1 », « v2 »…).
// Résultat : sorties/voix.json, complété des noms donnés à l'écoute dans la page compte rendu.
// La transcription n'est découpée par voix que si l'utilisateur le choisit (Utiliser).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/aloth-decideom/greffier/internal/audio"
	"github.com/aloth-decideom/greffier/internal/systeme"
)

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
	Noms     map[string]string `json:"noms"`             // voix → nom donné à l'écoute
	Mixtes   map[string]bool   `json:"mixtes"`           // voix où le modèle a regroupé plusieurs personnes
	Utiliser bool              `json:"utiliser"`         // transcription découpée par voix
	Choisi   bool              `json:"choisi,omitempty"` // découpage choisi à la main ; sinon proposé dès qu'une voix a un nom
}

func fichierVoix(sorties string) string { return filepath.Join(sorties, "voix.json") }

func LireVoix(sorties string) (voixAtelier, bool) {
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
	return systeme.EcrireAtomique(fichierVoix(sorties), raw)
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
		if !slices.Contains(v.Segments, segs[i].ID) {
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

// ResumerVoix : chaque voix trouvée, avec son temps de parole et des phrases à écouter, réparties
// sur l'enregistrement (phrases de la transcription où elle parle seule, sinon ses plus longs tours).
func ResumerVoix(sorties string, v voixAtelier) []resumeVoix {
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
		ms, err := lireSortieWhisper(filepath.Join(audio.Dossier(sorties), seg+".json"))
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

// MajVoix enregistre les noms, les voix qui regroupent plusieurs personnes et le choix de découper
// la transcription par voix, puis réécrit transcription.md en conséquence.
func MajVoix(dir string, noms map[string]string, mixtes map[string]bool, utiliser, choisi bool) error {
	sorties := filepath.Join(dir, "sorties")
	v, ok := LireVoix(sorties)
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
	v.Utiliser, v.Choisi = utiliser, v.Choisi || choisi
	if err := ecrireVoix(sorties, v); err != nil {
		return err
	}
	if _, err := assemblerTranscription(dir); err != nil && !strings.Contains(err.Error(), "aucun enregistrement transcrit") {
		return err
	}
	return nil
}
