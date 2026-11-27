// Jasette iOS — la langue de l'interface, comme `t()` du bureau et d'Android :
// les textes sont écrits en français dans le code, et [t] rend leur
// traduction anglaise quand l'interface est en anglais. Par défaut
// (« auto »), la langue du téléphone.

import Foundation
import Observation

@Observable
final class Langue {
    static let partagee = Langue()
    private static let cle = "langue"

    /// « auto », « fr » ou « en ».
    var choix: String {
        didSet { UserDefaults.standard.set(choix, forKey: Langue.cle) }
    }

    private init() {
        let c = UserDefaults.standard.string(forKey: Langue.cle) ?? "auto"
        choix = ["auto", "fr", "en"].contains(c) ? c : "auto"
    }

    static var telephoneFrancais: Bool {
        (Locale.preferredLanguages.first ?? "").hasPrefix("fr")
    }

    var anglais: Bool {
        switch choix {
        case "fr": return false
        case "en": return true
        default: return !Langue.telephoneFrancais
        }
    }
}

/// Le texte [fr] dans la langue de l'interface.
func t(_ fr: String) -> String {
    Langue.partagee.anglais ? (ANGLAIS[fr] ?? fr) : fr
}

// Les textes repris d'Android (Langue.kt) quand ils existent.
private let ANGLAIS: [String: String] = [
    // Jumelage et connexion (Serveur.kt).
    "Serveur Jasette": "Jasette server",
    "Utiliser mon serveur Jasette": "Use my Jasette server",
    "Adresse du serveur": "Server address",
    "Code de jumelage": "Pairing code",
    "Nom de cet appareil": "Name of this device",
    "Sur un appareil déjà jumelé : Réglages > Serveur Jasette > Jumeler un appareil (ou, sur le serveur : jasette-serveur --jumeler). Entre ici son adresse et le code affiché.":
        "On a device that's already paired: Settings > Jasette server > Pair a device (or, on the server: jasette-serveur --jumeler). Enter its address and the code shown here.",
    "Continuer": "Continue",
    "Le serveur": "Does the server",
    "affiche-t-il ce code de vérification ?": "show this verification code?",
    "Oui, jumeler": "Yes, pair",
    "Non, annuler": "No, cancel",
    "Jumelage annulé : les codes ne correspondaient pas.": "Pairing cancelled: the codes did not match.",
    "adresse vide": "empty address",
    "adresse invalide": "invalid address",
    "port invalide": "invalid port",
    "ce n'est pas un serveur Jasette": "this is not a Jasette server",
    "connexion fermée": "connection closed",
    "connexion perdue": "connection lost",
    "délai dépassé": "timed out",
    "pas de certificat": "no certificate",
    "le certificat du serveur a changé : connexion refusée. S'il a été réinstallé, jumelle de nouveau.":
        "the server's certificate has changed: connection refused. If it was reinstalled, pair again.",
    "le code de jumelage a 6 chiffres": "the pairing code has 6 digits",
    "le serveur n'a pas prouvé qu'il connaissait le code": "the server did not prove it knew the code",
    "réponse inattendue du serveur": "unexpected answer from the server",
    "serveur injoignable": "server unreachable",
    "appareil inconnu ou révoqué : jumelez-le de nouveau": "unknown or revoked device: pair it again",
    "code de jumelage invalide ou expiré": "invalid or expired pairing code",
    "Connexion…": "Connecting…",
    "Reconnexion…": "Reconnecting…",
    "Connecté à ton serveur Jasette": "Connected to your Jasette server",
    "Hors ligne": "Offline",
    "Version limitée (alpha) : client d'un Serveur Jasette, sans notifications push ni connexion des services.":
        "Limited version (alpha): a client for a Jasette Server, without push notifications or service sign-in.",

    // La liste.
    "Tout": "All",
    "Groupes": "Groups",
    "Aucune conversation": "No conversations",
    "Chargement des conversations…": "Loading conversations…",
    "Aucun service n'est connecté sur ton serveur Jasette. Connecte-les sur le serveur.":
        "No service is connected on your Jasette server. Connect them on the server.",
    "Hier": "Yesterday",
    "Épinglée": "Pinned",

    // Le fil.
    "Message": "Message",
    "Envoyer": "Send",
    "Envoi…": "Sending…",
    "Échec": "Failed",
    "Messages plus anciens": "Older messages",
    "Lecture seule": "Read only",
    "Image non reçue · Réessayer": "Image not received · Retry",
    "📷 Photo": "📷 Photo",
    "📎 Pièce jointe": "📎 Attachment",
    "Aujourd'hui": "Today",
    "Moi": "Me",

    // Réglages.
    "Réglages": "Settings",
    "OK": "OK",
    "Fermer": "Close",
    "Votre serveur": "Your server",
    "Adresse": "Address",
    "Cet appareil": "This device",
    "État": "Status",
    "Services": "Services",
    "Les services se connectent et se déconnectent sur le serveur.":
        "Services are connected and disconnected on the server.",
    "Langue": "Language",
    "Automatique": "Automatic",
    "À propos": "About",
    "Version": "Version",
    "Déconnecter": "Sign out",
    "Ne plus utiliser ce serveur ?": "Stop using this server?",
    "Cet appareil se débranchera du serveur Jasette. Tes services restent connectés sur le serveur ; pour revenir, il faudra jumeler de nouveau avec un nouveau code.":
        "This device will disconnect from the Jasette server. Your services stay connected on the server; to come back, you will need to pair again with a new code.",
    "Annuler": "Cancel",
    "prêt": "ready",
    "connexion": "connecting",
    "non connecté": "not connected",
    "association": "pairing",
    "reconnexion": "reconnecting",
    "téléphone hors ligne": "phone offline",
    "Les notifications push et la connexion des services viendront dans une prochaine version. Jasette se met à jour quand l'app est ouverte.":
        "Push notifications and service sign-in will come in a later version. Jasette refreshes while the app is open.",

    // Messages de service des ponts (§5.14).
    "🚫 Message retiré": "🚫 Message removed",
    "📞 Appel": "📞 Call",
    "Historique effacé": "History cleared",
    "🔐 Le numéro de sécurité de cette personne a changé": "🔐 This person's safety number has changed",
    "⚠ Un message n'a pas pu être déchiffré : vérifie dans Signal sur le téléphone":
        "⚠ A message couldn't be decrypted: check in Signal on the phone",
]
