# Jasette

Toutes tes messageries dans une seule app : Google Messages (SMS/RCS), Messenger,
Instagram, WhatsApp, Signal, X, Telegram et Bluesky. Pour Linux et Android.

Page de présentation et téléchargements : https://jonathanguernon.net/jasette/

## Comment ça marche

- **Bureau** (`src/`) : l'interface, en Rust avec GPUI.
- **Ponts** (`pont*/`) : un petit programme Go par service, qui parle le
  protocole du service au nom de ton compte. Ils reposent sur les bibliothèques
  de [mautrix](https://github.com/mautrix) (gmessages, meta, whatsmeow,
  signalmeow), sur [libsignal](https://github.com/signalapp/libsignal) et, pour
  Telegram, sur [gotd/td](https://github.com/gotd/td). Le pont Bluesky
  (messages privés) parle directement l'API XRPC d'AT Protocol.
- **Serveur Jasette** (`serveur/`) : facultatif. Il garde les sessions sur une
  machine toujours allumée, et tes autres appareils (bureau, Android) s'y
  branchent, par un lien TLS épinglé et un jumelage à code. Protocole :
  `serveur/PROTOCOLE.md`.
  Il peut aussi ouvrir une **connexion MCP** pour Claude (claude.ai, Claude
  Code) : Réglages > Serveur Jasette > « Ouvrir ma connexion MCP ». Fermée par
  défaut, protégée par un jeton, en lecture seule (l'envoi de messages est une
  case à part). Voir `serveur/PROTOCOLE.md`, §9.
- **Android** (`android/`) : Kotlin et Compose. Il fonctionne seul (ponts
  embarqués) ou comme client de ton serveur Jasette.

Trois façons de s'en servir : **Autonome** (tout sur l'appareil), **Serveur**
(cette machine garde les sessions), **Client** (passe par ton serveur).

## Compiler

Tous les caches et sorties de compilation vont dans `.cache/` du projet.

```sh
cargo build --release                       # le bureau
cd pont && go build -o ../.cache/bin/jasette-pont .   # un pont (même chose pour les autres)
cd serveur && go build -o ../.cache/bin/jasette-serveur .
android/compiler.sh                          # l'APK (ponts arm64 compris)
```

Les paquets à télécharger (`.deb`, `.tar.gz`, APK release signé, `SHA256SUMS`)
sortent dans `.cache/paquets/` :

```sh
paquets/empaqueter.sh            # tout ; --sans-apk pour Linux seulement
paquets/empaqueter.sh --apk-seul # l'APK release et version.json seulement
android/compiler.sh --release    # l'APK seul, signé avec signing/ (jamais dans git)
```

**Mise à jour dans l'app Android.** `empaqueter.sh` écrit aussi
`version.json` (versionCode, versionName, adresse et SHA-256 de l'APK release
signé, notes de `paquets/notes-maj.json`), publié avec l'APK dans
`jasette/telecharger/` du site. L'app le lit au démarrage (au plus toutes les
6 heures) ou par Réglages > À propos > « Chercher une mise à jour » ; elle
télécharge l'APK seulement quand on touche « Mettre à jour », vérifie son
SHA-256 et son certificat, puis l'installe (`MiseAJour.kt`). Le versionCode
vaut 10 000 + le nombre de commits git : chaque release est plus récente que la
précédente. Une version debug (adb) ne se met pas à jour elle-même.

Signal demande `libsignal_ffi.a` : voir `pont-signal/build.sh` et
`android/build-libsignal.sh`.

Telegram demande l'`api_id` et l'`api_hash` de **ta propre application** :
connecte-toi sur https://my.telegram.org, ouvre « API development tools » et
remplis le formulaire (nom et nom court au choix). Entre-les ensuite dans
Réglages > Comptes > Telegram (ou dans `JASETTE_TELEGRAM_API_ID` et
`JASETTE_TELEGRAM_API_HASH`). Jasette n'en fournit aucun par défaut et
n'emprunte pas ceux d'une autre application (Telegram Desktop, par exemple) :
le pont les garde dans `<données>/telegram/api.json` (0600).

Bluesky (messages privés seulement) demande un **mot de passe d'app** avec
l'accès aux messages : dans Bluesky, Réglages > Confidentialité et sécurité >
Mots de passe d'app > Ajouter, en cochant « Autoriser l'accès à vos messages
privés ». Jasette ne garde que les jetons de session
(`<données>/bluesky/session.json`, 0600), jamais le mot de passe.

## Vie privée

Jasette parle directement aux services, depuis tes appareils ou ton propre
serveur. Aucune donnée ne passe par un service tiers. Les sessions sont
gardées en fichiers privés (0600).

**Liste privée** (Réglages > Liste privée, désactivée par défaut) : « Rendre
privée », dans le menu ⋮ d'une conversation, la cache partout (listes,
recherche, médias, non-lus, notifications, MCP). On la retrouve dans la vue
Privées, avec un code à 6 chiffres : Ctrl+Maj+P ou `jasette --prive` au
bureau, l'icône « Jasette privé » (au cadenas) sur Android. La liste des
privées est synchronisée par le serveur Jasette ; le code reste sur chaque
appareil, haché (PBKDF2).

## Avertissement

Jasette n'est affilié à aucun des services qu'il relie. Il utilise leurs
protocoles web non officiels : un service peut changer et briser une connexion
à tout moment. Utilise-le à tes risques, dans le respect des conditions de
chaque service.

## Licence

GNU Affero General Public License v3.0 ou toute version ultérieure — voir
`LICENSE`. © 2026 Jonathan Guernon.
