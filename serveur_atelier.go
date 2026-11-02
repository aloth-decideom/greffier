package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aloth-decideom/greffier/internal/atelier"
	"github.com/aloth-decideom/greffier/internal/audio"
	"github.com/aloth-decideom/greffier/internal/compterendu"
	"github.com/aloth-decideom/greffier/internal/outils"
	"github.com/aloth-decideom/greffier/internal/systeme"
	"github.com/aloth-decideom/greffier/internal/transcription"
)

// Pages et API d'un atelier : /a/<slug>/… (voir serveur.go).

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
		a, _, err := atelier.Charger(dir)
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
	servirRessource(w, r, path.Clean("/" + sub)[1:])
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
			if err := systeme.EcrireAtomique(filepath.Join(sorties, "session.json"), session); err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := systeme.EcrireAtomique(filepath.Join(sorties, "notes.md"), []byte(p.Markdown)); err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if label, ok := p.Historique.(string); ok && label != "" {
			label = systeme.Etiquette(label)
			if len(label) > 30 {
				label = label[:30]
			}
			name := fmt.Sprintf("session_%s_%s.json", time.Now().Format("2006-01-02_15h04"), label)
			if err := systeme.EcrireAtomique(filepath.Join(sorties, "historique", name), session); err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "heure": time.Now().Format("15:04:05")})

	case route == "claude" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]bool{"disponible": compterendu.CheminClaude() != ""})

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
		systeme.Logf("Compte rendu : rédaction par Claude en cours…")
		md, err := compterendu.Rediger(r.Context(), sorties, p.Texte)
		if err != nil {
			systeme.Logf("⚠ Compte rendu : %v", err)
			erreur(w, http.StatusOK, err.Error())
			return
		}
		fichier := compterendu.Fichier(dir)
		s.mu.Lock()
		err = compterendu.MettreDeCote(dir, "avant-claude") // les corrections faites dans la page ne sont jamais perdues
		if err == nil {
			err = systeme.EcrireAtomique(fichier, []byte(md+"\n"))
		}
		s.mu.Unlock()
		if err != nil {
			erreur(w, http.StatusOK, "compte rendu rédigé mais non enregistré : "+err.Error())
			return
		}
		systeme.Logf("Compte rendu enregistré : %s", fichier)
		writeJSON(w, http.StatusOK, map[string]string{"markdown": md, "fichier": fichier})

	// Compte rendu relu dans la page cr.html : enregistré sur le disque au fil de la frappe.
	case route == "cr" && r.Method == http.MethodGet:
		fichier := compterendu.Fichier(dir)
		res := map[string]any{"markdown": "", "fichier": fichier, "modifie": int64(0)}
		if data, err := os.ReadFile(fichier); err == nil {
			res["markdown"] = strings.TrimSuffix(string(data), "\n")
			if st, err := os.Stat(fichier); err == nil {
				res["modifie"] = st.ModTime().UnixMilli()
			}
		}
		writeJSON(w, http.StatusOK, res)

	// Questions et en-tête modifiés dans la page : questions.md réécrit, l'ancien gardé dans sorties/historique/.
	case route == "questions" && r.Method == http.MethodPut:
		var p struct {
			Meta  map[string]string `json:"meta"`
			Parts []atelier.Part    `json:"parts"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&p); err != nil {
			erreur(w, http.StatusBadRequest, "données illisibles")
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		ancien, _, err := atelier.Charger(dir)
		if err != nil {
			erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ancien.Mode == "libre" {
			erreur(w, http.StatusBadRequest, "notes libres : pas de questions à modifier")
			return
		}
		meta := map[string]string{}
		for k, v := range ancien.Meta {
			meta[k] = v
		}
		for k, v := range p.Meta {
			if k != "mode" && k != "slug" {
				meta[k] = v
			}
		}
		client := cmp.Or(atelier.NettoyerChamp(meta["client"]), "client")
		if atelier.Slugify(client+"-"+filepath.Base(dir)) != ancien.Slug {
			meta["slug"] = ancien.Slug // client renommé : même adresse de page, mêmes notes dans le navigateur
		}
		texte, err := atelier.FormatQuestions(meta, p.Parts)
		if err == nil {
			_, _, _, err = atelier.ParseQuestions(texte)
		}
		if err != nil {
			erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		fichier := filepath.Join(dir, "questions.md")
		if data, err := os.ReadFile(fichier); err == nil {
			nom := fmt.Sprintf("questions_%s.md", time.Now().Format("2006-01-02_15h04m05"))
			if err := systeme.EcrireAtomique(filepath.Join(sorties, "historique", nom), data); err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if err := systeme.EcrireAtomique(fichier, []byte(texte)); err != nil {
			erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		systeme.Logf("Questions modifiées depuis la page : %s", fichier)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "heure": time.Now().Format("15:04:05")})

	case route == "cr" && r.Method == http.MethodPut:
		// copie : version écartée par l'utilisateur (navigateur ≠ disque), gardée dans sorties/historique/
		var p struct {
			Markdown string `json:"markdown"`
			Copie    string `json:"copie"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&p); err != nil {
			erreur(w, http.StatusBadRequest, "données illisibles")
			return
		}
		s.mu.Lock()
		var err error
		if p.Copie != "" {
			err = compterendu.Copie(dir, p.Markdown, p.Copie)
		} else {
			err = systeme.EcrireAtomique(compterendu.Fichier(dir), []byte(p.Markdown+"\n"))
		}
		s.mu.Unlock()
		if err != nil {
			erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "heure": time.Now().Format("15:04:05")})

	case route == "audio" && r.Method == http.MethodGet:
		segments := audio.Lister(sorties)
		total := 0.0
		for _, sg := range segments {
			total += sg.Secondes
		}
		_, errT := os.Stat(filepath.Join(sorties, "transcription.md"))
		writeJSON(w, http.StatusOK, map[string]any{"segments": segments, "secondes": total, "whisper": outils.WhisperDisponible(),
			"transcription": errT == nil, "etat": s.Transcr.Etat(dir)})

	case route == "audio" && r.Method == http.MethodDelete:
		if _, err := os.Stat(filepath.Join(sorties, "transcription.md")); err != nil {
			erreur(w, http.StatusOK, "transcrire l'enregistrement avant de supprimer l'audio")
			return
		}
		if s.Transcr.Occupe() {
			erreur(w, http.StatusOK, "transcription en cours")
			return
		}
		s.mu.Lock()
		err := audio.Supprimer(sorties)
		s.mu.Unlock()
		if err != nil {
			erreur(w, http.StatusOK, err.Error())
			return
		}
		systeme.Logf("Audio déplacé dans %s", filepath.Join(audio.Dossier(sorties), "supprimes"))
		repondreOK(w)

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
			voies := 1 // 2 : stéréo entrelacée, micro + son de la visio
			if r.URL.Query().Get("voies") == "2" {
				voies = 2
			}
			s.mu.Lock()
			err = audio.Ajouter(sorties, seg, data, voies)
			s.mu.Unlock()
			if err == nil && !transcription.DirecteDesactivee && outils.WhisperDisponible() {
				_ = s.Transcr.Lancer(dir, s.Arret) // transcription au fil de l'enregistrement (sans effet si déjà lancée)
			}
		case fin == "extrait" && r.Method == http.MethodGet:
			q := r.URL.Query()
			debut, _ := strconv.ParseFloat(q.Get("debut"), 64)
			finExtrait, _ := strconv.ParseFloat(q.Get("fin"), 64)
			voie, _ := strconv.Atoi(q.Get("voie"))
			s.mu.Lock()
			wav, err := audio.Extrait(sorties, seg, debut, finExtrait, voie)
			s.mu.Unlock()
			if err != nil {
				erreur(w, http.StatusNotFound, err.Error())
				return
			}
			w.Header().Set("Content-Type", "audio/wav")
			w.Write(wav)
			return
		case fin == "fin" && r.Method == http.MethodPost:
			s.mu.Lock()
			err = audio.Finaliser(sorties, seg)
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
		repondreOK(w)

	case route == "voix" && r.Method == http.MethodGet:
		v, analyse := transcription.LireVoix(sorties)
		segments := audio.Lister(sorties)
		nouveaux := 0
		for _, sg := range segments {
			if !slices.Contains(v.Segments, sg.ID) {
				nouveaux++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"nemo": outils.NemoDisponible(), "etat": s.Voix.Etat(dir), "analyse": analyse,
			"utiliser": v.Utiliser, "choisi": v.Choisi, "audio": len(segments) > 0, "nouveaux": nouveaux, "voix": transcription.ResumerVoix(sorties, v)})

	case route == "voix" && r.Method == http.MethodPost:
		if err := s.Voix.Lancer(dir, s.Arret, &s.Transcr); err != nil {
			erreur(w, http.StatusOK, err.Error())
			return
		}
		repondreOK(w)

	case route == "voix" && r.Method == http.MethodPut:
		var p struct {
			Noms     map[string]string `json:"noms"`
			Mixtes   map[string]bool   `json:"mixtes"`
			Utiliser bool              `json:"utiliser"`
			Choisi   bool              `json:"choisi"` // l'utilisateur a coché lui-même une des deux options
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&p); err != nil {
			erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		s.mu.Lock()
		err := transcription.MajVoix(dir, p.Noms, p.Mixtes, p.Utiliser, p.Choisi)
		s.mu.Unlock()
		if err != nil {
			erreur(w, http.StatusOK, err.Error())
			return
		}
		repondreOK(w)

	case route == "transcrire" && r.Method == http.MethodPost:
		if err := s.Transcr.Lancer(dir, s.Arret); err != nil {
			erreur(w, http.StatusOK, err.Error())
			return
		}
		repondreOK(w)

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
			err = systeme.EcrireAtomique(p, data)
			s.mu.Unlock()
			if err != nil {
				erreur(w, http.StatusInternalServerError, err.Error())
				return
			}
			repondreOK(w)
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
			repondreOK(w)
		default:
			erreur(w, http.StatusMethodNotAllowed, "méthode non permise")
		}

	default:
		erreur(w, http.StatusNotFound, "inconnu")
	}
}
