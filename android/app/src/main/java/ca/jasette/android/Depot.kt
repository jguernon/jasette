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
    /** Change à chaque arrêt : les nouvelles d'un pont d'avant sont ignorées. */
    private var generation = 0
    /** En mode client : la connexion au serveur Jasette, partagée par les trois services. */
    private var connexion: ConnexionServeur? = null

    /** Le serveur Jasette jumelé (mode « client »), ou null : « Sur cet appareil ». */
    var serveur by mutableStateOf<ConfigServeur?>(null)
        private set

    // ---- Ce que l'interface affiche ----

    /** Les conversations (boîte de réception) des deux comptes, les plus récentes d'abord. */
    val convs = mutableStateListOf<Conv>()
    /** Les conversations archivées, à part (comme `archives` du bureau), les plus récentes d'abord. */
    val archives = mutableStateListOf<Conv>()
    /** Les services à qui on a demandé leurs archives, et ceux qui ont répondu. */
    private val archivesDemandees = mutableStateMapOf<Service, Boolean>()
    val archivesRecues = mutableStateMapOf<Service, Boolean>()
    /** La vue Archives a été ouverte : un pont qui (re)devient prêt renvoie ses archives. */
    private var archivesVoulues = false
    val messages = mutableStateMapOf<String, List<Msg>>()
    /** Y a-t-il des messages plus anciens à charger ? */
    val plusAnciens = mutableStateMapOf<String, Boolean>()
    /** conv → photo (fichier local). */
    val avatars = mutableStateMapOf<String, String>()
    /** media_id → fichier téléchargé. */
    val medias = mutableStateMapOf<String, String>()
    val echecs = mutableStateMapOf<String, Boolean>()
    /** La vignette (jpg) d'une vidéo en cours d'envoi : media_id local → fichier. */
    val vignettes = mutableStateMapOf<String, String>()
    /** Les envois pas encore confirmés : tmp_id → commande `send` ou `send_media` (pour Réessayer). */
    private val envois = HashMap<String, String>()
    /** Les réactions d'avant une réaction optimiste : id du message → (conv, réactions). */
    private val reactionsAvant = HashMap<String, Pair<String, List<Reaction>>>()
    private val demandes = HashSet<String>()

    /** L'état de chaque compte. */
    val etats = mutableStateMapOf<Service, Etat>().apply { Service.entries.forEach { put(it, Etat.DEMARRAGE) } }
    /**
     * Les services à montrer, dans l'ordre : sur cet appareil, Google, Messenger
     * et Instagram ; en mode client, ceux que le serveur annonce dans `auth ok`
     * (les trois premiers tant qu'il n'a pas répondu). Un service absent d'ici
     * n'apparaît nulle part (réglages, filtres, nouveau message).
     */
    var services by mutableStateOf(Service.LOCAUX)
        private set

    /** Le nom du compte : courriel Google, nom Messenger ou Instagram. */
    val noms = mutableStateMapOf<Service, String>()
    /** Ma photo dans chaque service (`avatar_moi`). */
    val avatarsMoi = mutableStateMapOf<Service, String>()

    private fun monAvatar(service: Service, chemin: String) {
        if (chemin.isEmpty()) return
        avatarsMoi[service] = chemin
        // Comme au bureau : dès qu'une photo existe, on la prend par défaut.
        if (Marques.avatarSource.isEmpty()) Marques.choisirAvatar(service.cle)
    }
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

    /** Le service au premier plan met sa notification à jour quand le mode change. */
    var surChangementMode: (() -> Unit)? = null

    /**
     * Lance les ponts (une seule fois) : locaux, ou, en mode client, les trois
     * services du serveur Jasette (aucun pont local ne tourne alors).
     */
    fun demarrer(ctx: Context) {
        if (enMarche) return
        appli = ctx.applicationContext
        enMarche = true
        generation++
        val cfg = ConfigServeur.lire(appli)
        serveur = cfg
        if (cfg != null) {
            // Les marques : le serveur est la source (Marques.recu, sur le fil principal).
            val g = generation
            val cx = ConnexionServeur(
                cfg, ConnexionServeur.dossierCache(appli, cfg.adresse),
                surServices = { cles -> principal.post { if (g == generation) servicesOfferts(cles) } },
            ) { m ->
                principal.post { if (g == generation) Marques.recu(m) }
            }
            connexion = cx
            Marques.brancher(cfg.empreinte) { cx.envoyerMarques(it) }
        } else {
            Marques.brancher(null, null)
        }
        services = Service.LOCAUX
        // WhatsApp, Signal et X attendent que le serveur les annonce (servicesOfferts).
        Service.LOCAUX.forEach { lancer(it) }
    }

    /**
     * Le serveur vient d'annoncer ses services (`auth ok`, à chaque connexion) :
     * WhatsApp, Signal et X s'abonnent s'ils sont offerts, et disparaissent
     * (conversations comprises) s'ils ne le sont plus.
     */
    private fun servicesOfferts(cles: List<String>) {
        if (!enMarche || connexion == null) return
        val offerts = Service.entries.filter { it.cle in cles }
        for (s in Service.entries.filter { it.serveurSeulement }) {
            if (s in offerts) {
                if (!ponts.containsKey(s)) lancer(s)
            } else {
                ponts.remove(s)?.arreter()
                oublier(s)
                etats[s] = Etat.DEMARRAGE
                noms.remove(s)
                avatarsMoi.remove(s)
            }
        }
        // Les trois services de toujours restent abonnés (le serveur dit lui-même s'il ne les offre pas).
        services = if (offerts.isEmpty()) Service.LOCAUX else offerts
    }

    fun etat(service: Service): Etat = etats[service] ?: Etat.DEMARRAGE

    fun arreter() {
        enMarche = false
        generation++
        ponts.values.forEach { it.arreter() }
        ponts.clear()
        connexion?.fermer()
        connexion = null
        Marques.brancher(null, null)
    }

    /** Passe en mode client ([config]) ou revient « Sur cet appareil » (null). */
    private fun changerMode(config: ConfigServeur?) {
        val relancer = enMarche
        ConfigServeur.garder(appli, config)
        arreter()
        vider()
        // Les copies des fichiers d'un serveur abandonné ne servent plus.
        if (config == null) File(appli.filesDir, "distant").deleteRecursively()
        serveur = config
        if (relancer) demarrer(appli)
        surChangementMode?.invoke()
    }

    /** Oublie tout ce qui vient des ponts d'avant. */
    private fun vider() {
        convs.clear()
        archives.clear()
        archivesDemandees.clear()
        archivesRecues.clear()
        archivesVoulues = false
        messages.clear()
        plusAnciens.clear()
        avatars.clear()
        medias.clear()
        echecs.clear()
        vignettes.clear()
        envois.clear()
        reactionsAvant.clear()
        demandes.clear()
        Service.entries.forEach { etats[it] = Etat.DEMARRAGE }
        services = Service.LOCAUX
        noms.clear()
        avatarsMoi.clear()
        emoji = null
        besoinNip = false
        erreurNip = null
        erreur = null
        chargement = null
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
     * WhatsApp, Signal et X : seulement sur le serveur, jamais en pont local.
     */
    private fun lancer(service: Service) {
        if (service.serveurSeulement && connexion == null) return
        val g = generation
        // Depuis le fil de lecture : préfixe du service, puis le fil principal.
        val recu: (JSONObject) -> Unit = { v ->
            if (service.meta) prefixerMeta(v, service)
            principal.post {
                // Un service que le serveur n'offre plus : ses dernières nouvelles sont ignorées.
                if (g == generation && (!service.serveurSeulement || service in services)) {
                    if (service.meta) nouvelleMeta(v, service) else nouvelle(v, Service.GOOGLE)
                }
            }
        }
        try {
            val cx = connexion
            ponts[service] = when {
                cx != null -> PontDistant(service.cle, cx, recu)
                service == Service.GOOGLE -> PontLocal("google", executable("libjasette-pont.so"), environnement(), recu)
                else -> {
                    val env = environnement() + ("JASETTE_META_SERVICE" to service.cle)
                    PontLocal(service.cle, executable("libjasette-meta.so"), env, recu)
                }
            }
        } catch (e: Exception) {
            Log.e("jasette", "pont ${service.court}", e)
            erreur = "${t("Le pont ne démarre pas")} (${service.court}) : ${e.message}"
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

    // ---- Le serveur Jasette (réglages et accueil) ----

    /** « Utiliser mon serveur Jasette » choisi : le formulaire est affiché. */
    var choixClient by mutableStateOf(false)
    /** Étape 1 faite : le code de vérification à faire confirmer. */
    var verifClient by mutableStateOf<Verification?>(null)
    var serveurOccupe by mutableStateOf(false)
        private set
    var erreurServeur by mutableStateOf<String?>(null)
    /** Un jumelage vient de réussir (l'écran du serveur peut se refermer). */
    var jumelageReussi by mutableStateOf(false)
    /** L'accueil : « Autonome » choisi, on passe au choix du premier service. */
    var accueilAutonome by mutableStateOf(false)

    private fun tacheServeur(travail: () -> Unit) {
        serveurOccupe = true
        erreurServeur = null
        Thread({
            try {
                travail()
            } catch (e: Exception) {
                val texte = e.message ?: e.toString()
                principal.post { erreurServeur = texte }
            } finally {
                principal.post { serveurOccupe = false }
            }
        }, "serveur-jumelage").start()
    }

    /** Client, étape 1 : se brancher et calculer le code de vérification (rien de secret ne part). */
    fun verifierClient(adresse: String, code: String) {
        val a = adresse.trim()
        val c = code.filter { it.isDigit() }
        if (a.isEmpty() || c.length != 6) {
            erreurServeur = t("Entre l'adresse du serveur et le code à 6 chiffres qu'il affiche.")
            return
        }
        tacheServeur {
            val v = Jumelage.verifier(a, c)
            principal.post { verifClient = v }
        }
    }

    /** Client, étape 2 : les codes correspondent, on jumelle ; le mode devient « client ». */
    fun jumelerClient() {
        val v = verifClient ?: return
        val nom = Jumelage.nomAppareil(appli)
        tacheServeur {
            val cfg = Jumelage.jumeler(v, nom)
            principal.post {
                verifClient = null
                choixClient = false
                jumelageReussi = true
                changerMode(cfg)
            }
        }
    }

    fun refuserVerification() {
        verifClient = null
        erreurServeur = t("Jumelage annulé : les codes ne correspondaient pas.")
    }

    /** « Sur cet appareil » : plus de serveur, les ponts locaux reprennent. */
    fun utiliserCetAppareil() {
        choixClient = false
        verifClient = null
        erreurServeur = null
        if (serveur != null) changerMode(null)
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
                    pretPourArchives(service)
                }
                if (apres == Etat.NON_ASSOCIE) oublier(service)
            }
            "paired" -> etats[service] = Etat.CONNEXION
            "account" -> v.chaine("name").let { if (it.isNotEmpty()) noms[service] = it else noms.remove(service) }
            "avatar_moi" -> monAvatar(service, v.chaine("path"))
            "history_pin" -> when {
                v.optBoolean("needed") -> besoinNip = true
                v.optBoolean("ok") -> {
                    besoinNip = false
                    erreurNip = null
                }
                v.optBoolean("declined") -> besoinNip = false
                v.chaine("text").isNotEmpty() -> erreurNip = v.chaine("text")
            }
            "search" -> recevoirRecherche(v, service)
            "hello", "e2ee", "typing" -> {}
            "error" -> {
                val texte = v.chaine("text")
                if (v.chaine("where") == "search") Log.d("jasette", "search ${service.court} : erreur (${texte.length} car.)")
                if (v.chaine("where") == "react") annulerReactions()
                if (texte.isNotEmpty()) erreur = "${service.court} : ${t(texte)}"
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
                    Etat.PRET -> {
                        emoji = null
                        pretPourArchives(Service.GOOGLE)
                    }
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
                val items = v.optJSONArray("items") ?: return
                erreur = null
                val vues = HashSet<String>()
                val nouvelles = (0 until items.length())
                    .map { Conv.de(items.getJSONObject(it)) }
                    .filter { vues.add(it.id) }
                // Chaque pont ne remplace que ses propres conversations (archives à part).
                if (v.chaine("folder") == "archive" || v.optBoolean("archive")) {
                    remplacerArchives(archives.filter { it.service != service } + nouvelles)
                    archivesRecues[service] = true
                } else {
                    remplacerConvs(convs.filter { it.service != service } + nouvelles)
                }
            }
            "conversation" -> {
                val c = Conv.de(v.optJSONObject("item") ?: return)
                val autres = convs.filter { it.id != c.id }
                val autresArchives = archives.filter { it.id != c.id }
                if (c.archived) {
                    remplacerConvs(autres)
                    remplacerArchives(autresArchives + c)
                } else {
                    remplacerConvs(autres + c)
                    if (autresArchives.size != archives.size) remplacerArchives(autresArchives)
                }
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
            "avatar_moi" -> monAvatar(Service.GOOGLE, v.chaine("path"))
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
            // Ponts Meta : une conversation supprimée (ici, sur un autre appareil ou dans l'app).
            "deleted" -> {
                val id = v.chaine("conv")
                if (id.isEmpty()) return
                if (convs.any { it.id == id }) remplacerConvs(convs.filter { it.id != id })
                if (archives.any { it.id == id }) remplacerArchives(archives.filter { it.id != id })
                messages.remove(id)
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
            // Les contacts du téléphone trouvés par le pont Google (ids : les numéros, sans préfixe).
            "search" -> recevoirRecherche(v, Service.GOOGLE)
            "error" -> {
                val texte = v.chaine("text")
                if (v.chaine("where") == "search") Log.d("jasette", "search ${service.court} : erreur (${texte.length} car.)")
                if (v.chaine("where") == "react") annulerReactions()
                if (texte.isNotEmpty()) erreur = t(texte)
                if (v.chaine("where") == "messages") chargement = null
            }
            "pont_ferme" -> pontFerme(service)
            else -> {}
        }
    }

    private fun recevoirMessage(m: Msg) {
        telechargerImages(listOf(m))
        if (m.tmpId.isNotEmpty()) envois.remove(m.tmpId)
        reactionsAvant.remove(m.id)
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

        val conv = conv(m.conv)
        if (conv != null) {
            var c = conv
            if (nouveau || c.time <= m.time) c = c.copy(preview = m.apercu(), fromMe = m.fromMe)
            c = c.copy(time = maxOf(c.time, m.time))
            if (nouveau && !m.fromMe && !ouverteIci) c = c.copy(unread = true)
            if (convs.any { it.id == c.id }) {
                remplacerConvs(convs.map { if (it.id == c.id) c else it })
            } else {
                remplacerArchives(archives.map { if (it.id == c.id) c else it })
            }
        }
        // Archives de Jasette : un message reçu ramène la conversation dans la boîte de réception.
        if (nouveau && !m.fromMe) Marques.desarchiverSiRecu(m.conv, m.time)
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

    private fun remplacerArchives(liste: List<Conv>) {
        val triee = liste.sortedByDescending { it.time }
        archives.clear()
        archives.addAll(triee)
    }

    /** Une conversation, dans la boîte de réception ou les archives. */
    fun conv(id: String): Conv? = convs.firstOrNull { it.id == id } ?: archives.firstOrNull { it.id == id }

    /** Archivée par Jasette (`Marques.archives`) : seulement pour les services qui n'archivent pas eux-mêmes. */
    fun archiveeJasette(id: String) = !Service.de(id).archiveNatif && Marques.estArchivee(id)

    /** Archivée, par son service ou par Jasette. */
    fun estArchivee(c: Conv) = c.archived || archives.any { it.id == c.id } || archiveeJasette(c.id)

    /** La boîte de réception (et les vues Favoris et par service) : sans les archives de Jasette. */
    val boite: List<Conv> get() = convs.filter { !archiveeJasette(it.id) }

    /** La vue Archives : celles des services et celles de Jasette, ensemble, les plus récentes d'abord. */
    val toutesArchives: List<Conv>
        get() = (archives + convs.filter { archiveeJasette(it.id) }).distinctBy { it.id }.sortedByDescending { it.time }

    /** Un compte déconnecté : ses conversations disparaissent de la liste (et des archives). */
    private fun oublier(service: Service) {
        if (convs.any { it.service == service }) remplacerConvs(convs.filter { it.service != service })
        if (archives.any { it.service == service }) remplacerArchives(archives.filter { it.service != service })
        archivesDemandees.remove(service)
        archivesRecues.remove(service)
    }

    // ---- Les archives ----

    /**
     * La vue Archives s'ouvre : chaque pont prêt reçoit
     * `{"cmd":"conversations","folder":"archive"}` (une fois ; le bureau fait
     * pareil au premier clic sur l'icône des archives).
     */
    fun chargerArchives() {
        archivesVoulues = true
        for (s in Service.entries) if (pret(s)) demanderArchives(s)
    }

    /** Un pont (re)devient prêt : s'il le faut, il renvoie ses archives. */
    private fun pretPourArchives(service: Service) {
        if (!archivesVoulues) return
        archivesDemandees.remove(service)
        demanderArchives(service)
    }

    private fun demanderArchives(service: Service) {
        if (archivesDemandees.put(service, true) == true) return
        commande(service, JSONObject().put("cmd", "conversations").put("folder", "archive").put("count", 100))
    }

    /** Les archives sont-elles encore attendues d'un pont ? */
    val archivesEnAttente get() = archivesDemandees.keys.any { archivesRecues[it] != true }


    /** Un pont s'est arrêté tout seul : on le relance après quelques secondes. */
    private fun pontFerme(service: Service) {
        if (!enMarche) return
        etats[service] = Etat.ARRETE
        principal.postDelayed({
            if (enMarche && ponts[service]?.vivant() != true && service in services) lancer(service)
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

    /**
     * Archive ou désarchive, et le montre tout de suite. Google et WhatsApp : par
     * leur pont (le téléphone confirmera par une nouvelle « conversation ») ; les
     * autres : par les archives de Jasette (`Marques.archives`, partagées par le
     * serveur). « Annuler » appelle simplement l'inverse.
     */
    fun archiver(id: String, archiver: Boolean) {
        if (!Service.de(id).archiveNatif) {
            Marques.archiver(id, archiver)
            return
        }
        commande(JSONObject().put("cmd", "archive").put("conv", id).put("archived", archiver))
        if (archiver) {
            val c = convs.firstOrNull { it.id == id } ?: return
            remplacerConvs(convs.filter { it.id != id })
            remplacerArchives(archives + c.copy(archived = true))
        } else {
            val c = archives.firstOrNull { it.id == id } ?: return
            remplacerArchives(archives.filter { it.id != id })
            remplacerConvs(convs + c.copy(archived = false))
        }
    }

    // ---- Nouveau message ----

    /**
     * Les personnes trouvées pour « À : » : (id, nom). Les ids Meta sont préfixés ;
     * ceux de Google sont des numéros (sans préfixe, donc `Service.de` vaut GOOGLE).
     */
    val resultatsMeta = mutableStateListOf<Pair<String, String>>()
    /** Le numéro affiché d'un contact trouvé par Google, par id. */
    val numerosContacts = mutableStateMapOf<String, String>()
    private var requeteContacts = ""

    /**
     * « À : » a changé : Messenger et Instagram cherchent parmi les amis et
     * contacts, Google parmi les contacts du téléphone (aussi par numéro),
     * WhatsApp et Signal parmi leurs contacts et groupes (aussi par numéro).
     */
    fun chercherContacts(texte: String) {
        val q = texte.trim()
        if (q == requeteContacts) return
        requeteContacts = q
        resultatsMeta.clear()
        numerosContacts.clear()
        if (q.length < 2) return
        val cmd = { JSONObject().put("cmd", "search").put("text", q) }
        val numero = estNumero(q)
        for (s in services) {
            if (!pret(s) || !(s.cherchePersonnes || s == Service.GOOGLE)) continue
            if (numero && !s.chercheNumeros) continue
            commande(s, cmd())
        }
    }

    /**
     * Les résultats d'une recherche de personnes : ceux de chaque service
     * remplacent les siens (comme `resultats_meta`), si la requête est encore la bonne.
     */
    private fun recevoirRecherche(v: JSONObject, service: Service) {
        val items = v.optJSONArray("items")
        val q = v.chaine("query")
        // Pour diagnostiquer par logcat, sans les noms : service, longueur, nombre.
        Log.d(
            "jasette",
            "search ${service.court} : requête ${q.length} car., ${items?.length() ?: 0} résultat(s)" +
                if (q == requeteContacts) "" else " (périmée)",
        )
        if (q != requeteContacts) return
        val trouves = (0 until (items?.length() ?: 0)).mapNotNull { i ->
            val o = items!!.optJSONObject(i) ?: return@mapNotNull null
            val id = o.chaine("id")
            val nom = o.chaine("name")
            if (id.isEmpty() || nom.isEmpty()) return@mapNotNull null
            if (service == Service.GOOGLE) numerosContacts[id] = o.chaine("number").ifEmpty { id }
            id to nom
        }
        val autres = resultatsMeta.filter { Service.de(it.first) != service }
        resultatsMeta.clear()
        resultatsMeta.addAll(autres + trouves.distinctBy { it.first })
    }

    /**
     * Crée (ou retrouve) une conversation avec la commande `new` : un numéro par
     * Google Messages, ou l'id d'une personne trouvée sur Messenger ou Instagram.
     * Le pont répond par une nouvelle « conversation » avec open, qui l'ouvre.
     */
    fun nouvelleConversation(service: Service, numero: String) {
        commande(service, JSONObject().put("cmd", "new").put("number", numero.removePrefix(service.prefixe)))
    }

    /**
     * Supprime une conversation (Google, Messenger et Instagram : commande
     * `delete`). Retirée tout de suite ; le pont confirme par l'événement
     * `deleted` (Meta) ou, s'il échoue, la remet par sa liste et une erreur.
     */
    fun supprimer(id: String) {
        commande(JSONObject().put("cmd", "delete").put("conv", id))
        remplacerConvs(convs.filter { it.id != id })
        remplacerArchives(archives.filter { it.id != id })
        messages.remove(id)
        Marques.oublier(id)
    }

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
        val cmd = JSONObject().put("cmd", "send").put("conv", conv).put("text", t).put("tmp_id", tmp)
        envois[tmp] = cmd.toString()
        commande(cmd)
    }

    // ---- Envoyer des fichiers ----

    /**
     * Envoie un fichier déjà copié dans le cache de l'app (`send_media`, comme
     * `envoyer_fichier` du bureau) : une bulle locale « Envoi… » le montre tout
     * de suite. En mode client, le serveur reçoit d'abord le fichier (`put`).
     * [texte] part avec le fichier (ponts Meta seulement ; Google l'envoie à part).
     */
    fun envoyerFichier(conv: String, fichier: File, mime: String, nom: String, texte: String, vignette: String?) {
        envoiSeq++
        val maintenant = System.currentTimeMillis()
        val tmp = "jasette_${maintenant}_$envoiSeq"
        val idLocal = "local:${fichier.path}"
        medias[idLocal] = fichier.path
        if (vignette != null) vignettes[idLocal] = vignette
        var w = 0L
        var h = 0L
        if (mime.startsWith("image/")) {
            val o = android.graphics.BitmapFactory.Options().apply { inJustDecodeBounds = true }
            android.graphics.BitmapFactory.decodeFile(fichier.path, o)
            w = o.outWidth.toLong().coerceAtLeast(0)
            h = o.outHeight.toLong().coerceAtLeast(0)
        }
        val piece = Piece(idLocal, "", mime, nom, fichier.length(), w, h)
        val local = Msg(
            id = tmp, conv = conv, text = texte, media = "", fromMe = true, sender = "", time = maintenant,
            status = "ENVOI", kind = "", tmpId = tmp, attachments = listOf(piece), reactions = emptyList(),
        )
        messages[conv] = messages[conv].orEmpty() + local
        val cmd = JSONObject().put("cmd", "send_media").put("conv", conv).put("path", fichier.path)
            .put("tmp_id", tmp).put("mime", mime)
        if (texte.isNotEmpty()) cmd.put("text", texte)
        // commande() retire les préfixes sur place : on garde une copie intacte.
        envois[tmp] = cmd.toString()
        commande(cmd)
    }

    fun peutReessayer(tmp: String) = envois.containsKey(tmp)

    /** Renvoie un message ou un fichier dont l'envoi a échoué (même tmp_id). */
    fun reessayer(conv: String, tmp: String) {
        val cmd = envois[tmp] ?: return
        messages[conv] = messages[conv].orEmpty().map { if (it.tmpId == tmp) it.copy(status = "ENVOI") else it }
        commande(JSONObject(cmd))
    }

    // ---- Les réactions ----

    /**
     * Réagit à un message (`react`), tout de suite à l'écran. Re-choisir ma
     * réaction la retire (emoji vide). Une erreur `react` revient en arrière.
     */
    fun reagir(conv: String, msg: String, emoji: String) {
        val liste = messages[conv] ?: return
        val m = liste.firstOrNull { it.id == msg } ?: return
        if (m.local) return
        val mienne = m.reactions.firstOrNull { it.fromMe }?.emoji
        val choix = if (mienne == emoji) "" else emoji
        if (!reactionsAvant.containsKey(msg)) reactionsAvant[msg] = conv to m.reactions
        val apres = m.reactions.filter { !it.fromMe } + if (choix.isEmpty()) emptyList() else listOf(Reaction(choix, "", true))
        messages[conv] = liste.map { if (it.id == msg) it.copy(reactions = apres) else it }
        commande(JSONObject().put("cmd", "react").put("conv", conv).put("id", msg).put("emoji", choix))
    }

    /** Un pont a refusé une réaction : les réactions optimistes reviennent en arrière. */
    private fun annulerReactions() {
        for ((msg, avant) in reactionsAvant) {
            val (conv, reactions) = avant
            messages[conv]?.let { l -> messages[conv] = l.map { if (it.id == msg) it.copy(reactions = reactions) else it } }
        }
        reactionsAvant.clear()
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
