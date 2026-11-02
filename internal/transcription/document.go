package transcription

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/aloth-decideom/greffier/internal/atelier"
	"github.com/aloth-decideom/greffier/internal/audio"
	"github.com/aloth-decideom/greffier/internal/systeme"
)

type morceau struct {
	DebutMs, FinMs int
	Texte          string
	Qui            string // qui parle : « Micro » ou « Visio » (segment stéréo), le nom d'une voix (voix.go) ; "" inconnu
}

type segmentTranscrit struct {
	ID       string
	Morceaux []morceau
}

// repere : la page affichait la question ou la page id à t secondes du début du segment seg.
type repere struct {
	Seg string  `json:"seg"`
	T   float64 `json:"t"`
	ID  string  `json:"id"`
}

func lireSortieWhisper(chemin string) ([]morceau, error) {
	raw, err := os.ReadFile(chemin)
	if err != nil {
		return nil, err
	}
	var w struct {
		Transcription []struct {
			Offsets struct{ From, To int } `json:"offsets"`
			Text    string                 `json:"text"`
			Speaker string                 `json:"speaker"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("%s : %w", filepath.Base(chemin), err)
	}
	var ms []morceau
	for _, t := range w.Transcription {
		ms = append(ms, morceau{DebutMs: t.Offsets.From, FinMs: t.Offsets.To, Texte: t.Text, Qui: nomsVoies[t.Speaker]})
	}
	return ms, nil
}

// Textes que Whisper « entend » dans les silences (génériques de sous-titres, bruits notés).
var reParasite = regexp.MustCompile(`(?i)^(\[.*\]|\(.*\)|\*.*\*|.*sous-titr.*|.*amara\.org.*|merci d'avoir regardé.*|abonnez-vous.*)$`)

func debutSegment(id string) (time.Time, bool) {
	t, err := time.ParseInLocation("seg-20060102-150405", id, time.Local)
	return t, err == nil
}

func horodatage(debut time.Time, ok bool, ms int) string {
	if ok {
		return debut.Add(time.Duration(ms) * time.Millisecond).Format("15:04:05")
	}
	s := ms / 1000
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// Voies d'un segment stéréo (whisper-cli --diarize) : qui parle, d'après la voie la plus forte du passage.
var nomsVoies = map[string]string{"0": "Micro", "1": "Visio", "?": "Micro ou visio"}

const (
	legendeVisio = "En visio : **Micro** = voix captées par le micro du poste (sur place), **Visio** = participants à distance (son de l'ordinateur), **Micro ou visio** = les deux à la fois ou indiscernables."
	legendeVoix  = "Voix séparées automatiquement après la réunion (Nemotron 3 Diarization) ; noms donnés à l'écoute par l'animateur. **Voix N** ou **Visio N** : voix non identifiée ; **(plusieurs personnes)** : voix que le modèle n'a pas su séparer, la phrase vient de l'une des personnes nommées sans qu'on sache laquelle ; **?** : plusieurs voix dans la même phrase."
)

// transcriptMarkdown assemble les segments en un document découpé par question ou page.
// titres : identifiant → intitulé (question, ou titre de la page libre) ; legende : qui parle (ou "").
func transcriptMarkdown(segs []segmentTranscrit, reperes []repere, titres map[string]string, legende string) string {
	var b strings.Builder
	b.WriteString("# Transcription de l'enregistrement\n\n")
	if legende == "" {
		b.WriteString("_Transcription automatique (whisper.cpp), non relue : erreurs de reconnaissance possibles, intervenants non identifiés. Horodatage à l'heure du poste._\n")
	} else {
		b.WriteString("_Transcription automatique (whisper.cpp), non relue : erreurs de reconnaissance possibles. Horodatage à l'heure du poste._\n\n_" + legende + "_\n")
	}
	for n, seg := range segs {
		debut, okDebut := debutSegment(seg.ID)
		var rs []repere
		for _, r := range reperes {
			if r.Seg == seg.ID {
				rs = append(rs, r)
			}
		}
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].T < rs[j].T })
		quand := ""
		if okDebut {
			quand = " — " + debut.Format("02/01/2006 à 15:04")
		}
		fmt.Fprintf(&b, "\n---\n\n**Enregistrement %d%s**\n", n+1, quand)

		courant, paragraphe, dernier, qui := "\x00", []string{}, "", ""
		debutPara, finPara := 0, 0
		vider := func() {
			if len(paragraphe) > 0 {
				prefixe := ""
				if qui != "" {
					prefixe = "**" + qui + "** : "
				}
				fmt.Fprintf(&b, "\n[%s] %s%s\n", horodatage(debut, okDebut, debutPara), prefixe, strings.Join(paragraphe, " "))
				paragraphe = nil
			}
		}
		for _, m := range seg.Morceaux {
			texte := strings.TrimSpace(m.Texte)
			if texte == "" || reParasite.MatchString(texte) || texte == dernier {
				continue
			}
			dernier = texte
			id := ""
			for _, r := range rs {
				if int(r.T*1000) <= m.DebutMs+500 { // tolérance : le repère tombe en milieu de phrase
					id = r.ID
				}
			}
			if id != courant {
				vider()
				courant = id
				if id != "" {
					titre := titres[id]
					if titre == "" {
						titre = id
					} else {
						titre = id + " · " + titre
					}
					fmt.Fprintf(&b, "\n## %s\n", titre)
				}
			}
			// une phrase « ? » (plusieurs voix) reste seule : les suivantes ne sont pas forcément des mêmes personnes
			if len(paragraphe) > 0 && (m.Qui != qui || qui == "?" || m.DebutMs-finPara > 1500 || m.DebutMs-debutPara > 45000) {
				vider()
			}
			if len(paragraphe) == 0 {
				debutPara, qui = m.DebutMs, m.Qui
			}
			paragraphe = append(paragraphe, texte)
			finPara = m.FinMs
		}
		vider()
	}
	return b.String()
}

type sessionAudio struct {
	Pages []struct{ ID, Title string } `json:"pages"`
	Audio struct {
		Reperes []repere `json:"reperes"`
	} `json:"audio"`
}

// lireSessionAudio : pages libres et repères d'enregistrement de sorties/session.json.
func lireSessionAudio(sorties string) sessionAudio {
	var s sessionAudio
	if raw, err := os.ReadFile(filepath.Join(sorties, "session.json")); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// assemblerTranscription écrit sorties/transcription.md à partir des .json des segments,
// des repères de session.json et des intitulés de l'atelier (lus maintenant : un titre de page
// ajouté après coup est donc repris).
func assemblerTranscription(dir string) (string, error) {
	sorties := filepath.Join(dir, "sorties")
	session := lireSessionAudio(sorties)
	titres := map[string]string{}
	if a, _, err := atelier.Charger(dir); err == nil {
		for _, p := range a.Parts {
			for _, s := range p.Sections {
				for _, q := range s.Questions {
					titres[q.ID] = q.Text
				}
			}
		}
	}
	for _, p := range session.Pages {
		titres[p.ID] = strings.TrimSpace(p.Title)
		if titres[p.ID] == "" {
			titres[p.ID] = "Page " + strings.TrimPrefix(p.ID, "P")
		}
	}
	var segs []segmentTranscrit
	for _, s := range audio.Lister(sorties) {
		if !s.Transcrit {
			continue
		}
		ms, err := lireSortieWhisper(filepath.Join(audio.Dossier(sorties), s.ID+".json"))
		if err != nil {
			return "", err
		}
		segs = append(segs, segmentTranscrit{ID: s.ID, Morceaux: ms})
	}
	if len(segs) == 0 {
		return "", errors.New("aucun enregistrement transcrit")
	}
	legende := ""
	if v, ok := LireVoix(sorties); ok && v.Utiliser {
		attribuerVoix(segs, v)
		legende = legendeVoix
	} else if slices.ContainsFunc(segs, func(s segmentTranscrit) bool {
		return slices.ContainsFunc(s.Morceaux, func(m morceau) bool { return m.Qui != "" })
	}) {
		legende = legendeVisio
	}
	chemin := filepath.Join(sorties, "transcription.md")
	return chemin, systeme.EcrireAtomique(chemin, []byte(transcriptMarkdown(segs, session.Audio.Reperes, titres, legende)))
}
