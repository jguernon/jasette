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
 * Et les archives de Jasette : les conversations archivées par Jasette elle-même,
 * pour les services qui ne savent pas archiver (Messenger, Instagram, Signal,
 * X ; Google et WhatsApp archivent pour de vrai). Un nouveau message reçu les
 * ramène dans la boîte de réception ([desarchiverSiRecu]).
 *
 * En mode client du serveur Jasette, le serveur est la source : il garde les
 * quatre listes et les partage entre les appareils (serveur/PROTOCOLE.md,
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
    /** Les archives de Jasette (ids préfixés), dans l'ordre d'archivage. */
    val archives: SnapshotStateList<String> = mutableStateListOf()
    /**
     * La Liste privée (ids préfixés) : cachées partout dans l'app normale, montrées
     * seulement dans la vue Privées ([Prive]). Partagée par le serveur comme les
     * autres listes (`privees`) ; le code, lui, reste sur l'appareil.
     */
    val privees: SnapshotStateList<String> = mutableStateListOf()
    /**
     * Depuis quand (ms, horloge du téléphone) chaque id est dans [archives], ici :
     * seul un message reçu après cette heure la désarchive (pas une réaction
     * ni la mise à jour d'un vieux message). Propre à l'appareil, jamais envoyé.
     */
    private val archivesDepuis = HashMap<String, Long>()

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

    /**
     * La grille du Head : le nombre de colonnes (3 à 6, 4 par défaut) ; autant
     * de rangées qu'il faut pour tous les Head. Propre à l'appareil : jamais
     * envoyé au serveur (pas dans [listes]).
     */
    var headGrille by mutableStateOf("4")
        private set

    /** Les choix offerts dans Réglages → Apparence. */
    val GRILLES = listOf("3", "4", "5", "6")

    /** Le nombre de colonnes d'un choix ; une ancienne valeur (« auto », « 6x2 »…) devient 3 à 6. */
    fun colonnes(grille: String): Int =
        grille.substringBefore('x').toIntOrNull()?.coerceIn(3, 6) ?: 4

    /** commit() : écrit tout de suite, comme les listes (voir [garder]). */
    fun choisirHeadGrille(grille: String) {
        headGrille = colonnes(grille).toString()
        prefs?.edit()?.putString("head_grille", headGrille)?.commit()
    }

    /** La grosseur du texte des messages (1 = normal, 0,85 à 1,6). Propre à l'appareil. */
    var tailleMessages by mutableStateOf(1f)
        private set

    fun choisirTailleMessages(e: Float) {
        tailleMessages = e.coerceIn(0.85f, 1.6f)
        prefs?.edit()?.putFloat("taille_messages", tailleMessages)?.apply()
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
        headGrille = colonnes(p.getString("head_grille", "4") ?: "4").toString()
        tailleMessages = p.getFloat("taille_messages", 1f).coerceIn(0.85f, 1.6f)
        head.clear()
        head.addAll(lire(p, "head"))
        favoris.clear()
        favoris.addAll(lire(p, "favoris"))
        epingles.clear()
        epingles.addAll(lire(p, "epingles"))
        archives.clear()
        archives.addAll(lire(p, "archives"))
        privees.clear()
        privees.addAll(lire(p, "privees"))
        archivesDepuis.clear()
        try {
            val o = JSONObject(p.getString("archives_depuis", "{}") ?: "{}")
            for (k in o.keys()) archivesDepuis[k] = o.optLong(k)
        } catch (_: Exception) {}
        majDepuis()
    }

    /** Une heure d'archivage pour chaque archive (maintenant, si elle manque), aucune pour les autres. */
    private fun majDepuis() {
        val maintenant = System.currentTimeMillis()
        archivesDepuis.keys.retainAll(archives.toSet())
        for (id in archives) if ((archivesDepuis[id] ?: 0L) <= 0L) archivesDepuis[id] = maintenant
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
            ?.putString("archives", JSONArray(archives.toList()).toString())
            ?.putString("privees", JSONArray(privees.toList()).toString())
            ?.putString("archives_depuis", JSONObject(archivesDepuis.toMap()).toString())
            ?.putLong("version", version)
            ?.putBoolean("a_envoyer", aEnvoyer)
            ?.putString("serveur", serveurMarques)
            ?.commit()
    }

    fun estFavori(id: String) = id in favoris
    fun estEpinglee(id: String) = id in epingles
    fun estDansHead(id: String) = id in head
    fun estArchivee(id: String) = id in archives
    fun estPrivee(id: String) = id in privees

    /** Rend privée (à la fin de la liste) ou ne l'est plus ; rien si c'est déjà fait. */
    fun rendrePrivee(id: String, oui: Boolean) {
        if (oui == (id in privees)) return
        if (oui) privees.add(id) else privees.remove(id)
        changement()
        // Le partage direct : une privée n'y apparaît jamais.
        Raccourcis.privees()
    }

    /** Toutes les privées redeviennent normales (désactivation de la Liste privée). */
    fun remettrePrivees() {
        if (privees.isEmpty()) return
        privees.clear()
        changement()
        Raccourcis.privees()
    }

    /** Archive par Jasette (à la fin de la liste) ou désarchive ; rien si c'est déjà fait. */
    fun archiver(id: String, oui: Boolean) {
        if (oui == (id in archives)) return
        if (oui) archives.add(id) else archives.remove(id)
        majDepuis()
        changement()
    }

    /**
     * Un message reçu (pas envoyé par moi) dans [id], daté de [temps] (ms) :
     * s'il est plus récent que l'archivage, la conversation revient dans la
     * boîte de réception. Idempotent. En mode client, le serveur fait de même
     * et sa diffusion arrive d'habitude avant le message : rien à faire alors.
     */
    fun desarchiverSiRecu(id: String, temps: Long) {
        val depuis = archivesDepuis[id] ?: return
        if (id !in archives || temps <= depuis) return
        archives.remove(id)
        majDepuis()
        changement()
    }

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

    /** Une conversation supprimée n'est plus ni favorite, ni épinglée, ni archivée. */
    fun oublier(id: String) {
        val a = favoris.remove(id)
        val b = epingles.remove(id)
        val c = head.remove(id)
        val d = archives.remove(id)
        val e = privees.remove(id)
        if (d) majDepuis()
        if (a || b || c || d || e) changement()
    }

    // ---- La synchronisation avec le serveur Jasette (§5.5) ----

    /** Les quatre listes, figées (head, favoris, épinglées, archives). */
    private data class Listes(
        val head: List<String>,
        val favoris: List<String>,
        val epingles: List<String>,
        val archives: List<String>,
        val privees: List<String>,
    ) {
        fun vide() = head.isEmpty() && favoris.isEmpty() && epingles.isEmpty() && archives.isEmpty() && privees.isEmpty()

        /** Chaque liste de this, dans son ordre, puis les ids de [b] qui n'y sont pas. */
        fun union(b: Listes) = Listes(
            (head + b.head).filter { it.isNotEmpty() }.distinct(),
            (favoris + b.favoris).filter { it.isNotEmpty() }.distinct(),
            (epingles + b.epingles).filter { it.isNotEmpty() }.distinct(),
            (archives + b.archives).filter { it.isNotEmpty() }.distinct(),
            (privees + b.privees).filter { it.isNotEmpty() }.distinct(),
        )

        /** Les ids de this absents de [b], liste par liste. */
        fun moins(b: Listes) = Listes(
            head - b.head.toSet(), favoris - b.favoris.toSet(), epingles - b.epingles.toSet(),
            archives - b.archives.toSet(), privees - b.privees.toSet(),
        )
    }

    private fun listes() = Listes(head.toList(), favoris.toList(), epingles.toList(), archives.toList(), privees.toList())

    private fun poser(l: Listes) {
        if (head.toList() != l.head) { head.clear(); head.addAll(l.head) }
        if (favoris.toList() != l.favoris) { favoris.clear(); favoris.addAll(l.favoris) }
        if (epingles.toList() != l.epingles) { epingles.clear(); epingles.addAll(l.epingles) }
        if (archives.toList() != l.archives) { archives.clear(); archives.addAll(l.archives); majDepuis() }
        if (privees.toList() != l.privees) {
            privees.clear()
            privees.addAll(l.privees)
            Raccourcis.privees()
        }
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
        CacheMarques.synchroniser()
        pousser()
    }

    private fun pousser() {
        val envoi = envoyeur ?: return
        if (!synchro || enVol != null || !aEnvoyer) return
        val l = listes()
        prochain++
        enVol = prochain to l
        val corps = JSONObject().put("head", JSONArray(l.head)).put("favoris", JSONArray(l.favoris))
            .put("epingles", JSONArray(l.epingles)).put("archives", JSONArray(l.archives))
            .put("privees", JSONArray(l.privees))
            .put("version", version).put("id", prochain)
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
            // Un serveur d'avant les archives ne les envoie pas : elles restent
            // alors propres à cet appareil (le serveur est censé avoir les nôtres).
            if (m.has("archives")) m.optJSONArray("archives").chaines().distinct() else archives.toList(),
            // De même pour les privées (serveur d'avant `privees`).
            if (m.has("privees")) m.optJSONArray("privees").chaines().distinct() else privees.toList(),
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
        if (listes() != avant.first) CacheMarques.synchroniser()
        pousser()
    }
}
