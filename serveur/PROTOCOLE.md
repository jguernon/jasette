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

`services` : ceux que ce serveur fait tourner (`--services`). Après `ok`,
la connexion passe au régime du §5, sans limite de durée.

Révocation : `jasette-serveur --appareils` liste les appareils,
`--revoquer <nom>` en retire un ; s'il est branché, il est débranché dans les
3 secondes, et ses connexions suivantes reçoivent `{"erreur":"jeton"}`.

## 5. Multiplexage

### 5.1 Commandes (client → serveur)

```json
{"svc":"google","cmd":{"cmd":"messages","conv":"123","count":30}}
```

`svc` : `google`, `messenger` ou `instagram`. `cmd` : exactement la commande
qu'on écrirait sur l'entrée standard du pont (voir l'en-tête de
`pont/main.go` et `pont-meta/main.go`). Particularités :

- `{"cmd":"quit"}` est **ignoré** : le pont appartient au serveur.
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
`state`, `account`, `avatar_moi`, `qr` (seulement en cours d'association),
`history_pin` (seulement s'il attend un NIP : `{"needed":true}`). Un client
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

### 5.5 Marques (Head, favoris, épinglées)

Les marques de l'utilisateur sont gardées par le serveur et partagées entre
tous ses appareils. Ce sont trois listes **ordonnées** d'ids de
conversations, tels que l'interface les voit (« m: » pour Messenger,
« i: » pour Instagram, sans préfixe pour Google) :

- `head` : la grille en tête de la boîte de réception ;
- `favoris` : la vue de l'étoile (`etoiles` dans les réglages du bureau) ;
- `epingles` : les conversations épinglées en haut de la liste.

C'est une trame à part, comme `ping` : elle ne passe pas par les ponts.

**Serveur → client** (rejeu, juste après `{"auth":"ok"}`, puis à chaque
changement, à **tous** les clients) :

```json
{"marques":{"head":["i:42"],"favoris":["123","m:99"],"epingles":[],"version":7}}
```

`version` : incrémentée à chaque changement accepté. **0** = le serveur n'a
encore rien reçu (listes vides).

**Client → serveur** :

```json
{"marques":{"head":[…],"favoris":[…],"epingles":[…],"version":7,"id":12}}
```

- `version` : la dernière version reçue du serveur (0 si aucune).
- `id` (facultatif, entier) : renvoyé tel quel dans la réponse **à ce client
  seulement**, pour qu'il reconnaisse la réponse à sa propre trame.
- Une liste absente ne change pas. Ids vides ou en double retirés ; au plus
  5000 ids par liste, 512 octets par id.

Règles du serveur :

1. `version` = la sienne : les listes sont prises telles quelles (une liste
   vide l'est exprès : c'est ainsi qu'un client à jour vide une liste).
2. `version` ≠ la sienne (client en retard) :
   - **garde-fou** : si une liste reçue est vide alors que celle du serveur
     ne l'est pas, la trame est **refusée** ; le client reçoit les marques
     du serveur, avec `"refus":"<texte>"` (et son `id`) ;
   - sinon, union : les listes du client dans leur ordre, puis les ids du
     serveur qu'elles n'ont pas.
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
donne l'état du serveur et la règle ci-dessus s'applique.

### 5.6 Reconnexion (côté client)

Une seule connexion par serveur, partagée par tous les services. Si elle
tombe : montrer `state: reconnecting` pour chaque service branché, puis
réessayer avec une attente progressive (1, 2, 4… 30 s ; 60 s après un refus
de jeton ou d'empreinte). Si le serveur n'a jamais répondu depuis le
lancement : `state: connecting`. Les commandes envoyées pendant la coupure
peuvent être gardées et envoyées après l'`auth` suivante.

## 6. Fichiers

Les événements contiennent des chemins de fichiers **du serveur** :
`avatar.path`, `avatar_moi.path`, `avatars_groupe.members[].path`,
`media.path`… Le client les télécharge dans un cache local et réécrit les
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
`<données>/serveur/` (0700) contient `cert.pem`, `cle.pem`,
`appareils.json` (noms, SHA-256 des jetons, dates), `jumelage.json` (code en
cours), `marques.json` (§5.5 ; écrit par fichier temporaire puis renommage,
la version précédente dans `marques.json.bak` ; un fichier illisible est mis
de côté, jamais écrasé) et `depots/`, tous en 0600. Les journaux (sortie d'erreur) ne
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
| C→S | `{"rejouer":"<svc>"}` | redemander l'état d'un service |
| ↔ | `{"ping":n}` / `{"pong":n}` | signes de vie |
| S→C | `{"marques":{"head","favoris","epingles","version"[,"id","refus"]}}` | rejeu, changement |
| C→S | `{"marques":{"head","favoris","epingles","version"[,"id"]}}` | changement local |
| C→S | `{"get":{"id","chemin","sha256"}}` | télécharger |
| S→C | `{"fichier":{"id",…}}` | morceaux ou erreur |
| C→S | `{"put":{"id","nom","donnees","fin"}}` | téléverser |
| S→C | `{"depose":{"id","chemin"|"erreur"}}` | fin du dépôt |
