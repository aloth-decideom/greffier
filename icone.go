//go:build !darwin || cgo

package main

// Icône dans la zone de notification (barre des tâches Windows, barre d'état Linux,
// barre des menus macOS) : rouvrir la page si le navigateur a été fermé, arrêter Greffier.
// Sous macOS, elle demande cgo (binaires compilés sur un Mac par la GitHub Action) ; sans cgo, voir icone_sans.go.

import (
	_ "embed"
	"errors"
	"runtime"

	"fyne.io/systray"

	"github.com/aloth-decideom/greffier/internal/maj"
	"github.com/aloth-decideom/greffier/internal/systeme"
)

const iconeDisponible = true

var (
	//go:embed winres/tray.ico
	iconeICO []byte
	//go:embed winres/tray.png
	iconePNG []byte
)

// lancerIcone bloque jusqu'à quitterIcone. Si l'icône est impossible (Linux sans bus de
// session…), elle rend la main et Greffier continue avec la console seule.
func lancerIcone(s *Serveur) {
	defer func() { _ = recover() }()
	systray.Run(func() {
		if runtime.GOOS == "windows" {
			systray.SetIcon(iconeICO)
		} else {
			systray.SetIcon(iconePNG)
		}
		systray.SetTooltip("Greffier DecideOm — " + s.origin + "/")
		ouvrir := systray.AddMenuItem("Ouvrir Greffier", "Ouvre la page dans le navigateur")
		accueil := systray.AddMenuItem("Choisir un autre atelier", "Page d'accueil de Greffier")
		accueil.Hide() // tant qu'aucun atelier n'est ouvert, « Ouvrir Greffier » mène déjà à l'accueil
		adresse := systray.AddMenuItem("Adresse : "+s.origin+"/", "À taper dans le navigateur")
		adresse.Disable()
		systray.AddSeparator()
		avisMaj := systray.AddMenuItem("", "")
		avisMaj.Hide() // affiché si GitHub annonce une nouvelle version
		arreter := systray.AddMenuItem("Arrêter Greffier", "Termine les écritures en cours puis arrête l'application")

		s.siOuverture(func(titre string) {
			ouvrir.SetTitle("Ouvrir l'atelier : " + titre)
			accueil.Show()
		})
		systray.SetOnTapped(func() { systeme.OuvrirNavigateur(s.adressePage()) }) // clic gauche
		// Avis de nouvelle version, mis à jour à chaque vérification (toutes les 4 h)
		go func() {
			<-s.Maj.Pret()
			for {
				if info := s.Maj.Info(); info.Nouvelle && !s.majEnCours.Load() {
					titreMaj(avisMaj, info)
					avisMaj.Show()
				}
				<-s.Maj.Changements()
			}
		}()
		// onReady doit rendre la main : le menu n'est publié qu'après.
		go func() {
			for {
				select {
				case <-avisMaj.ClickedCh:
					info := s.Maj.Info()
					if !info.Auto {
						systeme.OuvrirNavigateur(info.Page)
						break
					}
					avisMaj.SetTitle("Mise à jour en cours…")
					avisMaj.Disable()
					go func() {
						if err := s.mettreAJour(); err != nil && !errors.Is(err, errMajEnCours) {
							titreMaj(avisMaj, s.Maj.Info())
							avisMaj.Enable()
							systeme.OuvrirNavigateur(info.Page) // page de la release : téléchargement à la main
						}
					}()
				case <-ouvrir.ClickedCh:
					systeme.OuvrirNavigateur(s.adressePage())
				case <-accueil.ClickedCh:
					systeme.OuvrirNavigateur(s.origin + "/")
				case <-arreter.ClickedCh:
					systeme.Logf("Arrêt demandé depuis l'icône Greffier.")
					s.demanderArret()
				}
			}
		}()
	}, nil)
}

func titreMaj(item *systray.MenuItem, info maj.InfoVersion) {
	if info.Auto {
		item.SetTitle("⬆ Installer Greffier " + info.Derniere + " et redémarrer")
		item.SetTooltip("Télécharge la nouvelle version, la vérifie, remplace l'application et la relance")
	} else {
		item.SetTitle("⬆ Greffier " + info.Derniere + " disponible : télécharger")
		item.SetTooltip("Ouvre la page de la nouvelle version")
	}
}

func quitterIcone() {
	defer func() { _ = recover() }()
	systray.Quit()
}
