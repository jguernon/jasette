package ca.jasette.android

import android.content.ComponentName
import android.media.MediaMetadata
import android.media.session.MediaController
import android.media.session.MediaSessionManager
import android.media.session.PlaybackState
import android.os.Handler
import android.os.Looper
import android.service.notification.NotificationListenerService
import android.util.Log

/**
 * L'écouteur de notifications : Android ne donne les sessions multimédia des
 * autres apps (MediaSessionManager.getActiveSessions) qu'à un écouteur à qui
 * l'utilisateur a accordé l'accès aux notifications. On n'y lit **aucune**
 * notification : seulement la session de Spotify (titre, artiste, lecture),
 * transmise à [Musique] (« Partager ce que j'écoute », PROTOCOLE.md §5.12).
 */
class EcouteMusique : NotificationListenerService() {
    private val principal = Handler(Looper.getMainLooper())
    private var gestionnaire: MediaSessionManager? = null
    private var controleur: MediaController? = null

    private val rappel = object : MediaController.Callback() {
        override fun onPlaybackStateChanged(state: PlaybackState?) = lire()
        override fun onMetadataChanged(metadata: MediaMetadata?) = lire()
        override fun onSessionDestroyed() {
            controleur?.unregisterCallback(this)
            controleur = null
            lire()
        }
    }

    private val sessionsChangees = MediaSessionManager.OnActiveSessionsChangedListener { suivre(it) }

    /** Un pas par minute : l'anti-rafale et l'effacement après 20 minutes sans lecture. */
    private val minute = object : Runnable {
        override fun run() {
            Musique.pas()
            principal.postDelayed(this, 60_000L)
        }
    }

    override fun onListenerConnected() {
        Musique.branche = true
        val g = getSystemService(MediaSessionManager::class.java)
        gestionnaire = g
        try {
            val cn = ComponentName(this, EcouteMusique::class.java)
            g?.addOnActiveSessionsChangedListener(sessionsChangees, cn, principal)
            suivre(g?.getActiveSessions(cn))
        } catch (e: SecurityException) {
            Log.w("jasette", "sessions multimédia refusées : ${e.message}")
        }
        principal.removeCallbacks(minute)
        principal.postDelayed(minute, 60_000L)
    }

    override fun onListenerDisconnected() {
        Musique.branche = false
        principal.removeCallbacks(minute)
        runCatching { gestionnaire?.removeOnActiveSessionsChangedListener(sessionsChangees) }
        controleur?.unregisterCallback(rappel)
        controleur = null
        Musique.lu(null)
    }

    /** La session de Spotify parmi les sessions actives (une seule suivie). */
    private fun suivre(sessions: List<MediaController>?) {
        val s = sessions?.firstOrNull { it.packageName == Musique.SPOTIFY }
        if (s?.sessionToken != controleur?.sessionToken) {
            controleur?.unregisterCallback(rappel)
            controleur = s
            s?.registerCallback(rappel, principal)
        }
        lire()
    }

    private fun lire() {
        val c = controleur
        val joue = c?.playbackState?.state == PlaybackState.STATE_PLAYING
        val m = c?.metadata
        Musique.lu(
            if (!joue || m == null) null else ecouteSpotify(
                m.getString(MediaMetadata.METADATA_KEY_TITLE),
                m.getString(MediaMetadata.METADATA_KEY_ARTIST) ?: m.getString(MediaMetadata.METADATA_KEY_ALBUM_ARTIST),
                m.getString(MediaMetadata.METADATA_KEY_ALBUM),
                m.getString(MediaMetadata.METADATA_KEY_MEDIA_ID),
            ),
        )
    }
}
