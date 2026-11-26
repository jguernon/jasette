package ca.jasette.android.ui

/**
 * Le débordement des services dans le rail (écran large) : combien de tuiles
 * de service montrer, et faut-il le bouton « ••• ». Logique pure (testée dans
 * RailTuilesTest), sans Compose.
 *
 * [n] : les services connectés ; [hauteur] : la place réelle entre le trait
 * sous Archives et l'engrenage (dp) ; [tuile] : la hauteur d'une tuile, écart
 * compris ; [marge] : l'air gardé au-dessus de l'engrenage.
 *
 * Tout tient : tous les services, pas de « ••• ». Sinon, autant de tuiles
 * qu'il en tient moins une, et la dernière place va au « ••• » (qui ouvre la
 * liste de tous les services).
 */
data class TuilesRail(val montres: Int, val plus: Boolean)

fun tuilesRail(n: Int, hauteur: Float, tuile: Float, marge: Float): TuilesRail {
    if (n <= 0) return TuilesRail(0, false)
    val places = if (tuile <= 0f) n else ((hauteur - marge) / tuile).toInt().coerceAtLeast(0)
    if (n <= places) return TuilesRail(n, false)
    return TuilesRail((places - 1).coerceAtLeast(0), true)
}

/** Les non-lus des services cachés par le débordement (la pastille du « ••• »). */
fun nonLusCaches(nonLus: List<Int>, montres: Int): Int = nonLus.drop(montres).sum()
