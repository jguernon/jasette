package ca.jasette.android

import org.json.JSONArray
import org.json.JSONObject

/**
 * Les services. Les ids des services autres que Google portent un préfixe dans
 * l'interface, comme sur le bureau : « m: » pour Messenger, « i: » pour
 * Instagram, « w: » pour WhatsApp, « s: » pour Signal, « x: » pour X. Le
 * préfixe dit aussi à quel pont envoyer une commande.
 *
 * WhatsApp, Signal et X n'existent qu'en mode client : leurs ponts tournent
 * sur le serveur Jasette, jamais sur le téléphone (voir [serveurSeulement]).
 */
enum class Service(val titre: String, val court: String, val prefixe: String, val cle: String) {
    GOOGLE("Google Messages", "Google Messages", "", "google"),
    MESSENGER("Facebook Messenger", "Messenger", "m:", "messenger"),
    INSTAGRAM("Instagram", "Instagram", "i:", "instagram"),
    WHATSAPP("WhatsApp", "WhatsApp", "w:", "whatsapp"),
    SIGNAL("Signal", "Signal", "s:", "signal"),
    X("X", "X", "x:", "x");

    /** Un service préfixé (tous sauf Google) : ses nouvelles passent par `nouvelleMeta`. */
    val meta get() = this != GOOGLE

    /** Servi seulement par le serveur Jasette (aucun pont embarqué sur le téléphone). */
    val serveurSeulement get() = this == WHATSAPP || this == SIGNAL || this == X

    /** « Nouveau message » interroge-t-il ce pont (commande `search`) ? */
    val cherchePersonnes get() = this == MESSENGER || this == INSTAGRAM || this == WHATSAPP || this == SIGNAL

    /** Un numéro tapé se cherche-t-il aussi dans ce pont ? (pas chez Meta) */
    val chercheNumeros get() = this == GOOGLE || this == WHATSAPP || this == SIGNAL

    /**
     * La taille d'envoi maximale d'un fichier (octets) : environ 100 Mo pour
     * Google Messages (RCS), WhatsApp et Signal, 25 Mo pour les autres.
     */
    val limiteEnvoi: Long get() = if (this == GOOGLE || this == WHATSAPP || this == SIGNAL) 100L shl 20 else 25L shl 20

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
) {
    val service get() = Service.de(id)

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
) {
    /** Un envoi local pas encore confirmé par le pont. */
    val local get() = id == tmpId

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
                text = o.chaine("text"),
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
