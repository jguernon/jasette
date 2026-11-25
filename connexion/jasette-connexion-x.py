#!/usr/bin/env python3
"""Jasette — la fenêtre de connexion X (Twitter).

Ouvre https://x.com/i/flow/login. Une fois connecté (les cookies auth_token et
ct0 existent sur x.com ou twitter.com), elle demande le code d'accès X Chat :
X chiffre maintenant les messages privés, et ce code (4 chiffres, choisi
dans X Chat) ouvre le coffre où X garde les clés du compte. Puis elle écrit
sur sa sortie standard une seule ligne JSON :

    {"cookies": {"auth_token": "...", "ct0": "...", "twid": "...", ...,
                 "jasette_pin": "1234"},
     "platform": "x"}

et se ferme. Jasette la passe au pont jasette-x :
    {"cmd":"login","cookies":<cookies>,"platform":"x"}

Le code d'accès voyage avec les cookies (l'interface les transmet tels
quels) ; le pont le retire aussitôt et ne le garde jamais : il ne garde que
les clés qu'il a servi à récupérer.

Avec --sans-code, la fenêtre ne demande pas le code : elle écrit les cookies
dès la connexion et se ferme (l'interface de Jasette demande alors le code
elle-même, et l'envoie au pont dans « pin »).

Code de sortie : 0 si les cookies ont été écrits, 1 si la fenêtre a été
fermée avant (rien n'est écrit sur la sortie standard).

Les données de navigation restent dans ~/.local/share/jasette/connexion-x
(X se souvient de toi la fois suivante). Rien d'autre n'est lu.
"""
import json
import os
import sys
from pathlib import Path

import gi
gi.require_version("Gtk", "4.0")
gi.require_version("WebKit", "6.0")
from gi.repository import GLib, Gtk, WebKit  # noqa: E402

DEBUT = "https://x.com/i/flow/login"
DOMAINES = ["https://x.com/", "https://twitter.com/", "https://api.x.com/"]
# Ceux sans lesquels X ne reconnaît pas la session (comme mautrix-twitter).
REQUIS = {"auth_token", "ct0"}
# Ceux qu'on transmet au pont : twid donne l'identifiant du compte, les
# autres font ressembler les requêtes du pont à celles du navigateur.
VOULUS = REQUIS | {"twid", "kdt", "att", "guest_id", "guest_id_ads", "guest_id_marketing",
                   "personalization_id", "lang", "dnt", "d_prefs", "__cf_bm", "gt"}
# --sans-code : l'interface demande le code elle-même.
SANS_CODE = "--sans-code" in sys.argv[1:]
# Le nom sous lequel le code d'accès voyage parmi les cookies (voir login.go du pont).
CLE_CODE = "jasette_pin"
# Le pont parle à X comme Chrome : la fenêtre se présente de la même façon.
AGENT = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) "
         "Chrome/141.0.0.0 Safari/537.36")

DONNEES = Path(os.environ.get("XDG_DATA_HOME") or Path.home() / ".local" / "share") / "jasette" / "connexion-x"


def est_x(uri):
    return any(uri.startswith(p) for p in (
        "https://x.com/", "https://www.x.com/", "https://twitter.com/",
        "https://www.twitter.com/", "https://mobile.x.com/", "https://mobile.twitter.com/"))


class Connexion(Gtk.ApplicationWindow):
    def __init__(self, app):
        super().__init__(application=app, title="Connexion à X — Jasette")
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
        self.trouves = None  # les cookies, une fois connecté (on attend le code)

        self.vue = WebKit.WebView(network_session=session)
        reglages = self.vue.get_settings()
        reglages.set_user_agent(AGENT)
        reglages.set_enable_developer_extras(False)
        self.vue.connect("load-changed", self.charge)
        # « Continuer avec Google / Apple » ouvre une fenêtre surgissante :
        # sans elle, X affiche « Something went wrong ».
        self.vue.connect("create", self.surgissante)
        # X est une appli d'une seule page : l'adresse change sans nouveau
        # chargement. On vérifie aussi quand l'adresse change.
        self.vue.connect("notify::uri", lambda v, _p: self.verifier())
        self.set_child(self.vue)
        self.connect("close-request", self.fermee)
        # Les cookies arrivent parfois après le dernier changement d'adresse :
        # on revérifie toutes les 2 secondes.
        GLib.timeout_add_seconds(2, self.minuterie)
        self.vue.load_uri(DEBUT)

    def surgissante(self, vue, _action):
        """Une fenêtre surgissante (connexion Google ou Apple), liée à la vue
        principale : même session, et X reçoit la réponse par window.opener."""
        # Google refuse les navigateurs « intégrés » qui se disent Chrome :
        # comme la fenêtre de Google Messages, on se présente comme Firefox.
        # Des réglages à elle : ceux de la vue de X ne changent pas.
        reglages = WebKit.Settings()
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
            print("jasette-connexion-x : fenêtre fermée sans connexion", file=sys.stderr)
            self.get_application().quit()
        return False

    def minuterie(self):
        if self.fini or self.trouves is not None:
            return False
        self.verifier()
        return True

    def charge(self, _vue, evenement):
        if evenement == WebKit.LoadEvent.FINISHED:
            self.verifier()

    def verifier(self):
        if self.fini or self.lecture or self.trouves is not None:
            return
        uri = self.vue.get_uri() or ""
        if not est_x(uri):
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
                print(f"jasette-connexion-x : {e.message}", file=sys.stderr)
            suivant()

        suivant()

    def connecte(self, trouves):
        if self.trouves is not None or not REQUIS.issubset(trouves):
            # Pas encore connecté : la minuterie revérifie.
            return
        self.trouves = trouves
        if SANS_CODE:
            self.fini = True
            print(json.dumps({"cookies": trouves, "platform": "x"}), flush=True)
            self.get_application().quit()
            return
        self.demander_code()

    # ---- Le code d'accès X Chat ----

    def demander_code(self):
        boite = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=14)
        boite.set_margin_top(48)
        boite.set_margin_bottom(48)
        boite.set_margin_start(40)
        boite.set_margin_end(40)
        boite.set_valign(Gtk.Align.CENTER)

        titre = Gtk.Label()
        titre.set_markup("<span size='x-large' weight='bold'>Code d'accès X Chat</span>")
        boite.append(titre)

        texte = Gtk.Label(label=(
            "X chiffre maintenant tes messages privés. Pour que Jasette puisse "
            "les lire, entre le code d'accès (4 chiffres) que tu as choisi dans "
            "X Chat.\n\nIl sert une seule fois à récupérer tes clés ; Jasette ne "
            "le garde pas. Attention : X verrouille X Chat après quelques "
            "mauvais essais.\n\nPas encore de code ? Active d'abord X Chat dans "
            "l'application X ou sur x.com/messages."))
        texte.set_wrap(True)
        texte.set_justify(Gtk.Justification.CENTER)
        texte.set_max_width_chars(48)
        boite.append(texte)

        self.champ = Gtk.PasswordEntry()
        self.champ.set_show_peek_icon(True)
        self.champ.set_property("placeholder-text", "Code d'accès")
        self.champ.set_halign(Gtk.Align.CENTER)
        self.champ.set_size_request(220, -1)
        self.champ.connect("activate", lambda _c: self.valider())
        boite.append(self.champ)

        self.erreur = Gtk.Label()
        self.erreur.add_css_class("error")
        boite.append(self.erreur)

        bouton = Gtk.Button(label="Continuer")
        bouton.add_css_class("suggested-action")
        bouton.set_halign(Gtk.Align.CENTER)
        bouton.connect("clicked", lambda _b: self.valider())
        boite.append(bouton)

        self.set_child(boite)
        self.champ.grab_focus()

    def valider(self):
        code = self.champ.get_text().strip()
        if not code.isdigit() or len(code) < 4:
            self.erreur.set_text("Le code d'accès est fait de chiffres (au moins 4).")
            return
        self.fini = True
        cookies = dict(self.trouves)
        cookies[CLE_CODE] = code
        print(json.dumps({"cookies": cookies, "platform": "x"}), flush=True)
        self.get_application().quit()


def main():
    app = Gtk.Application(application_id="ca.jasette.ConnexionX")
    etat = {"fen": None}

    def active(a):
        etat["fen"] = Connexion(a)
        etat["fen"].present()

    app.connect("activate", active)
    app.run([sys.argv[0]])
    sys.exit(0 if etat["fen"] is not None and etat["fen"].fini else 1)


if __name__ == "__main__":
    main()
