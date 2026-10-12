package main

// Transcription locale de l'enregistrement avec whisper.cpp (whisper-cli, installé sur le poste) :
// rien ne quitte l'ordinateur. Le modèle est téléchargé une fois dans le cache de l'utilisateur.
// Chaque segment donne audio/<segment>.json ; sorties/transcription.md les assemble, découpée
// par question ou par page grâce aux repères posés par la page pendant l'enregistrement.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
)

// Modèles proposés (dépôt HuggingFace de whisper.cpp). Sur CPU, l'encodeur du modèle « large »
// demande ~1 min 30 par tranche de 30 s : trop lent pour un atelier de 2 h. On prend donc
// « small » (~15 s par tranche), sauf sur Mac Apple Silicon où whisper.cpp calcule sur le GPU.
type modeleWhisper struct {
	Nom, Somme, Depot string
	Taille            int64
}

const depotWhisper = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"

var modelesWhisper = map[string]modeleWhisper{
	"small": {"ggml-small-q5_1.bin", "ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb", depotWhisper, 190085487},
	"large": {"ggml-large-v3-turbo-q5_0.bin", "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2", depotWhisper, 574041195},
}

// Détection de la voix (Silero, < 1 Mo) : whisper-cli saute les silences. C'est deux fois plus
// rapide sur un atelier réel, et Whisper n'invente plus de texte dans les blancs.
var modeleVAD = modeleWhisper{"ggml-silero-v6.2.0.bin", "2aa269b785eeb53a82983a20501ddf7c1d9c48e33ab63a41391ac6c9f7fb6987",
	"https://huggingface.co/ggml-org/whisper-vad/resolve/main/", 885098}

// cheminWhisper : GREFFIER_WHISPER, sinon le PATH, sinon à côté de Greffier ou aux emplacements habituels.
func cheminWhisper() string {
	exe := "whisper-cli"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	var candidats []string
	if p, err := executable(); err == nil {
		d := filepath.Dir(p)
		candidats = append(candidats, filepath.Join(d, "whisper", exe), filepath.Join(d, exe))
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		candidats = append(candidats, filepath.Join(os.Getenv("LOCALAPPDATA"), "whisper", exe))
	} else {
		candidats = append(candidats, "/opt/homebrew/bin/"+exe, "/usr/local/bin/"+exe, filepath.Join(home, ".local", "bin", exe))
	}
	return trouverOutil("GREFFIER_WHISPER", []string{"whisper-cli"}, candidats)
}

// choixModele : GREFFIER_MODELE_WHISPER = « small », « large » ou le chemin d'un modèle ggml déjà
// présent ; sinon « large » sur Mac Apple Silicon et « small » ailleurs.
func choixModele() (m modeleWhisper, chemin string, err error) {
	choix := os.Getenv("GREFFIER_MODELE_WHISPER")
	if choix == "" {
		choix = "small"
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			choix = "large"
		}
	}
	m, connu := modelesWhisper[choix]
	if !connu {
		return m, choix, nil // chemin fourni : rien à télécharger
	}
	chemin, err = cheminCache(m)
	return m, chemin, err
}

func cheminCache(m modeleWhisper) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "greffier", "whisper", m.Nom), nil
}

// assurerModele : le modèle de transcription, téléchargé s'il manque.
func assurerModele(ctx context.Context, progres func(pourcent int)) (string, error) {
	m, chemin, err := choixModele()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(chemin); err == nil {
		return chemin, nil
	} else if m.Nom == "" {
		return "", fmt.Errorf("modèle introuvable (GREFFIER_MODELE_WHISPER) : %s", chemin)
	}
	return chemin, telechargerModele(ctx, m, chemin, progres)
}

// telechargerModele écrit le modèle m dans chemin, en vérifiant son SHA-256.
func telechargerModele(ctx context.Context, m modeleWhisper, chemin string, progres func(int)) error {
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		return err
	}
	logf("Téléchargement du modèle %s (%d Mo, une seule fois)…", m.Nom, max(m.Taille>>20, 1))
	r, err := requete(ctx, m.Depot+m.Nom)
	if err != nil {
		return fmt.Errorf("téléchargement du modèle : %w", err)
	}
	defer r.Body.Close()
	part := chemin + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h, &compteur{total: m.Taille, progres: progres}), io.LimitReader(r.Body, m.Taille+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != m.Somme {
		err = errors.New("le modèle téléchargé ne correspond pas à la somme de contrôle attendue")
	}
	if err != nil {
		os.Remove(part)
		return fmt.Errorf("téléchargement du modèle : %w", err)
	}
	return os.Rename(part, chemin)
}

// assurerVAD : le modèle de détection de la voix, si whisper-cli sait s'en servir (versions
// récentes) ; "" sinon, et la transcription se fait sans sauter les silences.
func assurerVAD(ctx context.Context, whisper string) string {
	aide, _ := exec.CommandContext(ctx, whisper, "--help").CombinedOutput()
	if !strings.Contains(string(aide), "--vad-model") {
		return ""
	}
	chemin, err := cheminCache(modeleVAD)
	if err != nil {
		return ""
	}
	if _, err := os.Stat(chemin); err == nil {
		return chemin
	}
	if err := telechargerModele(ctx, modeleVAD, chemin, func(int) {}); err != nil {
		logf("⚠ %v : transcription sans détection des silences", err)
		return ""
	}
	return chemin
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

var reProgres = regexp.MustCompile(`progress\s*=\s*(\d+)%`)

// GREFFIER_SANS_TRANSCRIPTION_DIRECTE=1 : ne transcrire qu'à la demande (bouton « Transcrire »),
// par exemple sur un poste trop lent pour transcrire pendant l'atelier.
var transcriptionDirecteDesactivee = os.Getenv("GREFFIER_SANS_TRANSCRIPTION_DIRECTE") != ""

// Un long passage sur la même question est coupé toutes les 2 min, pour être transcrit pendant
// que l'enregistrement continue.
const dureeMaxMorceau = 120 * frequenceAudio

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
		c := int(t * frequenceAudio)
		if c-coupes[len(coupes)-1] >= frequenceAudio/2 && n-c >= frequenceAudio/2 { // morceaux d'au moins 0,5 s
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

// Audio d'un segment : les données du .wav finalisé, suivies du .pcm en cours d'écriture
// (les deux n'existent ensemble qu'après une reprise sur plantage).
func fichiersAudio(sorties, seg string) [2]struct {
	chemin string
	entete int64
} {
	base := filepath.Join(dossierAudio(sorties), seg)
	return [2]struct {
		chemin string
		entete int64
	}{{base + ".wav", tailleEnteteWAV}, {base + ".pcm", 0}}
}

// echantillons : nombre d'échantillons enregistrés dans le segment.
func echantillons(sorties, seg string) int {
	var n int64
	for _, f := range fichiersAudio(sorties, seg) {
		if st, err := os.Stat(f.chemin); err == nil {
			n += max(st.Size()-f.entete, 0)
		}
	}
	return int(n / 2)
}

// lirePlage : les échantillons [d, f) du segment, sans relire tout l'enregistrement.
func lirePlage(sorties, seg string, d, f int) ([]byte, error) {
	out := make([]byte, 0, (f-d)*2)
	off, reste := int64(d)*2, int64(f-d)*2
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

// Les morceaux transcrits sont gardés dans audio/<seg>.morceaux/<début>-<fin>.json
// (en échantillons) : rien n'est transcrit deux fois.
func cheminMorceau(sorties, seg string, b [2]int) string {
	return filepath.Join(dossierAudio(sorties), seg+".morceaux", fmt.Sprintf("%d-%d", b[0], b[1]))
}

// transcrireMorceau passe un morceau du segment à whisper-cli (sauf s'il est déjà transcrit).
func (t *transcripteur) transcrireMorceau(ctx context.Context, whisper, modele, vad string, threads int, sorties, seg string, b [2]int, progres func(int)) error {
	f := cheminMorceau(sorties, seg, b)
	if _, err := os.Stat(f + ".json"); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	deverrouiller := t.verrou()
	donnees, err := lirePlage(sorties, seg, b[0], b[1])
	deverrouiller()
	if err != nil {
		return err
	}
	if err := os.WriteFile(f+".wav", append(enteteWAV(uint32(len(donnees))), donnees...), 0o644); err != nil {
		return err
	}
	defer os.Remove(f + ".wav")
	tmp := f + ".en-cours"
	args := []string{"-m", modele, "-f", f + ".wav", "-l", "fr", "-t", strconv.Itoa(threads), "-pp", "-oj", "-of", tmp}
	if vad != "" {
		args = append(args, "--vad", "-vm", vad)
	}
	if err := lancerWhisper(ctx, whisper, args, progres); err != nil {
		os.Remove(tmp + ".json")
		return err
	}
	return os.Rename(tmp+".json", f+".json")
}

// assemblerSegment réunit les morceaux transcrits d'un segment terminé dans audio/<seg>.json
// (format de whisper-cli, décalages en ms depuis le début du segment).
func assemblerSegment(sorties, seg string, bornes [][2]int) error {
	type sortie struct {
		Offsets struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
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
		debut, fin := b[0]*1000/frequenceAudio, b[1]*1000/frequenceAudio
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
	return writeAtomic(filepath.Join(dossierAudio(sorties), seg+".json"), res)
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

// ---------------------------------------------------------------- assemblage

type morceau struct {
	DebutMs, FinMs int
	Texte          string
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
		} `json:"transcription"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("%s : %w", filepath.Base(chemin), err)
	}
	var ms []morceau
	for _, t := range w.Transcription {
		ms = append(ms, morceau{DebutMs: t.Offsets.From, FinMs: t.Offsets.To, Texte: t.Text})
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

// transcriptMarkdown assemble les segments en un document découpé par question ou page.
// titres : identifiant → intitulé (question, ou titre de la page libre).
func transcriptMarkdown(segs []segmentTranscrit, reperes []repere, titres map[string]string) string {
	var b strings.Builder
	b.WriteString("# Transcription de l'enregistrement\n\n")
	b.WriteString("_Transcription automatique (whisper.cpp), non relue : erreurs de reconnaissance possibles, intervenants non identifiés. Horodatage à l'heure du poste._\n")
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

		courant, paragraphe, dernier := "\x00", []string{}, ""
		debutPara, finPara := 0, 0
		vider := func() {
			if len(paragraphe) > 0 {
				fmt.Fprintf(&b, "\n[%s] %s\n", horodatage(debut, okDebut, debutPara), strings.Join(paragraphe, " "))
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
			if len(paragraphe) > 0 && (m.DebutMs-finPara > 1500 || m.DebutMs-debutPara > 45000) {
				vider()
			}
			if len(paragraphe) == 0 {
				debutPara = m.DebutMs
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
	if a, _, err := LoadAtelier(dir); err == nil {
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
	for _, s := range listerSegments(sorties) {
		if !s.Transcrit {
			continue
		}
		ms, err := lireSortieWhisper(filepath.Join(dossierAudio(sorties), s.ID+".json"))
		if err != nil {
			return "", err
		}
		segs = append(segs, segmentTranscrit{ID: s.ID, Morceaux: ms})
	}
	if len(segs) == 0 {
		return "", errors.New("aucun enregistrement transcrit")
	}
	chemin := filepath.Join(sorties, "transcription.md")
	return chemin, writeAtomic(chemin, []byte(transcriptMarkdown(segs, session.Audio.Reperes, titres)))
}

// ---------------------------------------------------------------- tâche de fond

// EtatTranscription : réponse de api/audio pendant et après une transcription.
type EtatTranscription struct {
	EnCours  bool   `json:"enCours"`
	Etape    string `json:"etape,omitempty"`
	Pourcent int    `json:"pourcent"`
	Erreur   string `json:"erreur,omitempty"`
}

// transcripteur : une transcription à la fois (elle occupe le processeur). Démarrée par le
// premier morceau d'audio reçu, elle suit l'enregistrement : chaque question terminée (ou toutes
// les 2 min) est transcrite pendant l'atelier, avec moins de threads pour ne pas gêner le poste.
// Quand plus rien ne s'enregistre, elle transcrit le reste et écrit transcription.md.
type transcripteur struct {
	mu      sync.Mutex
	dossier string
	etat    EtatTranscription
	disque  *sync.Mutex // verrou des écritures sur disque du serveur (ajouts d'audio, finalisation)
	reveil  chan struct{}
}

// Reveiller : un enregistrement vient d'être mis en pause, inutile d'attendre la prochaine vérification.
func (t *transcripteur) Reveiller() {
	select {
	case t.canalReveil() <- struct{}{}:
	default: // un réveil est déjà en attente
	}
}

func (t *transcripteur) canalReveil() chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reveil == nil {
		t.reveil = make(chan struct{}, 1)
	}
	return t.reveil
}

func (t *transcripteur) Etat(dir string) EtatTranscription {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dossier != dir {
		return EtatTranscription{}
	}
	return t.etat
}

func (t *transcripteur) occupe() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.etat.EnCours
}

func (t *transcripteur) maj(f func(e *EtatTranscription)) {
	t.mu.Lock()
	f(&t.etat)
	t.mu.Unlock()
}

func (t *transcripteur) verrou() func() {
	if t.disque == nil {
		return func() {}
	}
	t.disque.Lock()
	return t.disque.Unlock
}

// Lancer démarre la transcription de l'atelier dir (sans effet si elle tourne déjà pour lui).
// arret : fermé à l'arrêt de l'application, qui interrompt whisper-cli.
func (t *transcripteur) Lancer(dir string, arret <-chan struct{}) error {
	whisper := cheminWhisper()
	if whisper == "" {
		return errors.New("whisper-cli introuvable : installer whisper.cpp (voir le README)")
	}
	t.mu.Lock()
	if t.etat.EnCours {
		autre := t.dossier != dir
		t.mu.Unlock()
		if autre {
			return errors.New("la transcription d'un autre atelier est en cours")
		}
		return nil
	}
	t.dossier, t.etat = dir, EtatTranscription{EnCours: true, Etape: "Préparation"}
	t.mu.Unlock()

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
		err := t.executer(ctx, dir, whisper)
		t.maj(func(e *EtatTranscription) {
			*e = EtatTranscription{Etape: "Transcription terminée", Pourcent: 100}
			if err != nil {
				*e = EtatTranscription{Erreur: err.Error()}
			}
		})
		if err != nil {
			logf("⚠ Transcription : %v", err)
		} else {
			logf("Transcription terminée (%s après son démarrage) : %s", time.Since(debut).Round(time.Second), filepath.Join(dir, "sorties", "transcription.md"))
		}
	}()
	return nil
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

func (t *transcripteur) executer(ctx context.Context, dir, whisper string) error {
	sorties := filepath.Join(dir, "sorties")
	t.maj(func(e *EtatTranscription) { e.Etape = "Modèle de transcription" })
	modele, err := assurerModele(ctx, func(p int) {
		t.maj(func(e *EtatTranscription) {
			e.Etape, e.Pourcent = "Téléchargement du modèle de transcription (une seule fois)", p
		})
	})
	if err != nil {
		return err
	}
	t.maj(func(e *EtatTranscription) { e.Etape, e.Pourcent = "Détection des silences", 0 })
	vad := assurerVAD(ctx, whisper)
	threadsDirect := max(1, min(runtime.NumCPU()/2, 6))
	threads := min(runtime.NumCPU(), 8)

	for {
		reperes := lireSessionAudio(sorties).Audio.Reperes
		segments := listerSegments(sorties)
		enDirect := false
		total, fait := 0.0, 0.0
		type tache struct {
			seg    infoSegment
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
				err = finaliserWAV(sorties, sg.ID)
			}
			n := echantillons(sorties, sg.ID)
			deverrouiller()
			if err != nil {
				return err
			}
			bornes := morceauxAudio(n, reperes, sg.ID, !sg.EnCours)
			for _, b := range bornes {
				if _, err := os.Stat(cheminMorceau(sorties, sg.ID, b) + ".json"); err == nil {
					fait += float64(b[1]-b[0]) / frequenceAudio
				}
			}
			taches = append(taches, tache{sg, bornes})
		}
		libelle := "Transcription"
		if enDirect {
			libelle = "Transcription au fil de l'enregistrement"
		}
		etat := func(f float64) {
			t.maj(func(e *EtatTranscription) {
				e.Etape = fmt.Sprintf("%s : %s sur %s", libelle, dureeLisible(f), dureeLisible(total))
				e.Pourcent = int(f * 100 / max(total, 1))
			})
		}
		etat(fait)
		for _, ta := range taches {
			n := threads
			if enDirect {
				n = threadsDirect
			}
			for _, b := range ta.bornes {
				if _, err := os.Stat(cheminMorceau(sorties, ta.seg.ID, b) + ".json"); err == nil {
					continue
				}
				d := float64(b[1]-b[0]) / frequenceAudio
				err := t.transcrireMorceau(ctx, whisper, modele, vad, n, sorties, ta.seg.ID, b, func(p int) { etat(fait + d*float64(p)/100) })
				if err != nil {
					return err
				}
				fait += d
				etat(fait)
			}
			if !ta.seg.EnCours {
				if err := assemblerSegment(sorties, ta.seg.ID, ta.bornes); err != nil {
					return err
				}
			}
		}
		if !enDirect {
			break
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
