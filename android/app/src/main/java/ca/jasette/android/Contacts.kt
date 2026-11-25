package ca.jasette.android

import android.Manifest
import android.content.Context
import android.content.SharedPreferences
import android.content.pm.PackageManager
import android.database.ContentObserver
import android.os.Handler
import android.os.Looper
import android.provider.ContactsContract
import android.provider.ContactsContract.CommonDataKinds.Phone as P
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Un numéro d'un contact du téléphone. */
data class NumeroTel(
    /** Tel qu'écrit dans le carnet (pour l'affichage). */
    val brut: String,
    /** Les chiffres seulement. */
    val chiffres: String,
    /** Les 10 derniers chiffres : la clé pour l'Amérique du Nord. */
    val cle: String,
    /** Le numéro +E.164 du carnet, s'il est connu. */
    val e164: String?,
    val mobile: Boolean,
)

/** Un contact du téléphone : nom, numéros, photos (URI content://, lues par le ContentResolver). */
data class ContactTel(
    val id: Long,
    val nom: String,
    val numeros: List<NumeroTel>,
    /** PHOTO_THUMBNAIL_URI : la petite photo (listes). */
    val photo: String?,
    /** PHOTO_URI : la grande photo (gros avatars). */
    val grande: String?,
)

/** Une personne trouvée parmi les contacts du téléphone (une rangée par numéro). */
data class ContactTrouve(
    val nom: String,
    /** Le numéro affiché (« Contact · numéro »). */
    val numero: String,
    /** Le numéro envoyé dans la commande `new` de Google Messages. */
    val envoi: String,
    val cle: String,
    val photo: String?,
)

/**
 * Les contacts du téléphone (ContactsContract), lus en arrière-plan et gardés
 * en mémoire. Ils ne quittent jamais le téléphone : ni le serveur Jasette ni
 * les ponts ne les reçoivent (seul le numéro choisi part, dans `new`). Rien
 * de personnel (nom, numéro) n'est écrit dans les journaux.
 *
 * Les choix (« Utiliser les photos de mes contacts », « Aussi pour Messenger
 * et Instagram ») vivent dans les préférences privées « contacts ».
 */
object Contacts {
    private const val PREFS = "contacts"
    private const val CLE_PHOTOS = "photos"
    private const val CLE_META = "meta"
    /** La boîte du système a déjà été montrée (pour dire « refusé »). */
    private const val CLE_DEMANDEE = "demandee"
    /** « Nouveau message » a déjà proposé l'accès une fois. */
    private const val CLE_NOUVEAU = "nouveau_propose"

    private var appli: Context? = null
    private var prefs: SharedPreferences? = null
    private val portee = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var chargement: Job? = null
    private var observateur: ContentObserver? = null

    /** La permission READ_CONTACTS est-elle accordée ? */
    var permis by mutableStateOf(false)
        private set
    private var demandee by mutableStateOf(false)
    private var prefPhotos by mutableStateOf(true)
    private var prefMeta by mutableStateOf(false)

    /** Tous les contacts, triés par nom. */
    var liste by mutableStateOf<List<ContactTel>>(emptyList())
        private set
    /** Les contacts par clé de numéro (10 derniers chiffres). */
    private var parCle by mutableStateOf<Map<String, List<ContactTel>>>(emptyMap())
    /** Les contacts par nom plié (sans accents ni casse). */
    private var parNom by mutableStateOf<Map<String, List<ContactTel>>>(emptyMap())

    /** Le système a refusé l'accès (la boîte a été montrée, la permission manque). */
    val refusee: Boolean get() = demandee && !permis

    /** « Utiliser les photos de mes contacts » : activé par défaut une fois la permission accordée. */
    val photosActives: Boolean get() = permis && prefPhotos

    /** « Aussi pour Messenger et Instagram (par le nom) » : désactivé par défaut. */
    val metaActives: Boolean get() = photosActives && prefMeta

    /** Au lancement de l'interface : lit les choix ; charge les contacts si permis. Ne demande rien. */
    fun demarrer(ctx: Context) {
        if (appli == null) {
            val a = ctx.applicationContext
            appli = a
            val p = a.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            prefs = p
            prefPhotos = p.getBoolean(CLE_PHOTOS, true)
            prefMeta = p.getBoolean(CLE_META, false)
            demandee = p.getBoolean(CLE_DEMANDEE, false)
        }
        verifier()
    }

    /** L'app revient au premier plan : la permission a pu changer, les contacts aussi. */
    fun auPremierPlan(ctx: Context) {
        demarrer(ctx)
        if (permis) recharger(0)
    }

    /** Relit la permission ; branche l'observateur et charge la première fois qu'elle est là. */
    private fun verifier() {
        val a = appli ?: return
        val ok = a.checkSelfPermission(Manifest.permission.READ_CONTACTS) == PackageManager.PERMISSION_GRANTED
        val avant = permis
        permis = ok
        if (ok && observateur == null) {
            val o = object : ContentObserver(Handler(Looper.getMainLooper())) {
                override fun onChange(selfChange: Boolean) {
                    recharger(800)
                }
            }
            try {
                a.contentResolver.registerContentObserver(ContactsContract.Contacts.CONTENT_URI, true, o)
                observateur = o
            } catch (_: Throwable) {
            }
        } else if (!ok) {
            observateur?.let { runCatching { a.contentResolver.unregisterContentObserver(it) } }
            observateur = null
            liste = emptyList()
            parCle = emptyMap()
            parNom = emptyMap()
        }
        if (ok && !avant) recharger(0)
    }

    /** La boîte du système a répondu. */
    fun apresDemande(ok: Boolean) {
        demandee = true
        prefs?.edit()?.putBoolean(CLE_DEMANDEE, true)?.commit()
        verifier()
        if (ok) choisirPhotos(true)
    }

    /** « Nouveau message » doit-il proposer l'accès (une seule fois, jamais au démarrage) ? */
    fun proposerAuNouveau(): Boolean {
        if (permis || prefs?.getBoolean(CLE_NOUVEAU, false) != false) return false
        prefs?.edit()?.putBoolean(CLE_NOUVEAU, true)?.commit()
        return true
    }

    fun choisirPhotos(oui: Boolean) {
        prefPhotos = oui
        prefs?.edit()?.putBoolean(CLE_PHOTOS, oui)?.commit()
    }

    fun choisirMeta(oui: Boolean) {
        prefMeta = oui
        prefs?.edit()?.putBoolean(CLE_META, oui)?.commit()
    }

    /** Relit les contacts en arrière-plan ; [attente] regroupe les avis en rafale. */
    private fun recharger(attente: Long) {
        val a = appli ?: return
        chargement?.cancel()
        chargement = portee.launch {
            if (attente > 0) delay(attente)
            val lus = try {
                lire(a)
            } catch (_: Throwable) {
                // Permission retirée en route, fournisseur absent… : rien de personnel au journal.
                null
            } ?: return@launch
            val cles = HashMap<String, MutableList<ContactTel>>()
            val noms = HashMap<String, MutableList<ContactTel>>()
            for (c in lus) {
                for (n in c.numeros.distinctBy { it.cle }) cles.getOrPut(n.cle) { mutableListOf() }.add(c)
                val p = plier(c.nom).trim()
                if (p.isNotEmpty()) noms.getOrPut(p) { mutableListOf() }.add(c)
            }
            withContext(Dispatchers.Main) {
                // Une photo a pu changer derrière la même URI : on oublie les images décodées.
                ca.jasette.android.ui.oublierImagesContenu()
                liste = lus
                parCle = cles
                parNom = noms
            }
        }
    }

    /** Lit le carnet : les contacts (nom, photos), puis leurs numéros. */
    private fun lire(ctx: Context): List<ContactTel>? {
        if (ctx.checkSelfPermission(Manifest.permission.READ_CONTACTS) != PackageManager.PERMISSION_GRANTED) return null
        val cr = ctx.contentResolver
        data class Base(val nom: String, val photo: String?, val grande: String?)
        val bases = LinkedHashMap<Long, Base>()
        cr.query(
            ContactsContract.Contacts.CONTENT_URI,
            arrayOf(
                ContactsContract.Contacts._ID,
                ContactsContract.Contacts.DISPLAY_NAME_PRIMARY,
                ContactsContract.Contacts.PHOTO_THUMBNAIL_URI,
                ContactsContract.Contacts.PHOTO_URI,
            ),
            null, null,
            ContactsContract.Contacts.DISPLAY_NAME_PRIMARY + " COLLATE LOCALIZED ASC",
        )?.use { c ->
            while (c.moveToNext()) {
                val nom = c.getString(1)?.trim().orEmpty()
                if (nom.isEmpty()) continue
                bases[c.getLong(0)] = Base(nom, c.getString(2), c.getString(3) ?: c.getString(2))
            }
        }
        val numeros = HashMap<Long, MutableList<NumeroTel>>()
        cr.query(
            P.CONTENT_URI,
            arrayOf(P.CONTACT_ID, P.NUMBER, P.NORMALIZED_NUMBER, P.TYPE),
            null, null, null,
        )?.use { c ->
            while (c.moveToNext()) {
                val brut = c.getString(1)?.trim().orEmpty()
                val chiffres = chiffres(brut)
                if (chiffres.length < 3) continue
                val e164 = c.getString(2)?.takeIf { it.startsWith("+") }
                val type = c.getInt(3)
                val mobile = type == P.TYPE_MOBILE || type == P.TYPE_WORK_MOBILE
                numeros.getOrPut(c.getLong(0)) { mutableListOf() }
                    .add(NumeroTel(brut, chiffres, cle(chiffres), e164, mobile))
            }
        }
        return bases.map { (id, b) ->
            ContactTel(id, b.nom, numeros[id].orEmpty().distinctBy { it.chiffres }, b.photo, b.grande)
        }
    }

    /** Les chiffres seulement. */
    fun chiffres(s: String) = s.filter { it.isDigit() }

    /** La clé d'un numéro : ses 10 derniers chiffres (Amérique du Nord), sinon tous. */
    fun cle(chiffres: String) = if (chiffres.length > 10) chiffres.takeLast(10) else chiffres

    /** La clé d'un numéro écrit n'importe comment ; vide s'il n'y a pas assez de chiffres. */
    fun cleDe(numero: String): String = chiffres(numero).let { if (it.length < 7) "" else cle(it) }

    /**
     * La photo de contact à montrer pour [conv], ou null (on garde l'avatar habituel).
     * Google à deux : par le numéro (10 derniers chiffres). Messenger et Instagram :
     * par le nom exact (sans accents ni casse), si un seul contact porte ce nom.
     */
    fun photoPour(conv: Conv, grande: Boolean = false): String? {
        if (!photosActives || conv.group) return null
        val c: ContactTel? = when (conv.service) {
            Service.GOOGLE -> {
                val numero = conv.number.ifEmpty { conv.members.singleOrNull()?.takeIf { estNumero(it) }.orEmpty() }
                val k = cleDe(numero)
                if (k.isEmpty()) null else parCle[k]?.firstOrNull { it.photo != null }
            }
            else -> {
                if (!metaActives) return null
                val p = plier(conv.name).trim()
                if (p.isEmpty()) null else parNom[p]?.singleOrNull()
            }
        }
        if (c == null) return null
        return if (grande) c.grande ?: c.photo else c.photo ?: c.grande
    }

    /**
     * Les contacts du téléphone qui correspondent à [texte] (nom sans accents, ou
     * numéro) : une rangée par numéro mobile, sinon le premier ; sans doublons de numéro.
     */
    fun chercher(texte: String, max: Int = 8): List<ContactTrouve> {
        if (!permis) return emptyList()
        val brut = texte.trim()
        val q = plier(brut)
        if (q.length < 2) return emptyList()
        val parNumero = estNumero(brut)
        val qChiffres = chiffres(brut)
        val vus = HashSet<String>()
        val sortie = ArrayList<ContactTrouve>()
        for (c in liste) {
            if (c.numeros.isEmpty()) continue
            val rangs: List<NumeroTel> = when {
                parNumero && qChiffres.length >= 3 -> c.numeros.filter { it.chiffres.contains(qChiffres) }
                plier(c.nom).contains(q) -> c.numeros.filter { it.mobile }.ifEmpty { listOf(c.numeros.first()) }
                else -> emptyList()
            }
            for (n in rangs) {
                if (!vus.add(n.cle)) continue
                sortie += ContactTrouve(c.nom, n.brut, n.e164 ?: n.brut, n.cle, c.photo ?: c.grande)
                if (sortie.size >= max) return sortie
            }
        }
        return sortie
    }
}
