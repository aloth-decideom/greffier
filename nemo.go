package main

// NeMo-Speech.cpp (séparation des voix, voix.go) sans installation : au premier usage, Greffier
// télécharge la version officielle pour le système (GitHub de NVIDIA, 5 à 7 Mo) et le modèle
// Nemotron 3 Diarization (Hugging Face, 107 Mo), sommes de contrôle vérifiées, dans le cache de
// l'utilisateur. Une version installée à la main (GREFFIER_NEMO, PATH, à côté de Greffier) reste prioritaire.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

const versionNemo = "v0.2.0"

// Archives publiées par NVIDIA (CPU ; Metal sur Mac Apple Silicon). Windows ARM exécute la version x64.
var archivesNemo = map[string]archiveOutil{
	"linux/amd64": {modeleWhisper{"nemo-speech-0.2.0-linux-x86_64-cpu.tar.gz", "f396057150f1b774935c7414fd32ebc04195a795a34120d78d8c9ecefa1b7507", depotNemo, 6683477},
		"nemo-speech-0.2.0-linux-x86_64-cpu/bin/nemo-speech"},
	"linux/arm64": {modeleWhisper{"nemo-speech-0.2.0-linux-aarch64-cpu.tar.gz", "ac50694d4729f9eae390a9851d38f51269d9af9829b535325539090a3b95c787", depotNemo, 6309769},
		"nemo-speech-0.2.0-linux-aarch64-cpu/bin/nemo-speech"},
	"darwin/arm64": {modeleWhisper{"nemo-speech-0.2.0-macos-aarch64-metal.tar.gz", "5cb02ba7c04f0b5585ce5cde9c830be5f0b7dfb4c83083c500109c381b4f2da9", depotNemo, 5031510},
		"nemo-speech-0.2.0-macos-aarch64-metal/bin/nemo-speech"},
	"darwin/amd64": {modeleWhisper{"nemo-speech-0.2.0-macos-x86_64-cpu.tar.gz", "d52c318d8197511d4f7eee02bcf05511f7e91b300555a130b562e571695a0f39", depotNemo, 5256846},
		"nemo-speech-0.2.0-macos-x86_64-cpu/bin/nemo-speech"},
	"windows/amd64": {modeleWhisper{"nemo-speech-0.2.0-windows-x86_64-cpu.zip", "82c80451086e86194aba9af19f800da77cd66d1c28d993d0efceffd6cdb49bb8", depotNemo, 5528418},
		"nemo-speech-0.2.0-windows-x86_64-cpu/bin/nemo-speech.exe"},
	"windows/arm64": {modeleWhisper{"nemo-speech-0.2.0-windows-x86_64-cpu.zip", "82c80451086e86194aba9af19f800da77cd66d1c28d993d0efceffd6cdb49bb8", depotNemo, 5528418},
		"nemo-speech-0.2.0-windows-x86_64-cpu/bin/nemo-speech.exe"},
}

const depotNemo = "https://github.com/NVIDIA/NeMo-Speech.cpp/releases/download/" + versionNemo + "/"

// Modèle de séparation des voix (révision figée du dépôt Hugging Face de NVIDIA).
var modeleDiarisation = modeleWhisper{"Nemotron-3-Diarization.q8_0.gguf", "08456d9e22cd9a323c0364d98375f3746d6e68507ebb705cd46438c534c7a3a1",
	"https://huggingface.co/nvidia/Nemotron-3-Diarization/resolve/f667ed73aee57d40cc39428eb768b4fd87a0a29e/", 107012128}

// nemoDisponible : nemo-speech installé, ou téléchargeable pour ce système.
func nemoDisponible() bool {
	_, ok := pourCeSysteme(archivesNemo)
	return ok || cheminNemo() != ""
}

// assurerNemo : nemo-speech et le modèle de séparation des voix, téléchargés s'ils manquent.
// etape(libellé, pourcent) suit les téléchargements.
func assurerNemo(ctx context.Context, etape func(string, int)) (nemo, modele string, err error) {
	if nemo = cheminNemo(); nemo == "" {
		a, ok := pourCeSysteme(archivesNemo)
		if !ok {
			return "", "", fmt.Errorf("NeMo-Speech.cpp n'existe pas pour ce système")
		}
		// décompressée directement dans cache/greffier (dossier nemo-speech-<version>-<système>)
		nemo, err = installerOutil(ctx, a, "", func(p int) { etape("Téléchargement de NeMo-Speech (une seule fois)", p) })
		if err != nil {
			return "", "", fmt.Errorf("installation de NeMo-Speech : %w", err)
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	modele = filepath.Join(cache, "greffier", "voix", modeleDiarisation.Nom)
	if _, err := os.Stat(modele); err != nil {
		err := telechargerModele(ctx, modeleDiarisation, modele, func(p int) {
			etape("Téléchargement du modèle de séparation des voix (107 Mo, une seule fois)", p)
		})
		if err != nil {
			return "", "", err
		}
	}
	return nemo, modele, nil
}
