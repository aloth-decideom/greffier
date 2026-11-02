// Package audio : enregistrement du son de l'atelier, segment par segment.
package audio

// Enregistrement du son de l'atelier. La page capte le micro en PCM 16 bits, 16 kHz, mono
// (le format attendu par whisper.cpp) et en envoie un morceau toutes les 5 s : chaque
// période d'enregistrement (segment) s'écrit au fil de l'eau dans sorties/audio/<segment>.pcm,
// puis devient un .wav (en-tête ajouté) à la pause ou avant la transcription.
// Un plantage du navigateur ne fait donc perdre que les dernières secondes.
// Avec le son d'une visio, le segment est en stéréo (voie gauche : micro, voie droite : son de
// l'ordinateur), signalé par <segment>.stereo : la transcription dit alors qui parle.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/aloth-decideom/greffier/internal/systeme"
)

const (
	Frequence        = 16000
	OctetsParSeconde = Frequence * 2 // 16 bits, par voie
	TailleEnteteWAV  = 44
)

// Segment : seg-AAAAMMJJ-HHMMSS (heure de début, locale au poste qui enregistre).
var reSegment = regexp.MustCompile(`^seg-\d{8}-\d{6}$`)

type InfoSegment struct {
	ID        string  `json:"id"`
	Secondes  float64 `json:"secondes"`
	Transcrit bool    `json:"transcrit"`
	EnCours   bool    `json:"enCours"` // .pcm écrit il y a peu : enregistrement en cours
	modifie   time.Time
	aPCM      bool
}

// Délai sans écriture au-delà duquel un .pcm est considéré comme abandonné (plantage, onglet fermé).
const delaiSegmentActif = 20 * time.Second

func Dossier(sorties string) string { return filepath.Join(sorties, "audio") }

// Voies : 2 si le segment est en stéréo (micro + visio), 1 sinon.
func Voies(sorties, seg string) int {
	if _, err := os.Stat(filepath.Join(Dossier(sorties), seg+".stereo")); err == nil {
		return 2
	}
	return 1
}

// Ajouter ajoute un morceau de PCM (voies : 1, ou 2 entrelacées) à la fin du segment.
func Ajouter(sorties, seg string, data []byte, voies int) error {
	if !reSegment.MatchString(seg) {
		return errors.New("nom de segment invalide")
	}
	if voies != 1 && voies != 2 {
		return errors.New("nombre de voies invalide")
	}
	if len(data)%(2*voies) != 0 {
		return errors.New("morceau audio incomplet")
	}
	dir := Dossier(sorties)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if Echantillons(sorties, seg) == 0 {
		// début du segment : son nombre de voies est fixé
		marque := filepath.Join(dir, seg+".stereo")
		var err error
		if voies == 2 {
			err = os.WriteFile(marque, nil, 0o644)
		} else if err = os.Remove(marque); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		if err != nil {
			return err
		}
	} else if Voies(sorties, seg) != voies {
		return fmt.Errorf("segment %s : nombre de voies différent du début de l'enregistrement", seg)
	}
	f, err := os.OpenFile(filepath.Join(dir, seg+".pcm"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// EnteteWAV : en-tête RIFF d'un fichier PCM 16 bits à 16 kHz (voies : 1 mono, 2 stéréo) de n octets de données.
func EnteteWAV(n uint32, voies int) []byte {
	h := make([]byte, 0, TailleEnteteWAV)
	le := binary.LittleEndian
	h = append(h, "RIFF"...)
	h = le.AppendUint32(h, 36+n)
	h = append(h, "WAVEfmt "...)
	h = le.AppendUint32(h, 16)                             // taille du bloc fmt
	h = le.AppendUint16(h, 1)                              // PCM
	h = le.AppendUint16(h, uint16(voies))                  // voies
	h = le.AppendUint32(h, Frequence)                      // échantillons par seconde
	h = le.AppendUint32(h, uint32(OctetsParSeconde*voies)) // octets par seconde
	h = le.AppendUint16(h, uint16(2*voies))                // octets par échantillon (toutes voies)
	h = le.AppendUint16(h, 16)                             // bits par échantillon
	h = append(h, "data"...)
	return le.AppendUint32(h, n)
}

// Finaliser transforme <seg>.pcm en <seg>.wav. Si un .wav existe déjà (reprise du même
// segment après un plantage), le PCM est ajouté à la suite de ses données.
func Finaliser(sorties, seg string) error {
	if !reSegment.MatchString(seg) {
		return errors.New("nom de segment invalide")
	}
	dir := Dossier(sorties)
	pcm := filepath.Join(dir, seg+".pcm")
	wav := filepath.Join(dir, seg+".wav")
	nouveau, err := os.ReadFile(pcm)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // déjà finalisé
	}
	if err != nil {
		return err
	}
	var donnees []byte
	if ancien, err := os.ReadFile(wav); err == nil && len(ancien) >= TailleEnteteWAV {
		donnees = ancien[TailleEnteteWAV:]
	}
	donnees = append(donnees, nouveau...)
	if len(donnees) > 0xFFFFFFFF-36 {
		return fmt.Errorf("segment %s trop long pour un fichier WAV", seg)
	}
	if err := systeme.EcrireAtomique(wav, append(EnteteWAV(uint32(len(donnees)), Voies(sorties, seg)), donnees...)); err != nil {
		return err
	}
	return os.Remove(pcm)
}

// Lister : les segments enregistrés (.pcm en cours ou .wav), dans l'ordre chronologique.
func Lister(sorties string) []InfoSegment {
	dir := Dossier(sorties)
	entries, _ := os.ReadDir(dir)
	parID := map[string]*InfoSegment{}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		id := strings.TrimSuffix(e.Name(), ext)
		if e.IsDir() || (ext != ".pcm" && ext != ".wav") || !reSegment.MatchString(id) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		n := st.Size()
		if ext == ".wav" {
			n -= TailleEnteteWAV
		}
		s := parID[id]
		if s == nil {
			s = &InfoSegment{ID: id}
			parID[id] = s
		}
		s.Secondes += float64(max(n, 0)) / OctetsParSeconde
		if st.ModTime().After(s.modifie) {
			s.modifie = st.ModTime()
		}
		if ext == ".pcm" {
			s.aPCM = true
			s.EnCours = time.Since(st.ModTime()) < delaiSegmentActif
		}
	}
	liste := []InfoSegment{}
	for _, s := range parID {
		s.Secondes /= float64(Voies(sorties, s.ID))
		// transcrit : le .json est plus récent que tout l'audio du segment (sinon, à refaire)
		st, err := os.Stat(filepath.Join(dir, s.ID+".json"))
		s.Transcrit = err == nil && !s.aPCM && !st.ModTime().Before(s.modifie)
		liste = append(liste, *s)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].ID < liste[j].ID })
	return liste
}

// Supprimer déplace les enregistrements dans audio/supprimes/ (jamais effacés, comme
// les photos). Les transcriptions (.json) restent en place.
func Supprimer(sorties string) error {
	dir := Dossier(sorties)
	dest := filepath.Join(dir, "supprimes")
	for _, s := range Lister(sorties) {
		for _, ext := range []string{".wav", ".pcm", ".stereo"} {
			p := filepath.Join(dir, s.ID+ext)
			if _, err := os.Stat(p); err != nil {
				continue
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			if err := os.Rename(p, filepath.Join(dest, s.ID+ext)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- lecture

// Audio d'un segment : les données du .wav finalisé, suivies du .pcm en cours d'écriture
// (les deux n'existent ensemble qu'après une reprise sur plantage).
func fichiersAudio(sorties, seg string) [2]struct {
	chemin string
	entete int64
} {
	base := filepath.Join(Dossier(sorties), seg)
	return [2]struct {
		chemin string
		entete int64
	}{{base + ".wav", TailleEnteteWAV}, {base + ".pcm", 0}}
}

// Echantillons : nombre d'échantillons enregistrés dans le segment (par voie).
func Echantillons(sorties, seg string) int {
	var n int64
	for _, f := range fichiersAudio(sorties, seg) {
		if st, err := os.Stat(f.chemin); err == nil {
			n += max(st.Size()-f.entete, 0)
		}
	}
	return int(n / int64(2*Voies(sorties, seg)))
}

// LirePlage : les échantillons [d, f) du segment (toutes voies), sans relire tout l'enregistrement.
func LirePlage(sorties, seg string, d, f int) ([]byte, error) {
	taille := int64(2 * Voies(sorties, seg))
	out := make([]byte, 0, int64(f-d)*taille)
	off, reste := int64(d)*taille, int64(f-d)*taille
	for _, fa := range fichiersAudio(sorties, seg) {
		fi, err := os.Open(fa.chemin)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		st, err := fi.Stat()
		if err == nil {
			taille := st.Size() - fa.entete
			if off < taille && reste > 0 {
				buf := make([]byte, min(reste, taille-off))
				_, err = fi.ReadAt(buf, fa.entete+off)
				out, reste, off = append(out, buf...), reste-int64(len(buf)), 0
			} else {
				off -= max(taille, 0)
			}
		}
		fi.Close()
		if err != nil {
			return nil, err
		}
	}
	if reste > 0 {
		return nil, fmt.Errorf("audio incomplet pour %s", seg)
	}
	return out, nil
}

// ExtraireVoie : la voie (0 ou 1) d'un PCM 16 bits stéréo entrelacé.
func ExtraireVoie(data []byte, voie int) []byte {
	out := make([]byte, len(data)/2)
	for i, j := voie*2, 0; i+1 < len(data); i, j = i+4, j+2 {
		out[j], out[j+1] = data[i], data[i+1]
	}
	return out
}

// Écoute d'un passage depuis la page compte rendu : 60 s au plus.
const DureeMaxExtrait = 60

// Extrait : le passage [debut, fin] (secondes) du segment, en WAV mono (la voie demandée d'un segment stéréo).
func Extrait(sorties, seg string, debut, fin float64, voie int) ([]byte, error) {
	if !reSegment.MatchString(seg) {
		return nil, errors.New("nom de segment invalide")
	}
	if fin-debut > DureeMaxExtrait {
		return nil, errors.New("passage trop long")
	}
	d, f := max(int(debut*Frequence), 0), min(int(fin*Frequence), Echantillons(sorties, seg))
	if f <= d {
		return nil, errors.New("passage invalide")
	}
	data, err := LirePlage(sorties, seg, d, f)
	if err != nil {
		return nil, err
	}
	if Voies(sorties, seg) == 2 {
		data = ExtraireVoie(data, min(max(voie, 0), 1))
	}
	return append(EnteteWAV(uint32(len(data)), 1), data...), nil
}
