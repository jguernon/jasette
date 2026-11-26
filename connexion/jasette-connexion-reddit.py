#!/usr/bin/env python3
"""Jasette — la fenêtre de connexion Reddit.

Ouvre https://www.reddit.com/login/. L'utilisateur s'y connecte lui-même
(captcha et double authentification compris). Une fois connecté (le cookie
reddit_session existe sur reddit.com), la fenêtre ouvre la page du chat
(https://www.reddit.com/chat/) pour que Reddit pose aussi ses cookies de
chat, puis écrit sur sa sortie standard une seule ligne JSON :

    {"cookies": {"reddit_session": "...", "csrf_token": "...",
                 "token_v2": "...", "loid": "...", ...},
     "platform": "reddit"}

et se ferme. Jasette la passe au pont jasette-reddit :
    {"cmd":"login","cookies":<cookies>,"platform":"reddit"}

Le pont en tire le jeton du chat (POST /svc/shreddit/token) et ne garde que
les cookies utiles, dans <données>/reddit/session.json (0600).

Code de sortie : 0 si les cookies ont été écrits, 1 si la fenêtre a été
fermée avant (rien n'est écrit sur la sortie standard).

Les données de navigation restent dans ~/.local/share/jasette/connexion-reddit
(Reddit se souvient de toi la fois suivante). Rien d'autre n'est lu.
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

DEBUT = "https://www.reddit.com/login/"
CHAT = "https://www.reddit.com/chat/"
DOMAINES = ["https://www.reddit.com/", "https://reddit.com/", "https://chat.reddit.com/"]
# Sans lui, Reddit ne reconnaît pas la session.
REQUIS = {"reddit_session"}
# Ceux qu'on transmet au pont (les mêmes que pont-reddit/jeton.go).
VOULUS = REQUIS | {"csrf_token", "token_v2", "loid", "session_tracker", "edgebucket", "rdt", "csv"}

DONNEES = Path(os.environ.get("XDG_DATA_HOME") or Path.home() / ".local" / "share") / "jasette" / "connexion-reddit"


def est_reddit(uri):
    return any(uri.startswith(p) for p in (
        "https://www.reddit.com/", "https://reddit.com/", "https://old.reddit.com/",
        "https://new.reddit.com/", "https://chat.reddit.com/", "https://sh.reddit.com/"))


class Connexion(Gtk.ApplicationWindow):
    def __init__(self, app):
        super().__init__(application=app, title="Connexion à Reddit — Jasette")
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
        self.chat_ouvert = False
        self.chat_pret = False  # la page du chat a fini de charger (et d'être lue)
        self.version = None  # X-Reddit-Client-Version, lue dans la page du chat

        self.vue = WebKit.WebView(network_session=session)
        reglages = self.vue.get_settings()
        # L'agent propre de WebKit (comme la fenêtre de X) : un moteur qui se
        # dit Chrome se fait repérer. JASETTE_REDDIT_AGENT pour essayer autre chose.
        if os.environ.get("JASETTE_REDDIT_AGENT"):
            reglages.set_user_agent(os.environ["JASETTE_REDDIT_AGENT"])
        reglages.set_enable_developer_extras(False)
        self.vue.connect("load-changed", self.charge)
        # « Continuer avec Google / Apple » : une fenêtre surgissante.
        self.vue.connect("create", self.surgissante)
        # Reddit est une appli d'une seule page : on vérifie aussi quand l'adresse change.
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
            print("jasette-connexion-reddit : fenêtre fermée sans connexion", file=sys.stderr)
            self.get_application().quit()
        return False

    def minuterie(self):
        if self.fini:
            return False
        self.verifier()
        return True

    def charge(self, _vue, evenement):
        if evenement == WebKit.LoadEvent.FINISHED:
            if self.chat_ouvert:
                self.lire_version()
            self.verifier()

    def lire_version(self):
        """La version du client web (en-tête X-Reddit-Client-Version), si la
        page du chat la nomme : le pont la reprend tel quel."""
        def fini(vue, res):
            self.chat_pret = True
            try:
                val = vue.evaluate_javascript_finish(res)
                texte = val.to_string() if val is not None else ""
            except GLib.Error:
                self.verifier()
                return
            m = re.search(r'"?(?:client[-_]?version|clientVersion)"?\s*[:=]\s*"(\d{4}-\d{2}-\d{2}T[^"]{1,40})"', texte or "")
            if m:
                self.version = m.group(1)
            self.verifier()
        self.vue.evaluate_javascript("document.documentElement.outerHTML.slice(0, 400000)", -1, None, None, None, fini)

    def verifier(self):
        if self.fini or self.lecture:
            return
        uri = self.vue.get_uri() or ""
        if not est_reddit(uri):
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
                    if nom in VOULUS and nom not in trouves and ck.get_value():
                        trouves[nom] = ck.get_value()
            except GLib.Error as e:
                print(f"jasette-connexion-reddit : {e.message}", file=sys.stderr)
            suivant()

        suivant()

    def connecte(self, trouves):
        if self.fini or not REQUIS.issubset(trouves):
            # Pas encore connecté : la minuterie revérifie.
            return
        uri = self.vue.get_uri() or ""
        if not self.chat_ouvert:
            # Connecté : la page du chat pose csrf_token et token_v2 à jour.
            self.chat_ouvert = True
            if not uri.startswith(CHAT):
                self.vue.load_uri(CHAT)
            else:
                self.lire_version()
            return
        if not self.chat_pret or ("csrf_token" not in trouves and "token_v2" not in trouves):
            return
        self.fini = True
        if self.version:
            trouves["jasette_client_version"] = self.version
        print(json.dumps({"cookies": trouves, "platform": "reddit"}), flush=True)
        self.get_application().quit()


def main():
    app = Gtk.Application(application_id="ca.jasette.ConnexionReddit")
    etat = {"fen": None}

    def active(a):
        etat["fen"] = Connexion(a)
        etat["fen"].present()

    app.connect("activate", active)
    app.run([sys.argv[0]])
    sys.exit(0 if etat["fen"] is not None and etat["fen"].fini else 1)


if __name__ == "__main__":
    main()
