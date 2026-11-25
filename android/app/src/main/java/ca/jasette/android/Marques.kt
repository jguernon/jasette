package ca.jasette.android

import android.content.Context
import android.content.SharedPreferences
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateList
import org.json.JSONArray

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

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
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

    private fun lire(p: SharedPreferences, cle: String): List<String> =
        try {
            JSONArray(p.getString(cle, "[]") ?: "[]").chaines().distinct()
        } catch (_: Exception) {
            emptyList()
        }

    private fun garder() {
        prefs?.edit()
            ?.putString("favoris", JSONArray(favoris.toList()).toString())
            ?.putString("epingles", JSONArray(epingles.toList()).toString())
            ?.putString("head", JSONArray(head.toList()).toString())
            ?.apply()
    }

    fun estFavori(id: String) = id in favoris
    fun estEpinglee(id: String) = id in epingles
    fun estDansHead(id: String) = id in head

    /** Ajoute au Head (à la fin) ou l'en retire. */
    fun basculerHead(id: String) {
        if (!head.remove(id)) head.add(id)
        garder()
    }

    /** Ajoute aux favoris (à la fin) ou les en retire. */
    fun basculerFavori(id: String) {
        if (!favoris.remove(id)) favoris.add(id)
        garder()
    }

    /** Épingle (à la fin des épinglées) ou désépingle. */
    fun basculerEpingle(id: String) {
        if (!epingles.remove(id)) epingles.add(id)
        garder()
    }

    /** Monte (-1) ou descend (+1) un favori d'un cran (comme `deplacer_favori`). */
    fun deplacerFavori(id: String, sens: Int) {
        val i = favoris.indexOf(id)
        val j = i + sens
        if (i < 0 || j < 0 || j >= favoris.size) return
        favoris[i] = favoris[j]
        favoris[j] = id
        garder()
    }

    /** Une conversation supprimée n'est plus ni favorite ni épinglée. */
    fun oublier(id: String) {
        val a = favoris.remove(id)
        val b = epingles.remove(id)
        val c = head.remove(id)
        if (a || b || c) garder()
    }
}
