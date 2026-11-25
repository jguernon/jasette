package ca.jasette.android

import android.content.Context
import android.content.res.Resources
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * La langue de l'interface, comme `t()` du bureau (src/main.rs) : les textes
 * sont écrits en français dans le code, et [t] rend leur traduction anglaise
 * quand l'interface est en anglais.
 *
 * Par défaut (« auto »), la langue du téléphone : français si sa locale
 * commence par « fr », sinon anglais. Le choix est un état Compose : le
 * changer redessine aussitôt tout ce qui est à l'écran.
 */
object Langue {
    private const val PREFS = "reglages"
    private const val CLE = "langue"
    private var prefs: android.content.SharedPreferences? = null

    /** « auto », « fr » ou « en ». */
    var choix by mutableStateOf("auto")
        private set

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
        choix = p.getString(CLE, "auto")?.takeIf { it in listOf("auto", "fr", "en") } ?: "auto"
    }

    fun choisir(valeur: String) {
        choix = valeur
        prefs?.edit()?.putString(CLE, valeur)?.apply()
    }

    /** Le téléphone est-il en français ? (sa locale, pas celle de l'app) */
    fun telephoneFrancais(): Boolean {
        val l = Resources.getSystem().configuration.locales
        return l.size() > 0 && l[0].language.startsWith("fr")
    }

    val anglais: Boolean get() = when (choix) {
        "fr" -> false
        "en" -> true
        else -> !telephoneFrancais()
    }
}

/** Le texte [fr] dans la langue de l'interface. */
fun t(fr: String): String = if (!Langue.anglais) fr else ANGLAIS[fr] ?: fr

private val ANGLAIS: Map<String, String> = mapOf(
    // Réglages → Serveur Jasette → Connexion MCP (Claude).
    "Connexion MCP (Claude)" to "MCP connection (Claude)",
    "Branchez Claude (claude.ai ou Claude Code) sur les messageries de ce serveur : il pourra lister vos conversations, les lire et y chercher. Lire ne marque rien comme lu." to
        "Connect Claude (claude.ai or Claude Code) to this server's messaging: it can list your conversations, read them and search them. Reading marks nothing as read.",
    "Fermée par défaut. Un jeton secret protège l'accès ; Fermer le révoque." to "Closed by default. A secret token protects access; Close revokes it.",
    "Ouvrir ma connexion MCP" to "Open my MCP connection",
    "Adresse du point MCP :" to "MCP endpoint address:",
    "Jeton (secret, comme un mot de passe) :" to "Token (secret, like a password):",
    "Le serveur n'a pas pu ouvrir le port MCP :" to "The server could not open the MCP port:",
    "Régénérer le jeton" to "Regenerate token",
    "Copié" to "Copied",
    "Sur une machine qui joint le serveur, dans un terminal :" to "On a machine that can reach the server, in a terminal:",
    "claude.ai joint le serveur depuis Internet : il lui faut une adresse HTTPS publique qui mène au port MCP. Au choix (à installer vous-même) : Tailscale Funnel (tailscale funnel PORT), Cloudflare Tunnel (cloudflared), ou un mandataire inverse (Caddy, nginx) avec un certificat." to
        "claude.ai reaches the server from the Internet: it needs a public HTTPS address leading to the MCP port. Your choice (to install yourself): Tailscale Funnel (tailscale funnel PORT), Cloudflare Tunnel (cloudflared), or a reverse proxy (Caddy, nginx) with a certificate.",
    "claude.ai n'envoie pas d'en-tête choisi : donnez-lui l'URL secrète ci-dessous (Réglages > Connecteurs > Ajouter un connecteur personnalisé), en remplaçant le nom public. Elle contient le jeton : gardez-la pour vous." to
        "claude.ai doesn't send custom headers: give it the secret URL below (Settings > Connectors > Add custom connector), replacing the public name. It contains the token: keep it to yourself.",
    "VOTRE-NOM-PUBLIC" to "YOUR-PUBLIC-NAME",
    "Permettre à Claude d'envoyer des messages" to "Allow Claude to send messages",
    "Désactivé par défaut. Seulement dans une conversation existante, au plus 5 envois par minute et 30 par heure." to
        "Off by default. Only in an existing conversation, at most 5 sends per minute and 30 per hour.",
    "Pas de connexion au serveur Jasette." to "No connection to the Jasette server.",
    "Le serveur Jasette ne répond pas. S'il est d'une version sans connexion MCP, mettez-le à jour." to
        "The Jasette server isn't answering. If its version has no MCP connection, update it.",
    // Réglages → Apparence : les thèmes et les teintes du verre (comme au bureau).
    "Verre" to "Glass",
    "EGA" to "EGA",
    "Aplats gris et fil noir, bulles plates." to "Flat grays, black thread, flat bubbles.",
    "Verre clair sur halos pastel." to "Light glass over pastel halos.",
    "Seize couleurs et police IBM, comme un PC de 1984." to "Sixteen colors and the IBM font, like a 1984 PC.",
    "Teinte du verre" to "Glass tint",
    "Choisissez le thème Verre pour changer de teinte." to "Pick the Glass theme to change the tint.",
    "Lavande" to "Lavender",
    "Rose" to "Pink",
    "Pêche" to "Peach",
    "Citron" to "Lemon",
    "Menthe" to "Mint",
    "Ciel" to "Sky",
    "Lilas" to "Lilac",
    "Corail" to "Coral",
    "Contact" to "Contact",
    " : " to ": ",
    "🎤 Message vocal" to "🎤 Voice message",
    "📎 Pièce jointe" to "📎 Attachment",
    "📷 Photo" to "📷 Photo",
    "Adresse du serveur" to "Server address",
    "adresse invalide" to "invalid address",
    "adresse vide" to "empty address",
    "affiche-t-il ce code de vérification ?" to "show this verification code?",
    "Appel dans" to "Call in",
    "Appeler" to "Call",
    "Appel vidéo" to "Video call",
    "Appel vidéo dans" to "Video call in",
    "Aucun compte connecté." to "No account connected.",
    "Aucune app de téléphone sur cet appareil" to "No phone app on this device",
    "Aucun navigateur pour ouvrir" to "No browser to open",
    "Aucun service n'est connecté sur ton serveur Jasette. Connecte-les sur le serveur." to "No service is connected on your Jasette server. Connect them on the server.",
    "Aujourd'hui" to "Today",
    "Automatique" to "Automatic",
    "Autonome" to "Standalone",
    "Avec Facebook" to "With Facebook",
    "Avec Messenger" to "With Messenger",
    "Bienvenue dans Jasette" to "Welcome to Jasette",
    "Blanche" to "White",
    "Celle du téléphone" to "The phone's language",
    "ce n'est pas un serveur Jasette" to "this is not a Jasette server",
    "Cet appareil" to "This device",
    "Ce téléphone se connecte lui-même à tes services (Messenger, Instagram)." to "This phone connects to your services itself (Messenger, Instagram).",
    "Changer de mode" to "Change mode",
    "Chargement des conversations…" to "Loading conversations…",
    "Chiffré" to "Encrypted",
    "Client" to "Client",
    "Code à 6 chiffres" to "6-digit code",
    "Comment utiliser Jasette ici ?" to "How do you want to use Jasette here?",
    "Comptes" to "Accounts",
    "Connecté à tes messageries" to "Connected to your messaging services",
    "Connecté à ton serveur Jasette" to "Connected to your Jasette server",
    "Connecte un premier service :" to "Connect a first service:",
    "Connexion à Facebook" to "Sign in to Facebook",
    "Connexion à Google" to "Sign in to Google",
    "Connexion à Instagram" to "Sign in to Instagram",
    "Connexion à Messenger" to "Sign in to Messenger",
    "connexion fermée" to "connection closed",
    "connexion perdue" to "connection lost",
    "Continuer" to "Continue",
    "Déconnecter" to "Sign out",
    "Distribué" to "Delivered",
    "Échec" to "Failed",
    "Entre l'adresse du serveur et le code à 6 chiffres qu'il affiche." to "Enter the server's address and the 6-digit code it shows.",
    "Entre le NIP de 6 chiffres choisi dans Messenger pour restaurer tes anciennes conversations chiffrées." to "Enter the 6-digit PIN you chose in Messenger to restore your older encrypted chats.",
    "Envoi…" to "Sending…",
    "Envoi impossible pour l'instant" to "Can't send right now",
    "Envoyé" to "Sent",
    "Envoyer" to "Send",
    "Fermer" to "Close",
    "Garde la connexion à Google Messages, Messenger et Instagram (ou à ton serveur Jasette)." to "Keeps the connection to Google Messages, Messenger and Instagram (or to your Jasette server).",
    "Google Meet refuse l'appel vidéo direct" to "Google Meet refuses the direct video call",
    "Hier" to "Yesterday",
    "Historique chiffré de Messenger" to "Messenger encrypted history",
    "Icône de l'app" to "App icon",
    "Image indisponible" to "Image unavailable",
    "introuvable" to "not found",
    "Jasette en marche" to "Jasette running",
    "Jasette se connecte elle-même à chaque service." to "Jasette connects to each service itself.",
    "Jumelage annulé : les codes ne correspondaient pas." to "Pairing cancelled: the codes did not match.",
    "Langue" to "Language",
    "L'appel vidéo passe par l'app Google Meet, qui n'est pas installée." to "Video calls go through the Google Meet app, which is not installed.",
    "le certificat du serveur a changé : connexion refusée. S'il a été réinstallé, jumelle de nouveau." to "the server's certificate has changed: connection refused. If it was reinstalled, pair again.",
    "le code de jumelage a 6 chiffres" to "the pairing code has 6 digits",
    "Le lanceur peut prendre quelques secondes à montrer la nouvelle icône." to "The launcher may take a few seconds to show the new icon.",
    "Le pont ne démarre pas" to "The bridge does not start",
    "Le serveur" to "Does the server",
    "le serveur n'a pas confirmé le dépôt" to "the server did not confirm the upload",
    "le serveur n'a pas prouvé qu'il connaissait le code" to "the server did not prove it knew the code",
    "le serveur n'offre pas" to "the server does not offer",
    "Les services se connectent et se déconnectent sur le serveur." to "Services are connected and disconnected on the server.",
    "Lu" to "Read",
    "Message" to "Message",
    "Messages plus anciens" to "Older messages",
    "Messenger : entre ton NIP pour restaurer l'historique chiffré" to "Messenger: enter your PIN to restore the encrypted history",
    "Ne plus l'utiliser" to "Stop using it",
    "NIP" to "PIN",
    "nom ou IP (port 47800 par défaut)" to "name or IP (port 47800 by default)",
    "Non" to "No",
    "Nouveaux messages" to "New messages",
    "Oui, jumeler" to "Yes, pair",
    "pas de certificat" to "no certificate",
    "Pas maintenant" to "Not now",
    "port invalide" to "invalid port",
    "Pour changer de serveur, choisis « Sur cet appareil », puis jumelle de nouveau." to "To change server, choose “On this device”, then pair again.",
    "Quitter" to "Quit",
    "Quitter Jasette (arrête les ponts)" to "Quit Jasette (stops the bridges)",
    "Quitter Jasette (se débranche du serveur)" to "Quit Jasette (disconnects from the server)",
    "Recommandé" to "Recommended",
    "Réglages" to "Settings",
    "réponse inattendue du serveur" to "unexpected answer from the server",
    "Restaurer" to "Restore",
    "Retour" to "Back",
    "Se brancher à ton serveur Jasette, qui garde les sessions. Sur Android, Google Messages ne peut se connecter que de cette façon." to "Connect to your Jasette server, which keeps the sessions. On Android, this is the only way Google Messages can connect.",
    "Se connecter" to "Sign in",
    "serveur injoignable" to "server unreachable",
    "Serveur Jasette" to "Jasette server",
    "Sombre" to "Dark",
    "Sur cet appareil" to "On this device",
    "Sur le serveur : jasette-serveur --jumeler (ou, au bureau, Réglages > Serveur Jasette > Jumeler un appareil). Entre ici son adresse et le code affiché." to "On the server: jasette-serveur --jumeler (or, on the desktop, Settings > Jasette server > Pair a device). Enter its address and the code shown here.",
    "Touche cet emoji dans Google Messages sur ton téléphone" to "Tap this emoji in Google Messages on your phone",
    "Tous les services passent par ton serveur : rien à connecter ici. Google Messages fonctionne ainsi." to "All services go through your server: nothing to connect here. Google Messages works this way.",
    "Un instant…" to "One moment…",
    "Utiliser mon serveur Jasette" to "Use my Jasette server",
    "Votre serveur" to "Your server",
    "Vous : " to "You: ",
    "Démarrage" to "Starting",
    "Non connecté" to "Not connected",
    "Association" to "Pairing",
    "Connexion…" to "Connecting…",
    "Connecté" to "Connected",
    "Téléphone hors ligne" to "Phone offline",
    "Reconnexion…" to "Reconnecting…",
    "Pont arrêté" to "Bridge stopped",
    "appareil inconnu ou révoqué : jumelez-le de nouveau" to "unknown or revoked device: pair it again",
    "code de jumelage invalide ou expiré" to "invalid or expired pairing code",
    "trop d'essais, réessayez dans quelques minutes" to "too many attempts, try again in a few minutes",
    "trop d'essais" to "too many attempts",
    "impossible d'enregistrer l'appareil" to "could not register the device",
    // Rail, vues, menu d'une conversation, nouveau message, recherche, apparence.
    "Affichage en deux volets sur grand écran" to "Two-pane layout on large screens",
    "Afficher l'heure de chaque message" to "Show the time of every message",
    "Ajouter au Head" to "Add to Head",
    "Ajouter aux favoris" to "Add to favorites",
    "Annuler" to "Cancel",
    "Apparence" to "Appearance",
    "Archiver" to "Archive",
    "Archivée par le service : désarchive-la dans son app." to "Archived by the service: unarchive it in its app.",
    "Archives" to "Archive",
    "Aucun favori : ajoutez-en par un appui long sur une conversation." to "No favorites yet: long-press a conversation to add some.",
    "Aucun média dans les messages chargés." to "No media in the loaded messages.",
    "Aucun résultat" to "No results",
    "Personnes" to "People",
    "Votre avatar" to "Your avatar",
    "Avatar de Google Messages" to "Google Messages avatar",
    "Votre fiche sur le téléphone" to "Your contact card on the phone",
    "Aucune photo trouvée" to "No photo found",
    "Avatar de Facebook" to "Facebook avatar",
    "Quand Messenger sera connecté" to "Once Messenger is connected",
    "Avatar d'Instagram" to "Instagram avatar",
    "Quand Instagram sera connecté" to "Once Instagram is connected",
    "Choisir une image…" to "Choose an image…",
    "Une image de votre téléphone" to "An image from your phone",
    "Initiale" to "Initial",
    "Sans photo" to "No photo",
    "Moi" to "Me",
    "Grosseur du texte des messages" to "Message text size",
    "Liste des conversations plus grosse" to "Larger conversation list",
    "Noms, aperçus, heures et titres des Head un peu plus gros." to "Slightly larger names, previews, times and Head titles.",
    "Texte des messages : %d %%" to "Message text: %d %%",
    "On se voit au chalet vendredi ?" to "See you at the cabin Friday?",
    "Oui ! J'apporte les guimauves." to "Yes! I'm bringing marshmallows.",
    "Message à %s sur %s" to "Message %s on %s",
    "ce contact" to "this contact",
    "Ne plus utiliser ce serveur ?" to "Stop using this server?",
    "Cet appareil se débranchera du serveur Jasette. Tes services restent connectés sur le serveur ; pour revenir, il faudra jumeler de nouveau avec un nouveau code." to "This device will disconnect from the Jasette server. Your services stay connected on the server; to come back, you will need to pair again with a new code.",
    "Aucun service connecté." to "No service connected.",
    "Aucune conversation" to "No conversations",
    "Aucune conversation archivée" to "No archived conversations",
    "Boîte de réception" to "Inbox",
    "Chaque bulle montre son heure, et sa date si ce n'est pas aujourd'hui." to "Each bubble shows its time, and its date if it isn't today.",
    "Chargement des archives…" to "Loading archive…",
    "Choisissez une conversation" to "Pick a conversation",
    "Descendre" to "Move down",
    "Désactivé" to "Off",
    "Désarchiver" to "Unarchive",
    "Désépingler" to "Unpin",
    "Effacer" to "Clear",
    "Elle sera supprimée de Google Messages, sur ton téléphone aussi. C'est définitif." to "It will be deleted from Google Messages, on your phone too. This cannot be undone.",
    "Favoris" to "Favorites",
    "Inbox" to "Inbox",
    "La liste et la conversation côte à côte quand l'écran est large (téléphone déplié, tablette)." to "The list and the conversation side by side on a wide screen (unfolded phone, tablet).",
    "La liste, puis la conversation en plein écran, comme sur un téléphone plié." to "The list, then the conversation full screen, as on a folded phone.",
    "Monter" to "Move up",
    "Médias" to "Media",
    "Nom ou numéro de téléphone" to "Name or phone number",
    "Nouveau message" to "New message",
    "Nouveau message par Google Messages" to "New message via Google Messages",
    "Nouvelle conversation Instagram" to "New Instagram chat",
    "Nouvelle conversation Messenger" to "New Messenger chat",
    "Nouvelle conversation WhatsApp" to "New WhatsApp chat",
    "Nouvelle conversation Signal" to "New Signal chat",
    "Nouvelle conversation Telegram" to "New Telegram chat",
    "Nouvelle conversation Bluesky" to "New Bluesky chat",
    // Bluesky (Bluesky.kt).
    "Connecter Bluesky" to "Connect Bluesky",
    "Jasette lit et envoie tes messages privés Bluesky. Il lui faut un mot de passe d'app qui a accès aux messages : dans Bluesky, Réglages → Confidentialité et sécurité → Mots de passe d'app → Ajouter, en cochant « Autoriser l'accès à vos messages privés ». Ton vrai mot de passe n'est jamais demandé." to
        "Jasette reads and sends your Bluesky direct messages. It needs an app password with access to messages: in Bluesky, Settings → Privacy and security → App passwords → Add, checking “Allow access to your direct messages”. Your real password is never asked for.",
    "Ouvrir les mots de passe d'app" to "Open app passwords",
    "Nom d'utilisateur ou courriel" to "Handle or email",
    "Mot de passe d'app" to "App password",
    // Telegram (Telegram.kt).
    "Connecter Telegram" to "Connect Telegram",
    "Application Telegram" to "Telegram app",
    "Telegram demande à chaque application son propre identifiant. Crée le tien, gratuitement : connecte-toi sur my.telegram.org avec ton numéro, ouvre « API development tools », remplis le formulaire (nom et nom court au choix), puis copie ici l'api_id et l'api_hash. Ils restent sur ce téléphone." to
        "Telegram requires each app to have its own identifier. Create yours for free: sign in at my.telegram.org with your number, open “API development tools”, fill in the form (any name and short name), then paste the api_id and api_hash here. They stay on this phone.",
    "Ouvrir my.telegram.org" to "Open my.telegram.org",
    "Application réglée. Pour la changer, entre un autre api_id et api_hash." to "App set. To change it, enter another api_id and api_hash.",
    "Enregistrer" to "Save",
    "Entre ton numéro Telegram, avec l'indicatif du pays : Telegram t'enverra un code." to
        "Enter your Telegram number, with the country code: Telegram will send you a code.",
    "Recevoir un code" to "Get a code",
    "Se connecter plutôt avec un code QR" to "Sign in with a QR code instead",
    "Se connecter plutôt avec mon numéro" to "Sign in with my number instead",
    "Entre le code que Telegram vient d'envoyer dans l'app Telegram (sur un autre appareil connecté), ou par SMS." to
        "Enter the code Telegram just sent in the Telegram app (on another signed-in device), or by SMS.",
    "Code" to "Code",
    "Ce compte est protégé par un mot de passe (vérification en deux étapes)." to
        "This account is protected by a password (two-step verification).",
    "Indice :" to "Hint:",
    "Mot de passe" to "Password",
    "Recommencer" to "Start over",
    "Scanne-le avec un autre appareil qui a Telegram : Paramètres → Appareils → Connecter un appareil." to
        "Scan it with another device that has Telegram: Settings → Devices → Link Desktop Device.",
    "Nouvelle conversation X" to "New X chat",
    "Connecte-le depuis Jasette sur ton ordinateur" to "Connect it from Jasette on your computer",
    "Supprimer : pas encore pour ce service." to "Delete: not yet for this service.",
    "Elle sera supprimée de Messenger, sur tous tes appareils. C'est définitif." to "It will be deleted from Messenger, on all your devices. This cannot be undone.",
    "Elle sera supprimée d'Instagram, sur tous tes appareils. C'est définitif." to "It will be deleted from Instagram, on all your devices. This cannot be undone.",
    "Plus" to "More",
    "Rechercher" to "Search",
    "Rechercher dans la conversation" to "Search in conversation",
    "Recherche…" to "Searching…",
    "Retirer des favoris" to "Remove from favorites",
    "Retirer du Head" to "Remove from Head",
    "Réorganiser" to "Reorder",
    "Résultat précédent" to "Previous result",
    "Résultat suivant" to "Next result",
    "Sans nom" to "Unnamed",
    "Services" to "Services",
    "Supprimer" to "Delete",
    "Supprimer la conversation" to "Delete conversation",
    "Supprimer la conversation ?" to "Delete conversation?",
    "Terminé" to "Done",
    "À :" to "To:",
    "Épingler en haut" to "Pin to top",
    "Épinglée" to "Pinned",
    // Pièces jointes, réactions, coches.
    "Appareil photo" to "Camera",
    "Copier le texte" to "Copy text",
    "Copier le lien" to "Copy link",
    "Aperçus des liens" to "Link previews",
    "Jasette va chercher l'image et le titre des liens (par le serveur Jasette s'il y en a un). Les aperçus envoyés par le service restent affichés." to
        "Jasette fetches the image and title of links (through the Jasette server if there is one). Previews sent by the service are always shown.",
    "Fichier" to "File",
    "Joindre" to "Attach",
    "Mo au plus" to "MB max",
    "Photos et vidéos" to "Photos and videos",
    "Plus de réactions" to "More reactions",
    "Quelqu'un" to "Someone",
    "Retirer" to "Remove",
    "Réactions" to "Reactions",
    "Vidéo" to "Video",
    "Vous" to "You",
    "est trop gros pour" to "is too large for",
    "lecture impossible" to "cannot be read",
    "Échec de l'envoi · Réessayer" to "Failed to send · Retry",
    // Réglages → Gestes : les glissements sur les conversations de la liste.
    "Gestes" to "Gestures",
    "Glisser vers la droite" to "Swipe right",
    "Glisser vers la gauche" to "Swipe left",
    "Répondre" to "Reply",
    "Rien" to "Nothing",
    "Conversation archivée" to "Conversation archived",
    "Conversation désarchivée" to "Conversation unarchived",
    // Contacts du téléphone : permission, Réglages → Apparence.
    "Accès aux contacts" to "Contacts access",
    "Jasette peut montrer les photos de tes contacts et les proposer dans « Nouveau message ». Tes contacts restent sur ce téléphone : ils ne sont jamais envoyés au serveur ni aux services." to
        "Jasette can show your contacts' photos and suggest them in “New message”. Your contacts stay on this phone: they are never sent to the server or to the services.",
    "Plus tard" to "Not now",
    "Paramètres" to "Settings",
    "Accès aux contacts refusé" to "Contacts access denied",
    "Utiliser les photos de mes contacts" to "Use my contacts' photos",
    "La photo d'un contact du téléphone remplace celle de Google Messages, par le numéro." to
        "A phone contact's photo replaces the Google Messages one, matched by number.",
    "Aussi pour Messenger et Instagram (par le nom)" to "Also for Messenger and Instagram (by name)",
    "Seulement quand un seul contact porte exactement le nom de la conversation." to
        "Only when exactly one contact has the conversation's exact name.",
    // Réglages → Apparence → Grille du Head.
    "Grille du Head" to "Head grid",
    "Le nombre d'avatars par rangée ; il y a autant de rangées qu'il faut pour tout le Head." to
        "Avatars per row; there are as many rows as the whole Head needs.",
    "%d de large" to "%d wide",
    // WhatsApp, Signal et X sur cet appareil (Association.kt, Connexion.kt).
    "Avec un numéro de téléphone" to "With a phone number",
    "Code QR" to "QR code",
    "Lier avec un code QR" to "Link with a QR code",
    "Lier Signal" to "Link Signal",
    "Associer WhatsApp" to "Link WhatsApp",
    "Associer plutôt avec un numéro de téléphone" to "Link with a phone number instead",
    "Afficher plutôt un code QR (pour un autre appareil)" to "Show a QR code instead (for another device)",
    "Scanne-le avec un autre téléphone qui a Signal (Paramètres → Appareils liés). Les anciens messages ne sont pas récupérés." to
        "Scan it with another phone that has Signal (Settings → Linked devices). Older messages are not retrieved.",
    "Scanne-le avec le téléphone qui a WhatsApp : Appareils connectés → Connecter un appareil." to
        "Scan it with the phone that has WhatsApp: Linked devices → Link a device.",
    "Le code se renouvelle tout seul." to "The code renews itself.",
    "Associer avec un numéro de téléphone" to "Link with phone number",
    "Entre le numéro du compte WhatsApp, avec l'indicatif du pays. WhatsApp te donnera un code à taper sur ce téléphone-là." to
        "Enter the WhatsApp account's number, with the country code. WhatsApp will give you a code to type on that phone.",
    "Numéro de téléphone" to "Phone number",
    "Obtenir le code" to "Get the code",
    "Réessayer" to "Try again",
    "Copier" to "Copy",
    "Dans WhatsApp : Appareils connectés → Connecter un appareil → Associer avec un numéro de téléphone" to
        "In WhatsApp: Linked devices → Link a device → Link with phone number instead",
    "Puis tape ce code. Le code expire après quelques minutes." to "Then type this code. It expires after a few minutes.",
    "Changer de numéro" to "Change number",
    "Connexion à X" to "Sign in to X",
    "Code d'accès X Chat" to "X Chat passcode",
    "X chiffre tes messages privés. Pour que Jasette puisse les lire, entre le code d'accès (4 chiffres) choisi dans X Chat. Jasette ne le garde pas." to
        "X encrypts your direct messages. For Jasette to read them, enter the 4-digit passcode you chose in X Chat. Jasette does not keep it.",
    "X verrouille X Chat après quelques codes faux." to "X locks X Chat after a few wrong codes.",
    "Code d'accès" to "Passcode",
    "Pas encore de code ? Active d'abord X Chat dans l'application X ou sur x.com/messages." to
        "No passcode yet? First turn on X Chat in the X app or at x.com/messages.",
    // Head, favoris et épingles d'un service absent (CacheMarques, ui/Absentes.kt).
    "chargement…" to "loading…",
    "déconnecté" to "disconnected",
    "se connecte…" to "is connecting…",
    "n'est pas connecté" to "is not connected",
    "Les messages de cette conversation s'afficheront dès que le service sera connecté." to
        "This conversation's messages will appear as soon as the service is connected.",
    "Réglages → Comptes" to "Settings → Accounts",
    // La Liste privée (Prive.kt, ui/Prive.kt).
    "Liste privée" to "Private list",
    "Privées" to "Private",
    "Rendre privée" to "Make private",
    "Retirer des privées" to "Remove from private",
    "Verrouiller" to "Lock",
    "Conversations privées" to "Private conversations",
    "Entre ton code" to "Enter your code",
    "Code incorrect" to "Wrong code",
    "Trop d'essais : attends" to "Too many tries: wait",
    "La Liste privée n'est pas activée." to "The private list is not turned on.",
    "Changer le code" to "Change the code",
    "Choisis un code à 6 chiffres" to "Choose a 6-digit code",
    "Entre le code une 2e fois" to "Enter the code again",
    "Les deux codes diffèrent : recommence." to "The codes don't match: try again.",
    "Entre le code actuel" to "Enter the current code",
    "Désactiver la liste" to "Turn off the list",
    "Il reste des conversations privées :" to "Private conversations left:",
    "Qu'en fait-on ?" to "What should happen to them?",
    "Les remettre dans l'app (conseillé)" to "Put them back in the app (recommended)",
    "Les garder cachées" to "Keep them hidden",
    "Cachées, elles ne se verront plus avant de réactiver la Liste privée." to
        "Hidden, they won't show again until the private list is turned back on.",
    "Confirmer : elles redeviennent normales" to "Confirm: they become normal again",
    "conversation(s) privée(s)" to "private conversation(s)",
    "Aucune conversation privée : ajoutez-en par le menu d'une conversation." to
        "No private conversations: add some from a conversation's menu.",
    "Des conversations cachées partout dans Jasette, qu'on rouvre par l'icône « Jasette privé » et un code à 6 chiffres. Le code reste sur cet appareil." to
        "Conversations hidden everywhere in Jasette, reopened through the “Jasette privé” icon and a 6-digit code. The code stays on this device.",
    "conversation(s) rendue(s) privée(s) sur un autre appareil reste(nt) cachée(s) ici. Active la Liste privée pour les voir." to
        "conversation(s) made private on another device stay hidden here. Turn on the private list to see them.",
    "« Rendre privée » apparaît dans le menu d'une conversation. Une conversation privée ne donne aucune notification. La vue se reverrouille en arrière-plan, ou après 1 minute sans toucher l'écran. La liste des privées est partagée par le serveur Jasette ; le code, jamais." to
        "“Make private” appears in a conversation's menu. A private conversation never notifies. The view locks again in the background, or after 1 minute without touching the screen. The private list is shared by the Jasette server; the code never is.",
    // Le menu de partage d'Android (PartageActivity, ui/Partage.kt).
    "Partager dans Jasette" to "Share in Jasette",
    "Ajouter un message (facultatif)" to "Add a message (optional)",
    "Rechercher une conversation" to "Search a conversation",
    "Conversations récentes" to "Recent conversations",
    "Résultats" to "Results",
    "Aucune conversation pour l'instant. Ouvre Jasette pour connecter tes messageries." to
        "No conversations yet. Open Jasette to connect your messaging apps.",
    "Bluesky ne permet pas d'envoyer des fichiers en message privé. Choisis une autre conversation." to
        "Bluesky doesn't allow sending files in private messages. Choose another conversation.",
    "%s ne prend pas les fichiers" to "%s doesn't accept files",
    "Choisie" to "Selected",
    "Choisis une conversation" to "Choose a conversation",
    "Envoyer à %s" to "Send to %s",
    "Envoyer à %d conversations" to "Send to %d conversations",
    "Rien à partager : écris un message." to "Nothing to share: write a message.",
    "Envoi en cours…" to "Sending…",
    "Échec de l'envoi" to "Sending failed",
    "Préparation…" to "Preparing…",
    "Connexion à %s…" to "Connecting to %s…",
    "%s n'est pas connecté pour l'instant." to "%s isn't connected right now.",
    "Tu peux fermer : l'envoi continue en arrière-plan." to "You can close this: sending continues in the background.",
)
