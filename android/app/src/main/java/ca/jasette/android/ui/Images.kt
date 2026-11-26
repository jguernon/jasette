package ca.jasette.android.ui

import android.content.ContentResolver
import android.graphics.BitmapFactory
import android.net.Uri
import android.util.LruCache
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Les images locales (avatars et photos téléchargées par les ponts, photos des
 * contacts du téléphone par leur URI content://), décodées
 * hors du fil principal, réduites à [cote] pixels au plus, gardées en mémoire.
 */
private val cache = object : LruCache<String, ImageBitmap>(48 * 1024 * 1024) {
    override fun sizeOf(key: String, value: ImageBitmap) = value.width * value.height * 4
}

/**
 * Oublie les images lues par le ContentResolver (photos des contacts) : une
 * photo changée garde parfois la même URI.
 */
fun oublierImagesContenu() {
    for (k in cache.snapshot().keys) if (k.startsWith("content:")) cache.remove(k)
}

/** Oublie les images décodées d'un fichier (réécrit sous le même nom). */
fun oublierImage(chemin: String) {
    for (k in cache.snapshot().keys) if (k.startsWith("$chemin@")) cache.remove(k)
}

/**
 * Décode [chemin] : un fichier, ou une URI « content:// » (photo d'un contact,
 * lue directement par le ContentResolver, sans copie sur le disque).
 */
private fun decoder(chemin: String, cote: Int, resolveur: ContentResolver, pixels: Int = 0): ImageBitmap? {
    val cle = if (pixels > 0) "$chemin@$cote@px$pixels" else "$chemin@$cote"
    cache.get(cle)?.let { return it }
    return try {
        val contenu = chemin.startsWith("content:")
        fun lire(o: BitmapFactory.Options): android.graphics.Bitmap? =
            if (contenu) {
                resolveur.openInputStream(Uri.parse(chemin))?.use { BitmapFactory.decodeStream(it, null, o) }
            } else {
                BitmapFactory.decodeFile(chemin, o)
            }
        val bornes = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        lire(bornes)
        var echantillon = 1
        while (bornes.outWidth / (echantillon * 2) >= cote && bornes.outHeight / (echantillon * 2) >= cote) echantillon *= 2
        val bmp = lire(BitmapFactory.Options().apply { inSampleSize = echantillon })
            ?: return null
        val fin = if (pixels > 0) {
            // Live et Sirène : l'avatar en gros pixels d'époque (carré central
            // réduit à pixels × pixels, agrandi ensuite sans lissage).
            val c = minOf(bmp.width, bmp.height)
            val carre = android.graphics.Bitmap.createBitmap(bmp, (bmp.width - c) / 2, (bmp.height - c) / 2, c, c)
            android.graphics.Bitmap.createScaledBitmap(carre, pixels, pixels, true)
        } else {
            bmp
        }
        fin.asImageBitmap().also { cache.put(cle, it) }
    } catch (_: Throwable) {
        null
    }
}

@Composable
fun ImageFichier(
    chemin: String,
    modifier: Modifier = Modifier,
    cote: Int = 1024,
    contentScale: ContentScale = ContentScale.Crop,
    filtre: androidx.compose.ui.graphics.ColorFilter? = null,
    /** > 0 : l'image réduite à pixels × pixels et dessinée en gros pixels (avatars rétro). */
    pixels: Int = 0,
    enAttente: @Composable () -> Unit = {},
) {
    val resolveur = LocalContext.current.applicationContext.contentResolver
    val cle = if (pixels > 0) "$chemin@$cote@px$pixels" else "$chemin@$cote"
    val image by produceState(cache.get(cle), chemin, cote, pixels) {
        if (value == null) value = withContext(Dispatchers.IO) { decoder(chemin, cote, resolveur, pixels) }
    }
    val img = image
    if (img != null) {
        Image(
            img, contentDescription = null, modifier = modifier, contentScale = contentScale, colorFilter = filtre,
            filterQuality = if (pixels > 0) androidx.compose.ui.graphics.FilterQuality.None else androidx.compose.ui.graphics.drawscope.DrawScope.DefaultFilterQuality,
        )
    } else {
        Box(modifier) { enAttente() }
    }
}

/** Live et Sirène : les avatars en 24 × 24 gros pixels, comme les photos d'époque. */
const val PIXELS_RETRO = 24
