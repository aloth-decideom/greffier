package main

// Lecture de questions.md et génération des fichiers d'un atelier
// (outil/, prompt-cr.md, marp/).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Question struct {
	ID       string   `json:"id"`
	Text     string   `json:"text"`
	Star     bool     `json:"star"`
	Relances []string `json:"relances"`
}

type Section struct {
	Code      string     `json:"code"`
	Title     string     `json:"title"`
	Minutes   int        `json:"minutes"`
	Cible     string     `json:"cible"`
	Etape     string     `json:"etape"`
	Questions []Question `json:"questions"`
}

type Part struct {
	Title    string    `json:"title"`
	Sections []Section `json:"sections"`
}

// Atelier : données transmises à la page (globalThis.ATELIER).
type Atelier struct {
	Mode       string            `json:"mode"` // "" (questions) ou "libre" (pages libres seulement)
	Meta       map[string]string `json:"meta"`
	Parts      []Part            `json:"parts"`
	Slug       string            `json:"slug"`
	StorageKey string            `json:"storageKey"`
	Dossier    string            `json:"dossier"`
	PromptCR   string            `json:"promptCR"`
}

var (
	reSection  = regexp.MustCompile(`^([A-Z]\d+)\.\s*(.+)$`)
	reQuestion = regexp.MustCompile(`^\s*- Q:`)
	reRelance  = regexp.MustCompile(`^\s+- relance:`)
	reComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	reNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
	reDigits   = regexp.MustCompile(`\D`)
)

// ParseQuestions lit le format de questions.md. Les avertissements (lignes ignorées) sont renvoyés à part.
func ParseQuestions(text string) (meta map[string]string, parts []Part, warnings []string, err error) {
	meta = map[string]string{}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimPrefix(text, "\uFEFF") // BOM éventuel (Bloc-notes Windows)
	body := text
	if strings.HasPrefix(text, "---") {
		chunks := strings.SplitN(text, "---", 3)
		if len(chunks) == 3 {
			for _, line := range strings.Split(strings.TrimSpace(chunks[1]), "\n") {
				if k, v, ok := strings.Cut(line, ":"); ok {
					meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
			body = chunks[2]
		}
	}
	body = reComment.ReplaceAllString(body, "")

	var part *Part
	var section *Section
	for n, raw := range strings.Split(body, "\n") {
		line := strings.TrimRightFunc(raw, unicode.IsSpace)
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# "):
			parts = append(parts, Part{Title: strings.TrimSpace(line[2:])})
			part = &parts[len(parts)-1]
			section = nil
		case strings.HasPrefix(line, "## "):
			if part == nil {
				return nil, nil, nil, fmt.Errorf("ligne %d : section sans partie (ajouter une ligne « # Partie … » avant) : %s", n+1, line)
			}
			chunks := strings.FieldsFunc(line[3:], func(r rune) bool { return r == '|' || r == '·' })
			for i := range chunks {
				chunks[i] = strings.TrimSpace(chunks[i])
			}
			m := reSection.FindStringSubmatch(chunks[0])
			if m == nil {
				return nil, nil, nil, fmt.Errorf("ligne %d : en-tête de section invalide, il faut un code comme « ## A1. Titre » : %s", n+1, line)
			}
			s := Section{Code: m[1], Title: m[2], Questions: []Question{}}
			for _, c := range chunks[1:] {
				if strings.HasSuffix(c, "min") {
					s.Minutes, _ = strconv.Atoi(reDigits.ReplaceAllString(c, ""))
				} else if k, v, ok := strings.Cut(c, ":"); ok {
					k, v = strings.TrimSpace(k), strings.TrimSpace(v)
					if strings.HasPrefix(k, "étape") || strings.HasPrefix(k, "etape") {
						s.Etape = v
					} else if k == "cible" {
						s.Cible = v
					}
				}
			}
			part.Sections = append(part.Sections, s)
			section = &part.Sections[len(part.Sections)-1]
		case reQuestion.MatchString(line):
			if section == nil {
				return nil, nil, nil, fmt.Errorf("ligne %d : question hors section : %s", n+1, line)
			}
			_, q, _ := strings.Cut(line, "Q:")
			q = strings.TrimSpace(q)
			// Question prioritaire : « * » en tête (« ★ » accepté pour les anciens fichiers ; « ** » = gras, pas une priorité)
			star := false
			if strings.HasPrefix(q, "★") {
				star, q = true, strings.TrimSpace(strings.TrimPrefix(q, "★"))
			} else if strings.HasPrefix(q, "*") && !strings.HasPrefix(q, "**") {
				star, q = true, strings.TrimSpace(strings.TrimPrefix(q, "*"))
			}
			section.Questions = append(section.Questions, Question{
				ID: fmt.Sprintf("%s-%d", section.Code, len(section.Questions)+1), Text: q, Star: star, Relances: []string{},
			})
		case reRelance.MatchString(line):
			if section == nil || len(section.Questions) == 0 {
				return nil, nil, nil, fmt.Errorf("ligne %d : relance sans question : %s", n+1, line)
			}
			_, r, _ := strings.Cut(line, "relance:")
			last := &section.Questions[len(section.Questions)-1]
			last.Relances = append(last.Relances, strings.TrimSpace(r))
		default:
			warnings = append(warnings, fmt.Sprintf("ligne %d ignorée : %s", n+1, strings.TrimSpace(line)))
		}
	}
	if len(parts) == 0 && !estLibre(meta) {
		return nil, nil, nil, fmt.Errorf("aucune partie trouvée (une ligne « # Partie … » est nécessaire, ou « mode: libre » dans l'en-tête)")
	}
	if parts == nil {
		parts = []Part{}
	}
	return meta, parts, warnings, nil
}

var accents = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "á", "a", "ã", "a", "ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "í", "i", "ô", "o", "ö", "o", "ó", "o", "ù", "u", "û", "u", "ü", "u", "ú", "u",
	"ÿ", "y", "œ", "oe", "æ", "ae", "ñ", "n",
)

func slugify(s string) string {
	s = accents.Replace(strings.ToLower(s))
	return strings.Trim(reNonAlnum.ReplaceAllString(s, "-"), "-")
}

// LoadAtelier lit <dir>/questions.md et prépare les données de la page.
func LoadAtelier(dir string) (*Atelier, []string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "questions.md"))
	if err != nil {
		return nil, nil, fmt.Errorf("pas de questions.md dans %s", dir)
	}
	meta, parts, warnings, err := ParseQuestions(string(raw))
	if err != nil {
		return nil, nil, err
	}
	name := filepath.Base(dir)
	client := meta["client"]
	if client == "" {
		client = "client"
		if estLibre(meta) {
			client = "notes"
		}
	}
	slug := slugify(client + "-" + name)
	if s := slugify(meta["slug"]); s != "" { // fixé quand le client a été renommé depuis la page : même adresse, mêmes notes
		slug = s
	}
	a := &Atelier{Meta: meta, Parts: parts, Slug: slug, Dossier: name}
	if estLibre(meta) {
		a.Mode = "libre"
	}
	a.StorageKey = "decideom-" + a.Slug
	a.PromptCR = buildPrompt(meta)
	return a, warnings, nil
}

// Rappel du format écrit en tête de questions.md quand la page le réécrit.
const formatQuestions = `<!--
FORMAT (lu par Greffier) — à respecter :
  # Partie X — Titre (durée)
  ## X1. Titre de section | 15 min | cible: CODIR | étape: 2     (durée, cible, étape : facultatives)
  - Q: Texte de la question            (* en tête = question prioritaire)
    - relance: Question de relance (affichée en petit / en notes présentateur)
Modifiable aussi depuis la page de l'atelier (⋯ › ✎ Modifier les questions).
Après modification à la main : recharger la page de l'atelier (Greffier relit ce fichier).
-->`

// Clés de l'en-tête dans l'ordre d'écriture ; les autres suivent par ordre alphabétique.
var ordreMeta = []string{"atelier", "client", "mission", "animateurs", "contexte", "mode", "slug"}

// FormatQuestions : inverse de ParseQuestions. Les codes de section sont renumérotés
// (lettre de la partie, rang dans la partie) : A1, A2… B1… ; les ids des questions en découlent.
func FormatQuestions(meta map[string]string, parts []Part) (string, error) {
	if len(parts) > 26 {
		return "", fmt.Errorf("26 parties au plus")
	}
	ligne := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var b strings.Builder
	b.WriteString("---\n")
	cles := append([]string{}, ordreMeta...)
	var autres []string
	for k := range meta {
		if !slices.Contains(ordreMeta, k) {
			autres = append(autres, k)
		}
	}
	sort.Strings(autres)
	for _, k := range append(cles, autres...) {
		k = strings.TrimSpace(strings.ReplaceAll(k, ":", ""))
		if v := nettoyerChamp(meta[k]); k != "" && v != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString("---\n\n" + formatQuestions + "\n")
	for pi, p := range parts {
		lettre := string(rune('A' + pi))
		titre := ligne(p.Title)
		if titre == "" {
			titre = "Partie " + lettre
		}
		fmt.Fprintf(&b, "\n# %s\n", titre)
		for si, s := range p.Sections {
			titre := strings.NewReplacer("|", "/", "·", "-").Replace(ligne(s.Title))
			if titre == "" {
				titre = "Sans titre"
			}
			fmt.Fprintf(&b, "\n## %s%d. %s", lettre, si+1, titre)
			if s.Minutes > 0 {
				fmt.Fprintf(&b, " | %d min", s.Minutes)
			}
			for _, kv := range [][2]string{{"cible", s.Cible}, {"étape", s.Etape}} {
				if v := strings.NewReplacer("|", "/", "·", "-").Replace(ligne(kv[1])); v != "" {
					fmt.Fprintf(&b, " | %s: %s", kv[0], v)
				}
			}
			b.WriteString("\n")
			for _, q := range s.Questions {
				texte := ligne(q.Text)
				if !q.Star { // un « * » ou « ★ » en tête serait relu comme une priorité
					for (strings.HasPrefix(texte, "*") && !strings.HasPrefix(texte, "**")) || strings.HasPrefix(texte, "★") {
						texte = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(texte, "★"), "*"))
					}
				}
				if texte == "" {
					continue
				}
				if q.Star {
					texte = "* " + texte
				}
				fmt.Fprintf(&b, "- Q: %s\n", texte)
				for _, r := range q.Relances {
					if r = ligne(r); r != "" {
						fmt.Fprintf(&b, "  - relance: %s\n", r)
					}
				}
			}
		}
	}
	return b.String(), nil
}

func (a *Atelier) QuestionCount() (sections, questions int) {
	for _, p := range a.Parts {
		for _, s := range p.Sections {
			sections++
			questions += len(s.Questions)
		}
	}
	return
}

// estLibre : notes libres (« mode: libre » dans l'en-tête), sans questions préparées.
func estLibre(meta map[string]string) bool {
	return strings.EqualFold(meta["mode"], "libre")
}

func buildPrompt(m map[string]string) string {
	prompt, _ := embedded.ReadFile("prompt-compte-rendu.md")
	nomModele := "modele-compte-rendu.md"
	if estLibre(m) {
		nomModele = "modele-compte-rendu-libre.md"
	}
	modele, _ := embedded.ReadFile(nomModele)
	ctx := m["contexte"]
	if ctx == "" {
		ctx = "non précisé"
	}
	return strings.NewReplacer(
		"{{CLIENT}}", m["client"], "{{MISSION}}", m["mission"], "{{ATELIER}}", m["atelier"],
		"{{ANIMATEURS}}", m["animateurs"], "{{CONTEXTE}}", ctx, "{{MODELE}}", strings.TrimSpace(string(modele)),
	).Replace(string(prompt))
}

// QuestionsJS : contenu de questions.js (données de la page).
func (a *Atelier) QuestionsJS() []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	_ = enc.Encode(a)
	return []byte("// Généré par Greffier à partir de questions.md, ne pas modifier à la main.\n" +
		"globalThis.ATELIER = " + strings.TrimRight(buf.String(), "\n") + ";\n")
}

// Generate écrit dans le dossier d'atelier : outil/ (page autonome pour le double-clic),
// prompt-cr.md, sorties/ et marp/ (slides.md + notes ; slides.html si marp est installé).
func (a *Atelier) Generate(dir string) (notes []string, err error) {
	outil := filepath.Join(dir, "outil")
	if err := os.RemoveAll(outil); err != nil {
		return nil, err
	}
	if err := copyEmbedded("app", outil); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(outil, "questions.js"), a.QuestionsJS(), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt-cr.md"), []byte(a.PromptCR), 0o644); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "sorties"), 0o755); err != nil {
		return nil, err
	}
	if a.Mode != "libre" { // pas de slides pour des notes libres
		notes = append(notes, a.generateMarp(filepath.Join(dir, "marp"))...)
	}
	return notes, nil
}

// CreerNotesLibres crée <base>/AAAA-MM-JJ_HHhMM-<titre>/questions.md (en-tête seul, « mode: libre »)
// et renvoie le dossier créé.
func CreerNotesLibres(base, titre, client string, now time.Time) (string, error) {
	titre, client = nettoyerChamp(titre), nettoyerChamp(client)
	if titre == "" {
		titre = "Notes du " + now.Format("02/01/2006")
	}
	nom := now.Format("2006-01-02_15h04")
	if s := slugify(titre); s != "" {
		if len(s) > 50 {
			s = strings.Trim(s[:50], "-")
		}
		nom += "-" + s
	}
	dir := filepath.Join(base, nom)
	for i := 2; ; i++ { // jamais d'écrasement d'un dossier existant
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		dir = filepath.Join(base, fmt.Sprintf("%s-%d", nom, i))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	contenu := fmt.Sprintf(`---
atelier: %s
client: %s
mission:
animateurs:
contexte:
mode: libre
---

<!--
Notes libres Greffier : pas de questions préparées, on prend des notes page par page.
Les champs ci-dessus (mission, animateurs, contexte) sont repris dans le prompt du compte rendu :
les compléter améliore le CR. Après modification : recharger la page.
-->
`, titre, client)
	if err := os.WriteFile(filepath.Join(dir, "questions.md"), []byte(contenu), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// nettoyerChamp : une seule ligne, sans « --- » qui casserait l'en-tête.
func nettoyerChamp(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(strings.ReplaceAll(s, "---", "—"))
}

func copyEmbedded(root, dest string) error {
	return fs.WalkDir(embedded, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(p, root)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := embedded.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func (a *Atelier) generateMarp(out string) (notes []string) {
	m := a.Meta
	var s strings.Builder
	fmt.Fprintf(&s, "---\nmarp: true\ntheme: decideom\npaginate: true\nfooter: \"%s × DecideOm · %s\"\n---\n\n", m["client"], m["mission"])
	fmt.Fprintf(&s, "<!-- _class: cover -->\n<!-- _paginate: false -->\n\n# %s\n\n**%s** · %s\n%s\n\n---\n\n<!-- _class: agenda -->\n\n# Ordre du jour\n",
		orDefault(m["atelier"], "Atelier"), m["client"], m["mission"], m["animateurs"])
	for _, p := range a.Parts {
		fmt.Fprintf(&s, "\n**%s**\n\n", p.Title)
		for _, sec := range p.Sections {
			fmt.Fprintf(&s, "- %s. %s%s\n", sec.Code, sec.Title, minutesLabel(sec.Minutes, " · *%d min*"))
		}
	}
	for _, p := range a.Parts {
		for _, sec := range p.Sections {
			fmt.Fprintf(&s, "\n---\n\n<!-- _class: section -->\n\n<div class=\"part\">%s</div>\n\n# %s. %s\n\n%s\n",
				p.Title, sec.Code, sec.Title, strings.Trim(minutesLabel(sec.Minutes, "%d min")+" · "+sec.Cible, " ·"))
			for _, q := range sec.Questions {
				star := ""
				if q.Star {
					star = `<span class="star">★</span> `
				}
				rel := make([]string, len(q.Relances))
				for i, r := range q.Relances {
					rel[i] = "- " + r
				}
				fmt.Fprintf(&s, "\n---\n\n<div class=\"qmeta\">%s. %s · %s</div>\n\n## %s%s\n\n<!--\n%s\n-->\n",
					sec.Code, sec.Title, q.ID, star, q.Text, strings.Join(rel, "\n"))
			}
		}
	}
	s.WriteString("\n---\n\n# Synthèse et prochaines étapes\n\n- Décisions actées\n- Actions : qui, quand\n- Points ouverts pour le prochain atelier\n")

	if err := os.MkdirAll(out, 0o755); err != nil {
		return []string{"marp : " + err.Error()}
	}
	_ = os.WriteFile(filepath.Join(out, "slides.md"), []byte(s.String()), 0o644)

	notesFile := filepath.Join(out, "notes-atelier.md")
	if _, err := os.Stat(notesFile); err != nil {
		var n strings.Builder
		fmt.Fprintf(&n, "# Notes : %s\n\nDate : \nParticipants : \n\n> Tags en fin de ligne : `#decision`, `#action(qui, échéance)`, `#risque`, `#creuser`, `#hors`\n", orDefault(m["atelier"], "Atelier"))
		for _, p := range a.Parts {
			fmt.Fprintf(&n, "\n# %s\n", p.Title)
			for _, sec := range p.Sections {
				fmt.Fprintf(&n, "\n## %s. %s%s\n", sec.Code, sec.Title, minutesLabel(sec.Minutes, " (%d min)"))
				for _, q := range sec.Questions {
					star := ""
					if q.Star {
						star = "★ "
					}
					fmt.Fprintf(&n, "\n### [%s] %s%s\n\n- \n", q.ID, star, q.Text)
				}
			}
		}
		_ = os.WriteFile(notesFile, []byte(n.String()), 0o644)
	}
	for _, d := range []string{"fonts", "img"} {
		_ = os.RemoveAll(filepath.Join(out, d))
		_ = copyEmbedded("app/"+d, filepath.Join(out, d))
	}
	theme := filepath.Join(out, "decideom.css")
	css, _ := embedded.ReadFile("marp/decideom.css")
	_ = os.WriteFile(theme, css, 0o644)

	marp := findMarp()
	if marp == "" {
		return []string{"slides Marp : slides.md généré (slides.html nécessite marp-cli, facultatif)"}
	}
	cmd := exec.Command(marp, "slides.md", "--theme-set", "decideom.css", "--html", "--no-stdin", "--allow-local-files", "-o", "slides.html")
	cmd.Dir = out
	if outp, err := cmd.CombinedOutput(); err != nil {
		return []string{"slides Marp : échec (" + strings.TrimSpace(string(outp)) + ")"}
	}
	return []string{"slides Marp : marp/slides.html régénéré"}
}

func minutesLabel(min int, format string) string {
	if min <= 0 {
		return ""
	}
	return fmt.Sprintf(format, min)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// findMarp cherche marp-cli : dans le PATH, ou installé dans marp/ (à côté de l'application ou du dépôt).
func findMarp() string {
	if p, err := exec.LookPath("marp"); err == nil {
		return p
	}
	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	for _, dir := range []string{base, filepath.Join(base, ".."), filepath.Join(base, "kit")} {
		for _, name := range []string{"marp.cmd", "marp"} {
			p := filepath.Join(dir, "marp", "node_modules", ".bin", name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
