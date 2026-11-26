package ca.jasette.android

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.util.concurrent.Executors

/**
 * « Partager ce que j'écoute sur Spotify » (serveur/PROTOCOLE.md §5.12), côté
 * Android : [EcouteMusique] lit la session de Spotify et appelle [lu] ; ici,
 * on décide avec [MoteurMusique] (anti-rafale) et on publie par [Notes].
 * Seul l'appareil nommé par la marque `notes_musique` publie. Sur le fil principal.
 */
object Musique {
    const val SPOTIFY = "com.spotify.music"

    private val principal = Handler(Looper.getMainLooper())
    private val moteur = MoteurMusique()
    private var prefs: android.content.SharedPreferences? = null

    /** Ce qui joue maintenant (null : rien, ou pas d'accès). */
    var lecture by mutableStateOf<Ecoute?>(null)
        private set

    /** L'écouteur de notifications est-il branché (accès accordé) ? */
    var branche by mutableStateOf(false)

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences("musique", Context.MODE_PRIVATE)
        prefs = p
        moteur.derniereLecture = p.getLong("derniere_lecture", 0)
        moteur.dernierePublication = p.getLong("derniere_publication", 0)
        moteur.texteMusical = p.getString("texte", null)
        Genres.charger(ctx)
    }

    private fun garder() {
        prefs?.edit()
            ?.putLong("derniere_lecture", moteur.derniereLecture)
            ?.putLong("derniere_publication", moteur.dernierePublication)
            ?.putString("texte", moteur.texteMusical)
            ?.apply()
    }

    /** Le nom de cet appareil, tel que la marque `notes_musique` le garde. */
    fun nomAppareil(): String {
        val m = Build.MODEL.orEmpty().trim()
        val f = Build.MANUFACTURER.orEmpty().trim()
        val nom = if (f.isNotEmpty() && !m.startsWith(f, ignoreCase = true)) "$f $m" else m
        return bornerOctets("Android · ${nom.ifEmpty { "téléphone" }}", 128)
    }

    /** Le partage est-il confié à cet appareil ? */
    fun actifIci(): Boolean = Marques.notesMusique.isNotEmpty() && Marques.notesMusique == nomAppareil()

    /** L'accès aux notifications (nécessaire pour lire la session de Spotify) est-il accordé ? */
    fun accesAccorde(ctx: Context): Boolean {
        val cn = ComponentName(ctx, EcouteMusique::class.java).flattenToString()
        val liste = Settings.Secure.getString(ctx.contentResolver, "enabled_notification_listeners").orEmpty()
        return liste.split(":").any { it == cn }
    }

    /** Ouvre l'écran d'Android où l'on accorde l'accès aux notifications. */
    fun ouvrirAcces(ctx: Context) {
        val i = Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        runCatching { ctx.startActivity(i) }
    }

    /** Active (sur cet appareil) ou désactive le partage. */
    fun activer(oui: Boolean) {
        if (oui) {
            Marques.choisirNotesMusique(nomAppareil())
            pas()
        } else {
            Marques.choisirNotesMusique("")
            pas()
        }
    }

    /** Une nouvelle de la session Spotify (de n'importe quel fil). */
    fun lu(e: Ecoute?) {
        principal.post {
            lecture = e
            pas()
        }
    }

    /** Un pas du moteur : toutes les minutes, et à chaque changement. */
    fun pas() {
        val maintenant = System.currentTimeMillis()
        if (!actifIci()) {
            // Désactivé (pas confié à un autre appareil) : la note musicale part, une fois.
            if (Marques.notesMusique.isEmpty() && moteur.texteMusical != null) {
                moteur.texteMusical = null
                Notes.effacer("musique")
                garder()
            }
            return
        }
        val services = Notes.choisis()
        val limite = Notes.limite(services)
        val format = Marques.notesMusiqueFormat
        val action = moteur.pas(maintenant, lecture, format, Notes.musicale(maintenant), services.isNotEmpty()) { e ->
            val genre = if (format == "genre" && !e.balado) Genres.genre(e.artiste) { principal.post { pas() } } else ""
            // Genre en cours de recherche : on attend la réponse (ou le pas suivant).
            if (genre == null) null else texteMusique(e, format, genre.ifEmpty { null }, Langue.anglais, limite)
        }
        when (action) {
            is MoteurMusique.Action.Publier -> Notes.publier(action.texte, "musique", services)
            MoteurMusique.Action.Effacer -> Notes.effacer("musique")
            null -> {}
        }
        garder()
    }
}

/**
 * Le genre d'un artiste, par MusicBrainz (API publique sans clé), seulement
 * pour le format « genre » : au plus une requête par seconde, un User-Agent
 * sans aucune donnée personnelle, et un cache local artiste → genre (jamais
 * deux fois le même artiste). Hors du fil principal.
 */
object Genres {
    private const val UA = "Jasette/0.1 (https://jonathanguernon.net/jasette)"
    private var prefs: android.content.SharedPreferences? = null
    private val fil = Executors.newSingleThreadExecutor { r -> Thread(r, "genres-musique").apply { isDaemon = true } }
    private val enCours = HashSet<String>()
    /** Les échecs (réseau) : l'heure, pour ne pas réessayer avant une heure. */
    private val echecs = HashMap<String, Long>()
    @Volatile private var derniere = 0L

    fun charger(ctx: Context) {
        prefs = ctx.applicationContext.getSharedPreferences("genres_musique", Context.MODE_PRIVATE)
    }

    /**
     * Le genre gardé de [artiste] ("" : inconnu), ou null s'il faut le chercher :
     * la recherche part alors, et [pret] est appelé à la réponse. Fil principal.
     */
    fun genre(artiste: String, pret: () -> Unit): String? {
        val cle = artiste.trim().lowercase()
        if (cle.isEmpty()) return ""
        prefs?.getString(cle, null)?.let { return it }
        echecs[cle]?.let { if (System.currentTimeMillis() - it < 3_600_000L) return "" }
        if (cle in enCours) return null
        enCours.add(cle)
        fil.execute {
            val g = chercher(artiste.trim())
            Handler(Looper.getMainLooper()).post {
                enCours.remove(cle)
                if (g == null) echecs[cle] = System.currentTimeMillis()
                else prefs?.edit()?.putString(cle, g)?.apply()
                pret()
            }
        }
        return null
    }

    /** La requête (fil à part) : le genre, "" si MusicBrainz n'en connaît pas, null en cas d'échec. */
    private fun chercher(artiste: String): String? {
        val attente = 1_100L - (System.currentTimeMillis() - derniere)
        if (attente > 0) Thread.sleep(attente)
        derniere = System.currentTimeMillis()
        val q = URLEncoder.encode("artist:\"${artiste.replace("\"", "")}\"", "UTF-8")
        return try {
            val c = (URL("https://musicbrainz.org/ws/2/artist/?query=$q&limit=1&fmt=json").openConnection() as HttpURLConnection).apply {
                connectTimeout = 10_000
                readTimeout = 10_000
                setRequestProperty("User-Agent", UA)
                setRequestProperty("Accept", "application/json")
            }
            try {
                if (c.responseCode != 200) return null
                val texte = c.inputStream.use { it.readBytes().toString(Charsets.UTF_8) }
                genreMusicBrainz(texte) ?: ""
            } finally {
                c.disconnect()
            }
        } catch (e: Exception) {
            Log.d("jasette", "musicbrainz : ${e.javaClass.simpleName}")
            null
        }
    }
}
