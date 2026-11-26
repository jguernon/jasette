package ca.jasette.android.ui

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * « Pastille sur l'onglet Canaux » et « Pastille sur l'onglet Archives »
 * (Réglages → Apparence) : décochée, l'onglet n'a plus de pastille de non-lus
 * dans le rail ni dans la barre du bas. Cochées par défaut ; propres à
 * l'appareil (préférences « affichage », comme [Taille]).
 */
object Pastilles {
    var canaux by mutableStateOf(true)
        private set
    var archives by mutableStateOf(true)
        private set
    private var charge = false

    fun charger(ctx: Context) {
        if (charge) return
        charge = true
        val p = ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE)
        canaux = p.getBoolean("pastille_canaux", true)
        archives = p.getBoolean("pastille_archives", true)
    }

    fun choisirCanaux(ctx: Context, v: Boolean) {
        canaux = v
        ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).edit().putBoolean("pastille_canaux", v).apply()
    }

    fun choisirArchives(ctx: Context, v: Boolean) {
        archives = v
        ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).edit().putBoolean("pastille_archives", v).apply()
    }
}
