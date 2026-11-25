package ca.jasette.android.ui

import ca.jasette.android.t
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
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

/** La forme des logos de services : un carré aux coins arrondis (30 % de la taille, comme `logo()` du bureau). */
fun formeLogo(taille: Dp) = forme(taille * 0.3f)

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
        val rond = Modifier.size(taille).clip(formeRonde)
        if (photo != null) {
            ImageFichier(photo, rond.background(P.surface), cote = 256)
        } else {
            val base = couleurHex(conv.color) ?: P.avatarDefaut
            Box(rond.background(fondAvatar(base)), contentAlignment = Alignment.Center) {
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
                Modifier.align(Alignment.BottomEnd).size(b).border(2.dp, P.fond, formeLogo(b))
                    .padding(1.dp).clip(formeLogo(b)),
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
    private fun icone(nom: String, chemin: String): ImageVector =
        ImageVector.Builder(nom, 24.dp, 24.dp, 24f, 24f)
            .addPath(addPathNodes(chemin), fill = SolidColor(Color.Black)).build()

    val inbox by lazy {
        icone("inbox", "M19,3H4.99c-1.11,0 -1.98,0.89 -1.98,2L3,19c0,1.1 0.88,2 1.99,2H19c1.1,0 2,-0.9 2,-2V5C21,3.89 20.1,3 19,3zM19,15h-4c0,1.66 -1.35,3 -3,3s-3,-1.34 -3,-3H4.99V5H19V15z")
    }
    val archive by lazy {
        icone("archive", "M20.54,5.23l-1.39,-1.68C18.88,3.21 18.47,3 18,3H6c-0.47,0 -0.88,0.21 -1.16,0.55L3.46,5.23C3.17,5.57 3,6.02 3,6.5V19c0,1.1 0.9,2 2,2h14c1.1,0 2,-0.9 2,-2V6.5c0,-0.48 -0.17,-0.93 -0.46,-1.27zM12,17.5L6.5,12H10v-2h4v2h3.5L12,17.5zM5.12,5l0.81,-1h12l0.94,1H5.12z")
    }
    val desarchiver by lazy {
        icone("unarchive", "M20.55,5.22l-1.39,-1.68C18.88,3.21 18.47,3 18,3H6c-0.47,0 -0.88,0.21 -1.15,0.55L3.46,5.22C3.17,5.57 3,6.01 3,6.5V19c0,1.1 0.89,2 2,2h14c1.1,0 2,-0.9 2,-2V6.5c0,-0.49 -0.17,-0.93 -0.45,-1.28zM12,9.5l5.5,5.5H14v2h-4v-2H6.5L12,9.5zM5.12,5l0.82,-1h12l0.93,1H5.12z")
    }
    val epingle by lazy {
        icone("pin", "M16,9V4l1,0c0.55,0 1,-0.45 1,-1v0c0,-0.55 -0.45,-1 -1,-1H7C6.45,2 6,2.45 6,3v0c0,0.55 0.45,1 1,1l1,0v5c0,1.66 -1.34,3 -3,3v2h5.97v7l1,1l1,-1v-7H19v-2C17.34,12 16,10.66 16,9z")
    }
    val services by lazy {
        icone("apps", "M4,8h4V4H4V8zM10,20h4v-4h-4V20zM4,20h4v-4H4V20zM4,14h4v-4H4V14zM10,14h4v-4h-4V14zM16,4v4h4V4H16zM10,8h4V4h-4V8zM16,14h4v-4h-4V14zM16,20h4v-4h-4V20z")
    }
    val reorganiser by lazy {
        icone("swap_vert", "M16,17.01V10h-2v7.01h-3L15,21l4,-3.99h-3zM9,3L5,6.99h3V14h2V6.99h3L9,3z")
    }
    val grille by lazy {
        icone("grid_view", "M3,3v8h8V3H3zM9,9H5V5h4V9zM3,13v8h8v-8H3zM9,19H5v-4h4V19zM13,3v8h8V3H13zM19,9h-4V5h4V9zM13,13v8h8v-8H13zM19,19h-4v-4h4V19z")
    }
    val images by lazy {
        icone("photo_library", "M22,16V4c0,-1.1 -0.9,-2 -2,-2H8c-1.1,0 -2,0.9 -2,2v12c0,1.1 0.9,2 2,2h12c1.1,0 2,-0.9 2,-2zM11,12l2.03,2.71L16,11l4,5H8l3,-4zM2,6v14c0,1.1 0.9,2 2,2h14v-2H4V6H2z")
    }
    val photo by lazy {
        icone("photo_camera", "M12,15.2m-3.2,0a3.2,3.2 0,1 1,6.4 0a3.2,3.2 0,1 1,-6.4 0M9,2L7.17,4H4c-1.1,0 -2,0.9 -2,2v12c0,1.1 0.9,2 2,2h16c1.1,0 2,-0.9 2,-2V6c0,-1.1 -0.9,-2 -2,-2h-3.17L15,2H9zM12,17c-2.76,0 -5,-2.24 -5,-5s2.24,-5 5,-5 5,2.24 5,5 -2.24,5 -5,5z")
    }
    val horloge by lazy {
        icone("schedule", "M11.99,2C6.47,2 2,6.48 2,12s4.47,10 9.99,10C17.52,22 22,17.52 22,12S17.52,2 11.99,2zM12,20c-4.42,0 -8,-3.58 -8,-8s3.58,-8 8,-8 8,3.58 8,8 -3.58,8 -8,8zM12.5,7H11v6l5.25,3.15 0.75,-1.23 -4.5,-2.67z")
    }
    val doubleCoche by lazy {
        icone("done_all", "M18,7l-1.41,-1.41 -6.34,6.34 1.41,1.41L18,7zM22.24,5.59L11.66,16.17 7.48,12l-1.41,1.41L11.66,19l12,-12 -1.42,-1.41zM0.41,13.41L6,19l1.41,-1.41L1.83,12 0.41,13.41z")
    }
    val copier by lazy {
        icone("content_copy", "M16,1H4c-1.1,0 -2,0.9 -2,2v14h2V3h12V1zM19,5H8c-1.1,0 -2,0.9 -2,2v14c0,1.1 0.9,2 2,2h11c1.1,0 2,-0.9 2,-2V7c0,-1.1 -0.9,-2 -2,-2zM19,21H8V7h11v14z")
    }
    val video by lazy {
        icone("videocam", "M17,10.5V7c0,-0.55 -0.45,-1 -1,-1H4c-0.55,0 -1,0.45 -1,1v10c0,0.55 0.45,1 1,1h12c0.55,0 1,-0.45 1,-1v-3.5l4,4v-11l-4,4z")
    }
    val fichier by lazy {
        icone("attach_file", "M16.5,6v11.5c0,2.21 -1.79,4 -4,4s-4,-1.79 -4,-4V5c0,-1.38 1.12,-2.5 2.5,-2.5s2.5,1.12 2.5,2.5v10.5c0,0.55 -0.45,1 -1,1s-1,-0.45 -1,-1V6H10v9.5c0,1.38 1.12,2.5 2.5,2.5s2.5,-1.12 2.5,-2.5V5c0,-2.21 -1.79,-4 -4,-4S7,2.79 7,5v12.5c0,3.04 2.46,5.5 5.5,5.5s5.5,-2.46 5.5,-5.5V6h-1.5z")
    }
    val volets by lazy {
        icone("vertical_split", "M3,15h8v-2H3v2zM3,19h8v-2H3v2zM3,11h8V9H3v2zM3,5v2h8V5H3zM13,5h8v14h-8V5z")
    }
    val pleinEcran by lazy {
        icone("view_agenda", "M3,13h18v8H3V13zM3,3h18v8H3V3z")
    }

    val camera: ImageVector by lazy {
        ImageVector.Builder("camera", 24.dp, 24.dp, 24f, 24f).addPath(
            addPathNodes(
                "M17,10.5V7c0,-0.55 -0.45,-1 -1,-1H4c-0.55,0 -1,0.45 -1,1v10c0,0.55 0.45,1 1,1h12c0.55,0 1,-0.45 1,-1v-3.5l4,4v-11l-4,4z",
            ),
            fill = SolidColor(Color.Black),
        ).build()
    }
}
