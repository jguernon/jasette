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
`bluesky`. Les cinq derniers sont **facultatifs** : le serveur ne les lance que si leur
binaire est présent (`jasette-wa`, `jasette-signal`, `jasette-x`,
`jasette-telegram`, `jasette-bluesky`, voir §5.4) ; sinon
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

`svc` : `google`, `messenger`, `instagram`, `whatsapp`, `signal`, `x`, `telegram` ou `bluesky`.
`cmd` : exactement la commande qu'on écrirait sur l'entrée standard du pont
(voir l'en-tête de `pont/main.go`, `pont-meta/main.go`, `pont-wa/main.go`,
`pont-signal/main.go`, `pont-x/main.go`, `pont-telegram/main.go` et
`pont-bluesky/main.go`). Particularités :

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
`state`, `account`, `avatar_moi`, `api` (Telegram : application réglée ou
non), `qr` (seulement en cours d'association),
`pair_code` (WhatsApp, seulement en cours d'association par code),
`login_step` (Telegram, seulement en attente du code ou du mot de passe),
`history_pin` (seulement s'il attend un NIP : `{"needed":true}`), puis les
dernières listes `conversations` gardées (boîte de réception, puis archives :
§5.8), puis la présence en ligne (§5.10). Le `qr` et
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

### 5.4 Connexion des services WhatsApp, Signal, X, Telegram et Bluesky

Ces cinq ponts parlent le même protocole que les autres (mêmes commandes
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

### 5.5 Marques (Head, favoris, épinglées, archives, privées, moods, emojis rapides, garder_archives, cacher_apres_jours, presence_en_ligne)

Les marques de l'utilisateur sont gardées par le serveur et partagées entre
tous ses appareils. Ce sont cinq listes **ordonnées** d'ids de
conversations, tels que l'interface les voit (« m: » pour Messenger,
« i: » pour Instagram, « w: » pour WhatsApp, « s: » pour Signal, « x: »
pour X, « t: » pour Telegram, « b: » pour Bluesky, sans préfixe pour Google) :

- `head` : la grille en tête de la boîte de réception ;
- `favoris` : la vue de l'étoile (`etoiles` dans les réglages du bureau) ;
- `epingles` : les conversations épinglées en haut de la liste ;
- `archives` : l'**archivage propre à Jasette**, pour les services qui ne
  savent pas archiver (Messenger, Instagram, Signal, X, Bluesky). Google, WhatsApp et
  Telegram archivent pour de vrai (commande `archive` de leur pont) et n'y
  vont pas ;
- `privees` : la **Liste privée** (voir « Règle des privées » plus bas).

S'y ajoutent deux objets (facultatifs) :

- `moods` : le **Mood** de chaque conversation, `{id: nom}` (voir « Règle
  des moods » plus bas) ;
- `emojis` : l'**emoji rapide** de chaque conversation, `{id: "👍"}` (voir
  « Règle des emojis rapides » plus bas).

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

C'est une trame à part, comme `ping` : elle ne passe pas par les ponts.

**Serveur → client** (rejeu, juste après `{"auth":"ok"}`, puis à chaque
changement, à **tous** les clients) :

```json
{"marques":{"head":["i:42"],"favoris":["123","m:99"],"epingles":[],"archives":["m:77","s:+15145551234"],"privees":["w:5551"],"moods":{"m:99":"amour"},"emojis":{"m:99":"👍"},"garder_archives":false,"cacher_apres_jours":0,"presence_en_ligne":true,"version":7}}
```

`version` : incrémentée à chaque changement accepté. **0** = le serveur n'a
encore rien reçu (listes vides).

**Client → serveur** :

```json
{"marques":{"head":[…],"favoris":[…],"epingles":[…],"archives":[…],"privees":[…],"moods":{…},"emojis":{…},"garder_archives":false,"cacher_apres_jours":0,"presence_en_ligne":true,"version":7,"id":12}}
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
     `emojis`.
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
`cacher_apres_jours` et `presence_en_ligne`.

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
n'a pas le dernier message annoncé par la liste (`time`). Ces réponses (et
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
message (WhatsApp, Signal, Telegram, Messenger et Instagram ; Google Messages,
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

Les ponts Messenger, Instagram, WhatsApp et Telegram annoncent qui est en
ligne, pour les conversations **individuelles** qu'ils connaissent :

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
  Conséquences : les contacts vous voient « en ligne » tant que le pont
  tourne, et le téléphone ne sonne plus pour les messages WhatsApp tant
  qu'un appareil lié est disponible (comportement de WhatsApp Web).
  Désactivé : « indisponible », comme avant (disponible seulement pendant
  3 minutes après une action dans Jasette).
- **Messenger** (messagix) : la table lightspeed
  `LSDeleteThenInsertContactPresence` (Status ≠ 0 : actif, jusqu'à
  `ExpirationTimestampMs`) et `LSTruncatePresenceDatabase`, quand Meta les
  pousse sur le socket.
- **Instagram** (instameow, qui n'implémente pas le flux de présence du
  site) : `GET /api/v1/direct_v2/get_presence/` une fois par minute ;
  trois échecs de suite, pause de 30 minutes.
- **Telegram** : le `status` de chaque personne (`userStatusOnline`,
  `userStatusOffline` avec `was_online`, sinon heure inconnue) et
  `updateUserStatus`. Le pont ne se déclare jamais en ligne lui-même.

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

Le serveur le relaie à tous les clients, sans le garder ni le rejouer. Les
clients le montrent (bulle reçue aux trois points qui rebondissent en bas du
fil, « écrit… » dans la liste et sous le nom dans l'en-tête) si la marque
`presence_en_ligne` est vraie : « Afficher qui est en ligne et qui écrit ».
Jasette n'envoie jamais son propre « en train d'écrire » (la commande
`typing` des ponts Signal et Telegram existe, l'interface ne s'en sert pas).

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
`<données>/signal/`, `<données>/x/`, `<données>/telegram/`, `<données>/bluesky/` (bases SQLite et session : jamais
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
| S→C | `{"marques":{"head","favoris","epingles","archives","privees","moods","emojis","garder_archives","cacher_apres_jours","presence_en_ligne","version"[,"id","refus"]}}` | rejeu, changement |
| C→S | `{"marques":{"head","favoris","epingles","archives","privees","moods","emojis","garder_archives","cacher_apres_jours","presence_en_ligne","version"[,"id"]}}` | changement local |
| S→C | `{"svc":…,"ev":{"ev":"presence","item":{"conv","user","online","last_active"}}}` | présence (§5.10), aussi au rejeu |
| S→C | `{"svc":…,"ev":{"ev":"typing","conv","on"[,"name","audio"]}}` | « en train d'écrire » (§5.11) |
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
