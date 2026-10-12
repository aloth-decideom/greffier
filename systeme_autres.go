//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Titre de la fenêtre du Terminal (macOS, Linux), seulement si on écrit dans un terminal.
func initConsole() {
	if st, err := os.Stdout.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		fmt.Printf("\033]0;%s\007", titreConsole)
	}
}

func cacherFenetre(cmd *exec.Cmd) {}

// Nouvelle version lancée par une mise à jour : elle survit à la fermeture du Terminal de l'ancienne.
func detacher(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
