---
description: Rédige le compte rendu d'un atelier à partir de l'export des notes, puis le convertit en PDF
argument-hint: <dossier-atelier> [chemin vers notes.md]  ex. atelier-01
---

Rédige le compte rendu de l'atelier `$ARGUMENTS`.

1. **Repère les fichiers** :
   - les consignes de rédaction : `<atelier>/prompt-cr.md` (s’il manque : `greffier --generer <atelier>`, ou `go run . --generer <atelier>` depuis ce dépôt). Suis-les à la lettre : ce sont les mêmes que celles données à Gemini, le résultat doit avoir le même format ;
   - les notes : le chemin donné en 2e argument, sinon le `notes.md` le plus récent sous `<atelier>/sorties/` (export .zip dézippé). Si tu ne trouves qu'un `.zip`, dézippe-le dans un sous-dossier de `sorties/`. Si rien n'est trouvé, arrête-toi et demande l'export.
2. **Lis les photos** référencées dans les notes (`photos/…`) pour décrire les schémas, comme le prévoit la règle 9.
3. **Écris** le compte rendu dans `<atelier>/sorties/CR-<atelier>.md`, dans le même dossier que `notes.md` et `photos/` pour que les liens des images restent valides.
4. **Convertis en PDF** : `python3 cr2pdf.py <atelier>/sorties/<…>/CR-<atelier>.md --client <client>` (ou la page `outil/cr.html` → Imprimer / PDF).
5. **Rends compte** en quelques lignes : chemin du PDF, nombre de décisions, d'actions et de risques, et la liste des mentions [À CONFIRMER] à faire valider (une ligne chacune, avec la section).
