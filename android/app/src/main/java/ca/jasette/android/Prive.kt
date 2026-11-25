package ca.jasette.android

import android.content.ComponentName
import android.content.Context
import android.content.SharedPreferences
import android.content.pm.PackageManager
import android.os.SystemClock
import android.util.Base64
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.PBEKeySpec

/**
 * La Liste privée (comme src/prive.rs du bureau) : le code à 6 chiffres qui
 * ouvre la vue Privées, et le verrou de cette vue.
 *
 * - La liste elle-même est une marque ([Marques.privees], `privees` du §5.5),
 *   partagée par le serveur Jasette. Le code, lui, ne quitte jamais l'appareil :
 *   seul son haché est gardé, dans les SharedPreferences privées « prive »
 *   (PBKDF2WithHmacSHA256, sel aléatoire de 16 octets), jamais le code en clair.
 * - Après 5 mauvais codes : attente croissante (30 s, puis le double à chaque
 *   erreur, 1 h au plus). Le compte est gardé : relancer l'app ne l'efface pas.
 * - La vue se reverrouille quand elle passe en arrière-plan ([PriveActivity]
 *   onStop) ou après 1 minute sans toucher l'écran ([DELAI_MS]).
 * - Le lanceur « Jasette privé » (activity-alias `.LanceurPrive`, icône au
 *   cadenas) n'est activé que lorsque la liste l'est.
 */
object Prive {
    private const val PREFS = "prive"
    private const val ITERATIONS = 120_000
    const val CHIFFRES = 6
    const val DELAI_MS = 60_000L
    private const val ESSAIS_LIBRES = 5
    private const val LANCEUR = "ca.jasette.android.LanceurPrive"

    private var prefs: SharedPreferences? = null

    /** La Liste privée est activée sur cet appareil (un code y est posé). */
    var actif by mutableStateOf(false)
        private set

    /** La vue Privées est ouverte (bon code donné, pas encore reverrouillée). */
    var deverrouille by mutableStateOf(false)
        private set

    /** Dernier toucher dans la vue Privées (horloge depuis le démarrage). */
    private var activite = 0L

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
        actif = !p.getString("hache", null).isNullOrEmpty() && !p.getString("sel", null).isNullOrEmpty()
    }

    private fun deriver(code: String, sel: ByteArray, iterations: Int): ByteArray {
        val spec = PBEKeySpec(code.toCharArray(), sel, iterations, 256)
        try {
            return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256").generateSecret(spec).encoded
        } finally {
            spec.clearPassword()
        }
    }

    private fun b64(o: ByteArray) = Base64.encodeToString(o, Base64.NO_WRAP)

    /** Pose (ou remplace) le code ; active la liste et son lanceur. */
    fun poserCode(ctx: Context, code: String) {
        val sel = ByteArray(16).also { SecureRandom().nextBytes(it) }
        val h = deriver(code, sel, ITERATIONS)
        prefs?.edit()
            ?.putString("sel", b64(sel))
            ?.putString("hache", b64(h))
            ?.putInt("iterations", ITERATIONS)
            ?.putInt("echecs", 0)
            ?.putLong("attente_jusqu", 0L)
            ?.commit()
        actif = true
        poserLanceur(ctx, true)
    }

    /** Vérifie un code (comparaison en temps constant). Compte les échecs. */
    fun verifier(code: String): Boolean {
        val p = prefs ?: return false
        if (attente() > 0) return false
        val ok = try {
            val sel = Base64.decode(p.getString("sel", "") ?: "", Base64.NO_WRAP)
            val attendu = Base64.decode(p.getString("hache", "") ?: "", Base64.NO_WRAP)
            sel.isNotEmpty() && attendu.isNotEmpty() &&
                MessageDigest.isEqual(deriver(code, sel, p.getInt("iterations", ITERATIONS)), attendu)
        } catch (_: Exception) {
            false
        }
        if (ok) {
            if (p.getInt("echecs", 0) != 0) p.edit().putInt("echecs", 0).putLong("attente_jusqu", 0L).commit()
        } else {
            val n = p.getInt("echecs", 0) + 1
            val e = p.edit().putInt("echecs", n)
            if (n >= ESSAIS_LIBRES) {
                val secondes = (30L shl (n - ESSAIS_LIBRES).coerceAtMost(7)).coerceAtMost(3600L)
                e.putLong("attente_jusqu", System.currentTimeMillis() + secondes * 1000)
            }
            e.commit()
        }
        return ok
    }

    /** Les secondes à attendre avant le prochain essai (0 : on peut). */
    fun attente(): Long {
        val reste = (prefs?.getLong("attente_jusqu", 0L) ?: 0L) - System.currentTimeMillis()
        return if (reste > 0) (reste + 999) / 1000 else 0
    }

    /** Désactive la liste ici : le haché part, le lanceur aussi, la vue se ferme. */
    fun desactiver(ctx: Context) {
        prefs?.edit()?.clear()?.commit()
        actif = false
        verrouiller()
        poserLanceur(ctx, false)
    }

    fun deverrouiller() {
        deverrouille = true
        activite = SystemClock.elapsedRealtime()
    }

    fun verrouiller() {
        deverrouille = false
    }

    /** Un toucher dans la vue Privées : le délai repart. */
    fun toucher() {
        activite = SystemClock.elapsedRealtime()
    }

    /** Plus d'une minute sans toucher : vrai s'il faut reverrouiller. */
    fun expiree() = deverrouille && SystemClock.elapsedRealtime() - activite >= DELAI_MS

    /** Le lanceur au cadenas : activé avec la liste, désactivé avec elle. */
    fun poserLanceur(ctx: Context, oui: Boolean) {
        val etat = if (oui) PackageManager.COMPONENT_ENABLED_STATE_ENABLED else PackageManager.COMPONENT_ENABLED_STATE_DISABLED
        try {
            ctx.packageManager.setComponentEnabledSetting(
                ComponentName(ctx, LANCEUR), etat, PackageManager.DONT_KILL_APP,
            )
        } catch (e: Exception) {
            android.util.Log.w("jasette", "lanceur privé : ${e.message}")
        }
    }
}
