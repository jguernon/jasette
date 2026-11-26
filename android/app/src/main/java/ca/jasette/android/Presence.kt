package ca.jasette.android

/**
 * La présence en ligne d'une conversation individuelle (serveur/PROTOCOLE.md,
 * §5.10) : l'événement `presence` des ponts Messenger, Instagram, WhatsApp et
 * Telegram. Gardée par [Depot.presences] ; affichée seulement si
 * [Marques.presenceEnLigne].
 */
data class Presence(val online: Boolean, val lastActive: Long)

/** Au-delà, « Actif il y a… » ne dit plus rien. */
private const val ACTIVITE_MAX_MS = 24L * 3600 * 1000

/** « Actif maintenant », « Actif il y a 12 min », « Actif il y a 3 h », ou null (inconnue, plus de 24 h). */
fun texteActivite(p: Presence, maintenant: Long, anglais: Boolean = Langue.anglais): String? {
    if (p.online) return if (anglais) "Active now" else "Actif maintenant"
    if (p.lastActive <= 0) return null
    val ecart = (maintenant - p.lastActive).coerceAtLeast(0)
    if (ecart >= ACTIVITE_MAX_MS) return null
    val minutes = (ecart / 60_000).coerceAtLeast(1)
    return if (minutes < 60) {
        if (anglais) "Active $minutes min ago" else "Actif il y a $minutes min"
    } else {
        val heures = minutes / 60
        if (anglais) "Active $heures h ago" else "Actif il y a $heures h"
    }
}
