package ca.jasette.android.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

/**
 * La palette du thème « Sombre » de Jasette (`Palette::sombre` dans src/main.rs) :
 * plat, à la Beeper, aplats gris et fil noir.
 */
object P {
    val fond = Color(0xFF1C1C1E)
    val liste = Color(0xFF252527)
    val fil = Color(0xFF000000)
    /** Bulle reçue, pièce jointe, champ de la barre d'envoi. */
    val surface = Color(0xFF2C2C2E)
    val champ = Color(0xFF333336)
    val choisi = Color(0xFF3A3A3C)
    val survol = Color(0xFF2E2E30)
    val traitFin = Color(0xFF333335)
    val menu = Color(0xFF2C2C2E)
    val carte = Color(0xFF1C1C1E)
    val texteFort = Color(0xFFFFFFFF)
    val texte = Color(0xFFC7C7CC)
    val textePale = Color(0xFF8E8E93)
    val apercu = Color(0xFFA0A0A5)
    val icone = Color(0xFFA0A0A5)
    val accent = Color(0xFF2F6BFF)
    /** Le nom de l'expéditeur dans un groupe. */
    val expediteur = Color(0xFFF08A6C)
    val jourFond = Color(0xFF2C2C2E)
    val heureRecue = Color(0xFF8E8E93)
    val heureEnvoyee = Color(0xB3FFFFFF)
    val danger = Color(0xFFFF453A)
    val succes = Color(0xFF30D158)
    val averti = Color(0xFFD97706)
    /** L'accent par défaut des avatars sans couleur (ACCENT du bureau). */
    val avatarDefaut = Color(0xFF3B82F6)
}

@Composable
fun JasetteTheme(contenu: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = P.accent,
            onPrimary = Color.White,
            secondary = P.expediteur,
            background = P.fond,
            onBackground = P.texteFort,
            surface = P.fond,
            onSurface = P.texteFort,
            surfaceVariant = P.surface,
            onSurfaceVariant = P.texte,
            surfaceContainer = P.menu,
            surfaceContainerHigh = P.menu,
            surfaceContainerHighest = P.champ,
            outline = P.traitFin,
            outlineVariant = P.traitFin,
            error = P.danger,
        ),
        content = contenu,
    )
}
