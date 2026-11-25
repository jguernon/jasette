package ca.jasette.android.ui

import android.graphics.BitmapFactory
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
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Les images locales (avatars et photos téléchargées par les ponts), décodées
 * hors du fil principal, réduites à [cote] pixels au plus, gardées en mémoire.
 */
private val cache = object : LruCache<String, ImageBitmap>(48 * 1024 * 1024) {
    override fun sizeOf(key: String, value: ImageBitmap) = value.width * value.height * 4
}

private fun decoder(chemin: String, cote: Int): ImageBitmap? {
    val cle = "$chemin@$cote"
    cache.get(cle)?.let { return it }
    return try {
        val bornes = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(chemin, bornes)
        var echantillon = 1
        while (bornes.outWidth / (echantillon * 2) >= cote && bornes.outHeight / (echantillon * 2) >= cote) echantillon *= 2
        val bmp = BitmapFactory.decodeFile(chemin, BitmapFactory.Options().apply { inSampleSize = echantillon })
            ?: return null
        bmp.asImageBitmap().also { cache.put(cle, it) }
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
    enAttente: @Composable () -> Unit = {},
) {
    val image by produceState(cache.get("$chemin@$cote"), chemin, cote) {
        if (value == null) value = withContext(Dispatchers.IO) { decoder(chemin, cote) }
    }
    val img = image
    if (img != null) {
        Image(img, contentDescription = null, modifier = modifier, contentScale = contentScale)
    } else {
        Box(modifier) { enAttente() }
    }
}
