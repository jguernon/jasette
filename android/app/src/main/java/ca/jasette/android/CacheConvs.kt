package ca.jasette.android

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.Executors

/**
 * Le cache local des conversations et des messages : de quoi tout montrer dès
 * le lancement, avant que les services soient connectés, et ouvrir une
 * conversation sans attendre sa réponse.
 *
 * Rangé dans les fichiers privés de l'app (`<files>/cache-convs/`) :
 * - `listes/<service>.json` : la dernière liste de conversations du service
 *   (boîte de réception et archives, [MAX_CONVS] chacune au plus) et les
 *   photos déjà téléchargées de ces conversations ;
 * - `fils/<service>-<empreinte>.json` : les [MAX_MSGS] derniers messages d'une
 *   conversation, et les fichiers déjà téléchargés de leurs pièces jointes ;
 *   [MAX_CONVS] conversations par service au plus (les plus récentes).
 *
 * [Depot] signale ce qui change ([noterListes], [noterFil]) sur le fil
 * principal ; l'écriture est groupée ([DELAI_MS] après le premier changement)
 * et se fait sur un fil à part : jamais une écriture par événement. Les
 * photos et pièces jointes elles-mêmes ne sont pas copiées : elles sont déjà
 * dans des dossiers durables (`<files>/media` des ponts locaux,
 * `<files>/distant/<hôte>` des copies d'un serveur), jamais vidés au démarrage.
 */
object CacheConvs {
    private const val MAX_CONVS = 200
    private const val MAX_MSGS = 60
    private const val DELAI_MS = 2500L
    private const val ETIQUETTE = "jasette"

    private val principal = Handler(Looper.getMainLooper())
    /** Un seul fil d'écriture : les écritures et les effacements gardent leur ordre. */
    private val ecrivain = Executors.newSingleThreadExecutor { r -> Thread(r, "cache-convs").apply { isDaemon = true } }
    private val verrou = Any()
    private var dossier: File? = null

    /** Incrémenté à chaque effacement complet : une écriture ou une lecture d'avant est abandonnée. */
    @Volatile private var epoque = 0
    /** Pareil, par service (effacement d'un seul service). */
    private val epoquesService = IntArray(Service.entries.size)

    // Sur le fil principal seulement.
    private var listesSales = false
    private val filsSales = HashSet<String>()
    private var prevu = false
    private val ecriture = Runnable { ecrire() }
    /** Les fils du disque ont-ils été relus (dans ce processus) ? Avant, on n'élague rien. */
    private var filsCharges = false
    private var lectureFils = false

    private fun racine(ctx: Context) = File(ctx.applicationContext.filesDir, "cache-convs")
    private fun listes() = dossier?.let { File(it, "listes") }
    private fun fils() = dossier?.let { File(it, "fils") }

    fun initialiser(ctx: Context) {
        if (dossier == null) dossier = racine(ctx)
    }

    // ---- Lecture (au démarrage) ----

    /** Ce qu'on a gardé de la liste d'un service. */
    class Listes(val convs: List<Conv>, val archives: List<Conv>, val avatars: Map<String, String>)

    /** Les services dont une liste est gardée (pour les montrer avant que le serveur les annonce). */
    fun servicesGardes(): List<Service> {
        val d = listes() ?: return emptyList()
        return Service.entries.filter { File(d, "${it.cle}.json").isFile }
    }

    /** La liste gardée d'un service, ou null. Petite : lue sur le fil principal, avant la connexion. */
    fun lireListes(service: Service): Listes? {
        val f = File(listes() ?: return null, "${service.cle}.json")
        if (!f.isFile) return null
        return try {
            val o = JSONObject(f.readText())
            fun convs(cle: String): List<Conv> {
                val a = o.optJSONArray(cle) ?: return emptyList()
                return (0 until a.length()).mapNotNull { a.optJSONObject(it)?.let { c -> Conv.de(c) } }
                    .filter { it.id.isNotEmpty() && Service.de(it.id) == service }
                    .distinctBy { it.id }
            }
            val av = HashMap<String, String>()
            o.optJSONObject("avatars")?.let { a ->
                for (k in a.keys()) {
                    val p = a.chaine(k)
                    if (p.isNotEmpty() && File(p).isFile) av[k] = p
                }
            }
            Listes(convs("convs"), convs("archives"), av)
        } catch (e: Exception) {
            Log.w(ETIQUETTE, "cache des conversations (${service.court}) illisible : ignoré", e)
            null
        }
    }

    /** Ce qu'on a gardé d'une conversation. */
    class Fil(val conv: String, val messages: List<Msg>, val plus: Boolean, val medias: Map<String, String>)

    /**
     * Relit les fils gardés des [services] sur un fil à part, puis les passe à
     * [recu] sur le fil principal (une seule fois par processus).
     */
    fun lireFils(services: Collection<Service>, recu: (List<Fil>) -> Unit) {
        if (filsCharges || lectureFils) return
        val d = fils() ?: return
        lectureFils = true
        val e = epoque
        val epoques = services.associateWith { epoquesService[it.ordinal] }
        val cles = services.map { it.cle + "-" }
        ecrivain.execute {
            val lus = ArrayList<Fil>()
            for (f in d.listFiles().orEmpty()) {
                if (!f.name.endsWith(".json") || cles.none { f.name.startsWith(it) }) continue
                try {
                    val o = JSONObject(f.readText())
                    val conv = o.chaine("conv")
                    val a = o.optJSONArray("items") ?: continue
                    val msgs = (0 until a.length()).mapNotNull { a.optJSONObject(it)?.let { m -> Msg.de(m) } }
                        .filter { it.id.isNotEmpty() && !it.local }
                    if (conv.isEmpty() || msgs.isEmpty()) continue
                    val med = HashMap<String, String>()
                    o.optJSONObject("medias")?.let { m ->
                        for (k in m.keys()) {
                            val p = m.chaine(k)
                            if (p.isNotEmpty() && File(p).isFile) med[k] = p
                        }
                    }
                    lus.add(Fil(conv, msgs, o.optBoolean("plus", true), med))
                } catch (ex: Exception) {
                    Log.w(ETIQUETTE, "un fil du cache est illisible : effacé", ex)
                    f.delete()
                }
            }
            principal.post {
                lectureFils = false
                filsCharges = true
                if (e != epoque) return@post
                // Un service effacé pendant la lecture : ses fils ne reviennent pas.
                recu(lus.filter { epoques[Service.de(it.conv)] == epoquesService[Service.de(it.conv).ordinal] })
            }
        }
    }

    // ---- Écriture (groupée) ----

    /** Les listes de conversations ont changé. */
    fun noterListes() {
        if (dossier == null) return
        listesSales = true
        prevoir()
    }

    /** Les messages d'une conversation ont changé. */
    fun noterFil(conv: String) {
        if (dossier == null || conv.isEmpty()) return
        filsSales.add(conv)
        prevoir()
    }

    private fun prevoir() {
        if (prevu) return
        prevu = true
        principal.postDelayed(ecriture, DELAI_MS)
    }

    /** Écrit tout de suite ce qui attend (l'app passe en arrière-plan ou s'arrête). */
    fun ecrireMaintenant() {
        if (prevu) ecrire()
    }

    private fun empreinte(id: String): String =
        MessageDigest.getInstance("SHA-1").digest(id.toByteArray()).joinToString("") { "%02x".format(it) }

    private fun nomFil(conv: String) = "${Service.de(conv).cle}-${empreinte(conv)}.json"

    /**
     * Prend sur le fil principal une copie de ce qui a changé (listes et
     * messages sont immuables : copier les références suffit), puis écrit sur
     * le fil d'écriture.
     */
    private fun ecrire() {
        principal.removeCallbacks(ecriture)
        prevu = false
        val lst = listes() ?: return
        val fl = fils() ?: return
        val e = epoque

        // Les listes : seulement les services montrés, et jamais une liste vide
        // (un service coupé ou pas encore prêt garde la sienne ; on n'efface
        // qu'à la déconnexion, par effacer()).
        val listesAEcrire = ArrayList<Pair<Service, JSONObject?>>()
        if (listesSales) {
            listesSales = false
            val convs = Depot.convs.toList()
            val archives = Depot.archives.toList()
            for (s in Depot.services) {
                val c = convs.filter { it.service == s }.take(MAX_CONVS)
                val a = archives.filter { it.service == s }.take(MAX_CONVS)
                if (c.isEmpty() && a.isEmpty()) continue
                // Avec l'icône des espaces parents (§5.13), gardée sous l'id préfixé du parent.
                val cles = (c + a).map { it.id } + c.filter { it.parent.isNotEmpty() }.map { s.prefixe + it.parent }
                val av = cles.distinct().mapNotNull { k -> Depot.avatars[k]?.let { k to it } }
                listesAEcrire.add(s to JSONObject().apply {
                    put("convs", JSONArray(c.map { it.json() }))
                    put("archives", JSONArray(a.map { it.json() }))
                    put("avatars", JSONObject().apply { av.forEach { (k, v) -> put(k, v) } })
                })
            }
        }

        // Les fils : les MAX_MSGS derniers messages confirmés de chaque conversation changée.
        val gardes = convsGardees()
        val filsAEcrire = ArrayList<Pair<String, JSONObject?>>()
        for (conv in filsSales) {
            val msgs = Depot.messages[conv].orEmpty().filter { !it.local }.takeLast(MAX_MSGS)
            if (msgs.isEmpty() || (gardes != null && conv !in gardes)) {
                filsAEcrire.add(nomFil(conv) to null)
                continue
            }
            val med = JSONObject()
            for (m in msgs) for (p in m.attachments) {
                Depot.medias[p.mediaId]?.let { if (!p.mediaId.startsWith("local:")) med.put(p.mediaId, it) }
            }
            // « Plus anciens » : vrai aussi si on a coupé des messages plus vieux.
            val plus = Depot.plusAnciens[conv] != false || Depot.messages[conv].orEmpty().count { !it.local } > msgs.size
            filsAEcrire.add(nomFil(conv) to JSONObject().put("conv", conv).put("plus", plus).put("medias", med)
                .put("items", JSONArray(msgs.map { it.json() })))
        }
        filsSales.clear()
        // Les fils sortis des MAX_CONVS plus récents de leur service : effacés.
        val aEffacer = if (gardes == null) emptyList() else {
            Depot.messages.keys.filter { it !in gardes }.map { nomFil(it) }
        }

        ecrivain.execute {
            synchronized(verrou) {
                if (e != epoque) return@execute
                try {
                    lst.mkdirs()
                    fl.mkdirs()
                    for ((s, o) in listesAEcrire) ecrireFichier(File(lst, "${s.cle}.json"), o!!.toString())
                    for ((nom, o) in filsAEcrire) {
                        if (o == null) File(fl, nom).delete() else ecrireFichier(File(fl, nom), o.toString())
                    }
                    for (nom in aEffacer) File(fl, nom).delete()
                } catch (ex: Exception) {
                    Log.w(ETIQUETTE, "cache des conversations : écriture impossible", ex)
                }
            }
        }
    }

    /**
     * Les conversations dont on garde les messages : les [MAX_CONVS] plus
     * récentes (dernier message) de chaque service. Null tant que les fils du
     * disque n'ont pas été relus (on ne connaît pas encore tout).
     */
    private fun convsGardees(): Set<String>? {
        if (!filsCharges) return null
        val parService = Depot.messages.entries
            .mapNotNull { (k, v) -> v.lastOrNull { !it.local }?.let { k to it.time } }
            .groupBy { Service.de(it.first) }
        return parService.values.flatMap { l -> l.sortedByDescending { it.second }.take(MAX_CONVS).map { it.first } }.toSet()
    }

    /** Fichier temporaire puis renommage : jamais un cache à moitié écrit. */
    private fun ecrireFichier(f: File, texte: String) {
        val tmp = File(f.parentFile, f.name + ".tmp")
        tmp.writeText(texte)
        if (!tmp.renameTo(f)) {
            f.delete()
            if (!tmp.renameTo(f)) tmp.delete()
        }
    }

    // ---- Effacement ----

    /** Un service est déconnecté (« Déconnecter », unpair) : son cache part. */
    fun effacer(service: Service) {
        val d = dossier ?: return
        epoquesService[service.ordinal]++
        val prefixe = service.cle + "-"
        filsSales.removeAll { Service.de(it) == service }
        ecrivain.execute {
            synchronized(verrou) {
                File(d, "listes/${service.cle}.json").delete()
                File(d, "fils").listFiles()?.forEach { if (it.name.startsWith(prefixe)) it.delete() }
            }
        }
    }

    /** Changement de mode (autre serveur, ou plus de serveur) : tout le cache part. */
    fun toutEffacer() {
        val d = dossier ?: return
        principal.removeCallbacks(ecriture)
        prevu = false
        listesSales = false
        filsSales.clear()
        filsCharges = false
        synchronized(verrou) {
            epoque++
            d.deleteRecursively()
        }
    }
}
