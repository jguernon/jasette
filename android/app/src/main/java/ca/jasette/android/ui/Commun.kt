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
import androidx.compose.ui.draw.alpha
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

/**
 * L'icône du service : les mêmes PNG que le bureau (assets/services) pour
 * Google, Messenger et Instagram ; des dessins vectoriels à nous pour
 * WhatsApp, Signal et X (res/drawable/service_*.xml).
 */
fun iconeService(s: Service) = when (s) {
    Service.GOOGLE -> R.drawable.service_google
    Service.MESSENGER -> R.drawable.service_messenger
    Service.INSTAGRAM -> R.drawable.service_instagram
    Service.WHATSAPP -> R.drawable.service_whatsapp
    Service.SIGNAL -> R.drawable.service_signal
    Service.X -> R.drawable.service_x
    Service.TELEGRAM -> R.drawable.service_telegram
    Service.BLUESKY -> R.drawable.service_bluesky
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

/**
 * L'avatar d'une conversation, avec le petit badge de son service. La photo
 * d'un contact du téléphone passe avant celle du service quand le réglage est
 * actif (voir [ca.jasette.android.Contacts.photoPour]) ; la grande photo pour les gros avatars.
 * Sans photo du service, celle gardée par [ca.jasette.android.CacheMarques] (conversation marquée).
 * [grise] : la conversation n'est pas chargée (service absent) ; le logo du service reste en couleur.
 */
@Composable
fun Avatar(conv: Conv, taille: Dp, badge: Boolean = true, grise: Boolean = false, presence: Boolean = false, fondPoint: Color = P.fond) {
    val photo = ca.jasette.android.Contacts.photoPour(conv, grande = taille >= 48.dp) ?: Depot.avatars[conv.id]
        ?: ca.jasette.android.CacheMarques.avatar(conv.id)
    Box(Modifier.size(taille)) {
        // Rond, carré (EGA), cadre vert brillant (Live) ou carré en creux (Sirène).
        val rond = Modifier.cadreAvatar(taille)
        if (photo != null) {
            ImageFichier(
                photo, rond.background(P.surface).then(if (grise) Modifier.alpha(0.7f) else Modifier), cote = 256,
                filtre = if (grise) filtreGris else null,
                pixels = if (P.retro) PIXELS_RETRO else 0,
            )
        } else {
            val base = if (grise) GRIS_ABSENT else couleurHex(conv.color) ?: P.avatarDefaut
            Box(rond.background(fondAvatar(base)), contentAlignment = Alignment.Center) {
                val i = if (conv.group) null else initiales(conv.name)
                if (i != null) {
                    Text(i, color = Color.White, fontSize = (taille.value * 0.38f).sp, fontWeight = FontWeight.SemiBold)
                } else {
                    Icon(Icons.Filled.Person, null, tint = Color.White, modifier = Modifier.size(taille * 0.5f))
                }
            }
        }
        // Le point vert « en ligne » (PROTOCOLE.md §5.10), à l'opposé du badge du service.
        if (presence && Depot.presenceDe(conv)?.online == true) {
            PointPresence(taille, fondPoint, Modifier.align(Alignment.BottomStart))
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

/**
 * Le point vert d'un contact en ligne, cerclé de la couleur du fond [fond].
 * Carré dans les thèmes aux avatars pixelisés ou carrés (EGA, Live, Sirène).
 */
@Composable
fun PointPresence(taille: Dp, fond: Color, modifier: Modifier = Modifier) {
    val d = (taille * 0.26f).coerceIn(10.dp, 16.dp)
    val carre = P.ega || P.retro
    val forme = if (carre) androidx.compose.ui.graphics.RectangleShape else CircleShape
    val vert = when {
        P.ega -> Color(0xff55ff55)
        P.retro -> Color(0xff2fb52a)
        else -> Color(0xff31c45a)
    }
    Box(modifier.size(d).clip(forme).background(fond).padding(2.dp).clip(forme).background(vert))
}

/** Le fond d'un avatar sans photo d'une conversation absente (un gris neutre ; EGA : gris foncé). */
private val GRIS_ABSENT = Color(0xff6e6e73)

private val heureFmt = DateTimeFormatter.ofPattern("HH:mm")
private val jourFmtFr = DateTimeFormatter.ofPattern("d MMM", Locale.CANADA_FRENCH)
private val jourLongFmtFr = DateTimeFormatter.ofPattern("EEEE d MMMM", Locale.CANADA_FRENCH)
private val jourFmtEn = DateTimeFormatter.ofPattern("MMM d", Locale.ENGLISH)
private val jourLongFmtEn = DateTimeFormatter.ofPattern("EEEE, MMMM d", Locale.ENGLISH)
// Une autre année que l'année en cours : la date porte aussi l'année.
private val jourAnFmtFr = DateTimeFormatter.ofPattern("d MMM yyyy", Locale.CANADA_FRENCH)
private val jourLongAnFmtFr = DateTimeFormatter.ofPattern("EEEE d MMMM yyyy", Locale.CANADA_FRENCH)
private val jourAnFmtEn = DateTimeFormatter.ofPattern("MMM d, yyyy", Locale.ENGLISH)
private val jourLongAnFmtEn = DateTimeFormatter.ofPattern("EEEE, MMMM d, yyyy", Locale.ENGLISH)
private fun jourFmt(d: LocalDate) = when {
    d.year != LocalDate.now().year -> if (ca.jasette.android.Langue.anglais) jourAnFmtEn else jourAnFmtFr
    else -> if (ca.jasette.android.Langue.anglais) jourFmtEn else jourFmtFr
}
private fun jourLongFmt(d: LocalDate) = when {
    d.year != LocalDate.now().year -> if (ca.jasette.android.Langue.anglais) jourLongAnFmtEn else jourLongAnFmtFr
    else -> if (ca.jasette.android.Langue.anglais) jourLongFmtEn else jourLongFmtFr
}

/** L'heure dans la liste : l'heure aujourd'hui, sinon la date. */
fun heureListe(ms: Long): String {
    if (ms <= 0) return ""
    val t = Instant.ofEpochMilli(ms).atZone(ZoneId.systemDefault())
    return if (t.toLocalDate() == LocalDate.now()) t.format(heureFmt) else t.format(jourFmt(t.toLocalDate()))
}

fun heure(ms: Long): String = Instant.ofEpochMilli(ms).atZone(ZoneId.systemDefault()).format(heureFmt)

fun jour(ms: Long): String {
    val d = Instant.ofEpochMilli(ms).atZone(ZoneId.systemDefault()).toLocalDate()
    val auj = LocalDate.now()
    return when (d) {
        auj -> t("Aujourd'hui")
        auj.minusDays(1) -> t("Hier")
        else -> d.format(jourLongFmt(d)).replaceFirstChar { it.uppercase() }
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
    // Les tuiles des sections des Réglages (comme les icônes Lucide du bureau).
    val arobase by lazy {
        icone("alternate_email", "M12,1.95c-5.52,0 -10,4.48 -10,10s4.48,10 10,10h5v-2h-5c-4.34,0 -8,-3.66 -8,-8s3.66,-8 8,-8 8,3.66 8,8v1.43c0,0.79 -0.71,1.57 -1.5,1.57s-1.5,-0.78 -1.5,-1.57v-1.43c0,-2.76 -2.24,-5 -5,-5s-5,2.24 -5,5 2.24,5 5,5c1.38,0 2.64,-0.56 3.54,-1.47 0.65,0.89 1.77,1.47 2.96,1.47 1.97,0 3.5,-1.6 3.5,-3.57v-1.43c0,-5.52 -4.48,-10 -10,-10zM12,14.95c-1.66,0 -3,-1.34 -3,-3s1.34,-3 3,-3 3,1.34 3,3 -1.34,3 -3,3z")
    }
    val forum by lazy {
        icone("forum", "M21,6h-2v9H6v2c0,0.55 0.45,1 1,1h11l4,4V7c0,-0.55 -0.45,-1 -1,-1zM17,12V3c0,-0.55 -0.45,-1 -1,-1H3c-0.55,0 -1,0.45 -1,1v14l4,-4h10c0.55,0 1,-0.45 1,-1z")
    }
    val serveur by lazy {
        icone("dns", "M20,13H4c-0.55,0 -1,0.45 -1,1v6c0,0.55 0.45,1 1,1h16c0.55,0 1,-0.45 1,-1v-6c0,-0.55 -0.45,-1 -1,-1zM7,19c-1.1,0 -2,-0.9 -2,-2s0.9,-2 2,-2 2,0.9 2,2 -0.9,2 -2,2zM20,3H4c-0.55,0 -1,0.45 -1,1v6c0,0.55 0.45,1 1,1h16c0.55,0 1,-0.45 1,-1V4c0,-0.55 -0.45,-1 -1,-1zM7,9c-1.1,0 -2,-0.9 -2,-2s0.9,-2 2,-2 2,0.9 2,2 -0.9,2 -2,2z")
    }
    val langue by lazy {
        icone("translate", "M12.87,15.07l-2.54,-2.51 0.03,-0.03c1.74,-1.94 2.98,-4.17 3.71,-6.53L17,6L17,4h-7L10,2L8,2v2L1,4v1.99h11.17C11.5,7.92 10.44,9.75 9,11.35 8.07,10.32 7.3,9.19 6.69,8h-2c0.73,1.63 1.73,3.17 2.98,4.56l-5.09,5.02L4,19l5,-5 3.11,3.11 0.76,-2.04zM18.5,10h-2L12,22h2l1.12,-3h4.75L21,22h2l-4.5,-12zM15.88,17l1.62,-4.33L19.12,17h-3.24z")
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
    val palette by lazy {
        icone("palette", "M12,2C6.49,2 2,6.49 2,12s4.49,10 10,10c1.38,0 2.5,-1.12 2.5,-2.5c0,-0.61 -0.23,-1.2 -0.64,-1.67c-0.08,-0.1 -0.13,-0.21 -0.13,-0.33c0,-0.28 0.22,-0.5 0.5,-0.5H16c3.31,0 6,-2.69 6,-6C22,6.04 17.51,2 12,2zM17.5,13c-0.83,0 -1.5,-0.67 -1.5,-1.5c0,-0.83 0.67,-1.5 1.5,-1.5s1.5,0.67 1.5,1.5C19,12.33 18.33,13 17.5,13zM14.5,9C13.67,9 13,8.33 13,7.5C13,6.67 13.67,6 14.5,6S16,6.67 16,7.5C16,8.33 15.33,9 14.5,9zM5,11.5C5,10.67 5.67,10 6.5,10S8,10.67 8,11.5C8,12.33 7.33,13 6.5,13S5,12.33 5,11.5zM11,7.5C11,8.33 10.33,9 9.5,9S8,8.33 8,7.5C8,6.67 8.67,6 9.5,6S11,6.67 11,7.5z")
    }
    val sourire by lazy {
        icone("mood", "M11.99,2C6.47,2 2,6.48 2,12s4.47,10 9.99,10C17.52,22 22,17.52 22,12S17.52,2 11.99,2zM12,20c-4.42,0 -8,-3.58 -8,-8s3.58,-8 8,-8 8,3.58 8,8 -3.58,8 -8,8zM15.5,11c0.83,0 1.5,-0.67 1.5,-1.5S16.33,8 15.5,8 14,8.67 14,9.5s0.67,1.5 1.5,1.5zM8.5,11c0.83,0 1.5,-0.67 1.5,-1.5S9.33,8 8.5,8 7,8.67 7,9.5 7.67,11 8.5,11zM12,17.5c2.33,0 4.31,-1.46 5.11,-3.5L6.89,14c0.8,2.04 2.78,3.5 5.11,3.5z")
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
