// Jasette — le serveur.
//
// Une machine toujours allumée détient les sessions (Google Messages n'en
// permet qu'une à la fois) et les autres appareils de l'utilisateur s'y
// branchent. Le serveur lance un pont par service (jasette-pont,
// jasette-meta, jasette-ig), comme le fait l'interface en mode local, et
// partage leurs lignes JSON avec les clients jumelés, sur TLS.
//
// Usage :
//
//	jasette-serveur [--port 47800] [--ecoute ADRESSE] [--services google,messenger,instagram] [--bin DOSSIER]
//	jasette-serveur --jumeler         affiche un code de jumelage (10 minutes)
//	jasette-serveur --appareils       liste les appareils jumelés
//	jasette-serveur --revoquer NOM    retire un appareil (débranché aussitôt)
//	jasette-serveur --empreinte       affiche l'empreinte du certificat
//
// Les données vivent dans <données Jasette>/serveur (JASETTE_DATA, sinon
// ~/.local/share/jasette). Les journaux vont dans la sortie d'erreur ; on n'y
// écrit jamais de jeton ni de cookie. Le protocole : PROTOCOLE.md.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const portParDefaut = 47800

var journal = log.New(os.Stderr, "jasette-serveur : ", log.LstdFlags)

func logf(format string, args ...any) { journal.Printf(format, args...) }

func main() {
	port := flag.Int("port", portDefaut(), "port TCP d'écoute (TLS)")
	ecoute := flag.String("ecoute", "", "adresse d'écoute (défaut : toutes)")
	services := flag.String("services", "google,messenger,instagram", "services à servir, séparés par des virgules")
	binDir := flag.String("bin", "", "dossier des ponts (défaut : à côté de jasette-serveur)")
	jumeler := flag.Bool("jumeler", false, "afficher un code de jumelage valable 10 minutes")
	appareils := flag.Bool("appareils", false, "lister les appareils jumelés")
	revoquerNom := flag.String("revoquer", "", "révoquer l'appareil de ce nom")
	empreinte := flag.Bool("empreinte", false, "afficher l'empreinte SHA-256 du certificat")
	flag.Parse()

	switch {
	case *jumeler:
		os.Exit(cmdJumeler())
	case *appareils:
		os.Exit(cmdAppareils())
	case *revoquerNom != "":
		os.Exit(cmdRevoquer(*revoquerNom))
	case *empreinte:
		_, fp, err := chargerCertificat()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(fp)
		return
	}
	if err := lancerServeur(net.JoinHostPort(*ecoute, strconv.Itoa(*port)), *services, *binDir); err != nil {
		logf("%v", err)
		os.Exit(1)
	}
}

func portDefaut() int {
	if p, err := strconv.Atoi(os.Getenv("JASETTE_SERVEUR_PORT")); err == nil && p > 0 {
		return p
	}
	return portParDefaut
}

func lancerServeur(adresse, services, binDir string) error {
	cert, fp, err := chargerCertificat()
	if err != nil {
		return fmt.Errorf("certificat : %w", err)
	}
	srv := &Serveur{
		clients:   map[*Client]struct{}{},
		ponts:     map[string]*Pont{},
		empreinte: fp,
		depots:    depotsDir(),
		limiteur:  nouveauLimiteur(10, 10*time.Minute),
	}
	srv.interdit, _ = filepath.EvalSymlinks(serveurDir())
	for _, r := range []racine{{chemin: mediaDir()}, {chemin: dataDir(), mediasSeulement: true}} {
		_ = os.MkdirAll(r.chemin, 0o700)
		if reel, err := filepath.EvalSymlinks(r.chemin); err == nil {
			r.chemin = reel
			srv.racines = append(srv.racines, r)
		}
	}
	if reel, err := filepath.EvalSymlinks(srv.depots); err == nil {
		srv.depots = reel
	}

	for _, svc := range strings.Split(services, ",") {
		svc = strings.TrimSpace(svc)
		if svc == "" {
			continue
		}
		p, err := nouveauPont(srv, svc, binDir)
		if err != nil {
			logf("%v : service ignoré", err)
			continue
		}
		srv.ponts[svc] = p
	}
	if len(srv.ponts) == 0 {
		return fmt.Errorf("aucun pont à lancer")
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	ln, err := tls.Listen("tcp", adresse, config)
	if err != nil {
		return err
	}
	logf("à l'écoute sur %s (TLS), empreinte %s", ln.Addr(), fp)
	logf("services : %s", strings.Join(srv.servicesTries(), ", "))

	for _, p := range srv.ponts {
		go p.boucle()
	}
	go srv.surveillerRevocations()
	go srv.nettoyerDepots()

	for {
		conn, err := ln.Accept()
		if err != nil {
			logf("accept : %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go srv.servir(conn)
	}
}

func cmdJumeler() int {
	_, fp, err := chargerCertificat()
	if err != nil {
		fmt.Fprintln(os.Stderr, "certificat :", err)
		return 1
	}
	j, err := nouveauCode()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Code de jumelage : %s %s\n", j.Code[:3], j.Code[3:])
	v := codeVerification(j.Code, fp)
	fmt.Printf("Vérification     : %s %s\n", v[:3], v[3:])
	fmt.Printf("Valable jusqu'à %s (un seul appareil).\n", time.Unix(j.Expire, 0).Format("15:04:05"))
	fmt.Println("Sur l'appareil, entrez le code ; il doit afficher le même code de vérification.")
	return 0
}

func cmdAppareils() int {
	liste, err := lireAppareils()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(liste) == 0 {
		fmt.Println("Aucun appareil jumelé.")
		return 0
	}
	for _, a := range liste {
		vu := "jamais"
		if a.Vu > 0 {
			vu = time.Unix(a.Vu, 0).Format("2006-01-02 15:04")
		}
		fmt.Printf("%-30s jumelé le %s, vu : %s\n", a.Nom, time.Unix(a.Cree, 0).Format("2006-01-02 15:04"), vu)
	}
	return 0
}

func cmdRevoquer(nom string) int {
	ok, err := revoquer(nom)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "Aucun appareil nommé %q.\n", nom)
		return 1
	}
	fmt.Printf("Appareil %q révoqué.\n", nom)
	return 0
}
