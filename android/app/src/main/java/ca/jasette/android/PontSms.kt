package ca.jasette.android

import android.Manifest
import android.app.Activity
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.database.ContentObserver
import android.database.Cursor
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.ContactsContract
import android.provider.Telephony
import android.telephony.SmsManager
import android.util.Log
import android.webkit.MimeTypeMap
import androidx.core.content.ContextCompat
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.concurrent.Executors

/**
 * « SMS du téléphone » : Google Messages remplacé, en mode autonome, par les
 * SMS et MMS de l'appareil lui-même (fournisseurs `Telephony.Sms` et
 * `Telephony.Mms`), sans compte Google. Il parle le même protocole que les
 * ponts Go (une commande JSON, une nouvelle JSON ; serveur/PROTOCOLE.md) :
 * la liste, le fil, les notifications et la Liste privée ne voient pas la
 * différence. Les ids sont décrits dans [SmsJson].
 *
 * - Lecture : une conversation par fil (`thread_id`) ; les noms viennent des
 *   contacts du téléphone (PhoneLookup), sinon le numéro.
 * - Nouveautés : un ContentObserver sur content://mms-sms/ (pas seulement
 *   SMS_RECEIVED) ; chaque changement relit les messages récents et émet
 *   `conversation` puis `message` pour ce qui est nouveau ou a changé.
 * - Envoi : SmsManager.sendMultipartTextMessage, vers l'adresse du fil (pas
 *   les groupes). Quand Jasette n'est pas l'app SMS par défaut, Android écrit
 *   normalement lui-même l'envoi dans le fournisseur (API 19+), mais certains
 *   téléphones ne le font pas : si, quelques secondes après « envoyé »,
 *   aucune rangée n'y correspond, Jasette garde sa propre copie (en mémoire et
 *   dans filesDir/sms/envoyes.json) et la montre dans le fil. Ces copies ne
 *   sont alors visibles que dans Jasette, pas dans l'app Messages.
 * - MMS : texte et pièces reçus (images téléchargées et affichées) ; l'envoi
 *   de MMS (photos, groupes) n'est pas fait.
 * - Les messages RCS (« chat ») ne passent pas par ces fournisseurs : ils
 *   restent dans l'app Messages de Google.
 * - Marquer lu : gardé par Jasette seulement (le fournisseur n'est modifiable
 *   que par l'app SMS par défaut). Réactions et suppression : refusées.
 *
 * Tout le travail se fait sur un seul fil ([travail]) ; rien de personnel
 * (numéro, texte) n'est écrit dans les journaux.
 */
class PontSms(ctx: Context, private val surNouvelle: (JSONObject) -> Unit) : Pont {
    override val nom = "sms"
    private val appli = ctx.applicationContext
    private val cr = appli.contentResolver
    private val travail = Executors.newSingleThreadExecutor { r -> Thread(r, "pont-sms") }
    private val principal = Handler(Looper.getMainLooper())
    @Volatile private var arrete = false
    private var observateur: ContentObserver? = null
    private var recepteur: BroadcastReceiver? = null

    // ---- L'état, touché seulement sur le fil [travail] ----

    /** id de message → signature (type et statut) déjà vue. */
    private val vus = HashMap<String, String>()
    /** id de rangée SMS → tmp_id de l'envoi de Jasette qu'elle représente. */
    private val tmpDe = HashMap<String, String>()
    private val envois = LinkedHashMap<String, Envoi>()
    private val noms = HashMap<String, String>()
    private val adresses = HashMap<Long, String>()
    private val lus = appli.getSharedPreferences("sms_lus", Context.MODE_PRIVATE)
    private val fichierLocaux = File(appli.filesDir, "sms/envoyes.json")
    /** Les envois gardés par Jasette : `Msg` (JSON) avec le champ « thread ». */
    private val locaux = ArrayList<JSONObject>()

    private class Envoi(
        val tmp: String, val thread: Long, val texte: String, val temps: Long, val parts: Int,
        var recus: Int = 0, var echec: Boolean = false, var ligne: String? = null,
    )

    private val relire = Runnable { executer { changement() } }

    init {
        executer { demarrer() }
    }

    private fun executer(f: () -> Unit) {
        if (arrete) return
        try {
            travail.execute {
                if (arrete) return@execute
                try {
                    f()
                } catch (e: Exception) {
                    Log.w("jasette-sms", "erreur : ${e.javaClass.simpleName} ${e.message?.take(120)}")
                }
            }
        } catch (_: Exception) {
        }
    }

    private fun emettre(v: JSONObject) {
        if (!arrete) surNouvelle(v)
    }

    private fun etat(s: String) = emettre(JSONObject().put("ev", "state").put("state", s))

    private fun erreur(ou: String, texte: String) = emettre(JSONObject().put("ev", "error").put("where", ou).put("text", texte))

    private fun permis(p: String) = appli.checkSelfPermission(p) == PackageManager.PERMISSION_GRANTED

    private fun demarrer() {
        etat("connecting")
        if (!permis(Manifest.permission.READ_SMS)) {
            etat("unpaired")
            erreur("sms", "SMS : l'accès aux messages texte n'est pas accordé. Touche « Utiliser les SMS du téléphone » pour le redonner.")
            return
        }
        lireLocaux()
        principal.post {
            if (arrete) return@post
            val o = object : ContentObserver(principal) {
                override fun onChange(selfChange: Boolean) = planifier(700)
            }
            try {
                cr.registerContentObserver(Uri.parse("content://mms-sms/"), true, o)
                observateur = o
            } catch (e: Exception) {
                Log.w("jasette-sms", "observateur : ${e.message}")
            }
            val r = object : BroadcastReceiver() {
                override fun onReceive(c: Context, i: Intent) {
                    val tmp = i.getStringExtra(EXTRA_TMP) ?: return
                    val ok = resultCode == Activity.RESULT_OK
                    executer { partieEnvoyee(tmp, ok) }
                }
            }
            ContextCompat.registerReceiver(appli, r, IntentFilter(ACTION_ENVOYE), ContextCompat.RECEIVER_NOT_EXPORTED)
            recepteur = r
        }
        // Ce qui existe déjà n'est pas « nouveau » : rien ne s'émet au premier passage.
        scanner(emettre = false)
        etat("ready")
    }

    /** Un changement dans content://mms-sms/ : relu après [attente] ms (les rafales se regroupent). */
    private fun planifier(attente: Long) {
        if (arrete) return
        principal.removeCallbacks(relire)
        principal.postDelayed(relire, attente)
    }

    override fun envoyer(commande: JSONObject) {
        val c = JSONObject(commande.toString())
        executer { traiter(c) }
    }

    override fun vivant(): Boolean = !arrete

    override fun arreter() {
        if (arrete) return
        arrete = true
        principal.removeCallbacks(relire)
        principal.post {
            observateur?.let { runCatching { cr.unregisterContentObserver(it) } }
            observateur = null
            recepteur?.let { runCatching { appli.unregisterReceiver(it) } }
            recepteur = null
        }
        travail.shutdown()
    }

    // ---- Les commandes ----

    private fun traiter(c: JSONObject) {
        when (c.chaine("cmd")) {
            "conversations" -> {
                if (c.chaine("folder") == "archive") {
                    // Pas d'archives dans le fournisseur SMS (celles de Jasette passent par Marques).
                    emettre(JSONObject().put("ev", "conversations").put("folder", "archive").put("items", JSONArray()))
                } else {
                    emettre(JSONObject().put("ev", "conversations").put("items", JSONArray(listeConvs(c.optInt("count", 50)))))
                }
            }
            "messages" -> messages(c.chaine("conv"), c.optInt("count", 60), c.optLong("before_time"))
            "send" -> envoyerSms(c.chaine("conv"), c.chaine("text"), c.chaine("tmp_id"))
            "send_media" -> {
                emettre(JSONObject().put("ev", "sent").put("ok", false).put("tmp_id", c.chaine("tmp_id")))
                erreur("send_media", "Envoi de photos : pas encore en mode SMS")
            }
            "download" -> telecharger(c.chaine("media_id"), c.chaine("mime"))
            "read" -> marquerLu(c.chaine("conv"))
            "search" -> chercher(c.chaine("text"))
            "new" -> nouvelleConv(c.chaine("number"))
            "react" -> erreur("react", "Réactions : pas en mode SMS")
            "delete" -> {
                erreur("delete", "Supprimer : seulement dans l'app SMS par défaut")
                emettre(JSONObject().put("ev", "conversations").put("items", JSONArray(listeConvs(50))))
            }
            // Rien à faire : pas de compte, pas d'état de saisie ni d'archives côté SMS.
            else -> {}
        }
    }

    // ---- Lecture ----

    private fun requete(uri: Uri, colonnes: Array<String>?, selection: String?, args: Array<String>?, tri: String?): Cursor? =
        try {
            cr.query(uri, colonnes, selection, args, tri)
        } catch (e: Exception) {
            Log.w("jasette-sms", "requête ${uri.authority}${uri.path} : ${e.javaClass.simpleName}")
            null
        }

    private fun Cursor.chaine(col: String): String {
        val i = getColumnIndex(col)
        return if (i < 0 || isNull(i)) "" else getString(i) ?: ""
    }

    private fun Cursor.long(col: String): Long {
        val i = getColumnIndex(col)
        return if (i < 0 || isNull(i)) 0 else getLong(i)
    }

    private fun Cursor.entier(col: String, defaut: Int = 0): Int {
        val i = getColumnIndex(col)
        return if (i < 0 || isNull(i)) defaut else getInt(i)
    }

    private val uriFils = Uri.parse("content://mms-sms/conversations?simple=true")

    /** Les fils, les plus récents d'abord. */
    private fun listeConvs(count: Int): List<JSONObject> {
        val max = count.coerceAtLeast(100)
        val sortie = ArrayList<JSONObject>()
        requete(uriFils, null, null, null, "date DESC")?.use { c ->
            while (c.moveToNext() && sortie.size < max) {
                if (c.getColumnIndex("message_count") >= 0 && c.long("message_count") == 0L) continue
                convDe(c.long("_id"), c.chaine("recipient_ids"), c.entier("read", 1) == 0)?.let { sortie.add(it) }
            }
        }
        return sortie
    }

    /** Le fil [thread], même vide (nouvelle conversation), ou null s'il n'existe pas. */
    private fun convPour(thread: Long): JSONObject? {
        requete(uriFils, null, "_id=?", arrayOf(thread.toString()), null)?.use { c ->
            if (c.moveToFirst()) return convDe(thread, c.chaine("recipient_ids"), c.entier("read", 1) == 0)
        }
        return null
    }

    private fun adressesDe(recipientIds: String): List<String> =
        recipientIds.split(' ').mapNotNull { it.trim().toLongOrNull() }.mapNotNull { adresse(it) }

    private fun destinatairesDe(thread: Long): List<String> {
        requete(uriFils, arrayOf("_id", "recipient_ids"), "_id=?", arrayOf(thread.toString()), null)?.use { c ->
            if (c.moveToFirst()) return adressesDe(c.chaine("recipient_ids"))
        }
        return emptyList()
    }

    private fun convDe(thread: Long, recipientIds: String, nonLuSysteme: Boolean): JSONObject? {
        val adr = adressesDe(recipientIds)
        val d = dernier(thread)
        val lu = lus.getLong(thread.toString(), 0)
        val temps = d?.temps ?: 0L
        val nonLu = nonLuSysteme && d != null && !d.deMoi && d.temps > lu
        return SmsJson.conv(thread, adr, adr.map { nomDe(it) }, d?.apercu.orEmpty(), d?.deMoi ?: false, temps, nonLu)
    }

    private class Dernier(val apercu: String, val deMoi: Boolean, val temps: Long)

    /** Le dernier message d'un fil (SMS, MMS, ou envoi gardé par Jasette). */
    private fun dernier(thread: Long): Dernier? {
        var d: Dernier? = null
        requete(
            Telephony.Sms.CONTENT_URI, arrayOf("_id", "body", "date", "type"),
            "thread_id=? AND type<>3", arrayOf(thread.toString()), "date DESC",
        )?.use { c ->
            if (c.moveToFirst()) d = Dernier(c.chaine("body"), SmsJson.smsDeMoi(c.entier("type")), c.long("date"))
        }
        requete(
            Telephony.Mms.CONTENT_URI, arrayOf("_id", "date", "msg_box"),
            "thread_id=? AND msg_box<>3", arrayOf(thread.toString()), "date DESC",
        )?.use { c ->
            if (c.moveToFirst()) {
                val temps = c.long("date") * 1000
                if (d == null || temps > d.temps) {
                    val m = lireMms(c.long("_id"), thread, c.long("date"), c.entier("msg_box"), avecAdresse = false)
                    d = Dernier(SmsJson.apercuMms(m), SmsJson.mmsDeMoi(m.boite), temps)
                }
            }
        }
        for (l in locaux) {
            if (l.optLong("thread") != thread) continue
            val temps = l.optLong("time")
            if (d == null || temps > d.temps) d = Dernier(l.optString("text"), true, temps)
        }
        return d
    }

    private fun lireSms(c: Cursor) = LigneSms(
        id = c.long("_id"), thread = c.long("thread_id"), adresse = c.chaine("address"), corps = c.chaine("body"),
        date = c.long("date"), type = c.entier("type"), statut = c.entier("status", -1),
    )

    private val colonnesSms = arrayOf("_id", "thread_id", "address", "body", "date", "type", "status")
    private val colonnesMms = arrayOf("_id", "thread_id", "date", "msg_box")

    /** Un MMS : ses parties (texte, pièces) et, s'il est reçu, son expéditeur. */
    private fun lireMms(id: Long, thread: Long, dateSec: Long, boite: Int, avecAdresse: Boolean = true): LigneMms {
        val textes = ArrayList<String>()
        val pieces = ArrayList<PieceMms>()
        requete(Uri.parse("content://mms/$id/part"), arrayOf("_id", "ct", "text", "name", "cl"), null, null, null)?.use { c ->
            while (c.moveToNext()) {
                val mime = c.chaine("ct").lowercase()
                when {
                    mime == "application/smil" -> {}
                    mime == "text/plain" -> c.chaine("text").let { if (it.isNotEmpty()) textes.add(it) }
                    mime.isNotEmpty() -> pieces.add(PieceMms(c.long("_id"), mime, c.chaine("name").ifEmpty { c.chaine("cl") }))
                }
            }
        }
        var de = ""
        if (avecAdresse && boite == 1) {
            // 137 : l'adresse « From » (PduHeaders.FROM).
            requete(Uri.parse("content://mms/$id/addr"), arrayOf("address", "type"), "type=137", null, null)?.use { c ->
                if (c.moveToFirst()) de = c.chaine("address")
            }
        }
        return LigneMms(id, thread, dateSec, boite, de, textes.joinToString("\n"), pieces)
    }

    /** L'adresse d'un destinataire (table canonical_addresses), gardée en mémoire. */
    private fun adresse(id: Long): String? {
        adresses[id]?.let { return it }
        requete(Uri.parse("content://mms-sms/canonical-address/$id"), null, null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                val a = c.chaine("address")
                if (a.isNotEmpty()) {
                    adresses[id] = a
                    return a
                }
            }
        }
        return null
    }

    /** Le nom d'un numéro parmi les contacts du téléphone (si permis), sinon le numéro. */
    private fun nomDe(adresse: String): String {
        if (adresse.isEmpty()) return ""
        noms[adresse]?.let { return it }
        var nom = adresse
        if (permis(Manifest.permission.READ_CONTACTS)) {
            val uri = Uri.withAppendedPath(ContactsContract.PhoneLookup.CONTENT_FILTER_URI, Uri.encode(adresse))
            requete(uri, arrayOf(ContactsContract.PhoneLookup.DISPLAY_NAME), null, null, null)?.use { c ->
                if (c.moveToFirst()) c.chaine(ContactsContract.PhoneLookup.DISPLAY_NAME).let { if (it.isNotEmpty()) nom = it }
            }
        }
        noms[adresse] = nom
        return nom
    }

    /** `messages` : les [count] derniers messages du fil (avant [avant], en ms, si > 0), du plus vieux au plus récent. */
    private fun messages(conv: String, count: Int, avant: Long) {
        val thread = SmsJson.threadDe(conv)
        if (thread == null) {
            emettre(JSONObject().put("ev", "messages").put("conv", conv).put("items", JSONArray()).put("more", false))
            return
        }
        val n = count.coerceIn(1, 500)
        val lignes = ArrayList<Pair<Long, () -> JSONObject?>>()
        val smsVus = ArrayList<LigneSms>()
        var sel = "thread_id=? AND type<>3"
        var args = arrayOf(thread.toString())
        if (avant > 0) {
            sel += " AND date<?"
            args += avant.toString()
        }
        requete(Telephony.Sms.CONTENT_URI, colonnesSms, sel, args, "date DESC")?.use { c ->
            while (c.moveToNext() && smsVus.size <= n) {
                val l = lireSms(c)
                smsVus.add(l)
                lignes.add(l.date to { SmsJson.msgSms(l, nomDe(l.adresse)) })
            }
        }
        var selM = "thread_id=? AND msg_box<>3"
        var argsM = arrayOf(thread.toString())
        if (avant > 0) {
            selM += " AND date<?"
            argsM += (avant / 1000).toString()
        }
        var nMms = 0
        requete(Telephony.Mms.CONTENT_URI, colonnesMms, selM, argsM, "date DESC")?.use { c ->
            while (c.moveToNext() && nMms <= n) {
                nMms++
                val id = c.long("_id")
                val date = c.long("date")
                val boite = c.entier("msg_box")
                lignes.add(date * 1000 to {
                    val m = lireMms(id, thread, date, boite)
                    SmsJson.msgMms(m, nomDe(m.de))
                })
            }
        }
        lignes.sortByDescending { it.first }
        val plus = lignes.size > n
        val gardees = lignes.take(n)
        val debut = if (plus) gardees.lastOrNull()?.first ?: 0L else 0L
        val items = ArrayList<JSONObject>()
        for ((_, f) in gardees) f()?.let { items.add(it) }
        // Les envois gardés par Jasette dans la même période (ceux qu'Android a fini par écrire partent).
        nettoyerLocaux(thread, smsVus)
        for (l in locaux) {
            if (l.optLong("thread") != thread) continue
            val t = l.optLong("time")
            if (t >= debut && (avant <= 0 || t < avant)) items.add(JSONObject(l.toString()).apply { remove("thread") })
        }
        items.sortBy { it.optLong("time") }
        val v = JSONObject().put("ev", "messages").put("conv", conv).put("items", JSONArray(items)).put("more", plus)
        if (avant > 0) v.put("older", true)
        emettre(v)
    }

    // ---- Les nouveautés (observateur) ----

    /** Relit les messages récents ; émet ce qui est nouveau ou a changé, sinon la liste (lu ailleurs, supprimé…). */
    private fun changement() {
        if (!scanner(emettre = true)) {
            emettre(JSONObject().put("ev", "conversations").put("items", JSONArray(listeConvs(50))))
        }
    }

    /**
     * Les 40 SMS et 20 MMS les plus récents (par _id) : ceux dont la signature
     * a changé depuis le dernier passage. Rend vrai si quelque chose s'est émis.
     */
    private fun scanner(emettre: Boolean): Boolean {
        val nouveaux = ArrayList<JSONObject>()
        val fils = LinkedHashSet<Long>()
        requete(Telephony.Sms.CONTENT_URI, colonnesSms, "type<>3", null, "_id DESC")?.use { c ->
            var i = 0
            while (c.moveToNext() && i++ < 40) {
                val l = lireSms(c)
                val id = SmsJson.idSms(l.id)
                val sig = SmsJson.signatureSms(l)
                if (vus[id] == sig) continue
                vus[id] = sig
                if (!emettre) continue
                val m = SmsJson.msgSms(l, nomDe(l.adresse)) ?: continue
                if (SmsJson.smsDeMoi(l.type)) associerEnvoi(id, l)?.let { m.put("tmp_id", it) }
                nouveaux.add(m)
                fils.add(l.thread)
                // Android a écrit cet envoi : la copie de Jasette ne sert plus.
                nettoyerLocaux(l.thread, listOf(l))
            }
        }
        requete(Telephony.Mms.CONTENT_URI, colonnesMms, "msg_box<>3", null, "_id DESC")?.use { c ->
            var i = 0
            while (c.moveToNext() && i++ < 20) {
                val idMms = c.long("_id")
                val boite = c.entier("msg_box")
                val id = SmsJson.idMms(idMms)
                val sig = "$boite"
                if (vus[id] == sig) continue
                vus[id] = sig
                if (!emettre) continue
                val thread = c.long("thread_id")
                val l = lireMms(idMms, thread, c.long("date"), boite)
                val m = SmsJson.msgMms(l, nomDe(l.de)) ?: continue
                nouveaux.add(m)
                fils.add(thread)
            }
        }
        // Ne garder que les récents : on oublie tout, puis on relit les récents sans rien émettre.
        if (vus.size > 2000) {
            vus.clear()
            scanner(emettre = false)
        }
        if (nouveaux.isEmpty()) return false
        // La conversation d'abord (un nouveau fil doit exister pour la notification), puis les messages.
        for (f in fils) convPour(f)?.let { emettre(JSONObject().put("ev", "conversation").put("item", it)) }
        nouveaux.sortBy { it.optLong("time") }
        for (m in nouveaux) emettre(JSONObject().put("ev", "message").put("item", m))
        return true
    }

    // ---- Envoi ----

    private fun envoyerSms(conv: String, texte: String, tmp: String) {
        fun echec(raison: String) {
            emettre(JSONObject().put("ev", "sent").put("ok", false).put("tmp_id", tmp))
            erreur("send", raison)
        }
        val thread = SmsJson.threadDe(conv) ?: return echec("SMS : conversation inconnue")
        if (texte.isEmpty()) return
        if (!permis(Manifest.permission.SEND_SMS)) return echec("SMS : l'envoi n'est pas permis. Touche « Utiliser les SMS du téléphone » dans les réglages.")
        val dest = destinatairesDe(thread)
        if (dest.size != 1) return echec("Envoi aux groupes : pas encore en mode SMS")
        try {
            val sm = if (Build.VERSION.SDK_INT >= 31) appli.getSystemService(SmsManager::class.java) else @Suppress("DEPRECATION") SmsManager.getDefault()
            val parties = sm.divideMessage(texte)
            envois[tmp] = Envoi(tmp, thread, texte, System.currentTimeMillis(), parties.size)
            val intentions = ArrayList<PendingIntent>()
            for (i in parties.indices) {
                val intent = Intent(ACTION_ENVOYE).setPackage(appli.packageName).putExtra(EXTRA_TMP, tmp).putExtra("part", i)
                intentions.add(
                    PendingIntent.getBroadcast(
                        appli, (tmp.hashCode() * 31) + i, intent,
                        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
                    ),
                )
            }
            sm.sendMultipartTextMessage(dest[0], null, parties, intentions, null)
        } catch (e: Exception) {
            envois.remove(tmp)
            Log.w("jasette-sms", "envoi : ${e.javaClass.simpleName}")
            echec("SMS non envoyé")
        }
    }

    /** Une rangée SMS de moi : est-ce un envoi de Jasette en attente ? Rend son tmp_id. */
    private fun associerEnvoi(id: String, l: LigneSms): String? {
        tmpDe[id]?.let { return it }
        val e = envois.values.firstOrNull { it.ligne == null && it.thread == l.thread && it.texte == l.corps && l.date >= it.temps - 60_000 }
            ?: return null
        e.ligne = id
        tmpDe[id] = e.tmp
        if (tmpDe.size > 200) tmpDe.remove(tmpDe.keys.first())
        return e.tmp
    }

    /** Le système a répondu pour une partie d'un SMS. */
    private fun partieEnvoyee(tmp: String, ok: Boolean) {
        val e = envois[tmp] ?: return
        e.recus++
        if (!ok) e.echec = true
        if (e.recus < e.parts) return
        // Laisser à Android le temps d'écrire (ou de mettre à jour) la rangée.
        principal.postDelayed({ executer { verifierEcrit(tmp) } }, 4000)
    }

    /**
     * Quelques secondes après la réponse du système : l'envoi est-il dans le
     * fournisseur SMS ? Sinon (Android ne l'a pas écrit), Jasette le garde.
     */
    private fun verifierEcrit(tmp: String) {
        scanner(emettre = true)
        val e = envois.remove(tmp) ?: return
        if (e.ligne != null) return
        if (e.echec) {
            emettre(JSONObject().put("ev", "sent").put("ok", false).put("tmp_id", tmp))
            erreur("send", "SMS non envoyé")
            return
        }
        val m = SmsJson.msgLocal(tmp, e.thread, e.texte, e.temps)
        locaux.add(JSONObject(m.toString()).put("thread", e.thread))
        ecrireLocaux()
        emettre(JSONObject().put("ev", "message").put("item", m))
    }

    // ---- Les envois gardés par Jasette ----

    private fun lireLocaux() {
        locaux.clear()
        try {
            if (!fichierLocaux.isFile) return
            val a = JSONArray(fichierLocaux.readText())
            for (i in 0 until a.length()) a.optJSONObject(i)?.let { locaux.add(it) }
        } catch (e: Exception) {
            Log.w("jasette-sms", "envoyes.json illisible : ${e.javaClass.simpleName}")
        }
    }

    private fun ecrireLocaux() {
        try {
            fichierLocaux.parentFile?.mkdirs()
            val tmp = File(fichierLocaux.path + ".tmp")
            tmp.writeText(JSONArray(locaux).toString())
            tmp.renameTo(fichierLocaux)
        } catch (e: Exception) {
            Log.w("jasette-sms", "envoyes.json : ${e.javaClass.simpleName}")
        }
    }

    private fun nettoyerLocaux(thread: Long, sms: List<LigneSms>) {
        if (locaux.isEmpty()) return
        val avant = locaux.size
        locaux.removeAll { l -> l.optLong("thread") == thread && sms.any { SmsJson.memeEnvoi(l, it) } }
        if (locaux.size != avant) ecrireLocaux()
    }

    // ---- Le reste ----

    /** `download` : copie une pièce MMS dans filesDir/media/sms, puis `media` avec son chemin. */
    private fun telecharger(mediaId: String, mime: String) {
        val v = JSONObject().put("ev", "media").put("media_id", mediaId)
        val id = SmsJson.pieceDe(mediaId)
        if (id == null) {
            emettre(v.put("path", ""))
            return
        }
        val ext = MimeTypeMap.getSingleton().getExtensionFromMimeType(mime) ?: "bin"
        val f = File(appli.filesDir, "media/sms/$id.$ext")
        try {
            if (!f.isFile || f.length() == 0L) {
                f.parentFile?.mkdirs()
                val tmp = File(f.path + ".part")
                cr.openInputStream(Uri.parse("content://mms/part/$id"))?.use { e -> tmp.outputStream().use { e.copyTo(it) } }
                    ?: throw IllegalStateException("pièce absente")
                tmp.renameTo(f)
            }
            emettre(v.put("path", f.path))
        } catch (e: Exception) {
            Log.w("jasette-sms", "pièce MMS : ${e.javaClass.simpleName}")
            emettre(v.put("path", ""))
        }
    }

    /** `read` : lu pour Jasette seulement (le fournisseur n'est modifiable que par l'app par défaut). */
    private fun marquerLu(conv: String) {
        val thread = SmsJson.threadDe(conv) ?: return
        lus.edit().putLong(thread.toString(), System.currentTimeMillis()).apply()
        convPour(thread)?.let { emettre(JSONObject().put("ev", "conversation").put("item", it)) }
    }

    /** `search` : les contacts du téléphone (déjà chargés par Contacts), un numéro par rangée. */
    private fun chercher(q: String) {
        val items = Contacts.chercher(q, 12).map {
            JSONObject().put("id", it.envoi).put("name", it.nom).put("number", it.numero)
        }
        emettre(JSONObject().put("ev", "search").put("query", q.trim()).put("items", JSONArray(items)))
    }

    /** `new` : le fil d'un numéro (créé s'il n'existe pas), ouvert. */
    private fun nouvelleConv(numero: String) {
        if (numero.isBlank()) return
        try {
            val thread = Telephony.Threads.getOrCreateThreadId(appli, numero.trim())
            val c = convPour(thread)
                ?: SmsJson.conv(thread, listOf(numero), listOf(nomDe(numero)), "", false, System.currentTimeMillis(), false)
            emettre(JSONObject().put("ev", "conversation").put("item", c).put("open", true))
        } catch (e: Exception) {
            Log.w("jasette-sms", "nouveau fil : ${e.javaClass.simpleName}")
            erreur("new", "Impossible d'ouvrir cette conversation SMS")
        }
    }

    companion object {
        private const val ACTION_ENVOYE = "ca.jasette.android.SMS_ENVOYE"
        private const val EXTRA_TMP = "tmp"

        /** Les permissions de « Utiliser les SMS du téléphone » (READ_CONTACTS : les noms). */
        val PERMISSIONS = arrayOf(
            Manifest.permission.READ_SMS,
            Manifest.permission.SEND_SMS,
            Manifest.permission.RECEIVE_SMS,
            Manifest.permission.RECEIVE_MMS,
            Manifest.permission.READ_CONTACTS,
        )
    }
}
