package ca.jasette.android.ui

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Ecrit
import ca.jasette.android.t
import kotlin.math.PI
import kotlin.math.sin

/**
 * Le saut d'un des trois points (0 au repos, 1 en haut) : chacun bondit à son
 * tour, puis se repose, en boucle (1,2 s).
 */
@Composable
private fun sautPoint(i: Int): Float {
    val boucle = rememberInfiniteTransition(label = "ecrit")
    val t by boucle.animateFloat(
        0f, 1f, infiniteRepeatable(tween(1200, easing = LinearEasing), RepeatMode.Restart), label = "ecrit-$i",
    )
    val phase = ((t - i * 0.16f) % 1f + 1f) % 1f
    return if (phase < 0.4f) sin(phase / 0.4f * PI).toFloat() else 0f
}

/**
 * « En train d'écrire » en bas du fil : une bulle reçue (couleur des bulles
 * reçues du thème, ou du Mood) aux trois points qui rebondissent ; carrés en
 * EGA, Live et Sirène. Dans un groupe, les initiales de qui écrit, si on le sait.
 */
@Composable
fun BulleEcrit(e: Ecrit, groupe: Boolean) {
    val mood = LocalMood.current
    val fond = mood?.recue(P.clair) ?: P.surface
    val carre = P.ega || P.retro
    val forme = if (carre) androidx.compose.ui.graphics.RectangleShape else formeBulle(18.dp, 18.dp, 18.dp, 4.dp)
    Row(Modifier.fillMaxWidth().padding(top = 4.dp, bottom = 2.dp), verticalAlignment = Alignment.Bottom) {
        if (groupe && e.nom.isNotEmpty()) {
            val lettres = e.nom.split(Regex("\\s+")).mapNotNull { m -> m.firstOrNull { it.isLetterOrDigit() } }.take(2)
                .joinToString("").uppercase()
            Box(
                Modifier.size(24.dp).clip(formeRonde).background(P.avatarDefaut),
                contentAlignment = Alignment.Center,
            ) { Text(lettres, color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.SemiBold) }
            Spacer(Modifier.width(6.dp))
        }
        Row(
            Modifier.clip(forme).background(fond)
                .let { if (P.bordBulle.alpha > 0f) it.border(1.dp, P.bordBulle, forme) else it }
                .padding(horizontal = 14.dp, vertical = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(5.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            for (i in 0 until 3) {
                val s = sautPoint(i)
                Box(
                    Modifier.offset(y = (-5 * s).dp).size(7.dp).alpha(0.45f + 0.55f * s)
                        .clip(if (carre) androidx.compose.ui.graphics.RectangleShape else formeRonde).background(P.textePale),
                )
            }
        }
    }
}

/** « écrit… » à la place de l'aperçu dans la liste, avec trois petits points animés. */
@Composable
fun ApercuEcrit(taille: TextUnit, couleur: Color, modifier: Modifier = Modifier) {
    val carre = P.ega || P.retro
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        Text(t("écrit…"), color = couleur, fontSize = taille, fontWeight = FontWeight.SemiBold, maxLines = 1)
        Spacer(Modifier.width(4.dp))
        for (i in 0 until 3) {
            val s = sautPoint(i)
            Box(
                Modifier.padding(top = 4.dp).size(3.dp).alpha(0.35f + 0.65f * s)
                    .clip(if (carre) androidx.compose.ui.graphics.RectangleShape else formeRonde).background(couleur),
            )
            Spacer(Modifier.width(2.dp))
        }
        Spacer(Modifier.height(0.dp))
    }
}
