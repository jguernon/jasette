package ca.jasette.android

import org.json.JSONObject

/*
 * Le spam par conversation (comme src/spam.rs du bureau) : « Marquer comme
 * spam » (menu de la conversation, juste sous « Classer… ») range la
 * conversation dans le dossier Spam (Réglages > Spam) et la met en sourdine
 * sans fin. Elle quitte la boîte de réception, les groupes automatiques, le
 * Head, les favoris, les épinglées et les archives ; ses nouveaux messages n'y
 * reviennent pas, ne notifient jamais (ni son, ni vibration, ni Android Auto)
 * et ne comptent pas dans les non-lus.
 *
 * Marque partagée `spam` (Marques.spam, PROTOCOLE.md §5.5) : id → heure (ms)
 * du marquage. « Ce n'est pas du spam » retire la marque et la sourdine sans
 * fin posée avec elle. Une conversation supprimée garde sa marque : si
 * l'expéditeur réécrit, elle revient droit dans Spam.
 *
 * Pour une conversation Google Messages à deux (SMS/RCS), la confirmation
 * offre aussi « Marquer comme spam et envoyer STOP » : le texte exact STOP part
 * par l'envoi ordinaire (Depot.envoyer), seulement sur ce toucher.
 */

/** Le texte envoyé pour se désinscrire des envois massifs par SMS. */
const val TEXTE_STOP = "STOP"

/** La marque normalisée comme par le serveur : ids propres, rien de négatif. */
fun nettoyerSpam(m: Map<String, Long>): Map<String, Long> =
    m.filter { (k, v) -> k.isNotEmpty() && k.toByteArray().size <= 512 && v >= 0 }

/** {id: heure du marquage} d'un objet JSON ; les valeurs non numériques sont ignorées. */
fun lireSpam(o: JSONObject): Map<String, Long> {
    val r = HashMap<String, Long>()
    for (k in o.keys()) {
        val v = o.opt(k) as? Number ?: continue
        r[k] = v.toLong()
    }
    return nettoyerSpam(r)
}

/** « Envoyer STOP » n'est offert que pour une conversation Google Messages (SMS/RCS) à deux. */
fun stopOffert(id: String, groupe: Boolean, grand: Boolean): Boolean =
    Service.de(id) == Service.GOOGLE && !groupe && !grand

fun stopOffert(c: Conv): Boolean = stopOffert(c.id, c.group, c.large)

/** Les conversations du dossier qu'on peut supprimer (le service sait le faire) et celles qui restent. */
fun aViderSpam(l: List<Conv>): Pair<List<Conv>, List<Conv>> = l.partition { it.peutSupprimer }

object Spam {
    /** Le dossier Spam : boîte et archives (pas les privées), les plus récemment marquées d'abord. */
    val liste: List<Conv>
        get() = (Depot.convs + Depot.archives).filter { Marques.estSpam(it.id) && !Marques.estPrivee(it.id) }
            .distinctBy { it.id }
            .sortedWith(compareByDescending<Conv> { Marques.spam[it.id] ?: 0L }.thenByDescending { it.time })

    /**
     * Marque [c] comme spam ; [stop] : envoie aussi STOP (Google à deux
     * seulement, sur toucher explicite), par l'envoi ordinaire.
     */
    fun marquer(c: Conv, stop: Boolean) {
        if (stop && stopOffert(c)) Depot.envoyer(c.id, TEXTE_STOP)
        Marques.marquerSpam(c.id)
    }

    /** « Tout vider » : supprime celles dont le service le permet (comme « Supprimer ») ; rend leur nombre. */
    fun vider(): Int {
        val (oui, _) = aViderSpam(liste)
        for (c in oui) Depot.supprimer(c.id)
        return oui.size
    }
}
