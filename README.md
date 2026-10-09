# Greffier

**Animer un atelier, prendre les notes en direct, produire le compte rendu.**
Un outil DecideOm pour les consultants : la question s'affiche à l'écran, la réponse se saisit au fil de l'eau (tags, photos du tableau blanc), tout est **enregistré en continu sur le disque**, et le compte rendu est rédigé par une IA (Gemini, Claude…) puis mis en page en PDF à la charte DecideOm.

Tout fonctionne **sans internet**, sur **Windows, macOS et Linux**, sans rien installer.

## Télécharger
Dans les [**Releases**](../../releases), prendre le fichier correspondant à son système :

| Système | Fichier |
|---|---|
| Windows (la plupart des PC) | `greffier-windows-x64.exe` |
| Windows sur processeur ARM (Surface Pro X…) | `greffier-windows-arm64.exe` |
| Mac Apple Silicon (M1, M2…) | `greffier-macos-apple-silicon` |
| Mac Intel | `greffier-macos-intel` |
| Linux | `greffier-linux-x64` |

**Premier lancement**
- **Windows** : « Windows a protégé votre ordinateur » → *Informations complémentaires* → *Exécuter quand même* (l'application n'est pas signée).
- **macOS** : clic droit → *Ouvrir* → *Ouvrir* ; ou `xattr -d com.apple.quarantine greffier-macos-*` dans le Terminal.
- **Linux** : `chmod +x greffier-linux-x64` ; le sélecteur de dossier utilise `zenity` ou `kdialog`.
- Greffier n'écoute que sur ton PC (127.0.0.1) : pas de question du pare-feu.

## Utiliser
1. **Préparer l'atelier** : un dossier avec un fichier `questions.md` (partir de [`exemple/atelier-exemple`](exemple/atelier-exemple/questions.md)).
2. **Double-clic sur Greffier** : une fenêtre de console (à garder ouverte) et une page dans le navigateur s'ouvrent.
3. **Choisir l'atelier** : dans la liste, avec **📂 Parcourir…** (sélecteur de dossier du système), en collant le chemin, ou en glissant le dossier sur Greffier.
4. **Animer** : question à l'écran, saisie de la réponse, tags (Décision, Action, Risque, À creuser, Hors périmètre), photos (bouton, glisser-déposer, Ctrl+V), **écran projeté** (touche P), synthèse (touche S). Un sujet imprévu : **page libre** (touche N).
5. **Arrêter** : bouton **⏻ Arrêter l'application** (ou fermer la fenêtre de console).

Mode opératoire complet : [`docs/mode-operatoire.md`](docs/mode-operatoire.md).

### Notes libres (sans `questions.md`)
Sur la page d'accueil : **✏️ Notes libres**, un titre, **Démarrer**. Greffier crée un dossier daté dans `Documents/Greffier/`, et on prend des notes **page par page**, une par sujet (**N** pour une nouvelle page). La sauvegarde, les photos, les tags et le CR par IA fonctionnent comme pour un atelier, avec un modèle de compte rendu de réunion ([`modele-compte-rendu-libre.md`](modele-compte-rendu-libre.md)).

### Mettre à jour
Au démarrage, Greffier regarde s'il existe une version plus récente sur GitHub. Si c'est le cas, la page d'accueil affiche **Télécharger**. Il suffit de remplacer l'ancien fichier par le nouveau : les notes sont dans les dossiers d'atelier, rien n'est perdu. Hors ligne, rien ne s'affiche. Pour désactiver la vérification : `--sans-maj` (ou la variable `GREFFIER_SANS_MAJ=1`).

### Le fichier `questions.md`
```md
---
atelier: Atelier de cadrage
client: Exemple SA
mission: Cadrage d'une plateforme de données
animateurs: Prénom Nom (DecideOm)
contexte: Quelques phrases de contexte, reprises dans le prompt du compte rendu.
---

# Partie A — Cadrage avec la direction (1 h)

## A1. Vision et ambitions | 15 min | cible: Direction
- Q: * Quelle place la donnée occupe-t-elle dans votre stratégie ?
  - relance: Projet IT, projet métier ou projet d'entreprise ?
- Q: Quelles décisions prenez-vous avec des chiffres contestés ?
```
- `# …` : une partie ; `## A1. Titre` : une section (le code `A1` est obligatoire, il numérote les questions `A1-1`, `A1-2`…).
- Après le titre, séparés par `|` et facultatifs : durée (`15 min`, utilisée par le chronomètre), `cible:`, `étape:`.
- `- Q:` une question (`*` en tête = prioritaire, affichée avec ★) ; `- relance:` une relance, affichée à l'animateur seulement.
- Modifier le fichier puis **recharger la page** : Greffier le relit.

### Ce que Greffier écrit dans le dossier de l'atelier
```
mon-atelier/
  questions.md    la source (à toi)
  sorties/        session.json, notes.md, photos/, historique/ (copie toutes les 10 min)
  outil/          page autonome (secours : double-clic dans Chrome / Edge), outil/cr.html
  prompt-cr.md    prompt du compte rendu (contexte + modèle)
  marp/           slides.md (+ slides.html si marp-cli est installé), notes-atelier.md (pas pour les notes libres)
```
`outil/`, `prompt-cr.md` et `marp/` sont régénérés à chaque ouverture : dans un dépôt Git, les ignorer (`.gitignore`) et ne versionner que `questions.md` et `sorties/`.

## Ce qui protège les notes
| Protection | Détail |
|---|---|
| Navigateur | Chaque frappe est enregistrée dans le navigateur (survit à un plantage) |
| **Disque, en continu** | `sorties/` réécrit ~1,5 s après chaque modification, en écriture atomique |
| Historique | `sorties/historique/` : toutes les 10 min, avant « Nouvelle session », avant une restauration |
| Restauration | Navigateur vidé ? La page propose de recharger la session depuis le disque, photos comprises |
| Photos supprimées | Déplacées dans `sorties/photos/supprimees/`, jamais effacées |

## Le compte rendu par IA
Un prompt commun ([`prompt-compte-rendu.md`](prompt-compte-rendu.md)) et un modèle ([`modele-compte-rendu.md`](modele-compte-rendu.md), ou [`modele-compte-rendu-libre.md`](modele-compte-rendu-libre.md) pour des notes libres) imposent la structure du CR, interdisent d'inventer et font marquer **[À CONFIRMER]** ce qui est ambigu.

1. Page d'atelier → **📝 Compte rendu (IA → PDF)** → **🤖 Copier pour l'IA** → coller dans Gemini, ChatGPT ou Claude.
2. Coller la réponse dans la page : aperçu A4 avec les photos, [À CONFIRMER] surlignés et comptés.
3. Corriger, puis **🖨 Imprimer / PDF**.

Avec **Claude Code**, la commande [`/compte-rendu`](.claude/commands/compte-rendu.md) fait tout, jusqu'au PDF ([`cr2pdf.py`](cr2pdf.py), qui nécessite pandoc).

> ⚠ Coller des notes client dans un assistant en ligne les transmet à son éditeur : utiliser un compte professionnel et vérifier la compatibilité avec la confidentialité convenue avec le client.

## Développement
Go ≥ 1.22, sans dépendance externe. La page (`app/`), la page de choix (`lanceur.html`), le prompt, le modèle et le thème Marp sont **intégrés à l'exécutable** : les modifier nécessite de recompiler.

```sh
go test ./...                          # tests
go run . exemple/atelier-exemple       # lancer sans compiler
go run . --generer exemple/atelier-exemple   # générer les fichiers d'un atelier sans navigateur
./compiler.sh v1.2.3                   # les 5 binaires dans dist/
```

| Fichier | Rôle |
|---|---|
| `main.go`, `atelier.go`, `serveur.go`, `systeme*.go` | application (serveur local, lecture de `questions.md`, intégration système) |
| `maj.go` | avis de nouvelle version (API GitHub, dernière release) |
| `app/` | page d'atelier, page CR, moteur de notes (`notes-core.js`), charte (`theme.css`), mise en page A4 (`document.css`) |
| `docs/charte-decideom.md` | charte graphique DecideOm appliquée |
| `winres/`, `rsrc_windows_*.syso` | icône et propriétés de l'exe Windows (régénérer : `go run github.com/tc-hib/go-winres@v0.3.3 simply --icon winres/icon.png --manifest cli --arch amd64,arm64`) |
| `.github/workflows/binaires.yml` | GitHub Action : tests et binaires à chaque push ; *Release* à chaque étiquette `v*` |

**Publier une version** : `git tag v1.0.0 && git push --tags` → l'Action crée la *Release* avec les binaires.

## Licence
[MIT](LICENSE) © DecideOm. Le logo et la marque DecideOm ne sont pas couverts par la licence MIT. Polices Poppins et Open Sans sous licence SIL OFL 1.1.
