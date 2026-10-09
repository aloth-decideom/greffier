# Mode opératoire : animer et prendre les notes en même temps

## Le principe
- **Une source unique** : `<atelier>/questions.md`.
- **Un outil** : **Greffier** (double-clic, puis choix du dossier de l'atelier). On y affiche la question, on saisit la réponse et on ajoute les photos du tableau. Tout est **enregistré en continu** dans `<atelier>/sorties/` (témoin « 💾 » en haut de la page).
- **Deux écrans** (conseillé) : ton PC garde la fenêtre d'animation (sommaire, relances, chrono). L'**écran projeté** (touche **P**) affiche une version épurée sur le vidéoprojecteur.

Chaque réponse peut porter des **tags**, qui alimentent automatiquement la synthèse et le compte rendu :

| Tag | Quand l'utiliser | Dans le CR |
|---|---|---|
| **Décision** | Un choix est acté en séance | Tableau « Décisions » |
| **Action** | Quelqu'un doit faire quelque chose (renseigner **qui** et **échéance**) | Tableau « Actions » |
| **Risque** | Point de vigilance, blocage, dette | « Risques », base du rapport d'étonnement |
| **À creuser** | Réponse incomplète, à reprendre | « Points ouverts » |
| **Hors périmètre** | Intéressant, mais pas pour cette mission | « Parking » |

Les questions marquées `*` dans `questions.md` (affichées ★) sont prioritaires : si le temps manque, on traite celles-là.

## Photos (schéma au tableau, document montré en séance…)
Trois façons de les ajouter à la question affichée :
1. **Bouton « 📷 Ajouter une photo »** ou touche **I** : on choisit le fichier.
2. **Glisser-déposer** le fichier sur la page.
3. **Ctrl+V** : colle une image copiée (capture d'écran, photo copiée depuis une autre appli).

Les photos sont réduites automatiquement (environ 300 Ko), enregistrées dans `sorties/photos/`, légendables et cliquables pour les agrandir. Elles apparaissent sur l'écran projeté et dans tous les exports.

**Faire passer la photo du téléphone au PC**, au choix : câble USB, partage Bluetooth, KDE Connect / GSConnect (Linux), Quick Share ou Phone Link (Windows), AirDrop (Mac), ou une synchronisation cloud si on a du réseau. Le plus simple en séance : prendre les photos à la pause et les glisser toutes d'un coup sur les bonnes questions.

## Avant l'atelier (J-3)
1. Envoyer `ordre-du-jour.md`, avec la liste des documents à préparer.
2. Ajuster `questions.md` (ajouter ou retirer des questions et des `*`) ; recharger la page de l'atelier pour voir le résultat.
3. **Répétition** : lancer Greffier, ouvrir l'atelier, vérifier le témoin « 💾 », appuyer sur **P**, glisser la 2e fenêtre sur l'écran externe, **F** pour le plein écran. Saisir une réponse, coller une image, vérifier qu'elles apparaissent dans `sorties/`, puis « Nouvelle session » (la répétition est gardée dans `sorties/historique/`).
4. Vérifier que le navigateur autorise les pop-ups pour cette page (sinon l'écran projeté ne s'ouvre pas).

## Pendant l'atelier
- **Rôles** : l'un anime et relance, l'autre tient le clavier. En solo, noter en mots-clés seulement.
- Noter les **faits, chiffres, noms et irritants**. Les citations marquantes vont entre guillemets.
- Taguer **immédiatement** les décisions, actions et risques.
- Une question qui déborde : tag « À creuser », et on avance. Le chrono passe au rouge en cas de retard sur le minutage.
- Un sujet sensible : **C** masque les réponses sur l'écran projeté uniquement.
- Un **sujet imprévu**, hors des questions : touche **N** (ou « ＋ Page »). On ouvre une **page libre**, avec un titre facultatif. Elle apparaît dans « Hors questions » du sommaire et dans les notes, et l'IA la rattache au bon thème du CR.
- **Garder la fenêtre de Greffier ouverte** : c'est elle qui écrit sur le disque. Si le témoin passe au rouge, cliquer dessus pour voir le message.
- **5 dernières minutes** : touche **S**. La synthèse (décisions, actions, risques) s'affiche aussi à l'écran projeté, et on la fait valider en séance.

**En fin de séance** : **⏻ Arrêter l'application** (en bas de la barre latérale). Les notes restent dans `sorties/`.

## Après l'atelier
1. **Le soir même** : rien à exporter, `sorties/` contient déjà `notes.md`, `photos/` et `session.json`.
   Optionnel : « 🖨 Notes brutes PDF » pour diffuser les notes en interne ; « Exporter l'atelier (.zip) » pour envoyer l'ensemble à un collègue.
2. **J+1, rédaction par l'IA** : « 📝 Compte rendu (IA → PDF) », puis « 🤖 Copier pour l'IA » et coller dans **Gemini**. Le prompt impose le modèle de CR, interdit d'inventer et fait marquer **[À CONFIRMER]** ce qui est ambigu.
   Avec Claude Code : `/compte-rendu <atelier>` fait tout, jusqu'au PDF.
3. **Relecture** : coller la réponse dans `cr.html`, traiter chaque [À CONFIRMER] (ils sont surlignés et comptés), vérifier les actions (qui, quand) et les chiffres.
4. **PDF** : « 🖨 Imprimer / PDF », puis « Enregistrer en PDF » ; envoi sous 48 h. Garder aussi le « ⬇ CR .md » dans `sorties/`.

L'IA produit aussi, dans le CR :
- un premier jet du **rapport d'étonnement** (garder / refondre / abandonner) ;
- les **points ouverts** à reprendre dans l'atelier suivant ;
- les premiers **cas d'usage**, pour la matrice de priorisation.

## Notes libres (sans questions préparées)
Pour une réunion, un entretien imprévu ou un point d'avancement : sur la page d'accueil de Greffier, **✏️ Notes libres**, un titre, puis **Démarrer**.
- Greffier crée un dossier daté dans `Documents/Greffier/` (modifiable avec « Changer… »). Ce dossier contient un `questions.md` réduit à son en-tête, `mode: libre`.
- **Une page par sujet** : on change de page avec **N**, ou **Ctrl+Entrée** depuis la dernière page. Le titre est facultatif. Découper ainsi aide l'IA à structurer le CR.
- **Trop de pages ?** Dans une page vide, **Retour arrière** la supprime et ramène à la précédente. Sinon : 🗑 en haut, la croix au survol dans le sommaire, ou **Suppr**. Une confirmation est demandée si la page contient des notes, et une copie va dans l'historique. Les pages vides n'apparaissent ni dans les notes exportées ni dans le prompt du CR.
- Tags, photos, sauvegarde continue, écran projeté et synthèse fonctionnent comme pour un atelier.
- **Le CR** suit le même chemin (« 📝 Compte rendu ») avec un modèle de compte rendu de réunion : synthèse, sujets abordés, décisions, actions, risques, prochaines étapes. Compléter `mission`, `animateurs` et `contexte` dans `questions.md` améliore le résultat.

## Raccourcis clavier (hors du champ de saisie)
| Touche | Effet |
|---|---|
| ← → · PgUp PgDn | Question précédente / suivante (télécommande) |
| Entrée / E | Saisir la réponse (Échap pour sortir, Ctrl+Entrée pour passer à la suivante) |
| 1 … 5 (Alt+1 … 5 pendant la saisie) | Tags |
| I | Ajouter une photo |
| P | Ouvrir l'écran projeté |
| S | Synthèse |
| C | Masquer les réponses sur l'écran projeté |
| R · M · F · T | Relances · sommaire · plein écran · chrono |
| X · V | Question sautée · à revoir |
| N (Ctrl+Maj+Entrée pendant la saisie) | Nouvelle page libre |
| Suppr (Retour arrière dans une page vide) | Supprimer la page libre |
