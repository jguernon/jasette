package ca.jasette.android.ui

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.sp

/**
 * « Liste des conversations plus grosse » (Réglages → Apparence, sous le
 * gradateur du texte des messages) : noms, aperçus, heures et titres des Head
 * un point et demi plus gros. Propre à l'appareil.
 */
object Taille {
    var listeGrosse by mutableStateOf(false)
        private set
    private var charge = false

    fun charger(ctx: Context) {
        if (charge) return
        charge = true
        listeGrosse = ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).getBoolean("liste_grosse", false)
    }

    fun choisir(ctx: Context, v: Boolean) {
        listeGrosse = v
        ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).edit().putBoolean("liste_grosse", v).apply()
    }

    /** [base] grossie d'un point et demi si la liste est « plus grosse ». */
    fun liste(base: Float): TextUnit = (if (listeGrosse) base + 1.5f else base).sp
}
