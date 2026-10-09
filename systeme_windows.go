//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// Console Windows : accents en UTF-8 et titre explicite dans la barre des tâches.
func initConsole() {
	k := syscall.NewLazyDLL("kernel32.dll")
	_, _, _ = k.NewProc("SetConsoleOutputCP").Call(65001)
	if titre, err := syscall.UTF16PtrFromString(titreConsole); err == nil {
		_, _, _ = k.NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(titre)))
	}
}

// Évite qu'une fenêtre PowerShell noire apparaisse pendant le sélecteur de dossier.
func cacherFenetre(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
