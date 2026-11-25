# Publier Jasette sur Google Play : le guide pas à pas

Pour Jonathan. Tout ce qui est à copier-coller est prêt dans ce dossier :

| Fichier | Contenu |
|---|---|
| `FICHE.md` + `metadata/android/<langue>/` | titre, descriptions, notes de version, icône, bannière, consignes des captures |
| `FORMULAIRES.md` | réponses de tous les formulaires de « Contenu de l'application » |
| `RISQUES.md` | les politiques en jeu et la recommandation (lis-le avant l'étape 3) |
| `serveur-fictif.sh` | serveur aux conversations inventées, pour les captures et la vidéo |

L'AAB : `.cache/paquets/jasette-0.1.0.aab` (hors git ; refais-le avec
`android/compiler.sh --aab`).

---

## Étape 0 : avant de commencer (10 min)

1. Lis `RISQUES.md` et décide : **app complète** (option A) ou **variante
   `play`** (option B, recommandée). L'AAB actuel est l'app complète. Avec B,
   il faut d'abord programmer le flavor `play` (décrit dans `RISQUES.md`).
2. Prépare :
   - une **pièce d'identité** officielle (permis ou passeport) et une preuve
     d'adresse si on te la demande ;
   - une **carte de crédit** (25 $ US, une seule fois) ;
   - un **courriel de contact public** pour la fiche (une adresse que tu veux
     bien rendre publique) ;
   - un **téléphone Android** pour la vérification de l'appareil (l'app Play
     Console) ;
   - pour l'option A : un **compte de test** Instagram ou Messenger, sans
     double authentification (`FORMULAIRES.md` § 4).

## Étape 1 : créer le compte de développeur (30 min, puis quelques jours d'attente)

1. Va sur <https://play.google.com/console/signup> avec le compte Google qui
   possédera l'app. Utilise un compte que tu gardes à vie, pas un compte jetable.
2. Type de compte : **« Pour moi-même »** (compte personnel). Un compte
   d'organisation demande un numéro D-U-N-S, que tu n'as pas besoin d'obtenir
   pour Jasette.
3. Remplis le profil du développeur. Le **nom du développeur** sera public :
   « Jonathan Guernon ». Le site : `https://jonathanguernon.net`.
4. Paie les **25 $ US**.
5. **Vérification d'identité** : envoie la pièce d'identité. Le nom légal doit
   correspondre à celui du profil de paiement. Compte de quelques heures à
   quelques jours.
6. **Vérification de l'appareil** : installe l'app **Google Play Console** sur
   ton téléphone et connecte-toi avec le même compte. La Console le demande.
7. Vérifie aussi ton **numéro de téléphone** et ton **courriel** de contact
   (codes envoyés par la Console).

> **Pour les nouveaux comptes personnels**, la règle est toujours en vigueur
> en novembre 2026 ([aide Play](https://support.google.com/googleplay/android-developer/answer/14151465)) :
> avant la production, il faut un **test fermé avec au moins 12 testeurs
> inscrits sans interruption pendant au moins 14 jours**. Un testeur qui se
> désinscrit puis revient recommence son compte de 14 jours. Recrute tes 12 à
> 15 testeurs dès maintenant (famille, amis, lecteurs, abonnés), chacun avec un
> compte Google et un téléphone Android **arm64** (presque tous les téléphones
> depuis 2017).

## Étape 2 : créer l'app (10 min)

Console › **Créer une application** :

- Nom : **Jasette**
- Langue par défaut : **Français (Canada) – fr-CA**
- Application ou jeu : **Application**
- Gratuite ou payante : **Gratuite**
- Coche les deux déclarations (règles du programme, lois d'exportation des
  États-Unis).

## Étape 3 : Play App Signing (5 min)

Console › Tester et publier › **Intégrité de l'appli › Signature d'application**.

Pour une nouvelle app, Play active la signature par Play **automatiquement**,
avec une clé que **Google** génère. En 2026, c'est une signature hybride
« quantique » ([aide](https://support.google.com/googleplay/android-developer/answer/9842756)).
Ta clé `signing/jasette-release.jks` (alias `jasette`) devient alors ta
**clé d'importation** : elle signe ce que tu envoies, puis Google re-signe
pour les téléphones. Rien d'autre à faire. Le premier AAB que tu téléverses
enregistre cette clé d'importation.

- **Recommandé : laisser la clé générée par Google.** Conséquence : les
  téléphones qui ont l'APK du site (signé avec ta clé) ne peuvent pas passer à
  la version Play sans désinstaller. Avec l'option B, c'est voulu de toute
  façon (deux variantes distinctes).
- Variante : bouton **« Changer de clé »**, puis « fournir une copie de ta clé
  de signature », **avant** toute sortie en test ouvert ou en production. Play
  signerait alors avec `jasette-release.jks`, et l'APK du site et la version
  Play seraient interchangeables. Suis les instructions affichées (outil PEPK,
  qui chiffre la clé pour Google). Google recommande alors de **générer une
  autre clé d'importation** (« your upload key and app signing key should be
  different »).

**Sauvegarde** `signing/jasette-release.jks` et
`signing/jasette-release.properties` hors de cet ordinateur (clé USB chiffrée +
gestionnaire de mots de passe). Si tu perds la clé d'importation, Google peut la
réinitialiser (Console › Signature › demander une réinitialisation). Si tu
choisis ta propre clé de signature et que tu la perds, tu ne peux plus mettre
l'app à jour.

## Étape 4 : remplir « Contenu de l'application » (45 min)

Console › Surveiller et améliorer › **Règles et programmes › Contenu de
l'application**. Remplis chaque carte avec `FORMULAIRES.md` :

1. Politique de confidentialité (§ 1). **Ajoute d'abord** le paragraphe
   « Au sens de Google Play » à `confidentialite.html` et publie le site.
2. Accès à l'app (§ 4)
3. Annonces : non (§ 3)
4. Classification du contenu, questionnaire IARC (§ 5)
5. Public cible : 18 ans et plus (§ 6)
6. Applications d'actualités : non (§ 7)
7. Sécurité des données (§ 2)
8. Applications gouvernementales : non (§ 8)
9. Fonctionnalités financières : aucune (§ 9)
10. Santé : non (§ 10)
11. Autorisations de service au premier plan (§ 13) : prévois d'abord la
    **vidéo** (YouTube non répertoriée), filmée avec `serveur-fictif.sh`.

## Étape 5 : la fiche du Play Store (30 min)

Console › Développer votre audience › **Présence sur le Play Store › Fiche
principale** :

1. Copie `metadata/android/fr-CA/title.txt`, `short_description.txt` et
   `full_description.txt`.
2. Téléverse `images/icon.png` (512 × 512) et `images/featureGraphic.png`
   (1024 × 500).
3. Captures téléphone : prends-les avec le serveur fictif (`FICHE.md`), au
   moins 2 et idéalement 4 à 8. Tablette 7 po et 10 po : facultatives.
4. **Gérer les traductions › Ajouter › English (United States)** : même chose
   avec `metadata/android/en-US/`.
5. **Paramètres de la fiche** (Configuration du Store) : catégorie
   **Communication**, courriel de contact, site
   `https://jonathanguernon.net/jasette/` (tableau dans `FICHE.md`).

## Étape 6 : test interne, ton propre téléphone (15 min)

Console › Tester et publier › Tests › **Tests internes** :

1. **Testeurs** : crée une liste avec ton adresse Gmail. Copie le **lien
   d'inscription**.
2. **Créer une version** → téléverse `.cache/paquets/jasette-0.1.0.aab`.
   Nom de la version : `0.1.0`. Notes : colle `changelogs/1000.txt` (fr-CA et
   en-US).
3. Enregistrer → Examiner → **Lancer le déploiement sur les tests internes**.
4. Sur ton téléphone, ouvre le lien d'inscription, accepte, puis installe
   Jasette **depuis le Play Store**. Désinstalle d'abord l'APK du site : les
   signatures diffèrent si tu gardes la clé de Google.
5. **Vérifie que les ponts démarrent** (Réglages › Comptes : connecter
   Messenger). C'est le test réel de l'extraction des `.so` installés depuis
   Play. Je l'ai vérifié avec `bundletool`, mais pas sur un vrai téléphone.

Pour les versions suivantes : augmente `version` dans `Cargo.toml` (le
versionCode suit tout seul : 0.1.1 → 1001, 0.2.0 → 2000), puis
`android/compiler.sh --aab`, et ajoute `changelogs/<versionCode>.txt`.

## Étape 7 : test fermé, 12 testeurs pendant 14 jours

Console › Tests › **Tests fermés** › piste « Alpha » (ou crée-en une) :

1. Testeurs : une **liste de courriels** (12 minimum, vise 15 pour avoir de la
   marge) ou un **Google Groupe** (plus simple : ils s'y inscrivent eux-mêmes).
2. Pays : Canada (+ États-Unis, France…).
3. Créer une version → **réutilise l'AAB** de la piste interne (« Ajouter
   depuis la bibliothèque ») → **Envoyer pour examen**. C'est le **premier vrai
   examen de Google** : compte de quelques jours, parfois plus pour un nouveau
   compte.
4. Une fois l'examen réussi, envoie le **lien d'inscription** aux testeurs.
   Chacun doit : ouvrir le lien avec son compte Google, cliquer « Devenir
   testeur », installer depuis Play, et **ne pas se désinscrire** pendant
   14 jours. Demande-leur de vraiment s'en servir : Google regarde l'activité
   pendant le test et pose des questions à ce sujet à l'étape suivante.
5. Corrige ce qu'ils remontent ; tu peux publier de nouvelles versions pendant
   le test sans remettre le compteur à zéro.

## Étape 8 : demander l'accès à la production

Après 14 jours avec au moins 12 testeurs, la Console (Tableau de bord) active
**« Demander l'accès à la production »**. Il y a un questionnaire, à remplir
honnêtement :

- Comment tu as recruté les testeurs : famille, amis, lecteurs.
- Ce qu'ils ont testé : boîte unifiée, envoi et réception, recherche, thèmes,
  notifications.
- Ce que tu as corrigé grâce à eux : note-le pendant le test.
- Pourquoi l'app est prête, et qui elle vise : les gens qui jonglent avec
  plusieurs messageries et qui tiennent à leur vie privée.

Réponse en environ 7 jours.

## Étape 9 : production

Console › **Production** › Créer une version → AAB de la bibliothèque → pays
→ **déploiement progressif** (commence à 20 %) → Envoyer pour examen. Une fois
l'examen réussi, Jasette est publique.

Ensuite :
- remplace « Bientôt sur Google Play » sur la page par le badge officiel
  « Disponible sur Google Play » (règles d'usage du badge :
  <https://play.google.com/intl/fr/badges/>) ;
- surveille **Statistiques Android › Plantages et ANR** ;
- réponds aux avis.

## Si Google refuse

Le courriel cite la politique en cause. Les causes les plus probables, et la
parade de chacune :

| Refus | Que faire |
|---|---|
| Service au premier plan (vidéo ou cas d'usage) | Refaire la vidéo plus claire ; sinon le repli `specialUse` (`FORMULAIRES.md` § 13) |
| Accès à l'app impossible | Vérifier le compte de test (2FA désactivée) ; ou le mode démo |
| Sécurité des données incohérente | Aligner la politique (§ 1) et le formulaire |
| Usage abusif du réseau / conditions de tiers | Passer à l'option B (`RISQUES.md`) ; ne pas renvoyer la même version |
| Usurpation d'identité / marque | Retirer le logo ou le nom en cause des captures ou de la fiche |

Tu peux faire **appel** une fois par décision (Console › Boîte de réception
des règles). Ne crée **jamais** un deuxième compte pour contourner un refus :
c'est une cause de fermeture définitive de tous tes comptes.

## Hors Play, à savoir

Google déploie en 2026-2027 la **vérification des développeurs Android** :
même les APK installés hors Play devront venir d'un développeur vérifié, sur
les appareils certifiés. Le compte Play Console que tu crées ici sert aussi à
enregistrer `ca.jasette` pour l'APK de ton site. Après l'inscription, regarde
la section « Vérification des développeurs Android » de la Console.
