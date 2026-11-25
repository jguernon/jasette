package ca.jasette.android.ui

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import android.util.Log
import android.util.Size
import android.webkit.MimeTypeMap
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.core.content.FileProvider
import ca.jasette.android.Depot
import ca.jasette.android.Service
import ca.jasette.android.t
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.io.FileOutputStream

/**
 * L'envoi de photos, vidéos et fichiers : les URI du sélecteur (ou de
 * l'appareil photo) sont copiées dans le cache de l'app, puis passées aux
 * ponts par `send_media` (voir [Depot.envoyerFichier]). Les vidéos ne sont pas
 * recompressées : au-delà de la limite du service, une erreur claire.
 */
object Envoi {
    private const val JOURNAL = "jasette-envoi"
    private val principal = Handler(Looper.getMainLooper())
    /** Le serveur Jasette refuse les dépôts de plus de 200 Mio (PROTOCOLE.md §6.2). */
    private const val LIMITE_SERVEUR = 200L shl 20

    /** Une URI où l'appareil photo écrira (cache/capture, partagé par le FileProvider). */
    fun uriCapture(ctx: Context, extension: String): Uri {
        val dossier = File(ctx.cacheDir, "capture").apply { mkdirs() }
        val f = File(dossier, "capture_${System.currentTimeMillis()}.$extension")
        return FileProvider.getUriForFile(ctx, ctx.packageName + ".fichiers", f)
    }

    /** Le nom affichable d'une URI. */
    fun nom(ctx: Context, u: Uri): String {
        try {
            ctx.contentResolver.query(u, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                if (c.moveToFirst() && !c.isNull(0)) return c.getString(0)
            }
        } catch (_: Exception) {
        }
        return u.lastPathSegment?.substringAfterLast('/') ?: "fichier"
    }

    private fun taille(ctx: Context, u: Uri): Long {
        try {
            ctx.contentResolver.query(u, arrayOf(OpenableColumns.SIZE), null, null, null)?.use { c ->
                if (c.moveToFirst() && !c.isNull(0)) return c.getLong(0)
            }
        } catch (_: Exception) {
        }
        return -1
    }

    private fun mime(ctx: Context, u: Uri, nom: String): String =
        ctx.contentResolver.getType(u)?.takeIf { it.isNotEmpty() && it != "application/octet-stream" }
            ?: MimeTypeMap.getSingleton().getMimeTypeFromExtension(nom.substringAfterLast('.', "").lowercase())
            ?: "application/octet-stream"

    /** La vignette d'une pièce choisie (image ou vidéo), ou null pour un autre fichier. */
    suspend fun vignette(ctx: Context, u: Uri): ImageBitmap? = withContext(Dispatchers.IO) {
        try {
            val type = ctx.contentResolver.getType(u).orEmpty()
            val bmp: Bitmap? = when {
                Build.VERSION.SDK_INT >= 29 && (type.startsWith("image/") || type.startsWith("video/")) ->
                    ctx.contentResolver.loadThumbnail(u, Size(256, 256), null)
                type.startsWith("image/") -> {
                    val o = BitmapFactory.Options().apply { inSampleSize = 8 }
                    ctx.contentResolver.openInputStream(u)?.use { BitmapFactory.decodeStream(it, null, o) }
                }
                type.startsWith("video/") -> MediaMetadataRetriever().run {
                    try {
                        setDataSource(ctx, u)
                        getFrameAtTime(0)
                    } finally {
                        release()
                    }
                }
                else -> null
            }
            bmp?.asImageBitmap()
        } catch (e: Exception) {
            null
        }
    }

    /** Efface les copies d'envoi et les captures de plus de deux jours. */
    private fun menage(ctx: Context) {
        val limite = System.currentTimeMillis() - 2 * 24 * 3600 * 1000L
        for (d in listOf("envoi", "capture")) {
            File(ctx.cacheDir, d).listFiles()?.forEach { if (it.lastModified() < limite) it.delete() }
        }
    }

    /** Une pièce copiée, prête pour `send_media`. */
    private class Prete(val fichier: File, val mime: String, val nom: String, val vignette: String?)

    /**
     * Copie les [uris] (hors du fil principal) puis les envoie dans [conv].
     * [texte] part avec la première pièce si le service le permet (Meta),
     * sinon en message séparé (Google). [erreur] reçoit les refus (taille, lecture).
     * [envoyes] reçoit les tmp_id des envois lancés (le partage suit leur état).
     */
    fun envoyer(
        ctx: Context, conv: String, uris: List<Uri>, texte: String,
        envoyes: (List<String>) -> Unit = {}, erreur: (String) -> Unit,
    ) {
        val appli = ctx.applicationContext
        val service = Service.de(conv)
        val limite = if (Depot.serveur != null) minOf(service.limiteEnvoi, LIMITE_SERVEUR) else service.limiteEnvoi
        Thread({
            menage(appli)
            val dossier = File(appli.cacheDir, "envoi").apply { mkdirs() }
            val pretes = ArrayList<Prete>()
            val erreurs = ArrayList<String>()
            for (u in uris) {
                val nom = nom(appli, u).replace('/', '_')
                val trop = "« $nom » ${t("est trop gros pour")} ${service.court} (${limite shr 20} ${t("Mo au plus")})."
                try {
                    if (taille(appli, u) > limite) {
                        erreurs.add(trop)
                        continue
                    }
                    val mime = mime(appli, u, nom)
                    val f = File(dossier, "${System.currentTimeMillis()}_$nom")
                    var total = 0L
                    var depasse = false
                    appli.contentResolver.openInputStream(u)?.use { entree ->
                        FileOutputStream(f).use { sortie ->
                            val tampon = ByteArray(64 shl 10)
                            while (true) {
                                val n = entree.read(tampon)
                                if (n < 0) break
                                total += n
                                if (total > limite) {
                                    depasse = true
                                    break
                                }
                                sortie.write(tampon, 0, n)
                            }
                        }
                    } ?: throw java.io.IOException(t("lecture impossible"))
                    if (depasse) {
                        f.delete()
                        erreurs.add(trop)
                        continue
                    }
                    // Une vidéo : sa première image sert de vignette dans la bulle d'envoi.
                    var vignette: String? = null
                    if (mime.startsWith("video/")) {
                        try {
                            val r = MediaMetadataRetriever()
                            try {
                                r.setDataSource(f.path)
                                r.getFrameAtTime(0)?.let { bmp ->
                                    val v = File(dossier, f.name + ".vignette.jpg")
                                    FileOutputStream(v).use { bmp.compress(Bitmap.CompressFormat.JPEG, 80, it) }
                                    vignette = v.path
                                }
                            } finally {
                                r.release()
                            }
                        } catch (_: Exception) {
                        }
                    }
                    pretes.add(Prete(f, mime, nom, vignette))
                } catch (e: Exception) {
                    Log.w(JOURNAL, "copie de $u : ${e.message}")
                    erreurs.add("« $nom » : ${e.message ?: t("lecture impossible")}")
                }
            }
            principal.post {
                Log.i(JOURNAL, "envoi de ${pretes.size} pièce(s) dans $conv (${service.court})")
                val tmps = ArrayList<String>()
                pretes.forEachIndexed { i, p ->
                    val avec = if (i == 0 && service.texteAvecFichier) texte else ""
                    tmps.add(Depot.envoyerFichier(conv, p.fichier, p.mime, p.nom, avec, p.vignette))
                }
                // Google (ou plus aucune pièce) : le texte part en message séparé.
                if (!service.texteAvecFichier || pretes.isEmpty()) Depot.envoyer(conv, texte)?.let { tmps.add(it) }
                envoyes(tmps)
                if (erreurs.isNotEmpty()) erreur(erreurs.joinToString("\n"))
            }
        }, "jasette-envoi").start()
    }
}
