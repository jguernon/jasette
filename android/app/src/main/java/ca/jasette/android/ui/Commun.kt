package ca.jasette.android.ui

import ca.jasette.android.t
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Etat
import ca.jasette.android.R
import ca.jasette.android.Service
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/** L'icône du service (les mêmes PNG que le bureau, assets/services). */
fun iconeService(s: Service) = when (s) {
    Service.GOOGLE -> R.drawable.service_google
    Service.MESSENGER -> R.drawable.service_messenger
    Service.INSTAGRAM -> R.drawable.service_instagram
}

fun couleurEtat(e: Etat) = when (e) {
    Etat.PRET -> P.succes
    Etat.TELEPHONE_HORS_LIGNE, Etat.NON_ASSOCIE, Etat.ARRETE -> P.danger
    else -> P.averti
}

/** Une couleur « #rrggbb » des ponts. */
fun couleurHex(s: String): Color? {
    val h = s.removePrefix("#")
    if (h.length != 6) return null
    return h.toLongOrNull(16)?.let { Color(0xFF000000 or it) }
}

/** Les initiales d'un nom (comme `avatar()` du bureau) ; null pour un numéro. */
private fun initiales(nom: String): String? {
    val l = nom.split(Regex("\\s+")).mapNotNull { m -> m.firstOrNull { it.isLetterOrDigit() } }.take(2)
        .joinToString("").uppercase()
    return if (l.isEmpty() || l.all { it.isDigit() }) null else l
}

/** L'avatar d'une conversation, avec le petit badge de son service. */
@Composable
fun Avatar(conv: Conv, taille: Dp, badge: Boolean = true) {
    val photo = Depot.avatars[conv.id]
    Box(Modifier.size(taille)) {
        val rond = Modifier.size(taille).clip(CircleShape)
        if (photo != null) {
            ImageFichier(photo, rond.background(P.surface), cote = 256)
        } else {
            val base = couleurHex(conv.color) ?: P.avatarDefaut
            Box(rond.background(base), contentAlignment = Alignment.Center) {
                val i = if (conv.group) null else initiales(conv.name)
                if (i != null) {
                    Text(i, color = Color.White, fontSize = (taille.value * 0.38f).sp, fontWeight = FontWeight.SemiBold)
                } else {
                    Icon(Icons.Filled.Person, null, tint = Color.White, modifier = Modifier.size(taille * 0.5f))
                }
            }
        }
        if (badge) {
            val b = taille * 0.36f
            Image(
                painterResource(iconeService(conv.service)), null,
                Modifier.align(Alignment.BottomEnd).size(b).clip(CircleShape)
                    .border(2.dp, P.fond, CircleShape),
            )
        }
    }
}

private val heureFmt = DateTimeFormatter.ofPattern("HH:mm")
private val jourFmtFr = DateTimeFormatter.ofPattern("d MMM", Locale.CANADA_FRENCH)
private val jourLongFmtFr = DateTimeFormatter.ofPattern("EEEE d MMMM", Locale.CANADA_FRENCH)
private val jourFmtEn = DateTimeFormatter.ofPattern("MMM d", Locale.ENGLISH)
private val jourLongFmtEn = DateTimeFormatter.ofPattern("EEEE, MMMM d", Locale.ENGLISH)
private val jourFmt get() = if (ca.jasette.android.Langue.anglais) jourFmtEn else jourFmtFr
private val jourLongFmt get() = if (ca.jasette.android.Langue.anglais) jourLongFmtEn else jourLongFmtFr

/** L'heure dans la liste : l'heure aujourd'hui, sinon la date. */
fun heureListe(ms: Long): String {
    if (ms <= 0) return ""
    val t = Instant.ofEpochMilli(ms).atZone(ZoneId.systemDefault())
    return if (t.toLocalDate() == LocalDate.now()) t.format(heureFmt) else t.format(jourFmt)
}

fun heure(ms: Long): String = Instant.ofEpochMilli(ms).atZone(ZoneId.systemDefault()).format(heureFmt)

fun jour(ms: Long): String {
    val d = Instant.ofEpochMilli(ms).atZone(ZoneId.systemDefault()).toLocalDate()
    val auj = LocalDate.now()
    return when (d) {
        auj -> t("Aujourd'hui")
        auj.minusDays(1) -> t("Hier")
        else -> d.format(jourLongFmt).replaceFirstChar { it.uppercase() }
    }
}

fun jourDe(ms: Long): LocalDate = Instant.ofEpochMilli(ms).atZone(ZoneId.systemDefault()).toLocalDate()

/** Icônes absentes de material-icons-core, tracées d'après Material Symbols. */
object Icones {
    val camera: ImageVector by lazy {
        ImageVector.Builder("camera", 24.dp, 24.dp, 24f, 24f).addPath(
            addPathNodes(
                "M17,10.5V7c0,-0.55 -0.45,-1 -1,-1H4c-0.55,0 -1,0.45 -1,1v10c0,0.55 0.45,1 1,1h12c0.55,0 1,-0.45 1,-1v-3.5l4,4v-11l-4,4z",
            ),
            fill = SolidColor(Color.Black),
        ).build()
    }
}
