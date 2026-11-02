package outils

// whisper-cli sans installation : au premier enregistrement, Greffier télécharge whisper.cpp pour
// le système, une version figée : binaires officiels de whisper.cpp pour Windows et
// Linux, binaires compilés par ce dépôt pour macOS (workflow whisper-macos.yml, pré-release
// outils-whisper-<version>), que whisper.cpp ne publie pas. Une installation existante
// (GREFFIER_WHISPER, PATH, Homebrew, dossier whisper à côté de Greffier) reste prioritaire.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/aloth-decideom/greffier/internal/systeme"
)

const buildWhisper = "b5454" // whisper.cpp 1.9.5

const (
	depotWhisperCpp = "https://github.com/ggml-org/whisper.cpp/releases/download/" + buildWhisper + "/"
	depotWhisperMac = "https://github.com/aloth-decideom/greffier/releases/download/outils-whisper-" + buildWhisper + "-2/"
)

var archivesWhisper = map[string]archiveOutil{
	"linux/amd64": {Fichier{"whisper-bin-ubuntu-x64.tar.gz", "a72becf15d7917f990f6313867a52638b82b7f9ef237fb0c980dac56a135781c", depotWhisperCpp, 10364195},
		"whisper-bin-ubuntu-x64/whisper-cli"},
	"linux/arm64": {Fichier{"whisper-bin-ubuntu-arm64.tar.gz", "6b95ebfc60447df48e70ef00a73bdc3f41679ed2d01ef465827206a2ff325149", depotWhisperCpp, 4608377},
		"whisper-bin-ubuntu-arm64/whisper-cli"},
	"windows/amd64": {Fichier{"whisper-bin-x64.zip", "6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb", depotWhisperCpp, 8928640},
		"Release/whisper-cli.exe"},
	"windows/arm64": {Fichier{"whisper-bin-win-cpu-arm64.zip", "28c37e7b598c3d9bbfef94f3bd67f2ee6f12b7c86f3da5edcf5e4308d615ff6d", depotWhisperCpp, 4371193},
		"Release/whisper-cli.exe"},
	"darwin/arm64": {Fichier{"whisper-cli-macos-arm64.tar.gz", "0d0ef9829a15f2bda3ec2fa095f54adedb1d89b46954c63bdbf2173d4ccbd4fc", depotWhisperMac, 2097072},
		"whisper-cli-macos-arm64/whisper-cli"},
	"darwin/amd64": {Fichier{"whisper-cli-macos-x86_64.tar.gz", "05fc941b7b994e8b5f57d2d385efe1295b1b72ad25f44729c3f852b462493042", depotWhisperMac, 1315757},
		"whisper-cli-macos-x86_64/whisper-cli"},
}

var whisper = outil{nom: "whisper-cli", env: "GREFFIER_WHISPER", dossier: "whisper", archives: archivesWhisper}

// CheminWhisper : whisper-cli installé (GREFFIER_WHISPER, PATH, à côté de Greffier…), ou "".
func CheminWhisper() string { return whisper.chemin() }

// WhisperDisponible : whisper-cli installé, ou téléchargeable pour ce système.
func WhisperDisponible() bool { return whisper.disponible() }

// AssurerWhisper : whisper-cli, téléchargé s'il n'est pas installé.
func AssurerWhisper(ctx context.Context, progres func(int)) (string, error) {
	if p := whisper.chemin(); p != "" {
		return p, nil
	}
	a, ok := pourCeSysteme(archivesWhisper)
	if !ok {
		return "", errors.New("whisper.cpp n'existe pas pour ce système : l'installer à la main (voir le README)")
	}
	return installer(ctx, a, "whisper-"+buildWhisper, progres)
}

// ---------------------------------------------------------------- modèles

// Modèles proposés (dépôt HuggingFace de whisper.cpp). Sur CPU, l'encodeur du modèle « large »
// demande ~1 min 30 par tranche de 30 s : trop lent pour un atelier de 2 h. On prend donc
// « small » (~15 s par tranche), sauf sur Mac Apple Silicon où whisper.cpp calcule sur le GPU.
const depotModeles = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"

var modelesWhisper = map[string]Fichier{
	"small": {"ggml-small-q5_1.bin", "ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb", depotModeles, 190085487},
	"large": {"ggml-large-v3-turbo-q5_0.bin", "394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2", depotModeles, 574041195},
}

// Détection de la voix (Silero, < 1 Mo) : whisper-cli saute les silences. C'est deux fois plus
// rapide sur un atelier réel, et Whisper n'invente plus de texte dans les blancs.
var modeleVAD = Fichier{"ggml-silero-v6.2.0.bin", "2aa269b785eeb53a82983a20501ddf7c1d9c48e33ab63a41391ac6c9f7fb6987",
	"https://huggingface.co/ggml-org/whisper-vad/resolve/main/", 885098}

// AssurerModele : le modèle de transcription, téléchargé s'il manque.
// GREFFIER_MODELE_WHISPER = « small », « large » ou le chemin d'un modèle ggml déjà présent ;
// sinon « large » sur Mac Apple Silicon et « small » ailleurs.
func AssurerModele(ctx context.Context, progres func(pourcent int)) (string, error) {
	choix := os.Getenv("GREFFIER_MODELE_WHISPER")
	if choix == "" {
		choix = "small"
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			choix = "large"
		}
	}
	m, connu := modelesWhisper[choix]
	if !connu { // chemin fourni : rien à télécharger
		if _, err := os.Stat(choix); err != nil {
			return "", fmt.Errorf("modèle introuvable (GREFFIER_MODELE_WHISPER) : %s", choix)
		}
		return choix, nil
	}
	return enCache(ctx, m, "whisper", progres)
}

// AssurerVAD : le modèle de détection de la voix, si whisper-cli sait s'en servir (versions
// récentes) ; "" sinon, et la transcription se fait sans sauter les silences.
func AssurerVAD(ctx context.Context, whisper string) string {
	aide, _ := exec.CommandContext(ctx, whisper, "--help").CombinedOutput()
	if !strings.Contains(string(aide), "--vad-model") {
		return ""
	}
	chemin, err := enCache(ctx, modeleVAD, "whisper", nil)
	if err != nil {
		systeme.Logf("⚠ %v : transcription sans détection des silences", err)
		return ""
	}
	return chemin
}
