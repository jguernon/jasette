package ca.jasette.android

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.PorterDuff
import android.graphics.PorterDuffXfermode
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.core.app.Person
import androidx.core.content.LocusIdCompat
import androidx.core.content.pm.ShortcutInfoCompat
import androidx.core.content.pm.ShortcutManagerCompat
import androidx.core.graphics.drawable.IconCompat
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors

/**
 * Le partage direct d'Android (Direct Share) : les conversations récentes,
 * publiées comme raccourcis de conversation (`pushDynamicShortcut`, longue
 * durée, catégorie [CATEGORIE] liée à la `<share-target>` de
 * res/xml/shortcuts.xml), apparaissent dans la première rangée du menu de
 * partage. En choisir une ouvre [PartageActivity] avec cette conversation déjà
 * choisie. Les notifications de message s'y rattachent ([pourNotification]) :
 * Android les traite alors comme des conversations (priorité, bulles).
 *
 * Liste privée : une conversation privée n'est jamais publiée, et celle qui le
 * devient est retirée aussitôt, y compris des raccourcis gardés par le
 * système pour les notifications ([privees]).
 */
object Raccourcis {
    /** La catégorie de partage (la même que dans res/xml/shortcuts.xml). */
    const val CATEGORIE = "ca.jasette.CATEGORIE_PARTAGE"
    private const val MAX = 10
    private const val DELAI_MS = 2000L
    private const val ETIQUETTE = "jasette-raccourcis"

    private val principal = Handler(Looper.getMainLooper())
    /** Un seul fil : les publications et les retraits gardent leur ordre. */
    private val fil = Executors.newSingleThreadExecutor { r -> Thread(r, "raccourcis").apply { isDaemon = true } }
    @Volatile private var appli: Context? = null
    private var prevu = false
    private val maj = Runnable {
        prevu = false
        publier()
    }
    /** id → ce qui a été publié (nom, photo, heure) : on ne repousse que ce qui a changé. */
    private val publies = ConcurrentHashMap<String, String>()

    /** Ce qu'il faut d'une conversation pour son raccourci (lu sur le fil principal). */
    private class Fiche(val id: String, val nom: String, val photo: String?, val couleur: String, val groupe: Boolean, val time: Long) {
        val signature get() = "$nom|$photo|$time"
    }

    fun initialiser(ctx: Context) {
        if (appli == null) appli = ctx.applicationContext
    }

    /** Les conversations ont changé (message envoyé ou reçu, liste rechargée) : publication groupée. */
    fun noter() {
        if (appli == null || prevu) return
        prevu = true
        principal.postDelayed(maj, DELAI_MS)
    }

    /** La Liste privée a changé : les privées sont retirées tout de suite, le reste suit. */
    fun privees() {
        val a = appli ?: return
        val ids = Marques.privees.toList()
        if (ids.isNotEmpty()) fil.execute { retirerPrivees(a, ids.toSet()) }
        noter()
    }

    private fun fiche(c: Conv) = Fiche(
        id = c.id,
        nom = nomAffiche(c).ifEmpty { c.members.joinToString(", ") }.ifEmpty { c.service.court },
        photo = Contacts.photoPour(c, grande = true) ?: Depot.avatarDe(c) ?: CacheMarques.avatar(c.id),
        couleur = c.color,
        groupe = c.group,
        time = c.time,
    )

    /** Sur le fil principal : les [MAX] conversations les plus récentes de la boîte (sans privées, archives ni grands espaces cachés). */
    private fun publier() {
        val a = appli ?: return
        val privees = Marques.privees.toSet()
        val fiches = Depot.inbox.filter { !it.readOnly }.take(MAX).map { fiche(it) }
        fil.execute { appliquer(a, fiches, privees) }
    }

    private fun appliquer(a: Context, fiches: List<Fiche>, privees: Set<String>) {
        try {
            retirerPrivees(a, privees)
            // Rien de chargé (démarrage, changement de mode) : on garde ce qui est publié.
            if (fiches.isEmpty()) return
            val max = minOf(MAX, ShortcutManagerCompat.getMaxShortcutCountPerActivity(a)).coerceAtLeast(1)
            val garder = fiches.take(max)
            val ids = garder.mapTo(HashSet()) { it.id }
            val dynamiques = ShortcutManagerCompat.getDynamicShortcuts(a).map { it.id }
            val vieux = dynamiques.filter { it !in ids }
            if (vieux.isNotEmpty()) {
                ShortcutManagerCompat.removeDynamicShortcuts(a, vieux)
                vieux.forEach { publies.remove(it) }
            }
            garder.forEachIndexed { rang, f ->
                if (Marques.estPrivee(f.id)) return@forEachIndexed
                if (publies[f.id] == f.signature && f.id in dynamiques) return@forEachIndexed
                ShortcutManagerCompat.pushDynamicShortcut(a, raccourci(a, f, rang, avatarRond(a, f.photo)))
                publies[f.id] = f.signature
            }
            // Devenue privée pendant la publication : retirée aussitôt.
            val tard = garder.map { it.id }.filter { Marques.estPrivee(it) }
            if (tard.isNotEmpty()) retirerPrivees(a, tard.toSet())
        } catch (e: Exception) {
            Log.w(ETIQUETTE, "publication : ${e.message}")
        }
    }

    /** Retire partout (dynamiques et gardés pour les notifications) les raccourcis des conversations privées. */
    private fun retirerPrivees(a: Context, privees: Set<String>) {
        if (privees.isEmpty()) return
        try {
            val existants = ShortcutManagerCompat.getShortcuts(
                a, ShortcutManagerCompat.FLAG_MATCH_DYNAMIC or ShortcutManagerCompat.FLAG_MATCH_CACHED,
            ).map { it.id }
            val aRetirer = existants.filter { it in privees }
            if (aRetirer.isNotEmpty()) ShortcutManagerCompat.removeLongLivedShortcuts(a, aRetirer)
            privees.forEach { publies.remove(it) }
        } catch (e: Exception) {
            Log.w(ETIQUETTE, "retrait des privées : ${e.message}")
        }
    }

    /**
     * Le raccourci d'une conversation, publié tout de suite pour sa
     * notification (Android ne la traite en conversation que si le raccourci
     * existe déjà). Null pour une conversation privée ou en lecture seule.
     * [image] : la photo ronde déjà chargée pour la notification.
     */
    fun pourNotification(ctx: Context, conv: Conv, image: Bitmap?): String? {
        if (Marques.estPrivee(conv.id) || conv.readOnly || Depot.estArchivee(conv)) return null
        val a = ctx.applicationContext
        return try {
            val f = fiche(conv)
            ShortcutManagerCompat.pushDynamicShortcut(a, raccourci(a, f, 0, image ?: avatarRond(a, f.photo)))
            publies[f.id] = f.signature
            conv.id
        } catch (e: Exception) {
            Log.w(ETIQUETTE, "raccourci de notification : ${e.message}")
            null
        }
    }

    private fun raccourci(a: Context, f: Fiche, rang: Int, photo: Bitmap?): ShortcutInfoCompat {
        val icone = IconCompat.createWithBitmap(photo ?: initiales(f.nom, f.couleur, f.groupe))
        // Touché depuis le lanceur : la conversation s'ouvre dans Jasette.
        val ouvrir = Intent(a, MainActivity::class.java)
            .setAction(Intent.ACTION_VIEW)
            .putExtra(MainActivity.EXTRA_CONV, f.id)
        return ShortcutInfoCompat.Builder(a, f.id)
            .setShortLabel(f.nom)
            .setLongLabel(f.nom)
            .setIcon(icone)
            .setIntent(ouvrir)
            .setLongLived(true)
            .setCategories(setOf(CATEGORIE))
            .setLocusId(LocusIdCompat(f.id))
            .setRank(rang)
            .apply {
                if (!f.groupe) setPerson(Person.Builder().setName(f.nom).setKey(f.id).setIcon(icone).build())
            }
            .build()
    }

    /** Sans photo : un rond de la couleur de la conversation, avec ses initiales (comme les avatars de l'app). */
    private fun initiales(nom: String, couleur: String, groupe: Boolean): Bitmap {
        val cote = 192
        val bmp = Bitmap.createBitmap(cote, cote, Bitmap.Config.ARGB_8888)
        val toile = Canvas(bmp)
        val fond = couleur.removePrefix("#").takeIf { it.length == 6 }?.toLongOrNull(16)?.let { (0xFF000000 or it).toInt() }
            ?: 0xFF3B82F6.toInt()
        val p = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = fond }
        toile.drawCircle(cote / 2f, cote / 2f, cote / 2f, p)
        val lettres = if (groupe) "" else nom.split(Regex("\\s+"))
            .mapNotNull { m -> m.firstOrNull { it.isLetterOrDigit() } }.take(2).joinToString("").uppercase()
            .takeUnless { it.all { c -> c.isDigit() } }.orEmpty()
        val texte = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = 0xFFFFFFFF.toInt()
            textSize = cote * 0.38f
            textAlign = Paint.Align.CENTER
            isFakeBoldText = true
        }
        val ecrit = lettres.ifEmpty { "•" }
        toile.drawText(ecrit, cote / 2f, cote / 2f - (texte.descent() + texte.ascent()) / 2f, texte)
        return bmp
    }
}

/** Découpe une image carrée en rond, comme les avatars de l'app (le reste transparent). */
private fun rond(src: Bitmap): Bitmap {
    val cote = src.width
    val sortie = Bitmap.createBitmap(cote, cote, Bitmap.Config.ARGB_8888)
    val toile = Canvas(sortie)
    val pinceau = Paint(Paint.ANTI_ALIAS_FLAG)
    toile.drawCircle(cote / 2f, cote / 2f, cote / 2f, pinceau)
    pinceau.xfermode = PorterDuffXfermode(PorterDuff.Mode.SRC_IN)
    toile.drawBitmap(src, 0f, 0f, pinceau)
    return sortie
}

/**
 * Une photo d'avatar (fichier ou content://), réduite à 256 px et découpée en
 * rond, ou null. Sert aux notifications et aux raccourcis de partage.
 */
fun avatarRond(ctx: Context, chemin: String?): Bitmap? {
    if (chemin.isNullOrEmpty()) return null
    return try {
        val bmp = if (chemin.startsWith("content:")) {
            ctx.contentResolver.openInputStream(Uri.parse(chemin))?.use { BitmapFactory.decodeStream(it) }
        } else {
            BitmapFactory.decodeFile(chemin.removePrefix("file://"))
        }
        bmp?.let {
            val cote = minOf(it.width, it.height)
            val carre = Bitmap.createBitmap(it, (it.width - cote) / 2, (it.height - cote) / 2, cote, cote)
            val reduit = if (cote > 256) Bitmap.createScaledBitmap(carre, 256, 256, true) else carre
            rond(reduit)
        }
    } catch (_: Exception) {
        null
    }
}
