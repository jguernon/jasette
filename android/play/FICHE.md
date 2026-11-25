# Fiche Google Play de Jasette

Les textes de la fiche sont rangés comme `fastlane supply` les attend : on
peut les copier-coller à la main dans la Play Console, ou les envoyer plus tard
avec `fastlane supply` (pas nécessaire).

```
android/play/metadata/android/
├── fr-CA/                       français (Canada) : langue par défaut
│   ├── title.txt                titre (30 car. max)          28
│   ├── short_description.txt    description courte (80 max)  77
│   ├── full_description.txt     description complète (4000)  ~2 800
│   ├── changelogs/1000.txt      notes de version du versionCode 1000 (500 max)
│   └── images/
│       ├── icon.png             512 × 512, PNG 24 bits sans transparence
│       ├── featureGraphic.png   1024 × 500 (bannière)
│       ├── phoneScreenshots/    À PRENDRE (voir plus bas)
│       ├── sevenInchScreenshots/   facultatif
│       └── tenInchScreenshots/     facultatif
└── en-US/                       anglais (États-Unis), même structure
```

## Coordonnées et classement (Play Console › Présence sur Google Play)

| Champ | Valeur |
|---|---|
| Nom de l'app | Jasette |
| Langue par défaut | Français (Canada) – fr-CA ; traduction : English (United States) – en-US |
| Type | Application (pas un jeu) |
| Catégorie | **Communication** |
| Étiquettes (tags) | Messagerie, Chat, SMS (choisir dans la liste proposée) |
| Courriel de contact (public, obligatoire) | **À REMPLIR** (une adresse que tu veux montrer publiquement) |
| Téléphone | (laisser vide) |
| Site Web | https://jonathanguernon.net/jasette/ |
| Politique de confidentialité | https://jonathanguernon.net/jasette/confidentialite.html |
| Prix | Gratuit (une app gratuite ne peut plus devenir payante) |
| Pays | Tous, ou au minimum Canada et États-Unis |

Les noms Google Messages, Messenger, Instagram, WhatsApp, Signal et X
n'apparaissent **ni dans le titre, ni dans l'icône, ni dans la bannière** : la
politique « Usurpation d'identité » de Play interdit tout ce qui suggère un lien
avec ces marques. Ils sont seulement nommés dans la description, avec la mention
« non affilié » à la fin (voir `RISQUES.md`).

## Visuels

- `icon.png` : l'icône de Jasette en carré plein (Play arrondit lui-même les
  coins ; ne pas mettre de coins arrondis ni d'ombre dans le fichier).
- `featureGraphic.png` : la bannière, une par langue (« Toutes tes
  messageries. Une seule app. » / « All your messengers. One app. »), dans les
  couleurs de la page (dégradé corail → rose → violet → bleu, police Unbounded).
  Sources : `.cache/play-visuels/` (hors git), rendues avec Chrome sans tête.

## Captures d'écran : à prendre par Jonathan

Je n'ai pas pu faire tourner l'app Android ici (pas de KVM, donc pas
d'émulateur, et le téléphone est hors limites). Les captures du bureau ne
conviennent pas : Play exige que les captures montrent **l'app Android
elle-même** (politique « Métadonnées » : pas de visuels trompeurs). Donc :

### Données fictives, sans toucher à tes vrais comptes

`android/play/serveur-fictif.sh` lance un serveur Jasette rempli des **mêmes
conversations inventées** que les captures du bureau (Camille Tremblay, Club de
lecture, Pizzeria Chez Gino…), sans réseau ni compte. J'ai vérifié qu'il démarre
les six faux ponts.

1. Sur le PC : `android/play/serveur-fictif.sh fr` (ou `en` pour les captures
   anglaises ; relance-le pour changer de langue).
2. Sur le téléphone, **dans un profil invité ou un 2e utilisateur Android**
   (Réglages › Système › Utilisateurs), pour ne pas toucher à ta vraie Jasette :
   installe l'APK (`adb install --user <id> …` ou le fichier), choisis
   **Client**, adresse `<IP du PC>:47801`.
3. Dans un 2e terminal : `android/play/serveur-fictif.sh --jumeler`, entre le
   code dans l'app, vérifie le code de vérification.
4. Prends les captures (bouton Marche + Volume bas). Une tablette jumelée de la
   même façon donne les captures tablette.
5. Supprime ensuite le profil invité et `.cache/serveur-fictif/`.

### Liste des captures (par langue, 4 à 8)

1. La boîte de réception, thème Sombre, Head affiché (6 services visibles).
2. Une conversation de groupe avec réactions et accusés de lecture.
3. La recherche globale (un mot trouvé dans plusieurs services).
4. Le thème Verre (une teinte pastel).
5. Le thème EGA.
6. Réglages › Serveur Jasette, ou l'écran « Comment utiliser Jasette ici ? ».
7. (facultatif) Une notification avec la photo de la personne.

Pas de capture de la page de connexion d'un service (logos et pages de tiers).

### Formats exigés par Play

- PNG ou JPEG, **sans transparence**, 320 à 3840 px de côté, et le grand côté
  ne dépasse pas **2 fois** le petit. Une capture d'un téléphone récent
  (1080 × 2400 ou 1080 × 2520) est trop allongée : il faut la recadrer.
- Pour être mis en avant, Play recommande 1080 × 1920 (9:16). Recadrage :

```sh
cd android/play/metadata/android/fr-CA/images/phoneScreenshots
for f in *.png; do magick "$f" -gravity north -crop 1080x2160+0+0 +repage -resize 1080x1920! -alpha off "$f"; done
```

  (Le recadrage `north` enlève le bas, souvent vide ; ajuste au besoin. Le
  `-resize …!` aplatit à peine de 2160 à 1920 ; sinon, `-crop 1080x1920+0+120`.)
- Tablette (facultatif mais conseillé) : `sevenInchScreenshots/` et
  `tenInchScreenshots/`, même règle de proportions, au moins 1080 px de large
  pour la mise en avant.

Nomme-les `01-boite.png`, `02-groupe.png`… : fastlane les envoie dans l'ordre
alphabétique.
