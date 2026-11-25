package ca.jasette.android

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.widget.Toast

/**
 * Les boutons d'appel de l'en-tête d'une conversation.
 *
 * Google Messages (conversation à deux) : le téléphone ouvre le composeur ;
 * la caméra lance l'appel vidéo de Google Messages, c'est-à-dire Google Meet
 * (ex-Duo, paquet « tachyon ») appelé directement sur le numéro, comme le
 * bouton caméra de Google Messages. Jamais une réunion meet.google.com.
 *
 * Messenger / Instagram : la conversation s'ouvre dans l'app officielle si
 * elle est installée (sinon le site) ; le bouton d'appel s'y trouve.
 */
object Appels {
    private const val MEET = "com.google.android.apps.tachyon"
    private const val MEET_APPEL = "com.google.android.apps.tachyon.action.CALL"
    private const val MESSENGER = "com.facebook.orca"
    private const val INSTAGRAM = "com.instagram.android"

    fun telephone(activite: Activity, numero: String) {
        try {
            activite.startActivity(Intent(Intent.ACTION_DIAL, Uri.fromParts("tel", numero, null)))
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(activite, "Aucune app de téléphone sur cet appareil", Toast.LENGTH_LONG).show()
        }
    }

    /**
     * L'appel vidéo direct par Google Meet. L'action et le paquet sont ceux qu'utilise
     * le composeur de Google (DuoConstants) ; Meet attend d'être lancé « pour résultat ».
     * Les deux extras demandent la vidéo (un extra inconnu est simplement ignoré).
     */
    @Suppress("DEPRECATION")
    fun video(activite: Activity, numero: String) {
        val intent = Intent(MEET_APPEL, Uri.fromParts("tel", numero, null))
            .setPackage(MEET)
            .putExtra("com.google.android.apps.tachyon.extra.IS_AUDIO_ONLY", false)
            .putExtra("android.telecom.extra.START_CALL_WITH_VIDEO_STATE", 3) // VideoProfile.STATE_BIDIRECTIONAL
        try {
            activite.startActivityForResult(intent, 0)
        } catch (_: ActivityNotFoundException) {
            Toast.makeText(
                activite,
                "L'appel vidéo passe par l'app Google Meet, qui n'est pas installée.",
                Toast.LENGTH_LONG,
            ).show()
        } catch (_: SecurityException) {
            Toast.makeText(activite, "Google Meet refuse l'appel vidéo direct", Toast.LENGTH_LONG).show()
        }
    }

    /** Ouvre une conversation Messenger ou Instagram dans l'app officielle, sinon le site. */
    fun ouvrirMeta(activite: Activity, conv: Conv) {
        val id = conv.id.removePrefix(conv.service.prefixe)
        val (url, paquet) = when (conv.service) {
            Service.INSTAGRAM -> "https://www.instagram.com/direct/t/$id/" to INSTAGRAM
            else -> (if (conv.e2ee) "https://www.messenger.com/e2ee/t/$id" else "https://www.messenger.com/t/$id") to MESSENGER
        }
        val vue = Intent(Intent.ACTION_VIEW, Uri.parse(url))
        try {
            activite.startActivity(Intent(vue).setPackage(paquet))
        } catch (_: ActivityNotFoundException) {
            try {
                activite.startActivity(vue)
            } catch (_: ActivityNotFoundException) {
                Toast.makeText(activite, "Aucun navigateur pour ouvrir $url", Toast.LENGTH_LONG).show()
            }
        }
    }
}
