package ca.jasette.android

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import org.json.JSONArray
import org.json.JSONObject

/**
 * Les favoris et les épingles (`etoiles`/`head` et `epingles` des réglages du
 * bureau) : des listes d'ids de conversations, dans leur ordre, gardées dans
 * les SharedPreferences privées de l'app.
 *
 * Comme au bureau, deux listes distinctes : les favoris (la vue de l'étoile
 * du rail) et le « Head » (la grille d'avatars en haut de la boîte de réception).
 *
 * On y garde aussi deux choix d'apparence : deux volets sur grand écran, et
 * l'heure de chaque message.
 *
 * En mode client du serveur Jasette, le serveur est la source : il garde les
 * trois listes et les partage entre les appareils (serveur/PROTOCOLE.md,
 * §5.5, et `SuiviMarques` dans src/distant.rs, dont ceci est la traduction).
 * La copie locale garde aussi la dernière version reçue (0 : jamais
 * synchronisé avec ce serveur) et « changées ici, pas encore confirmées ».
 * Tout se passe sur le fil principal.
 */
object Marques {
    private const val PREFS = "marques"
    private var prefs: SharedPreferences? = null

    /** Les favoris, dans l'ordre choisi (réorganisable). */
    val favoris: SnapshotStateList<String> = mutableStateListOf()
    /** Les épinglées en haut de la liste, dans l'ordre d'épinglage. */
    val epingles: SnapshotStateList<String> = mutableStateListOf()
    /** Le Head : la grille en haut de la boîte de réception, dans l'ordre d'ajout. */
    val head: SnapshotStateList<String> = mutableStateListOf()

    /** Chaque bulle montre son heure (et sa date si ce n'est pas aujourd'hui), comme `details_heures`. */
    var heureChaqueMessage by mutableStateOf(false)
        private set

    fun choisirHeureChaqueMessage(oui: Boolean) {
        heureChaqueMessage = oui
        prefs?.edit()?.putBoolean("heure_chaque_message", oui)?.apply()
    }

    /** Deux volets (liste + fil) sur grand écran : « Automatique » (vrai, le défaut) ou « Désactivé ». */
    var deuxVolets by mutableStateOf(true)
        private set

    fun choisirDeuxVolets(oui: Boolean) {
        deuxVolets = oui
        prefs?.edit()?.putBoolean("deux_volets", oui)?.apply()
    }

    /** Mon avatar : « google », « messenger », « instagram », « fichier », « initiale » ou vide (pas encore choisi). */
    var avatarSource by mutableStateOf("")
        private set
    /** L'image choisie sur le téléphone (copiée dans les fichiers privés de l'app). */
    var avatarFichier by mutableStateOf<String?>(null)
        private set

    fun choisirAvatar(source: String, fichier: String? = null) {
        avatarSource = source
        if (fichier != null) avatarFichier = fichier
        prefs?.edit()?.putString("avatar_source", source)?.putString("avatar_fichier", avatarFichier)?.apply()
    }

    /** La dernière version des marques reçue du serveur (0 : jamais synchronisé avec lui). */
    private var version = 0L
    /** Changées ici, pas encore confirmées par le serveur. */
    private var aEnvoyer = false
    /** L'empreinte du serveur dont vient [version]. */
    private var serveurMarques = ""

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
        version = p.getLong("version", 0L)
        aEnvoyer = p.getBoolean("a_envoyer", false)
        serveurMarques = p.getString("serveur", "") ?: ""
        avatarSource = p.getString("avatar_source", "") ?: ""
        avatarFichier = p.getString("avatar_fichier", null)
        deuxVolets = p.getBoolean("deux_volets", true)
        heureChaqueMessage = p.getBoolean("heure_chaque_message", false)
        head.clear()
        head.addAll(lire(p, "head"))
        favoris.clear()
        favoris.addAll(lire(p, "favoris"))
        epingles.clear()
        epingles.addAll(lire(p, "epingles"))
    }

    private fun lire(p: SharedPreferences, cle: String): List<String> {
        val brut = p.getString(cle, "[]") ?: "[]"
        return try {
            JSONArray(brut).chaines().distinct()
        } catch (_: Exception) {
            // Jamais perdu : le texte abîmé est mis de côté avant toute réécriture.
            Log.w("jasette", "marques « $cle » illisibles : mises de côté")
            p.edit().putString("${cle}_illisible", brut).commit()
            emptyList()
        }
    }

    /**
     * commit(), pas apply() : écrit tout de suite sur le disque, pour qu'une
     * mise à jour de l'app juste après un changement ne le perde pas.
     */
    private fun garder() {
        prefs?.edit()
            ?.putString("favoris", JSONArray(favoris.toList()).toString())
            ?.putString("epingles", JSONArray(epingles.toList()).toString())
            ?.putString("head", JSONArray(head.toList()).toString())
            ?.putLong("version", version)
            ?.putBoolean("a_envoyer", aEnvoyer)
            ?.putString("serveur", serveurMarques)
            ?.commit()
    }

    fun estFavori(id: String) = id in favoris
    fun estEpinglee(id: String) = id in epingles
    fun estDansHead(id: String) = id in head

    /** Ajoute au Head (à la fin) ou l'en retire. */
    fun basculerHead(id: String) {
        if (!head.remove(id)) head.add(id)
        changement()
    }

    /** Ajoute aux favoris (à la fin) ou les en retire. */
    fun basculerFavori(id: String) {
        if (!favoris.remove(id)) favoris.add(id)
        changement()
    }

    /** Épingle (à la fin des épinglées) ou désépingle. */
    fun basculerEpingle(id: String) {
        if (!epingles.remove(id)) epingles.add(id)
        changement()
    }

    /** Monte (-1) ou descend (+1) un favori d'un cran (comme `deplacer_favori`). */
    fun deplacerFavori(id: String, sens: Int) {
        val i = favoris.indexOf(id)
        val j = i + sens
        if (i < 0 || j < 0 || j >= favoris.size) return
        favoris[i] = favoris[j]
        favoris[j] = id
        changement()
    }

    /** Une conversation supprimée n'est plus ni favorite ni épinglée. */
    fun oublier(id: String) {
        val a = favoris.remove(id)
        val b = epingles.remove(id)
        val c = head.remove(id)
        if (a || b || c) changement()
    }

    // ---- La synchronisation avec le serveur Jasette (§5.5) ----

    /** Les trois listes, figées (head, favoris, épinglées). */
    private data class Listes(val head: List<String>, val favoris: List<String>, val epingles: List<String>) {
        fun vide() = head.isEmpty() && favoris.isEmpty() && epingles.isEmpty()

        /** Chaque liste de this, dans son ordre, puis les ids de [b] qui n'y sont pas. */
        fun union(b: Listes) = Listes(
            (head + b.head).filter { it.isNotEmpty() }.distinct(),
            (favoris + b.favoris).filter { it.isNotEmpty() }.distinct(),
            (epingles + b.epingles).filter { it.isNotEmpty() }.distinct(),
        )

        /** Les ids de this absents de [b], liste par liste. */
        fun moins(b: Listes) = Listes(head - b.head.toSet(), favoris - b.favoris.toSet(), epingles - b.epingles.toSet())
    }

    private fun listes() = Listes(head.toList(), favoris.toList(), epingles.toList())

    private fun poser(l: Listes) {
        if (head.toList() != l.head) { head.clear(); head.addAll(l.head) }
        if (favoris.toList() != l.favoris) { favoris.clear(); favoris.addAll(l.favoris) }
        if (epingles.toList() != l.epingles) { epingles.clear(); epingles.addAll(l.epingles) }
    }

    /** Envoie une trame au serveur ; faux si la connexion est coupée. Posé par [Depot] en mode client. */
    private var envoyeur: ((JSONObject) -> Boolean)? = null
    /** L'empreinte du serveur branché (mode client), ou vide. */
    private var serveurBranche = ""
    /** Le serveur a donné son état sur cette connexion (rejeu reçu). */
    private var synchro = false
    /** Notre trame en attente de réponse : son id et ce qu'elle portait. */
    private var enVol: Pair<Long, Listes>? = null
    private var prochain = 0L

    /** Mode client (ou plus de serveur si [envoi] est null) : tout recommence, on attend le rejeu. */
    fun brancher(empreinte: String?, envoi: ((JSONObject) -> Boolean)?) {
        serveurBranche = empreinte ?: ""
        envoyeur = envoi
        synchro = false
        enVol = null
    }

    private fun changement() {
        aEnvoyer = true
        garder()
        pousser()
    }

    private fun pousser() {
        val envoi = envoyeur ?: return
        if (!synchro || enVol != null || !aEnvoyer) return
        val l = listes()
        prochain++
        enVol = prochain to l
        val corps = JSONObject().put("head", JSONArray(l.head)).put("favoris", JSONArray(l.favoris))
            .put("epingles", JSONArray(l.epingles)).put("version", version).put("id", prochain)
        if (!envoi(JSONObject().put("marques", corps))) {
            // La connexion tombe : le rejeu suivant tranchera.
            synchro = false
            enVol = null
        }
    }

    /** Une trame `marques` du serveur, ou null : la connexion est tombée. Sur le fil principal. */
    fun recu(m: JSONObject?) {
        if (m == null) {
            synchro = false
            enVol = null
            return
        }
        val s = Listes(
            m.optJSONArray("head").chaines().distinct(),
            m.optJSONArray("favoris").chaines().distinct(),
            m.optJSONArray("epingles").chaines().distinct(),
        )
        val v = m.optLong("version", -1)
        if (v < 0) return
        val id = if (m.has("id")) m.optLong("id", -1) else -1
        val refus = m.optString("refus", "")
        val avant = Triple(listes(), version, aEnvoyer)
        val serveurAvant = serveurMarques
        // Un autre serveur que celui des marques locales : jamais synchronisé avec lui.
        if (serveurBranche.isNotEmpty() && serveurMarques != serveurBranche) {
            serveurMarques = serveurBranche
            version = 0
        }
        synchro = true
        val vol = enVol
        when {
            vol != null && id == vol.first -> {
                enVol = null
                version = v
                val l = if (refus.isNotEmpty()) {
                    // Refusée (version périmée qui aurait vidé une liste) : l'ordre du serveur, puis le nôtre.
                    Log.w("jasette", "marques refusées par le serveur : union avec les siennes")
                    s.union(listes())
                } else {
                    // Acceptée : nos changements faits depuis, plus ce que le serveur a ajouté.
                    listes().union(s.moins(vol.second))
                }
                poser(l)
                aEnvoyer = l != s
            }
            // La diffusion d'un autre appareil a croisé notre trame : sa réponse tranchera.
            vol != null -> return
            // Le serveur n'a encore rien : il prend nos marques.
            v == 0L -> {
                version = 0
                aEnvoyer = !listes().vide()
            }
            // Rien de changé ici : le serveur est la source.
            !aEnvoyer && version in 1L..v -> {
                poser(s)
                version = v
            }
            // Changé ici pendant une coupure, et personne d'autre : la copie locale part.
            aEnvoyer && version == v -> {}
            // Première synchronisation avec ce serveur, ou changé des deux côtés : union.
            else -> {
                val l = s.union(listes())
                poser(l)
                version = v
                aEnvoyer = l != s
            }
        }
        if (Triple(listes(), version, aEnvoyer) != avant || serveurMarques != serveurAvant) garder()
        pousser()
    }
}
