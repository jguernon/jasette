package ca.jasette.android

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioManager
import android.media.SoundPool
import android.os.SystemClock
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.setValue

/**
 * Les sons des conversations, comme Messenger (marque `sons_conversation`,
 * serveur/PROTOCOLE.md §5.5) : un jeu (Bulle, Bois, Verre) pour l'envoi et la
 * réception, plus « écrit » (les « … ») et l'émoji rapide, communs aux jeux.
 * Les mp3 sont dans res/raw (copies de assets/sons, normalisées : `GAIN_RAW`).
 *
 * Seulement dans la conversation ouverte et visible, l'app au premier plan,
 * et jamais si la sonnerie est coupée (silencieux ou vibreur). SoundPool, en
 * « sonification d'assistance » (le volume des sons de l'interface).
 */
object Sons {
    enum class Son { ENVOYE, RECU, ECRIT, EMOJI }

    /** Les jeux, dans l'ordre des Réglages : (id, nom). */
    val JEUX = listOf("bulle" to "Bulle", "bois" to "Bois", "verre" to "Verre", "aucun" to "Aucun son")

    /** Le volume au cran 100 % (celui d'avant le curseur), relatif aux mp3 d'assets/sons. */
    private const val VOLUME = 0.5f

    /**
     * Les crans du curseur de volume (Réglages > Sons), en % : 100 garde le
     * volume d'avant, jusqu'à 200 pour monter, 0 coupe les sons (le jeu reste).
     * Comme au bureau (src/sons.rs).
     */
    val CRANS_VOLUME = listOf(0, 25, 50, 75, 100, 125, 150, 175, 200)

    /** Le volume choisi sur cet appareil (préférences locales, jamais synchronisé). */
    var volume by mutableIntStateOf(100)
        private set
    private var prefs: android.content.SharedPreferences? = null

    /**
     * SoundPool ne monte pas au-dessus de 1 : les copies de res/raw sont donc
     * normalisées (crête vers -1,5 dBFS) et jouées plus bas d'autant, en dB
     * (mesuré : écart de crête RMS avec assets/sons). Plafonner à 1 garde
     * ainsi la crête sous -1,5 dBFS : le limiteur.
     */
    private val GAIN_RAW = mapOf(
        R.raw.ecrit to 4.62f, R.raw.emoji to 27.1f,
        R.raw.envoye_bois to 20.96f, R.raw.recu_bois to 17.15f,
        R.raw.envoye_bulle to 17.18f, R.raw.recu_bulle to 19.25f,
        R.raw.envoye_verre to 19.65f, R.raw.recu_verre to 20.25f,
    )

    /** Le cran le plus proche d'un volume (une valeur venue d'ailleurs). */
    fun cran(v: Int): Int = CRANS_VOLUME.indices.minByOrNull { kotlin.math.abs(CRANS_VOLUME[it] - v) } ?: 4

    /** Le gain d'un cran, sur une courbe au carré (25 % ≈ -24 dB, 200 % ≈ +12 dB). */
    fun gain(v: Int): Float {
        val r = v.coerceIn(0, 200) / 100f
        return VOLUME * r * r
    }

    /** Le volume SoundPool d'un fichier de res/raw au volume [v], plafonné à 1. */
    private fun volumePool(r: Int, v: Int): Float =
        (gain(v) * Math.pow(10.0, -(GAIN_RAW[r] ?: 0f) / 20.0).toFloat()).coerceAtMost(1f)

    /** Réglages > Sons : poser un cran (gardé tout de suite). */
    fun choisirVolume(v: Int) {
        if (v == volume) return
        volume = v
        prefs?.edit()?.putInt("volume_sons", v)?.apply()
    }
    /** Au plus un son de réception par 400 ms. */
    private const val ECART_RECU_MS = 400L
    /** Les « … » ne resonnent que s'ils avaient disparu depuis au moins 3 s. */
    private const val ECART_ECRIT_MS = 3_000L
    /** Un message plus vieux n'est pas « nouveau » (synchro, rattrapage). */
    private const val RECU_RECENT_MS = 120_000L

    private var pool: SoundPool? = null
    private var audio: AudioManager? = null
    /** Le fichier (res/raw) → l'id SoundPool. */
    private val charges = HashMap<Int, Int>()
    private var dernierRecu = 0L
    private val ecritParti = HashMap<String, Long>()

    fun init(ctx: Context) {
        if (pool != null) return
        val app = ctx.applicationContext
        prefs = app.getSharedPreferences("sons", Context.MODE_PRIVATE).also {
            volume = CRANS_VOLUME[cran(it.getInt("volume_sons", 100))]
        }
        audio = app.getSystemService(Context.AUDIO_SERVICE) as? AudioManager
        val p = SoundPool.Builder().setMaxStreams(3).setAudioAttributes(
            AudioAttributes.Builder()
                .setUsage(AudioAttributes.USAGE_ASSISTANCE_SONIFICATION)
                .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                .build(),
        ).build()
        for (r in listOf(
            R.raw.envoye_bulle, R.raw.recu_bulle, R.raw.envoye_bois, R.raw.recu_bois,
            R.raw.envoye_verre, R.raw.recu_verre, R.raw.ecrit, R.raw.emoji,
        )) {
            charges[r] = p.load(app, r, 1)
        }
        pool = p
    }

    private fun fichier(jeu: String, son: Son): Int? = when {
        jeu == "aucun" -> null
        son == Son.ECRIT -> R.raw.ecrit
        son == Son.EMOJI -> R.raw.emoji
        jeu == "bois" -> if (son == Son.ENVOYE) R.raw.envoye_bois else R.raw.recu_bois
        jeu == "verre" -> if (son == Son.ENVOYE) R.raw.envoye_verre else R.raw.recu_verre
        else -> if (son == Son.ENVOYE) R.raw.envoye_bulle else R.raw.recu_bulle
    }

    /** Joue un son de [jeu] au volume choisi, si la sonnerie n'est pas coupée. */
    fun jouer(jeu: String, son: Son) {
        val r = fichier(jeu, son) ?: return
        if (volume <= 0 || audio?.ringerMode != AudioManager.RINGER_MODE_NORMAL) return
        val id = charges[r] ?: return
        val v = volumePool(r, volume)
        pool?.play(id, v, v, 1, 0, 1f)
    }

    /** La conversation est-elle ouverte et visible (l'app au premier plan) ? */
    private fun ici(conv: String) = Depot.visible && Depot.ouverte == conv && Marques.sonsConversation != "aucun" && volume > 0

    /** Mon message part (bulle locale) ; [emoji] : l'émoji rapide. */
    fun envoi(conv: String, emoji: Boolean) {
        if (ici(conv)) jouer(Marques.sonsConversation, if (emoji) Son.EMOJI else Son.ENVOYE)
    }

    /** Un nouveau message reçu (pas l'historique, ni une réaction), d'heure [temps] (ms). */
    fun recu(conv: String, temps: Long) {
        if (!ici(conv) || temps < System.currentTimeMillis() - RECU_RECENT_MS) return
        val t = SystemClock.elapsedRealtime()
        if (t - dernierRecu < ECART_RECU_MS) return
        dernierRecu = t
        jouer(Marques.sonsConversation, Son.RECU)
    }

    /** Les « … » apparaissent. */
    fun ecrit(conv: String) {
        val parti = ecritParti[conv]
        if (parti != null && SystemClock.elapsedRealtime() - parti < ECART_ECRIT_MS) return
        if (ici(conv)) jouer(Marques.sonsConversation, Son.ECRIT)
    }

    /** Les « … » viennent de disparaître. */
    fun ecritParti(conv: String) {
        val t = SystemClock.elapsedRealtime()
        ecritParti.entries.removeAll { t - it.value >= ECART_ECRIT_MS }
        ecritParti[conv] = t
    }

    /** Réglages > Sons : l'aperçu d'un jeu, l'envoi puis la réception 0,5 s après. */
    fun apercu(jeu: String) {
        if (jeu == "aucun") return
        jouer(jeu, Son.ENVOYE)
        android.os.Handler(android.os.Looper.getMainLooper()).postDelayed({ jouer(jeu, Son.RECU) }, 500)
    }
}
