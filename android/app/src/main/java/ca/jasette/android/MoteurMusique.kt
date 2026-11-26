package ca.jasette.android

import org.json.JSONObject

/**
 * « Partager ce que j'écoute » (serveur/PROTOCOLE.md §5.12) : la partie sans
 * Android, testable sur la JVM. Le lecteur (EcouteMusique.kt) appelle
 * [MoteurMusique.pas] régulièrement ; le moteur décide s'il faut publier ou
 * effacer, avec l'anti-rafale :
 *
 * - au plus une publication toutes les 15 minutes, et seulement si le texte a
 *   changé depuis la dernière publication musicale (ou si la note actuelle
 *   n'est pas musicale : la musique passe par-dessus la note écrite à la main) ;
 * - une pause n'efface rien : `note_clear` (source « musique ») seulement après
 *   20 minutes sans lecture, une seule fois ;
 * - pour une période de 15 minutes, on retient ce qui a **joué le plus
 *   longtemps** (durées cumulées), pas seulement le dernier morceau.
 */

/** Ce qui joue : un morceau, ou un épisode de balado ([balado], [emission]). */
data class Ecoute(val titre: String, val artiste: String, val emission: String = "", val balado: Boolean = false) {
    /** Ce qu'on cumule : l'artiste (ou l'émission), ou le morceau pour le format « titre ». */
    fun cle(format: String): String = when {
        format == "titre" -> "$titre\u0000$artiste"
        balado -> "🎙\u0000" + emission.ifEmpty { artiste }
        else -> artiste
    }
}

class MoteurMusique {
    sealed class Action {
        data class Publier(val texte: String) : Action()
        object Effacer : Action()
    }

    /** L'heure de la dernière lecture vue (ms), 0 : jamais. */
    var derniereLecture = 0L
    /** L'heure de la dernière publication musicale (ms), 0 : jamais. */
    var dernierePublication = 0L
    /** Le texte de la dernière note musicale publiée, ou null (aucune, ou effacée). */
    var texteMusical: String? = null

    /** Les écoutes des 15 dernières minutes : (heure, clé, durée ms), et la dernière écoute de chaque clé. */
    private val echantillons = ArrayDeque<Triple<Long, String, Long>>()
    private val ecoutes = HashMap<String, Ecoute>()
    private var dernierPas = 0L

    /** Ce qui a joué le plus longtemps pendant les 15 dernières minutes. */
    private fun retenue(maintenant: Long): Ecoute? {
        while (echantillons.isNotEmpty() && maintenant - echantillons.first().first >= QUINZE_MIN) echantillons.removeFirst()
        val total = HashMap<String, Long>()
        for ((_, cle, d) in echantillons) total[cle] = (total[cle] ?: 0L) + d
        ecoutes.keys.retainAll(total.keys)
        return total.maxByOrNull { it.value }?.key?.let { ecoutes[it] }
    }

    /**
     * Un pas, à appeler toutes les minutes environ (et à chaque changement du lecteur).
     * [lecture] : ce qui joue maintenant, null en pause ou à l'arrêt.
     * [actuelleMusicale] : la note actuelle (sur un service au moins) vient de la musique.
     * [peutPublier] : partage activé ici, au moins un service prêt.
     * [texte] : le texte d'une écoute, ou null s'il n'est pas encore prêt (genre en cours de recherche).
     */
    fun pas(
        maintenant: Long,
        lecture: Ecoute?,
        format: String,
        actuelleMusicale: Boolean,
        peutPublier: Boolean,
        texte: (Ecoute) -> String?,
    ): Action? {
        val ecart = if (dernierPas == 0L) 0L else (maintenant - dernierPas).coerceIn(0L, MAX_PAS)
        dernierPas = maintenant
        if (lecture != null) {
            derniereLecture = maintenant
            val cle = lecture.cle(format)
            // Le premier pas d'une écoute compte au moins une seconde : elle existe.
            echantillons.addLast(Triple(maintenant, cle, maxOf(ecart, 1_000L)))
            ecoutes[cle] = lecture
            val choisie = retenue(maintenant) ?: lecture
            if (!peutPublier) return null
            if (dernierePublication != 0L && maintenant - dernierePublication < QUINZE_MIN) return null
            val t = texte(choisie) ?: return null
            if (t == texteMusical && actuelleMusicale) return null
            texteMusical = t
            dernierePublication = maintenant
            return Action.Publier(t)
        }
        // Rien ne joue : on efface une seule fois, après 20 minutes.
        if (texteMusical != null && derniereLecture != 0L && maintenant - derniereLecture >= VINGT_MIN) {
            texteMusical = null
            echantillons.clear()
            ecoutes.clear()
            return Action.Effacer
        }
        return null
    }

    companion object {
        const val QUINZE_MIN = 15 * 60_000L
        const val VINGT_MIN = 20 * 60_000L
        /** Un pas ne compte jamais plus de 2 minutes (appareil en veille entre deux pas). */
        const val MAX_PAS = 2 * 60_000L
    }
}

/** [s] coupé à [max] caractères (points de code), avec « … » s'il est coupé. */
fun tronquerNote(s: String, max: Int): String {
    val n = s.codePointCount(0, s.length)
    if (n <= max) return s
    if (max <= 1) return "…"
    val fin = s.offsetByCodePoints(0, max - 1)
    return s.substring(0, fin).trimEnd() + "…"
}

/** Le nombre de caractères d'une note, en points de code (comme les ponts). */
fun longueurNote(s: String): Int = s.codePointCount(0, s.length)

/**
 * Le texte de la note musicale (§5.12), tronqué à [limite] :
 * « 🎵 Écoute Daft Punk », « 🎙️ Écoute Joe Rogan », « 🎵 Écoute du rock ! »,
 * « 🎙️ Écoute un podcast », « 🎵 Titre — Artiste ». [genre] : le genre
 * trouvé (format « genre »), ou null (inconnu : comme « artiste »).
 */
fun texteMusique(e: Ecoute, format: String, genre: String?, anglais: Boolean, limite: Int): String {
    val emoji = if (e.balado) "🎙️" else "🎵"
    val qui = if (e.balado) e.emission.ifEmpty { e.artiste } else e.artiste
    val brut = when {
        format == "titre" -> "$emoji ${e.titre}" + if (qui.isNotEmpty()) " — $qui" else ""
        format == "genre" && e.balado -> if (anglais) "🎙️ Listening to a podcast" else "🎙️ Écoute un podcast"
        format == "genre" && !genre.isNullOrEmpty() ->
            if (anglais) "🎵 Listening to ${genreAnglais(genre)}!" else "🎵 Écoute ${genreFrancais(genre)} !"
        else -> if (anglais) "$emoji Listening to ${qui.ifEmpty { e.titre }}" else "$emoji Écoute ${qui.ifEmpty { e.titre }}"
    }
    return tronquerNote(brut, limite)
}

/** Les genres courants, avec leur article (« du rock », « de la pop »). */
private val GENRES_FR = mapOf(
    "rock" to "du rock", "pop" to "de la pop", "hip hop" to "du hip-hop", "hip-hop" to "du hip-hop",
    "rap" to "du rap", "jazz" to "du jazz", "electronic" to "de l'électro", "electronica" to "de l'électro",
    "electro" to "de l'électro", "edm" to "de l'électro", "dance" to "de la dance", "classical" to "du classique",
    "metal" to "du metal", "heavy metal" to "du metal", "country" to "du country", "r&b" to "du R&B",
    "rnb" to "du R&B", "rhythm and blues" to "du R&B", "contemporary r&b" to "du R&B", "folk" to "du folk",
    "punk" to "du punk", "punk rock" to "du punk", "blues" to "du blues", "reggae" to "du reggae",
    "soul" to "de la soul", "funk" to "du funk", "indie" to "de l'indie", "indie rock" to "du rock indé",
    "indie pop" to "de la pop indé", "alternative rock" to "du rock alternatif", "alternative" to "du rock alternatif",
    "house" to "de la house", "techno" to "de la techno", "disco" to "du disco", "latin" to "de la musique latine",
    "k-pop" to "de la K-pop", "soundtrack" to "une trame sonore", "ambient" to "de l'ambient",
    "gospel" to "du gospel", "chanson" to "de la chanson", "french pop" to "de la pop française",
    "synth-pop" to "de la synthpop", "synthpop" to "de la synthpop", "grunge" to "du grunge", "emo" to "de l'emo",
    "trap" to "de la trap", "drum and bass" to "du drum and bass", "dubstep" to "du dubstep", "lo-fi" to "du lo-fi",
)

private fun genreFrancais(g: String): String {
    val k = g.trim().lowercase()
    GENRES_FR[k]?.let { return it }
    // Tel quel, avec un article : « de l' » devant une voyelle.
    return if (k.firstOrNull()?.let { it in "aeiouyéèêàh" } == true) "de l'$g" else "du $g"
}

private fun genreAnglais(g: String): String = when (g.trim().lowercase()) {
    "r&b", "rnb", "rhythm and blues", "contemporary r&b" -> "R&B"
    "hip hop" -> "hip-hop"
    "k-pop" -> "K-pop"
    else -> g.trim().lowercase()
}

/**
 * Le genre d'une réponse de MusicBrainz (`/ws/2/artist/?query=…&fmt=json`) :
 * le tag le plus voté (`count`) du premier artiste, ou null.
 */
fun genreMusicBrainz(json: String): String? = try {
    val artistes = JSONObject(json).optJSONArray("artists")
    val premier = artistes?.optJSONObject(0)
    val tags = premier?.optJSONArray("tags")
    var meilleur: String? = null
    var votes = Int.MIN_VALUE
    if (tags != null) for (i in 0 until tags.length()) {
        val t = tags.optJSONObject(i) ?: continue
        val nom = t.optString("name", "").trim()
        val n = t.optInt("count", 0)
        if (nom.isNotEmpty() && n > votes) {
            meilleur = nom
            votes = n
        }
    }
    meilleur
} catch (_: Exception) {
    null
}

/**
 * La sélection « Publier sur » après un clic sur la case d'un service
 * ([cle]) : depuis « Tout », tous les [compatibles] sauf lui ; sinon on
 * l'ajoute ou le retire. Jamais vide (le dernier ne se décoche pas).
 * Seule la case « Tout » ramène ["tout"].
 */
fun basculerNotesService(actuel: List<String>, cle: String, compatibles: List<String>): List<String> {
    val base = if (actuel == listOf("tout")) compatibles else actuel
    val n = if (cle in base) base - cle else base + cle
    return if (n.isEmpty()) actuel else n
}

/**
 * Ce que joue Spotify, d'après les métadonnées de sa session (MediaMetadata),
 * ou null sans titre ni artiste. Un balado se reconnaît à l'id du média
 * (`METADATA_KEY_MEDIA_ID`) qui contient « episode » (spotify:episode:…) ;
 * l'émission est dans l'album, sinon dans l'artiste. Supposé d'après les
 * sessions de Spotify observées ailleurs (MPRIS) : à vérifier au premier essai.
 */
fun ecouteSpotify(titre: String?, artiste: String?, album: String?, idMedia: String?): Ecoute? {
    val t = titre.orEmpty().trim()
    val a = artiste.orEmpty().trim()
    if (t.isEmpty() && a.isEmpty()) return null
    val balado = idMedia.orEmpty().contains("episode", ignoreCase = true)
    return Ecoute(t, a, if (balado) album.orEmpty().trim().ifEmpty { a } else "", balado)
}
