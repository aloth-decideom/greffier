package main

// Intégration au système : sélecteur de dossier natif, navigateur, ateliers récents.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const titreConsole = "Greffier DecideOm - fermer cette fenêtre arrête l'application"

func logf(format string, args ...any) {
	fmt.Printf("%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func nomSysteme() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

var errAnnule = errors.New("annulé")

// choisirDossier ouvre le sélecteur de dossier du système et renvoie le chemin choisi.
// creation : autoriser la création d'un nouveau dossier depuis le sélecteur (Windows).
func choisirDossier(invite string, creation bool) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Fenêtre Windows standard, forcée au premier plan (sinon elle s'ouvre derrière le navigateur).
		ps := `[Console]::OutputEncoding=[Text.Encoding]::UTF8;` +
			`Add-Type -AssemblyName System.Windows.Forms;` +
			`$o=New-Object System.Windows.Forms.Form -Property @{TopMost=$true;ShowInTaskbar=$false};` +
			`$f=New-Object System.Windows.Forms.FolderBrowserDialog;` +
			`$f.Description='` + strings.ReplaceAll(invite, "'", "''") + `';` +
			`$f.ShowNewFolderButton=$` + strconv.FormatBool(creation) + `;` +
			`if($f.ShowDialog($o) -eq 'OK'){[Console]::Out.Write($f.SelectedPath)}`
		cmd = exec.Command("powershell", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", ps)
	case "darwin":
		cmd = exec.Command("osascript", "-e",
			`POSIX path of (choose folder with prompt "`+strings.ReplaceAll(invite, `"`, `'`)+`")`)
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.Command(p, "--file-selection", "--directory", "--title="+invite)
		} else if p, err := exec.LookPath("kdialog"); err == nil {
			cmd = exec.Command(p, "--getexistingdirectory", os.Getenv("HOME"), "--title", invite)
		} else {
			return "", errors.New("aucun sélecteur de dossier disponible (installer zenity ou kdialog) : coller le chemin à la main")
		}
	}
	cacherFenetre(cmd)
	out, err := cmd.Output()
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		if err != nil && runtime.GOOS == "windows" {
			return "", fmt.Errorf("sélecteur indisponible (%v) : coller le chemin à la main", err)
		}
		return "", errAnnule
	}
	return filepath.Clean(dir), nil
}

func ouvrirNavigateur(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		logf("Ouvre cette adresse dans ton navigateur : %s", url)
	}
}

// ---------------------------------------------------------------- ateliers récents

func fichierRecents() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "greffier", "recents.json")
}

func chargerRecents() []string {
	var l []string
	if raw, err := os.ReadFile(fichierRecents()); err == nil {
		_ = json.Unmarshal(raw, &l)
	}
	return l
}

func ajouterRecent(dir string) {
	l := []string{dir}
	for _, d := range chargerRecents() {
		if d != dir && len(l) < 12 {
			l = append(l, d)
		}
	}
	raw, _ := json.MarshalIndent(l, "", " ")
	_ = os.MkdirAll(filepath.Dir(fichierRecents()), 0o755)
	_ = os.WriteFile(fichierRecents(), raw, 0o644)
}

// dossiersDeRecherche : où chercher des ateliers (sous-dossiers contenant questions.md).
func dossiersDeRecherche() []string {
	var l []string
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		l = append(l, filepath.Dir(exe), filepath.Dir(filepath.Dir(exe)))
	}
	if wd, err := os.Getwd(); err == nil {
		l = append(l, wd)
	}
	return append(l, dossierNotesParDefaut())
}

// dossierNotesParDefaut : où sont créées les notes libres (Documents/Greffier, sinon ~/Greffier).
func dossierNotesParDefaut() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "Greffier")
	}
	if st, err := os.Stat(filepath.Join(home, "Documents")); err == nil && st.IsDir() {
		return filepath.Join(home, "Documents", "Greffier")
	}
	return filepath.Join(home, "Greffier")
}
