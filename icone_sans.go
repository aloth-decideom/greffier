//go:build darwin && !cgo

package main

// macOS compilé sans cgo (compilation croisée depuis Linux) : pas d'icône dans la barre
// des menus, la fenêtre du Terminal et les boutons des pages suffisent.

const iconeDisponible = false

func lancerIcone(s *Serveur) {}

func quitterIcone() {}
