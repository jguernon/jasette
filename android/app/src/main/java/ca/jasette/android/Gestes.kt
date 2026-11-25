package ca.jasette.android

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/** Ce que fait un glissement sur une conversation de la liste. */
enum class ActionGeste(val cle: String) {
    REPONDRE("repondre"),
    ARCHIVER("archiver"),
    SUPPRIMER("supprimer"),
    RIEN("rien");

    companion object {
        fun de(cle: String?, defaut: ActionGeste) = entries.firstOrNull { it.cle == cle } ?: defaut
    }
}

/**
 * Réglages → Gestes : l'action de chaque côté du glissement dans la liste
 * (droite = Répondre, gauche = Archiver par défaut). Gardés dans des
 * SharedPreferences privées « gestes » ; ce sont des états Compose, donc la
 * liste suit aussitôt un changement.
 */
object Gestes {
    private const val PREFS = "gestes"
    private const val DROITE = "droite"
    private const val GAUCHE = "gauche"
    private var prefs: android.content.SharedPreferences? = null

    var droite by mutableStateOf(ActionGeste.REPONDRE)
        private set
    var gauche by mutableStateOf(ActionGeste.ARCHIVER)
        private set

    /**
     * La conversation à ouvrir le curseur dans le champ de saisie et le clavier
     * ouvert (glissement « Répondre ») ; le fil la remet à null une fois fait.
     */
    var clavierPour by mutableStateOf<String?>(null)

    /** Lit les réglages (une seule fois ; on peut l'appeler de partout). */
    fun charger(ctx: Context) {
        if (prefs != null) return
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
        droite = ActionGeste.de(p.getString(DROITE, null), ActionGeste.REPONDRE)
        gauche = ActionGeste.de(p.getString(GAUCHE, null), ActionGeste.ARCHIVER)
    }

    fun choisirDroite(a: ActionGeste) {
        droite = a
        prefs?.edit()?.putString(DROITE, a.cle)?.commit()
    }

    fun choisirGauche(a: ActionGeste) {
        gauche = a
        prefs?.edit()?.putString(GAUCHE, a.cle)?.commit()
    }
}

/** Archiver et supprimer : Google Messages seulement pour l'instant (comme le menu). */
val Conv.peutArchiver: Boolean get() = service == Service.GOOGLE
val Conv.peutSupprimer: Boolean get() = service == Service.GOOGLE
