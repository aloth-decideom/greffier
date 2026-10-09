// Greffier (DecideOm) : un seul exécutable (Windows, macOS, Linux) qui
//   - affiche une page pour choisir le dossier d'atelier (sélecteur natif du système) ;
//   - sert la page d'animation et de prise de notes ;
//   - enregistre les notes en continu dans <atelier>/sorties/ ;
//   - régénère outil/, prompt-cr.md et marp/ à partir de questions.md.
//
// Usage : double-clic, ou   greffier [dossier-atelier] [--port 8770] [--no-open] [--generer] [--sans-maj]
// Compilation : voir compiler.sh (ou la GitHub Action « Binaires greffier »).
package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed app lanceur.html prompt-compte-rendu.md modele-compte-rendu.md modele-compte-rendu-libre.md marp/decideom.css
var embedded embed.FS

var version = "dev" // renseignée à la compilation (-ldflags "-X main.version=…")

func main() {
	initConsole()
	port := flag.Int("port", 8770, "port local (garder le même : le navigateur y range ses données)")
	noOpen := flag.Bool("no-open", false, "ne pas ouvrir le navigateur")
	generer := flag.Bool("generer", false, "générer les fichiers de l'atelier (outil/, prompt-cr.md, marp/) puis quitter")
	sansMaj := flag.Bool("sans-maj", os.Getenv("GREFFIER_SANS_MAJ") != "", "ne pas vérifier s'il existe une nouvelle version (aussi : GREFFIER_SANS_MAJ=1)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Greffier DecideOm %s\nUsage : greffier [dossier-atelier] [options]\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()
	dossier := flag.Arg(0) // aussi rempli quand on glisse un dossier sur l'exécutable

	if *generer {
		if dossier == "" {
			fatal("--generer demande un dossier d'atelier")
		}
		a, warnings, err := LoadAtelier(dossier)
		if err != nil {
			fatal(err.Error())
		}
		notes, err := a.Generate(dossier)
		if err != nil {
			fatal(err.Error())
		}
		ns, nq := a.QuestionCount()
		fmt.Printf("OK : %d parties, %d sections, %d questions\n", len(a.Parts), ns, nq)
		for _, n := range append(warnings, notes...) {
			fmt.Println("  " + n)
		}
		return
	}

	srv := NewServeur(*port)
	base := srv.origin

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		// Port occupé : l'application tourne sans doute déjà → on lui confie l'ouverture.
		if dejaLancee(base) {
			fmt.Println("Greffier est déjà lancé : ouverture dans le navigateur.")
			ouvrirViaInstance(base, dossier, !*noOpen)
			time.Sleep(1500 * time.Millisecond)
			return
		}
		fatal(fmt.Sprintf("le port %d est utilisé par un autre programme. Relancer avec --port %d", *port, *port+1))
	}

	fmt.Printf("\n  Greffier DecideOm %s\n  ──────────────────────────────\n", version)
	fmt.Printf("  Page : %s\n", base+"/")
	fmt.Println("  Laisse cette fenêtre ouverte pendant l'atelier : la fermer arrête la sauvegarde sur disque.")
	fmt.Println()

	url := base + "/"
	if dossier != "" {
		if u, notes, err := srv.preparer(dossier); err != nil {
			logf("⚠ %v", err)
		} else {
			url = base + u
			for _, n := range notes {
				logf("  %s", n)
			}
		}
	}
	if !*noOpen {
		go func() { time.Sleep(400 * time.Millisecond); ouvrirNavigateur(url) }()
	}
	if !*sansMaj {
		go srv.Maj.Verifier(version)
	}
	server := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-srv.Arret
		logf("Arrêt demandé depuis la page : fin des écritures en cours…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx) // n'interrompt pas une sauvegarde en cours
	}()
	if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(err.Error())
	}
	logf("Application arrêtée. Les notes sont enregistrées dans le dossier sorties/ de l'atelier.")
	time.Sleep(1200 * time.Millisecond) // le temps de lire le message avant que la fenêtre se ferme
}

func dejaLancee(base string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get(base + "/api/ateliers")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	return r.StatusCode == http.StatusOK
}

func ouvrirViaInstance(base, dossier string, ouvrir bool) {
	url := base + "/"
	if dossier != "" {
		body, _ := json.Marshal(map[string]string{"chemin": absOr(dossier)})
		req, _ := http.NewRequest(http.MethodPost, base+"/api/ouvrir", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		if r, err := (&http.Client{Timeout: 30 * time.Second}).Do(req); err == nil {
			var res struct{ URL, Erreur string }
			_ = json.NewDecoder(r.Body).Decode(&res)
			r.Body.Close()
			if res.URL != "" {
				url = base + res.URL
			} else if res.Erreur != "" {
				fmt.Println("⚠", res.Erreur)
			}
		}
	}
	if ouvrir {
		ouvrirNavigateur(url)
	}
}

func absOr(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// fatal affiche l'erreur ; sous Windows, garde la fenêtre ouverte pour qu'on puisse la lire.
func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "Erreur :", msg)
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "Appuie sur Entrée pour fermer.")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	os.Exit(1)
}
