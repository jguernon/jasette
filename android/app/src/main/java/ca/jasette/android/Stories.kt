package ca.jasette.android

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Handler
import android.os.Looper
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.setValue
import org.json.JSONObject

/**
 * Les stories des contacts (serveur/PROTOCOLE.md §5.15) : qui a une story
 * Instagram ou Facebook active, pour un cercle autour de son avatar. Jasette
 * n'affiche pas la story : toucher le cercle ouvre le lien (l'app officielle
 * le prend si elle est installée), et retient « ouverte » dans la marque
 * partagée `stories_ouvertes`. Jamais rien n'est marqué vu chez Meta.
 * Tout se passe sur le fil principal.
 */
object Stories {
    /** Une story active d'un contact (événement `stories`). */
    data class Story(
        val conv: String,
        val latest: Long,
        val until: Long,
        val seen: Boolean,
        val link: String,
        /** "instagram" ou "facebook". */
        val service: String,
    )

    /** Les stories reçues, par service puis par conversation (id préfixé). */
    private val parService = HashMap<Service, Map<String, Story>>()
    /** Toutes, par conversation (id préfixé) ; l'état observé par l'interface. */
    val parConv = mutableStateMapOf<String, Story>()
    /** Pourquoi un service ne donne rien (`indispo`), à montrer sous la case du réglage. */
    val indispo = mutableStateMapOf<Service, String>()

    /** L'horloge du cercle : avance chaque minute (une story échue disparaît toute seule). */
    var horloge by mutableLongStateOf(System.currentTimeMillis())
        private set
    private val principal = Handler(Looper.getMainLooper())
    private var enMarche = false
    private val tic = object : Runnable {
        override fun run() {
            horloge = System.currentTimeMillis()
            principal.postDelayed(this, 60_000L)
        }
    }

    fun demarrer() {
        if (enMarche) return
        enMarche = true
        principal.postDelayed(tic, 60_000L)
    }

    /** Un événement `stories` d'un pont (ids déjà préfixés). */
    fun recevoir(v: JSONObject, service: Service) {
        if (!Marques.stories || (service != Service.INSTAGRAM && service != Service.MESSENGER)) return
        val items = v.optJSONArray("items")
        val n = HashMap<String, Story>()
        if (items != null) for (i in 0 until items.length()) {
            val o = items.optJSONObject(i) ?: continue
            val conv = o.chaine("conv")
            val lien = o.chaine("link")
            if (conv.isEmpty() || !(lien.startsWith("https://www.instagram.com/") || lien.startsWith("https://www.facebook.com/"))) continue
            n[conv] = Story(
                conv, o.optLong("latest", 0), o.optLong("until", 0), o.optBoolean("seen"), lien,
                o.chaine("service").ifEmpty { if (service == Service.INSTAGRAM) "instagram" else "facebook" },
            )
        }
        val raison = v.chaine("indispo")
        if (raison.isNotEmpty()) indispo[service] = raison else indispo.remove(service)
        // La liste remplace la précédente de ce service.
        parService[service] = n
        reconstruire()
    }

    private fun reconstruire() {
        val tout = HashMap<String, Story>()
        for (m in parService.values) tout.putAll(m)
        parConv.keys.filter { it !in tout }.forEach { parConv.remove(it) }
        for ((k, s) in tout) if (parConv[k] != s) parConv[k] = s
    }

    /** Le service est dissocié : ses stories ne valent plus rien. */
    fun oublier(service: Service) {
        parService.remove(service)
        indispo.remove(service)
        reconstruire()
    }

    /** Tout recommence (autre serveur, ponts locaux). */
    fun vider() = regle(false)

    /** Le réglage change : désactivé, tout est oublié. */
    fun regle(oui: Boolean) {
        if (oui) return
        parService.clear()
        parConv.clear()
        indispo.clear()
    }

    /** La story de [id] à cercler, ou null (réglage, échéance, déjà vue, déjà ouverte). */
    fun active(id: String): Story? {
        if (!Marques.stories) return null
        val s = parConv[id] ?: return null
        return s.takeIf { cercleVisible(it.latest, it.until, it.seen, Marques.storiesOuvertes[id], horloge) }
    }

    /**
     * La story à cercler pour l'avatar de [conv] : la sienne, sinon celle d'une
     * autre conversation de sa personne (Instagram avant Facebook) ; [propre] :
     * la sienne seulement. Jamais pour un groupe.
     */
    fun pour(conv: Conv, propre: Boolean = false): Story? {
        if (conv.group || conv.large || !Marques.stories || parConv.isEmpty()) return null
        if (propre) return active(conv.id)
        val ids = Marques.personnes.firstOrNull { conv.id in it.convs }?.convs ?: listOf(conv.id)
        return premiere(ids)
    }

    /** La première story active parmi [ids], Instagram avant Facebook. */
    fun premiere(ids: List<String>): Story? {
        val a = ids.mapNotNull { active(it) }
        return a.firstOrNull { it.service == "instagram" } ?: a.firstOrNull()
    }

    /** Ouvre la story hors de Jasette, puis retient qu'elle a été ouverte (partout). */
    fun ouvrir(ctx: Context, s: Story) {
        runCatching {
            ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(s.link)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
        Marques.storyOuverte(s.conv, s.latest)
    }
}

/**
 * Le cercle se montre si la story n'est pas échue, pas déjà vue, et plus
 * récente que celle ouverte depuis Jasette ([ouverte], null : jamais). Une
 * heure inconnue (0, Facebook) : visible tant qu'elle n'a pas été ouverte.
 */
fun cercleVisible(latest: Long, until: Long, seen: Boolean, ouverte: Long?, maintenant: Long): Boolean =
    until > maintenant && !seen && (ouverte == null || latest > ouverte)
