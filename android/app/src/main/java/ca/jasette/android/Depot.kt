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
import org.json.JSONArray
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

    /** La connexion au serveur Jasette (mode client), pour les aperçus de liens. */
    fun connexionServeur(): ConnexionServeur? = connexion

    /** Le serveur Jasette jumelé (mode « client »), ou null : « Sur cet appareil ». */
    var serveur by mutableStateOf<ConfigServeur?>(null)
        private set

    // ---- Ce que l'interface affiche ----

    /** La présence en ligne des conversations individuelles (Presence.kt, PROTOCOLE.md §5.10). */
    val presences = mutableStateMapOf<String, Presence>()

    /** Qui écrit, par conversation (Presence.kt). */
    val ecrits = mutableStateMapOf<String, Ecrit>()

    /** Quelqu'un écrit-il dans cette conversation (et le réglage le montre-t-il) ? */
    fun ecritDe(id: String): Ecrit? = if (!Marques.presenceEnLigne) null else ecrits[id]

    /** Une ligne `typing` d'un pont (ids déjà préfixés). Jasette n'envoie jamais le sien. */
    private fun recevoirEcrit(v: JSONObject) {
        if (!Marques.presenceEnLigne) return
        val conv = v.chaine("conv")
        if (conv.isEmpty()) return
        if (!v.optBoolean("on")) {
            if (ecrits.remove(conv) != null) Sons.ecritParti(conv)
            return
        }
        val e = Ecrit(v.chaine("name"), System.currentTimeMillis())
        if (ecrits.put(conv, e) == null) Sons.ecrit(conv)
        // Sans nouvelle depuis 6 s : les « … » s'en vont.
        principal.postDelayed({
            if (ecrits[conv] === e) {
                ecrits.remove(conv)
                Sons.ecritParti(conv)
            }
        }, ECRIT_DUREE_MS)
    }

    /** La présence d'une conversation à montrer (réglage activé, pas un groupe), ou null. */
    fun presenceDe(c: Conv): Presence? = if (!Marques.presenceEnLigne || c.group) null else presences[c.id]

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
    /** Les pièces en cours de téléchargement (l'interface montre « Téléchargement… »). */
    val telechargements = mutableStateMapOf<String, Boolean>()
    /** Les pièces à ouvrir dès qu'elles arrivent (un clic sur un fichier pas encore là). */
    private val ouvrirApres = HashSet<String>()
    /** Les conversations dont le service a déjà répondu `messages` depuis qu'il est prêt (pas le cache). */
    private val fraiches = HashSet<String>()
    /** Les conversations dont on attend la réponse `messages` (une seule demande à la fois). */
    private val demandesFil = HashSet<String>()

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

    /**
     * « Utiliser les SMS du téléphone » : en mode autonome, Google Messages est
     * servi par [PontSms] (les SMS et MMS de l'appareil) au lieu du pont Go.
     * Gardé dans les préférences « reglages » ; sans effet en mode client.
     */
    var modeSms by mutableStateOf(false)
        private set

    /** Les SMS du téléphone sont-ils en service (mode autonome) ? */
    val smsActif: Boolean get() = modeSms && serveur == null

    /** Une conversation SMS du téléphone ? (pas d'envoi de photos, pas de réactions) */
    fun estSms(id: String) = smsActif && SmsJson.threadDe(id) != null

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
    /** WhatsApp et Signal, pendant l'association : le texte du code QR à afficher (`ev qr`). */
    val qr = mutableStateMapOf<Service, String>()
    /**
     * Telegram : l'étape de la connexion (`ev login_step` : « code »,
     * « password »), l'indice du mot de passe, et l'application réglée
     * (`ev api` : api_id et api_hash de my.telegram.org).
     */
    var etapeTelegram by mutableStateOf<String?>(null)

    /** Discord : le code QR a été lu (le nom du compte) ; reste à confirmer sur le téléphone. */
    var qrLuDiscord by mutableStateOf<String?>(null)
    var indiceTelegram by mutableStateOf<String?>(null)
    var apiTelegram by mutableStateOf<Boolean?>(null)
    /** WhatsApp, « Associer avec un numéro de téléphone » : le code à taper (`ev pair_code`). */
    val codeAssociation = mutableStateMapOf<Service, String>()
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
        Marques.surPresence = { oui -> principal.post { presenceReglee(oui) } }
        Sons.init(ctx)
        generation++
        val cfg = ConfigServeur.lire(appli)
        serveur = cfg
        modeSms = appli.getSharedPreferences(PREFS_REGLAGES, Context.MODE_PRIVATE).getBoolean(CLE_SMS, false)
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
        CacheConvs.initialiser(appli)
        if (cfg != null) {
            // WhatsApp, Signal et X attendent que le serveur les annonce (servicesOfferts) ;
            // ceux qu'il offrait la dernière fois (gardés en cache) se montrent déjà.
            services = Service.LOCAUX + CacheConvs.servicesGardes().filter { it.serveurSeulement }
            chargerCache()
            Service.LOCAUX.forEach { lancer(it) }
        } else {
            // Sur cet appareil : les ponts embarqués, plus WhatsApp, Signal et X
            // si leur pont a été compilé dans l'APK (sinon, absents sans erreur).
            services = Service.LOCAUX + Service.entries.filter { it.serveurSeulement && embarque(it) }
            chargerCache()
            services.forEach { lancer(it) }
        }
    }

    /**
     * Avant toute connexion : les listes gardées par [CacheConvs] s'affichent
     * tout de suite (lues ici, elles sont petites), puis les messages gardés
     * arrivent d'un fil à part. Ce qu'un service a déjà envoyé n'est pas écrasé.
     */
    private fun chargerCache() {
        for (s in services) {
            if (convs.any { it.service == s } || archives.any { it.service == s }) continue
            val l = CacheConvs.lireListes(s) ?: continue
            for ((k, v) in l.avatars) if (!avatars.containsKey(k)) avatars[k] = v
            if (l.convs.isNotEmpty()) remplacerConvs(convs + l.convs.filter { c -> archives.none { it.id == c.id } })
            if (l.archives.isNotEmpty()) remplacerArchives(archives + l.archives.filter { c -> convs.none { it.id == c.id } })
        }
        val g = generation
        CacheConvs.lireFils(services) { fils ->
            if (g != generation) return@lireFils
            for (f in fils) {
                for ((k, v) in f.medias) if (!medias.containsKey(k)) medias[k] = v
                val deja = messages[f.conv]
                messages[f.conv] = if (deja == null) f.messages else ajouterAnciens(deja, f.messages)
                if (!plusAnciens.containsKey(f.conv)) plusAnciens[f.conv] = f.plus
                if (chargement == f.conv && messages[f.conv].orEmpty().isNotEmpty()) chargement = null
            }
        }
    }

    /** [deja] (venu du service) d'abord ; les messages gardés qui n'y sont pas s'y ajoutent, dans l'ordre. */
    private fun ajouterAnciens(deja: List<Msg>, gardes: List<Msg>): List<Msg> {
        val ids = deja.mapTo(HashSet()) { it.id }
        val (locaux, confirmes) = deja.partition { it.local }
        return (confirmes + gardes.filter { it.id !in ids }).sortedBy { it.time } + locaux
    }

    /**
     * La réponse `messages` (non `older`) fusionnée avec ce qu'on montre déjà
     * (le cache, ou une réponse précédente), par id de message, sans doublon,
     * dans l'ordre. Les envois locaux pas encore confirmés restent à la fin.
     * Une vraie réponse du service ([autorite], pas `cache:true`) fait foi sur
     * sa période : un message gardé qui tombe entre son plus vieux et son plus
     * récent message, mais qu'elle n'a pas, a été supprimé entre-temps.
     */
    private fun fusionner(liste: List<Msg>, items: List<Msg>, autorite: Boolean): List<Msg> {
        val recus = items.filter { !it.local }.distinctBy { it.id }
        val ids = recus.mapTo(HashSet()) { it.id }
        val tmps = recus.mapNotNullTo(HashSet()) { it.tmpId.ifEmpty { null } }
        val debut = recus.minOfOrNull { it.time } ?: Long.MAX_VALUE
        val fin = recus.maxOfOrNull { it.time } ?: Long.MIN_VALUE
        val locaux = ArrayList<Msg>()
        val autres = ArrayList<Msg>()
        for (m in liste) {
            when {
                m.id in ids -> {}
                m.tmpId.isNotEmpty() && m.tmpId in tmps -> {}
                recus.any { m.localPour(it) } -> {}
                m.local -> locaux.add(m)
                autorite && m.time in debut..fin -> {}
                else -> autres.add(m)
            }
        }
        return (recus + autres).sortedBy { it.time } + locaux
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
        CacheConvs.ecrireMaintenant()
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
        // Autre serveur, ou plus de serveur : le cache des conversations ne vaut plus.
        CacheConvs.toutEffacer()
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
        telechargements.clear()
        ouvrirApres.clear()
        fraiches.clear()
        demandesFil.clear()
        Service.entries.forEach { etats[it] = Etat.DEMARRAGE }
        services = Service.LOCAUX
        noms.clear()
        avatarsMoi.clear()
        qr.clear()
        codeAssociation.clear()
        presences.clear()
        ecrits.clear()
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
            // « Afficher qui est en ligne » (PROTOCOLE.md §5.10) ; ensuite, la commande `presence`.
            "JASETTE_PRESENCE" to if (Marques.presenceEnLigne) "1" else "0",
        )
    }

    /** Les services dont le pont suit la présence. */
    private val SERVICES_PRESENCE = listOf(Service.MESSENGER, Service.INSTAGRAM, Service.WHATSAPP, Service.TELEGRAM, Service.DISCORD)

    /**
     * Le réglage « Afficher qui est en ligne » vient de changer : les ponts
     * locaux reçoivent la commande `presence` (en mode client, le serveur
     * prévient les siens) ; désactivé, tout est oublié.
     */
    private fun presenceReglee(oui: Boolean) {
        if (!oui) {
            presences.clear()
            ecrits.clear()
        }
        if (connexion != null) return
        for (s in SERVICES_PRESENCE) (ponts[s] as? PontLocal)?.envoyer(JSONObject().put("cmd", "presence").put("on", oui))
    }

    /** Une ligne `presence` d'un pont (ids déjà préfixés). */
    private fun recevoirPresence(v: JSONObject) {
        if (!Marques.presenceEnLigne) return
        val item = v.optJSONObject("item") ?: return
        val conv = item.chaine("conv")
        if (conv.isEmpty() || (presences.size > 2000 && conv !in presences)) return
        presences[conv] = Presence(item.optBoolean("online"), item.optLong("last_active", 0))
    }

    private fun executable(nom: String) = File(appli.applicationInfo.nativeLibraryDir, nom)

    /** Le pont de WhatsApp, Signal ou X embarqué dans l'APK (lib*.so de build-ponts.sh). */
    private fun executableDe(service: Service): File? = when (service) {
        Service.WHATSAPP -> executable("libjasette-wa.so")
        Service.SIGNAL -> executable("libjasette-signal.so")
        Service.X -> executable("libjasette-x.so")
        Service.TELEGRAM -> executable("libjasette-telegram.so")
        Service.BLUESKY -> executable("libjasette-bluesky.so")
        Service.DISCORD -> executable("libjasette-discord.so")
        else -> null
    }

    /** Ce pont a-t-il été compilé dans l'APK ? (le pont Signal demande libsignal, parfois absente) */
    private fun embarque(service: Service): Boolean = executableDe(service)?.isFile == true

    /**
     * Lance le pont d'un service. Le pont Meta sert Messenger ou Instagram selon
     * JASETTE_META_SERVICE, chacun avec son dossier (<données>/messenger ou /instagram).
     * WhatsApp, Signal et X : sur le serveur, ou en pont local s'il est embarqué.
     * Chaque pont range ses données dans <données>/<service> (whatsapp, signal, x).
     */
    private fun lancer(service: Service) {
        if (service.serveurSeulement && connexion == null && !embarque(service)) return
        val g = generation
        // Le pont lancé ici : les nouvelles d'un pont remplacé (Google ↔ SMS) sont ignorées.
        var lui: Pont? = null
        // Depuis le fil de lecture : préfixe du service, puis le fil principal.
        val recu: (JSONObject) -> Unit = { v ->
            if (service.meta) prefixerMeta(v, service)
            principal.post {
                // Un service que le serveur n'offre plus : ses dernières nouvelles sont ignorées.
                if (g == generation && ponts[service] === lui && (!service.serveurSeulement || service in services)) {
                    if (service.meta) nouvelleMeta(v, service) else nouvelle(v, Service.GOOGLE)
                }
            }
        }
        try {
            val cx = connexion
            val p = when {
                cx != null -> PontDistant(service.cle, cx, recu)
                // Les SMS du téléphone à la place de Google Messages (PontSms).
                service == Service.GOOGLE && modeSms -> PontSms(appli, recu)
                service == Service.GOOGLE -> PontLocal("google", executable("libjasette-pont.so"), environnement(), recu)
                service.serveurSeulement -> PontLocal(service.cle, executableDe(service)!!, environnement(), recu)
                else -> {
                    val env = environnement() + ("JASETTE_META_SERVICE" to service.cle)
                    // Instagram a changé de protocole : son propre pont (instameow),
                    // comme au bureau ; l'ancien pont Meta y plante (« base_url not found »).
                    val ig = executable("libjasette-ig.so")
                    val bin = if (service == Service.INSTAGRAM && ig.isFile) ig else executable("libjasette-meta.so")
                    PontLocal(service.cle, bin, env, recu)
                }
            }
            lui = p
            ponts[service] = p
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

    /** La connexion MCP du serveur (Claude) : dernier état reçu, demande en cours, erreur. */
    var mcp by mutableStateOf<EtatMcp?>(null)
        private set
    var mcpOccupe by mutableStateOf(false)
        private set
    var mcpErreur by mutableStateOf<String?>(null)
        private set

    /** Une trame `mcp` au serveur (`etat`, `ouvrir`, `fermer`, `regenerer`, `envoi`). */
    fun actionMcp(action: String, envoi: Boolean? = null) {
        val cx = connexion ?: return
        if (mcpOccupe) return
        mcpOccupe = true
        mcpErreur = null
        Thread({
            try {
                val e = cx.commandeMcp(action, envoi)
                principal.post {
                    mcp = e
                    mcpErreur = e.erreur
                }
            } catch (e: Exception) {
                val texte = e.message ?: e.toString()
                principal.post { mcpErreur = texte }
            } finally {
                principal.post { mcpOccupe = false }
            }
        }, "serveur-mcp").start()
    }

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

    fun deconnecter(service: Service) {
        // SMS du téléphone : « Déconnecter » revient au choix (Google ou SMS), rien n'est effacé du téléphone.
        if (service == Service.GOOGLE && smsActif) {
            utiliserSms(appli, false)
            return
        }
        qr.remove(service)
        codeAssociation.remove(service)
        // Le service est déconnecté : son cache (listes et messages) est effacé.
        oublier(service)
        for (k in messages.keys.filter { Service.de(it) == service }) {
            messages.remove(k)
            plusAnciens.remove(k)
        }
        fraiches.removeAll { Service.de(it) == service }
        demandesFil.removeAll { Service.de(it) == service }
        CacheConvs.effacer(service)
        // Les ponts WhatsApp, Signal et X ne connaissent que « unpair ».
        val cmd = if (service == Service.GOOGLE || service.serveurSeulement) "unpair" else "logout"
        commande(service, JSONObject().put("cmd", cmd))
    }

    /**
     * Passe Google Messages aux SMS du téléphone ([oui]) ou revient au pont
     * Google (pas encore connecté : le bouton « Se connecter »). Le choix est
     * gardé ; les conversations et le cache de l'autre source sont oubliés.
     */
    fun utiliserSms(ctx: Context, oui: Boolean) {
        ctx.applicationContext.getSharedPreferences(PREFS_REGLAGES, Context.MODE_PRIVATE).edit().putBoolean(CLE_SMS, oui).apply()
        // Même choix qu'avant : le pont repart quand même (ex. accès aux SMS redonné).
        modeSms = oui
        if (!enMarche || serveur != null) return
        ponts.remove(Service.GOOGLE)?.arreter()
        emoji = null
        erreur = null
        noms.remove(Service.GOOGLE)
        avatarsMoi.remove(Service.GOOGLE)
        oublier(Service.GOOGLE)
        for (k in messages.keys.filter { Service.de(it) == Service.GOOGLE }) {
            messages.remove(k)
            plusAnciens.remove(k)
        }
        fraiches.removeAll { Service.de(it) == Service.GOOGLE }
        demandesFil.removeAll { Service.de(it) == Service.GOOGLE }
        CacheConvs.effacer(Service.GOOGLE)
        etats[Service.GOOGLE] = Etat.DEMARRAGE
        lancer(Service.GOOGLE)
    }

    /** WhatsApp ou Signal : association par code QR (`pair`, puis des `ev qr`). */
    fun associerQr(service: Service) {
        erreur = null
        codeAssociation.remove(service)
        commande(service, JSONObject().put("cmd", "pair"))
    }

    /** WhatsApp : « Associer avec un numéro de téléphone » (`pair_phone`, puis `ev pair_code`). */
    fun associerTelephone(numero: String) {
        erreur = null
        codeAssociation.remove(Service.WHATSAPP)
        qr.remove(Service.WHATSAPP)
        commande(Service.WHATSAPP, JSONObject().put("cmd", "pair_phone").put("number", numero))
    }

    /** Telegram : l'api_id et l'api_hash de l'application de l'utilisateur (gardés par le pont, 0600). */
    fun telegramApi(id: String, hash: String) {
        erreur = null
        commande(Service.TELEGRAM, JSONObject().put("cmd", "api").put("api_id", id).put("api_hash", hash))
    }

    /** Telegram : le numéro → Telegram envoie un code dans l'app (`ev login_step`). */
    fun telegramNumero(numero: String) {
        erreur = null
        qr.remove(Service.TELEGRAM)
        etapeTelegram = "attente"
        commande(Service.TELEGRAM, JSONObject().put("cmd", "login").put("number", numero))
    }

    /** Telegram : le code reçu. */
    fun telegramCode(code: String) {
        erreur = null
        etapeTelegram = "attente"
        commande(Service.TELEGRAM, JSONObject().put("cmd", "login_code").put("code", code))
    }

    /** Telegram : le mot de passe (vérification en deux étapes). */
    fun telegramMotDePasse(mdp: String) {
        erreur = null
        etapeTelegram = "attente"
        commande(Service.TELEGRAM, JSONObject().put("cmd", "login_password").put("password", mdp))
    }

    /**
     * Bluesky : l'identifiant et un mot de passe d'app (avec l'accès aux
     * messages privés). Le pont ne garde que les jetons de session.
     */
    fun blueskyConnexion(identifiant: String, motDePasse: String) {
        erreur = null
        etats[Service.BLUESKY] = Etat.ASSOCIATION
        commande(Service.BLUESKY, JSONObject().put("cmd", "login").put("number", identifiant).put("password", motDePasse))
    }

    /** Discord : la connexion par code QR (`pair`, puis des `ev qr`). */
    fun discordQr() {
        erreur = null
        qrLuDiscord = null
        qr.remove(Service.DISCORD)
        commande(Service.DISCORD, JSONObject().put("cmd", "pair"))
    }

    /** Discord : le jeton du compte, collé à la main (gardé par le pont seul, 0600). */
    fun discordJeton(jeton: String) {
        erreur = null
        qrLuDiscord = null
        qr.remove(Service.DISCORD)
        etats[Service.DISCORD] = Etat.ASSOCIATION
        commande(Service.DISCORD, JSONObject().put("cmd", "login").put("token", jeton))
    }

    /** X : les cookies de x.com et le code X Chat, en un seul essai (X verrouille après quelques codes faux). */
    fun connecterX(cookies: Map<String, String>, pin: String) {
        erreur = null
        commande(Service.X, JSONObject().put("cmd", "login").put("cookies", JSONObject(cookies)).put("pin", pin))
    }

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
                    filsAuPret(service)
                }
                if (apres == Etat.NON_ASSOCIE) oublier(service)
                // Le pont n'écoute plus : sa présence ne vaut plus rien.
                if (apres != Etat.PRET) presences.keys.filter { Service.de(it) == service }.forEach { presences.remove(it) }
                // Hors association, le QR et le code n'ont plus de sens.
                if (apres != Etat.ASSOCIATION) {
                    qr.remove(service)
                    codeAssociation.remove(service)
                    if (service == Service.TELEGRAM) {
                        etapeTelegram = null
                        indiceTelegram = null
                    }
                    if (service == Service.DISCORD) qrLuDiscord = null
                }
            }
            "paired" -> {
                qr.remove(service)
                codeAssociation.remove(service)
                etats[service] = Etat.CONNEXION
            }
            // WhatsApp et Signal : un code QR (renouvelé) à scanner.
            "qr" -> v.chaine("code").ifEmpty { v.chaine("data") }.let {
                if (it.isNotEmpty()) qr[service] = it
                if (service == Service.DISCORD) qrLuDiscord = null
            }
            // Discord : le code QR a été lu ; le téléphone demande de confirmer.
            "qr_scanned" -> if (service == Service.DISCORD) qrLuDiscord = v.chaine("name")
            // WhatsApp : le code à taper sur le téléphone ; les QR cessent.
            "pair_code" -> v.chaine("code").let {
                if (it.isNotEmpty()) {
                    codeAssociation[service] = it
                    qr.remove(service)
                }
            }
            "account" -> v.chaine("name").let { if (it.isNotEmpty()) noms[service] = it else noms.remove(service) }
            // Telegram : l'étape suivante de la connexion, et l'application réglée.
            "login_step" -> {
                val etape = v.chaine("step")
                if (etape == "password") {
                    v.chaine("hint").let { if (it.isNotEmpty()) indiceTelegram = it }
                } else {
                    indiceTelegram = null
                }
                etapeTelegram = etape.ifEmpty { null }
                qr.remove(service)
            }
            "api" -> apiTelegram = v.optBoolean("set")
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
            "presence" -> recevoirPresence(v)
            "hello", "e2ee" -> {}
            "error" -> {
                val texte = v.chaine("text")
                if (v.chaine("where") == "search") Log.d("jasette", "search ${service.court} : erreur (${texte.length} car.)")
                if (v.chaine("where") == "react") annulerReactions()
                if (v.chaine("where") == "messages") {
                    chargement = null
                    demandesFil.removeAll { Service.de(it) == service }
                }
                if (texte.isNotEmpty()) erreur = "${service.court} : ${t(texte)}"
                // Telegram : le code ou le mot de passe refusé, on peut réessayer.
                if (service == Service.TELEGRAM && etapeTelegram == "attente") etapeTelegram = null
            }
            "pont_ferme" -> pontFerme(service)
            else -> nouvelle(v, service)
        }
    }

    private fun nouvelle(v: JSONObject, service: Service) {
        when (v.chaine("ev")) {
            "state" -> {
                val avant = etat(Service.GOOGLE)
                etats[Service.GOOGLE] = Etat.de(v.chaine("state"), avant)
                when (etat(Service.GOOGLE)) {
                    Etat.NON_ASSOCIE -> {
                        emoji = null
                        oublier(Service.GOOGLE)
                    }
                    Etat.PRET -> {
                        emoji = null
                        // Prêt : on demande la liste. Le pont l'envoie seul à son
                        // démarrage, mais un client d'un serveur Jasette branché
                        // plus tard ne la recevrait jamais (pas rejouée).
                        if (avant != Etat.PRET) {
                            commande(Service.GOOGLE, JSONObject().put("cmd", "conversations").put("count", 50))
                            filsAuPret(Service.GOOGLE)
                        }
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
                if (v.optBoolean("open") && !Marques.estPrivee(c.id)) aOuvrir = c.id
            }
            "avatar" -> {
                val conv = v.chaine("conv")
                val path = v.chaine("path")
                if (conv.isNotEmpty() && path.isNotEmpty()) {
                    avatars[conv] = path
                    CacheMarques.noterAvatar(conv)
                    CacheConvs.noterListes()
                }
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
                        CacheMarques.noterAvatar(conv)
                        CacheConvs.noterListes()
                        break
                    }
                }
            }
            "avatar_moi" -> monAvatar(Service.GOOGLE, v.chaine("path"))
            "media" -> {
                val id = v.chaine("media_id")
                demandes.remove(id)
                telechargements.remove(id)
                val path = v.chaine("path")
                if (path.isNotEmpty()) {
                    medias[id] = path
                    echecs.remove(id)
                    if (ouvrirApres.remove(id)) ouvrirFichier(id, path)
                    // Le fichier est gardé avec les messages de sa conversation.
                    for ((conv, liste) in messages) {
                        if (liste.any { m -> m.attachments.any { it.mediaId == id } }) CacheConvs.noterFil(conv)
                    }
                } else {
                    echecs[id] = true
                    ouvrirApres.remove(id)
                }
            }
            "messages" -> {
                val conv = v.chaine("conv")
                val arr = v.optJSONArray("items")
                val items = (0 until (arr?.length() ?: 0)).map { Msg.de(arr!!.getJSONObject(it)) }
                val older = v.optBoolean("older")
                // Le serveur Jasette répond d'abord depuis son propre cache (`cache:true`),
                // puis le service répond pour de vrai : les deux se fusionnent pareil.
                val duCache = v.optBoolean("cache")
                telechargerImages(items)
                val liste = messages[conv].orEmpty()
                if (older) {
                    messages[conv] = items.filter { m -> liste.none { it.id == m.id } } + liste
                    plusAnciens[conv] = v.optBoolean("more")
                } else {
                    val fusion = fusionner(liste, items, autorite = !duCache)
                    messages[conv] = fusion
                    // Des messages gardés plus vieux que la réponse : on pourra en demander d'autres.
                    val plusVieux = items.minOfOrNull { it.time }?.let { d -> fusion.any { !it.local && it.time < d } } ?: false
                    plusAnciens[conv] = v.optBoolean("more") || plusVieux
                    if (!duCache) {
                        fraiches.add(conv)
                        demandesFil.remove(conv)
                    }
                }
                if (chargement == conv && (older || !duCache || messages[conv].orEmpty().isNotEmpty())) chargement = null
                if (!older && visible && ouverte == conv) marquerLu(conv)
                CacheConvs.noterFil(conv)
            }
            "message" -> {
                val m = Msg.de(v.optJSONObject("item") ?: return)
                recevoirMessage(m)
            }
            "typing" -> recevoirEcrit(v)
            // Ponts Meta : une conversation supprimée (ici, sur un autre appareil ou dans l'app).
            "deleted" -> {
                val id = v.chaine("conv")
                if (id.isEmpty()) return
                if (convs.any { it.id == id }) remplacerConvs(convs.filter { it.id != id })
                if (archives.any { it.id == id }) remplacerArchives(archives.filter { it.id != id })
                messages.remove(id)
                CacheConvs.noterFil(id)
                // Une suppression explicite (ici ou ailleurs) : ses marques partent aussi.
                // C'est, avec Depot.supprimer, le seul retrait automatique de marques :
                // une conversation simplement absente (service coupé, liste rechargée) les garde.
                Marques.oublier(id)
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
                if (v.chaine("where") == "messages") {
                    chargement = null
                    demandesFil.clear()
                }
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
            .let { if (it >= 0) it else liste.indexOfFirst { x -> x.localPour(m) } }
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
        CacheConvs.noterFil(m.conv)
        // Le message est là : plus de « … ».
        if (nouveau && !m.fromMe) {
            if (ecrits.remove(m.conv) != null) Sons.ecritParti(m.conv)
            Sons.recu(m.conv, m.time)
        }

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
            } else if (!Marques.estPrivee(m.conv)) {
                // Liste privée : aucune notification, même neutre (rien ne trahit la conversation).
                notifier?.invoke(m, conv)
            }
        }
    }

    private fun remplacerConvs(liste: List<Conv>) {
        val triee = liste.sortedByDescending { it.time }
        convs.clear()
        convs.addAll(triee)
        // Les conversations marquées (Head, favoris, épingles) : gardées pour quand leur service sera absent.
        CacheMarques.noter(triee)
        CacheConvs.noterListes()
        // Le partage direct : les conversations récentes, republiées (groupé).
        Raccourcis.noter()
    }

    private fun remplacerArchives(liste: List<Conv>) {
        val triee = liste.sortedByDescending { it.time }
        archives.clear()
        archives.addAll(triee)
        CacheMarques.noter(triee)
        CacheConvs.noterListes()
    }

    /** Une conversation, dans la boîte de réception ou les archives. */
    fun conv(id: String): Conv? = convs.firstOrNull { it.id == id } ?: archives.firstOrNull { it.id == id }

    /** Archivée par Jasette (`Marques.archives`) : seulement pour les services qui n'archivent pas eux-mêmes. */
    fun archiveeJasette(id: String) = !Service.de(id).archiveNatif && Marques.estArchivee(id)

    /** Archivée, par son service ou par Jasette. */
    fun estArchivee(c: Conv) = c.archived || archives.any { it.id == c.id } || archiveeJasette(c.id)

    /** La boîte de réception (et les vues Favoris et par service) : sans les archives de Jasette. */
    val boite: List<Conv> get() = convs.filter { !archiveeJasette(it.id) && !Marques.estPrivee(it.id) }

    /** La vue Archives : celles des services et celles de Jasette, ensemble, les plus récentes d'abord. */
    val toutesArchives: List<Conv>
        get() = (archives + convs.filter { archiveeJasette(it.id) }).distinctBy { it.id }
            .filter { !Marques.estPrivee(it.id) }.sortedByDescending { it.time }

    /** La vue Privées : les conversations de la Liste privée (boîte et archives), les plus récentes d'abord. */
    val privees: List<Conv>
        get() = (convs + archives).filter { Marques.estPrivee(it.id) }.distinctBy { it.id }.sortedByDescending { it.time }

    /**
     * Un compte déconnecté : ses conversations disparaissent de la liste (et des
     * archives). Ses marques (Head, favoris, épingles) restent : elles s'affichent
     * depuis [CacheMarques].
     */
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

    /**
     * Ouvre une conversation : ses messages gardés s'affichent tout de suite,
     * et le service est interrogé une fois (depuis qu'il est prêt) ; sa réponse
     * se fusionne à l'écran. L'indicateur de chargement ne paraît que si l'on
     * n'a encore aucun message à montrer.
     */
    fun ouvrir(id: String) {
        ouverte = id
        val vide = messages[id].isNullOrEmpty()
        if (id !in fraiches) {
            if (vide) chargement = id
            demanderFil(id)
        }
        // Messages gardés, service pas encore prêt : « lu » partira quand il le sera (filsAuPret).
        if (!vide && pret(Service.de(id))) marquerLu(id)
    }

    private fun demanderFil(id: String) {
        if (!demandesFil.add(id)) return
        commande(JSONObject().put("cmd", "messages").put("conv", id).put("count", 60))
    }

    /**
     * Un service (re)devient prêt : ses réponses d'avant ne sont plus sûres
     * (des messages ont pu arriver pendant la coupure). La conversation ouverte
     * est redemandée ; les autres le seront à leur ouverture.
     */
    private fun filsAuPret(service: Service) {
        fraiches.removeAll { Service.de(it) == service }
        demandesFil.removeAll { Service.de(it) == service }
        val o = ouverte ?: return
        if (Service.de(o) == service) ouvrir(o)
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
        // Une conversation peut se trouver dans les deux listes (une liste du
        // service arrivée avant que l'archivage y soit pris en compte) : on la
        // retire des deux avant de la mettre d'un seul côté, sinon un geste
        // suivant la croyait encore « archivée » dans la boîte (et l'inverse).
        val c = (if (archiver) convs.firstOrNull { it.id == id } else archives.firstOrNull { it.id == id })
            ?: convs.firstOrNull { it.id == id } ?: archives.firstOrNull { it.id == id } ?: return
        val autres = convs.filter { it.id != id }
        val autresArch = archives.filter { it.id != id }
        if (archiver) {
            remplacerConvs(autres)
            remplacerArchives(autresArch + c.copy(archived = true))
        } else {
            remplacerArchives(autresArch)
            remplacerConvs(autres + c.copy(archived = false))
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
    /** Les services interrogés par la dernière recherche (null : tous). */
    private var servicesContacts: Set<Service>? = null

    /**
     * « À : » a changé : Messenger et Instagram cherchent parmi les amis et
     * contacts, Google parmi les contacts du téléphone (aussi par numéro),
     * WhatsApp et Signal parmi leurs contacts et groupes (aussi par numéro).
     */
    fun chercherContacts(texte: String, montre: Set<Service>? = null) {
        val q = texte.trim()
        if (q == requeteContacts && montre == servicesContacts) return
        requeteContacts = q
        servicesContacts = montre
        resultatsMeta.clear()
        numerosContacts.clear()
        if (q.length < 2) return
        val cmd = { JSONObject().put("cmd", "search").put("text", q) }
        val numero = estNumero(q)
        for (s in services) {
            if (!pret(s) || !(s.cherchePersonnes || s == Service.GOOGLE)) continue
            // La puce de service choisie : seulement ses ponts.
            if (montre != null && s !in montre) continue
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
     * Crée un groupe (commande `create_group`, §5.6 bis du protocole) avec des
     * personnes d'un même service ([membres] : ids sans préfixe). Le pont
     * répond par la conversation avec open, qui l'ouvre, ou par une erreur.
     */
    fun creerGroupe(service: Service, membres: List<String>, nom: String) {
        val m = JSONArray()
        membres.forEach { m.put(it) }
        commande(service, JSONObject().put("cmd", "create_group").put("members", m).put("name", nom.trim()))
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
        CacheConvs.noterFil(id)
        Marques.oublier(id)
    }

    /** Envoie un texte ; rend le tmp_id de la bulle locale (pour suivre l'envoi), ou null s'il n'y a rien à envoyer. */
    fun envoyer(conv: String, texte: String, emojiRapide: Boolean = false): String? {
        val t = texte.trim()
        if (t.isEmpty()) return null
        envoiSeq++
        val maintenant = System.currentTimeMillis()
        val tmp = "jasette_${maintenant}_$envoiSeq"
        val local = Msg(
            id = tmp, conv = conv, text = t, media = "", fromMe = true, sender = "", time = maintenant,
            status = "ENVOI", kind = "", tmpId = tmp, attachments = emptyList(), reactions = emptyList(),
        )
        messages[conv] = messages[conv].orEmpty() + local
        Sons.envoi(conv, emojiRapide)
        val cmd = JSONObject().put("cmd", "send").put("conv", conv).put("text", t).put("tmp_id", tmp)
        envois[tmp] = cmd.toString()
        commande(cmd)
        return tmp
    }

    // ---- Envoyer des fichiers ----

    /**
     * Envoie un fichier déjà copié dans le cache de l'app (`send_media`, comme
     * `envoyer_fichier` du bureau) : une bulle locale « Envoi… » le montre tout
     * de suite. En mode client, le serveur reçoit d'abord le fichier (`put`).
     * [texte] part avec le fichier (ponts Meta seulement ; Google l'envoie à part).
     */
    fun envoyerFichier(conv: String, fichier: File, mime: String, nom: String, texte: String, vignette: String?): String {
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
        Sons.envoi(conv, false)
        val cmd = JSONObject().put("cmd", "send_media").put("conv", conv).put("path", fichier.path)
            .put("tmp_id", tmp).put("mime", mime)
        if (texte.isNotEmpty()) cmd.put("text", texte)
        // commande() retire les préfixes sur place : on garde une copie intacte.
        envois[tmp] = cmd.toString()
        commande(cmd)
        return tmp
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
            CacheConvs.noterFil(conv)
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
        telechargements[p.mediaId] = true
        commande(
            JSONObject().put("cmd", "download").put("media_id", p.mediaId).put("key", p.key).put("mime", p.mime),
        )
    }

    /**
     * Un clic sur une pièce qui n'est pas une image (PDF, document…), envoyée
     * ou reçue : ouverte tout de suite si le fichier est là, sinon téléchargée
     * puis ouverte à son arrivée (`media`). Un échec se réessaie ainsi ; un
     * fichier disparu (copie d'envoi effacée) est redemandé.
     */
    fun ouvrirPiece(p: Piece) {
        val id = p.mediaId
        if (id.isEmpty()) return
        val chemin = medias[id]
        if (chemin != null && File(chemin).isFile) {
            Fichiers.ouvrir(appli, chemin, p.mime, p.name)
            return
        }
        if (chemin != null) medias.remove(id)
        // Une copie locale d'envoi (« local:… ») perdue : rien à demander aux ponts.
        if (id.startsWith("local:")) {
            echecs[id] = true
            return
        }
        echecs.remove(id)
        ouvrirApres.add(id)
        piecesOuvertes[id] = p
        telecharger(p)
    }

    /** Les pièces demandées par [ouvrirPiece] (leur type et leur nom, pour l'Intent). */
    private val piecesOuvertes = HashMap<String, Piece>()

    private fun ouvrirFichier(id: String, chemin: String) {
        val p = piecesOuvertes.remove(id)
        Fichiers.ouvrir(appli, chemin, p?.mime.orEmpty(), p?.name.orEmpty())
    }

    val versionAndroid get() = Build.VERSION.SDK_INT

    private const val PREFS_REGLAGES = "reglages"
    private const val CLE_SMS = "google_sms"
}
