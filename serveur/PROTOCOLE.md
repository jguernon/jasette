# Protocole du serveur Jasette (version 1)

Le serveur (`jasette-serveur`) détient les sessions des services et lance
leurs ponts. Les appareils de l'utilisateur (bureau, Android) s'y branchent
pour un service ou plusieurs. Ce document décrit tout ce qu'un client doit
savoir. La référence côté client est `src/distant.rs` (Rust).

## 1. Transport

- TCP, port **47800** par défaut (`--port`). L'adresse saisie par
  l'utilisateur est `hôte`, `hôte:port` ou `[ipv6]:port`.
- **TLS 1.3** uniquement. Le certificat est **auto-signé** (ECDSA P-256),
  créé au premier lancement et gardé. Le client n'utilise **aucune** autorité
  de certification et ne vérifie pas le nom d'hôte : il épingle l'empreinte.
- **Empreinte** = SHA-256 du certificat feuille **encodé en DER**, en
  hexadécimal **minuscule** (64 caractères). Exemple :
  `468e2bbec1c879a076bf15d769a2dead300b4a4897ec60d7c2f92b50bf2a42f4`.
  `jasette-serveur --empreinte` l'affiche.
- SNI : le client peut envoyer `jasette-serveur` (le serveur l'ignore).
- **Trames** : une ligne JSON (UTF-8) par message, terminée par `\n`, dans
  les deux sens. Pas d'autre encadrement. Taille maximale d'une ligne :
  4 Mio vers le serveur ; le client doit accepter au moins 8 Mio.
- Un champ inconnu est ignoré, des deux côtés.

En Kotlin : un `X509TrustManager` qui calcule
`MessageDigest.getInstance("SHA-256").digest(chain[0].encoded)` et le compare à
l'empreinte épinglée (ou le retient, à l'étape de vérification du
jumelage) ; `SSLContext.getInstance("TLSv1.3")` ; et un `HostnameVerifier`
inutile puisqu'on ouvre un `SSLSocket` directement.

## 2. Accueil

Dès la poignée de main TLS finie, le serveur envoie :

```json
{"jasette":"serveur","version":1,"nonce":"<base64 standard de 32 octets>"}
```

Le client répond par **une** trame : `jumeler` (§3) ou `auth` (§4). Le
serveur coupe s'il n'a rien reçu dans les **20 s**.

Les erreurs, à cette étape, ont toutes la forme suivante, suivie de la
fermeture :

```json
{"erreur":"code|jeton|limite|trame|interne","texte":"message lisible, en français"}
```

## 3. Jumelage

### 3.1 Côté serveur

`jasette-serveur --jumeler` affiche, par exemple :

```
Code de jumelage : 219 229
Vérification     : 388 261
Valable jusqu'à 12:15:51 (un seul appareil).
```

- Le **code** a 6 chiffres. Il vaut **10 minutes** et **un seul** jumelage
  réussi. Après **5** preuves fausses, il est annulé. Il est gardé dans
  `<données>/serveur/jumelage.json` (0600), si bien que la commande marche
  que le serveur tourne ou non, et que le serveur en marche le voit aussitôt.
  Un nouveau `--jumeler` remplace le code précédent.
- Le serveur limite aussi les essais à **10 par adresse IP sur 10 minutes**
  (jumelages et authentifications ratées). Au-delà : `{"erreur":"limite"}`.
  Chaque échec est répondu après 1 s.

### 3.2 Pourquoi un code de vérification

Un code de 6 chiffres est trop court pour qu'une preuve qui en dépend
résiste à une recherche exhaustive hors ligne : un intermédiaire qui
recevrait une telle preuve retrouverait le code en une fraction de seconde.
Le client ne doit donc **rien** envoyer qui dépende du code avant d'être sûr
du certificat.

Le **code de vérification** lie le code à l'empreinte du certificat :

```
verification = SHA-256( "jasette/1/verification|" + code + "|" + empreinte )
               → 4 premiers octets, entier non signé grand-boutiste, modulo 1 000 000,
                 écrit sur 6 chiffres (zéros à gauche)
```

(`code` = les 6 chiffres sans espace ; `empreinte` = hexadécimal minuscule.)

Le serveur l'affiche avec le code, pour **son** empreinte. Le client le
calcule avec l'empreinte **qu'il voit**, avant d'envoyer quoi que ce soit.
Un intermédiaire doit présenter son certificat avant de connaître le code :
il n'a qu'une chance sur un million de tomber sur le même code de
vérification. L'utilisateur compare les deux nombres (ou les saisit tous les
deux, et le client compare lui-même).

### 3.3 Déroulement côté client

1. **Vérifier.** Ouvrir une connexion TLS sans épinglage, retenir
   l'empreinte `E`, lire l'accueil, fermer. Calculer
   `verification(code, E)` et l'afficher : « Le serveur affiche-t-il
   388 261 ? ». Ne rien envoyer.
2. Si l'utilisateur confirme, **jumeler** sur une nouvelle connexion, en
   exigeant l'empreinte `E` (refuser tout autre certificat). Lire l'accueil :
   nonce du serveur `NS` (la chaîne base64 telle quelle). Tirer 32 octets
   aléatoires, en base64 standard : `NC`. Envoyer :

   ```json
   {"jumeler":{"nom":"Pixel de Jonathan","nonce":"<NC>","preuve":"<hex>"}}
   ```

   avec

   ```
   preuve = HMAC-SHA256( clé = code (6 octets ASCII),
                         message = "jasette/1/client|" + E + "|" + NS + "|" + NC )
   ```

   en hexadécimal minuscule. `nom` : le nom de l'appareil (60 caractères au
   plus) ; s'il est déjà pris, le serveur ajoute « (2) », « (3) »…
3. Réponse en cas de succès :

   ```json
   {"jumele":{"jeton":"<jeton>","nom":"<nom retenu>","preuve":"<hex>"}}
   ```

   Le client vérifie la preuve du serveur :
   `HMAC-SHA256(code, "jasette/1/serveur|" + E + "|" + NS + "|" + NC)`. Si
   elle est fausse, il jette le jeton.
   Échec : `{"erreur":"code","texte":"code de jumelage invalide ou expiré"}`.
4. Le client garde **adresse, empreinte E, jeton, nom** (le jeton comme un
   mot de passe : stockage chiffré sur Android). Le serveur ne garde que le
   SHA-256 du jeton.

Le serveur vérifie la preuve avec **sa propre** empreinte : un intermédiaire
qui relaierait la trame d'un client trompé échouerait, et l'essai compterait.

## 4. Authentification

Sur une connexion épinglée (certificat ≠ empreinte gardée → refuser, et
le dire à l'utilisateur : « le certificat du serveur a changé ; s'il a été
réinstallé, jumelez de nouveau »), après l'accueil :

```json
{"auth":{"jeton":"<jeton>"}}
```

Réponses :

```json
{"auth":"ok","nom":"Pixel de Jonathan","services":["google","messenger","instagram"]}
{"erreur":"jeton","texte":"appareil inconnu ou révoqué : jumelez-le de nouveau"}
```

`services` : ceux que ce serveur fait tourner (`--services`), dans l'ordre
`google`, `messenger`, `instagram`, `whatsapp`, `signal`, `x`, `telegram`,
`bluesky`, `discord`. Les six derniers sont **facultatifs** : le serveur ne les lance que si leur
binaire est présent (`jasette-wa`, `jasette-signal`, `jasette-x`,
`jasette-telegram`, `jasette-bluesky`, `jasette-discord`, voir §5.4) ; sinon
ils sont simplement absents de la liste. Le client n'affiche que les
services de cette liste. Après `ok`, la connexion passe au régime du §5,
sans limite de durée.

Révocation : `jasette-serveur --appareils` liste les appareils,
`--revoquer <nom>` en retire un ; s'il est branché, il est débranché dans les
3 secondes, et ses connexions suivantes reçoivent `{"erreur":"jeton"}`.

## 5. Multiplexage

### 5.1 Commandes (client → serveur)

```json
{"svc":"google","cmd":{"cmd":"messages","conv":"123","count":30}}
```

`svc` : `google`, `messenger`, `instagram`, `whatsapp`, `signal`, `x`, `telegram`, `bluesky` ou `discord`.
`cmd` : exactement la commande qu'on écrirait sur l'entrée standard du pont
(voir l'en-tête de `pont/main.go`, `pont-meta/main.go`, `pont-wa/main.go`,
`pont-signal/main.go`, `pont-x/main.go`, `pont-telegram/main.go`,
`pont-bluesky/main.go` et `pont-discord/main.go`). Particularités :

- `{"cmd":"quit"}` est **ignoré** : le pont appartient au serveur.
- `{"cmd":"delete","conv":"<id>"}` : supprime la conversation (§5.6).
- `{"cmd":"create_group","members":[…],"name":"…"}` : crée un groupe (§5.6 bis).
- `send_media` : `path` doit être un chemin **renvoyé par un dépôt** (§6.2) ;
  tout autre chemin est refusé.
- Service non offert, ou pont en train de redémarrer : le serveur répond
  à ce client seul un événement d'erreur :
  `{"svc":"google","ev":{"ev":"error","where":"serveur","text":"…"}}`.

### 5.2 Événements (serveur → client)

```json
{"svc":"google","ev":{"ev":"message","item":{…}}}
```

`ev` : exactement la ligne du pont. Chaque événement de chaque pont va à
**tous** les clients branchés, y compris les réponses aux commandes d'un
autre client.

**Rejeu.** Juste après `{"auth":"ok"}`, le serveur envoie d'abord les
marques (`{"marques":{…}}`, §5.5), puis, pour chaque service, le dernier
état connu, dans cet ordre et s'ils existent : `hello`,
`state`, `account`, `avatar_moi`, `note_moi` (§5.12), `api` (Telegram : application réglée ou
non), `qr` (seulement en cours d'association),
`pair_code` (WhatsApp, seulement en cours d'association par code),
`login_step` (Telegram, seulement en attente du code ou du mot de passe),
`history_pin` (seulement s'il attend un NIP : `{"needed":true}`), puis les
dernières listes `conversations` gardées (boîte de réception, puis archives :
§5.8), puis la présence en ligne (§5.10) et les notes des contacts
(`notes`, §5.12). Le `qr` et
le `pair_code` (et le `login_step`) gardés sont oubliés dès `paired`, `state` `ready` ou
`unpaired`, ou si le pont s'arrête ; un `pair_code` remplace le `qr` (le pont
WhatsApp cesse alors de montrer des QR). Un client
qui ajoute un service en cours de route peut redemander ce rejeu :

```json
{"rejouer":"messenger"}
```

**Pont mort.** Si un pont s'arrête, le serveur le relance (attente
progressive de 1 s à 60 s) et diffuse aussitôt
`{"svc":…,"ev":{"ev":"state","state":"reconnecting"}}` ; le pont relancé
envoie ensuite `hello` puis son état. Le client ne voit jamais `pont_ferme`.

### 5.3 Signes de vie

- Le serveur envoie `{"ping":<nombre>}` toutes les **25 s** ; le client
  répond `{"pong":<le même nombre>}`.
- Le client peut aussi envoyer `{"ping":n}` ; réponse `{"pong":n}`.
- Le serveur coupe un client dont il n'a rien reçu depuis **90 s** ; le
  client doit faire de même (aucune ligne depuis 90 s → connexion perdue).
- Un client trop lent (8192 événements en attente) est coupé ; il se
  reconnecte et reçoit le rejeu.

### 5.4 Connexion des services WhatsApp, Signal, X, Telegram, Bluesky et Discord

Ces six ponts parlent le même protocole que les autres (mêmes commandes
`conversations`, `messages`, `send`, `send_media`, `react`, `read`,
`download`, `new`, `search`, `search_messages`, `unpair`…, mêmes
événements `hello`, `state`, `account`, `avatar`, `avatar_moi`,
`conversations`, `message`, `sent`, `media`, `typing`, `error`…). Seule
l'association diffère. États : `connecting`, `pairing`, `ready`,
`reconnecting`, `unpaired`.

| Service | Binaire | Données (0700) | Association |
|---|---|---|---|
| `whatsapp` | `jasette-wa` (ou `JASETTE_WA`) | `<données>/whatsapp/` | QR ou code à 8 caractères |
| `signal` | `jasette-signal` (ou `JASETTE_SIGNAL`) | `<données>/signal/` | QR (appareil lié) |
| `x` | `jasette-x` (ou `JASETTE_X`) | `<données>/x/` | cookies de x.com + code X Chat |
| `telegram` | `jasette-telegram` (ou `JASETTE_TELEGRAM`) | `<données>/telegram/` | numéro + code (+ mot de passe), ou QR |
| `bluesky` | `jasette-bluesky` (ou `JASETTE_BLUESKY`) | `<données>/bluesky/` | identifiant + mot de passe d'app |
| `discord` | `jasette-discord` (ou `JASETTE_DISCORD`) | `<données>/discord/` | QR (app Discord), ou jeton |

Les binaires sont cherchés comme les autres : variable d'environnement,
`--bin`, à côté de `jasette-serveur`, puis le `PATH`. Le serveur crée le
dossier de données en 0700 avant de lancer le pont.

**WhatsApp** (appareil lié, comme WhatsApp Web) :

```json
{"svc":"whatsapp","cmd":{"cmd":"pair"}}
{"svc":"whatsapp","cmd":{"cmd":"pair_phone","number":"+15145551234"}}
```

- `pair` → `state: pairing`, puis des `{"ev":"qr","code":"…","data":"…"}`
  renouvelés (même texte dans `code` et `data`), à afficher en QR et à
  scanner dans WhatsApp (Appareils connectés).
- `pair_phone` (numéro avec l'indicatif du pays) → `{"ev":"pair_code","code":"ABCD-EFGH"}`,
  à taper sur le téléphone (Associer avec un numéro). Les QR cessent.
- Réussite : `paired`, puis `account`, `state: ready`. Échec ou délai
  (10 min) : `error` (`where`: `pair` ou `pair_phone`) et `state: unpaired`.
- `login` est refusé (erreur) ; `archive` (`conv`, `archived`) est propre à
  WhatsApp.

**Signal** (appareil lié, comme Signal Desktop) :

```json
{"svc":"signal","cmd":{"cmd":"pair"}}
```

- → `state: pairing`, puis `{"ev":"qr","code":"sgnl://linkdevice?…","data":"…"}`,
  renouvelé à l'expiration ; à scanner dans Signal (Paramètres > Appareils
  liés). Un nouveau `pair` recommence avec un nouveau code.
- Réussite : `paired`, puis `account`, `state: ready`. Échec : `error`
  (`where`: `pair`) et `state: unpaired`. Pas d'ancien historique : seuls
  les messages reçus après la liaison. `typing` (`conv`, `on`) est accepté.

**X** (messages privés XChat, chiffrés) :

```json
{"svc":"x","cmd":{"cmd":"login","cookies":{"auth_token":"…","ct0":"…","twid":"u%3D…"},"pin":"1234"}}
```

- `cookies` : ceux de x.com après connexion dans la fenêtre de connexion
  (`auth_token` et `ct0` obligatoires, `twid` conseillé). `pin` : le code
  d'accès X Chat à 4 chiffres (il peut aussi venir dans les cookies sous le
  nom `jasette_pin`). **Un seul essai par `login`** : X verrouille X Chat
  après quelques codes faux.
- → `state: pairing`, puis `paired`, `account`, `state: ready` ; échec :
  `error` (`where`: `login`) et `state: unpaired`. `pair` est refusé.
- Les cookies et le code ne passent que dans la commande ; le serveur ne
  les écrit jamais dans ses journaux.

**Telegram** (une session de plus du compte, comme Telegram Desktop) :

Telegram exige l'`api_id` et l'`api_hash` d'une application enregistrée par
l'utilisateur sur https://my.telegram.org. Le pont les prend dans
`JASETTE_TELEGRAM_API_ID` et `JASETTE_TELEGRAM_API_HASH`, sinon dans
`<données>/telegram/api.json` (0600), qu'il écrit quand une commande les
apporte. Aucune valeur par défaut. `{"ev":"api","set":true|false}` dit s'ils
sont réglés (rejoué).

```json
{"svc":"telegram","cmd":{"cmd":"api","api_id":"123456","api_hash":"<32 hex>"}}
{"svc":"telegram","cmd":{"cmd":"login","number":"+15145551234"}}
{"svc":"telegram","cmd":{"cmd":"login_code","code":"12345"}}
{"svc":"telegram","cmd":{"cmd":"login_password","password":"…"}}
{"svc":"telegram","cmd":{"cmd":"pair"}}
```

- `login` → `state: pairing`, puis `{"ev":"login_step","step":"code","via":"app|sms|call|email"}`.
- `login_code` → réussite, ou `{"ev":"login_step","step":"password","hint":"…"}`
  (vérification en deux étapes). Un code ou un mot de passe faux : `error`
  (`where` : `login_code`, `login_password`) puis la même étape, avec
  `"retry":true`.
- `pair` → `{"ev":"qr","code":"tg://login?token=…","data":"…"}`, renouvelé ;
  à scanner dans Telegram (Paramètres > Appareils > Connecter un appareil).
  Peut ensuite demander le mot de passe (`login_step`).
- `login` et `pair` acceptent aussi `api_id` et `api_hash`.
- Réussite : `paired`, puis `account`, `state: ready`. Échec : `error` et
  `state: unpaired`.
- `archive` (`conv`, `archived`) : les archives de Telegram. Les ids :
  `u<n>` (personne), `g<n>` (groupe simple), `c<n>` (canal ou
  supergroupe) ; un message : `<fil>/<numéro>`.
- Le mot de passe et le code ne passent que dans la commande ; le serveur ne
  les écrit jamais dans ses journaux.

**Bluesky** (messages privés seulement, `chat.bsky.convo.*` par le PDS du
compte et le relais `did:web:api.bsky.chat`) :

```json
{"svc":"bluesky","cmd":{"cmd":"login","number":"nom.bsky.social","password":"xxxx-xxxx-xxxx-xxxx"}}
```

- `number` : le nom d'utilisateur (ou le courriel, ou le DID) ; `password` :
  un **mot de passe d'app** avec l'accès aux messages privés (Bluesky :
  Réglages > Confidentialité et sécurité > Mots de passe d'app, case
  « Autoriser l'accès à vos messages privés »). Un mot de passe d'app sans
  cet accès est refusé avec une erreur qui l'explique.
- Réussite : `paired`, `account`, `state: ready`. Échec : `error`
  (`where` : `login`) et `state: unpaired`.
- Le pont ne garde que les jetons (`accessJwt`, `refreshJwt`) dans
  `<données>/bluesky/session.json` (0600) et les renouvelle ; jamais le mot
  de passe. Les nouvelles : `chat.bsky.convo.getLog` toutes les 5 secondes.
- Pas de pièces jointes (`send_media` répond par une erreur), pas
  d'archives chez Bluesky (celles de Jasette, §5.5). Les ids : celui de la
  conversation Bluesky ; un message : `<conversation>/<id>`. Les résultats de
  `search` ont le DID de la personne pour id (à passer à `new`).

**Discord** (messages privés, groupes privés et salons des serveurs ; une session de
plus du compte, qui se présente comme le client web, par la discordgo
modifiée de mautrix-discord) :

```json
{"svc":"discord","cmd":{"cmd":"pair"}}
{"svc":"discord","cmd":{"cmd":"login","token":"…"}}
```

- `pair` → `state: pairing`, puis `{"ev":"qr","code":"https://discordapp.com/ra/…","data":"…"}`,
  à scanner dans l'app Discord du téléphone (Paramètres → Scanner le code
  QR) ; renouvelé à l'expiration (trois codes au plus). Une fois lu :
  `{"ev":"qr_scanned","name":"…"}` (à confirmer sur le téléphone ; pas
  rejoué). Refusé sur le téléphone : `error` (`where` : `pair`) et
  `state: unpaired`.
- `login` : le jeton du compte, collé à la main (`token`, ou `password`).
  Un jeton de robot (`Bot …`) est refusé.
- Réussite : `paired`, `account`, `state: ready`. Échec : `error`
  (`where` : `pair` ou `login`) et `state: unpaired`. Un jeton révoqué
  plus tard (mot de passe changé, appareil déconnecté) : `error` puis
  `state: unpaired`.
- Le pont garde le jeton dans `<données>/discord/session.json` (0600) ;
  le serveur ne l'écrit jamais dans ses journaux. `unpair` déconnecte
  aussi la session chez Discord.
- Les ids : celui du salon privé (snowflake) ; un salon de serveur :
  `g<guilde>:<salon>` (donc `d:g<guilde>:<salon>` dans les marques et chez
  les clients) ; un message : son snowflake.
  Les résultats de `search` (amis et gens des conversations connues, sans
  requête) ont l'id Discord de la personne pour id (à passer à `new`).
  `search_messages` ne cherche que dans les messages vus depuis le
  lancement du pont.
- **Serveurs** (§5.13) : les salons texte et d'annonces que je peux voir
  (permissions calculées sur mes rôles), tirés de READY et des événements
  qui suivent (serveur rejoint ou quitté, salon créé, modifié, supprimé,
  mes rôles changés), **sans requête REST** et **sans abonnement « lazy
  guild » (op 14)**. Ils arrivent dans la liste `conversations` après les
  messages privés (200 au plus), avec `large`, `kind: "server"`,
  `parent: "g<guilde>"`, `parent_name`, `section`, `section_order`,
  `order`, `announce` (salon d'annonces), `read_only` (pas le droit
  d'écrire) et `mentions` (`mention_count` du `read_state`). 150 salons
  au plus par serveur, 2000 en tout. Pas d'image de salon, pas de « … ».
  Un salon sans état de lecture est pris pour lu. **Aucun historique** tant
  que l'utilisateur ne l'ouvre pas (`messages` : une page de 50 au plus) ;
  le dernier message et les non-lus viennent de READY (`last_message_id`,
  sans texte : l'aperçu reste vide) et des messages que la passerelle
  pousse d'elle-même. Discord ne pousse pas tout pour les très gros
  serveurs sans op 14 : ces salons se mettent à jour à leur ouverture.
  `send`, `send_media`, `react`, `read` et `typing` : le même chemin que pour
  un message privé. Les messages portent `mentions_me` (`@moi`,
  `@everyone`, `@here`, un de mes rôles) et `reply_to_me`.
- Discord interdit les « self-bots » : le pont n'agit que sur commande,
  espace ses requêtes (400 ms au moins) et le serveur en précharge moins
  (§5.8). Pas d'archives chez Discord (celles de Jasette, §5.5) ; 10 Mo au
  plus par fichier envoyé.

### 5.5 Marques (Head, favoris, épinglées, archives, privées, moods, emojis rapides, personnes, garder_archives, cacher_apres_jours, presence_en_ligne, sons_conversation, notes_*)

Les marques de l'utilisateur sont gardées par le serveur et partagées entre
tous ses appareils. Ce sont cinq listes **ordonnées** d'ids de
conversations, tels que l'interface les voit (« m: » pour Messenger,
« i: » pour Instagram, « w: » pour WhatsApp, « s: » pour Signal, « x: »
pour X, « t: » pour Telegram, « b: » pour Bluesky, « d: » pour Discord, sans préfixe pour Google) :

- `head` : la grille en tête de la boîte de réception ;
- `favoris` : la vue de l'étoile (`etoiles` dans les réglages du bureau) ;
- `epingles` : les conversations épinglées en haut de la liste ;
- `archives` : l'**archivage propre à Jasette**, pour les services qui ne
  savent pas archiver (Messenger, Instagram, Signal, X, Bluesky, Discord). Google, WhatsApp et
  Telegram archivent pour de vrai (commande `archive` de leur pont) et n'y
  vont pas ;
- `privees` : la **Liste privée** (voir « Règle des privées » plus bas).

Un champ inconnu (par exemple `groupes_inbox`, essayé puis abandonné) est
ignoré, d'un client comme d'un ancien `marques.json`.

S'y ajoutent deux objets (facultatifs) :

- `moods` : le **Mood** de chaque conversation, `{id: nom}` (voir « Règle
  des moods » plus bas) ;
- `emojis` : l'**emoji rapide** de chaque conversation, `{id: "👍"}` (voir
  « Règle des emojis rapides » plus bas).

Et deux listes pour la **fiche de la personne** (voir « Règle des
personnes » plus bas) :

- `personnes` : les conversations reliées **à la main**,
  `[{"id":"p…","convs":["<id>","m:<id>"],"principal":"<id>"}]` ;
- `personnes_refus` : les paires « Pas la même personne »,
  `[["<id A>","<id B>"]]`.

Et des réglages (facultatifs) :

- `garder_archives` (booléen, **faux** par défaut) : « Garder les
  conversations archivées hors de la boîte de réception ». Vrai : un message
  reçu ne ramène plus une conversation de `archives` dans l'Inbox (voir
  « Règle des archives » plus bas).
- `cacher_apres_jours` (entier, **0** par défaut = tout afficher) : « Cacher
  les conversations plus anciennes de la boîte de réception », en jours
  (crans des clients : 0, 7, 15, 30, 60, 90, 182, 365). Seuls les clients
  filtrent, à l'affichage : la vue Inbox (« Tout ») ne montre plus une
  conversation dont le dernier message est plus vieux que cette durée, sauf
  si elle est épinglée ou a des non-lus. Elle reste dans Favoris, Archives,
  les vues par service, Privées, la recherche et le Head ; un nouveau
  message la ramène d'elle-même. Le serveur ne filtre rien, le MCP non plus
  (`lister_conversations` a ses propres filtres et sa `limite`).
- `presence_en_ligne` (booléen, **vrai** par défaut) : « Afficher qui est en
  ligne et qui écrit » (point vert, « Actif il y a 12 min », §5.10 ; « … »
  quand on vous écrit, §5.11). Le serveur le
  transmet aux ponts ; faux, les ponts ne suivent aucune présence et le
  serveur ne relaie plus l'événement `presence`.
- `sons_conversation` (chaîne, **`"bulle"`** par défaut) : le jeu de sons
  des conversations, `"bulle"`, `"bois"`, `"verre"` ou `"aucun"` (aucun son).
  Seuls les clients jouent : dans la conversation ouverte et visible, l'app
  au premier plan, un son à l'envoi (ou celui de l'émoji rapide), à la
  réception d'un message (au plus un par 400 ms ; ni historique, ni
  synchro, ni réaction) et à l'apparition des « … » (§5.11).
- `notes_contacts` (booléen, **vrai** par défaut) : « Afficher les notes de
  mes contacts » (bulles du Head, §5.12). Le serveur le transmet au pont
  Instagram (`JASETTE_NOTES`, commande `notes_contacts`) ; faux, le pont ne
  lit plus le plateau des notes et le serveur ne relaie plus `notes`.
- `notes_musique` (chaîne, **`""`** par défaut) : « Partager ce que
  j'écoute sur Spotify » : le nom de **l'appareil** qui publie la musique
  (§5.12), `""` = désactivé. Un seul appareil à la fois : l'activer sur un
  autre appareil le lui donne. 128 octets au plus.
- `notes_musique_format` (chaîne, **`"artiste"`** par défaut) : ce que dit
  la note musicale : `"artiste"` (« 🎵 Écoute Daft Punk », « 🎙️ Écoute
  Joe Rogan »), `"genre"` (« 🎵 Écoute du rock ! », « 🎙️ Écoute un
  podcast ») ou `"titre"` (« 🎵 Titre — Artiste »).
- `notes_services` (liste de chaînes, **`["tout"]`** par défaut) : les
  services où « Quoi de neuf ? » publie (écran « Moi », Réglages > Quoi de
  neuf, musique). `["tout"]` : tous les services compatibles connectés, y
  compris ceux qu'on branchera plus tard ; sinon une liste précise de noms
  de services (`instagram`, `messenger`, `whatsapp`, `telegram`, `bluesky`,
  `discord`…).

C'est une trame à part, comme `ping` : elle ne passe pas par les ponts.

**Serveur → client** (rejeu, juste après `{"auth":"ok"}`, puis à chaque
changement, à **tous** les clients) :

```json
{"marques":{"head":["i:42"],"favoris":["123","m:99"],"epingles":[],"archives":["m:77","s:+15145551234"],"privees":["w:5551"],"moods":{"m:99":"amour"},"emojis":{"m:99":"👍"},"garder_archives":false,"cacher_apres_jours":0,"presence_en_ligne":true,"sons_conversation":"bulle","notes_contacts":true,"notes_musique":"","notes_musique_format":"artiste","notes_services":["tout"],"personnes":[{"id":"p1764160000000a3f","convs":["123","m:99"],"principal":"123"}],"personnes_refus":[["i:42","m:99"]],"version":7}}
```

`version` : incrémentée à chaque changement accepté. **0** = le serveur n'a
encore rien reçu (listes vides).

**Client → serveur** :

```json
{"marques":{"head":[…],"favoris":[…],"epingles":[…],"archives":[…],"privees":[…],"moods":{…},"emojis":{…},"garder_archives":false,"cacher_apres_jours":0,"presence_en_ligne":true,"sons_conversation":"bulle","notes_contacts":true,"notes_musique":"","notes_musique_format":"artiste","notes_services":["tout"],"personnes":[…],"personnes_refus":[…],"version":7,"id":12}}
```

- `version` : la dernière version reçue du serveur (0 si aucune).
- `id` (facultatif, entier) : renvoyé tel quel dans la réponse **à ce client
  seulement**, pour qu'il reconnaisse la réponse à sa propre trame.
- Une liste absente ne change pas. Ids vides ou en double retirés ; au plus
  5000 ids par liste, 512 octets par id. Un client d'avant `archives` ou
  `privees` (qui n'envoie pas ce champ) ne les vide donc jamais : le serveur
  les garde. De même pour `moods` : absent, rien ne change ; présent, c'est
  l'objet complet (une clé absente = plus de mood pour cette conversation).
  Entrées à id ou nom vide retirées ; au plus 5000 entrées, 512 octets par
  id, 64 par nom. Le serveur envoie toujours `moods` (objet, jamais `null`).
  Même règle pour `emojis`, avec au plus **32 octets** par emoji (un
  graphème : 👍, ❤️, 👨‍👩‍👧…) ; plus long, l'entrée est retirée.
- `garder_archives` : absent, rien ne change (un client d'avant ce réglage
  ne le remet jamais à faux) ; présent, il est pris tel quel, même d'un
  client en retard (un oui/non : rien à unir, pas de garde-fou). Le serveur
  l'envoie toujours ; absent de `marques.json` (fichier d'avant) : faux.
- `cacher_apres_jours` : même règle (absent, rien ne change ; présent, pris
  tel quel). Négatif ou au-delà de 3650 : ramené à 0. Toujours envoyé ;
  absent de `marques.json` : 0.
- `presence_en_ligne` : même règle, mais **absent veut dire vrai** : absent
  de `marques.json` (fichier d'avant), le serveur le prend vrai ; un client
  qui reçoit des marques sans ce champ (serveur d'avant) garde le sien.
- `sons_conversation` : absent ou `null`, rien ne change ; une chaîne
  inconnue devient `"bulle"` ; un booléen (premiers clients) : `true` donne
  `"bulle"`, `false` donne `"aucun"`. Toujours envoyé (une chaîne) ; absent
  de `marques.json` : `"bulle"`. Un client qui reçoit des marques sans ce
  champ (serveur d'avant) garde le sien.
- `notes_contacts` : comme `presence_en_ligne` (absent veut dire vrai).
- `notes_musique` : absent ou `null`, rien ne change ; une chaîne est prise
  telle quelle (tronquée à 128 octets). Toujours envoyé ; absent de
  `marques.json` : `""`.
- `notes_musique_format` : absent ou `null`, rien ne change ; une valeur
  inconnue devient `"artiste"`. Toujours envoyé.
- `notes_services` : absent ou `null`, rien ne change ; une liste est prise
  telle quelle (sans doublon ni nom vide, 32 noms de 32 octets au plus) ;
  vide, ou contenant `"tout"` : `["tout"]`. Pas d'union : c'est un réglage.
  Toujours envoyé ; absent de `marques.json` : `["tout"]`.
- `personnes` : absent ou `null`, rien ne change (un client d'avant la fiche
  de la personne ne les efface pas) ; présent, c'est la liste complète. Un
  élément mal formé est sauté (pas toute la trame). Nettoyage : id de
  personne non vide, unique, 64 octets au plus ; chaque conversation dans
  **une seule** personne (la première qui la nomme) ; 32 conversations au
  plus par personne ; une personne de moins de **deux** conversations
  disparaît ; `principal` gardé seulement s'il est une de ses `convs` ;
  2000 personnes au plus. Toujours envoyé (liste, jamais `null`) ; absent
  de `marques.json` : `[]`.
- `personnes_refus` : même règle (absent ou `null`, rien ne change). Chaque
  refus est une paire de deux ids différents, rangée (le plus petit
  d'abord), sans doublon ; 5000 paires au plus. Toujours envoyé ; absent de
  `marques.json` : `[]`.

Règles du serveur :

1. `version` = la sienne : les listes sont prises telles quelles (une liste
   vide l'est exprès : c'est ainsi qu'un client à jour vide une liste).
2. `version` ≠ la sienne (client en retard) :
   - **garde-fou** : si une liste reçue est vide alors que celle du serveur
     ne l'est pas, la trame est **refusée** ; le client reçoit les marques
     du serveur, avec `"refus":"<texte>"` (et son `id`) ;
   - sinon, union : les listes du client dans leur ordre, puis les ids du
     serveur qu'elles n'ont pas. Pour `moods` (même garde-fou s'il est vide
     alors que celui du serveur ne l'est pas) : les entrées du client, puis
     celles du serveur pour les ids que le client n'a pas. De même pour
     `emojis`. Pour `personnes` (même garde-fou) : les personnes du client
     telles quelles (même id : celle du client gagne, entière), puis celles
     du serveur qu'il n'a pas, **moins** les conversations que le client
     relie déjà à une autre personne (une personne qui n'a plus deux
     conversations tombe). Pour `personnes_refus` (même garde-fou) : l'union
     des paires.
3. Si rien ne change, seul l'expéditeur reçoit l'état courant (avec son
   `id`). Sinon le serveur écrit `marques.json`, passe à `version+1` et
   diffuse les nouvelles marques à tous (l'expéditeur les reçoit avec son `id`).

Côté client (`src/distant.rs`, `SuiviMarques` ; `Marques.kt` sur Android) :
les marques reçues remplacent la copie locale, qui garde aussi la dernière
`version` reçue et un drapeau « changé ici, pas encore confirmé ». Une seule
trame en vol à la fois ; les changements faits entre-temps partent à la
réponse. À la réception :

- version 0 (serveur vide) : le client envoie ses marques locales, s'il en a ;
- jamais synchronisé avec ce serveur (première connexion, ou autre serveur),
  ou changé ici alors que la version a bougé : **union** — l'ordre du
  serveur, puis les ids locaux qui manquent — renvoyée au serveur si elle
  diffère ;
- changé ici et version inchangée : la copie locale est renvoyée ;
- sinon : les marques du serveur remplacent la copie locale.

Rien n'est mis en file pendant une coupure : à la reconnexion, le rejeu
donne l'état du serveur et la règle ci-dessus s'applique. Un serveur d'avant
`archives` (ou `privees`) n'envoie pas ce champ : le client garde alors ses
archives (ou ses privées) pour lui seul, sans les vider. De même pour
`garder_archives` : un serveur d'avant ce réglage ne l'envoie pas, et le
client garde le sien pour lui (il l'applique alors lui-même). Union de deux
copies : `garder_archives` de celle qui passe d'abord. Idem pour
`cacher_apres_jours`, `presence_en_ligne`, `sons_conversation`,
`notes_contacts`, `notes_musique`, `notes_musique_format` et
`notes_services`. Pour `personnes` et `personnes_refus`, l'union suit la
règle du serveur (personnes par id, celles de la copie qui passe d'abord
gagnent ; union des refus) ; un serveur d'avant ces champs ne les envoie
pas, et le client garde les siens pour lui.

**Règle des personnes** (fiche de la personne).

- Toucher le nom ou l'avatar dans l'en-tête d'une conversation **à deux**
  ouvre la fiche de la personne : grand avatar (la meilleure photo parmi
  les conversations reliées), présence, nom (et le nom sur chaque service
  s'il diffère), sa **pensée** (la note de §5.12, avec le temps restant),
  et une ligne par conversation reliée (service, nom, dernier message,
  non-lus, présence). Toucher une ligne ouvre cette conversation
  (« changer de service ») ; la conversation actuelle est marquée active.
  Les conversations restent **séparées** partout ailleurs (boîte de
  réception, non-lus, marques) : la fiche ne fait que les montrer ensemble.
- **Correspondance automatique**, faite par chaque client, **localement**,
  sans aucune requête aux services, avec les données déjà reçues (listes
  `conversations`, contacts du téléphone, notes, présence) :
  - **sûre** : le même numéro de téléphone (clé : les 10 derniers chiffres,
    7 au moins ; `(418) 555-1234`, `+1 418 555 1234` et `1-418-555-1234`
    sont le même). Les numéros connus : Google (`number` d'une conversation
    à deux, sinon son membre ou son nom s'il est un numéro), WhatsApp (l'id
    `<numéro>@s.whatsapp.net` ; un `@lid` n'en a pas), Signal et Telegram
    (champ `number` de la conversation à deux, quand le pont le connaît :
    contact Signal avec son E.164, contact Telegram dont le numéro est
    visible). Messenger, Instagram, X, Bluesky et Discord n'en donnent
    aucun ;
  - **suggérée** seulement, jamais sûre : un nom très proche (sans accents,
    ni casse, ni ponctuation, quel que soit l'ordre prénom/nom ; une faute
    de frappe tolérée sur les noms longs), ou le nom exact d'un contact du
    téléphone qui porte le numéro d'une conversation de la personne. La
    fiche demande « Est-ce la même personne ? Oui / Non ».
- **Correspondance à la main** : « Lier une autre conversation… » (une
  conversation à deux de n'importe quel service, choisie avec une recherche
  et des puces par service) ; « Oui » sur une suggestion fait de même. La
  conversation est ajoutée à la personne de la fiche (créée au besoin, avec
  un id neuf `p<ms><hasard>`), retirée de toute autre personne, et ses refus
  avec les conversations de la fiche sont effacés.
- « **Délier** » une ligne : la retire de sa personne et ajoute un refus
  entre elle et chaque autre conversation de la fiche (un même numéro ne la
  ramène donc pas). « **Pas la même personne** » sur une suggestion : un
  refus entre elle et chaque conversation de la fiche. Un refus l'emporte
  sur un numéro commun ; un lien à la main l'emporte sur un refus (lier
  efface les refus en cause).
- `principal` : « Utiliser ce nom » sur une ligne : le nom de cette
  conversation devient celui de la personne. Sans `principal`, la fiche
  prend le nom de la conversation d'où on l'a ouverte.
- Le serveur ne fait **aucune** correspondance : il garde, nettoie, unit et
  diffuse les deux listes.

**Règle des moods.**

- Un mood colore une conversation : mes bulles (couleur ou dégradé), une
  teinte légère des bulles reçues et le fond du fil (dégradé à taches floues,
  en variante sombre ou claire selon le thème). Les noms connus : `rose_bebe`,
  `amour`, `bleu_bebe`, `vert_pomme`, `lavande`, `peche`, `menthe`, `soleil`,
  `ocean`, `aurore`, `nuit`, `coucher`. Le serveur ne les vérifie pas ; un
  client ignore un nom qu'il ne connaît pas (couleurs par défaut).
- « Par défaut » retire l'entrée. Le thème EGA n'a pas de mood : il ignore
  ceux qui sont posés, sans les effacer.
- Chaque appareil garde aussi ses moods pour lui (bureau : `moods` de
  `reglages.json` ; Android : préférences privées de l'app) : sans serveur,
  ils restent locaux. Union de deux copies : celle qui passe d'abord gagne
  pour chaque id, l'autre ajoute les ids qui manquent.
- Compatibilité : un client d'avant `moods` ne l'envoie pas (le serveur
  garde les moods) ; un serveur d'avant `moods` ne l'envoie pas (le client
  garde les siens pour lui, sans les vider). Une conversation privée garde
  son mood.

**Règle des emojis rapides.**

- Facultatif, aucun par défaut. Choisi dans la feuille Mood (rangée
  « Emoji rapide » ; en EGA, item « Emoji rapide » du menu ⋮, car l'EGA n'a
  pas de mood) ; « Aucun » retire l'entrée.
- Le client montre alors un bouton rond avec l'emoji à droite de la zone
  d'écriture, à la place du bouton d'envoi tant que le champ est vide. Un
  appui l'envoie seul, comme un message texte ordinaire (`send`, avec son
  `tmp_id`) : rien de neuf pour les ponts.
- Comme les moods : gardé sur chaque appareil (bureau : `emojis` de
  `reglages.json` ; Android : préférences privées), union par id, et
  compatibilité dans les deux sens (`emojis` absent : rien n'est effacé ;
  serveur d'avant `emojis` : le client garde les siens). Le serveur ne
  vérifie que la longueur.

**Règle des archives.**

- Une conversation de `archives` sort de la boîte de réception (et des vues
  Favoris et par service) et va dans la vue Archives, avec les archives des
  services (`{"cmd":"conversations","folder":"archive"}`).
- Archiver l'ajoute à la fin de la liste ; Désarchiver l'en retire (« Annuler »
  fait l'inverse, de la même façon).
- **Un nouveau message reçu la ramène dans l'Inbox** : le serveur, à chaque
  événement `message` d'un pont dont `item.from_me` est faux et dont
  `item.time` (ms) est plus récent que le moment où l'id est entré dans
  `archives` (horloge du serveur, gardée dans `marques.json` sous
  `archives_depuis`, jamais envoyée), retire l'id, passe à `version+1` et
  diffuse les nouvelles marques à tous, **avant** l'événement `message`
  lui-même. Idempotent : un id qui n'y est plus ne change rien. Un message
  envoyé par moi (`from_me`), une réaction ou la mise à jour d'un message
  plus vieux que l'archivage ne la ramènent pas.
- **Sauf avec `garder_archives`** : le serveur ne retire alors rien
  d'`archives` (pas de nouvelle version) ; la conversation reste dans la vue
  Archives. L'événement `message` part comme d'habitude : chez les clients,
  elle devient non lue (pastille de la vue Archives) et notifie comme
  avant (bureau : bulle `notify-send` ; Android : notification ordinaire,
  sans raccourci de conversation, comme pour toute archivée). Désactivé, la
  règle reprend pour les messages suivants (ceux d'entre-temps ne ramènent
  rien après coup).
- Sans serveur (ponts locaux), le client applique la même règle lui-même, à la
  réception du message (Android : `Marques.desarchiverSiRecu`, avec l'heure où
  l'id est entré dans sa liste ; bureau : `desarchiver_si_recu`), en tenant
  compte de son `garder_archives` local. En mode client, il peut le faire
  aussi : la diffusion du serveur arrive d'abord, et l'id n'y est déjà plus.

**Règle des privées.**

- Une conversation de `privees` n'apparaît plus **nulle part** dans l'app
  normale : boîte de réception, Head, favoris, épinglées, archives, vues par
  service, recherche globale, médias, compteurs de non-lus et badges. Elle
  garde ses autres marques, qui ne se voient pas tant qu'elle est privée.
- Elle se voit seulement dans la vue **Privées**, ouverte par un **code à
  6 chiffres** propre à chaque appareil (Réglages > Liste privée). Le code
  n'est **jamais** envoyé au serveur ni synchronisé : chaque appareil garde
  son haché (PBKDF2-HMAC-SHA256, sel aléatoire ; bureau :
  `<données>/prive.json` en 0600 ; Android : préférences privées de l'app).
  Un appareil où la Liste privée n'est pas activée cache quand même les
  conversations de `privees` (il le dit dans ses réglages).
- **Aucune notification** pour une conversation privée (ni son nom, ni son
  texte, ni une bulle neutre).
- Le **MCP** (§9) ne les montre jamais : ni dans `lister_conversations`, ni
  `etat_services`, ni `chercher_messages` ; `lire_messages` et
  `envoyer_message` répondent « conversation inconnue ».
- « Rendre privée » l'ajoute à la fin de la liste ; « Retirer des privées »
  l'en retire. Désactiver la Liste privée sur un appareil propose de les
  remettre dans l'app (la liste est alors vidée, pour tous les appareils) ou
  de les garder cachées.
- Compatibilité : un client d'avant `privees` n'envoie pas ce champ (le
  serveur garde la liste) et ignore celui qu'il reçoit : il montre les
  privées comme des conversations normales. Mettre à jour tous les appareils.

### 5.6 Supprimer une conversation

```json
{"svc":"messenger","cmd":{"cmd":"delete","conv":"<id sans préfixe>"}}
```

Offert par Google (`pont`), Messenger (`pont-meta`) et Instagram
(`pont-ig`, et `pont-meta` quand il sert Instagram). Le client retire la
conversation tout de suite (liste, archives, messages, marques).

- **Google** : rien de particulier en cas de réussite (la conversation
  disparaît des listes suivantes) ; échec : `{"ev":"error","where":"delete","text":"suppression : …"}`.
- **Messenger** (`DeleteThreadTask`, comme mautrix-meta : `remove_type` 0,
  groupe de synchronisation 1 ; pour un fil chiffré, la clé de son fil
  Facebook d'origine et le groupe 95) et **Instagram** (mutation
  `ig_direct_hide_thread` d'instameow, avec l'identifiant court du fil) :
  - réussite : `{"ev":"deleted","conv":"<id>"}` (id sans préfixe, comme
    toujours côté pont), puis une nouvelle liste `conversations` (et celle des
    archives si elle a été demandée) sans elle ;
  - le même `deleted` arrive quand la conversation est supprimée ailleurs
    (l'app, un autre appareil) ;
  - échec : `{"ev":"error","where":"delete","text":"…"}`, puis la liste
    renouvelée, où la conversation réapparaît. Un fil chiffré de Messenger
    sans fil Facebook connu ne peut pas être supprimé par ce canal :
    l'erreur le dit (« supprime-le dans l'app Messenger »).
- C'est « Supprimer la discussion » : seulement pour moi, sur tous mes
  appareils ; un groupe n'est **pas** quitté.

### 5.6 bis Créer un groupe : `create_group`

```json
{"svc":"whatsapp","cmd":{"cmd":"create_group","members":["<id>","<id>"],"name":"Les amis"}}
```

Toutes les personnes viennent du **même service** (celui de la trame). Les
`members` sont des identifiants rendus par `search` (sans préfixe, comme
toujours côté pont) ou des numéros de téléphone ; jamais un groupe. Au moins
deux personnes, au plus 50 (moi en plus) ; `name` est facultatif.

| Service | Pont | Bibliothèque | Nom |
|---|---|---|---|
| `google` | `pont` | `GetOrCreateConversation` à plusieurs numéros (RCS si tous l'ont, sinon MMS), comme mautrix-gmessages | groupe RCS seulement |
| `messenger` | `pont-meta` | `CreateGroupTask`, puis `RenameThreadTask` pour le nom | oui |
| `instagram` | `pont-ig` | `CreateGroup` d'instameow | non (ignoré) |
| `whatsapp` | `pont-wa` | `CreateGroup` de whatsmeow | oui, 25 caractères ; sans nom : ceux des membres |
| `signal` | `pont-signal` | `CreateGroup` de signalmeow (les PNI sont invités) | oui, 32 caractères ; sans nom : ceux des membres |
| `telegram` | `pont-telegram` | `messages.createChat` (groupe simple) | oui, 128 caractères ; sans nom : ceux des membres |
| `discord` | `pont-discord` | `POST /users/@me/channels` (`recipients`, comme le client web), puis `PATCH` du salon pour le nom ; 9 personnes au plus (10 avec moi) | oui, 100 caractères ; sans nom : ceux des membres |

X (mautrix-twitter ne sait pas créer de groupe XChat) et Bluesky (les
messages privés n'y ont pas de groupes) ne l'offrent pas : le pont répond
`commande inconnue`, et l'interface n'offre pas « Créer un groupe ».

- Réussite : `{"ev":"conversation","item":{…},"open":true}`, comme `new` :
  l'interface ouvre le groupe.
- Échec : `{"ev":"error","where":"create_group","text":"…"}` (personne
  introuvable, groupe refusé par le service…).
- Le serveur la relaie telle quelle ; le MCP ne l'offre pas.

### 5.7 Reconnexion (côté client)

Une seule connexion par serveur, partagée par tous les services. Si elle
tombe : montrer `state: reconnecting` pour chaque service branché, puis
réessayer avec une attente progressive (1, 2, 4… 30 s ; 60 s après un refus
de jeton ou d'empreinte). Si le serveur n'a jamais répondu depuis le
lancement : `state: connecting`. Les commandes envoyées pendant la coupure
peuvent être gardées et envoyées après l'`auth` suivante.

### 5.8 Cache des listes et des messages

Le serveur garde, par service, la dernière liste `conversations` du pont
(boîte de réception, et archives si un client les a demandées) et les
derniers messages des conversations récentes. Le but : qu'un client n'ait
jamais à attendre le pont pour montrer une conversation.

**Rejeu des listes.** Après l'état du service (§5.2), à chaque client qui se
branche ou qui envoie `{"rejouer":…}`, le serveur renvoie les lignes
`conversations` gardées, **telles que le pont les a écrites**, dans cet
ordre : la boîte de réception, puis les archives (celle qui porte
`"folder":"archive"`) :

```json
{"svc":"google","ev":{"ev":"conversations","items":[{"id":"123","name":"…","time":1764000000000,…},…]}}
{"svc":"google","ev":{"ev":"conversations","folder":"archive","items":[…]}}
```

Rien de nouveau pour le client : ce sont des listes ordinaires. Elles sont
tenues à jour par les événements `conversation` (ajout, déplacement entre
boîte et archives) et `deleted` ; le client peut toujours redemander
`{"cmd":"conversations",…}`, transmis au pont comme avant. Gardées sur le
disque, elles sont rejouées même avant que le pont relancé ait répondu.
Oubliées à `state: unpaired`.

**Préchargement.** Quand un service passe à `ready`, puis à chaque nouvelle
liste de la boîte de réception, le serveur demande lui-même au pont
`{"cmd":"messages","conv":"<id>","count":40}` pour les **30**
conversations les plus récentes de la boîte de réception et pour celles du
Head, des favoris et des épinglées (§5.5) qui sont dans ses listes. Une à la
fois (il attend la réponse, 45 s au plus), avec 1,5 s de pause entre deux ;
seulement celles dont la copie manque, date d'avant le lancement du pont, ou
n'a pas le dernier message annoncé par la liste (`time`). Jamais une
conversation `large` (§5.13), même marquée. **Discord** :
8 conversations récentes, 30 messages chacune, 15 conversations au plus
(marquées comprises), 4 s de pause. Ces réponses (et
leurs erreurs `where: "messages"`) ne vont **qu'au cache** : aucun client ne
les reçoit. Aucun pont ne marque une conversation comme lue sur `messages`
(seulement sur `read`, que le serveur n'envoie jamais de lui-même).

**Le cache des messages.** Au plus **200** conversations × **60** messages
par service, dans `<données>/serveur/cache-<service>.json` (0600). Une page
`messages` du pont (pas `older`) fait foi dans sa plage ; les événements
`message` (nouveaux messages, réactions, modifications, retraits, accusés :
même `id`, ou même `tmp_id` pour un envoi confirmé) mettent à jour une
conversation déjà gardée ; `deleted` l'efface.

**Réponse immédiate.** Quand un client envoie `messages` **sans** `before`
pour une conversation dont le serveur a une copie à jour (reçue du pont
depuis son lancement, suivie ensuite par les événements), il lui répond
**aussitôt, à lui seul**, avec un événement `messages` au format habituel,
marqué `"cache":true` :

```json
{"svc":"google","ev":{"ev":"messages","conv":"123","items":[{"id":"…","conv":"123","text":"…","time":1764000000000,…},…],"older":false,"more":true,"cache":true}}
```

- `items` : les messages gardés, du plus vieux au plus récent, tels que le
  pont les a écrits (au plus `count` si la commande en donne un) ; `more` :
  il y en a de plus anciens (au pont).
- Puis il transmet la commande au pont comme avant ; la réponse du pont
  arrive ensuite, **sans** `cache`, à tous les clients. La copie passe
  toujours **avant** cette réponse (même file d'envoi).
- Pas de copie (conversation inconnue du cache, ou serveur fraîchement
  relancé) : seulement la réponse du pont, comme avant.

Côté client (référence : `fusionner_copie` et `fusionner_page` de
`src/cache.rs`), par **id** de message :

- `cache:true` : chaque message remplace celui de même `id` (ou même
  `tmp_id`) qu'on avait, les autres s'ajoutent, rien n'est retiré ; trier
  par `time`. Ne pas marquer la conversation comme lue sur la copie : c'est
  la réponse du pont qui le fait. L'indicateur de chargement peut tomber.
- la réponse du pont (sans `cache`, pas `older`) fait foi dans sa plage :
  garder de l'existant les envois locaux pas encore confirmés, les messages
  plus récents que la page et, si le plus vieux message de la page est déjà
  là, ceux qui sont plus vieux (les pages déjà remontées) ; le reste est
  remplacé par la page.

**Anciens clients.** Rien ne change pour eux : un client qui ignore `cache`
remplace sa liste par la copie, puis par la réponse du pont ; les listes
rejouées sont des `conversations` ordinaires.

### 5.9 Aperçus de liens

**Fournis par le service.** Un message peut porter un champ facultatif
`link_preview`, que le pont remplit quand le service envoie l'aperçu avec le
message (WhatsApp, Signal, Telegram, Discord, Messenger et Instagram ; Google Messages,
X et Bluesky n'en envoient pas) :

```json
{"id":"…","conv":"…","text":"Regarde https://exemple.com/page",
 "link_preview":{"url":"https://exemple.com/page","title":"…","description":"…","site":"…",
                 "image":"/…/media/apercus/ab12….jpg"}}
```

- `url` (obligatoire), `title`, `description`, `site` : du texte brut.
- La vignette, au plus un des trois : `image`, un fichier du dossier des
  médias écrit par le pont (WhatsApp, Signal : réécrit comme tout chemin,
  §6) ; `image_url`, une adresse https que le client va chercher lui-même
  (mêmes garde-fous qu'au-dessous) ; `image_media`, une pièce jointe au
  format de `attachments` (`media_id`, `key`, `mime`…), à demander au pont
  par `download` (Telegram, Messenger, Instagram).
- Un client qui ignore le champ n'y perd que la carte : l'adresse reste dans
  `text` (Messenger et Instagram l'y ajoutent si elle n'y était pas).

**Demandés au serveur.** Pour une adresse du texte sans `link_preview`, le
client peut demander l'aperçu au serveur (une trame à part, comme `mcp`) :

```json
{"apercu":{"id":12,"url":"https://fr.wikipedia.org/wiki/Cornichon"}}
```

Réponse, **à ce client seulement** (le même `id`, l'`url` telle que reçue) :

```json
{"apercu":{"id":12,"url":"…","title":"Cornichon — Wikipédia","description":"…","site":"fr.wikipedia.org","image":"/…/media/apercus/d171….jpg"}}
{"apercu":{"id":12,"url":"…","erreur":"HTTP 400"}}
```

- `url` : `http`, `https`, ou sans schéma (`www.exemple.com` → `https://`).
- Le serveur lit la page : **5 s** au plus par requête, **2 Mio** de HTML au
  plus, **5** redirections au plus, User-Agent d'un navigateur courant. Il
  prend `og:title`, `og:description`, `og:site_name`, `og:image`, puis les
  `twitter:*`, puis `<title>` et `<meta name="description">` ; sans titre
  ni description : pas d'aperçu (`erreur`). Un site qui refuse (Facebook,
  Instagram…) donne une `erreur`, rien d'autre.
- **Jamais d'adresse privée ou locale** : 127.0.0.0/8, 10.0.0.0/8,
  172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, 100.64.0.0/10, 0.0.0.0/8,
  ::1, fc00::/7, fe80::/10 et les autres plages réservées, `localhost`,
  `*.local`, `*.internal`. Vérifié sur l'adresse réellement jointe, après
  la résolution du nom et à chaque redirection : un nom qui se résout vers
  une telle adresse est refusé.
- L'image (`og:image`, 8 Mio au plus) est réduite à **640 px** de large et
  gardée en JPEG dans `<médias>/apercus/` : un chemin du serveur, à
  télécharger par `get` (§6.1). Absente si elle n'a pas pu être lue.
- **Cache** sur le disque : `<données>/serveur/apercus/<clé>.json` (0600),
  7 jours pour un aperçu, 6 heures pour un échec. Deux demandes de la même
  adresse en même temps n'en font qu'une ; 4 pages au plus en même temps.
- Un serveur d'avant cette trame ne répond pas : le client attend 12 s au
  plus, puis fait comme sans serveur.
- `jasette-serveur --apercu <url>` affiche l'aperçu d'une adresse (essai).

**Côté client.** Les adresses du texte sont cliquables (`http(s)://…` et
`www.…`, sans la ponctuation finale `.`, `,`, `)`, `!`, `?`…). L'aperçu du
service passe en premier ; sinon, si le réglage « Aperçus des liens » est
activé (il l'est par défaut), le client demande celui de la **première**
adresse au serveur, ou, sans serveur, va chercher la page lui-même avec les
mêmes garde-fous. Rien n'attend : la carte apparaît quand l'aperçu arrive.

### 5.10 Présence en ligne

Les ponts Messenger, Instagram, WhatsApp, Telegram et Discord annoncent qui
est en ligne, pour les conversations **individuelles** qu'ils connaissent :

```json
{"ev":"presence","item":{"conv":"4155551234@s.whatsapp.net","user":"4155551234@s.whatsapp.net","online":true,"last_active":1764160000000}}
```

- `conv` : la conversation individuelle (id du pont, sans préfixe ; le
  client ajoute le sien) ; `user` : la personne, dans les ids du service.
- `online` : en ligne maintenant. `last_active` (ms) : dernière activité
  connue ; **0** = inconnue (statut caché, « récemment »…).
- Une ligne par changement. Rien n'est rejoué par le pont lui-même.

**Le réglage.** La marque `presence_en_ligne` (§5.5, vraie par défaut) est
transmise aux ponts : variable d'environnement `JASETTE_PRESENCE=1` (ou `0`)
au lancement, puis la commande `{"cmd":"presence","on":true|false}` à
chaque changement, sans redémarrage. Sans la variable, un pont la croit
désactivée. Désactivée, le pont ne fait **rien** : aucun abonnement, aucune
demande, aucun événement.

**Le serveur** relaie `presence` à tous les clients, garde le dernier de
chaque conversation (500 au plus par service) et le rejoue après les
listes `conversations` (§5.2). Il oublie tout si le pont s'arrête ou si le
réglage est désactivé ; désactivé, il ne relaie plus rien.

**Les clients** montrent un point vert en bas à droite de l'avatar
(liste, Head, en-tête de la conversation) et, sous le nom dans l'en-tête,
« Actif maintenant » ou « Actif il y a 12 min » (rien au-delà de 24 h). En
mode autonome (ponts locaux), ils passent eux-mêmes la variable et la
commande à leurs ponts.

Par service :

- **WhatsApp** (whatsmeow) : activé, l'appareil lié se déclare **disponible**
  (`PresenceAvailable`) en permanence et s'abonne (`SubscribePresence`) aux
  50 conversations individuelles les plus récentes, à chaque connexion ;
  WhatsApp ne transmet la présence des autres qu'à un appareil disponible.
  L'abonnement vise le LID de la personne quand on le connaît (sinon son
  numéro) ; la présence reçue est ramenée au numéro de la conversation.
  Conséquences : les contacts vous voient « en ligne » tant que le pont
  tourne, et le téléphone ne sonne plus pour les messages WhatsApp tant
  qu'un appareil lié est disponible (comportement de WhatsApp Web).
  Désactivé : « indisponible », comme avant (disponible seulement pendant
  3 minutes après une action dans Jasette).
- **Messenger** et **Instagram** : le flux « PresenceUnifiedJSON » que
  les sites facebook.com et instagram.com ouvrent sur le socket
  « streamcontroller » de leur passerelle (gateway.facebook.com,
  gateway.instagram.com), à côté du socket de messagix ou d'instameow
  (presence_flux.go, commun aux deux ponts). Le pont écoute sans annoncer
  sa propre présence, et demande toutes les 2,5 minutes celle des personnes
  de ses conversations individuelles récentes (comme le site). Coupure :
  nouvel essai de 5 s à 5 min ; cinq échecs de suite, pause de 30 minutes ;
  session refusée, arrêt jusqu'à la prochaine connexion. Messenger lit
  aussi la table lightspeed `LSDeleteThenInsertContactPresence` si Meta la
  pousse encore. L'ancien `GET /api/v1/direct_v2/get_presence/` d'Instagram
  répond 404 : il n'est plus appelé.
- **Telegram** : le `status` de chaque personne (`userStatusOnline`,
  `userStatusOffline` avec `was_online`, sinon heure inconnue) et
  `updateUserStatus`. Le pont ne se déclare jamais en ligne lui-même.
- **Discord** : les présences que Discord pousse déjà au compte (READY, puis
  `PRESENCE_UPDATE`, pour les amis) ; aucune requête de plus. `online`,
  `idle` et `dnd` comptent comme en ligne ; `last_active` est le moment où
  le pont a vu la personne passer hors ligne, sinon 0. Le pont se déclare
  toujours « invisible ».

### 5.11 « En train d'écrire »

Tous les ponts annoncent qu'une personne écrit dans une conversation :

```json
{"ev":"typing","conv":"4155551234@s.whatsapp.net","on":true,"name":"Camille","audio":false}
```

- `conv` : la conversation (id du pont, sans préfixe) ; `on` : vrai, elle
  écrit ; faux, elle a arrêté.
- `name` (facultatif ; WhatsApp, Telegram) : qui écrit, pour les groupes.
  `audio` (facultatif) : elle enregistre un vocal.
- Messenger, Instagram, Google Messages et Signal envoient `on:false` ; X et
  certains services ne le font pas toujours : le client oublie la bulle
  après **6 s** sans nouvel événement, ou à l'arrivée d'un message de cette
  conversation.
- WhatsApp ne transmet les « en train d'écrire » qu'à un appareil
  disponible : voir §5.10.
- Messenger, fils **chiffrés** (cadenas) : ils passent par l'infrastructure
  de WhatsApp, qui ne les transmet qu'à un appareil disponible. Comme
  mautrix-meta, le pont se déclare disponible quand l'utilisateur ouvre un
  fil chiffré (`messages` sans `before`, `read`, `send`, `send_media`) et
  redevient indisponible 5 minutes après la dernière de ces commandes. Le
  réglage désactivé : jamais. Fils non chiffrés : la table lightspeed
  `LSUpdateTypingIndicator`, sans rien demander. Le pont note dans son
  journal le premier « en train d'écrire » reçu de chaque sorte (« premier
  « en train d'écrire » Messenger reçu », fil chiffré ou non chiffré) et
  chaque changement de disponibilité (« canal chiffré : disponible… »).
- Les commandes que le serveur envoie lui-même aux ponts (préchargement
  §5.8, outils MCP §9) portent `"auto":true` : ce n'est pas l'utilisateur
  qui ouvre le fil, le pont Messenger ne se déclare pas disponible.

Le serveur le relaie à tous les clients, sans le garder ni le rejouer. Les
clients le montrent (bulle reçue aux trois points qui rebondissent en bas du
fil, « écrit… » dans la liste et sous le nom dans l'en-tête) si la marque
`presence_en_ligne` est vraie : « Afficher qui est en ligne et qui écrit ».
Jasette n'envoie jamais son propre « en train d'écrire » (la commande
`typing` des ponts Signal et Telegram existe, l'interface ne s'en sert pas).

### 5.12 « Quoi de neuf ? » : notes et statuts

Une **note** (ou un statut) courte, publiée d'un geste sur plusieurs
services : la note des messageries Instagram et Messenger, le texte
« Infos » de WhatsApp, la bio de Telegram, la description du profil
Bluesky, le statut personnalisé de Discord. Google Messages et X ne
l'offrent pas ; Signal non plus pour l'instant (signalmeow ne sait pas
écrire le profil).

| Service | Pont | Ce qui change | Limite | Durée |
|---|---|---|---|---|
| `instagram` | `pont-ig` | note de la boîte de réception (`usePolarisCreateInboxTrayItemSubmitMutation`) | 60 caractères | 24 h (Instagram) |
| `messenger` | `pont-meta` | note de Messenger (`useMWInboxTrayCreateNoteMutation`, privacy `FRIENDS`) | 60 caractères | 24 h (Facebook) |
| `whatsapp` | `pont-wa` | « Infos » (`SetStatusMessage`, éphémère 24 h) | 139 caractères | 24 h |
| `telegram` | `pont-telegram` | bio (`account.updateProfile about`) | 70 caractères | jusqu'à l'effacement |
| `bluesky` | `pont-bluesky` | description du profil (`app.bsky.actor.profile`) | 256 caractères | jusqu'à l'effacement |
| `discord` | `pont-discord` | statut personnalisé (`PATCH /users/@me/settings`, `custom_status`) | 128 caractères | jusqu'à l'effacement |

(caractères = points de code Unicode.) Pour les services où c'est une bio
ou un statut durable (WhatsApp, Telegram, Bluesky, Discord), le pont **garde l'ancienne valeur** avant la
première note et la **remet** à l'effacement.

**Commandes** (à chaque service choisi, une trame par service ; le client
n'envoie qu'aux services compatibles **prêts**) :

```json
{"svc":"instagram","cmd":{"cmd":"note_set","text":"Au chalet ce weekend 🏔️","audience":0,"source":"main","req":"n17"}}
{"svc":"instagram","cmd":{"cmd":"note_clear","req":"n18"}}
{"svc":"instagram","cmd":{"cmd":"notes_list"}}
{"svc":"instagram","cmd":{"cmd":"notes_contacts","on":true}}
```

- `text` : la note, 1 caractère au moins, la limite du service au plus
  (au-delà : erreur, jamais de troncature silencieuse par le pont).
- `audience` (Instagram seulement) : **0** abonnés que je suis en retour
  (défaut), **1** amis proches. Messenger : toujours `FRIENDS`.
- `source` : `"main"` (écrite à la main, défaut) ou `"musique"` (§ plus
  bas). Une note `musique` **remplace** une note écrite à la main, que le
  pont **met de côté** (texte et date, dans `notes-etat.json`).
  `note_clear` avec `"source":"musique"` ne touche qu'à une note musicale :
  si une note écrite à la main est de côté et a moins de 24 h, le pont la
  **remet** (une seule publication : elle remplace la note musicale) ;
  sinon il efface. Une note actuelle écrite à la main n'est jamais effacée
  par la musique. Un `note_clear` ordinaire (`main`) efface tout, y compris
  la note mise de côté.
- `req` (facultatif) : renvoyé tel quel dans `note_result`.
- **Une action, une publication par service.** Le pont n'essaie jamais deux
  fois, ne boucle jamais ; il répond `ok` sans rien envoyer si le texte est
  celui de la note actuelle (et `note_clear` sans note actuelle). Garde-fou
  en plus : au plus **6** publications (création ou effacement) par heure
  glissante pour Instagram et Messenger, 20 pour les autres ; au-delà,
  erreur « trop de publications ».

**Événements du pont** :

```json
{"ev":"note_moi","text":"Au chalet ce weekend 🏔️","time":1764160000000,"expires":1764246400000,"source":"main","audience":0}
{"ev":"note_moi","text":"🎵 Écoute Daft Punk","time":1764163000000,"expires":1764249400000,"source":"musique","audience":0,"main_text":"Au chalet ce weekend 🏔️","main_time":1764160000000}
{"ev":"note_result","op":"set","ok":true,"req":"n17"}
{"ev":"note_result","op":"set","ok":false,"error":"La note Instagram n'a pas pu être publiée (requête périmée).","req":"n17"}
{"ev":"notes","items":[{"conv":"340282366841710300949128","user":"1784…","username":"camille.t","text":"🎂 30 ans !","time":1764150000000}],"time":1764160000000}
```

- `note_moi` : **ma note actuelle** sur ce service (`text` vide : aucune ;
  `expires` 0 : sans échéance). Émis dès que le pont est prêt (le pont la
  garde dans `<données>/<service>/notes-etat.json`, 0600 ; une note échue
  n'est plus annoncée), puis à chaque changement. `main_text` et
  `main_time` (facultatifs) : la note écrite à la main mise de côté par la
  musique. Le **serveur garde le
  dernier** de chaque service et le **rejoue** (§5.2, après `avatar_moi`).
- `note_result` : le résultat d'une commande (`op` : `set` ou `clear`),
  `ok`, ou `error` : un texte clair, en français, pour l'utilisateur. Un
  `doc_id` périmé ou une erreur GraphQL donnent une erreur, jamais un
  plantage. Pas rejoué.
- `notes` (Instagram) : les **notes des contacts**, une par auteur, liste
  complète à chaque fois (remplace la précédente). `conv` : la conversation
  individuelle de l'auteur si le pont la connaît (sinon absent) ; `user` :
  son id Instagram ; `time` (ms) : sa date. Ma propre note n'y est pas.

**Lecture des notes des contacts.** Seulement si la marque
`notes_contacts` est vraie (transmise comme `presence_en_ligne` :
`JASETTE_NOTES=1|0` au lancement, puis `notes_contacts`). Le pont lit alors
le plateau (`IGDInboxTrayQuery`) dès qu'il est prêt, puis **au plus toutes
les 15 minutes** ; `notes_list` rend la dernière lecture si elle a moins de
15 minutes, sinon en refait une. Trois échecs de suite : pause d'une heure.
Désactivé : aucune lecture, aucun événement ; le serveur oublie `notes` et ne
le relaie plus.

**Le serveur** relaie les commandes au pont du service, garde le dernier
`note_moi` de chaque service et la dernière liste `notes` (rejoués), et
relaie `note_result` à tous les clients.

Discord : le pont reste « invisible » (aucune présence envoyée) ; le statut
personnalisé se voit quand le compte est en ligne ailleurs (téléphone,
ordinateur).

**Les `doc_id` de Meta** (Instagram, Messenger) : valeurs par défaut dans
le code, remplaçables **sans recompiler** par
`<données>/instagram/notes-docs.json` et `<données>/messenger/notes-docs.json`
(lus à chaque requête), `{"nom de la requête":"doc_id"}` :

```json
{"IGDInboxTrayQuery":"29231580869776032",
 "usePolarisCreateInboxTrayItemSubmitMutation":"28592645767037889",
 "usePolarisDeleteInboxTrayItemSubmitMutation":"28419182984337833"}
```

```json
{"MWInboxTrayNoteCreationDialogQuery":"30899655739648624",
 "useMWInboxTrayCreateNoteMutation":"25742693715382390",
 "MWInboxTrayNoteCreationDialogCreationStepContentMutation":"24060573783603122",
 "useMWInboxTrayDeleteNoteMutation":"9532619970198958"}
```

Les requêtes passent par la voie **web** de la session du pont (Instagram :
`POST https://www.instagram.com/api/graphql`, en-tête
`X-Root-Field-Name` ; Messenger : `POST /api/graphql/` du domaine de la
session, `www.facebook.com` pour une session Facebook), une seule fois,
sans nouvel essai automatique. Messenger n'essaie la mutation de repli
(`…CreationStepContentMutation`) que si Facebook dit ne pas connaître la
première (document inconnu) : jamais deux créations.

**Les clients.**

- Écran « Moi » (bureau et Android) : la bulle « Quoi de neuf ? » montre
  la note actuelle (la plus récente des `note_moi`), un champ, Publier et
  Effacer, le nombre de caractères restants pour le service **le plus
  limitant** parmi ceux choisis ; « Publier sur : Tout ▸ » (replié : une
  case « Tout », puis une par service compatible connecté ; l'audience
  Instagram) modifie la marque `notes_services`, comme le même sélecteur de
  Réglages > Quoi de neuf. Après la publication, un ✓ ou l'erreur par
  service (`note_result`).
- Head : la note Instagram d'un contact, en petite bulle au-dessus de son
  avatar (texte tronqué), si `notes_contacts`.
- Sans serveur (ponts locaux), le client fait le travail du serveur :
  variables `JASETTE_NOTES`, commande `notes_contacts`.

**« Ce que j'écoute » (Spotify).** Aucune API Spotify : l'appareil lit son
lecteur local (bureau Linux : MPRIS `org.mpris.MediaPlayer2.spotify`, titre,
artiste et `PlaybackStatus` ; Android : `MediaSessionManager` du paquet
`com.spotify.music`, avec l'accès aux notifications). Seul l'appareil
nommé par `notes_musique` publie, aux services de `notes_services`, avec
`"source":"musique"`, un texte tronqué (…) à la limite du service le plus
limitant, selon `notes_musique_format` :

- `artiste` : « 🎵 Écoute <artiste> » ; un balado : « 🎙️ Écoute
  <émission> » (le nom de l'émission, pas de l'épisode) ;
- `genre` : « 🎵 Écoute du rock ! » ; un balado : « 🎙️ Écoute un
  podcast ». Genre inconnu : comme `artiste`. Le genre vient de
  **MusicBrainz** (API publique sans clé, `GET
  https://musicbrainz.org/ws/2/artist/?query=artist:"<nom>"&fmt=json` :
  les `tags` du premier artiste, le plus voté ; traduit pour les genres
  courants, sinon tel quel), **seulement** avec ce format, au plus une
  requête par seconde, User-Agent `Jasette/0.1
  (https://jonathanguernon.net/jasette)` sans aucune donnée personnelle, et
  un cache local artiste → genre (jamais deux fois le même artiste) ;
- `titre` : « 🎵 Titre — Artiste ».

En anglais : « Listening to Daft Punk », « Listening to rock! »,
« Listening to a podcast ». Un balado se reconnaît à `mpris:trackid`
contenant `spotify:episode:` ou `/episode/` (une chanson :
`spotify:track:`), l'émission étant dans `xesam:album` (sinon
`xesam:artist`) ; sur Android, à l'id du média (`METADATA_KEY_MEDIA_ID`)
contenant `episode`, l'émission dans l'album, sinon l'artiste (supposé, à
vérifier au premier essai).

L'artiste (ou le genre) retenu pour une période de 15 minutes est celui qui
a **joué le plus longtemps** pendant cette période, pas seulement le
dernier. Anti-rafale, sur l'appareil :

- au plus **une** publication toutes les **15 minutes**, et seulement si le
  texte a changé depuis la dernière publication musicale (ou si la note
  actuelle n'est pas musicale) ;
- **la musique passe par-dessus la note écrite à la main** pendant qu'elle
  joue (le pont met celle-ci de côté) ; l'écran « Moi » le dit : « 🎵 La
  musique remplace ta note pendant que tu écoutes » ;
- une pause n'efface rien : après **20 minutes** sans lecture, une seule
  fois, `note_clear` avec `"source":"musique"` ; le pont remet la note
  écrite à la main si elle a moins de 24 h, sinon il efface.

### 5.13 Groupes : canaux, serveurs et communautés

L'onglet **Groupes** (Android : barre du bas `Inbox · Favoris · Groupes ·
Archives · Moi` ; bureau : une entrée de la colonne de gauche) réunit
**seulement les grands espaces** de type canal ou communauté. Les groupes de
personnes (Messenger, WhatsApp, Signal, Instagram, Google RCS/MMS, X,
Bluesky, groupes Telegram ordinaires, groupes privés Discord) n'y vont
**pas** : ils restent dans la boîte de réception, comme avant.

**Champs d'une conversation** (événements `conversations` et
`conversation`, tous facultatifs ; un pont d'avant ne les envoie pas et
tout reste comme avant) :

- `large` (booléen) : un grand espace (voir `kind`). Seul ce champ décide
  du classement chez les clients.
- `kind` (chaîne) : la sorte de grand espace, pour l'icône et le
  regroupement :
  - `"server"` : un salon texte ou d'annonces d'un **serveur Discord** ;
  - `"channel"` : un **canal** Telegram (diffusion) ;
  - `"supergroup"` : un **supergroupe** Telegram ;
  - `"community"` : une **communauté** WhatsApp (le groupe parent) ;
  - `"community_group"` : un groupe rattaché à une communauté WhatsApp (dont
    son groupe d'annonces : voir `announce`) ;
  - `"newsletter"` : une **chaîne** WhatsApp ;
  - `"community_chat"` : une discussion de communauté d'un Groupe Facebook
    (Messenger) ;
  - `"broadcast"` : un **canal de diffusion** (Instagram, Messenger).
  Un client ignore une valeur qu'il ne connaît pas (icône générique).
- `announce` (booléen) : seuls les administrateurs y écrivent (groupe
  d'annonces WhatsApp, salon d'annonces Discord).
- `parent` (chaîne) : l'id, propre au service et **sans préfixe**, de
  l'espace qui le contient : le serveur Discord (`g<guild>`), la
  communauté WhatsApp (l'id de la conversation parente, si elle est dans
  la liste). `parent_name` : son nom.
- `section` (chaîne) : la catégorie du salon (Discord) ; `section_order`
  (entier) : sa position ; `order` (entier) : la position du salon dans sa
  catégorie. Les clients rangent par `section_order`, puis `order`, puis
  nom ; un salon sans catégorie passe en tête.
- `mentions` (entier) : les non-lus qui me mentionnent, quand le service
  le compte lui-même (Discord : `mention_count` du `read_state` ;
  Telegram : `unread_mentions_count`).

**Champs d'un message** (événements `message` et `messages`, facultatifs) :

- `mentions_me` (booléen) : le message me mentionne (Discord : `@moi`,
  `@everyone`, `@here`, un de mes rôles ; Telegram : mention ou
  `@moi` ; WhatsApp : mention de mon numéro ou de mon LID) ;
- `reply_to_me` (booléen) : il répond à un de mes messages.
- `sender_id` (chaîne) : l'id de l'auteur chez le service, sans préfixe
  (Discord : salons et groupes privés). Sa photo arrive à part, par
  `sender_avatar`.

**Images** (événements facultatifs ; `path` est réécrit en mode serveur
comme toute clé `path`, §6) :

- `{"ev":"avatar","conv":"<parent>","path":…}` : l'icône d'un **parent**
  (serveur Discord `g<guild>`…), comme l'avatar d'une conversation ; le
  client préfixe `conv` comme d'habitude. Les clients la montrent sur la
  ligne du parent, dans son en-tête, et pour ses salons sans image propre.
  Sans icône : l'initiale du nom.
- `{"ev":"sender_avatar","service":"discord","id":"<sender_id>","path":…}` :
  la photo d'un auteur, montrée à côté de ses messages dans un espace ou un
  groupe. Le pont Discord la télécharge **paresseusement** : seulement
  pour les messages chargés ou reçus, une fois par adresse, au plus 20 par
  minute (en plus du rythme du CDN) ; une photo déjà sur le disque est
  redonnée sans réseau avec chaque page d'historique. L'avatar propre au
  serveur (`member.avatar`) d'abord, sinon celui du compte ; sans avatar,
  rien : les clients mettent l'initiale.

**Comment un pont fait apparaître des espaces dans Groupes** : les clients
ne connaissent aucun service en particulier dans l'onglet Groupes ; tout
repose sur les champs ci-dessus. Un pont (Telegram, WhatsApp, Messenger,
un futur pont Reddit…) qui les émet y apparaît sans changer les clients.

1. `large: true` : la conversation va dans Groupes, dans la section de son
   service (une par service, repliable).
2. Sans `parent` : une ligne à elle dans la section (photo, nom, aperçu),
   avec l'icône de sa sorte (`kind`).
3. Avec `parent` : les conversations du même `parent` se rangent sous
   **une ligne de parent** (nom `parent_name`, icône par l'événement
   `avatar` du parent, nombre de non-lus et de mentions). La toucher ouvre
   le parent : ses salons rangés par `section` / `section_order` / `order`
   / nom. Si le parent est lui-même une conversation de la liste (une
   communauté WhatsApp), elle passe en tête de ses salons, sans ligne à
   part.
4. Les mentions (`mentions`, `mentions_me`, `reply_to_me`) se comptent sur
   le salon et sur le parent, une fois chacune.

**Tout marquer comme lu** : l'en-tête de chaque section de service et
l'en-tête d'un parent ouvert portent un bouton « enveloppe ouverte »
(caché sans non-lus). Il ouvre une confirmation (« Tout marquer comme
lu ? » ; « Les 12 salons non lus de Discord seront marqués comme lus. » ou
« Les 4 salons non lus de *Pixel Nord*… »). Confirmé, le client marque lus
tout de suite, chez lui, les espaces non lus (ou avec mentions) de la
section ou du parent, et envoie un `read` ordinaire pour **chacun
d'eux seulement** (jamais pour un espace déjà lu). Les ponts les étalent :
Discord les passe au rythme REST (400 ms entre requêtes ; pas d'ack groupé,
`/read-states/ack-bulk` n'est pas dans discordgo) et n'ack qu'un salon qui
a des non-lus ; Telegram ne fait rien pour un dialogue sans non-lu ni
mention, et sinon espace ses `read` d'au moins 400 ms.

**Règle de la boîte de réception** (côté clients ; le serveur ne filtre
rien) : « la boîte de réception, c'est le chat ».

1. Une conversation sans `large` va dans la boîte de réception, comme
   avant, et **jamais** dans Groupes.
2. Une conversation `large` va **seulement** dans Groupes, **jamais** dans
   la boîte de réception ni dans les vues par service, même avec une
   mention, une réponse à moi ou une épingle. Favoris, Head et Privées
   gardent leurs propres règles.
3. Les mentions se voient **dans Groupes** : pastille `@N` sur le salon et
   sur le serveur (`mentions`, plus les messages reçus avec `mentions_me`
   ou `reply_to_me` depuis la dernière lecture, gardés en mémoire par
   chaque client), et pastille de l'onglet Groupes (rouge s'il y a une
   mention). Les pastilles de l'Inbox ne comptent pas les espaces `large`.
4. Notifications : une conversation `large` ne notifie que sur une mention
   ou une réponse à moi. Toucher la notification ouvre l'espace par
   l'onglet Groupes (au bureau : le serveur Discord qui le contient).

**Présentation** : une section repliable par service (badge du service,
nombre d'espaces non lus ; l'état replié est gardé sur l'appareil), puis
les règles génériques ci-dessus : Discord montre ses serveurs, WhatsApp
ses communautés, Messenger les Groupes Facebook de ses discussions de
communauté, Telegram ses canaux et supergroupes (sans parent).

**Préchargement** : le serveur ne précharge **jamais** les messages d'une
conversation `large` (§5.8) ; l'historique n'est demandé qu'à l'ouverture.

**Ce que chaque pont expose** :

- Discord : salons texte et d'annonces des serveurs (§5.4), `mentions`,
  `mentions_me`, `reply_to_me`.
- Telegram : canaux (`channel`) et supergroupes (`supergroup`) ;
  `mentions` (`unread_mentions_count` des dialogues) ; `mentions_me`
  (drapeau `mentioned`, levé aussi pour une réponse à moi) ; `reply_to_me`
  si le message cité est dans la base du pont. `read` lit aussi les
  mentions (`messages.readMentions`) quand il y en a, sinon Telegram les
  rendrait à la liste suivante.
- WhatsApp : communautés (`community`) et leurs groupes
  (`community_group`, `parent` = JID du parent), `announce` ;
  `mentions_me` (mon numéro ou mon LID), `reply_to_me`. **Pas les chaînes**
  (`newsletter`) : le pont les écarte ; il faudrait
  `GetSubscribedNewsletters` à la connexion et `GetNewsletterMessages` pour
  l'historique (requêtes GraphQL aux identifiants changeants), et éviter
  tout accusé de lecture ou de vue.
- Messenger : discussions de communauté (`community_chat` :
  `COMMUNITY_GROUP`, `COMMUNITY_SUB_THREAD`, `DISCOVERABLE_PUBLIC_CHAT`…)
  et canaux de diffusion (`broadcast` : `COMMUNITY_BROADCAST_*`,
  `DISCOVERABLE_PUBLIC_BROADCAST_CHAT*`), `parent` = le fil du Groupe
  Facebook. Seulement ceux que messagix synchronise (groupes de synchro 1
  et 95) : une discussion jamais ouverte peut manquer. Elles ne vont plus
  dans les archives (dossier « community »).
- Instagram : **rien** pour l'instant ; les types d'instameow ne
  distinguent pas les canaux de diffusion (le pont journalise
  `sous_type` et `diffusion` en débogage pour les reconnaître plus tard).
- Google Messages, Signal, X, Bluesky : seulement des groupes de
  personnes ; rien de neuf.

## 6. Fichiers

Les événements contiennent des chemins de fichiers **du serveur** :
`avatar.path`, `avatar_moi.path`, `avatars_groupe.members[].path`,
`media.path`… (les ponts WhatsApp, Signal et X n'utilisent que `path` :
`avatar`, `avatar_moi`, `media` ; leurs fichiers sont sous
`<médias>/avatars/` et `<médias>/pieces/`). Le client les télécharge dans un cache local et réécrit les
chemins avant de passer l'événement à l'interface. Règle de `distant.rs` :
toute valeur chaîne qui **commence par `/`** sous une clé `path`, `avatar`,
`photo`, `image`, `thumb` ou `fichier`, à toute profondeur. Si le
téléchargement échoue, l'événement passe tel quel.

### 6.1 Télécharger : `get`

```json
{"get":{"id":7,"chemin":"/home/…/media/avatars/ab12.jpg","sha256":"<hex ou vide>"}}
```

- `id` : entier choisi par le client, unique parmi ses requêtes en cours.
- `sha256` : celui de la copie locale, si elle existe ; si le fichier n'a
  pas changé, le serveur répond sans le renvoyer.

Réponses (trames `fichier`, intercalées avec les événements) :

```json
{"fichier":{"id":7,"taille":51234,"sha256":"…","donnees":"<base64>","fin":false}}
{"fichier":{"id":7,"donnees":"<base64>","fin":false}}
{"fichier":{"id":7,"donnees":"<base64>","fin":true}}
{"fichier":{"id":7,"inchange":true,"taille":51234,"sha256":"…","fin":true}}
{"fichier":{"id":7,"erreur":"chemin refusé","fin":true}}
```

Morceaux de 192 Kio au plus (avant base64), dans l'ordre ; le dernier porte
`fin: true` (il peut être vide). Écrire dans un fichier temporaire, puis
renommer.

Le serveur ne sert que des fichiers réguliers :

- sous le dossier des médias (`JASETTE_MEDIA`, sinon `<données>/media`) ;
- ou sous le dossier des données de Jasette, mais seulement des images,
  sons, vidéos, PDF et vCard (selon l'extension) — jamais `session.json`, les
  bases ou les cookies ;
- jamais sous `<données>/serveur` (clé, certificat, jetons, dépôts) ;
- chemin absolu, sans aucun segment `..` ; les liens symboliques sont
  résolus et la cible doit rester dans ces racines ; le dernier élément est
  ouvert sans suivre de lien.

### 6.2 Téléverser : `put` (pour `send_media`)

```json
{"put":{"id":8,"nom":"photo.jpg","donnees":"<base64>","fin":false}}
{"put":{"id":8,"donnees":"<base64>","fin":false}}
{"put":{"id":8,"donnees":"<base64>","fin":true}}
```

- `nom` dans la première trame (nettoyé par le serveur : nom de base, sans
  `/` ni `.` de tête). Morceaux de taille libre (une ligne ≤ 4 Mio ; 256 Kio
  conseillés). Un fichier vide : une seule trame, `donnees` vide, `fin: true`.
- 200 Mio au plus par fichier, 8 dépôts en cours au plus par connexion.
- Réponse, à la fin (ou dès une erreur) :

  ```json
  {"depose":{"id":8,"chemin":"/…/serveur/depots/9f3c…/photo.jpg","taille":123456}}
  {"depose":{"id":8,"erreur":"fichier trop gros"}}
  ```

- Ensuite : `{"svc":"google","cmd":{"cmd":"send_media","path":"<chemin>",…}}`.
  Les dépôts sont effacés au bout d'une heure, ou si la connexion tombe en
  plein téléversement.

## 7. Données du serveur

`<données>` = `JASETTE_DATA`, sinon `$XDG_DATA_HOME/jasette`, sinon
`~/.local/share/jasette` (le même dossier que les ponts).
Chaque pont a son dossier, en 0700 : `<données>/whatsapp/`,
`<données>/signal/`, `<données>/x/`, `<données>/telegram/`, `<données>/bluesky/`, `<données>/discord/` (bases SQLite et session : jamais
servis par `get`, §6.1).
`<données>/serveur/` (0700) contient `cert.pem`, `cle.pem`,
`appareils.json` (noms, SHA-256 des jetons, dates), `jumelage.json` (code en
cours), `marques.json` (§5.5, avec `archives_depuis` en plus ; écrit par fichier temporaire puis renommage,
la version précédente dans `marques.json.bak` ; un fichier illisible est mis
de côté, jamais écrasé), `cache-<service>.json` (listes et messages du §5.8 ;
un fichier illisible est ignoré) et `depots/`, tous en 0600. Les journaux (sortie d'erreur) ne
contiennent jamais de jeton, de code ni de cookie.

## 8. Résumé des trames

| Sens | Trame | Quand |
|---|---|---|
| S→C | `{"jasette":"serveur","version":1,"nonce":…}` | accueil |
| C→S | `{"jumeler":{"nom","nonce","preuve"}}` | jumelage |
| S→C | `{"jumele":{"jeton","nom","preuve"}}` | jumelage réussi |
| C→S | `{"auth":{"jeton"}}` | connexion |
| S→C | `{"auth":"ok","nom","services":[…]}` | connexion acceptée |
| S→C | `{"erreur":…,"texte":…}` | refus, puis fermeture |
| C→S | `{"svc":…,"cmd":{…}}` | commande pour un pont |
| S→C | `{"svc":…,"ev":{…}}` | événement d'un pont |
| S→C | `{"svc":…,"ev":{"ev":"messages",…,"cache":true}}` | copie du cache, avant la réponse du pont (§5.8) |
| C→S | `{"rejouer":"<svc>"}` | redemander l'état d'un service |
| ↔ | `{"ping":n}` / `{"pong":n}` | signes de vie |
| S→C | `{"marques":{"head","favoris","epingles","archives","privees","moods","emojis","garder_archives","cacher_apres_jours","presence_en_ligne","sons_conversation","notes_contacts","notes_musique","notes_musique_format","notes_services","version"[,"id","refus"]}}` | rejeu, changement |
| C→S | `{"marques":{"head","favoris","epingles","archives","privees","moods","emojis","garder_archives","cacher_apres_jours","presence_en_ligne","sons_conversation","notes_contacts","notes_musique","notes_musique_format","notes_services","version"[,"id"]}}` | changement local |
| S→C | `{"svc":…,"ev":{"ev":"presence","item":{"conv","user","online","last_active"}}}` | présence (§5.10), aussi au rejeu |
| S→C | `{"svc":…,"ev":{"ev":"typing","conv","on"[,"name","audio"]}}` | « en train d'écrire » (§5.11) |
| C→S | `{"svc":…,"cmd":{"cmd":"note_set","text","audience","source"[,"req"]}}` | publier ma note (§5.12) |
| C→S | `{"svc":…,"cmd":{"cmd":"note_clear"[,"source","req"]}}` | effacer ma note (§5.12) |
| S→C | `{"svc":…,"ev":{"ev":"note_moi","text","time","expires","source","audience"}}` | ma note actuelle (§5.12), aussi au rejeu |
| S→C | `{"svc":…,"ev":{"ev":"note_result","op","ok"[,"error","req"]}}` | résultat d'une publication (§5.12) |
| S→C | `{"svc":"instagram","ev":{"ev":"notes","items":[…],"time"}}` | notes des contacts (§5.12), aussi au rejeu |
| C→S | `{"get":{"id","chemin","sha256"}}` | télécharger |
| S→C | `{"fichier":{"id",…}}` | morceaux ou erreur |
| C→S | `{"put":{"id","nom","donnees","fin"}}` | téléverser |
| S→C | `{"depose":{"id","chemin"|"erreur"}}` | fin du dépôt |
| C→S | `{"apercu":{"id","url"}}` | aperçu d'un lien (§5.9) |
| S→C | `{"apercu":{"id","url","title","description","site","image"|"erreur"}}` | réponse, à ce client seul |
| C→S | `{"mcp":{"id","action"[,"envoi"]}}` | connexion MCP pour Claude (§9) |
| S→C | `{"mcp":{"id","ouvert","jeton","port","adresses",…}}` | état de la connexion MCP |

## 9. Connexion MCP (Claude)

Le serveur peut offrir un **serveur MCP** (Model Context Protocol) pour que
Claude (claude.ai, Claude Code) lise les messageries de l'utilisateur. Il est
**fermé par défaut** et ne s'ouvre que sur demande (bouton « Ouvrir ma
connexion MCP » des réglages, ou `jasette-serveur --mcp ouvrir`).

### 9.1 Transport et jeton

- **Streamable HTTP** (SDK Go officiel `github.com/modelcontextprotocol/go-sdk`),
  en HTTP simple, sur son **propre port** : **47810** par défaut
  (`--mcp-port`, ou `JASETTE_MCP_PORT`), sur les mêmes interfaces que le
  serveur (toutes, ou `--ecoute`), donc aussi `127.0.0.1`. Point d'accès :
  `http://<hôte>:47810/mcp`.
- Chaque requête exige le **jeton** : `Authorization: Bearer <jeton>`, ou,
  pour un client qui ne sait pas envoyer d'en-tête (claude.ai), l'URL
  secrète `/mcp/<jeton>`. Sinon : `401` ; après 20 jetons faux en
  10 minutes depuis une adresse : `429`. Fermé : rien n'écoute.
- Le jeton : 32 octets aléatoires en base64url (43 caractères), créé à
  l'ouverture, gardé dans `<données>/serveur/mcp.json` (0600). **Fermer le
  révoque** (une réouverture en crée un autre) ; « régénérer » le remplace :
  les sessions de l'ancien tombent aussitôt. Il n'est jamais écrit dans les
  journaux (ni l'URL qui peut le porter).
- La protection DNS-rebinding « hôte local » du SDK est désactivée : derrière
  un tunnel ou un mandataire, les requêtes arrivent sur 127.0.0.1 avec le nom
  public dans `Host`. Le jeton protège chaque requête.
- HTTP simple : sur le réseau local, le jeton passe en clair. Pour claude.ai,
  il faut de toute façon une URL **HTTPS publique** devant le port 47810
  (Tailscale Funnel, Cloudflare Tunnel, mandataire inverse avec certificat).

### 9.2 Outils

Ids de conversation préfixés comme les marques (§5.5). Les données viennent
du cache du serveur (§5.8) et, au besoin, du pont.

| Outil | Rôle |
|---|---|
| `lister_conversations` | conversations, de la plus récente : `service`, `nom`, `dernier_message`, `non_lu`, `marques`, `archivee` ; filtres `service`, `marque` (`head`, `favoris`, `epingles`, `archives`), `non_lus_seulement`, `inclure_archives`, `limite` |
| `lire_messages` | messages d'une conversation (`conversation`, `nombre` ≤ 100) ; plus anciens : repasser `suite` dans `avant` |
| `chercher_messages` | `texte` (sans accents ni casse) dans la copie du serveur et, sauf Google, par `search_messages` des ponts |
| `etat_services` | état, compte, conversations et non lues de chaque service |
| `envoyer_message` | **seulement si permis** (case à part, non par défaut, remise à non à la fermeture) : `conversation` existante, `texte` ≤ 4000 caractères ; 5 envois par minute et 30 par heure au plus |

Rien d'autre : aucun outil ne marque comme lu, n'archive, ne supprime ni ne
réagit.

- **Les conversations privées** (marque `privees`, §5.5) sont exclues de
  tous les outils : absentes des listes, des comptes et des résultats de
  recherche ; `lire_messages` et `envoyer_message` les disent inconnues.

- **Rien n'est marqué comme lu.** Le serveur n'envoie jamais `read`, et la
  commande `messages` des ponts ne marque rien. Les réponses des ponts aux
  demandes du MCP (`messages`, `search_messages`, et leurs `error`) ne vont
  **qu'au MCP** (et au cache), jamais aux appareils : leur interface ne les
  prend pas pour une conversation ouverte. S'il y a en même temps une
  demande d'un appareil pour la même conversation, la réponse lui revient
  d'abord. Une liste `conversations` redemandée par le MCP (quand celle du
  cache date de plus d'une minute) va à tous, comme d'habitude, et l'envoi
  (`sent`, `message`) aussi.
- **Journal** : chaque appel d'outil (nom, service ou conversation, nombre
  de caractères, résultat, durée), jamais le contenu d'un message ni le
  texte cherché.

### 9.3 Commande des appareils : trame `mcp`

C→S (une connexion authentifiée, §4) :

```json
{"mcp":{"id":4,"action":"etat"}}
{"mcp":{"id":5,"action":"ouvrir"}}
{"mcp":{"id":6,"action":"fermer"}}
{"mcp":{"id":7,"action":"regenerer"}}
{"mcp":{"id":8,"action":"envoi","envoi":true}}
```

S→C, **à ce client seulement**, après l'action :

```json
{"mcp":{"id":5,"ouvert":true,"jeton":"<43 caractères>","envoi":false,"port":47810,"chemin":"/mcp","adresses":["192.168.1.20","127.0.0.1"],"ecoute":true}}
```

- `jeton` : seulement si `ouvert`. `adresses` : les IPv4 du serveur (sans
  Docker), puis `127.0.0.1` ; le client montre d'abord l'hôte par lequel il
  joint lui-même le serveur. `ecoute` : faux si le port n'a pas pu être
  ouvert (`erreur` dit pourquoi).
- Échec : les mêmes champs, plus `"erreur":"…"` (action inconnue,
  `regenerer` ou `envoi` alors que la connexion est fermée).

En ligne de commande (sur le serveur) : `jasette-serveur --mcp etat|ouvrir|fermer|regenerer|envoi-oui|envoi-non`
(le serveur en marche applique le changement dans les 2 secondes).

### 9.4 Brancher Claude

Claude Code :

```sh
claude mcp add --transport http jasette http://<hôte>:47810/mcp --header "Authorization: Bearer <jeton>"
```

claude.ai (Réglages > Connecteurs > Ajouter un connecteur personnalisé) :
claude.ai joint le serveur depuis Internet et n'envoie pas d'en-tête
choisi : il faut une URL **HTTPS publique** vers le port 47810, et l'URL
secrète `https://<nom public>/mcp/<jeton>`. Au choix (rien n'est installé
par Jasette) : `tailscale funnel 47810` (nom `*.ts.net`), Cloudflare Tunnel
(`cloudflared tunnel --url http://localhost:47810`, ou un tunnel nommé), ou
un mandataire inverse (Caddy, nginx) avec un certificat, devant
`http://127.0.0.1:47810`. Régénérer le jeton change l'URL secrète.
