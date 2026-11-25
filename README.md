# Jasette

Toutes tes messageries dans une seule app : Google Messages (SMS/RCS), Messenger,
Instagram, WhatsApp, Signal et X. Pour Linux et Android.

Page de présentation et téléchargements : https://jonathanguernon.net/jasette/

## Comment ça marche

- **Bureau** (`src/`) : l'interface, en Rust avec GPUI.
- **Ponts** (`pont*/`) : un petit programme Go par service, qui parle le
  protocole du service au nom de ton compte. Ils reposent sur les bibliothèques
  de [mautrix](https://github.com/mautrix) (gmessages, meta, whatsmeow,
  signalmeow) et sur [libsignal](https://github.com/signalapp/libsignal).
- **Serveur Jasette** (`serveur/`) : facultatif. Il garde les sessions sur une
  machine toujours allumée, et tes autres appareils (bureau, Android) s'y
  branchent, par un lien TLS épinglé et un jumelage à code. Protocole :
  `serveur/PROTOCOLE.md`.
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
android/compiler.sh --release    # l'APK seul, signé avec signing/ (jamais dans git)
```

Signal demande `libsignal_ffi.a` : voir `pont-signal/build.sh` et
`android/build-libsignal.sh`.

## Vie privée

Jasette parle directement aux services, depuis tes appareils ou ton propre
serveur. Aucune donnée ne passe par un service tiers. Les sessions sont
gardées en fichiers privés (0600).

## Avertissement

Jasette n'est affilié à aucun des services qu'il relie. Il utilise leurs
protocoles web non officiels : un service peut changer et briser une connexion
à tout moment. Utilise-le à tes risques, dans le respect des conditions de
chaque service.

## Licence

GNU Affero General Public License v3.0 ou toute version ultérieure — voir
`LICENSE`. © 2026 Jonathan Guernon.
