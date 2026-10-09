package main

// Serveur local (127.0.0.1 uniquement) :
//   /                       page de choix de l'atelier (lanceur)
//   /api/ateliers           ateliers récents et trouvés à côté de l'application
//   /api/parcourir          sélecteur de dossier natif du système
//   /api/ouvrir             prépare un atelier et renvoie l'adresse de sa page
//   /api/notes-libres       crée un dossier de notes libres (sans questions) et l'ouvre
//   /api/version            version courante et éventuelle nouvelle version sur GitHub
//   /a/<slug>/…             page d'atelier (fichiers intégrés + questions.js lu en direct)
//   /a/<slug>/api/…         sauvegarde sur disque dans <atelier>/sorties/

import (
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
	"time"
)

const maxBody = 30 << 20

var rePhoto = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,120}\.(jpg|jpeg|png)$`)

type Serveur struct {
	origin    string
	mu        sync.Mutex        // une écriture disque à la fois
	ateliers  map[string]string // slug → dossier
	amu       sync.Mutex
	Arret     chan struct{} // fermé quand la page demande l'arrêt de l'application
	arretOnce sync.Once
	Maj       verificateurMaj
}

func NewServeur(port int) *Serveur {
	return &Serveur{origin: fmt.Sprintf("http://127.0.0.1:%d", port), ateliers: map[string]string{}, Arret: make(chan struct{}),
		Maj: verificateurMaj{info: InfoVersion{Version: version}}}
}

func (s *Serveur) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.lanceur)
	mux.HandleFunc("/api/ateliers", s.listeAteliers)
	mux.HandleFunc("/api/parcourir", s.parcourir)
	mux.HandleFunc("/api/ouvrir", s.ouvrir)
	mux.HandleFunc("/api/arreter", s.arreter)
	mux.HandleFunc("/api/notes-libres", s.notesLibres)
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, s.Maj.Info()) })
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

func (s *Serveur) listeAteliers(w http.ResponseWriter, r *http.Request) {
	vus := map[string]bool{}
	var liste []infoAtelier
	for _, d := range chargerRecents() {
		if _, err := os.Stat(filepath.Join(d, "questions.md")); err == nil && !vus[d] {
			vus[d] = true
			liste = append(liste, decrire(d, true))
		}
	}
	var trouves []string
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
	for _, d := range trouves {
		liste = append(liste, decrire(d, false))
	}
	if liste == nil {
		liste = []infoAtelier{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ateliers": liste, "systeme": nomSysteme(), "dossierNotes": dossierNotesParDefaut()})
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
	s.arretOnce.Do(func() { close(s.Arret) })
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
	s.amu.Lock()
	s.ateliers[a.Slug] = abs
	s.amu.Unlock()
	ajouterRecent(abs)
	_, nq := a.QuestionCount()
	quoi := fmt.Sprintf("%d questions", nq)
	if a.Mode == "libre" {
		quoi = "notes libres"
	}
	logf("Atelier ouvert : %s (%s) — sauvegarde dans %s", abs, quoi, filepath.Join(abs, "sorties"))
	return "/a/" + a.Slug + "/atelier.html", append(warnings, notes...), nil
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
