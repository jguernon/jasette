package ca.jasette.android

import org.json.JSONArray
import org.json.JSONObject

/** Une rangée de `Telephony.Sms`, telle que lue par [PontSms]. */
data class LigneSms(
    val id: Long,
    val thread: Long,
    val adresse: String,
    val corps: String,
    /** En millisecondes. */
    val date: Long,
    /** `Telephony.Sms.TYPE` : 1 reçu, 2 envoyé, 3 brouillon, 4 boîte d'envoi, 5 échec, 6 en file. */
    val type: Int,
    /** `Telephony.Sms.STATUS` : -1 aucun, 0 distribué, 32 en attente, 64 échec. */
    val statut: Int,
)

/** Une pièce d'un MMS (`content://mms/<id>/part`), hors texte et SMIL. */
data class PieceMms(val id: Long, val mime: String, val nom: String)

/** Une rangée de `Telephony.Mms`, avec son texte et ses pièces. */
data class LigneMms(
    val id: Long,
    val thread: Long,
    /** En secondes (c'est ainsi que les MMS sont datés). */
    val dateSec: Long,
    /** `Telephony.Mms.MESSAGE_BOX` : 1 reçu, 2 envoyé, 3 brouillon, 4 boîte d'envoi, 5 échec. */
    val boite: Int,
    /** L'adresse de l'expéditeur (reçus seulement). */
    val de: String,
    val texte: String,
    val pieces: List<PieceMms>,
)

/**
 * La conversion des SMS et MMS du téléphone vers le protocole des ponts
 * (serveur/PROTOCOLE.md, mêmes champs que `Conv` et `Msg`). Rien d'Android
 * ici : testé sur la JVM (SmsJsonTest).
 *
 * Les ids : « sms-<thread_id> » pour une conversation, « sms.<_id> » et
 * « mms.<_id> » pour un message, « mmspart-<_id> » pour une pièce, et
 * « smsloc-<tmp_id> » pour un envoi qu'Android n'a pas écrit dans le
 * fournisseur SMS (voir [PontSms]). Aucun ne porte un préfixe de service
 * (« m: », « s: »…) : `Service.de` les range donc avec Google Messages.
 */
object SmsJson {
    const val PREFIXE_CONV = "sms-"
    const val PREFIXE_PIECE = "mmspart-"
    const val PREFIXE_LOCAL = "smsloc-"

    fun idConv(thread: Long) = "$PREFIXE_CONV$thread"

    /** Le thread_id d'une conversation SMS, ou null. */
    fun threadDe(conv: String): Long? = if (conv.startsWith(PREFIXE_CONV)) conv.removePrefix(PREFIXE_CONV).toLongOrNull() else null

    fun idSms(id: Long) = "sms.$id"
    fun idMms(id: Long) = "mms.$id"
    fun idPiece(id: Long) = "$PREFIXE_PIECE$id"

    /** Le _id d'une pièce MMS, ou null. */
    fun pieceDe(mediaId: String): Long? =
        if (mediaId.startsWith(PREFIXE_PIECE)) mediaId.removePrefix(PREFIXE_PIECE).toLongOrNull() else null

    /** Un SMS envoyé (ou en cours d'envoi) par moi ? */
    fun smsDeMoi(type: Int) = type != 1

    /** Le statut du protocole d'un SMS, ou null pour un brouillon (jamais montré). */
    fun statutSms(type: Int, statut: Int): String? = when (type) {
        1 -> "INCOMING_COMPLETE"
        2 -> if (statut == 0) "OUTGOING_DELIVERED" else if (statut >= 64) "OUTGOING_FAILED" else "OUTGOING_COMPLETE"
        4, 6 -> "OUTGOING_SENDING"
        5 -> "OUTGOING_FAILED"
        else -> null
    }

    fun mmsDeMoi(boite: Int) = boite != 1

    fun statutMms(boite: Int): String? = when (boite) {
        1 -> "INCOMING_COMPLETE"
        2 -> "OUTGOING_COMPLETE"
        4 -> "OUTGOING_SENDING"
        5 -> "OUTGOING_FAILED"
        else -> null
    }

    /** Ce qui change l'affichage d'un message déjà vu (pour l'observateur). */
    fun signatureSms(l: LigneSms) = "${l.type}/${l.statut}"
    fun signatureMms(l: LigneMms) = "${l.boite}"

    /** Un SMS en `Msg` du protocole ; [nom] : le nom de l'expéditeur (reçus), sinon son numéro. */
    fun msgSms(l: LigneSms, nom: String?): JSONObject? {
        val statut = statutSms(l.type, l.statut) ?: return null
        val moi = smsDeMoi(l.type)
        return JSONObject().put("id", idSms(l.id)).put("conv", idConv(l.thread)).put("text", l.corps)
            .put("media", "").put("from_me", moi).put("sender", if (moi) "" else (nom ?: l.adresse))
            .put("time", l.date).put("status", statut).put("kind", "")
    }

    /** Un MMS en `Msg` : son texte, et ses pièces (images, vidéos, sons…) en `attachments`. */
    fun msgMms(l: LigneMms, nom: String?): JSONObject? {
        val statut = statutMms(l.boite) ?: return null
        val moi = mmsDeMoi(l.boite)
        val o = JSONObject().put("id", idMms(l.id)).put("conv", idConv(l.thread)).put("text", l.texte)
            .put("media", "").put("from_me", moi).put("sender", if (moi) "" else (nom ?: l.de))
            .put("time", l.dateSec * 1000).put("status", statut).put("kind", "")
        if (l.pieces.isNotEmpty()) {
            o.put(
                "attachments",
                JSONArray(
                    l.pieces.map {
                        JSONObject().put("media_id", idPiece(it.id)).put("key", "").put("mime", it.mime)
                            .put("name", it.nom).put("size", 0).put("w", 0).put("h", 0)
                    },
                ),
            )
        }
        return o
    }

    /** L'aperçu d'un MMS pour la liste (le texte, sinon la nature de sa première pièce). */
    fun apercuMms(l: LigneMms): String = when {
        l.texte.isNotEmpty() -> l.texte
        l.pieces.any { it.mime.startsWith("image/") } -> "📷 Photo"
        l.pieces.any { it.mime.startsWith("audio/") } -> "🎤 Message vocal"
        l.pieces.isNotEmpty() -> "📎 Pièce jointe"
        else -> ""
    }

    /**
     * Une conversation (un fil SMS/MMS) en `Conv` du protocole. [adresses] et
     * [noms] vont ensemble (le nom d'un contact, sinon le numéro). Un groupe
     * est en lecture seule : l'envoi en groupe (MMS) n'est pas fait.
     */
    fun conv(
        thread: Long,
        adresses: List<String>,
        noms: List<String>,
        apercu: String,
        deMoi: Boolean,
        temps: Long,
        nonLu: Boolean,
    ): JSONObject {
        val groupe = adresses.size > 1
        val nom = noms.filter { it.isNotEmpty() }.joinToString(", ").ifEmpty { adresses.joinToString(", ") }
        return JSONObject().put("id", idConv(thread)).put("name", nom).put("preview", apercu)
            .put("from_me", deMoi).put("time", temps).put("unread", nonLu).put("group", groupe).put("rcs", false)
            .put("color", "").put("members", JSONArray(noms)).put("read_only", groupe || adresses.isEmpty())
            .put("archived", false).put("e2ee", false).put("number", if (groupe) "" else adresses.firstOrNull().orEmpty())
    }

    /**
     * Un envoi gardé par Jasette (Android ne l'a pas écrit dans le fournisseur
     * SMS) : un `Msg` de moi, envoyé, avec son tmp_id.
     */
    fun msgLocal(tmp: String, thread: Long, texte: String, temps: Long): JSONObject =
        JSONObject().put("id", "$PREFIXE_LOCAL$tmp").put("conv", idConv(thread)).put("text", texte).put("media", "")
            .put("from_me", true).put("sender", "").put("time", temps).put("status", "OUTGOING_COMPLETE")
            .put("kind", "").put("tmp_id", tmp)

    /**
     * Le même envoi, déjà dans le fournisseur SMS ? (de moi, même texte, à
     * moins de 2 minutes) : alors la copie de Jasette ne sert plus.
     */
    fun memeEnvoi(local: JSONObject, sms: LigneSms): Boolean =
        smsDeMoi(sms.type) && sms.corps == local.optString("text") &&
            kotlin.math.abs(sms.date - local.optLong("time")) < 2 * 60 * 1000L
}
