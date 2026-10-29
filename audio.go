package main

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
)

const (
	frequenceAudio   = 16000
	octetsParSeconde = frequenceAudio * 2 // 16 bits, par voie
	tailleEnteteWAV  = 44
)

// Segment : seg-AAAAMMJJ-HHMMSS (heure de début, locale au poste qui enregistre).
var reSegment = regexp.MustCompile(`^seg-\d{8}-\d{6}$`)

type infoSegment struct {
	ID        string  `json:"id"`
	Secondes  float64 `json:"secondes"`
	Transcrit bool    `json:"transcrit"`
	EnCours   bool    `json:"enCours"` // .pcm écrit il y a peu : enregistrement en cours
	modifie   time.Time
	aPCM      bool
}

// Délai sans écriture au-delà duquel un .pcm est considéré comme abandonné (plantage, onglet fermé).
const delaiSegmentActif = 20 * time.Second

func dossierAudio(sorties string) string { return filepath.Join(sorties, "audio") }

// voiesSegment : 2 si le segment est en stéréo (micro + visio), 1 sinon.
func voiesSegment(sorties, seg string) int {
	if _, err := os.Stat(filepath.Join(dossierAudio(sorties), seg+".stereo")); err == nil {
		return 2
	}
	return 1
}

// ajouterAudio ajoute un morceau de PCM (voies : 1, ou 2 entrelacées) à la fin du segment.
func ajouterAudio(sorties, seg string, data []byte, voies int) error {
	if !reSegment.MatchString(seg) {
		return errors.New("nom de segment invalide")
	}
	if voies != 1 && voies != 2 {
		return errors.New("nombre de voies invalide")
	}
	if len(data)%(2*voies) != 0 {
		return errors.New("morceau audio incomplet")
	}
	dir := dossierAudio(sorties)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if echantillons(sorties, seg) == 0 {
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
	} else if voiesSegment(sorties, seg) != voies {
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

// enteteWAV : en-tête RIFF d'un fichier PCM 16 bits à 16 kHz (voies : 1 mono, 2 stéréo) de n octets de données.
func enteteWAV(n uint32, voies int) []byte {
	h := make([]byte, 0, tailleEnteteWAV)
	le := binary.LittleEndian
	h = append(h, "RIFF"...)
	h = le.AppendUint32(h, 36+n)
	h = append(h, "WAVEfmt "...)
	h = le.AppendUint32(h, 16)                             // taille du bloc fmt
	h = le.AppendUint16(h, 1)                              // PCM
	h = le.AppendUint16(h, uint16(voies))                  // voies
	h = le.AppendUint32(h, frequenceAudio)                 // échantillons par seconde
	h = le.AppendUint32(h, uint32(octetsParSeconde*voies)) // octets par seconde
	h = le.AppendUint16(h, uint16(2*voies))                // octets par échantillon (toutes voies)
	h = le.AppendUint16(h, 16)                             // bits par échantillon
	h = append(h, "data"...)
	return le.AppendUint32(h, n)
}

// finaliserWAV transforme <seg>.pcm en <seg>.wav. Si un .wav existe déjà (reprise du même
// segment après un plantage), le PCM est ajouté à la suite de ses données.
func finaliserWAV(sorties, seg string) error {
	if !reSegment.MatchString(seg) {
		return errors.New("nom de segment invalide")
	}
	dir := dossierAudio(sorties)
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
	if ancien, err := os.ReadFile(wav); err == nil && len(ancien) >= tailleEnteteWAV {
		donnees = ancien[tailleEnteteWAV:]
	}
	donnees = append(donnees, nouveau...)
	if len(donnees) > 0xFFFFFFFF-36 {
		return fmt.Errorf("segment %s trop long pour un fichier WAV", seg)
	}
	if err := writeAtomic(wav, append(enteteWAV(uint32(len(donnees)), voiesSegment(sorties, seg)), donnees...)); err != nil {
		return err
	}
	return os.Remove(pcm)
}

// listerSegments : les segments enregistrés (.pcm en cours ou .wav), dans l'ordre chronologique.
func listerSegments(sorties string) []infoSegment {
	dir := dossierAudio(sorties)
	entries, _ := os.ReadDir(dir)
	parID := map[string]*infoSegment{}
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
			n -= tailleEnteteWAV
		}
		s := parID[id]
		if s == nil {
			s = &infoSegment{ID: id}
			parID[id] = s
		}
		s.Secondes += float64(max(n, 0)) / octetsParSeconde
		if st.ModTime().After(s.modifie) {
			s.modifie = st.ModTime()
		}
		if ext == ".pcm" {
			s.aPCM = true
			s.EnCours = time.Since(st.ModTime()) < delaiSegmentActif
		}
	}
	liste := []infoSegment{}
	for _, s := range parID {
		s.Secondes /= float64(voiesSegment(sorties, s.ID))
		// transcrit : le .json est plus récent que tout l'audio du segment (sinon, à refaire)
		st, err := os.Stat(filepath.Join(dir, s.ID+".json"))
		s.Transcrit = err == nil && !s.aPCM && !st.ModTime().Before(s.modifie)
		liste = append(liste, *s)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].ID < liste[j].ID })
	return liste
}

// supprimerAudio déplace les enregistrements dans audio/supprimes/ (jamais effacés, comme
// les photos). Les transcriptions (.json) restent en place.
func supprimerAudio(sorties string) error {
	dir := dossierAudio(sorties)
	dest := filepath.Join(dir, "supprimes")
	for _, s := range listerSegments(sorties) {
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
