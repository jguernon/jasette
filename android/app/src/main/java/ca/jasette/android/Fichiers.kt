package ca.jasette.android

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.webkit.MimeTypeMap
import android.widget.Toast
import androidx.core.content.FileProvider
import java.io.File

/**
 * Ouvre une pièce jointe (PDF, document, archive…) avec l'app du système :
 * un Intent ACTION_VIEW sur une URI du FileProvider de Jasette
 * (res/xml/fichiers.xml), avec la permission de lecture accordée à cette URI
 * seulement.
 */
object Fichiers {
    /** Vrai si l'Intent est parti ; sinon un message dit pourquoi. */
    fun ouvrir(ctx: Context, chemin: String, mime: String, nom: String): Boolean {
        val f = File(chemin)
        if (!f.isFile) {
            Toast.makeText(ctx, t("Fichier introuvable"), Toast.LENGTH_LONG).show()
            return false
        }
        val uri = try {
            FileProvider.getUriForFile(ctx, ctx.packageName + ".fichiers", f)
        } catch (_: IllegalArgumentException) {
            // Hors des dossiers partagés : ne devrait pas arriver.
            Toast.makeText(ctx, t("Impossible d'ouvrir ce fichier"), Toast.LENGTH_LONG).show()
            return false
        }
        val type = typeDe(mime, nom.ifEmpty { f.name })
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(uri, type)
            clipData = ClipData.newUri(ctx.contentResolver, nom.ifEmpty { f.name }, uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        return try {
            ctx.startActivity(intent)
            true
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(ctx, t("Aucune app pour ouvrir ce fichier"), Toast.LENGTH_LONG).show()
            false
        } catch (_: SecurityException) {
            Toast.makeText(ctx, t("Impossible d'ouvrir ce fichier"), Toast.LENGTH_LONG).show()
            false
        }
    }

    /** Le type MIME du service, sinon celui de l'extension du nom. */
    private fun typeDe(mime: String, nom: String): String {
        if (mime.isNotEmpty() && mime != "application/octet-stream") return mime
        val ext = nom.substringAfterLast('.', "").lowercase()
        return MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: mime.ifEmpty { "*/*" }
    }
}
