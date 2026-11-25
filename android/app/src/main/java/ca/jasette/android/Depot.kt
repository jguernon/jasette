package ca.jasette.android

import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import org.json.JSONObject
import java.io.File

/**
 * L'état de Jasette, partagé par le service et l'interface : les deux ponts,
 * les conversations des deux comptes mélangées, les messages, les avatars.
 *
 * Tout l'état vit dans des « snapshots » Compose et n'est modifié que sur le
 * fil principal : les nouvelles des ponts y sont renvoyées par [principal].
 * C'est l'équivalent de `nouvelle()` / `nouvelle_meta()` de src/main.rs.
 */
object Depot {
    private val principal = Handler(Looper.getMainLooper())
    private lateinit var appli: Context

    /** Un pont par service : Google, puis le pont Meta lancé une fois par service Meta. */
    private val ponts = HashMap<Service, Pont>()
    private var enMarche = false

    // ---- Ce que l'interface affiche ----

    /** Les conversations (boîte de réception) des deux comptes, les plus récentes d'abord. */
    val convs = mutableStateListOf<Conv>()
    val messages = mutableStateMapOf<String, List<Msg>>()
    /** Y a-t-il des messages plus anciens à charger ? */
    val plusAnciens = mutableStateMapOf<String, Boolean>()
    /** conv → photo (fichier local). */
    val avatars = mutableStateMapOf<String, String>()
    /** media_id → fichier téléchargé. */
    val medias = mutableStateMapOf<String, String>()
    val echecs = mutableStateMapOf<String, Boolean>()
    private val demandes = HashSet<String>()

    /** L'état de chaque compte. */
    val etats = mutableStateMapOf<Service, Etat>().apply { Service.entries.forEach { put(it, Etat.DEMARRAGE) } }
    /** Le nom du compte : courriel Google, nom Messenger ou Instagram. */
    val noms = mutableStateMapOf<Service, String>()
    /** L'emoji à toucher sur le téléphone pendant l'association Google. */
    var emoji by mutableStateOf<String?>(null)
    /** Messenger a un historique chiffré à restaurer avec le NIP. */
    var besoinNip by mutableStateOf(false)
    var erreurNip by mutableStateOf<String?>(null)
    var erreur by mutableStateOf<String?>(null)
    /** La conversation dont on attend les messages. */
    var chargement by mutableStateOf<String?>(null)

    /** L'interface est-elle à l'écran, et quelle conversation est ouverte ? (notifications) */
    var visible = false
    var ouverte: String? = null
    /** Une conversation à ouvrir (notification touchée). */
    var aOuvrir by mutableStateOf<String?>(null)

    /** Le service affiche une notification pour un nouveau message. */
    var notifier: ((Msg, Conv?) -> Unit)? = null

    private var envoiSeq = 0

    // ---- Les ponts ----

    /** Lance les deux ponts (une seule fois). */
    fun demarrer(ctx: Context) {
        if (enMarche) return
        appli = ctx.applicationContext
        enMarche = true
        Service.entries.forEach { lancer(it) }
    }

    fun etat(service: Service): Etat = etats[service] ?: Etat.DEMARRAGE

    fun arreter() {
        enMarche = false
        ponts.values.forEach { it.arreter() }
        ponts.clear()
    }

    /** Les variables d'environnement que les ponts lisent déjà. */
    private fun environnement(): Map<String, String> {
        val donnees = File(appli.filesDir, "jasette").apply { mkdirs() }
        val media = File(appli.filesDir, "media").apply { mkdirs() }
        val tmp = File(appli.cacheDir, "pont").apply { mkdirs() }
        return mapOf(
            "JASETTE_DATA" to donnees.path,
            "JASETTE_MEDIA" to media.path,
            "HOME" to appli.filesDir.path,
            "TMPDIR" to tmp.path,
            "SSL_CERT_DIR" to "/system/etc/security/cacerts",
        )
    }

    private fun executable(nom: String) = File(appli.applicationInfo.nativeLibraryDir, nom)

    /**
     * Lance le pont d'un service. Le pont Meta sert Messenger ou Instagram selon
     * JASETTE_META_SERVICE, chacun avec son dossier (<données>/messenger ou /instagram).
     */
    private fun lancer(service: Service) {
        try {
            ponts[service] = if (service == Service.GOOGLE) {
                Pont("google", executable("libjasette-pont.so"), environnement()) { v ->
                    principal.post { nouvelle(v, Service.GOOGLE) }
                }
            } else {
                val env = environnement() + ("JASETTE_META_SERVICE" to service.cle)
                Pont(service.cle, executable("libjasette-meta.so"), env) { v ->
                    prefixerMeta(v, service)
                    principal.post { nouvelleMeta(v, service) }
                }
            }
        } catch (e: Exception) {
            Log.e("jasette", "pont ${service.court}", e)
            erreur = "Le pont ${service.court} ne démarre pas : ${e.message}"
            etats[service] = Etat.ARRETE
        }
    }

    /** Une commande, envoyée au bon pont selon le préfixe de ses ids. */
    fun commande(v: JSONObject) {
        val service = deprefixerMeta(v) ?: Service.GOOGLE
        ponts[service]?.envoyer(v)
    }

    fun commande(service: Service, v: JSONObject) {
        ponts[service]?.envoyer(v)
    }

    // ---- Les comptes ----

    /** Les cookies de la fenêtre de connexion Google : le téléphone montrera des emojis. */
    fun connecterGoogle(cookies: Map<String, String>) {
        erreur = null
        commande(Service.GOOGLE, JSONObject().put("cmd", "google").put("cookies", JSONObject(cookies)))
    }

    /** Les cookies de messenger.com, facebook.com ou instagram.com. */
    fun connecterMeta(service: Service, cookies: Map<String, String>, plateforme: String) {
        erreur = null
        commande(
            service,
            JSONObject().put("cmd", "login").put("cookies", JSONObject(cookies)).put("platform", plateforme),
        )
    }

    fun deconnecter(service: Service) =
        commande(service, JSONObject().put("cmd", if (service == Service.GOOGLE) "unpair" else "logout"))

    /** Le NIP Messenger (6 chiffres) ; une chaîne vide refuse la restauration. */
    fun envoyerNip(nip: String) {
        erreurNip = null
        commande(Service.MESSENGER, JSONObject().put("cmd", "history_pin").put("pin", nip))
    }

    // ---- Les nouvelles ----

    private fun nouvelleMeta(v: JSONObject, service: Service) {
        when (v.chaine("ev")) {
            "state" -> {
                val avant = etat(service)
                val apres = Etat.de(v.chaine("state"), avant)
                etats[service] = apres
                if (apres == Etat.PRET && avant != Etat.PRET) {
                    commande(service, JSONObject().put("cmd", "conversations").put("count", 50))
                }
                if (apres == Etat.NON_ASSOCIE) oublier(service)
            }
            "paired" -> etats[service] = Etat.CONNEXION
            "account" -> v.chaine("name").let { if (it.isNotEmpty()) noms[service] = it else noms.remove(service) }
            "avatar_moi" -> {}
            "history_pin" -> when {
                v.optBoolean("needed") -> besoinNip = true
                v.optBoolean("ok") -> {
                    besoinNip = false
                    erreurNip = null
                }
                v.optBoolean("declined") -> besoinNip = false
                v.chaine("text").isNotEmpty() -> erreurNip = v.chaine("text")
            }
            "hello", "e2ee", "typing", "search" -> {}
            "error" -> {
                val texte = v.chaine("text")
                if (texte.isNotEmpty()) erreur = "${service.court} : $texte"
            }
            "pont_ferme" -> pontFerme(service)
            else -> nouvelle(v, service)
        }
    }

    private fun nouvelle(v: JSONObject, service: Service) {
        when (v.chaine("ev")) {
            "state" -> {
                etats[Service.GOOGLE] = Etat.de(v.chaine("state"), etat(Service.GOOGLE))
                when (etat(Service.GOOGLE)) {
                    Etat.NON_ASSOCIE -> {
                        emoji = null
                        oublier(Service.GOOGLE)
                    }
                    Etat.PRET -> emoji = null
                    else -> {}
                }
            }
            "account" -> v.chaine("email").let { if (it.isNotEmpty()) noms[Service.GOOGLE] = it }
            "emoji" -> emoji = v.chaine("emoji").ifEmpty { null }
            "paired" -> {
                emoji = null
                etats[Service.GOOGLE] = Etat.CONNEXION
            }
            "conversations" -> {
                // Les archives ne sont pas (encore) affichées sur Android.
                if (v.chaine("folder") == "archive") return
                val items = v.optJSONArray("items") ?: return
                erreur = null
                val vues = HashSet<String>()
                val nouvelles = (0 until items.length())
                    .map { Conv.de(items.getJSONObject(it)) }
                    .filter { vues.add(it.id) }
                // Chaque pont ne remplace que ses propres conversations.
                val gardees = convs.filter { it.service != service }
                remplacerConvs(gardees + nouvelles)
            }
            "conversation" -> {
                val c = Conv.de(v.optJSONObject("item") ?: return)
                val autres = convs.filter { it.id != c.id }
                remplacerConvs(if (c.archived) autres else autres + c)
                if (v.optBoolean("open")) aOuvrir = c.id
            }
            "avatar" -> {
                val conv = v.chaine("conv")
                val path = v.chaine("path")
                if (conv.isNotEmpty() && path.isNotEmpty()) avatars[conv] = path
            }
            "avatars_groupe" -> {
                // Pas de mosaïque (encore) : la photo du premier membre qui en a une.
                val conv = v.chaine("conv")
                val membres = v.optJSONArray("members") ?: return
                if (avatars.containsKey(conv)) return
                for (i in 0 until membres.length()) {
                    val path = membres.getJSONObject(i).chaine("path")
                    if (path.isNotEmpty()) {
                        avatars[conv] = path
                        break
                    }
                }
            }
            "avatar_moi" -> {}
            "media" -> {
                val id = v.chaine("media_id")
                demandes.remove(id)
                val path = v.chaine("path")
                if (path.isNotEmpty()) medias[id] = path else echecs[id] = true
            }
            "messages" -> {
                val conv = v.chaine("conv")
                val arr = v.optJSONArray("items")
                val items = (0 until (arr?.length() ?: 0)).map { Msg.de(arr!!.getJSONObject(it)) }
                val older = v.optBoolean("older")
                telechargerImages(items)
                val liste = messages[conv].orEmpty()
                messages[conv] = if (older) {
                    items.filter { m -> liste.none { it.id == m.id } } + liste
                } else {
                    // Garde les envois locaux pas encore confirmés.
                    val attente = liste.filter { m -> m.local && items.none { it.tmpId == m.tmpId } }
                    items + attente
                }
                plusAnciens[conv] = v.optBoolean("more")
                if (chargement == conv) chargement = null
                if (!older && visible && ouverte == conv) marquerLu(conv)
            }
            "message" -> {
                val m = Msg.de(v.optJSONObject("item") ?: return)
                recevoirMessage(m)
            }
            "sent" -> {
                if (!v.optBoolean("ok")) {
                    val tmp = v.chaine("tmp_id")
                    for ((conv, liste) in messages.toMap()) {
                        if (liste.any { it.tmpId == tmp }) {
                            messages[conv] = liste.map { if (it.tmpId == tmp) it.copy(status = "ÉCHEC") else it }
                        }
                    }
                }
            }
            "error" -> {
                val texte = v.chaine("text")
                if (texte.isNotEmpty()) erreur = texte
                if (v.chaine("where") == "messages") chargement = null
            }
            "pont_ferme" -> pontFerme(service)
            else -> {}
        }
    }

    private fun recevoirMessage(m: Msg) {
        telechargerImages(listOf(m))
        val ouverteIci = ouverte == m.conv && visible
        val liste = messages[m.conv].orEmpty().toMutableList()
        val i = liste.indexOfFirst { it.id == m.id || (m.tmpId.isNotEmpty() && it.tmpId == m.tmpId) }
        val nouveau: Boolean
        if (i >= 0) {
            nouveau = liste[i].local && !m.fromMe
            liste[i] = m
        } else {
            nouveau = true
            liste.add(m)
            liste.sortBy { it.time }
        }
        messages[m.conv] = liste

        val conv = convs.firstOrNull { it.id == m.conv }
        if (conv != null) {
            var c = conv
            if (nouveau || c.time <= m.time) c = c.copy(preview = m.apercu(), fromMe = m.fromMe)
            c = c.copy(time = maxOf(c.time, m.time))
            if (nouveau && !m.fromMe && !ouverteIci) c = c.copy(unread = true)
            remplacerConvs(convs.map { if (it.id == c.id) c else it })
        }
        if (nouveau && !m.fromMe) {
            if (ouverteIci) {
                commande(JSONObject().put("cmd", "read").put("conv", m.conv).put("msg", m.id))
            } else {
                notifier?.invoke(m, conv)
            }
        }
    }

    private fun remplacerConvs(liste: List<Conv>) {
        val triee = liste.sortedByDescending { it.time }
        convs.clear()
        convs.addAll(triee)
    }

    /** Un compte déconnecté : ses conversations disparaissent de la liste. */
    private fun oublier(service: Service) {
        if (convs.any { it.service == service }) remplacerConvs(convs.filter { it.service != service })
    }

    /** Un pont s'est arrêté tout seul : on le relance après quelques secondes. */
    private fun pontFerme(service: Service) {
        if (!enMarche) return
        etats[service] = Etat.ARRETE
        principal.postDelayed({
            if (enMarche && ponts[service]?.vivant() != true) lancer(service)
        }, 5000)
    }

    // ---- Les actions de l'interface ----

    fun ouvrir(id: String) {
        ouverte = id
        if (!messages.containsKey(id)) {
            chargement = id
            commande(JSONObject().put("cmd", "messages").put("conv", id).put("count", 60))
        } else {
            marquerLu(id)
        }
    }

    fun plusVieux(id: String) {
        val premier = messages[id]?.firstOrNull { !it.local } ?: return
        if (chargement == id) return
        chargement = id
        commande(
            JSONObject().put("cmd", "messages").put("conv", id).put("count", 60)
                .put("before", premier.id).put("before_time", premier.time),
        )
    }

    fun marquerLu(id: String) {
        val dernier = messages[id]?.lastOrNull { !it.local }
        if (dernier != null) commande(JSONObject().put("cmd", "read").put("conv", id).put("msg", dernier.id))
        if (convs.any { it.id == id && it.unread }) {
            remplacerConvs(convs.map { if (it.id == id) it.copy(unread = false) else it })
        }
    }

    fun pret(service: Service) = etat(service) == Etat.PRET

    fun envoyer(conv: String, texte: String) {
        val t = texte.trim()
        if (t.isEmpty()) return
        envoiSeq++
        val maintenant = System.currentTimeMillis()
        val tmp = "jasette_${maintenant}_$envoiSeq"
        val local = Msg(
            id = tmp, conv = conv, text = t, media = "", fromMe = true, sender = "", time = maintenant,
            status = "ENVOI", kind = "", tmpId = tmp, attachments = emptyList(), reactions = emptyList(),
        )
        messages[conv] = messages[conv].orEmpty() + local
        commande(JSONObject().put("cmd", "send").put("conv", conv).put("text", t).put("tmp_id", tmp))
    }

    private fun telechargerImages(msgs: List<Msg>) {
        for (m in msgs) for (p in m.attachments) {
            if (p.estImage) telecharger(p)
        }
    }

    fun telecharger(p: Piece) {
        if (p.mediaId.isEmpty() || medias.containsKey(p.mediaId) || echecs.containsKey(p.mediaId)) return
        if (!demandes.add(p.mediaId)) return
        commande(
            JSONObject().put("cmd", "download").put("media_id", p.mediaId).put("key", p.key).put("mime", p.mime),
        )
    }

    val versionAndroid get() = Build.VERSION.SDK_INT
}
