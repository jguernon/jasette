package ca.jasette.android

import org.json.JSONObject
import java.time.Instant
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.temporal.ChronoUnit

/*
 * La sourdine par conversation (comme src/sourdine.rs du bureau) : « Mettre en
 * sourdine » coupe seulement les notifications système de la conversation
 * (aucune notification, aucun son, aucune vibration, rien d'annoncé sur
 * Android Auto) pour 1 h, 8 h, 24 h, 1 semaine ou « jusqu'à ce que je la
 * réactive ». Dans l'app, rien ne change, sauf la cloche barrée
 * (NotificationsOff) dans la liste, l'en-tête du fil et la fiche.
 *
 * Marque partagée `sourdine` (Marques.sourdine, PROTOCOLE.md §5.5) : id →
 * échéance (ms), 0 = sans fin. Une échéance passée ne compte plus.
 */

/** Les durées offertes par la fenêtre « Mettre en sourdine ». */
enum class DureeSourdine(val libelle: String, private val ms: Long) {
    HEURE("Pendant 1 heure", 3_600_000L),
    HUIT_HEURES("Pendant 8 heures", 8 * 3_600_000L),
    JOUR("Pendant 24 heures", 24 * 3_600_000L),
    SEMAINE("Pendant 1 semaine", 7 * 24 * 3_600_000L),
    TOUJOURS("Jusqu'à ce que je la réactive", 0L);

    /** L'échéance d'une sourdine posée à [maintenant] ; 0 : sans fin. */
    fun echeance(maintenant: Long): Long = if (ms == 0L) 0L else maintenant + ms
}

/** [id] est-elle en sourdine à [maintenant] (ms) ? */
fun sourdineActive(s: Map<String, Long>, id: String, maintenant: Long): Boolean {
    val t = s[id] ?: return false
    return t == 0L || t > maintenant
}

/** La sourdine normalisée comme par le serveur : ids propres, rien de négatif ni d'échu. */
fun nettoyerSourdine(m: Map<String, Long>, maintenant: Long = System.currentTimeMillis()): Map<String, Long> =
    m.filter { (k, v) -> k.isNotEmpty() && k.toByteArray().size <= 512 && (v == 0L || v > maintenant) }

/** {id: échéance} d'un objet JSON ; les valeurs non numériques sont ignorées. */
fun lireSourdine(o: JSONObject): Map<String, Long> {
    val r = HashMap<String, Long>()
    for (k in o.keys()) {
        val v = o.opt(k) as? Number ?: continue
        r[k] = v.toLong()
    }
    return nettoyerSourdine(r)
}

/** L'échéance commune de plusieurs conversations (la fiche) : 0 si l'une est sans fin, sinon la plus lointaine ; null : aucune. */
fun echeanceSourdine(s: Map<String, Long>, ids: Collection<String>, maintenant: Long = System.currentTimeMillis()): Long? {
    val l = ids.filter { sourdineActive(s, it, maintenant) }.mapNotNull { s[it] }
    return when {
        l.isEmpty() -> null
        0L in l -> 0L
        else -> l.max()
    }
}

/**
 * La notification système d'un message de [conv] a-t-elle lieu ? Non pour une
 * conversation en sourdine (Depot, Android Auto) ; oui dès l'échéance passée.
 * Jamais pour une conversation marquée comme spam (Spam.kt), même sans sourdine.
 */
fun notificationPermise(conv: String, maintenant: Long = System.currentTimeMillis()): Boolean =
    notificationPermiseDans(Marques.spam, Marques.sourdine, conv, maintenant)

/** [notificationPermise] sur des marques données (pour les essais). */
fun notificationPermiseDans(spam: Map<String, Long>, sourdine: Map<String, Long>, conv: String, maintenant: Long): Boolean =
    conv !in spam && !sourdineActive(sourdine, conv, maintenant)

private val JOURS_FR = listOf("lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche")
private val JOURS_EN = listOf("Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday")
private val MOIS_FR = listOf("janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc.")
private val MOIS_EN = listOf("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")

/** « En sourdine jusqu'à demain 14 h 00 » (heures locales) ; [fin] null : « En sourdine ». */
fun libelleSourdineA(fin: LocalDateTime?, maintenant: LocalDateTime, en: Boolean): String {
    if (fin == null) return if (en) "Muted" else "En sourdine"
    val h = fin.hour
    val m = "%02d".format(fin.minute)
    val jours = ChronoUnit.DAYS.between(maintenant.toLocalDate(), fin.toLocalDate())
    val jour = fin.dayOfWeek.value - 1
    return if (en) {
        val heure = "${if (h % 12 == 0) 12 else h % 12}:$m ${if (h < 12) "AM" else "PM"}"
        when (jours) {
            0L -> "Muted until $heure"
            1L -> "Muted until tomorrow $heure"
            in 2L..6L -> "Muted until ${JOURS_EN[jour]} $heure"
            else -> "Muted until ${MOIS_EN[fin.monthValue - 1]} ${fin.dayOfMonth}, $heure"
        }
    } else {
        val heure = "$h h $m"
        when (jours) {
            0L -> "En sourdine jusqu'à $heure"
            1L -> "En sourdine jusqu'à demain $heure"
            in 2L..6L -> "En sourdine jusqu'à ${JOURS_FR[jour]} $heure"
            else -> "En sourdine jusqu'au ${if (fin.dayOfMonth == 1) "1er" else fin.dayOfMonth.toString()} ${MOIS_FR[fin.monthValue - 1]} $heure"
        }
    }
}

/** Le libellé d'une échéance (ms ; 0 : sans fin), à l'heure du téléphone. */
fun libelleSourdine(echeance: Long): String {
    val zone = ZoneId.systemDefault()
    val fin = if (echeance > 0) LocalDateTime.ofInstant(Instant.ofEpochMilli(echeance), zone) else null
    return libelleSourdineA(fin, LocalDateTime.now(zone), Langue.anglais)
}
