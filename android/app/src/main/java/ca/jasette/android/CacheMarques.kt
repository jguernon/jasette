package ca.jasette.android

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.compose.runtime.mutableStateMapOf
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest

/**
 * Le cache local des conversations marquées (Head, favoris, épingles) : de
 * quoi les montrer quand leur service est déconnecté, pas encore prêt, ou
 * retiré par le serveur (absent de `auth ok`). Sans lui, ces marques
 * restaient dans [Marques] mais disparaissaient de l'écran.
 *
 * Pour chaque id marqué : le nom, le service (déduit de l'id), la photo
 * (copiée dans les fichiers privés de l'app : l'originale, celle d'un pont
 * ou du cache d'un serveur, peut être effacée), la date et l'aperçu.
 * Mis à jour chaque fois que la conversation arrive de son service ([noter]),
 * gardé dans des préférences privées avec commit(). Un id qui n'est plus
 * marqué en sort ([synchroniser]). Tout se passe sur le fil principal.
 */
object CacheMarques {
    private const val PREFS = "cache_marques"
    private const val CLE = "fiches"
    private var prefs: SharedPreferences? = null
    private var dossier: File? = null

    /** Ce qu'on garde d'une conversation marquée. */
    data class Fiche(
        val id: String,
        val nom: String,
        val apercu: String,
        val deMoi: Boolean,
        val temps: Long,
        val groupe: Boolean,
        val couleur: String,
        val numero: String,
        /** La copie privée de la photo, ou "". */
        val avatar: String,
        /** Le chemin d'origine de la photo copiée (pour ne la recopier que si elle change). */
        val avatarSource: String,
    ) {
        fun json(): JSONObject = JSONObject().put("id", id).put("nom", nom).put("apercu", apercu)
            .put("de_moi", deMoi).put("temps", temps).put("groupe", groupe).put("couleur", couleur)
            .put("numero", numero).put("avatar", avatar).put("avatar_source", avatarSource)

        /** Une conversation à montrer à la place de la vraie (lecture seule, jamais non lue). */
        fun conv() = Conv(
            id = id, name = nom, preview = apercu, fromMe = deMoi, time = temps, unread = false,
            group = groupe, rcs = false, color = couleur, members = emptyList(), readOnly = false,
            archived = false, e2ee = false, number = numero,
        )

        companion object {
            fun de(o: JSONObject) = Fiche(
                id = o.chaine("id"), nom = o.chaine("nom"), apercu = o.chaine("apercu"),
                deMoi = o.optBoolean("de_moi"), temps = o.optLong("temps"), groupe = o.optBoolean("groupe"),
                couleur = o.chaine("couleur"), numero = o.chaine("numero"), avatar = o.chaine("avatar"),
                avatarSource = o.chaine("avatar_source"),
            )
        }
    }

    /** id → fiche, un état Compose : l'écran suit les changements. */
    val fiches = mutableStateMapOf<String, Fiche>()

    fun charger(ctx: Context) {
        val app = ctx.applicationContext
        val p = app.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
        dossier = File(app.filesDir, "marques-avatars")
        fiches.clear()
        try {
            val o = JSONObject(p.getString(CLE, "{}") ?: "{}")
            for (k in o.keys()) {
                val f = Fiche.de(o.getJSONObject(k))
                if (f.id.isNotEmpty()) fiches[f.id] = f
            }
        } catch (e: Exception) {
            Log.w("jasette", "cache des marques illisible : ignoré", e)
        }
        synchroniser()
    }

    /** commit() : écrit tout de suite (comme les marques elles-mêmes). */
    private fun garder() {
        val o = JSONObject()
        for ((k, f) in fiches) o.put(k, f.json())
        prefs?.edit()?.putString(CLE, o.toString())?.commit()
    }

    private fun marque(id: String) = id in Marques.head || id in Marques.favoris || id in Marques.epingles

    /** Tous les ids marqués, sans doublon. */
    fun marques(): List<String> = (Marques.head + Marques.favoris + Marques.epingles).distinct()

    /** La conversation de remplacement d'un id marqué, ou null s'il n'a jamais été vu. */
    fun conv(id: String): Conv? = fiches[id]?.conv()

    /** La photo gardée d'un id, si sa copie existe encore. */
    fun avatar(id: String): String? = fiches[id]?.avatar?.takeIf { it.isNotEmpty() && File(it).isFile }

    /**
     * Les marques ont changé (ici ou par le serveur) : les ids qui ne sont plus
     * marqués sortent du cache (et leur photo est effacée) ; ceux qui viennent
     * de l'être et dont la conversation est chargée y entrent.
     */
    fun synchroniser() {
        var change = false
        for (id in fiches.keys.toList()) {
            if (!marque(id)) {
                fiches.remove(id)?.avatar?.takeIf { it.isNotEmpty() }?.let { File(it).delete() }
                change = true
            }
        }
        for (id in marques()) {
            val c = Depot.conv(id) ?: continue
            if (maj(c)) change = true
        }
        if (change) garder()
    }

    /** Des conversations viennent d'arriver (ou de changer) : celles qui sont marquées sont notées. */
    fun noter(convs: Collection<Conv>) {
        var change = false
        for (c in convs) if (marque(c.id) && maj(c)) change = true
        if (change) garder()
    }

    /** La photo d'une conversation vient d'arriver. */
    fun noterAvatar(id: String) {
        if (!marque(id)) return
        val c = Depot.conv(id) ?: return
        if (maj(c)) garder()
    }

    /** Met la fiche de [c] à jour ; vrai si elle a changé. */
    private fun maj(c: Conv): Boolean {
        val avant = fiches[c.id]
        var avatar = avant?.avatar.orEmpty()
        var source = avant?.avatarSource.orEmpty()
        val photo = Depot.avatars[c.id]
        if (!photo.isNullOrEmpty() && (photo != source || avatar.isEmpty() || !File(avatar).isFile)) {
            copier(c.id, photo)?.let {
                avatar = it
                source = photo
            }
        }
        val f = Fiche(
            id = c.id,
            // Un nom vide (conversation pas encore nommée) ne remplace pas un nom connu.
            nom = c.name.ifEmpty { c.members.joinToString(", ") }.ifEmpty { avant?.nom.orEmpty() },
            apercu = c.preview, deMoi = c.fromMe,
            temps = if (c.time > 0) c.time else avant?.temps ?: 0L,
            groupe = c.group, couleur = c.color, numero = c.number.ifEmpty { avant?.numero.orEmpty() },
            avatar = avatar, avatarSource = source,
        )
        if (f == avant) return false
        fiches[c.id] = f
        return true
    }

    /** Copie la photo [chemin] dans les fichiers privés ; le chemin de la copie, ou null. */
    private fun copier(id: String, chemin: String): String? {
        val d = dossier ?: return null
        return try {
            val src = File(chemin)
            if (!src.isFile) return null
            d.mkdirs()
            val dest = File(d, empreinte(id) + ".img")
            val tmp = File(d, empreinte(id) + ".tmp")
            src.copyTo(tmp, overwrite = true)
            if (!tmp.renameTo(dest)) {
                tmp.copyTo(dest, overwrite = true)
                tmp.delete()
            }
            // Le décodeur d'images garde ses images par chemin : la nouvelle photo doit être relue.
            ca.jasette.android.ui.oublierImage(dest.path)
            dest.path
        } catch (e: Exception) {
            Log.w("jasette", "photo d'une marque : copie impossible", e)
            null
        }
    }

    private fun empreinte(id: String): String =
        MessageDigest.getInstance("SHA-1").digest(id.toByteArray()).joinToString("") { "%02x".format(it) }
}
