//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// Titre de la fenêtre du Terminal (macOS, Linux), seulement si on écrit dans un terminal.
func initConsole() {
	if st, err := os.Stdout.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		fmt.Printf("\033]0;%s\007", titreConsole)
	}
}

func cacherFenetre(cmd *exec.Cmd) {}
