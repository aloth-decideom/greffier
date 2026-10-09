# Charte graphique DecideOm

Source : modèle Google Slides interne DecideOm (« TemplateDecideOm2026 », slides « Charte graphique »).
Valeurs reprises dans `app/theme.css` (variables CSS), `app/document.css` (documents A4) et `marp/decideom.css` (slides).

## Polices
| Usage | Police | Couleur | Interligne |
|---|---|---|---|
| Titres | **Poppins** | gris foncé `#474747` | simple |
| Contenus | **Open Sans** | gris `#7f7f7f` | simple |

Les deux polices sont embarquées dans `app/fonts/` (sous-ensemble latin, licence OFL) : aucun appel réseau.

## Couleurs
| Rôle | Valeur | Variable CSS |
|---|---|---|
| Rouge principal (logo) | `#ea232f` | `--rouge` |
| Rouge foncé (logo) | `#ad151e` | `--rouge-fonce` |
| Gris (logo) | `#bcbdbf` | `--gris-logo` |
| Pictogrammes | `rgb(200, 60, 60)` = `#c83c3c` | `--rouge-icone` |
| Schème offre, du bleu au rouge | `#4C5E8B` `#7c5f7f` `#9b6075` `#ac5e62` `#ca5a56` `#e95041` | `--offre-1` … `--offre-6` |
| Gouvernance | `#98B3D7` | `--gouvernance` |

## Codes visuels du modèle
- **9 pastilles rouges** (3 × 3) devant les titres → classe `.dots9`.
- **Titres sur deux niveaux** : 1re ligne fine, 2e ligne en gras (« Charte graphique / **Police** ») → classe `.titre-dm`.
- **Cartouches blancs** avec ombre douce → classe `.cartouche`.
- **Filet rose fin** horizontal (`#e8a3a8`) → variable `--filet`.
- **Numéro de page** en bas à gauche, au format « 05. » ; **logo centré en bas** ; filet rouge en pied de slide.
- Logos : `app/img/logo-decideom.png` (couleur, fond clair) et `app/img/logo-decideom-blanc.png` (fonds sombres).

## Écarts assumés
- Le texte long affiché à l'écran ou projeté (réponses saisies, CR) utilise `#595959` (`--texte-fort`) au lieu de `#7f7f7f` : le gris de la charte est trop clair pour une lecture prolongée ou au vidéoprojecteur.
- Les couleurs des tags (Décision, Action…) sont tirées de la palette « offre » ; « À creuser » utilise un orange `#c97a12`, absent de la charte, pour rester distinct du rouge « Risque ».
