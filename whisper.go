package main

// whisper-cli sans installation : au premier enregistrement, Greffier télécharge whisper.cpp pour
// le système (outils.go), une version figée : binaires officiels de whisper.cpp pour Windows et
// Linux, binaires compilés par ce dépôt pour macOS (workflow whisper-macos.yml, pré-release
// outils-whisper-<version>), que whisper.cpp ne publie pas. Une installation existante
// (GREFFIER_WHISPER, PATH, Homebrew, dossier whisper à côté de Greffier) reste prioritaire.

import (
	"context"
	"errors"
)

const buildWhisper = "b5454" // whisper.cpp 1.9.5

const (
	depotWhisperCpp = "https://github.com/ggml-org/whisper.cpp/releases/download/" + buildWhisper + "/"
	depotWhisperMac = "https://github.com/aloth-decideom/greffier/releases/download/outils-whisper-" + buildWhisper + "-2/"
)

var archivesWhisper = map[string]archiveOutil{
	"linux/amd64": {modeleWhisper{"whisper-bin-ubuntu-x64.tar.gz", "a72becf15d7917f990f6313867a52638b82b7f9ef237fb0c980dac56a135781c", depotWhisperCpp, 10364195},
		"whisper-bin-ubuntu-x64/whisper-cli"},
	"linux/arm64": {modeleWhisper{"whisper-bin-ubuntu-arm64.tar.gz", "6b95ebfc60447df48e70ef00a73bdc3f41679ed2d01ef465827206a2ff325149", depotWhisperCpp, 4608377},
		"whisper-bin-ubuntu-arm64/whisper-cli"},
	"windows/amd64": {modeleWhisper{"whisper-bin-x64.zip", "6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb", depotWhisperCpp, 8928640},
		"Release/whisper-cli.exe"},
	"windows/arm64": {modeleWhisper{"whisper-bin-win-cpu-arm64.zip", "28c37e7b598c3d9bbfef94f3bd67f2ee6f12b7c86f3da5edcf5e4308d615ff6d", depotWhisperCpp, 4371193},
		"Release/whisper-cli.exe"},
	"darwin/arm64": {modeleWhisper{"whisper-cli-macos-arm64.tar.gz", "0d0ef9829a15f2bda3ec2fa095f54adedb1d89b46954c63bdbf2173d4ccbd4fc", depotWhisperMac, 2097072},
		"whisper-cli-macos-arm64/whisper-cli"},
	"darwin/amd64": {modeleWhisper{"whisper-cli-macos-x86_64.tar.gz", "05fc941b7b994e8b5f57d2d385efe1295b1b72ad25f44729c3f852b462493042", depotWhisperMac, 1315757},
		"whisper-cli-macos-x86_64/whisper-cli"},
}

// whisperDisponible : whisper-cli installé, ou téléchargeable pour ce système.
func whisperDisponible() bool {
	_, ok := pourCeSysteme(archivesWhisper)
	return ok || cheminWhisper() != ""
}

// assurerWhisper : whisper-cli, téléchargé s'il n'est pas installé.
func assurerWhisper(ctx context.Context, progres func(int)) (string, error) {
	if p := cheminWhisper(); p != "" {
		return p, nil
	}
	a, ok := pourCeSysteme(archivesWhisper)
	if !ok {
		return "", errors.New("whisper.cpp n'existe pas pour ce système : l'installer à la main (voir le README)")
	}
	return installerOutil(ctx, a, "whisper-"+buildWhisper, progres)
}
