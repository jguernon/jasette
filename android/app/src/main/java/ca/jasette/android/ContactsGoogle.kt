package ca.jasette.android

import android.Manifest
import android.content.ContentProviderOperation
import android.content.Context
import android.content.pm.PackageManager
import android.provider.ContactsContract
import android.provider.ContactsContract.CommonDataKinds.Phone
import android.provider.ContactsContract.CommonDataKinds.Photo
import android.provider.ContactsContract.CommonDataKinds.StructuredName
import android.util.Log
import java.io.File
import java.util.concurrent.Executors

/*
 * « Enregistrer aussi dans les Contacts Google » (crayon de la fiche,
 * serveur/PROTOCOLE.md §5.5, « Règle des contacts perso ») : Jasette écrit
 * dans le carnet du téléphone (ContactsContract, permission WRITE_CONTACTS),
 * qui se synchronise lui-même avec le compte Google. Seulement pour une
 * personne qui a un numéro de téléphone. Rien de personnel au journal.
 */

/** Ce que l'enregistrement dans les Contacts Google fera. */
sealed class CibleContact {
    /** Aucun numéro connu pour cette personne : rien à faire dans le carnet. */
    data object SansNumero : CibleContact()

    /** Un contact du carnet porte déjà ce numéro : son nom affiché et sa photo changent. */
    data class Existant(val contactId: Long, val nom: String) : CibleContact()

    /** Aucun contact pour ce numéro : un nouveau, dans le compte Google [compte] (null : aucun compte Google). */
    data class Nouveau(val numero: String, val compte: String?) : CibleContact()
}

object ContactsGoogle {
    const val TYPE_GOOGLE = "com.google"
    private const val PREFS = "contacts_google"
    private const val CLE_APPLIQUEES = "appliquees"
    private const val CLE_INIT = "init"
    private val fil = Executors.newSingleThreadExecutor { Thread(it, "contacts-google") }

    // ---- La logique (sans Android : testée dans ContactsGoogleTest) ----

    /**
     * Les numéros (E.164) de la personne de [ids] (l'ancre d'abord), dans
     * l'ordre, sans doublon : Google Messages, WhatsApp, Signal, Telegram.
     */
    fun numeros(ids: List<String>, conv: (String) -> Conv?): List<String> =
        ids.mapNotNull { id -> conv(id)?.let { numeroDe(it.id, it.number, it.members.firstOrNull(), it.name) } }.distinct()

    /**
     * Le contact visé : pour le premier numéro de [numeros] qu'un contact
     * porte (même E.164, ou mêmes 10 derniers chiffres), celui dont le nom est
     * [nom] s'il y en a un, sinon le plus ancien (plus petit id). Aucun : un
     * nouveau contact pour le premier numéro, dans [compte].
     */
    fun cible(numeros: List<String>, contacts: List<ContactTel>, nom: String, compte: String?): CibleContact {
        if (numeros.isEmpty()) return CibleContact.SansNumero
        for (n in numeros) {
            val cle = Contacts.cle(Contacts.chiffres(n))
            val porteurs = contacts.filter { c -> c.numeros.any { it.e164 == n || it.cle == cle } }
            if (porteurs.isEmpty()) continue
            val c = porteurs.firstOrNull { mots(it.nom) == mots(nom) } ?: porteurs.minBy { it.id }
            return CibleContact.Existant(c.id, c.nom)
        }
        return CibleContact.Nouveau(numeros.first(), compte)
    }

    /** Le compte Google principal : celui qui a le plus de contacts (à égalité, le premier par ordre alphabétique). */
    fun comptePrincipal(comptes: Map<String, Int>): String? =
        comptes.entries.filter { it.key.isNotEmpty() }.sortedWith(compareByDescending<Map.Entry<String, Int>> { it.value }.thenBy { it.key }).firstOrNull()?.key

    /** La signature d'une entrée : un changement de nom ou de photo est à réappliquer. */
    fun signature(cle: String, c: ContactPerso) = "$cle\u0001${c.nom}\u0001${c.photo}"

    /** Les entrées marquées « google » pas encore appliquées ici. */
    fun aAppliquer(perso: Map<String, ContactPerso>, appliquees: Set<String>): List<Pair<String, ContactPerso>> =
        perso.filter { (k, c) -> c.google && signature(k, c) !in appliquees }.map { it.key to it.value }

    /** Les conversations de la personne de [cle] : celles de sa personne (marque `personnes`), sinon elle seule. */
    fun groupeDe(cle: String, personnes: List<Personne>): List<String> =
        (personnes.firstOrNull { cle in it.convs }?.convs ?: listOf(cle)).let { l -> listOf(cle) + l.filter { it != cle } }

    // ---- Android ----

    /** Les deux permissions (lire pour trouver le contact, écrire pour le changer). */
    val PERMISSIONS = arrayOf(Manifest.permission.READ_CONTACTS, Manifest.permission.WRITE_CONTACTS)

    fun permis(ctx: Context): Boolean = PERMISSIONS.all { ctx.checkSelfPermission(it) == PackageManager.PERMISSION_GRANTED }

    /** Les comptes Google qui ont des contacts sur ce téléphone, et combien. */
    private fun comptesGoogle(ctx: Context): Map<String, Int> {
        val r = HashMap<String, Int>()
        ctx.contentResolver.query(
            ContactsContract.RawContacts.CONTENT_URI, arrayOf(ContactsContract.RawContacts.ACCOUNT_NAME),
            "${ContactsContract.RawContacts.ACCOUNT_TYPE} = ? AND ${ContactsContract.RawContacts.DELETED} = 0", arrayOf(TYPE_GOOGLE), null,
        )?.use { c -> while (c.moveToNext()) c.getString(0)?.let { r[it] = (r[it] ?: 0) + 1 } }
        if (r.isEmpty()) {
            // Un compte Google sans contact encore : AccountManager (visible avec READ_CONTACTS).
            runCatching {
                android.accounts.AccountManager.get(ctx).getAccountsByType(TYPE_GOOGLE).forEach { r[it.name] = 0 }
            }
        }
        return r
    }

    /** Le contact visé pour la personne de [ids], dont le nom voulu est [nom]. Hors du fil principal. */
    fun cibleDe(ctx: Context, ids: List<String>, nom: String): CibleContact {
        val n = numeros(ids) { Depot.conv(it) }
        if (n.isEmpty()) return CibleContact.SansNumero
        val contacts = Contacts.lireMaintenant(ctx) ?: emptyList()
        return cible(n, contacts, nom, comptePrincipal(comptesGoogle(ctx)))
    }

    /**
     * Écrit [nom] (vide : le nom ne change pas ; un nouveau contact prend
     * alors [nomService]) et la photo [photo] (un fichier JPEG, ou null) dans
     * le carnet. Faux (et rien d'écrit) si ce n'est pas possible. Hors du fil principal.
     */
    fun ecrire(ctx: Context, cible: CibleContact, nom: String, nomService: String, photo: String?): Boolean {
        if (!permis(ctx)) return false
        val octets = photo?.let { runCatching { File(it).readBytes() }.getOrNull() }?.takeIf { it.isNotEmpty() }
        val ops = ArrayList<ContentProviderOperation>()
        val donnees = ContactsContract.Data.CONTENT_URI
        when (cible) {
            CibleContact.SansNumero -> return false
            is CibleContact.Nouveau -> {
                val compte = cible.compte ?: return false
                ops += ContentProviderOperation.newInsert(ContactsContract.RawContacts.CONTENT_URI)
                    .withValue(ContactsContract.RawContacts.ACCOUNT_TYPE, TYPE_GOOGLE)
                    .withValue(ContactsContract.RawContacts.ACCOUNT_NAME, compte).build()
                ops += ContentProviderOperation.newInsert(donnees).withValueBackReference(ContactsContract.Data.RAW_CONTACT_ID, 0)
                    .withValue(ContactsContract.Data.MIMETYPE, StructuredName.CONTENT_ITEM_TYPE)
                    .withValue(StructuredName.DISPLAY_NAME, nom.ifEmpty { nomService }).build()
                ops += ContentProviderOperation.newInsert(donnees).withValueBackReference(ContactsContract.Data.RAW_CONTACT_ID, 0)
                    .withValue(ContactsContract.Data.MIMETYPE, Phone.CONTENT_ITEM_TYPE)
                    .withValue(Phone.NUMBER, cible.numero).withValue(Phone.TYPE, Phone.TYPE_MOBILE).build()
                if (octets != null) {
                    ops += ContentProviderOperation.newInsert(donnees).withValueBackReference(ContactsContract.Data.RAW_CONTACT_ID, 0)
                        .withValue(ContactsContract.Data.MIMETYPE, Photo.CONTENT_ITEM_TYPE)
                        .withValue(ContactsContract.Data.IS_PRIMARY, 1).withValue(ContactsContract.Data.IS_SUPER_PRIMARY, 1)
                        .withValue(Photo.PHOTO, octets).build()
                }
            }
            is CibleContact.Existant -> {
                val brut = contactBrut(ctx, cible.contactId) ?: return false
                if (nom.isNotEmpty()) {
                    val ligne = ligneDe(ctx, brut, StructuredName.CONTENT_ITEM_TYPE)
                    ops += if (ligne != null) {
                        // Les parties du nom vidées : le carnet les refait d'après le nom affiché.
                        ContentProviderOperation.newUpdate(donnees).withSelection("${ContactsContract.Data._ID} = ?", arrayOf("$ligne"))
                            .withValue(StructuredName.DISPLAY_NAME, nom)
                            .withValue(StructuredName.GIVEN_NAME, null).withValue(StructuredName.FAMILY_NAME, null)
                            .withValue(StructuredName.MIDDLE_NAME, null).withValue(StructuredName.PREFIX, null)
                            .withValue(StructuredName.SUFFIX, null).build()
                    } else {
                        ContentProviderOperation.newInsert(donnees).withValue(ContactsContract.Data.RAW_CONTACT_ID, brut)
                            .withValue(ContactsContract.Data.MIMETYPE, StructuredName.CONTENT_ITEM_TYPE)
                            .withValue(StructuredName.DISPLAY_NAME, nom).build()
                    }
                }
                if (octets != null) {
                    val ligne = ligneDe(ctx, brut, Photo.CONTENT_ITEM_TYPE)
                    ops += if (ligne != null) {
                        ContentProviderOperation.newUpdate(donnees).withSelection("${ContactsContract.Data._ID} = ?", arrayOf("$ligne"))
                            .withValue(Photo.PHOTO, octets).withValue(ContactsContract.Data.IS_SUPER_PRIMARY, 1).build()
                    } else {
                        ContentProviderOperation.newInsert(donnees).withValue(ContactsContract.Data.RAW_CONTACT_ID, brut)
                            .withValue(ContactsContract.Data.MIMETYPE, Photo.CONTENT_ITEM_TYPE)
                            .withValue(ContactsContract.Data.IS_PRIMARY, 1).withValue(ContactsContract.Data.IS_SUPER_PRIMARY, 1)
                            .withValue(Photo.PHOTO, octets).build()
                    }
                }
            }
        }
        if (ops.isEmpty()) return true
        return try {
            ctx.contentResolver.applyBatch(ContactsContract.AUTHORITY, ops)
            Contacts.relire()
            true
        } catch (e: Exception) {
            Log.w("jasette", "Contacts Google : écriture impossible (${e.javaClass.simpleName})")
            false
        }
    }

    /** Le contact brut à changer : celui du compte Google, sinon le premier. */
    private fun contactBrut(ctx: Context, contactId: Long): Long? {
        var google: Long? = null
        var premier: Long? = null
        ctx.contentResolver.query(
            ContactsContract.RawContacts.CONTENT_URI, arrayOf(ContactsContract.RawContacts._ID, ContactsContract.RawContacts.ACCOUNT_TYPE),
            "${ContactsContract.RawContacts.CONTACT_ID} = ? AND ${ContactsContract.RawContacts.DELETED} = 0", arrayOf("$contactId"),
            ContactsContract.RawContacts._ID,
        )?.use { c ->
            while (c.moveToNext()) {
                if (premier == null) premier = c.getLong(0)
                if (google == null && c.getString(1) == TYPE_GOOGLE) google = c.getLong(0)
            }
        }
        return google ?: premier
    }

    /** La ligne de données [type] d'un contact brut, s'il en a une. */
    private fun ligneDe(ctx: Context, brut: Long, type: String): Long? =
        ctx.contentResolver.query(
            ContactsContract.Data.CONTENT_URI, arrayOf(ContactsContract.Data._ID),
            "${ContactsContract.Data.RAW_CONTACT_ID} = ? AND ${ContactsContract.Data.MIMETYPE} = ?", arrayOf("$brut", type), null,
        )?.use { c -> if (c.moveToFirst()) c.getLong(0) else null }

    // ---- Les entrées venues d'un autre appareil (bureau) ----

    private fun prefs(ctx: Context) = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    /** Cette entrée vient d'être écrite ici : ne pas la réappliquer. */
    fun noterAppliquee(ctx: Context, cle: String, c: ContactPerso) {
        val p = prefs(ctx)
        val s = (p.getStringSet(CLE_APPLIQUEES, emptySet()) ?: emptySet()) + signature(cle, c)
        p.edit().putStringSet(CLE_APPLIQUEES, s).putBoolean(CLE_INIT, true).apply()
    }

    /**
     * Les marques viennent d'arriver : une entrée « google » d'un autre
     * appareil, pas encore appliquée ici, est écrite dans le carnet, une fois,
     * si la permission est **déjà** accordée (jamais demandée pour elle). À la
     * toute première fois, les entrées existantes sont notées sans rien écrire.
     */
    fun appliquerRecues(ctx: Context, perso: Map<String, ContactPerso>) {
        val a = ctx.applicationContext
        if (!permis(a)) return
        val personnes = Marques.personnes
        fil.execute {
            val p = prefs(a)
            val faites = (p.getStringSet(CLE_APPLIQUEES, emptySet()) ?: emptySet()).toMutableSet()
            val restes = aAppliquer(perso, faites)
            if (!p.getBoolean(CLE_INIT, false)) {
                restes.forEach { (k, c) -> faites += signature(k, c) }
                p.edit().putStringSet(CLE_APPLIQUEES, faites).putBoolean(CLE_INIT, true).apply()
                return@execute
            }
            for ((k, c) in restes) {
                val ids = groupeDe(k, personnes)
                val nomService = ids.firstNotNullOfOrNull { Depot.conv(it)?.name?.takeIf { n -> n.isNotEmpty() } } ?: continue
                val cible = cibleDe(a, ids, c.nom.ifEmpty { nomService })
                // Sans numéro (ou conversations pas encore chargées) : on réessaiera à la prochaine réception.
                if (cible is CibleContact.SansNumero) continue
                val photo = c.photo.takeIf { it.isNotEmpty() }?.let { ContactsPerso.photoLocale(it) }
                if (c.photo.isNotEmpty() && photo == null) continue
                if (ecrire(a, cible, c.nom, nomService, photo)) faites += signature(k, c)
            }
            p.edit().putStringSet(CLE_APPLIQUEES, faites).apply()
        }
    }
}
