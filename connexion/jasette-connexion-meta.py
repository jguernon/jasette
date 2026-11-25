#!/usr/bin/env python3
"""Jasette — la fenêtre de connexion Messenger (Meta).

Ouvre messenger.com (ou facebook.com si Meta y renvoie). Une fois connecté,
quand la page arrive sur la boîte de réception, elle écrit sur sa sortie
standard une seule ligne JSON :

    {"cookies": {"c_user": "...", "xs": "...", "datr": "...", ...},
     "platform": "messenger" | "facebook"}

puis se ferme. Jasette la passe au pont jasette-meta :
    {"cmd":"login","cookies":<cookies>,"platform":<platform>}

Code de sortie : 0 si les cookies ont été écrits, 1 si la fenêtre a été
fermée avant (rien n'est écrit sur la sortie standard).

Les données de navigation restent dans ~/.local/share/jasette/connexion-meta
(Meta se souvient de toi la fois suivante). Rien d'autre n'est lu.
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

DEBUT = {
    "messenger": "https://www.messenger.com/?no_redirect=true",
    "facebook": "https://www.facebook.com/messages/",
    "instagram": "https://www.instagram.com/direct/inbox/",
}
# Les pages où l'on est connecté (mêmes motifs que mautrix-meta).
ARRIVEE = {
    "messenger": re.compile(r"^https://www\.messenger\.com/(?:e2ee/)?(?:t/[0-9]+/?)?(?:\?.*)?$"),
    "facebook": re.compile(r"^https://www\.facebook\.com/(?:messages/(?:e2ee/)?(?:t/[0-9]+/?)?)?(?:\?.*)?$"),
}
DOMAINES = {
    "messenger": ["https://www.messenger.com/", "https://messenger.com/", "https://www.facebook.com/"],
    "facebook": ["https://www.facebook.com/", "https://facebook.com/", "https://www.messenger.com/"],
    "instagram": ["https://www.instagram.com/", "https://instagram.com/"],
}
VOULUS = {"c_user", "xs", "datr", "fr", "sb", "wd", "dpr", "presence",
          "sessionid", "csrftoken", "ds_user_id", "mid", "ig_did", "rur", "shbid", "shbts"}
REQUIS_PAR = {
    "messenger": {"c_user", "xs", "datr"},
    "facebook": {"c_user", "xs", "datr"},
    "instagram": {"sessionid", "csrftoken", "ds_user_id"},
}
# Le pont parle à Meta comme Chrome : la fenêtre se présente de la même façon.
AGENT = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) "
         "Chrome/141.0.0.0 Safari/537.36")

DONNEES = Path(os.environ.get("XDG_DATA_HOME") or Path.home() / ".local" / "share") / "jasette" / "connexion-meta"


def plateforme_de(uri):
    if uri.startswith("https://www.instagram.com/") or uri.startswith("https://instagram.com/"):
        return "instagram"
    if uri.startswith("https://www.facebook.com/") or uri.startswith("https://facebook.com/"):
        return "facebook"
    if uri.startswith("https://www.messenger.com/") or uri.startswith("https://messenger.com/"):
        return "messenger"
    return None


class Connexion(Gtk.ApplicationWindow):
    def __init__(self, app, plateforme):
        titre = "Instagram" if plateforme == "instagram" else "Messenger"
        super().__init__(application=app, title=f"Connexion à {titre} — Jasette")
        self.set_default_size(560, 760)
        DONNEES.mkdir(parents=True, exist_ok=True, mode=0o700)
        os.chmod(DONNEES, 0o700)
        session = WebKit.NetworkSession.new(str(DONNEES / "donnees"), str(DONNEES / "cache"))
        cookies = session.get_cookie_manager()
        cookies.set_persistent_storage(str(DONNEES / "cookies.sqlite"), WebKit.CookiePersistentStorage.SQLITE)
        cookies.set_accept_policy(WebKit.CookieAcceptPolicy.ALWAYS)
        self.cookies = cookies
        self.fini = False
        self.lecture = False

        self.vue = WebKit.WebView(network_session=session)
        reglages = self.vue.get_settings()
        reglages.set_user_agent(AGENT)
        reglages.set_enable_developer_extras(False)
        self.vue.connect("load-changed", self.charge)
        # Messenger est une appli d'une seule page : l'adresse change sans
        # nouveau chargement. On vérifie aussi quand l'adresse change.
        self.vue.connect("notify::uri", lambda v, _p: self.verifier())
        self.set_child(self.vue)
        self.connect("close-request", self.fermee)
        # La page de connexion a la même adresse que la boîte de réception :
        # on revérifie les cookies toutes les 2 secondes.
        GLib.timeout_add_seconds(2, self.minuterie)
        self.vue.load_uri(DEBUT[plateforme])

    def fermee(self, _fen):
        if not self.fini:
            print("jasette-connexion-meta : fenêtre fermée sans connexion", file=sys.stderr)
            self.get_application().quit()
        return False

    def minuterie(self):
        if self.fini:
            return False
        self.verifier()
        return True

    def charge(self, _vue, evenement):
        if evenement == WebKit.LoadEvent.FINISHED:
            self.verifier()

    def verifier(self):
        if self.fini or self.lecture:
            return
        # Connecté = les cookies c_user et xs existent, quelle que soit la page
        # affichée (Messenger change souvent ses adresses).
        uri = self.vue.get_uri() or ""
        plat = plateforme_de(uri)
        if plat is None:
            return
        self.lire_cookies(plat)

    def lire_cookies(self, plat):
        self.lecture = True
        trouves = {}
        restants = list(DOMAINES[plat])

        def suivant():
            if not restants:
                self.lecture = False
                self.terminer(trouves, plat)
                return
            self.cookies.get_cookies(restants.pop(0), None, recu)

        def recu(mgr, res):
            try:
                for ck in mgr.get_cookies_finish(res):
                    nom = ck.get_name()
                    if nom in VOULUS and nom not in trouves:
                        trouves[nom] = ck.get_value()
            except GLib.Error as e:
                print(f"jasette-connexion-meta : {e.message}", file=sys.stderr)
            suivant()

        suivant()

    def terminer(self, trouves, plat):
        if not REQUIS_PAR[plat].issubset(trouves):
            # Page d'accueil pas encore connectée : la minuterie revérifie.
            return
        self.fini = True
        print(json.dumps({"cookies": trouves, "platform": plat}), flush=True)
        self.get_application().quit()


def main():
    plateforme = "messenger"
    if "--facebook" in sys.argv[1:]:
        plateforme = "facebook"
    if "--instagram" in sys.argv[1:]:
        plateforme = "instagram"
    app = Gtk.Application(application_id="ca.jasette.ConnexionMeta")
    etat = {"fen": None}

    def active(a):
        etat["fen"] = Connexion(a, plateforme)
        etat["fen"].present()

    app.connect("activate", active)
    app.run([sys.argv[0]])
    sys.exit(0 if etat["fen"] is not None and etat["fen"].fini else 1)


if __name__ == "__main__":
    main()
