package ca.jasette.android

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.media.ExifInterface
import android.net.Uri
import java.io.ByteArrayOutputStream
import java.io.File
import java.security.MessageDigest

/**
 * « Changer la photo » (crayon de la fiche) : l'image choisie dans le
 * sélecteur de photos du système, redressée (EXIF), recadrée en carré au
 * centre et réduite à 512 px au plus, en JPEG, dans les fichiers privés de
 * l'app (<files>/contacts/<sha>.jpg). Hors du fil principal.
 */
fun recadrerPhotoPerso(ctx: Context, uri: Uri): File {
    val cr = ctx.contentResolver
    val bornes = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    cr.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bornes) }
    require(bornes.outWidth > 0 && bornes.outHeight > 0) { "image illisible" }
    val ech = ContactsPerso.echantillon(minOf(bornes.outWidth, bornes.outHeight))
    val image = cr.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = ech }) }
        ?: throw IllegalArgumentException("image illisible")
    val rotation = runCatching {
        cr.openInputStream(uri)?.use {
            when (ExifInterface(it).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)) {
                ExifInterface.ORIENTATION_ROTATE_90 -> 90f
                ExifInterface.ORIENTATION_ROTATE_180 -> 180f
                ExifInterface.ORIENTATION_ROTATE_270 -> 270f
                else -> 0f
            }
        }
    }.getOrNull() ?: 0f
    val (x, y, c) = ContactsPerso.carreCentre(image.width, image.height)
    val m = Matrix().apply { if (rotation != 0f) postRotate(rotation) }
    val carre = Bitmap.createBitmap(image, x, y, c, c, m, true)
    val cible = minOf(512, c)
    val petite = if (cible == c) carre else Bitmap.createScaledBitmap(carre, cible, cible, true)
    val octets = ByteArrayOutputStream().also { petite.compress(Bitmap.CompressFormat.JPEG, 88, it) }.toByteArray()
    val nom = MessageDigest.getInstance("SHA-256").digest(octets).joinToString("") { "%02x".format(it.toInt() and 0xff) }.take(32)
    val dossier = File(ctx.filesDir, "contacts").apply { mkdirs() }
    return File(dossier, "$nom.jpg").apply { writeBytes(octets) }
}
