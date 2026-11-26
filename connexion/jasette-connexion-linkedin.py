#!/usr/bin/env python3
"""Jasette — la fenêtre de connexion LinkedIn.

Ouvre https://www.linkedin.com/login. L'utilisateur s'y connecte lui-même
(captcha, vérification par courriel et double authentification compris).
Une fois connecté (les cookies li_at et JSESSIONID existent sur
linkedin.com, hors des pages de connexion et de vérification), la fenêtre
écrit sur sa sortie standard une seule ligne JSON :

    {"cookies": {"li_at": "...", "JSESSIONID": "...", "bcookie": "...", ...,
                 "jasette_user_agent": "Mozilla/5.0 ..."},
     "platform": "linkedin"}

et se ferme. Jasette la passe au pont jasette-linkedin :
    {"cmd":"login","cookies":<cookies>,"platform":"linkedin"}

`jasette_user_agent` n'est pas un cookie : l'agent de cette fenêtre, que le
pont reprend pour ressembler à la même session de navigateur. Le pont garde
les cookies dans <données>/linkedin/session.json (0600).

Code de sortie : 0 si les cookies ont été écrits, 1 si la fenêtre a été
fermée avant (rien n'est écrit sur la sortie standard).

Les données de navigation restent dans ~/.local/share/jasette/connexion-linkedin
(LinkedIn se souvient de toi la fois suivante). Rien d'autre n'est lu.
"""
import json
import os
import re
import sys
from pathlib import Path

import gi
gi.require_version("Gtk", "4.0")
gi.require_version("WebKit", "6.0")
from gi.repository import GLib, Gtk, WebKit  # noqa: E402

DEBUT = "https://www.linkedin.com/login"
DOMAINES = ["https://www.linkedin.com/", "https://linkedin.com/"]
# Sans eux, LinkedIn ne reconnaît pas la session (li_at : la session ;
# JSESSIONID : le jeton CSRF).
REQUIS = {"li_at", "JSESSIONID"}
# Les pages où la connexion n'est pas finie.
EN_COURS = ("/login", "/checkpoint", "/uas/", "/authwall", "/signup")
NOM_VALIDE = re.compile(r"^[A-Za-z0-9_.\-]{1,64}$")

DONNEES = Path(os.environ.get("XDG_DATA_HOME") or Path.home() / ".local" / "share") / "jasette" / "connexion-linkedin"


def est_linkedin(uri):
    return uri.startswith("https://www.linkedin.com/") or uri.startswith("https://linkedin.com/")


def connexion_finie(uri):
    if not est_linkedin(uri):
        return False
    chemin = re.sub(r"^https://(www\.)?linkedin\.com", "", uri)
    return not any(chemin.startswith(p) for p in EN_COURS)


class Connexion(Gtk.ApplicationWindow):
    def __init__(self, app):
        super().__init__(application=app, title="Connexion à LinkedIn — Jasette")
        self.set_default_size(520, 760)
        DONNEES.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(DONNEES, 0o700)
        session = WebKit.NetworkSession.new(str(DONNEES / "donnees"), str(DONNEES / "cache"))
        cookies = session.get_cookie_manager()
        cookies.set_persistent_storage(str(DONNEES / "cookies.sqlite"), WebKit.CookiePersistentStorage.SQLITE)
        cookies.set_accept_policy(WebKit.CookieAcceptPolicy.ALWAYS)
        self.cookies = cookies
        self.fini = False
        self.lecture = False
        self.agent = None

        self.vue = WebKit.WebView(network_session=session)
        reglages = self.vue.get_settings()
        # L'agent propre de WebKit : un moteur qui se dit Chrome se fait
        # repérer. JASETTE_LINKEDIN_AGENT pour essayer autre chose.
        if os.environ.get("JASETTE_LINKEDIN_AGENT"):
            reglages.set_user_agent(os.environ["JASETTE_LINKEDIN_AGENT"])
        reglages.set_enable_developer_extras(False)
        self.vue.connect("load-changed", self.charge)
        # « Continuer avec Google / Apple » : une fenêtre surgissante.
        self.vue.connect("create", self.surgissante)
        self.vue.connect("notify::uri", lambda v, _p: self.verifier())
        self.set_child(self.vue)
        self.connect("close-request", self.fermee)
        # Les cookies arrivent parfois après le dernier changement d'adresse.
        GLib.timeout_add_seconds(2, self.minuterie)
        self.vue.load_uri(DEBUT)

    def surgissante(self, vue, _action):
        """Une fenêtre surgissante (connexion Google ou Apple), liée à la vue principale."""
        reglages = WebKit.Settings()
        # Google refuse les navigateurs « intégrés » qui se disent Chrome.
        reglages.set_user_agent("Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0")
        nouvelle = WebKit.WebView(related_view=vue, settings=reglages)
        fen = Gtk.Window(title="Connexion — Jasette", transient_for=self)
        fen.set_default_size(500, 680)
        fen.set_child(nouvelle)
        nouvelle.connect("ready-to-show", lambda _v: fen.present())
        nouvelle.connect("close", lambda _v: fen.destroy())
        return nouvelle

    def fermee(self, _fen):
        if not self.fini:
            print("jasette-connexion-linkedin : fenêtre fermée sans connexion", file=sys.stderr)
            self.get_application().quit()
        return False

    def minuterie(self):
        if self.fini:
            return False
        self.verifier()
        return True

    def charge(self, _vue, evenement):
        if evenement == WebKit.LoadEvent.FINISHED:
            if self.agent is None:
                self.lire_agent()
            self.verifier()

    def lire_agent(self):
        """L'agent du navigateur de cette fenêtre (le pont le reprend)."""
        def fini(vue, res):
            try:
                val = vue.evaluate_javascript_finish(res)
                texte = val.to_string() if val is not None else ""
            except GLib.Error:
                return
            if texte and texte.startswith("Mozilla/"):
                self.agent = texte[:400]
                self.verifier()
        self.vue.evaluate_javascript("navigator.userAgent", -1, None, None, None, fini)

    def verifier(self):
        if self.fini or self.lecture:
            return
        if not connexion_finie(self.vue.get_uri() or ""):
            return
        self.lire_cookies()

    def lire_cookies(self):
        self.lecture = True
        trouves = {}
        restants = list(DOMAINES)

        def suivant():
            if not restants:
                self.lecture = False
                self.connecte(trouves)
                return
            self.cookies.get_cookies(restants.pop(0), None, recu)

        def recu(mgr, res):
            try:
                for ck in mgr.get_cookies_finish(res):
                    nom = ck.get_name()
                    if NOM_VALIDE.match(nom) and nom not in trouves and ck.get_value():
                        trouves[nom] = ck.get_value()
            except GLib.Error as e:
                print(f"jasette-connexion-linkedin : {e.message}", file=sys.stderr)
            suivant()

        suivant()

    def connecte(self, trouves):
        if self.fini or not REQUIS.issubset(trouves):
            # Pas encore connecté : la minuterie revérifie.
            return
        if not connexion_finie(self.vue.get_uri() or ""):
            return
        if self.agent is None:
            # L'agent d'abord (une page de plus au plus) : la minuterie revérifie.
            self.lire_agent()
            return
        self.fini = True
        trouves["jasette_user_agent"] = self.agent
        print(json.dumps({"cookies": trouves, "platform": "linkedin"}), flush=True)
        self.get_application().quit()


def main():
    app = Gtk.Application(application_id="ca.jasette.ConnexionLinkedIn")
    etat = {"fen": None}

    def active(a):
        etat["fen"] = Connexion(a)
        etat["fen"].present()

    app.connect("activate", active)
    app.run([sys.argv[0]])
    sys.exit(0 if etat["fen"] is not None and etat["fen"].fini else 1)


if __name__ == "__main__":
    main()
