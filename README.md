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
2. **Double-clic sur Greffier** : une fenêtre de console (à garder ouverte), une page dans le navigateur et une **icône Greffier dans la zone de notification** (à côté de l'horloge) s'ouvrent. Navigateur fermé par erreur ? Clic sur l'icône : la page de l'atelier en cours se rouvre.
3. **Choisir l'atelier** : dans la liste, avec **📂 Parcourir…** (sélecteur de dossier du système), en collant le chemin, ou en glissant le dossier sur Greffier. Le bouton **⋯** de chaque ligne permet d'ouvrir le dossier de l'atelier, de le retirer des récents ou de le supprimer définitivement (après confirmation).
4. **Animer** : question à l'écran, saisie de la réponse, tags (Décision, Action, Risque, À creuser, Hors périmètre), photos (bouton, glisser-déposer, Ctrl+V), **écran projeté** (touche P), synthèse (touche S). Un sujet imprévu : **page libre** (touche N).
5. **Arrêter** : bouton **⏻ Arrêter l'application**, menu de l'icône → **Arrêter Greffier**, ou fermer la fenêtre de console.

Mode opératoire complet : [`docs/mode-operatoire.md`](docs/mode-operatoire.md).

### Notes libres (sans `questions.md`)
Sur la page d'accueil : **✏️ Notes libres**, un titre, **Démarrer**. Greffier crée un dossier daté dans `Documents/Greffier/`, et on prend des notes **page par page**, une par sujet (**N** pour une nouvelle page). La sauvegarde, les photos, les tags et le CR par IA fonctionnent comme pour un atelier, avec un modèle de compte rendu de réunion ([`modele-compte-rendu-libre.md`](modele-compte-rendu-libre.md)).

### Mettre à jour
Au démarrage, puis toutes les 4 h tant qu'il reste ouvert, Greffier regarde s'il existe une version plus récente sur GitHub. Si c'est le cas, l'icône Greffier propose **⬆ Installer Greffier vX et redémarrer**, et la page d'accueil **Installer et redémarrer** : Greffier télécharge la nouvelle version, vérifie sa somme de contrôle (`SHA256SUMS.txt` de la release), remplace son propre fichier et redémarre en quelques secondes. Les pages ouvertes continuent d'enregistrer, les notes ne bougent pas.
Si l'application est dans un dossier non modifiable (Program Files, `/usr/local/bin`, Mac lancé depuis Téléchargements…), seul **Télécharger** est proposé : remplacer l'ancien fichier par le nouveau. Hors ligne, rien ne s'affiche. Pour désactiver la vérification : `--sans-maj` (ou la variable `GREFFIER_SANS_MAJ=1`).

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
- Ou, sans toucher au fichier : **⋯ › ✎ Modifier les questions** (touche **Q**) dans la page de l'atelier, avant ou pendant la séance. On y modifie l'en-tête, les parties, sections, questions, ★ et relances, et on réordonne sections et questions par glisser-déposer (poignée ⠿) ; Greffier réécrit `questions.md` (l'ancien est gardé dans `sorties/historique/`) et renumérote les sections. Les notes déjà prises suivent leur question ; une question retirée qui avait des notes devient une page libre « (question retirée) … ».

### Ce que Greffier écrit dans le dossier de l'atelier
```
mon-atelier/
  questions.md    la source (à toi)
  sorties/        session.json, notes.md, photos/, historique/ (copie toutes les 10 min),
                  audio/ et transcription.md si l'atelier est enregistré
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
| Enregistrement audio | Écrit sur le disque toutes les 5 s : un plantage ne perd que les dernières secondes |

## Enregistrer et transcrire
Dans la page d'atelier (ou de notes libres), **🎙 Enregistrer** capte le micro du poste. La première fois, Greffier demande de confirmer que **les participants sont informés et d'accord**. Pendant l'enregistrement, le bouton devient rouge et affiche la durée ; un clic met en pause, un autre reprend. Une jauge à côté du bouton montre le niveau du micro dès l'ouverture de la page (si le navigateur a déjà autorisé le micro ; sinon un clic sur la jauge l'active), pour le vérifier avant de commencer : rien n'est enregistré tant qu'on n'a pas cliqué sur 🎙, et un clic sur la jauge ferme le micro. Le nom du micro utilisé s'affiche au démarrage ; si rien n'est capté pendant 15 s (micro muet, débranché ou mauvais micro choisi dans le navigateur), un bandeau rouge le signale avec un bouton **Relancer l'enregistrement**. Chaque question ou page affichée pendant l'enregistrement sert de repère : la transcription est découpée par question (ou par page en notes libres).

**Atelier en visio** (Teams, Meet…) : avec un casque, le micro n'entend que soi. Le bouton **🎧 Visio**, à côté de la jauge, capte aussi le son de l'ordinateur, donc la voix des participants ; le choix est retenu. Micro et visio sont enregistrés sur deux voies séparées (WAV stéréo : micro à gauche, visio à droite), si bien que la transcription indique qui parle : **Micro** (sur place) ou **Visio** (à distance). Activer ou couper la visio pendant l'enregistrement en ouvre un nouveau segment. Sous **Linux**, rien d'autre à faire (Firefox ou Chrome) : Greffier écoute la sortie audio par défaut (le casque). Sous **Windows et macOS** (Edge ou Chrome), le navigateur demande au démarrage de l'enregistrement quel écran partager : cocher **« Partager l'audio du système »**. Sans casque, le micro reprend aussi le son des haut-parleurs : les voix sont en double et la transcription distingue moins bien Micro et Visio.

Le son est écrit au fil de l'eau dans `sorties/audio/` (WAV 16 kHz mono, ~115 Mo par heure ; stéréo et le double avec la visio) et transcrit **sur le poste**, sans rien envoyer sur internet, **pendant l'atelier** : chaque question terminée (ou toutes les 2 min) est transcrite en arrière-plan, et le reste à la pause. L'avancement s'affiche au survol du bouton d'enregistrement et dans la page **📝 Compte rendu** (bouton **🎙 Transcrire** pour relancer). Poste trop lent pendant l'atelier : `GREFFIER_SANS_TRANSCRIPTION_DIRECTE=1` ne transcrit qu'à la demande. Le résultat, `sorties/transcription.md`, est joint automatiquement au prompt du compte rendu, où l'IA s'en sert pour compléter les notes (les notes priment). Une fois la transcription faite, **Supprimer l'audio** le déplace dans `sorties/audio/supprimes/`.

La transcription utilise [whisper.cpp](https://github.com/ggml-org/whisper.cpp), à installer une fois :
- **macOS** : `brew install whisper-cpp` ;
- **Windows** : télécharger le zip de la [dernière version](https://github.com/ggml-org/whisper.cpp/releases) et placer son contenu dans un dossier `whisper` à côté de Greffier ;
- **Linux** (paquets `git`, `cmake`, `g++`) :
  ```sh
  git clone --depth 1 https://github.com/ggml-org/whisper.cpp && cd whisper.cpp
  cmake -B build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF
  cmake --build build -j --target whisper-cli
  install -D build/bin/whisper-cli ~/.local/bin/whisper-cli
  ```

Au premier usage, Greffier télécharge le modèle de transcription et celui de détection de la voix dans le cache de l'utilisateur, une seule fois (sommes de contrôle vérifiées). Les silences sont sautés, ce qui accélère la transcription et évite que Whisper invente du texte dans les blancs ; l'audio est aussi découpé à chaque changement de question, pour que chaque passage soit rangé sous la bonne question. Durées indicatives, pour 1 h d'audio :
| Poste | Modèle par défaut | Durée |
|---|---|---|
| Mac Apple Silicon (GPU) | `large` (large-v3-turbo, 550 Mo) | quelques minutes |
| PC (processeur seul) | `small` (190 Mo) | de l'ordre de 30 min, moins s'il y a des silences |

Variables d'environnement : `GREFFIER_WHISPER` (chemin de `whisper-cli`), `GREFFIER_MODELE_WHISPER` (`small`, `large`, ou le chemin d'un modèle ggml). Limites : les intervenants ne sont pas identifiés (en visio, seulement « sur place » ou « à distance »), et un micro de salle donne de bien meilleurs résultats que celui d'un portable en bout de table. Enregistrer demande Chrome ou Edge, avec la page ouverte par Greffier (pas en double-clic).

## Le compte rendu par IA
Un prompt commun ([`prompt-compte-rendu.md`](prompt-compte-rendu.md)) et un modèle ([`modele-compte-rendu.md`](modele-compte-rendu.md), ou [`modele-compte-rendu-libre.md`](modele-compte-rendu-libre.md) pour des notes libres) imposent la structure du CR, interdisent d'inventer et font marquer **[À CONFIRMER]** ce qui est ambigu.

1. Page d'atelier → **📝 Compte rendu** (la page suit 3 étapes : transcription s'il y a un enregistrement, rédaction, relecture et PDF) :
   - **✨ Rédiger avec Claude** (si [Claude Code](https://claude.com/claude-code) est installé et connecté) : Greffier lance `claude -p` avec l'abonnement Claude, sans clé API. Claude ne peut que lire les fichiers de `sorties/` (pour décrire les photos) ; le CR s'affiche dans la page et est enregistré dans `sorties/CR-<atelier>.md`. Compter 1 à 3 minutes. Claude Code introuvable alors qu'il est installé : indiquer son chemin dans la variable `GREFFIER_CLAUDE`.
   - sinon **🤖 Copier pour une autre IA** → coller dans Gemini, ChatGPT ou Claude, puis coller la réponse dans la page.
2. Aperçu A4 avec les photos, [À CONFIRMER] surlignés et comptés.
3. Corriger, puis **🖨 Imprimer / PDF**. Les corrections sont **enregistrées au fil de la frappe** dans `sorties/CR-<atelier>.md` (💾 en haut de la page). Si la copie gardée par le navigateur diffère du fichier à l'ouverture (fichier modifié à la main, autre poste…), la page demande laquelle garder ; l'autre version, comme la précédente à chaque nouvelle rédaction par Claude, est mise de côté dans `sorties/historique/`.

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
| `winres/`, `rsrc_windows_*.syso` | icône et propriétés de l'exe Windows ; `tray.ico`, `tray.png` : icône de la zone de notification (barre des menus sur Mac) (régénérer : `go run github.com/tc-hib/go-winres@v0.3.3 simply --icon winres/icon.png --manifest cli --arch amd64,arm64`) |
| `.github/workflows/binaires.yml` | GitHub Action : tests et binaires à chaque push ; *Release* à chaque étiquette `v*` |

**Publier une version** : `git tag v1.0.0 && git push --tags` → l'Action crée la *Release* avec les binaires.

## Licence
[MIT](LICENSE) © DecideOm. Le logo et la marque DecideOm ne sont pas couverts par la licence MIT. Polices Poppins et Open Sans sous licence SIL OFL 1.1.
