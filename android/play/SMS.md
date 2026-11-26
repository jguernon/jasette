# SMS du téléphone et Google Play

Notes pour plus tard (publication sur Google Play en pause depuis le 25 novembre 2026).

## Ce qui a été ajouté

En mode autonome, Réglages → Google Messages offre « Utiliser les SMS du
téléphone » (`PontSms.kt`) : Jasette lit et envoie les SMS et MMS de
l'appareil, sans compte Google. Le manifeste demande donc :

- `READ_SMS`, `SEND_SMS`, `RECEIVE_SMS`, `RECEIVE_MMS` (groupe SMS) ;
- `READ_CONTACTS` (déjà là, pour les noms) ;
- `android.hardware.telephony` est déclaré facultatif.

## Le problème avec Google Play

La politique « Utilisation des autorisations SMS ou Journal d'appels » de Play
réserve ces permissions aux apps **choisies comme gestionnaire SMS par
défaut** (ou à quelques exceptions listées : sauvegarde, sécurité, etc.).
Jasette ne se déclare pas app SMS par défaut (il lui faudrait un
`SmsReceiver` `SMS_DELIVER`, un `MmsReceiver` `WAP_PUSH_DELIVER`, un service
`RESPOND_VIA_MESSAGE` et une activité `SENDTO`, et gérer elle-même toute la
messagerie SMS/MMS du téléphone). Un AAB qui contient ces permissions serait
donc refusé à l'examen, sauf déclaration acceptée dans la Play Console
(formulaire « Autorisations sensibles »), peu probable ici.

Ce n'est pas bloquant aujourd'hui : l'APK est distribué par le site
(jonathanguernon.net/jasette).

## Options le jour où Play reprend

1. **Une variante Play sans SMS** (le plus simple) : une saveur Gradle `play`
   dont le manifeste retire les quatre permissions SMS
   (`tools:node="remove"`), et `Depot.smsActif` toujours faux (le bouton
   n'apparaît pas). L'APK du site garde la fonction.
2. **Devenir app SMS par défaut** : beaucoup de travail (réception, MMS,
   notifications de l'opérateur, réponses rapides), et la déclaration Play.
3. Retirer la fonction de toutes les versions.

Recommandation : l'option 1. Ajouter alors une ligne dans `RISQUES.md` et le
formulaire de déclaration des permissions (`FORMULAIRES.md`).
