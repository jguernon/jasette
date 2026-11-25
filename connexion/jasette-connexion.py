#!/usr/bin/env python3
"""Jasette — la fenêtre de connexion Google.

Ouvre la page de connexion de Google (le même chemin que messages.google.com).
Une fois connecté, quand la page arrive sur messages.google.com, elle écrit
les cookies de session de Google en JSON sur sa sortie standard et se ferme.
Jasette les passe au pont, qui demande alors au téléphone de confirmer avec
un emoji.

Les données de navigation restent dans ~/.local/share/jasette/connexion
(Google se souvient de toi la fois suivante). Rien d'autre n'est lu.
"""
import json
import os
import sys
from pathlib import Path

import gi
gi.require_version("Gtk", "4.0")
gi.require_version("WebKit", "6.0")
from gi.repository import GLib, Gtk, WebKit  # noqa: E402

DEBUT = "https://accounts.google.com/AccountChooser?continue=https://messages.google.com/web/config"
VOULUS = {"SID", "HSID", "SSID", "APISID", "SAPISID", "OSID", "__Secure-1PSIDTS"}
REQUIS = {"SID", "HSID", "SSID", "APISID", "SAPISID", "OSID"}
# Google refuse parfois les navigateurs « intégrés » : on se présente comme Firefox.
AGENT = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"

DONNEES = Path(os.environ.get("XDG_DATA_HOME") or Path.home() / ".local" / "share") / "jasette" / "connexion"


class Connexion(Gtk.ApplicationWindow):
    def __init__(self, app):
        super().__init__(application=app, title="Connexion à Google — Jasette")
        self.set_default_size(520, 720)
        DONNEES.mkdir(parents=True, exist_ok=True, mode=0o700)
        session = WebKit.NetworkSession.new(str(DONNEES / "donnees"), str(DONNEES / "cache"))
        cookies = session.get_cookie_manager()
        cookies.set_persistent_storage(str(DONNEES / "cookies.sqlite"), WebKit.CookiePersistentStorage.SQLITE)
        cookies.set_accept_policy(WebKit.CookieAcceptPolicy.ALWAYS)
        self.cookies = cookies
        self.fini = False

        self.vue = WebKit.WebView(network_session=session)
        reglages = self.vue.get_settings()
        reglages.set_user_agent(AGENT)
        reglages.set_enable_developer_extras(False)
        self.vue.connect("load-changed", self.charge)
        self.set_child(self.vue)
        self.vue.load_uri(DEBUT)

    def charge(self, vue, evenement):
        if evenement != WebKit.LoadEvent.FINISHED or self.fini:
            return
        uri = vue.get_uri() or ""
        if uri.startswith("https://messages.google.com/"):
            self.lire_cookies()

    def lire_cookies(self):
        trouves = {}
        restants = ["https://messages.google.com/", "https://www.google.com/", "https://accounts.google.com/"]

        def suivant():
            if not restants:
                self.terminer(trouves)
                return
            self.cookies.get_cookies(restants.pop(0), None, recu)

        def recu(mgr, res):
            try:
                for ck in mgr.get_cookies_finish(res):
                    nom = ck.get_name()
                    if nom in VOULUS and nom not in trouves:
                        trouves[nom] = ck.get_value()
            except GLib.Error as e:
                print(f"jasette-connexion : {e.message}", file=sys.stderr)
            suivant()

        suivant()

    def terminer(self, trouves):
        if not REQUIS.issubset(trouves):
            manque = ", ".join(sorted(REQUIS - set(trouves)))
            print(f"jasette-connexion : cookies manquants ({manque}), on attend", file=sys.stderr)
            return
        self.fini = True
        print(json.dumps({"cookies": trouves}), flush=True)
        self.get_application().quit()


def main():
    app = Gtk.Application(application_id="ca.jasette.Connexion")
    app.connect("activate", lambda a: Connexion(a).present())
    sys.exit(app.run([sys.argv[0]]))


if __name__ == "__main__":
    main()
