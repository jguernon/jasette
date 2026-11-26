package ca.jasette.android

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import org.json.JSONObject

/**
 * « Quoi de neuf ? » (serveur/PROTOCOLE.md §5.12) : ma note (ou mon statut)
 * sur chaque service, les notes Instagram de mes contacts, et le résultat de
 * la dernière publication. Tout se passe sur le fil principal ([Depot] y
 * renvoie les nouvelles des ponts).
 */
object Notes {
    /** Les services qui ont une note ou un statut, et leur limite (points de code). */
    val LIMITES = mapOf(
        "instagram" to 60, "messenger" to 60, "whatsapp" to 139, "telegram" to 70, "bluesky" to 256, "discord" to 128,
    )

    /** Ceux où la note remplace une bio (le pont remet l'ancienne à l'effacement). */
    val BIOS = setOf("whatsapp", "telegram", "bluesky", "discord")

    /** Ma note sur un service (`note_moi`) ; [mainText] : la note écrite à la main mise de côté par la musique. */
    data class NoteMoi(
        val text: String,
        val time: Long,
        val expires: Long,
        val source: String,
        val audience: Int,
        val mainText: String = "",
        val mainTime: Long = 0,
    ) {
        fun active(maintenant: Long) = text.isNotEmpty() && (expires == 0L || expires > maintenant)
    }

    /** La note d'un contact Instagram (événement `notes`). */
    data class NoteContact(val conv: String, val user: String, val username: String, val text: String, val time: Long)

    /** Le résultat d'une publication, par service : en cours, ✓, ou l'erreur. */
    data class Resultat(val enCours: Boolean, val erreur: String? = null)

    val miennes = mutableStateMapOf<Service, NoteMoi>()
    /** Les notes des contacts, par conversation (id préfixé, « i:… »). */
    val contacts = mutableStateMapOf<String, NoteContact>()
    val resultats = mutableStateMapOf<Service, Resultat>()

    /** L'audience Instagram : 0 abonnés que je suis en retour, 1 amis proches. Gardée sur l'appareil. */
    var audience by mutableStateOf(0)
        private set

    private var prefs: android.content.SharedPreferences? = null
    private var seq = 0L
    /** La commande manuelle en cours, par service (req), pour ne montrer que son résultat. */
    private val attentes = HashMap<Service, String>()
    private var derniereListe = 0L

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences("reglages", Context.MODE_PRIVATE)
        prefs = p
        audience = p.getInt("notes_audience", 0).coerceIn(0, 1)
    }

    fun choisirAudience(a: Int) {
        audience = a.coerceIn(0, 1)
        prefs?.edit()?.putInt("notes_audience", audience)?.apply()
    }

    /** Les services compatibles, offerts et prêts, dans l'ordre de l'app. */
    fun compatibles(): List<Service> = Depot.services.filter { it.cle in LIMITES && Depot.etat(it) == Etat.PRET }

    /** Les services choisis (`notes_services`) parmi les compatibles prêts. */
    fun choisis(): List<Service> {
        val sel = Marques.notesServices
        return compatibles().filter { sel == listOf("tout") || it.cle in sel }
    }

    /** La limite du service le plus limitant parmi [services] (60 sans service). */
    fun limite(services: List<Service>): Int = services.mapNotNull { LIMITES[it.cle] }.minOrNull() ?: 60

    /** Ma note actuelle : la plus récente des notes actives, ou null. */
    fun actuelle(maintenant: Long = System.currentTimeMillis()): NoteMoi? =
        miennes.values.filter { it.active(maintenant) }.maxByOrNull { it.time }

    /**
     * L'échéance (ms) de ma note actuelle : la plus tardive des services qui la
     * portent ; null si l'un d'eux la garde sans échéance (une bio), ou sans note.
     */
    fun echeance(maintenant: Long = System.currentTimeMillis()): Long? {
        val a = actuelle(maintenant) ?: return null
        val memes = miennes.values.filter { it.active(maintenant) && it.text == a.text }
        return if (memes.any { it.expires == 0L }) null else memes.maxOfOrNull { it.expires }
    }

    /** La note actuelle d'un service au moins vient-elle de la musique ? */
    fun musicale(maintenant: Long = System.currentTimeMillis()): Boolean =
        miennes.values.any { it.source == "musique" && it.active(maintenant) }

    /** La note d'un contact à montrer dans le Head (réglage activé), ou null. */
    fun deContact(conv: String): NoteContact? = if (!Marques.notesContacts) null else contacts[conv]

    /**
     * Publie [texte] : une seule trame `note_set` par service, une fois. [source] :
     * "main" (écrite à la main) ou "musique". Rend les services visés.
     */
    fun publier(texte: String, source: String = "main", services: List<Service> = choisis()): List<Service> {
        val t = texte.trim()
        if (t.isEmpty()) return emptyList()
        for (s in services) {
            val lim = LIMITES[s.cle] ?: continue
            val req = "n${System.currentTimeMillis()}-${++seq}"
            val cmd = JSONObject().put("cmd", "note_set").put("text", tronquerNote(t, lim)).put("source", source).put("req", req)
            if (s == Service.INSTAGRAM) cmd.put("audience", audience)
            if (source == "main") {
                attentes[s] = req
                resultats[s] = Resultat(true)
            }
            Depot.commande(s, cmd)
        }
        return services
    }

    /**
     * Efface ma note : `note_clear` aux services qui en ont une (sinon aux
     * choisis). Avec [source] "musique", le pont n'efface que la note musicale
     * (et remet la note écrite à la main mise de côté, si elle a moins de 24 h).
     */
    fun effacer(source: String = "main") {
        val maintenant = System.currentTimeMillis()
        val avec = compatibles().filter { miennes[it]?.active(maintenant) == true }
        val cibles = if (source == "musique") {
            compatibles().filter { miennes[it]?.source == "musique" }.ifEmpty { choisis() }
        } else {
            avec.ifEmpty { choisis() }
        }
        for (s in cibles) {
            val req = "n${System.currentTimeMillis()}-${++seq}"
            val cmd = JSONObject().put("cmd", "note_clear").put("req", req)
            if (source == "musique") cmd.put("source", "musique")
            else {
                attentes[s] = req
                resultats[s] = Resultat(true)
            }
            Depot.commande(s, cmd)
        }
    }

    /** Les résultats de la dernière publication, effacés (l'écran « Moi » se ferme). */
    fun oublierResultats() {
        resultats.keys.filter { resultats[it]?.enCours != true }.forEach { resultats.remove(it) }
    }

    /** `notes_list` au pont Instagram, au plus toutes les 15 minutes, si le réglage est activé. */
    fun demanderListe(force: Boolean = false) {
        if (!Marques.notesContacts || Depot.etat(Service.INSTAGRAM) != Etat.PRET) return
        val maintenant = System.currentTimeMillis()
        if (!force && maintenant - derniereListe < 15 * 60_000L) return
        derniereListe = maintenant
        Depot.commande(Service.INSTAGRAM, JSONObject().put("cmd", "notes_list"))
    }

    /** Un événement `note_moi`, `note_result` ou `notes` d'un pont (ids déjà préfixés). */
    fun recevoir(v: JSONObject, service: Service) {
        when (v.chaine("ev")) {
            "note_moi" -> miennes[service] = NoteMoi(
                v.chaine("text"), v.optLong("time", 0), v.optLong("expires", 0),
                v.chaine("source").ifEmpty { "main" }, v.optInt("audience", 0),
                v.chaine("main_text"), v.optLong("main_time", 0),
            )
            "note_result" -> {
                val req = v.chaine("req")
                // Le résultat d'une publication automatique (musique) ne s'affiche pas.
                if (req.isEmpty() || attentes[service] != req) return
                attentes.remove(service)
                resultats[service] = if (v.optBoolean("ok")) Resultat(false) else
                    Resultat(false, v.chaine("error").ifEmpty { t("La publication a échoué.") })
            }
            "notes" -> {
                if (!Marques.notesContacts || service != Service.INSTAGRAM) return
                val items = v.optJSONArray("items")
                val n = HashMap<String, NoteContact>()
                if (items != null) for (i in 0 until items.length()) {
                    val o = items.optJSONObject(i) ?: continue
                    val conv = o.chaine("conv")
                    val texte = o.chaine("text")
                    if (conv.isEmpty() || texte.isEmpty()) continue
                    n[conv] = NoteContact(conv, o.chaine("user"), o.chaine("username"), texte, o.optLong("time", 0))
                }
                // La liste remplace la précédente.
                contacts.keys.filter { it !in n }.forEach { contacts.remove(it) }
                contacts.putAll(n)
            }
        }
    }

    /** Le réglage « Afficher les notes de mes contacts » change : désactivé, tout est oublié. */
    fun contactsRegles(oui: Boolean) {
        if (!oui) contacts.clear()
        derniereListe = 0
    }

    /** Un service n'est plus associé (ou un autre mode) : ses notes ne valent plus. */
    fun oublier(service: Service) {
        miennes.remove(service)
        resultats.remove(service)
        attentes.remove(service)
        if (service == Service.INSTAGRAM) contacts.clear()
    }

    fun vider() {
        miennes.clear()
        contacts.clear()
        resultats.clear()
        attentes.clear()
        derniereListe = 0
    }
}

/** Un point de code qui commence un émoji (Extended_Pictographic, à peu près, et les drapeaux). */
private fun debutEmoji(c: Int): Boolean =
    (c in 0x1F000..0x1FAFF || c in 0x2600..0x27BF || c in 0x2300..0x23FF || c in 0x2B05..0x2B55 ||
        c in 0x2190..0x21FF || c == 0x2934 || c == 0x2935 || c in 0x25AA..0x25FE || c == 0x203C || c == 0x2049 ||
        c == 0x2122 || c == 0x2139 || c == 0x3030 || c == 0x303D || c == 0x3297 || c == 0x3299 || c == 0xA9 || c == 0xAE) &&
        c !in 0x1F3FB..0x1F3FF

private fun regional(c: Int) = c in 0x1F1E6..0x1F1FF

/** Ce qui prolonge un émoji : variante, teinte de peau, touche, étiquettes. */
private fun modificateur(c: Int) = c == 0xFE0F || c == 0xFE0E || c in 0x1F3FB..0x1F3FF || c == 0x20E3 || c in 0xE0020..0xE007F

/**
 * Le premier émoji de [texte], avec ses séquences (ZWJ, variante, peau,
 * drapeaux, touches « 1️⃣ ») ; null s'il n'y en a pas.
 */
fun premierEmoji(texte: String): String? {
    val v = texte.codePoints().toArray()
    var i = 0
    while (i < v.size) {
        val c = v[i]
        val touche = (c in '0'.code..'9'.code || c == '#'.code || c == '*'.code) &&
            (v.getOrNull(i + 1) == 0x20E3 || (v.getOrNull(i + 1) == 0xFE0F && v.getOrNull(i + 2) == 0x20E3))
        if (!touche && !debutEmoji(c)) { i++; continue }
        var fin = i + 1
        if (regional(c)) {
            if (v.getOrNull(fin)?.let(::regional) == true) fin++
        } else {
            while (true) {
                while (v.getOrNull(fin)?.let(::modificateur) == true) fin++
                // ZWJ suivi d'un autre émoji : la séquence continue.
                if (v.getOrNull(fin) == 0x200D && v.getOrNull(fin + 1)?.let(::debutEmoji) == true) { fin += 2; continue }
                break
            }
        }
        return String(v, i, fin - i)
    }
    return null
}

/**
 * Le message n'est-il qu'un seul émoji (un graphème : ZWJ, peau, drapeau,
 * touche compris), espaces retirés ? Il s'affiche alors en grand, sans bulle.
 * Deux émojis ou plus restent du texte ordinaire.
 */
fun emojiSeul(texte: String): Boolean {
    val t = texte.trim()
    if (t.isEmpty()) return false
    val e = premierEmoji(t) ?: return false
    if (e != t) return false
    // Un symbole de texte seul (©, ®, ™, ↔…) n'est pas un émoji sans sa variante.
    val c = e.codePointAt(0)
    return e.codePointCount(0, e.length) > 1 || c >= 0x1F000 || c in 0x2600..0x27BF || c in 0x2B05..0x2B55 || c in 0x2300..0x23FF
}
