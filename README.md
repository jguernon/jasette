# Jasette

Toutes tes messageries dans une seule app : Google Messages (SMS/RCS), Messenger,
Instagram, WhatsApp, Signal, X, Telegram, Bluesky, Discord, Reddit et LinkedIn. Pour Linux et
Android.

Page de présentation et téléchargements : https://jonathanguernon.net/jasette/

## Comment ça marche

- **Bureau** (`src/`) : l'interface, en Rust avec GPUI.
- **Ponts** (`pont*/`) : un petit programme Go par service, qui parle le
  protocole du service au nom de ton compte. Ils reposent sur les bibliothèques
  de [mautrix](https://github.com/mautrix) (gmessages, meta, whatsmeow,
  signalmeow), sur [libsignal](https://github.com/signalapp/libsignal) et, pour
  Telegram, sur [gotd/td](https://github.com/gotd/td). Le pont Bluesky
  (messages privés) parle directement l'API XRPC d'AT Protocol. Le pont
  Discord reprend la façon de faire de
  [mautrix-discord](https://github.com/mautrix/discord) et son discordgo
  modifié ([beeper/discordgo](https://github.com/beeper/discordgo)). Le pont
  Reddit parle au serveur Matrix du chat de Reddit avec
  [mautrix-go](https://github.com/mautrix/go). Le pont LinkedIn (messagerie
  seulement) s'appuie sur linkedingo, la bibliothèque de
  [mautrix-linkedin](https://github.com/mautrix/linkedin).
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

**Mise à jour dans l'app du bureau** (`src/maj.rs`). `empaqueter.sh` écrit
aussi `version-linux.json` : versionCode et versionName (lus dans le binaire,
`jasette --version` ; `build.rs` y inscrit 10 000 + le nombre de commits, comme
Android), les notes, et pour le `.deb` et le `.tar.gz` leur adresse, leur
SHA-256 et leur taille. À publier dans `jasette/telecharger/` avec les deux
paquets. L'app le lit au démarrage (au plus toutes les 6 heures) ou par
Réglages > À propos ; « Mettre à jour » dépend de l'installation :

- paquet `.deb` (`/usr/lib/jasette`, connu de `dpkg -S`) : le `.deb` est
  téléchargé dans `<données>/maj/` (0600), son SHA-256 vérifié, puis installé
  par `pkexec apt-get install -y` (ou `pkexec dpkg -i`) : c'est la fenêtre du
  système qui demande le mot de passe, jamais Jasette ;
- archive `.tar.gz` dans un dossier de l'utilisateur (`~/.local/lib/jasette`) :
  l'archive est téléchargée et vérifiée, extraite à côté (sans chemin absolu,
  `..` ni lien), puis chaque exécutable est remplacé par un renommage ; l'ancien
  reste en `<nom>.ancien` (pour revenir en arrière : le renommer à sa place).
  « Redémarrer maintenant » relance Jasette. Si `jasette-serveur` tourne, il
  faut aussi `systemctl --user restart jasette-serveur` (bouton proposé en mode
  Serveur quand il tourne sous systemd) ;
- version de développement (dossier `target` de cargo) : rien d'automatique,
  le bouton dit seulement si une version publiée est plus récente.

HTTPS seulement, sur `https://jonathanguernon.net` seulement, sans redirection.
Pour les essais, une version debug accepte `JASETTE_MAJ_URL` (un
`version-linux.json` servi en HTTP sur 127.0.0.1) ; une version release
l'ignore.

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

Discord : messages privés, groupes privés et, dans l'onglet **Groupes**, les
salons texte des serveurs, avec prudence : la liste vient des données que
Discord envoie à la connexion (aucune requête de plus, aucun abonnement aux
serveurs), et un salon n'a d'historique que si tu l'ouvres (50 messages au
plus). Connexion par **code QR**, à scanner dans l'app Discord
du téléphone (Paramètres > Scanner le code QR), ou en collant le **jeton** du
compte. Le pont garde le jeton dans `<données>/discord/session.json` (0600).
Les ids de conversation portent le préfixe `d:`.

Reddit : le **chat** de Reddit (conversations, groupes privés et, dans
l'onglet **Groupes**, les chats de subreddit). Connexion dans une fenêtre
reddit.com (bureau : `connexion/jasette-connexion-reddit.py` ; Android : une
WebView), où captcha et double authentification se font comme d'habitude ;
la fenêtre passe ensuite par la page du chat, puis Jasette garde les
cookies utiles et le jeton du chat dans `<données>/reddit/session.json`
(0600). Les demandes de message s'acceptent ou s'ignorent au-dessus du
champ. Images seulement (pas de vidéo ni de fichier), pas de modification
de message, réactions limitées à la palette de Reddit. Les ids de
conversation portent le préfixe `r:`. Même prudence que pour Discord : pas
d'API officielle pour le chat, aucune action automatique, jamais
« en train d'écrire » envoyé, une page d'historique à l'ouverture ; à tes
risques.

LinkedIn : la **messagerie** seulement (conversations à deux et de groupe,
demandes de message, InMail reçus). Connexion dans une fenêtre linkedin.com
(bureau : `connexion/jasette-connexion-linkedin.py` ; Android : une
WebView), où captcha et double authentification se font comme d'habitude ;
Jasette garde ensuite les cookies `li_at` et `JSESSIONID` dans
`<données>/linkedin/session.json` (0600). Texte, réponses, photos,
vidéos, fichiers (20 Mo), réactions (une à la fois), « lu », « écrit »
et « vu » reçus. Pas de modification ni de suppression de message, pas de
nouvelle conversation ni de groupe (à commencer dans LinkedIn), pas de
Sales Navigator ni d'InMail envoyé. « Accepter » une demande la range dans
la boîte (LinkedIn l'accepte à ta première réponse) ; « Ignorer » la cache
dans Jasette. Les ids de conversation portent le préfixe `l:`. LinkedIn
surveille l'automatisation : rythme lent (une requête par seconde au plus),
petites pages, aucune action automatique, jamais « en train d'écrire » ni
accusé de réception envoyés ; à tes risques. `JASETTE_LINKEDIN_LECTURE_SEULE=1`
interdit au pont d'envoyer, de réagir et de marquer lu.

**Onglet Groupes** : les grands espaces (serveurs Discord, chats de subreddit, canaux et
supergroupes Telegram, communautés WhatsApp, discussions de communauté et
canaux de diffusion Messenger) y vivent, jamais dans la boîte de réception
(elle reste pour le chat) ; une mention ou une réponse à un de tes messages
s'y voit par une pastille « @ » et une notification. Les groupes de
personnes ne bougent pas. Voir
`serveur/PROTOCOLE.md`, §5.13.

**Risque** : Discord interdit officiellement les clients tiers sur un compte
d'utilisateur (« self-bot »). Le pont imite le client web (empreinte TLS de
Chrome, mêmes en-têtes, statut invisible), espace ses requêtes, ne charge
l'historique que par petites pages, ne marque lu et n'écrit qu'à ta demande,
et le serveur Jasette n'en précharge presque rien. Le risque d'un
avertissement ou d'une suspension du compte n'est pas nul : à tes risques.

**« Quoi de neuf ? »** (écran « Moi ») publie une note ou un statut sur
plusieurs services d'un geste : la note d'Instagram et de Messenger, le texte
« Infos » de WhatsApp, la bio de Telegram, la description du profil Bluesky, le statut
personnalisé de Discord.
Les `doc_id` GraphQL de Meta ont des valeurs par défaut dans les ponts ; s'ils
périment, on les corrige sans recompiler dans
`<données>/instagram/notes-docs.json` et `<données>/messenger/notes-docs.json`
(voir `serveur/PROTOCOLE.md`, §5.12). « Partager ce que j'écoute » lit le
lecteur Spotify de l'appareil (MPRIS au bureau, notifications sur Android),
sans API Spotify ; le format « Genre » demande le genre de l'artiste à
MusicBrainz.

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
