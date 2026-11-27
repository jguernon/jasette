package ca.jasette.android

// Le regroupement automatique de la boîte de réception (Réglages >
// « Regrouper automatiquement », désactivé par défaut) : les expéditeurs de
// codes de vérification (🔐 Codes), de publicités (🏷️ Promotions) et d'avis
// (📦 Avis : livraisons, rendez-vous, banque, factures, alertes) deviennent
// chacun une seule rangée repliable de la boîte.
//
// La même logique, pas à pas, que src/groupes_auto.rs (bureau) ; les deux
// côtés lisent les mêmes essais : assets/essais/groupes-auto.txt
// (GroupesAutoTest.kt). Toute modification se fait des deux côtés.
//
// Tout se décide localement, sans IA ni service externe, par EXPÉDITEUR :
// 1. chaque message reçu reçoit une sorte ([sorteMessage]) : un code de
//    vérification (Codes.kt), une promo, un avis, ou rien ;
// 2. l'expéditeur est un numéro court, un nom alphanumérique ou un expéditeur
//    ordinaire ([sorteExpediteur]) ;
// 3. [classer] décide d'après les derniers messages reçus : au moins deux qui
//    concordent (trois hors SMS pour un expéditeur ordinaire), au moins la
//    moitié ; un seul code suffit d'un numéro court ou d'un nom.
// Jamais classés (l'appelant le dit par `exclue`) : contact, favori, Head,
// épinglée, groupe, grand espace, privée ; ni une conversation où j'ai écrit.
// La correction à la main (marque `groupes_auto`) gagne toujours ([groupeFinal]).

/** Un groupe de la boîte. */
enum class GroupeAuto(val cle: String, val emoji: String, val fond: Long) {
    CODES("codes", "🔐", 0x7c6cf0),
    PROMOS("promos", "🏷️", 0xf0806c),
    AVIS("avis", "📦", 0xd9a441);

    /** Le titre de sa rangée (à traduire par t()). */
    val titre: String get() = when (this) {
        CODES -> "Codes"
        PROMOS -> "Promotions"
        AVIS -> "Avis"
    }
}

/**
 * Une valeur de la marque `groupes_auto` : [Correction] « mettre dans g » (g
 * null : « aucun », jamais regroupée) ; null : valeur inconnue.
 */
data class Correction(val groupe: GroupeAuto?)

fun lireCorrection(v: String?): Correction? = when (v) {
    "codes" -> Correction(GroupeAuto.CODES)
    "promos" -> Correction(GroupeAuto.PROMOS)
    "avis" -> Correction(GroupeAuto.AVIS)
    "aucun" -> Correction(null)
    else -> null
}

/** Le groupe d'une conversation : la correction à la main d'abord (inconnue : ignorée), sinon la détection. */
fun groupeFinal(correction: String?, detecte: GroupeAuto?): GroupeAuto? {
    val c = lireCorrection(correction)
    return if (c != null) c.groupe else detecte
}

/** La sorte d'un message reçu. */
enum class SorteMessage { CODE, PROMO, AVIS, AUTRE }

/** La sorte d'un expéditeur. */
enum class SorteExpediteur {
    /** Un numéro court (3 à 6 chiffres). */
    COURT,
    /** Un nom au lieu d'un numéro (« Google », « Desjardins »). */
    ALPHANUMERIQUE,
    /** Un numéro ordinaire, ou une personne d'un autre service. */
    ORDINAIRE,
}

private val PROMO_FORTS = listOf(
    "rabais", "discount", "code promo", "promo code", "coupon", "en solde", "soldes", "économisez", "economisez",
    "% off", "% de rabais", "black friday", "cyber monday", "liquidation", "clearance", "save up to",
)

private val PROMO_FAIBLES = listOf(
    "promo", "promotion", "promotions", "offre", "offres", "offer", "offers", "deal", "deals", "sale", "gratuit",
    "gratuite", "free", "exclusif", "exclusive", "limited time", "durée limitée", "aujourd'hui seulement", "today only",
    "magasinez",
)

private val DESABONNER =
    listOf("désabonner", "désabonnement", "désinscrire", "unsubscribe", "opt out", "opt-out", "se retirer", "unsub")

private val STOP = listOf("STOP", "ARRET", "ARRÊT")

private val LIENS_COURTS = listOf(
    "bit.ly/", "tinyurl.com/", "t.co/", "goo.gl/", "ow.ly/", "amzn.to/", "rb.gy/", "cutt.ly/", "is.gd/", "buff.ly/",
    "shorturl.at/", "tiny.cc/", "lnkd.in/", "qrco.de/", "bit.do/", "s.id/",
)

private val AVIS = listOf(
    "colis", "package", "parcel", "livraison", "livré", "livrée", "delivered", "delivery", "expédié", "expédiée",
    "shipped", "suivi", "tracking", "rendez-vous", "rdv", "appointment", "rappel", "reminder", "facture", "invoice",
    "bill", "solde", "balance", "paiement", "payment", "dépôt", "deposit", "transaction", "retrait", "withdrawal",
    "prélèvement", "virement", "alerte", "alert", "commande", "order", "réservation", "reservation", "booking",
    "ramassage", "pickup", "échéance", "relevé", "statement",
)

/** Un mot (ou une expression) entier dans [t] ; un bout qui est un signe n'exige pas de limite de ce côté. */
private fun mot(t: CharArray, m: String): Boolean {
    if (m.isEmpty() || m.length > t.size) return false
    val avant = m.first().isLetterOrDigit()
    val apres = m.last().isLetterOrDigit()
    for (i in 0..t.size - m.length) {
        if (!m.indices.all { t[i + it] == m[it] }) continue
        if (avant && i > 0 && t[i - 1].isLetterOrDigit()) continue
        if (apres && i + m.length < t.size && t[i + m.length].isLetterOrDigit()) continue
        return true
    }
    return false
}

private fun chiffre(c: Char) = c in '0'..'9'

/** Un pourcentage : un chiffre, une espace au plus, puis « % ». */
private fun pourcentage(t: CharArray): Boolean = t.indices.any { i ->
    t[i] == '%' && i >= 1 && (chiffre(t[i - 1]) || (i >= 2 && t[i - 1].isWhitespace() && chiffre(t[i - 2])))
}

/** La sorte d'un message reçu (voir src/groupes_auto.rs, `sorte_message`). */
fun sorteMessage(texte: String): SorteMessage {
    if (codeVerification(texte) != null) return SorteMessage.CODE
    val original = CharArray(texte.length) { if (texte[it] == '’') '\'' else texte[it] }
    val t = CharArray(original.size) { original[it].lowercaseChar() }
    var promo = 0
    promo += 2 * PROMO_FORTS.count { mot(t, it) }
    promo += PROMO_FAIBLES.count { mot(t, it) }
    promo += minOf(DESABONNER.count { mot(t, it) }, 1)
    if (STOP.any { mot(original, it) }) promo += 1
    if (pourcentage(t)) promo += 1
    if (LIENS_COURTS.any { mot(t, it) }) promo += 1
    val avis = AVIS.count { mot(t, it) }
    return when {
        promo >= 2 && promo > avis -> SorteMessage.PROMO
        avis >= 1 -> SorteMessage.AVIS
        else -> SorteMessage.AUTRE
    }
}

/** La sorte d'un expéditeur, d'après son numéro (Google) ou, sans numéro, son nom (voir `sorte_expediteur`). */
fun sorteExpediteur(nom: String, numero: String): SorteExpediteur {
    val source = if (numero.isBlank()) nom else numero
    val utiles = source.filter { !it.isWhitespace() && it !in "-+()." }
    if (utiles.isEmpty()) return SorteExpediteur.ORDINAIRE
    if (utiles.all { chiffre(it) }) return if (utiles.length in 3..6) SorteExpediteur.COURT else SorteExpediteur.ORDINAIRE
    if (numero.isNotBlank() && utiles.any { it.isLetter() }) return SorteExpediteur.ALPHANUMERIQUE
    return SorteExpediteur.ORDINAIRE
}

/** Les messages reçus qui comptent : les 10 derniers. */
const val MESSAGES_GROUPE_MAX = 10

/** Ce que [classer] sait d'une conversation. */
data class EntreeGroupe(
    val sms: Boolean,
    val expediteur: SorteExpediteur,
    /** Contact, favori, Head, épinglée, groupe, grand espace, privée : jamais classée. */
    val exclue: Boolean,
    /** J'y ai déjà écrit : jamais classée. */
    val jAiEcrit: Boolean,
    /** Les sortes des messages reçus, du plus vieux au plus récent. */
    val sortes: List<SorteMessage>,
)

/** Le groupe détecté d'une conversation (null : elle reste dans la boîte). */
fun classer(e: EntreeGroupe): GroupeAuto? {
    if (e.exclue || e.jAiEcrit || e.sortes.isEmpty()) return null
    val s = e.sortes.takeLast(MESSAGES_GROUPE_MAX)
    val n = s.size
    val compte = { x: SorteMessage -> s.count { it == x } }
    val commercial = e.expediteur != SorteExpediteur.ORDINAIRE
    // Un code évident d'un numéro court ou d'un nom : tout de suite.
    if (commercial && s[n - 1] == SorteMessage.CODE) return GroupeAuto.CODES
    val codes = compte(SorteMessage.CODE)
    if (codes >= 2 && codes * 2 >= n) return GroupeAuto.CODES
    val seuil = if (commercial || e.sms) 2 else 3
    val avis = compte(SorteMessage.AVIS)
    val promos = compte(SorteMessage.PROMO)
    // À égalité, Avis (le plus utile des deux).
    val (g, c) = if (promos > avis) GroupeAuto.PROMOS to promos else GroupeAuto.AVIS to avis
    return if (c >= seuil && c * 2 >= n) g else null
}

/** La rangée Codes monte en haut 15 minutes après l'arrivée d'un code. */
const val CODES_EN_HAUT_MS = 15L * 60 * 1000

/** La rangée Codes est-elle en haut ? (dernier code reçu à [codeMs]) */
fun codesEnHaut(codeMs: Long, maintenantMs: Long): Boolean =
    codeMs > 0 && maintenantMs >= codeMs && maintenantMs - codeMs < CODES_EN_HAUT_MS
