package atelier

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aloth-decideom/greffier/ressources"
)

// promptCR : prompt du compte rendu (prompt commun + modèle), complété par l'en-tête de questions.md.
func promptCR(m map[string]string) string {
	prompt, _ := ressources.Lire("compte-rendu/prompt.md")
	nomModele := "compte-rendu/modele.md"
	if estLibre(m) {
		nomModele = "compte-rendu/modele-libre.md"
	}
	modele, _ := ressources.Lire(nomModele)
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
	if err := ressources.Copier("app", outil); err != nil {
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
	titre, client = NettoyerChamp(titre), NettoyerChamp(client)
	if titre == "" {
		titre = "Notes du " + now.Format("02/01/2006")
	}
	nom := now.Format("2006-01-02_15h04")
	if s := Slugify(titre); s != "" {
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

func (a *Atelier) generateMarp(out string) (notes []string) {
	m := a.Meta
	var s strings.Builder
	fmt.Fprintf(&s, "---\nmarp: true\ntheme: decideom\npaginate: true\nfooter: \"%s × DecideOm · %s\"\n---\n\n", m["client"], m["mission"])
	fmt.Fprintf(&s, "<!-- _class: cover -->\n<!-- _paginate: false -->\n\n# %s\n\n**%s** · %s\n%s\n\n---\n\n<!-- _class: agenda -->\n\n# Ordre du jour\n",
		cmp.Or(m["atelier"], "Atelier"), m["client"], m["mission"], m["animateurs"])
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
		fmt.Fprintf(&n, "# Notes : %s\n\nDate : \nParticipants : \n\n> Tags en fin de ligne : `#decision`, `#action(qui, échéance)`, `#risque`, `#creuser`, `#hors`\n", cmp.Or(m["atelier"], "Atelier"))
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
		_ = ressources.Copier("app/"+d, filepath.Join(out, d))
	}
	theme := filepath.Join(out, "decideom.css")
	css, _ := ressources.Lire("marp/decideom.css")
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
