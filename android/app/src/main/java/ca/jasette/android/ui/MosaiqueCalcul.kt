package ca.jasette.android.ui

/**
 * La mosaïque d'un groupe sans image (`avatars_groupe`, PROTOCOLE.md §5.13),
 * comme `mosaique()` du bureau : deux photos en diagonale, trois en triangle,
 * quatre en grille. Logique pure (MosaiqueTest), sans Compose : le côté [cote]
 * de chaque petit avatar et la place (x, y) de son coin haut gauche, en
 * fractions du côté de l'avatar entier.
 */
class PlacesMosaique(val cote: Float, val places: List<Pair<Float, Float>>)

fun placesMosaique(n: Int): PlacesMosaique = when (n.coerceAtMost(4)) {
    0 -> PlacesMosaique(1f, emptyList())
    1 -> PlacesMosaique(1f, listOf(0f to 0f))
    2 -> 0.66f.let { d -> PlacesMosaique(d, listOf(0f to 0f, (1 - d) to (1 - d))) }
    3 -> 0.54f.let { d -> PlacesMosaique(d, listOf((1 - d) / 2 to 0f, 0f to (1 - d), (1 - d) to (1 - d))) }
    else -> 0.5f.let { d -> PlacesMosaique(d, listOf(0f to 0f, (1 - d) to 0f, 0f to (1 - d), (1 - d) to (1 - d))) }
}

/** Les initiales d'un membre (deux lettres au plus, comme le bureau) ; null pour un numéro ou un nom vide. */
fun initialesMembre(nom: String): String? {
    val l = nom.split(Regex("\\s+")).mapNotNull { m -> m.firstOrNull { it.isLetterOrDigit() } }.take(2)
        .joinToString("").uppercase()
    return if (l.isEmpty() || l.all { it.isDigit() }) null else l
}
