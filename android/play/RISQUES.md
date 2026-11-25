# Risques de refus sur Google Play, et recommandation

État des lieux au 25 novembre 2026. Je ne suis pas avocat. Ce qui suit lit les
politiques publiques de Google Play et les conditions des services. Personne ne
peut prédire avec certitude une décision d'examen.

## Ce que fait Jasette, vu par Google

L'app Android complète (l'AAB construit, `ca.jasette` 0.1.0, versionCode 1000) :

- embarque cinq exécutables Go (`libjasette-pont/meta/wa/signal/x.so`), lancés
  depuis `nativeLibraryDir` ;
- se connecte comme **client web ou « appareil lié » non officiel** à Messenger,
  Instagram, WhatsApp, Signal et X (bibliothèques mautrix : la même base que
  Beeper), et à Google Messages par le serveur Jasette de l'utilisateur ;
- récupère les cookies de session après une connexion dans une WebView ;
- tourne en service au premier plan `remoteMessaging` ;
- montre dans l'app les logos des services (`res/drawable-nodpi/service_*.png`).

## Les politiques en jeu

### 1. Usage abusif de l'appareil et du réseau : le risque n° 1

[Device and Network Abuse](https://support.google.com/googleplay/android-developer/answer/16559646) :

> « We don't allow apps that interfere with, disrupt, damage, or access in an
> unauthorized manner […] application programming interfaces (APIs), or
> services »
> Exemple de violation : « Apps that access or use a service or API in a
> manner that violates its terms of service. »

Or les conditions des services visés interdisent ce que fait Jasette, plus ou
moins clairement :

| Service | Clause | Risque |
|---|---|---|
| **WhatsApp** | [Conditions](https://www.whatsapp.com/legal/terms-of-service) : interdit d'accéder au service « through automated or other means » non autorisés et d'offrir un logiciel qui « function[s] substantially the same as our Services » de façon non autorisée. Meta bannit régulièrement les comptes qui passent par des clients modifiés. | **Élevé**, pour l'app et pour les utilisateurs (bannissement du numéro) |
| **Messenger / Instagram** | Conditions de Meta : pas d'accès automatisé sans permission. Meta dépose des plaintes de propriété intellectuelle auprès des magasins d'apps. | **Élevé** |
| **X** | Les conditions d'X interdisent d'accéder au service autrement que par ses interfaces publiées. X est connu pour ses poursuites. | **Moyen à élevé** |
| **Signal** | [Conditions](https://signal.org/legal/) : « You must not […] access, use, modify […] our Services in unauthorized manners ». Signal s'est déjà opposé publiquement aux clients tiers qui utilisent ses serveurs (LibreSignal, 2016). | **Moyen** |
| **Google Messages** | Protocole non officiel de « Messages pour le Web ». Ici, c'est **Google lui-même** qui examine l'app. Sur Android, il passe déjà par le serveur Jasette. | **Moyen** |

En pratique, l'examen de Play vérifie surtout la fiche, les permissions, les
déclarations et le comportement visible. Il analyse rarement les protocoles.
Le danger vient surtout **après** la publication : une plainte de Meta ou d'X
mène au retrait de l'app et à un **avertissement** (*strike*) sur le compte.
Plusieurs avertissements peuvent faire **fermer le compte de développeur**, avec
tous les comptes associés et toutes les apps, y compris tes futurs jeux.

**Précédent favorable** : Beeper (Automattic) est sur Google Play, catégorie
Communication, avec des connexions « sur l'appareil » vers WhatsApp, Signal,
Messenger, Instagram, X et Google Messages, sur la même base mautrix
([FAQ Beeper](https://beeper.com/faq), [annonce Android](https://blog.beeper.com/2024/04/09/beeper-is-now-available/)).
Le concept passe donc l'examen. Mais Beeper est une entreprise qui a des
juristes, et rien ne dit qu'elle n'a pas d'entente. Beeper Mini (iMessage),
elle, a quitté le Play Store
([Android Central](https://androidcentral.com/apps-software/beeper-mini-removed-from-the-play-store)).

### 2. Usurpation d'identité et propriété intellectuelle

[Impersonation](https://support.google.com/googleplay/android-developer/answer/9888374),
[Intellectual Property](https://support.google.com/googleplay/android-developer/answer/9888072),
[Metadata](https://support.google.com/googleplay/android-developer/answer/9898842) :

- Interdit : suggérer un lien avec une marque (« Don't imply that your app is
  related to or authorized by someone that it isn't »), copier un logo, mettre
  des noms de tiers dans le titre ou l'icône.
- **Ce qui est déjà fait** : titre, icône et bannière sans aucune marque de
  tiers ; la description nomme les services comme compatibilité, avec une
  mention « non affilié » explicite.
- **À surveiller** : les **logos des services dans l'app** (pastilles de
  service, écrans de connexion) et donc **dans les captures**. L'usage
  nominatif (« ceci est ta conversation WhatsApp ») est défendable, mais c'est
  ce qui déclenche une plainte de marque. Mesure simple : dans les captures,
  préfère les vues où les pastilles sont petites. Pour la variante `play`,
  envisage des pastilles neutres (initiale et couleur).

### 3. Code exécutable embarqué ou téléchargé : faible

La même politique interdit de **télécharger** du code exécutable (« an app may
not download executable code (such as dex, JAR, .so files) from a source other
than Google Play »). Les ponts de Jasette sont **dans l'AAB**, signés et
examinés avec lui, et Jasette ne télécharge aucun code. Les lancer depuis
`nativeLibraryDir` est exactement ce qu'Android permet (W^X). Termux et
d'autres font pareil. **Risque faible**, à condition de ne jamais ajouter de
mise à jour des ponts hors de Play.

### 4. Service au premier plan `remoteMessaging` : moyen

Déclaration obligatoire, avec une vidéo ([aide](https://support.google.com/googleplay/android-developer/answer/13392821)).
Le cas d'usage officiel est « relayer des messages texte vers un autre
appareil ». Jasette est l'**autre appareil**, le client web ou appareil lié :
c'est défendable, et c'est encore plus net en mode Client (le serveur relaie).
Les réponses sont prêtes dans `FORMULAIRES.md` § 13, avec le repli
`specialUse`. Un premier refus ici est courant et se corrige.

### 5. Accès pour l'examen : moyen

Sans compte de test qui fonctionne, Play refuse avec « impossible d'accéder à
l'app ». La parade : un compte Instagram ou Messenger secondaire, sans double
authentification (`FORMULAIRES.md` § 4), ou mieux, un **mode démo** (voir plus
bas).

### 6. Cohérence de la confidentialité : faible, mais à corriger

La page dit « aucune collecte », mais le formulaire Sécurité des données doit
déclarer une « collecte » au sens de Google. Le paragraphe à ajouter est prêt
dans `FORMULAIRES.md` § 1.

### 7. Exigences techniques : en règle

- `targetSdk 36` : c'est le minimum exigé depuis le 31 août 2026 pour les
  nouvelles apps et les mises à jour
  ([exigences d'API cible](https://developer.android.com/google/play/requirements/target-sdk)).
- AAB signé avec la clé d'importation. `bundletool validate` passe.
- Pages mémoire de 16 Ko : les `.so` Go sont alignés sur 0x4000 (vérifié avec
  `readelf`), et BundleConfig dit `PAGE_ALIGNMENT_16K`.
- `extractNativeLibs="true"` dans le manifeste fusionné, et `.so` compressés
  dans les APK que Play génère (vérifié avec `bundletool build-apks`, appareil
  arm64 et APK universel). Android les extrait donc dans `nativeLibraryDir` à
  l'installation depuis Play, comme pour l'APK du site.
- Aucune permission « à haut risque » (SMS, journal d'appels, accessibilité,
  tous les fichiers). Les contacts n'ont pas de formulaire dédié.

---

## Recommandation honnête

### Option A : publier l'app complète

- **Pour** : c'est l'app réelle ; Beeper montre que le concept passe l'examen ;
  rien à développer.
- **Contre** : l'app viole elle-même les conditions de WhatsApp, Meta et X,
  donc l'exemple écrit de la politique « Device and Network Abuse ». Il suffit
  d'une plainte pour un retrait et un avertissement, sur un **compte personnel**
  que tu voudras peut-être utiliser pour tes jeux. Si tu choisis A, retire au
  moins **WhatsApp** de la version Play : c'est le service le plus agressif et
  celui qui expose le plus les utilisateurs (bannissement).

### Option B, recommandée : une variante `play`, « client du serveur Jasette » avec mode démo

L'app Play ne parle qu'**au serveur Jasette de l'utilisateur**, une machine qui
lui appartient. Les ponts tournent sur ce serveur, hors de l'app.

- La clause « accède à un service en violation de ses conditions » ne vise plus
  l'app elle-même. Le risque principal tombe de « élevé » à « faible » ou
  « moyen » (la fiche décrit toujours à quoi sert le serveur).
- Plus d'exécutables embarqués : l'AAB passe d'environ 57 Mo à environ 25 Mo.
- Le service au premier plan colle mot pour mot au cas `remoteMessaging`.
- L'app complète (Autonome) reste sur ton site et sur GitHub (APK signé, et
  Obtainium ou F-Droid plus tard).
- **Inconvénient franc** : sur Play, seuls ceux qui installent un serveur
  pourront s'en servir. La fiche Play devient surtout une vitrine et une porte
  d'entrée vers la version complète.

Avec B, **commence par la piste de test fermée** (obligatoire de toute façon,
voir `GUIDE.md`) : l'examen se fait déjà à ce moment-là, sans exposition
publique.

### La variante `play`, en détail (non implémentée : elle touche `Depot.kt` et l'interface)

1. **`app/build.gradle.kts`**

   ```kotlin
   flavorDimensions += "distribution"
   productFlavors {
       create("complet") { dimension = "distribution" }   // site, GitHub
       create("play") {
           dimension = "distribution"
           buildConfigField("boolean", "PONTS", "false")
       }
   }
   // jniLibs pour « complet » seulement (au lieu de main) :
   sourceSets["complet"].jniLibs.directories.add(rootProject.file("../.cache/android-jni").path)
   ```

   Il faut aussi `buildConfigField("boolean", "PONTS", "true")` pour
   `complet`, `buildFeatures { buildConfig = true }`, et brancher la tâche
   `ponts` sur `preCompleteBuild` plutôt que sur `preBuild`.
   `compiler.sh --aab` construirait `bundlePlayRelease`, et `--release`
   construirait `assembleCompletRelease`.

2. **`Depot.kt`** : aujourd'hui, Google et Meta se lancent sans vérifier
   `embarque()`. Il faut faire passer **tous** les services par `embarque()`.
   Si `BuildConfig.PONTS` est faux, le mode Autonome n'existe pas : le
   démarrage va directement au jumelage Client.
3. **Interface (`ui/`, à faire quand l'autre agent aura fini)** : l'écran
   « Comment utiliser Jasette ici ? » cache « Autonome » et ajoute
   « Essayer la démo ».
4. **Mode démo** (environ une journée) : un `PontDemo` qui rejoue en Kotlin les
   conversations inventées de `.cache/captures/faux-pont.py` (mêmes
   événements JSON, avatars en `assets/`). Il sert trois fois : l'examinateur
   teste sans compte ni serveur, les captures Play deviennent faciles, et un
   curieux peut essayer l'app avant d'installer un serveur.
5. **Fiche** : enlever la puce « Autonome », mettre « Client » en tête, et
   ajouter « Il faut un serveur Jasette (gratuit, Linux) : voir le site » au
   début de la description.
6. **Identifiant** : garde `ca.jasette` pour Play. L'APK complet du site, signé
   avec la même clé (si tu choisis « ta propre clé » dans Play App Signing, voir
   `GUIDE.md`), peut alors s'installer par-dessus. Mais Play remplacera ensuite
   la version complète par la version Play à chaque mise à jour automatique.
   Pour l'éviter, donne au flavor `complet` un `applicationIdSuffix = ".complet"`
   (deux apps distinctes). C'est le choix le plus clair si les deux versions
   coexistent.

### En bref

- **Le plus sûr** : B, piste fermée d'abord.
- **Le plus utile, mais risqué** : A sans WhatsApp.
- **À éviter** : A avec les six services sur ton seul compte personnel.
