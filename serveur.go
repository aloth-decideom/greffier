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
//   /api/version            version courante et éventuelle nouvelle version sur GitHub
//   /api/mettre-a-jour      installe la nouvelle version puis redémarre (majauto.go)
//   /a/<slug>/…             page d'atelier (fichiers intégrés + questions.js lu en direct)
//   /a/<slug>/api/…         sauvegarde sur disque dans <atelier>/sorties/
//   /a/<slug>/api/cr-claude compte rendu rédigé par Claude Code (ia.go)
//   /a/<slug>/api/audio…    enregistrement du son (audio.go) ; api/transcrire : transcription locale (transcription.go)

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
	Maj        verificateurMaj
	majEnCours atomic.Bool
	crEnCours  atomic.Bool // une rédaction par Claude à la fois
	Transcr    transcripteur
	Relance    atomic.Bool // nouvelle version installée : main la lance après l'arrêt
}

func NewServeur(port int) *Serveur {
	s := &Serveur{origin: fmt.Sprintf("http://127.0.0.1:%d", port), ateliers: map[string]string{}, Arret: make(chan struct{}),
		Maj: verificateurMaj{info: InfoVersion{Version: version}}}
	s.Transcr.disque = &s.mu
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

// Empêche une autre page web ouverte dans le navigateur d'écrire via ce serveur.
func (s *Serveur) memeOrigine(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || o == s.origin || o == strings.Replace(s.origin, "127.0.0.1", "localhost", 1)
}

// Écrit dans un fichier temporaire puis remplace : jamais de fichier à moitié écrit.
func writeAtomic(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

// ---------------------------------------------------------------- lanceur

func (s *Serveur) statique(w http.ResponseWriter, r *http.Request) {
	clean, ok := strings.CutPrefix(path.Clean(r.URL.Path), "/static/")
	data, err := fs.ReadFile(embedded, "app/"+clean)
	if !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	if t := mime.TypeByExtension(path.Ext(clean)); t != "" {
		w.Header().Set("Content-Type", t)
	}
	_, _ = w.Write(data)
}

func (s *Serveur) lanceur(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, _ := embedded.ReadFile("lanceur.html")
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
	a, _, err := LoadAtelier(dir)
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
	for _, d := range chargerRecents() {
		if _, err := os.Stat(filepath.Join(d, "questions.md")); err == nil && !vus[d] {
			vus[d] = true
			recents = append(recents, d)
		}
	}
	for _, base := range dossiersDeRecherche() {
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
	writeJSON(w, http.StatusOK, map[string]any{"ateliers": liste, "systeme": nomSysteme(), "dossierNotes": dossierNotesParDefaut()})
}

// atelierDeLaListe lit le chemin envoyé par la page et vérifie qu'il s'agit bien d'un atelier
// affiché dans la liste : les actions du menu « ⋯ » ne touchent à rien d'autre.
func (s *Serveur) atelierDeLaListe(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodPost || !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "refusé")
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
	if err := ouvrirDossier(dir); err != nil {
		erreur(w, http.StatusOK, "impossible d'ouvrir le dossier : "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Serveur) oublier(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.atelierDeLaListe(w, r)
	if !ok {
		return
	}
	if err := retirerRecent(dir); err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Serveur) supprimer(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.atelierDeLaListe(w, r)
	if !ok {
		return
	}
	// Garde-fou : jamais un dossier racine, le dossier personnel ou un dossier où l'on cherche les ateliers.
	home, _ := os.UserHomeDir()
	interdits := append(dossiersDeRecherche(), home)
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
	_ = retirerRecent(dir)
	if err != nil {
		erreur(w, http.StatusOK, "suppression incomplète : "+err.Error())
		return
	}
	logf("Atelier supprimé : %s", dir)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Serveur) parcourir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "refusé")
		return
	}
	var req struct{ Notes bool } // notes libres : choix du dossier où les créer
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req)
	invite := "Choisir le dossier de l'atelier (celui qui contient questions.md)"
	if req.Notes {
		invite = "Choisir où créer les notes libres"
	}
	dir, err := choisirDossier(invite, req.Notes)
	if err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"chemin": dir})
}

func (s *Serveur) ouvrir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "refusé")
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
	if r.Method != http.MethodPost || !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "refusé")
		return
	}
	var req struct{ Titre, Client, Base string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		erreur(w, http.StatusBadRequest, "données illisibles")
		return
	}
	base := strings.Trim(strings.TrimSpace(req.Base), `"`)
	if base == "" {
		base = dossierNotesParDefaut()
	}
	dir, err := CreerNotesLibres(base, req.Titre, req.Client, time.Now())
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
	if r.Method != http.MethodPost || !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "refusé")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	s.demanderArret()
}

func (s *Serveur) demanderArret() { s.arretOnce.Do(func() { close(s.Arret) }) }

func (s *Serveur) majHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "refusé")
		return
	}
	if err := s.mettreAJour(); err != nil {
		erreur(w, http.StatusOK, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) // l'arrêt attend la fin de cette réponse
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
	logf("Mise à jour vers %s : téléchargement…", info.Derniere)
	exe, err := executable()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		err = installerMaj(ctx, info, exe)
		cancel()
	}
	if err != nil {
		s.Maj.sansAuto() // ensuite, seulement le téléchargement à la main
		s.majEnCours.Store(false)
		logf("⚠ Mise à jour impossible : %v", err)
		return err
	}
	logf("Greffier %s installé : redémarrage.", info.Derniere)
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
	a, warnings, err := LoadAtelier(abs)
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
	ajouterRecent(abs)
	_, nq := a.QuestionCount()
	quoi := fmt.Sprintf("%d questions", nq)
	if a.Mode == "libre" {
		quoi = "notes libres"
	}
	logf("Atelier ouvert : %s (%s) — sauvegarde dans %s", abs, quoi, filepath.Join(abs, "sorties"))
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
	for _, d := range chargerRecents() {
		if a, _, err := LoadAtelier(d); err == nil && a.Slug == slug {
			s.amu.Lock()
			s.ateliers[slug] = d
			s.amu.Unlock()
			return d
		}
	}
	return ""
}

// ---------------------------------------------------------------- pages et API d'un atelier

func (s *Serveur) atelier(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/a/")
	slug, sub, _ := strings.Cut(rest, "/")
	dir := s.dossierDe(slug)
	if dir == "" {
		http.Redirect(w, r, "/?inconnu="+slug, http.StatusFound)
		return
	}
	if strings.HasPrefix(sub, "api/") {
		s.api(w, r, dir, strings.TrimPrefix(sub, "api/"))
		return
	}
	if sub == "" {
		sub = "atelier.html"
	}
	w.Header().Set("Cache-Control", "no-store")
	if sub == "questions.js" { // lu en direct : modifier questions.md puis recharger la page suffit
		a, _, err := LoadAtelier(dir)
		if err != nil {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			msg, _ := json.Marshal("questions.md : " + err.Error())
			fmt.Fprintf(w, "alert(%s);", msg)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(a.QuestionsJS())
		return
	}
	clean := path.Clean("/" + sub)[1:]
	data, err := fs.ReadFile(embedded, "app/"+clean)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if t := mime.TypeByExtension(path.Ext(clean)); t != "" {
		w.Header().Set("Content-Type", t)
	}
	_, _ = w.Write(data)
}

func (s *Serveur) api(w http.ResponseWriter, r *http.Request, dir, route string) {
	sorties := filepath.Join(dir, "sorties")
	if r.Method != http.MethodGet && !s.memeOrigine(r) {
		erreur(w, http.StatusForbidden, "origine refusée")
		return
	}
	switch {
	case route == "ping":
		writeJSON(w, http.StatusOK, map[string]string{"app": "decideom-atelier", "dossier": sorties})

	case route == "session" && r.Method == http.MethodGet:
		var session any
		if raw, err := os.ReadFile(filepath.Join(sorties, "session.json")); err == nil {
			_ = json.Unmarshal(raw, &session)
		}
		photos := []string{}
		entries, _ := os.ReadDir(filepath.Join(sorties, "photos"))
		for _, e := range entries {
			if !e.IsDir() && rePhoto.MatchString(e.Name()) {
				photos = append(photos, e.Name())
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": session, "photos": photos})

	case route == "save" && r.Method == http.MethodPost:
		var p struct {
			Session        json.RawMessage `json:"session"`
			Markdown       string          `json:"markdown"`
			Historique     any             `json:"historique"`
			HistoriqueSeul bool            `json:"historique_seul"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&p); err != nil || len(p.Session) == 0 {
			erreur(w, http.StatusBadRequest, "données illisibles")
			return
		}
		var pretty any
		_ = json.Unmarshal(p.Session, &pretty)
		session, _ := json.MarshalIndent(pretty, "", " ")
		s.mu.Lock()
		defer s.mu.Unlock()
		if !p.HistoriqueSeul {
			if err := writeAtomic(filepath.Join(sorties, "session.json"), session); err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := writeAtomic(filepath.Join(sorties, "notes.md"), []byte(p.Markdown)); err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if label, ok := p.Historique.(string); ok && label != "" {
			label = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(strings.ToLower(label), "")
			if len(label) > 30 {
				label = label[:30]
			}
			name := fmt.Sprintf("session_%s_%s.json", time.Now().Format("2006-01-02_15h04"), label)
			if err := writeAtomic(filepath.Join(sorties, "historique", name), session); err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "heure": time.Now().Format("15:04:05")})

	case route == "claude" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]bool{"disponible": cheminClaude() != ""})

	case route == "cr-claude" && r.Method == http.MethodPost:
		var p struct{ Texte string }
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&p); err != nil || strings.TrimSpace(p.Texte) == "" {
			erreur(w, http.StatusBadRequest, "données illisibles")
			return
		}
		if !s.crEnCours.CompareAndSwap(false, true) {
			erreur(w, http.StatusOK, "une rédaction est déjà en cours")
			return
		}
		defer s.crEnCours.Store(false)
		logf("Compte rendu : rédaction par Claude en cours…")
		md, err := redigerAvecClaude(r.Context(), sorties, p.Texte)
		if err != nil {
			logf("⚠ Compte rendu : %v", err)
			erreur(w, http.StatusOK, err.Error())
			return
		}
		fichier := filepath.Join(sorties, "CR-"+filepath.Base(dir)+".md")
		s.mu.Lock()
		err = writeAtomic(fichier, []byte(md+"\n"))
		s.mu.Unlock()
		if err != nil {
			erreur(w, http.StatusOK, "compte rendu rédigé mais non enregistré : "+err.Error())
			return
		}
		logf("Compte rendu enregistré : %s", fichier)
		writeJSON(w, http.StatusOK, map[string]string{"markdown": md, "fichier": fichier})

	case route == "audio" && r.Method == http.MethodGet:
		segments := listerSegments(sorties)
		total := 0.0
		for _, sg := range segments {
			total += sg.Secondes
		}
		_, errT := os.Stat(filepath.Join(sorties, "transcription.md"))
		writeJSON(w, http.StatusOK, map[string]any{"segments": segments, "secondes": total, "whisper": cheminWhisper() != "",
			"transcription": errT == nil, "etat": s.Transcr.Etat(dir)})

	case route == "audio" && r.Method == http.MethodDelete:
		if _, err := os.Stat(filepath.Join(sorties, "transcription.md")); err != nil {
			erreur(w, http.StatusOK, "transcrire l'enregistrement avant de supprimer l'audio")
			return
		}
		if s.Transcr.occupe() {
			erreur(w, http.StatusOK, "transcription en cours")
			return
		}
		s.mu.Lock()
		err := supprimerAudio(sorties)
		s.mu.Unlock()
		if err != nil {
			erreur(w, http.StatusOK, err.Error())
			return
		}
		logf("Audio déplacé dans %s", filepath.Join(dossierAudio(sorties), "supprimes"))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case strings.HasPrefix(route, "audio/"):
		seg, fin, _ := strings.Cut(strings.TrimPrefix(route, "audio/"), "/")
		var err error
		switch {
		case fin == "" && r.Method == http.MethodPut:
			data, rerr := io.ReadAll(io.LimitReader(r.Body, maxBody))
			if rerr != nil {
				erreur(w, http.StatusBadRequest, rerr.Error())
				return
			}
			s.mu.Lock()
			err = ajouterAudio(sorties, seg, data)
			s.mu.Unlock()
			if err == nil && !transcriptionDirecteDesactivee && cheminWhisper() != "" {
				_ = s.Transcr.Lancer(dir, s.Arret) // transcription au fil de l'enregistrement (sans effet si déjà lancée)
			}
		case fin == "fin" && r.Method == http.MethodPost:
			s.mu.Lock()
			err = finaliserWAV(sorties, seg)
			s.mu.Unlock()
			s.Transcr.Reveiller()
		default:
			erreur(w, http.StatusMethodNotAllowed, "méthode non permise")
			return
		}
		if err != nil {
			erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case route == "transcrire" && r.Method == http.MethodPost:
		if err := s.Transcr.Lancer(dir, s.Arret); err != nil {
			erreur(w, http.StatusOK, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case route == "transcription.md" && r.Method == http.MethodGet:
		data, err := os.ReadFile(filepath.Join(sorties, "transcription.md"))
		if err != nil {
			erreur(w, http.StatusNotFound, "pas de transcription")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8") // affiché par le navigateur, pas téléchargé
		_, _ = w.Write(data)

	case strings.HasPrefix(route, "photo/"):
		name := strings.TrimPrefix(route, "photo/")
		if !rePhoto.MatchString(name) {
			erreur(w, http.StatusBadRequest, "nom de photo invalide")
			return
		}
		p := filepath.Join(sorties, "photos", name)
		switch r.Method {
		case http.MethodGet:
			data, err := os.ReadFile(p)
			if err != nil {
				erreur(w, http.StatusNotFound, "photo introuvable")
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(data)
		case http.MethodPut:
			data, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
			if err != nil {
				erreur(w, http.StatusBadRequest, err.Error())
				return
			}
			s.mu.Lock()
			err = writeAtomic(p, data)
			s.mu.Unlock()
			if err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		case http.MethodDelete: // pas de suppression définitive : la photo part dans photos/supprimees/
			s.mu.Lock()
			defer s.mu.Unlock()
			if _, err := os.Stat(p); err == nil {
				dest := filepath.Join(sorties, "photos", "supprimees", name)
				_ = os.MkdirAll(filepath.Dir(dest), 0o755)
				if err := os.Rename(p, dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
					erreur(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			erreur(w, http.StatusMethodNotAllowed, "méthode non permise")
		}

	default:
		erreur(w, http.StatusNotFound, "inconnu")
	}
}
