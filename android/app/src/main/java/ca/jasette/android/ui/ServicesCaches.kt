package ca.jasette.android.ui

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import ca.jasette.android.Service

/**
 * Les services cachés (Réglages > Comptes, « Dans la boîte de réception » ; au bureau,
 * `services_caches` de reglages.json) : leurs conversations quittent la boîte
 * de réception, les Head, les favoris et les archives. Rien n'est retiré des
 * marques : c'est un filtre d'affichage, et tout revient à sa place quand on
 * recoche. La vue du service et la recherche les montrent toujours ; les
 * notifications continuent. Propre à l'appareil, jamais synchronisé.
 */
object ServicesCaches {
    var caches by mutableStateOf(emptySet<String>())
        private set
    private var charge = false

    fun charger(ctx: Context) {
        if (charge) return
        charge = true
        caches = ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE)
            .getStringSet("services_caches", emptySet())?.toSet() ?: emptySet()
    }

    fun cache(s: Service): Boolean = s.cle in caches

    fun montrer(ctx: Context, s: Service, montre: Boolean) {
        caches = if (montre) caches - s.cle else caches + s.cle
        ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).edit()
            .putStringSet("services_caches", caches.toSet()).apply()
    }
}
