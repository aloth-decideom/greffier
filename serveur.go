package main

// Serveur local (127.0.0.1 uniquement) :
//   /                       page de choix de l'atelier (lanceur)
//   /api/ateliers           ateliers récents et trouvés à côté de l'application
//   /api/parcourir          sélecteur de dossier natif du système
//   /api/ouvrir             prépare un atelier et renvoie l'adresse de sa page
//   /api/notes-libres       crée un dossier de notes libres (sans questions) et l'ouvre
//   /api/montrer            ouvre le dossier d'un atelier de la liste dans l'explorateur de fichiers
//   /api/oublier            retire un atelier des récents (le dossier reste sur le disque)
//   /api/supprimer          supprime définitivement le dossier d'un atelier de la liste
//   /api/son-visio          Linux : source « Monitor of … » de la sortie audio, pour capter une visio
//   /api/version            version courante et éventuelle nouvelle version sur GitHub
//   /api/mettre-a-jour      installe la nouvelle version puis redémarre (internal/maj)
//   /a/<slug>/…             page d'atelier (fichiers intégrés + questions.js lu en direct)
//   /a/<slug>/api/…         sauvegarde sur disque dans <atelier>/sorties/
//   /a/<slug>/api/cr-claude compte rendu rédigé par Claude Code (internal/compterendu)
//   /a/<slug>/api/questions questions et en-tête modifiés dans la page (questions.md réécrit)
//   /a/<slug>/api/audio…    enregistrement du son (internal/audio) ; api/transcrire : transcription locale (internal/transcription)
//   /a/<slug>/api/voix      séparation des voix après la réunion, noms donnés à l'écoute (internal/transcription)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aloth-decideom/greffier/internal/atelier"
	"github.com/aloth-decideom/greffier/internal/maj"
	"github.com/aloth-decideom/greffier/internal/systeme"
	"github.com/aloth-decideom/greffier/internal/transcription"
	"github.com/aloth-decideom/greffier/ressources"
)

const maxBody = 30 << 20

var rePhoto = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,120}\.(jpg|jpeg|png)$`)

type Serveur struct {
	origin     string
	mu         sync.Mutex        // une écriture disque à la fois
	ateliers   map[string]string // slug → dossier
	amu        sync.Mutex
	dernier    [2]string          // dernier atelier ouvert : adresse de sa page, titre (icône de notification)
	ouverture  func(titre string) // prévient l'icône de notification qu'un atelier est ouvert
	Arret      chan struct{}      // fermé quand la page ou l'icône demande l'arrêt de l'application
	arretOnce  sync.Once
	Maj        *maj.Verificateur
	majEnCours atomic.Bool
	crEnCours  atomic.Bool // une rédaction par Claude à la fois
	Transcr    transcription.Transcripteur
	Voix       transcription.Separateur
	Relance    atomic.Bool // nouvelle version installée : main la lance après l'arrêt
}

func NewServeur(port int) *Serveur {
	s := &Serveur{origin: fmt.Sprintf("http://127.0.0.1:%d", port), ateliers: map[string]string{}, Arret: make(chan struct{}),
		Maj: maj.NouveauVerificateur(version)}
	s.Transcr.Disque = &s.mu
	s.Voix.Disque = &s.mu
	return s
}

func (s *Serveur) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.lanceur)
	mux.HandleFunc("/api/ateliers", s.listeAteliers)
	mux.HandleFunc("/api/parcourir", s.parcourir)
	mux.HandleFunc("/api/ouvrir", s.ouvrir)
	mux.HandleFunc("/api/arreter", s.arreter)
	mux.HandleFunc("/api/notes-libres", s.notesLibres)
	mux.HandleFunc("/api/montrer", s.montrer)
	mux.HandleFunc("/api/oublier", s.oublier)
	mux.HandleFunc("/api/supprimer", s.supprimer)
	mux.HandleFunc("/api/son-visio", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"moniteur": systeme.MoniteurSortieAudio()})
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.Maj.Info()) })
	mux.HandleFunc("/api/mettre-a-jour", s.majHTTP)
	mux.HandleFunc("/a/", s.atelier)
	mux.HandleFunc("/static/", s.statique) // charte, polices, logo pour la page de choix
	return mux
}

// ---------------------------------------------------------------- utilitaires

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func erreur(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"erreur": msg})
}

func repondreOK(w http.ResponseWriter) { writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) }

// postAutorise : requête POST venue d'une page de Greffier ; sinon répond 403 et renvoie false.
func (s *Serveur) postAutorise(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost || !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "refusé")
		return false
	}
	return true
}

// servirRessource : fichier intégré app/<nom> (page, charte, polices, logos).
func servirRessource(w http.ResponseWriter, r *http.Request, nom string) {
	data, err := fs.ReadFile(ressources.FS, "app/"+nom)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if t := mime.TypeByExtension(path.Ext(nom)); t != "" {
		w.Header().Set("Content-Type", t)
	}
	_, _ = w.Write(data)
}

// Empêche une autre page web ouverte dans le navigateur d'écrire via ce serveur.
func (s *Serveur) memeOrigine(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || o == s.origin || o == strings.Replace(s.origin, "127.0.0.1", "localhost", 1)
}

// ---------------------------------------------------------------- lanceur

func (s *Serveur) statique(w http.ResponseWriter, r *http.Request) {
	servirRessource(w, r, strings.TrimPrefix(path.Clean(r.URL.Path), "/static/"))
}

func (s *Serveur) lanceur(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, _ := ressources.Lire("lanceur.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

type infoAtelier struct {
	Chemin    string `json:"chemin"`
	Nom       string `json:"nom"`
	Titre     string `json:"titre"`
	Client    string `json:"client"`
	Questions int    `json:"questions"`
	Erreur    string `json:"erreur,omitempty"`
	Recent    bool   `json:"recent"`
	Mode      string `json:"mode,omitempty"`
}

func decrire(dir string, recent bool) infoAtelier {
	info := infoAtelier{Chemin: dir, Nom: filepath.Base(dir), Recent: recent}
	a, _, err := atelier.Charger(dir)
	if err != nil {
		info.Erreur = err.Error()
		return info
	}
	info.Titre, info.Client, info.Mode = a.Meta["atelier"], a.Meta["client"], a.Mode
	_, info.Questions = a.QuestionCount()
	return info
}

// ateliersConnus : les récents (encore présents), puis les ateliers trouvés à proximité.
func ateliersConnus() (recents, trouves []string) {
	vus := map[string]bool{}
	for _, d := range systeme.ChargerRecents() {
		if _, err := os.Stat(filepath.Join(d, "questions.md")); err == nil && !vus[d] {
			vus[d] = true
			recents = append(recents, d)
		}
	}
	for _, base := range systeme.DossiersDeRecherche() {
		entries, _ := os.ReadDir(base)
		for _, e := range entries {
			d := filepath.Join(base, e.Name())
			if e.IsDir() && !vus[d] {
				if _, err := os.Stat(filepath.Join(d, "questions.md")); err == nil {
					vus[d] = true
					trouves = append(trouves, d)
				}
			}
		}
	}
	sort.Strings(trouves)
	return recents, trouves
}

func (s *Serveur) listeAteliers(w http.ResponseWriter, r *http.Request) {
	recents, trouves := ateliersConnus()
	liste := []infoAtelier{}
	for _, d := range recents {
		liste = append(liste, decrire(d, true))
	}
	for _, d := range trouves {
		liste = append(liste, decrire(d, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ateliers": liste, "systeme": systeme.NomSysteme(), "dossierNotes": systeme.DossierNotesParDefaut()})
}

// atelierDeLaListe lit le chemin envoyé par la page et vérifie qu'il s'agit bien d'un atelier
// affiché dans la liste : les actions du menu « ⋯ » ne touchent à rien d'autre.
func (s *Serveur) atelierDeLaListe(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.postAutorise(w, r) {
		return "", false
	}
	var req struct{ Chemin string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || req.Chemin == "" {
		erreur(w, http.StatusBadRequest, "chemin manquant")
		return "", false
	}
	recents, trouves := ateliersConnus()
	for _, d := range append(recents, trouves...) {
		if d == req.Chemin {
			return d, true
		}
	}
	erreur(w, http.StatusOK, "cet atelier n'est plus dans la liste : recharge la page")
	return "", false
}

func (s *Serveur) montrer(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.atelierDeLaListe(w, r)
	if !ok {
		return
	}
	if err := systeme.OuvrirDossier(dir); err != nil {
		erreur(w, http.StatusOK, "impossible d'ouvrir le dossier : "+err.Error())
		return
	}
	repondreOK(w)
}

func (s *Serveur) oublier(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.atelierDeLaListe(w, r)
	if !ok {
		return
	}
	if err := systeme.RetirerRecent(dir); err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	repondreOK(w)
}

func (s *Serveur) supprimer(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.atelierDeLaListe(w, r)
	if !ok {
		return
	}
	// Garde-fou : jamais un dossier racine, le dossier personnel ou un dossier où l'on cherche les ateliers.
	home, _ := os.UserHomeDir()
	interdits := append(systeme.DossiersDeRecherche(), home)
	for _, x := range interdits {
		if x != "" && filepath.Clean(x) == filepath.Clean(dir) {
			erreur(w, http.StatusOK, "suppression refusée pour ce dossier : "+dir)
			return
		}
	}
	if filepath.Dir(dir) == dir {
		erreur(w, http.StatusOK, "suppression refusée pour ce dossier : "+dir)
		return
	}
	s.mu.Lock() // pas pendant une écriture de notes
	err := os.RemoveAll(dir)
	s.mu.Unlock()
	s.amu.Lock()
	for slug, d := range s.ateliers {
		if d == dir {
			delete(s.ateliers, slug) // une page encore ouverte ne recrée pas le dossier
			if s.dernier[0] == "/a/"+slug+"/atelier.html" {
				s.dernier = [2]string{} // l'icône de notification ramène à l'accueil
			}
		}
	}
	s.amu.Unlock()
	_ = systeme.RetirerRecent(dir)
	if err != nil {
		erreur(w, http.StatusOK, "suppression incomplète : "+err.Error())
		return
	}
	systeme.Logf("Atelier supprimé : %s", dir)
	repondreOK(w)
}

func (s *Serveur) parcourir(w http.ResponseWriter, r *http.Request) {
	if !s.postAutorise(w, r) {
		return
	}
	var req struct{ Notes bool } // notes libres : choix du dossier où les créer
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
	invite := "Choisir le dossier de l'atelier (celui qui contient questions.md)"
	if req.Notes {
		invite = "Choisir où créer les notes libres"
	}
	dir, err := systeme.ChoisirDossier(invite, req.Notes)
	if err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"chemin": dir})
}

func (s *Serveur) ouvrir(w http.ResponseWriter, r *http.Request) {
	if !s.postAutorise(w, r) {
		return
	}
	var req struct {
		Chemin string `json:"chemin"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || strings.TrimSpace(req.Chemin) == "" {
		erreur(w, http.StatusBadRequest, "chemin manquant")
		return
	}
	url, notes, err := s.preparer(strings.Trim(strings.TrimSpace(req.Chemin), `"`))
	if err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "notes": notes})
}

// notesLibres : crée <base>/<date>-<titre>/ (base par défaut : Documents/Greffier) puis l'ouvre.
func (s *Serveur) notesLibres(w http.ResponseWriter, r *http.Request) {
	if !s.postAutorise(w, r) {
		return
	}
	var req struct{ Titre, Client, Base string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		erreur(w, http.StatusBadRequest, "données illisibles")
		return
	}
	base := strings.Trim(strings.TrimSpace(req.Base), `"`)
	if base == "" {
		base = systeme.DossierNotesParDefaut()
	}
	dir, err := atelier.CreerNotesLibres(base, req.Titre, req.Client, time.Now())
	if err != nil {
		erreur(w, http.StatusOK, "création impossible : "+err.Error())
		return
	}
	url, notes, err := s.preparer(dir)
	if err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "notes": notes, "chemin": dir})
}

// arreter : bouton « Arrêter l'application » des pages. L'arrêt effectif (qui laisse finir
// les écritures en cours) est fait par main à la fermeture du canal Arret.
func (s *Serveur) arreter(w http.ResponseWriter, r *http.Request) {
	if !s.postAutorise(w, r) {
		return
	}
	repondreOK(w)
	s.demanderArret()
}

func (s *Serveur) demanderArret() { s.arretOnce.Do(func() { close(s.Arret) }) }

func (s *Serveur) majHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.postAutorise(w, r) {
		return
	}
	if err := s.mettreAJour(); err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	repondreOK(w) // l'arrêt attend la fin de cette réponse
}

var errMajEnCours = errors.New("mise à jour déjà en cours")

// mettreAJour installe la nouvelle version puis arrête l'application, que main relance.
func (s *Serveur) mettreAJour() error {
	if !s.majEnCours.CompareAndSwap(false, true) {
		return errMajEnCours
	}
	info := s.Maj.Info()
	if !info.Auto {
		s.majEnCours.Store(false)
		return errors.New("mise à jour automatique impossible ici : télécharger la nouvelle version")
	}
	systeme.Logf("Mise à jour vers %s : téléchargement…", info.Derniere)
	exe, err := systeme.Executable()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		err = maj.Installer(ctx, info, exe)
		cancel()
	}
	if err != nil {
		s.Maj.SansAuto() // ensuite, seulement le téléchargement à la main
		s.majEnCours.Store(false)
		systeme.Logf("⚠ Mise à jour impossible : %v", err)
		return err
	}
	systeme.Logf("Greffier %s installé : redémarrage.", info.Derniere)
	s.Relance.Store(true)
	s.demanderArret()
	return nil
}

// adressePage : la page du dernier atelier ouvert, sinon l'accueil.
func (s *Serveur) adressePage() string {
	s.amu.Lock()
	defer s.amu.Unlock()
	if s.dernier[0] != "" {
		return s.origin + s.dernier[0]
	}
	return s.origin + "/"
}

// siOuverture : f est appelée à chaque ouverture d'atelier (et tout de suite s'il y en a déjà un).
func (s *Serveur) siOuverture(f func(titre string)) {
	s.amu.Lock()
	s.ouverture = f
	titre := s.dernier[1]
	s.amu.Unlock()
	if titre != "" {
		f(titre)
	}
}

// preparer : vérifie l'atelier, génère ses fichiers, le mémorise et renvoie l'adresse de sa page.
func (s *Serveur) preparer(dir string) (string, []string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", nil, fmt.Errorf("dossier introuvable : %s", abs)
	}
	a, warnings, err := atelier.Charger(abs)
	if err != nil {
		return "", nil, err
	}
	notes, err := a.Generate(abs)
	if err != nil {
		return "", nil, fmt.Errorf("écriture impossible dans %s : %v", abs, err)
	}
	url := "/a/" + a.Slug + "/atelier.html"
	titre := a.Meta["atelier"]
	if titre == "" {
		titre = filepath.Base(abs)
	}
	s.amu.Lock()
	s.ateliers[a.Slug] = abs
	s.dernier = [2]string{url, titre}
	ouverture := s.ouverture
	s.amu.Unlock()
	if ouverture != nil {
		ouverture(titre)
	}
	systeme.AjouterRecent(abs)
	_, nq := a.QuestionCount()
	quoi := fmt.Sprintf("%d questions", nq)
	if a.Mode == "libre" {
		quoi = "notes libres"
	}
	systeme.Logf("Atelier ouvert : %s (%s) — sauvegarde dans %s", abs, quoi, filepath.Join(abs, "sorties"))
	return url, append(warnings, notes...), nil
}

// dossierDe retrouve le dossier d'un atelier (y compris après un redémarrage, via les récents).
func (s *Serveur) dossierDe(slug string) string {
	s.amu.Lock()
	dir, ok := s.ateliers[slug]
	s.amu.Unlock()
	if ok {
		return dir
	}
	for _, d := range systeme.ChargerRecents() {
		if a, _, err := atelier.Charger(d); err == nil && a.Slug == slug {
			s.amu.Lock()
			s.ateliers[slug] = d
			s.amu.Unlock()
			return d
		}
	}
	return ""
}
