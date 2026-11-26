package ca.jasette.android.ui

import android.provider.Settings
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.keyframes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalContext
import ca.jasette.android.Msg
import ca.jasette.android.emojiSeul

/*
 * Un message qui n'est qu'un émoji : deux fois plus gros, sans bulle, et un
 * « pop » élastique (0 → 1,25 → 0,95 → 1, 420 ms) quand il arrive en direct,
 * reçu ou envoyé (l'emoji rapide compris). Jamais à l'ouverture du fil ni
 * pour l'historique chargé ensuite.
 */

/** La durée du pop. */
private const val DUREE_POP = 420

/** Le grossissement d'un émoji seul par rapport à la taille du texte. */
const val ECHELLE_EMOJI_SEUL = 2f

/**
 * Les arrivées en direct d'un fil : les messages déjà là au premier passage
 * sont « vus » ; ensuite, un nouveau dernier message (récent) qui n'est qu'un
 * émoji reçoit son heure de pop. La confirmation d'un envoi (tmp_id → id)
 * garde le pop de l'envoi local.
 */
class PopsEmoji {
    private val vus = HashSet<String>()
    private var pret = false
    private val debuts = HashMap<String, Long>()

    fun observer(liste: List<Msg>) {
        if (!pret) {
            if (liste.isEmpty()) return
            liste.forEach { voir(it) }
            pret = true
            return
        }
        val dernier = liste.lastOrNull() ?: return
        val neuf = dernier.id !in vus && (dernier.tmpId.isEmpty() || dernier.tmpId !in vus)
        liste.forEach { voir(it) }
        val maintenant = System.currentTimeMillis()
        // « Récent » : un rattrapage d'historique au retour du réseau ne rebondit pas.
        if (neuf && emojiSeul(dernier.text) && maintenant - dernier.time < 60_000) debuts[dernier.id] = maintenant
    }

    private fun voir(m: Msg) {
        vus += m.id
        if (m.tmpId.isNotEmpty()) vus += m.tmpId
    }

    /** L'heure du pop de [m], s'il est encore en cours. */
    fun debut(m: Msg): Long? {
        val d = debuts[m.id] ?: debuts[m.tmpId.ifEmpty { m.id }] ?: return null
        return d.takeIf { System.currentTimeMillis() - it < DUREE_POP + 200 }
    }
}

/** Le pop élastique, si [debut] est là et que le système permet les animations. */
@Composable
fun Modifier.popElastique(debut: Long?): Modifier {
    val ctx = LocalContext.current
    // « Supprimer les animations » : l'échelle des animations à 0.
    val anime = remember {
        runCatching { Settings.Global.getFloat(ctx.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) }
            .getOrDefault(1f) > 0f
    }
    val actif = debut != null && anime
    val echelle = remember { Animatable(if (actif) 0f else 1f) }
    LaunchedEffect(actif) {
        if (actif && echelle.value < 1f) {
            echelle.animateTo(
                1f,
                keyframes {
                    durationMillis = DUREE_POP
                    1.25f at (DUREE_POP * 0.45f).toInt() using FastOutSlowInEasing
                    0.95f at (DUREE_POP * 0.72f).toInt() using FastOutSlowInEasing
                },
            )
        }
    }
    return graphicsLayer {
        scaleX = echelle.value
        scaleY = echelle.value
    }
}
