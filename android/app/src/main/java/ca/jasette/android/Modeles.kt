package ca.jasette.android

import org.json.JSONArray
import org.json.JSONObject

/**
 * Les services. Les ids des services autres que Google portent un préfixe dans
 * l'interface, comme sur le bureau : « m: » pour Messenger, « i: » pour
 * Instagram, « w: » pour WhatsApp, « s: » pour Signal, « x: » pour X, « t: »
 * pour Telegram, « b: » pour Bluesky, « d: » pour Discord, « r: » pour Reddit,
 * « l: » pour LinkedIn. Le
 * préfixe dit aussi à quel pont envoyer une commande.
 *
 * WhatsApp, Signal, X, Telegram, Bluesky, Discord, Reddit et LinkedIn : leurs ponts tournent sur le serveur Jasette, ou
 * sur le téléphone (mode « Sur cet appareil ») quand l'APK les embarque
 * (voir [serveurSeulement] et Depot.services).
 */
enum class Service(val titre: String, val court: String, val prefixe: String, val cle: String) {
    GOOGLE("Google Messages", "Google Messages", "", "google"),
    MESSENGER("Facebook Messenger", "Messenger", "m:", "messenger"),
    INSTAGRAM("Instagram", "Instagram", "i:", "instagram"),
    WHATSAPP("WhatsApp", "WhatsApp", "w:", "whatsapp"),
    SIGNAL("Signal", "Signal", "s:", "signal"),
    X("X", "X", "x:", "x"),
    TELEGRAM("Telegram", "Telegram", "t:", "telegram"),
    BLUESKY("Bluesky", "Bluesky", "b:", "bluesky"),
    DISCORD("Discord", "Discord", "d:", "discord"),
    REDDIT("Reddit", "Reddit", "r:", "reddit"),
    LINKEDIN("LinkedIn", "LinkedIn", "l:", "linkedin");

    /** Un service préfixé (tous sauf Google) : ses nouvelles passent par `nouvelleMeta`. */
    val meta get() = this != GOOGLE

    /**
     * Le service archive lui-même (commande `archive` de son pont) : Google
     * Messages, WhatsApp et Telegram. Les autres passent par les archives de Jasette
     * (`Marques.archives`, partagées par le serveur), comme les SMS du
     * téléphone (le fournisseur SMS n'a pas d'archives).
     */
    val archiveNatif get() = (this == GOOGLE && !Depot.smsActif) || this == WHATSAPP || this == TELEGRAM

    /**
     * WhatsApp, Signal, X, Telegram, Bluesky, Discord, Reddit ou LinkedIn : servi par le serveur Jasette, ou sur cet appareil
     * seulement si son pont a été compilé dans l'APK (sinon, service absent).
     */
    val serveurSeulement get() = this == WHATSAPP || this == SIGNAL || this == X || this == TELEGRAM || this == BLUESKY || this == DISCORD || this == REDDIT || this == LINKEDIN

    /** « Nouveau message » interroge-t-il ce pont (commande `search`) ? */
    val cherchePersonnes get() = this == MESSENGER || this == INSTAGRAM || this == WHATSAPP || this == SIGNAL || this == TELEGRAM || this == BLUESKY || this == DISCORD || this == REDDIT || this == LINKEDIN

    /** Un numéro tapé se cherche-t-il aussi dans ce pont ? (pas chez Meta) */
    val chercheNumeros get() = this == GOOGLE || this == WHATSAPP || this == SIGNAL

    /**
     * La taille d'envoi maximale d'un fichier (octets) : environ 100 Mo pour
     * Google Messages (RCS), WhatsApp, Signal et Telegram, 10 Mo pour Discord
     * (compte sans Nitro), 25 Mo pour les autres.
     */
    val limiteEnvoi: Long get() = when {
        this == GOOGLE || this == WHATSAPP || this == SIGNAL || this == TELEGRAM -> 100L shl 20
        this == DISCORD -> 10L shl 20
        // Reddit : des images seulement, 20 Mo (100 Mo pour un GIF, vérifié par le pont).
        this == REDDIT -> 100L shl 20
        // LinkedIn : 20 Mo (vérifié par le pont).
        this == LINKEDIN -> 20L shl 20
        else -> 25L shl 20
    }

    /**
     * Le service prend-il des fichiers ? Bluesky refuse les pièces jointes en
     * message privé ; les SMS du téléphone n'envoient pas encore de MMS.
     */
    val fichiers get() = this != BLUESKY && !(this == GOOGLE && Depot.smsActif)

    /** Le texte peut-il partir avec un fichier (champ `text` de `send_media`) ? */
    val texteAvecFichier get() = meta

    companion object {
        /** Les services servis par le pont Meta (une copie du pont par service). */
        val META = listOf(MESSENGER, INSTAGRAM)

        /** Les services dont le pont tourne aussi sur le téléphone (mode « Sur cet appareil »). */
        val LOCAUX = listOf(GOOGLE, MESSENGER, INSTAGRAM)

        /** Les services préfixés, pour reconnaître un id. */
        private val PREFIXES = entries.filter { it.prefixe.isNotEmpty() }

        fun de(id: String): Service = PREFIXES.firstOrNull { id.startsWith(it.prefixe) } ?: GOOGLE

        /** Le service d'une clé (« google », « whatsapp »…), ou null. */
        fun deCle(cle: String): Service? = entries.firstOrNull { it.cle == cle }

        /** Le service préfixé d'un id, ou null (Google ou id sans préfixe connu). */
        fun prefixeDe(id: String): Service? = PREFIXES.firstOrNull { id.startsWith(it.prefixe) }
    }
}

/** Une conversation (mêmes champs que `Conversation` des ponts). */
data class Conv(
    val id: String,
    val name: String,
    val preview: String,
    val fromMe: Boolean,
    val time: Long,
    val unread: Boolean,
    val group: Boolean,
    val rcs: Boolean,
    val color: String,
    val members: List<String>,
    val readOnly: Boolean,
    val archived: Boolean,
    val e2ee: Boolean,
    /** Le numéro de l'autre (Google, conversation à deux) : pour appeler. */
    val number: String,
    /**
     * Un grand espace (PROTOCOLE.md §5.13) : salon d'un serveur Discord, canal
     * ou supergroupe Telegram, communauté ou chaîne WhatsApp, canal de
     * diffusion… Il va dans l'onglet Groupes ; dans la boîte de réception,
     * seulement selon [dansBoiteDeReception]. Seul ce champ décide du classement.
     */
    val large: Boolean = false,
    /** La sorte de grand espace (`kind` : server, channel, supergroup, community…), ou "". */
    val kind: String = "",
    /** Seuls les administrateurs y écrivent (groupe d'annonces, salon d'annonces). */
    val announce: Boolean = false,
    /** L'id, sans préfixe, de l'espace qui le contient (serveur Discord `g<guild>`, communauté WhatsApp), ou "". */
    val parent: String = "",
    /** Le nom de cet espace parent. */
    val parentName: String = "",
    /** La catégorie du salon (Discord), sa position, et la position du salon dans sa catégorie. */
    val section: String = "",
    val sectionOrder: Int = 0,
    val order: Int = 0,
    /** Les non-lus qui me mentionnent, quand le service les compte lui-même. */
    val mentions: Int = 0,
    /** Une demande de message pas encore acceptée (Reddit, LinkedIn) : « Accepter » ou « Ignorer ». */
    val request: Boolean = false,
) {
    val service get() = Service.de(id)

    /** Les mêmes champs que ceux du pont (pour le cache sur disque, relu par [de]). */
    fun json(): JSONObject {
        val o = JSONObject().put("id", id).put("name", name).put("preview", preview)
            .put("from_me", fromMe).put("time", time).put("unread", unread).put("group", group).put("rcs", rcs)
            .put("color", color).put("members", JSONArray(members)).put("read_only", readOnly)
            .put("archived", archived).put("e2ee", e2ee).put("number", number)
        // Les champs des grands espaces (§5.13), seulement s'ils sont là.
        if (large) o.put("large", true)
        if (kind.isNotEmpty()) o.put("kind", kind)
        if (announce) o.put("announce", true)
        if (parent.isNotEmpty()) o.put("parent", parent)
        if (parentName.isNotEmpty()) o.put("parent_name", parentName)
        if (section.isNotEmpty()) o.put("section", section)
        if (sectionOrder != 0) o.put("section_order", sectionOrder)
        if (order != 0) o.put("order", order)
        if (mentions != 0) o.put("mentions", mentions)
        if (request) o.put("request", true)
        return o
    }

    companion object {
        fun de(o: JSONObject) = Conv(
            id = o.chaine("id"),
            name = o.chaine("name"),
            preview = o.chaine("preview"),
            fromMe = o.optBoolean("from_me"),
            time = o.optLong("time"),
            unread = o.optBoolean("unread"),
            group = o.optBoolean("group"),
            rcs = o.optBoolean("rcs"),
            color = o.chaine("color"),
            members = o.optJSONArray("members").chaines(),
            readOnly = o.optBoolean("read_only"),
            archived = o.optBoolean("archived"),
            e2ee = o.optBoolean("e2ee"),
            number = o.chaine("number"),
            large = o.optBoolean("large"),
            kind = o.chaine("kind"),
            announce = o.optBoolean("announce"),
            parent = o.chaine("parent"),
            parentName = o.chaine("parent_name"),
            section = o.chaine("section"),
            sectionOrder = o.optInt("section_order"),
            order = o.optInt("order"),
            mentions = o.optInt("mentions").coerceAtLeast(0),
            request = o.optBoolean("request"),
        )
    }
}

/** Une pièce jointe. La clé (base64) est renvoyée telle quelle au pont. */
data class Piece(
    val mediaId: String,
    val key: String,
    val mime: String,
    val name: String,
    val size: Long,
    val w: Long,
    val h: Long,
) {
    val estImage get() = mime.startsWith("image/")
    val estAudio get() = mime.startsWith("audio/")

    fun json(): JSONObject = JSONObject().put("media_id", mediaId).put("key", key).put("mime", mime)
        .put("name", name).put("size", size).put("w", w).put("h", h)

    companion object {
        fun de(o: JSONObject) = Piece(
            mediaId = o.chaine("media_id"),
            key = o.chaine("key"),
            mime = o.chaine("mime"),
            name = o.chaine("name"),
            size = o.optLong("size"),
            w = o.optLong("w"),
            h = o.optLong("h"),
        )
    }
}

data class Reaction(val emoji: String, val sender: String, val fromMe: Boolean)

data class Msg(
    val id: String,
    val conv: String,
    val text: String,
    val media: String,
    val fromMe: Boolean,
    val sender: String,
    val time: Long,
    val status: String,
    val kind: String,
    val tmpId: String,
    val attachments: List<Piece>,
    val reactions: List<Reaction>,
    /** L'aperçu du lien fourni par le service (`link_preview`, PROTOCOLE.md §5.9). */
    val apercu: LienApercu? = null,
    /** Le message me mentionne (`mentions_me`, §5.13). */
    val mentionsMe: Boolean = false,
    /** Il répond à un de mes messages (`reply_to_me`, §5.13). */
    val replyToMe: Boolean = false,
    /** L'id de l'auteur chez le service, sans préfixe (`sender_id`, §5.13) ; sa photo : Depot.avatarsAuteurs. */
    val senderId: String = "",
) {
    /** Un envoi local pas encore confirmé par le pont. */
    val local get() = id == tmpId

    /**
     * Cet envoi local est-il le même que [m], le message confirmé ? Par tmp_id,
     * sinon (le service ne renvoie pas toujours notre tmp_id, ex. RCS) : de moi,
     * même texte, même nombre de pièces, à moins de 10 minutes d'écart.
     */
    fun localPour(m: Msg): Boolean {
        if (!local || m.local) return false
        if (m.tmpId.isNotEmpty() && tmpId == m.tmpId) return true
        return m.fromMe && text == m.text && attachments.size == m.attachments.size &&
            kotlin.math.abs(m.time - time) < 10 * 60 * 1000L
    }

    /** Les mêmes champs que ceux du pont (pour le cache sur disque, relu par [de]). */
    fun json(): JSONObject {
        val o = JSONObject().put("id", id).put("conv", conv).put("text", text).put("media", media)
            .put("from_me", fromMe).put("sender", sender).put("time", time).put("status", status).put("kind", kind)
        if (tmpId.isNotEmpty()) o.put("tmp_id", tmpId)
        if (attachments.isNotEmpty()) o.put("attachments", JSONArray(attachments.map { it.json() }))
        if (reactions.isNotEmpty()) {
            o.put(
                "reactions",
                JSONArray(reactions.map { JSONObject().put("emoji", it.emoji).put("sender", it.sender).put("from_me", it.fromMe) }),
            )
        }
        apercu?.let { o.put("link_preview", it.json()) }
        if (mentionsMe) o.put("mentions_me", true)
        if (replyToMe) o.put("reply_to_me", true)
        if (senderId.isNotEmpty()) o.put("sender_id", senderId)
        return o
    }

    /** L'aperçu dans la liste ou une notification. */
    fun apercu(): String = when {
        text.isNotEmpty() -> text
        attachments.any { it.estImage } -> t("📷 Photo")
        attachments.any { it.estAudio } -> t("🎤 Message vocal")
        attachments.isNotEmpty() || media.isNotEmpty() -> t("📎 Pièce jointe")
        else -> ""
    }

    companion object {
        fun de(o: JSONObject): Msg {
            val pieces = o.optJSONArray("attachments")
            val reactions = o.optJSONArray("reactions")
            return Msg(
                id = o.chaine("id"),
                conv = o.chaine("conv"),
                // Un message de service connu (`system`, PROTOCOLE.md §5.14) : son
                // texte français de référence, traduit à l'affichage (traduirePont).
                text = texteSysteme(o.chaine("system")) ?: o.chaine("text"),
                media = o.chaine("media"),
                fromMe = o.optBoolean("from_me"),
                sender = o.chaine("sender"),
                time = o.optLong("time"),
                status = o.chaine("status"),
                kind = o.chaine("kind"),
                tmpId = o.chaine("tmp_id"),
                attachments = (0 until (pieces?.length() ?: 0)).map { Piece.de(pieces!!.getJSONObject(it)) },
                reactions = (0 until (reactions?.length() ?: 0)).map {
                    val r = reactions!!.getJSONObject(it)
                    Reaction(r.chaine("emoji"), r.chaine("sender"), r.optBoolean("from_me"))
                }.filter { it.emoji.isNotEmpty() },
                apercu = LienApercu.de(o.optJSONObject("link_preview")),
                mentionsMe = o.optBoolean("mentions_me"),
                replyToMe = o.optBoolean("reply_to_me"),
                senderId = o.chaine("sender_id"),
            )
        }
    }
}

/** Le statut d'un message, en mots (comme `statut()` du bureau). */
fun statut(s: String): String = when {
    s == "ENVOI" -> t("Envoi…")
    s == "ÉCHEC" -> t("Échec")
    s.contains("FAILED") -> t("Échec")
    s.contains("READ") -> t("Lu")
    s.contains("DELIVERED") -> t("Distribué")
    s.contains("COMPLETE") -> t("Envoyé")
    s.contains("SENDING") || s.contains("YET_TO_SEND") -> t("Envoi…")
    else -> ""
}

/** La coche d'un de mes messages (comme WhatsApp). */
enum class Coche { AUCUNE, ENVOI, ENVOYE, DISTRIBUE, LU, ECHEC }

/**
 * Le `status` d'un message, tel que les ponts l'écrivent, ramené à une coche.
 * Google Messages : noms de `MessageStatusType` (OUTGOING_COMPLETE, _DELIVERED,
 * _DISPLAYED pour lu, _FAILED_*, _SENDING, _YET_TO_SEND, _RESENDING…).
 * Les autres ponts : OUTGOING_SENDING, _COMPLETE, _DELIVERED, _READ, et
 * INCOMING_* pour les messages reçus. Un service qui ne signale pas la
 * distribution passe de ✓ à ✓✓ accent : jamais de faux ✓✓ gris.
 * Dans un groupe, le pont passe à « lu » dès qu'une personne a lu.
 */
fun coche(m: Msg): Coche {
    if (!m.fromMe) return Coche.AUCUNE
    val s = m.status
    return when {
        s == "ÉCHEC" -> Coche.ECHEC
        s == "ENVOI" -> Coche.ENVOI
        s.startsWith("INCOMING") -> Coche.AUCUNE
        s.contains("FAILED") || s.contains("CANCELED") || s.contains("CANCELLED") -> Coche.ECHEC
        s.contains("READ") || s.contains("DISPLAYED") -> Coche.LU
        s.contains("DELIVERED") -> Coche.DISTRIBUE
        s.contains("SENDING") || s.contains("YET_TO_SEND") || s.contains("RESENDING") || s.contains("AWAITING_RETRY") -> Coche.ENVOI
        s.contains("COMPLETE") || s.contains("SENT") -> Coche.ENVOYE
        else -> Coche.AUCUNE
    }
}

/** L'état d'un pont, tel qu'annoncé par ses nouvelles « state ». */
enum class Etat(private val fr: String) {
    DEMARRAGE("Démarrage"),
    NON_ASSOCIE("Non connecté"),
    ASSOCIATION("Association"),
    CONNEXION("Connexion…"),
    PRET("Connecté"),
    TELEPHONE_HORS_LIGNE("Téléphone hors ligne"),
    RECONNEXION("Reconnexion…"),
    ARRETE("Pont arrêté");

    /** En mots, dans la langue de l'interface. */
    val texte get() = t(fr)

    val associe get() = this == CONNEXION || this == PRET || this == TELEPHONE_HORS_LIGNE || this == RECONNEXION

    companion object {
        fun de(s: String, avant: Etat): Etat = when (s) {
            "unpaired" -> NON_ASSOCIE
            "pairing" -> ASSOCIATION
            "connecting" -> CONNEXION
            "ready" -> PRET
            "phone_offline" -> TELEPHONE_HORS_LIGNE
            "reconnecting" -> RECONNEXION
            else -> avant
        }
    }
}

/** Une chaîne d'un objet JSON : "" si absente ou null (optString rendrait « null »). */
fun JSONObject.chaine(k: String): String {
    val v = opt(k)
    return if (v == null || v == JSONObject.NULL) "" else v.toString()
}

fun JSONArray?.chaines(): List<String> = if (this == null) emptyList() else (0 until length()).mapNotNull { i -> opt(i).takeIf { it != null && it != JSONObject.NULL }?.toString() }

/** Ajoute le préfixe du service aux ids d'une nouvelle de son pont Meta (comme `prefixer_meta`). */
fun prefixerMeta(v: JSONObject, service: Service) {
    val prefixe = service.prefixe
    fun ids(x: Any?, dansItem: Boolean) {
        when (x) {
            is JSONObject -> for (k in x.keys().asSequence().toList()) {
                val valeur = x.get(k)
                val cle = k == "conv" || k == "media_id" || (dansItem && k == "id")
                if (cle && valeur is String && valeur.isNotEmpty() && !valeur.startsWith(prefixe)) {
                    x.put(k, prefixe + valeur)
                } else {
                    ids(valeur, k == "item" || k == "items")
                }
            }
            is JSONArray -> for (i in 0 until x.length()) ids(x.get(i), dansItem)
        }
    }
    ids(v, false)
}

/** Le service préfixé d'une commande (et on retire alors les préfixes), ou null pour Google. */
fun deprefixerMeta(v: JSONObject): Service? {
    var service: Service? = null
    for (k in listOf("conv", "media_id", "msg", "id")) {
        val s = v.opt(k) as? String ?: continue
        val sv = Service.prefixeDe(s) ?: continue
        v.put(k, s.removePrefix(sv.prefixe))
        service = sv
    }
    return service
}

/** Un numéro de téléphone tapé (comme `est_numero` du bureau). */
fun estNumero(s: String): Boolean {
    val chiffres = s.count { it.isDigit() }
    return chiffres >= 3 && s.all { it.isDigit() || it in " +-()." }
}

/** Pour chercher : minuscules et sans accents (comme `plier` du bureau). */
fun plier(s: String): String =
    java.text.Normalizer.normalize(s.lowercase(), java.text.Normalizer.Form.NFD).replace(Regex("\\p{Mn}+"), "")
