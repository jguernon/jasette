# Jasette pour iPhone et iPad (version limitée, alpha)

Un **client du Serveur Jasette**, en SwiftUI (iOS 17+, `ca.jasette.ios`).
iOS ne peut pas faire tourner les ponts (arrière-plan, sessions longues) :
l'app parle seulement au serveur de l'utilisateur, comme Android ou le bureau
en mode client. Protocole : `serveur/PROTOCOLE.md`.

> Version limitée (alpha) : client d'un Serveur Jasette, sans notifications
> push ni connexion des services.

## Ce que fait cette version

- **Jumelage** (§3) : adresse + code à 6 chiffres, puis le code de
  vérification à comparer avant d'envoyer quoi que ce soit qui dépende du
  code. Adresse, empreinte, jeton et nom vont dans le **Trousseau iOS**
  (`kSecAttrAccessibleAfterFirstUnlock`).
- **Connexion** (§1, §4, §5) : TLS 1.3 épinglé sur l'empreinte (Network.framework),
  `auth`, signes de vie, reconnexion progressive (1 → 30 s, 60 s après un refus),
  seulement **au premier plan** (débranché en quittant l'app).
- **Liste** de toutes les conversations des services du serveur (logo du service,
  avatar, aperçu, non-lus, épinglées en tête), filtres par service et « Groupes »
  (grands espaces, §5.13). Marques lues, jamais écrites : les **privées** ne
  paraissent nulle part, les archives Jasette et `cacher_apres_jours` sont respectés.
- **Conversation** : fil (cache du serveur puis réponse du pont, §5.8),
  messages plus anciens, images (`download` puis `get`, §6.1, seulement à
  l'écran), envoi de texte (seulement sur un geste). Ouvrir une conversation
  non lue envoie `read`, comme les autres clients.
- **Réglages** : serveur et état des services, langue (auto / français /
  English), À propos, Déconnecter (oublie tout sur l'appareil ; le serveur
  garde ses services).
- Thème sombre d'Android (`ui/Theme.kt`, palette `sombre()`), icône tirée de
  `assets/jasette.svg` (`outils/icone-ios.svg` : tuile pleine, iOS l'arrondit).

Pas encore : notifications push, stories, vocaux, envoi de fichiers,
réactions, recherche, connexion des services (elle se fait au bureau ou sur Android).

## Construire

Depuis le PC (le Mac : `ssh macminitc`, Xcode, dossier `~/Developer/jasette-ios/`) :

```sh
paquets/empaqueter-ios.sh              # .cache/paquets/jasette-<version>-ios.ipa (non signé)
paquets/empaqueter-ios.sh simulateur   # Jasette.app (Debug) pour le simulateur
```

Le script copie `ios/` sur le Mac, attend qu'aucune autre compilation ne
tourne (`cargo`, `rustc`, `construire.sh`, `xcodebuild`), puis lance
`construire-ios.sh` sous `caffeinate -i` (`xcodebuild -jobs 4`). Le projet
Xcode est généré par **XcodeGen** depuis `project.yml` (pas dans git).
Version : celle de `Cargo.toml` ; build : le nombre de commits ; affichée
« 1.0.1 alpha (306) ».

XcodeGen, sans sudo, une seule fois :

```sh
ssh macminitc 'mkdir -p ~/Developer/jasette-ios/outils && cd ~/Developer/jasette-ios/outils &&
  curl -sSLO https://github.com/yonaskolb/XcodeGen/releases/download/2.46.0/xcodegen.zip &&
  unzip -q xcodegen.zip && rm xcodegen.zip'
```

L'`.ipa` est `Payload/Jasette.app` zippé, **non signé** : AltStore et
SideStore le re-signent avec l'Apple ID de l'utilisateur.

## Installer (AltStore / SideStore)

1. Installer AltStore (altstore.io, avec AltServer sur un ordinateur) ou
   SideStore (sidestore.io, sans ordinateur après la première fois).
2. Envoyer `jasette-1.0.1-ios.ipa` sur l'iPhone (Fichiers, AirDrop…),
   puis AltStore/SideStore → **Mes apps** → **+** → choisir l'`.ipa`.
3. Ouvrir Jasette, entrer l'adresse du serveur (Tailscale p. ex.) et un code
   de **Réglages > Serveur Jasette > Jumeler un appareil** sur une app déjà
   jumelée (ou `jasette-serveur --jumeler`), comparer le code de vérification.

Limites d'Apple : avec un **Apple ID gratuit**, l'app expire après **7 jours**
(AltStore/SideStore la renouvellent) et **3 apps** au plus ainsi installées ;
avec un **compte développeur payant**, la signature dure **1 an**.
iOS 16+ demande aussi d'activer le **Mode développeur** (Réglages >
Confidentialité et sécurité).

## Essayer sans compte réel

`essai/pont-factice` imite un pont (conversations, messages, avatars, une
photo, écho des envois) : rien ne sort de la machine.

```sh
# Sur le PC : binaires macOS arm64 (GOCACHE dans .cache/)
(cd ios/essai/pont-factice && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o ../../../.cache/ios/essai/pont-factice .)
(cd serveur && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o ../.cache/ios/essai/jasette-serveur .)
rsync -a .cache/ios/essai/ macminitc:Developer/jasette-ios/essai/
# Sur le Mac (~/Developer/jasette-ios/essai) :
export JASETTE_DATA=$PWD/donnees JASETTE_PONT=$PWD/pont-factice JASETTE_META=$PWD/pont-factice JASETTE_MCP_PORT=47901
./jasette-serveur --port 47900 --ecoute 127.0.0.1 --services google,messenger,instagram &
./jasette-serveur --jumeler
```

Dans le simulateur, la version **Debug** accepte des arguments d'essai (jamais
compilés dans l'`.ipa`) : `-essaiAdresse 127.0.0.1:47900 -essaiCode 123456`
(jumelage sans geste), `-essaiArret verification`, `-essaiOuvrir m:201`,
`-essaiTexte "…"`, `-essaiEcran reglages` ; et `-AppleLanguages "(fr)"`.

```sh
U=<udid> ; xcrun simctl boot $U ; xcrun simctl install $U ~/Developer/jasette-ios/derive/Build/Products/Debug-iphonesimulator/Jasette.app
xcrun simctl launch $U ca.jasette.ios -essaiAdresse 127.0.0.1:47900 -essaiCode 123456
xcrun simctl io $U screenshot ios-liste.png ; xcrun simctl shutdown $U
```

## Plus tard : notifications push (APNs)

iOS ne laisse pas une app garder une connexion en arrière-plan : les
notifications doivent venir d'**APNs**. L'option la plus simple, sans service
tiers : le **serveur Jasette envoie lui-même** les notifications à APNs, avec
la **clé APNs (.p8) de l'utilisateur** (compte développeur payant : Certificates,
IDs & Profiles > Keys > Apple Push Notifications service).

- Côté app : l'entitlement `aps-environment`, `registerForRemoteNotifications`,
  puis une trame (p. ex. `{"apns":{"jeton":"<device token>","env":"production"}}`)
  qui donne au serveur le jeton de l'appareil, lié à son jeton Jasette.
- Côté serveur : un JWT ES256 signé avec la clé `.p8` (Key ID, Team ID),
  HTTP/2 vers `api.push.apple.com`, pour chaque message entrant d'une
  conversation non privée et non `large` ; contenu minimal (nom + aperçu, ou
  seulement « Nouveau message » si l'utilisateur le préfère), `mutable-content`
  pour une extension qui pourrait déchiffrer un contenu chiffré de bout en bout.
- Une app re-signée par AltStore/SideStore avec un Apple ID gratuit **ne peut
  pas** recevoir de push : il faut le compte payant (et l'app signée avec
  l'App ID qui porte la capacité Push), donc TestFlight, App Store ou ad hoc.
