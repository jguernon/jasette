# Formulaires de la Play Console : réponses prêtes

Play Console › ton app › **Contenu de l'application** (*App content*, sous
« Surveiller et améliorer › Règles et programmes »). Chaque section ci-dessous
correspond à une carte de cette page. Les réponses visent l'app complète
(ponts embarqués) ; pour la variante `play` décrite dans `RISQUES.md`, les
différences sont notées.

---

## 1. Politique de confidentialité

URL : `https://jonathanguernon.net/jasette/confidentialite.html`

⚠️ **À corriger sur la page avant l'envoi.** Elle dit « Jasette ne collecte
aucune donnée ». Au sens courant, c'est vrai (tu ne reçois rien). Mais au sens
du formulaire Sécurité des données, transmettre un message hors de l'appareil
(vers WhatsApp, Meta, etc.) **est** une « collecte » (voir § 2). Google compare
la politique et le formulaire. Une contradiction peut causer un refus. Ajoute
ce paragraphe sous « 3. Avec qui Jasette communique » :

> **Au sens de Google Play.** Le formulaire « Sécurité des données » de Google
> Play appelle « collecte » toute donnée que l'app envoie hors de l'appareil,
> même si elle ne va qu'au service que tu as choisi. La fiche Play de Jasette
> indique donc que l'app « collecte » tes messages, photos, vidéos, messages
> vocaux, fichiers et identifiants de compte : elle les transmet, chiffrés en
> transit, aux services que tu connectes (et à ton serveur Jasette, si tu en as
> un), uniquement pour que la messagerie fonctionne. Le développeur ne les
> reçoit jamais. Aucune donnée n'est vendue ni partagée à d'autres fins. Tes
> contacts, eux, ne quittent jamais le téléphone.

> **In Google Play's terms.** Google Play's "Data safety" form calls any data
> an app sends off the device "collected", even if it only goes to the service
> you chose. Jasette's Play listing therefore states that the app "collects"
> your messages, photos, videos, voice messages, files and account identifiers:
> it sends them, encrypted in transit, to the services you connect (and to your
> Jasette server, if you have one), only so that messaging works. The developer
> never receives them. No data is sold or shared for any other purpose. Your
> contacts never leave the phone.

---

## 2. Sécurité des données (*Data safety*)

### Les définitions de Google, et ce qu'elles donnent pour Jasette

Source : [Sécurité des données, aide Play Console](https://support.google.com/googleplay/android-developer/answer/10787469).

- **Collecter**, c'est « transmettre des données de ton app hors de l'appareil
  de l'utilisateur ». La définition ne dépend pas de la destination. Une donnée
  envoyée directement à un tiers compte donc aussi, même si elle n'arrive
  **jamais** chez le développeur. Pour Jasette, un message envoyé à WhatsApp ou
  une photo téléversée vers Messenger sont des données **collectées**.
- **Traité seulement sur l'appareil** : une donnée qui ne quitte jamais
  l'appareil n'a pas à être déclarée. C'est le cas des **contacts** (Jasette les
  lit pour les photos et les noms, sans jamais les envoyer), des réglages et des
  marques (Head, Favoris, épingles, archives) en mode Autonome.
- **Chiffrement de bout en bout** : une donnée illisible pour tout
  intermédiaire, *développeur compris*, est exemptée. Elle ne couvre ici
  **que** WhatsApp, Signal et les conversations chiffrées de Messenger, et pas
  le reste (SMS/RCS via Google, Instagram, X). On ne s'en sert donc pas pour
  tout exempter : on déclare.
- **Partager**, c'est transférer à un **tiers** des données collectées. Une
  exception existe : un transfert « à la suite d'une action précise de
  l'utilisateur, quand il s'y attend raisonnablement ». Ce n'est alors pas un
  partage. Ici, l'utilisateur connecte lui-même chaque service et envoie
  lui-même chaque message. → **Aucun partage.**
- **Le serveur Jasette** appartient à l'utilisateur. Le développeur n'y a pas
  accès. Il entre dans le même cas : un transfert voulu par l'utilisateur, qui
  n'est pas un partage.

### Réponses, écran par écran

**Vue d'ensemble**

| Question | Réponse |
|---|---|
| Votre app collecte-t-elle ou partage-t-elle des types de données utilisateur requis ? | **Oui** |
| Toutes les données collectées sont-elles chiffrées en transit ? | **Oui** (TLS vers les services ; TLS 1.3 épinglé vers le serveur Jasette) |
| Quels moyens de créer un compte votre app propose-t-elle ? | **Mon app ne permet pas de créer de compte** (on se connecte aux comptes existants des services ; aucun compte Jasette) |
| Les utilisateurs peuvent-ils demander la suppression de leurs données ? | **Oui** : Réglages › Comptes › Déconnecter efface la session d'un service ; désinstaller l'app efface tout. Aucune donnée n'est gardée par le développeur. |
| URL de suppression des données (si demandée) | `https://jonathanguernon.net/jasette/confidentialite.html` (section 6, « Garder le contrôle ») |

**Types de données à cocher**, tous avec les mêmes réponses :
*Collectées : Oui · Partagées : Non · Traitées de manière éphémère : Non ·
Obligatoire (pas facultative) · Finalité : Fonctionnement de l'app*.

| Catégorie | Type | Pourquoi |
|---|---|---|
| Messages | **SMS ou MMS** | Messages SMS/RCS de Google Messages, envoyés et reçus par le serveur Jasette de l'utilisateur. (Variante `play` sans Google : garder, puisque le mode Client les relaie.) |
| Messages | **Autres messages dans l'app** | Messenger, Instagram, WhatsApp, Signal, X |
| Photos et vidéos | **Photos**, **Vidéos** | Pièces jointes envoyées par l'utilisateur |
| Fichiers audio | **Enregistrements vocaux ou sonores** | Messages vocaux |
| Fichiers et documents | **Fichiers et documents** | Pièces jointes |
| Informations personnelles | **ID utilisateur** | Identifiant et jetons de session de chaque service, transmis à ce service (et au serveur Jasette en mode Client) |

**À ne pas cocher** (et pourquoi, si un examinateur pose la question) :

- **Contacts** : lus et utilisés seulement sur l'appareil. Quand l'utilisateur
  écrit à un numéro de son carnet, ce numéro devient le destinataire d'un
  message, déjà déclaré sous « Messages ».
- Position, Santé, Finances, Navigation Web, Historique des appels, Agenda,
  Identifiants publicitaires : jamais touchés.
- Infos et performances de l'app (plantages, diagnostics) : rien n'est envoyé.
- Activité dans l'app : les marques (Head, épingles, archives) restent sur
  l'appareil, ou vont seulement au serveur **de l'utilisateur**. Si
  l'examinateur insiste, ajoute « Activité dans l'app › Autres actions »,
  avec les mêmes réponses que plus haut.
- Le **nom de l'appareil** (« Pixel de Jonathan ») est envoyé une fois au
  serveur Jasette de l'utilisateur pendant le jumelage. C'est un libellé choisi
  par l'utilisateur, pas un identifiant d'appareil au sens de Google : on ne le
  déclare pas.

**Pratiques de sécurité** : « Chiffrées en transit » : Oui. « Suppression des
données » : Oui. (Ne pas cocher « examen de sécurité indépendant », il n'y en a
pas eu.)

---

## 3. Publicités

**Non, mon app ne contient pas de publicités.**

## 4. Accès à l'app (*App access*) : comment l'examinateur peut tester

Choisis **« Tout ou partie de l'application est soumis à des restrictions »**
et donne les instructions suivantes (en anglais, les examinateurs lisent
l'anglais) :

> Jasette has no account of its own and no developer server. It is a client
> for messaging services the user already has. Without signing in to at least
> one of them, the app shows only its welcome screen and settings.
>
> **To test:** on first launch, choose **Standalone**. Open **Settings ›
> Accounts › Instagram** (or Messenger) and sign in on the service's own
> official login page. Conversations appear within a few seconds. Sending a
> message, a reaction or a photo works as in the official app.
>
> **Test account (Instagram):** user `À REMPLIR` / password `À REMPLIR`. This
> dedicated test account has no two-factor authentication, and its login is
> not tied to a phone or email you can't reach.
>
> **Contacts permission:** requested only when you tap **New message** or turn
> on **Settings › Contact photos**. Denying it keeps the app fully usable.
>
> **Foreground service:** a permanent notification ("Jasette – Connected to
> your messaging services", with a **Quit** button) appears. It keeps the connections open
> so new messages arrive while the app is closed.

**Ce que Jonathan doit fournir** : un compte de test réel, qui t'appartient,
avec un seul profil sur le service. Instagram ou Messenger est le plus simple
(l'examinateur n'a besoin que d'un mot de passe). Désactive la double
authentification sur ce compte, ou Google ne pourra pas entrer. Sans compte de
test, le risque de refus « impossible d'accéder à l'app » est réel.
⚠️ Ne crée pas de faux compte en masse : les conditions de Meta l'interdisent.
Utilise un vrai compte secondaire à ton nom.

(Variante `play` « client seulement » : l'examinateur devrait se jumeler à un
serveur. C'est difficile à offrir : le code de jumelage est unique et dure 10
minutes. C'est l'une des raisons de prévoir un **mode démo**, voir
`RISQUES.md`.)

## 5. Classification du contenu (questionnaire IARC)

- Courriel : **À REMPLIR**
- Catégorie : **« Utilitaire, productivité, communication ou autre »**
  (*Utility, Productivity, Communication, or Other*). Pas « Réseaux sociaux…
  partage de contenu généré » : Jasette n'a aucun contenu public.

Réponses (Jasette ne crée aucun contenu : il affiche des messages privés que
l'utilisateur échange avec ses contacts) :

| Question | Réponse |
|---|---|
| Violence, sang, peur | Non |
| Sexualité, nudité | Non |
| Langage grossier | Non |
| Substances contrôlées (drogues, alcool, tabac) | Non |
| Jeux d'argent simulés ou réels | Non |
| Achats numériques / intégrés | Non |
| **Les utilisateurs peuvent-ils interagir ou échanger du contenu entre eux ?** | **Oui** (messagerie entre utilisateurs, photos, vidéos) |
| L'app partage-t-elle la position de l'utilisateur avec d'autres ? | Non |
| L'app permet-elle d'acheter des biens numériques ? | Non |
| Contenu généré par les utilisateurs : y a-t-il modération ? | Non applicable : Jasette ne publie rien ; la modération et le signalement relèvent de chaque service (Jasette n'a pas de contenu public) |
| L'app est-elle un navigateur Web ou un moteur de recherche ? | Non |
| L'app est-elle principalement une app d'actualités ou d'éducation ? | Non |

Classification attendue : **Tous / 3+ (PEGI 3, ESRB Everyone)**, avec les
éléments interactifs **« Les utilisateurs interagissent »** et **« Partage
d'informations »**. Une messagerie peut ressortir **Teen/12+** selon le pays.
C'est normal, rien à corriger.

## 6. Public cible et contenu

| Question | Réponse |
|---|---|
| Tranches d'âge visées | **18 ans et plus** seulement (cocher aussi 16-17 est possible, mais ça ajoute des exigences ; les services reliés exigent 13+ et WhatsApp 13/16+ selon les pays) |
| L'app pourrait-elle attirer les enfants involontairement ? | **Non** (aucun personnage, aucun jeu ; outil de messagerie) |
| Fiche : « Conçu pour les familles » | Non |

## 7. Applications d'actualités

**Non, mon app n'est pas une application d'actualités.**

## 8. Applications gouvernementales

**Non** (l'app n'est ni développée ni publiée pour le compte d'un gouvernement).

## 9. Fonctionnalités financières

**Mon app n'offre aucune fonctionnalité financière** (cocher la case « aucune »).

## 10. Applications de santé

**Non / aucune fonctionnalité de santé.**

## 11. Traçage des contacts / statut COVID-19

**Non.**

## 12. Déclaration des permissions sensibles

### READ_CONTACTS (Contacts)

Les contacts ne sont **pas** une permission « à haut risque » de Play (contrairement
à SMS, Journal d'appels, Accessibilité, Tous les fichiers). Il n'y a pas de
formulaire de déclaration dédié. Le contrôle passe par le formulaire Sécurité des
données (§ 2) et par la politique. Si un examinateur demande une justification
(Policy › « Permissions and APIs that Access Sensitive Information ») :

> Jasette reads the device's contacts (names, phone numbers and photos) only on
> the device, to show each person's photo next to their conversations and to
> let the user pick a recipient in "New message". The permission is requested
> only when the user opens one of these two features, never at launch, and the
> app works fully if it is denied. Contacts are never uploaded or sent anywhere.

### POST_NOTIFICATIONS

Permission d'exécution ordinaire, sans déclaration. Elle sert à annoncer les
nouveaux messages. Jasette la demande au premier lancement, et on peut la
refuser.

### INTERNET, ACCESS_NETWORK_STATE

Permissions normales, sans déclaration.

### `ca.jasette.DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION`

Ajoutée par AndroidX. Elle est interne à l'app et ne demande aucune déclaration.

## 13. Services au premier plan (*Foreground service permissions*)

La Console exige une déclaration pour `FOREGROUND_SERVICE_REMOTE_MESSAGING`
(type `remoteMessaging` de `PontService`) :
[aide Play](https://support.google.com/googleplay/android-developer/answer/13392821).

- **Type** : Remote messaging.
- **Cas d'usage** (liste) : « Relay text communication to another device ».
- **Description** :

> Jasette is a client for the user's messaging accounts. It runs as a linked
> device or web client of Google Messages, WhatsApp, Signal, Messenger,
> Instagram and X, or as a client of the user's own Jasette server, which
> relays the messages from those services. The foreground service keeps these
> connections open so incoming text messages are synced to this device and
> outgoing messages are sent, the same way a messaging web client stays
> connected. It starts when the user opens the app with at least one account
> connected. A permanent notification shows it, and the notification has a
> "Quit" button that stops it.

- **Impact si le système diffère ou interrompt la tâche** :

> If the service is deferred or stopped, the user stops receiving new messages
> and notifications until they reopen the app, and messages being sent may
> fail.

- **Vidéo** (obligatoire, lien YouTube non répertorié ou Drive public) :
  20 à 40 s, filmée avec le **serveur fictif** (voir `FICHE.md`). Montre :
  l'ouverture de Jasette ; la connexion (jumelage en mode Client) ; la
  notification permanente ; puis, app fermée, un message qui arrive avec sa
  notification et sa synchronisation. Pour qu'un message « arrive » pendant le
  tournage, envoie-le depuis Jasette pour bureau, branchée au même serveur
  fictif.

Si Play refuse `remoteMessaging` pour ce cas d'usage, le repli est le type
`specialUse`, avec une justification semblable. Il demande
`FOREGROUND_SERVICE_SPECIAL_USE`, une `<property
android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE">` dans le
manifeste et un nouvel examen. Ne le fais qu'après un refus.
