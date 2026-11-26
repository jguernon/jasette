package ca.jasette.android

/**
 * Faut-il demander ce média au pont ? Jamais s'il est déjà là ou déjà
 * demandé, ni après un échec (pas de boucle : seul un geste, ouvrir la pièce,
 * efface l'échec et redemande).
 */
fun aDemanderMedia(id: String, dejaLa: Boolean, enCours: Boolean, echoue: Boolean): Boolean =
    id.isNotEmpty() && !dejaLa && !enCours && !echoue
