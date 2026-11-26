package ca.jasette.android.ui

import androidx.compose.foundation.layout.heightIn
import android.content.Context
import android.content.SharedPreferences
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.enableEdgeToEdge
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.TextFieldColors
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import ca.jasette.android.R
import kotlin.math.sqrt

// ---- Les thèmes : Sombre, Verre et EGA (ceux du bureau, `enum Theme` de src/main.rs), Live et Sirène ----

/** Les thèmes, gardés dans le réglage « theme » par leur id. */
enum class Theme(val id: String) {
    SOMBRE("sombre"),
    VERRE("verre"),
    /** L'écran 16 couleurs d'un PC des années 80 : palette EGA, police IBM VGA 8x16, coins carrés. */
    EGA("ega"),
    /** La messagerie bleue des années 2000 : ciel en dégradé, en-tête bleu brillant, cadres verts (Retro.kt). */
    LIVE("live"),
    /** La messagerie verte des années 2000, look Windows 2000 : gris, reliefs, barre de titre verte (Retro.kt). */
    SIRENE("sirene");

    companion object {
        /** Le thème d'un id des réglages ; sombre par défaut. */
        fun depuis(id: String?) = entries.firstOrNull { it.id == id } ?: SOMBRE
    }
}

/** Une couleur 0xRRGGBB opaque. */
fun couleur(hex: Long) = Color(0xFF000000 or hex)
private fun c(hex: Long) = couleur(hex)
/** Une couleur 0xRRGGBB à l'opacité donnée. */
private fun ca(hex: Long, a: Float) = c(hex).copy(alpha = a)
/** Blanc à l'opacité donnée. */
private fun blanc(a: Float) = Color.White.copy(alpha = a)
private val TRANSPARENT = Color.Transparent

// Les couleurs du verre (constantes du haut de src/main.rs).
private const val ACCENT = 0x3b82f6L
private const val ACCENT_2 = 0x8b5cf6L
private const val PECHE = 0xffa878L
private const val FOND_VERRE = 0xebe9f5L // --ss-well
private const val TEXTE_FORT = 0x262628L
private const val TEXTE = 0x6a6a72L
private const val TEXTE_PALE = 0x9a9a9dL
private const val SUCCES = 0x16a34aL
private const val DANGER = 0xdc2626L

// La palette EGA exacte (adaptateur IBM EGA, comme ega-theme.css).
const val EGA_NOIR = 0x000000L
const val EGA_BLEU = 0x0000aaL
const val EGA_GRIS_CLAIR = 0xaaaaaaL
const val EGA_GRIS = 0x555555L
const val EGA_CYAN_VIF = 0x55ffffL
const val EGA_VERT_VIF = 0x55ff55L
const val EGA_ROUGE_VIF = 0xff5555L
const val EGA_MAGENTA_VIF = 0xff55ffL
const val EGA_JAUNE = 0xffff55L
const val EGA_BLANC = 0xffffffL

/** Le gris des fenêtres Windows 2000 (Sirène). */
const val GRIS_2000 = 0xd4d0c8L

/**
 * Une teinte pastel du thème verre (`TEINTES` du bureau) : son id gardé dans
 * les réglages, son nom affiché, l'angle de son fond, puis les trois couleurs
 * des halos (la première est l'accent, la deuxième finit le dégradé des bulles envoyées).
 */
class Teinte(val id: String, val nom: String, val angle: Float, val accent: Long, val accent2: Long, val halo: Long)

val TEINTES = listOf(
    Teinte("lavande", "Lavande", 250f, ACCENT, ACCENT_2, PECHE),
    Teinte("rose", "Rose", 335f, 0xdb2f74L, 0xb03fd0L, 0xffb38aL),
    Teinte("peche", "Pêche", 20f, 0xe0552bL, 0xd93a6aL, 0xffd166L),
    Teinte("citron", "Citron", 52f, 0xb07a00L, 0xd08a00L, 0x9be27aL),
    Teinte("menthe", "Menthe", 155f, 0x0b8f6aL, 0x14a3a0L, 0xb8e86aL),
    Teinte("ciel", "Ciel", 200f, 0x0a7fc4L, 0x1aa6c9L, 0x7be0c4L),
    Teinte("lilas", "Lilas", 285f, 0x9333eaL, 0xc026d3L, 0xf9a8d4L),
    Teinte("corail", "Corail", 5f, 0xd93b3bL, 0xe0613aL, 0xffc078L),
)

/** Une couleur HSL comme `hsla()` de gpui (teinte en degrés). */
fun hsl(angle: Float, s: Float, l: Float) = Color.hsl(((angle % 360f) + 360f) % 360f, s, l)

/** Le fond du verre d'une teinte (la lavande garde son fond d'origine, --ss-well). */
private fun fondVerre(t: Teinte) = if (t.id == "lavande") c(FOND_VERRE) else hsl(t.angle, 0.4f, 0.935f)

/**
 * Les couleurs d'un thème (`struct Palette` du bureau), plus ce qu'Android
 * ajoute : le fond des écrans et de leur barre du haut, les remplissages
 * d'accent (bulles envoyées, boutons), le texte posé dessus, etc.
 */
class Palette(
    val theme: Theme,
    /** Le fond de la fenêtre (sous les halos du verre). */
    val fond: Color,
    /** Le fond d'un écran posé sur la fenêtre (la liste, les réglages). */
    val ecran: Color,
    /** La barre du haut des écrans. */
    val barre: Color,
    /** Le rail et la barre du bas. */
    val liste: Color,
    val fil: Color,
    /** Bulle reçue, pièce jointe, champ de la barre d'envoi, groupes des réglages. */
    val surface: Color,
    /** Le liseré d'une bulle reçue (transparent en sombre). */
    val bordBulle: Color,
    /** Le liseré des cartes et des groupes (transparent en sombre). */
    val bordCarte: Color,
    /** Le trait entre deux rangées d'un groupe. */
    val filet: Color,
    val champ: Color,
    val choisi: Color,
    val survol: Color,
    val traitFin: Color,
    /** Menus, feuilles du bas et dialogues : opaques. */
    val menu: Color,
    val carte: Color,
    val texteFort: Color,
    val texte: Color,
    val textePale: Color,
    val apercu: Color,
    val icone: Color,
    /** L'accent des icônes, coches et liens. */
    val accent: Color,
    /** L'accent en aplat sous du texte blanc (boutons, pastilles, bouton flottant). */
    val accentPlein: Color,
    /** Le fond d'une bulle envoyée ou d'un bouton plein (le dégradé du verre). */
    val degrade: Brush,
    /** Le texte posé sur l'accent plein. */
    val surAccent: Color,
    /** Les titres des barres (jaunes en EGA). */
    val titre: Color,
    val expediteur: Color,
    val jourFond: Color,
    val jourTexte: Color,
    val heureRecue: Color,
    val heureEnvoyee: Color,
    /** Les coches « lu » et « échec » sur ma bulle. */
    val luSurBulle: Color,
    val echecSurBulle: Color,
    /** L'interrupteur éteint : sa piste. */
    val interrupteurEteint: Color,
    /** Le surlignage des messages trouvés. */
    val trouve: Color,
    val danger: Color,
    val succes: Color,
    val averti: Color,
    val avatarDefaut: Color,
    /** Les halos de couleur du verre, comme `halo()` du bureau. */
    val halos: List<Halo> = emptyList(),
    /** Derrière la barre d'état, au-dessus des barres de titre de Live et Sirène. */
    val bandeau: Color = barre,
    /** Le titre, le sous-titre et les icônes posés sur la barre du haut. */
    val texteBarre: Color = titre,
    val sousTitreBarre: Color = textePale,
    val iconeBarre: Color = icone,
    /** Une icône « active » sur la barre du haut (loupe ouverte, Terminé). */
    val accentBarre: Color = accent,
    /** La rangée choisie de la liste (deux volets), le Head choisi, l'indicateur du rail ; et son texte. */
    val selection: Color = choisi,
    val surSelection: Color = texteFort,
) {
    val ega get() = theme == Theme.EGA
    val live get() = theme == Theme.LIVE
    val sirene get() = theme == Theme.SIRENE
    /** Thème clair (Verre, Live, Sirène) : icônes sombres dans les barres système. */
    val clair get() = theme == Theme.VERRE || live || sirene
    /** Live ou Sirène : barres dessinées, fil sans bulles (« X dit (19:24) : »). */
    val retro get() = live || sirene
    /** Coins carrés : EGA et Sirène. */
    val carre get() = ega || sirene
    /** Pas d'animation fluide, tout change d'un coup : EGA et Sirène. */
    val sec get() = ega || sirene
    /** Ni flou ni verre liquide : barres opaques (EGA, Live, Sirène). */
    val plein get() = ega || live || sirene
}

/** Un halo du verre, aux mesures du bureau (couleur, opacité, x, y, taille, dans une fenêtre de 1280 × 820). */
class Halo(val couleur: Color, val opacite: Float, val x: Float, val y: Float, val taille: Float)

private fun sombre() = Palette(
    theme = Theme.SOMBRE,
    fond = c(0x1c1c1e), ecran = c(0x1c1c1e), barre = c(0x1c1c1e), liste = c(0x252527), fil = c(0x000000),
    surface = c(0x2c2c2e), bordBulle = TRANSPARENT, bordCarte = TRANSPARENT, filet = c(0x1c1c1e),
    champ = c(0x333336), choisi = c(0x3a3a3c), survol = c(0x2e2e30), traitFin = c(0x333335),
    menu = c(0x2c2c2e), carte = c(0x1c1c1e),
    texteFort = c(0xffffff), texte = c(0xc7c7cc), textePale = c(0x8e8e93), apercu = c(0xa0a0a5), icone = c(0xa0a0a5),
    accent = c(0x2f6bff), accentPlein = c(0x2f6bff), degrade = SolidColor(c(0x2f6bff)),
    surAccent = Color.White, titre = c(0xffffff),
    expediteur = c(0xf08a6c), jourFond = c(0x2c2c2e), jourTexte = c(0xffffff),
    heureRecue = c(0x8e8e93), heureEnvoyee = blanc(0.7f),
    luSurBulle = c(0xa8dcff), echecSurBulle = c(0xffb4ae),
    interrupteurEteint = c(0x48484a), trouve = c(0xffd60a),
    danger = c(0xff453a), succes = c(0x30d158), averti = c(0xd97706), avatarDefaut = c(ACCENT),
)

private fun verre(t: Teinte) = Palette(
    theme = Theme.VERRE,
    fond = fondVerre(t),
    ecran = blanc(0.5f), barre = TRANSPARENT, liste = blanc(0.5f), fil = blanc(0.42f),
    surface = blanc(0.85f), bordBulle = blanc(0.95f), bordCarte = blanc(0.75f), filet = fondVerre(t),
    champ = blanc(0.7f), choisi = blanc(0.82f), survol = blanc(0.45f), traitFin = blanc(0.55f),
    // Le menu du bureau (blanc à 94 %) posé sur le fond : opaque, pour les feuilles et les dialogues.
    menu = lerp(fondVerre(t), Color.White, 0.94f),
    carte = blanc(0.72f),
    texteFort = c(TEXTE_FORT), texte = c(TEXTE), textePale = c(TEXTE_PALE), apercu = c(TEXTE), icone = c(TEXTE),
    accent = c(t.accent), accentPlein = c(t.accent),
    degrade = Brush.linearGradient(listOf(c(t.accent), c(t.accent2)), Offset.Zero, Offset.Infinite),
    surAccent = Color.White, titre = c(TEXTE_FORT),
    expediteur = c(t.accent2), jourFond = blanc(0.5f), jourTexte = c(TEXTE),
    heureRecue = c(TEXTE_PALE), heureEnvoyee = blanc(0.8f),
    luSurBulle = c(0x5eead4), echecSurBulle = c(0xfca5a5),
    interrupteurEteint = ca(TEXTE_PALE, 0.35f), trouve = c(0xfbbf24),
    danger = c(DANGER), succes = c(SUCCES), averti = c(0xd97706), avatarDefaut = c(ACCENT),
    halos = listOf(
        Halo(c(t.accent), 0.42f, -180f, -220f, 760f),
        Halo(c(t.accent2), 0.36f, 760f, -160f, 720f),
        Halo(c(t.halo), 0.40f, 300f, 520f, 760f),
    ),
)

/**
 * L'EGA de Claude Desktop (ega-theme.css) : tout sur fond noir, texte gris
 * clair, titres et noms en jaune, sélection blanc sur bleu, traits gris
 * (#555555) ou gris clair (#AAAAAA), accent cyan vif, remplissages bleus.
 */
private fun egaPalette() = Palette(
    theme = Theme.EGA,
    fond = c(EGA_NOIR), ecran = c(EGA_NOIR), barre = c(EGA_NOIR), liste = c(EGA_NOIR), fil = c(EGA_NOIR),
    surface = c(EGA_NOIR), bordBulle = c(EGA_GRIS_CLAIR), bordCarte = c(EGA_GRIS), filet = c(EGA_GRIS),
    champ = c(EGA_NOIR), choisi = c(EGA_BLEU), survol = c(EGA_GRIS), traitFin = c(EGA_GRIS),
    menu = c(EGA_NOIR), carte = c(EGA_NOIR),
    texteFort = c(EGA_BLANC), texte = c(EGA_GRIS_CLAIR), textePale = c(EGA_GRIS_CLAIR),
    apercu = c(EGA_GRIS_CLAIR), icone = c(EGA_GRIS_CLAIR),
    accent = c(EGA_CYAN_VIF), accentPlein = c(EGA_BLEU), degrade = SolidColor(c(EGA_BLEU)),
    surAccent = c(EGA_BLANC), titre = c(EGA_JAUNE),
    expediteur = c(EGA_JAUNE), jourFond = c(EGA_NOIR), jourTexte = c(EGA_JAUNE),
    heureRecue = c(EGA_GRIS), heureEnvoyee = c(EGA_GRIS_CLAIR),
    luSurBulle = c(EGA_CYAN_VIF), echecSurBulle = c(EGA_ROUGE_VIF),
    // Le gris foncé du sombre (#48484a), ramené au gris EGA (`ega_snap`).
    interrupteurEteint = c(EGA_GRIS), trouve = c(EGA_JAUNE),
    danger = c(EGA_ROUGE_VIF), succes = c(EGA_VERT_VIF), averti = c(0xaa5500), avatarDefaut = c(EGA_BLEU),
)

/**
 * Live : la messagerie bleue des années 2000 (maquette Main.dc.html). Un ciel en
 * dégradé derrière, des écrans bleu très pâle, du blanc pour les champs et les
 * cartes, des liserés bleus (#94bbe8), texte #1d2f4a, noms #12335e. La barre
 * du haut est bleue et brillante, le bouton Envoyer aussi (Retro.kt).
 */
private fun livePalette() = Palette(
    theme = Theme.LIVE,
    fond = c(0xd7e9fb), ecran = ca(0xeaf3fd, 0.86f), barre = c(0xcfe3fa), liste = c(0xd6e8fb), fil = c(0xf7fbff),
    surface = c(0xffffff), bordBulle = c(0x94bbe8), bordCarte = c(0x94bbe8), filet = c(0xc3dbf5),
    champ = c(0xf6faff), choisi = c(0xd4e6fa), survol = c(0xe3f0fd), traitFin = c(0xb9d3f1),
    menu = c(0xf7fbff), carte = c(0xffffff),
    texteFort = c(0x12335e), texte = c(0x1d2f4a), textePale = c(0x4d6584), apercu = c(0x3f5d85), icone = c(0x1f5fb8),
    accent = c(0x0b5bc4), accentPlein = c(0x2f7ee0), degrade = BRILLANT_LIVE,
    surAccent = Color.White, titre = c(0x12335e),
    expediteur = c(0x0b5bc4), jourFond = c(0xe3f0fd), jourTexte = c(0x3f5d85),
    heureRecue = c(0x5d7088), heureEnvoyee = blanc(0.85f),
    luSurBulle = c(0xbff0a8), echecSurBulle = c(0xffc2b8),
    interrupteurEteint = c(0xa9c0dc), trouve = c(0xf2c230),
    danger = c(0xc0281a), succes = c(0x2f8a22), averti = c(0xc07a12), avatarDefaut = c(0x2f7ee0),
    bandeau = c(0xdcebfc), texteBarre = Color.White, sousTitreBarre = blanc(0.88f), iconeBarre = Color.White,
    accentBarre = c(0xfff3a0), selection = c(0xbcd9fb),
)

/**
 * Sirène : la messagerie verte des années 2000, look Windows 2000 (maquette
 * Sirene.dc.html). Gris #d4d0c8, reliefs clairs en haut à gauche et sombres en
 * bas à droite, champs et listes en creux sur blanc, barre de titre verte,
 * sélection bleu marine #0a246a et texte blanc, bouton Envoyer vert (Retro.kt).
 */
private fun sirenePalette() = Palette(
    theme = Theme.SIRENE,
    fond = c(GRIS_2000), ecran = c(GRIS_2000), barre = c(GRIS_2000), liste = c(GRIS_2000), fil = c(0xffffff),
    surface = c(0xffffff), bordBulle = c(0x808080), bordCarte = c(0x808080), filet = c(0xc0c0c0),
    champ = c(0xffffff), choisi = c(0xc9d3ea), survol = c(0xe4f3e0), traitFin = c(0x808080),
    menu = c(GRIS_2000), carte = c(0xffffff),
    texteFort = c(0x000000), texte = c(0x202020), textePale = c(0x505050), apercu = c(0x404040), icone = c(0x202020),
    accent = c(0x1f6f2a), accentPlein = c(0x2e9a3f), degrade = SolidColor(c(0x2e9a3f)),
    surAccent = Color.White, titre = c(0x000000),
    expediteur = c(0xc01818), jourFond = c(0xffffe1), jourTexte = c(0x000000),
    heureRecue = c(0x6a6a6a), heureEnvoyee = blanc(0.85f),
    luSurBulle = c(0xb8f5a8), echecSurBulle = c(0xffb4ae),
    interrupteurEteint = c(0xa0a0a0), trouve = c(0xffe26a),
    danger = c(0xc01818), succes = c(0x1f8a2f), averti = c(0x8a6200), avatarDefaut = c(0x2e9a3f),
    bandeau = c(GRIS_2000), texteBarre = Color.White, sousTitreBarre = blanc(0.88f), iconeBarre = Color.White,
    accentBarre = c(0xffff99), selection = c(0x0a246a), surSelection = Color.White,
)

/** Le thème et la teinte choisis : un état Compose, gardé dans les SharedPreferences. */
object Apparence {
    private const val PREFS = "reglages"
    private var prefs: SharedPreferences? = null

    var theme by mutableStateOf(Theme.SOMBRE)
        private set
    var teinte by mutableStateOf(TEINTES[0])
        private set

    private val SOMBRE = sombre()
    private val EGA = egaPalette()
    private val LIVE = livePalette()
    private val SIRENE = sirenePalette()
    private val VERRES = TEINTES.associate { it.id to verre(it) }

    /** La palette du thème en cours (lire [theme] et [teinte] abonne l'appelant). */
    val palette: Palette get() = when (theme) {
        Theme.SOMBRE -> SOMBRE
        Theme.EGA -> EGA
        Theme.LIVE -> LIVE
        Theme.SIRENE -> SIRENE
        Theme.VERRE -> VERRES.getValue(teinte.id)
    }

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        prefs = p
        theme = Theme.depuis(p.getString("theme", null))
        teinte = TEINTES.firstOrNull { it.id == p.getString("teinte", null) } ?: TEINTES[0]
    }

    fun choisirTheme(t: Theme) {
        theme = t
        prefs?.edit()?.putString("theme", t.id)?.apply()
    }

    fun choisirTeinte(t: Teinte) {
        teinte = t
        prefs?.edit()?.putString("teinte", t.id)?.apply()
    }
}

/**
 * La palette du thème courant, sous les noms de toujours (`P.fond`, `P.accent`…) :
 * chaque lecture passe par l'état de [Apparence], donc changer de thème redessine tout.
 */
object P {
    private val p get() = Apparence.palette
    val ega get() = p.ega
    val live get() = p.live
    val sirene get() = p.sirene
    val clair get() = p.clair
    val retro get() = p.retro
    val carre get() = p.carre
    val sec get() = p.sec
    val plein get() = p.plein
    val fond get() = p.fond
    val ecran get() = p.ecran
    val barre get() = p.barre
    val liste get() = p.liste
    val fil get() = p.fil
    /** Bulle reçue, pièce jointe, champ de la barre d'envoi. */
    val surface get() = p.surface
    val bordBulle get() = p.bordBulle
    val bordCarte get() = p.bordCarte
    val filet get() = p.filet
    val champ get() = p.champ
    val choisi get() = p.choisi
    val survol get() = p.survol
    val traitFin get() = p.traitFin
    val menu get() = p.menu
    val carte get() = p.carte
    val texteFort get() = p.texteFort
    val texte get() = p.texte
    val textePale get() = p.textePale
    val apercu get() = p.apercu
    val icone get() = p.icone
    val accent get() = p.accent
    val accentPlein get() = p.accentPlein
    val degrade get() = p.degrade
    val surAccent get() = p.surAccent
    val titre get() = p.titre
    /** Le nom de l'expéditeur dans un groupe. */
    val expediteur get() = p.expediteur
    val jourFond get() = p.jourFond
    val jourTexte get() = p.jourTexte
    val heureRecue get() = p.heureRecue
    val heureEnvoyee get() = p.heureEnvoyee
    val luSurBulle get() = p.luSurBulle
    val echecSurBulle get() = p.echecSurBulle
    val interrupteurEteint get() = p.interrupteurEteint
    val trouve get() = p.trouve
    val danger get() = p.danger
    val succes get() = p.succes
    val averti get() = p.averti
    /** L'accent par défaut des avatars sans couleur (ACCENT du bureau). */
    val avatarDefaut get() = p.avatarDefaut
    val bandeau get() = p.bandeau
    val texteBarre get() = p.texteBarre
    val sousTitreBarre get() = p.sousTitreBarre
    val iconeBarre get() = p.iconeBarre
    val accentBarre get() = p.accentBarre
    val selection get() = p.selection
    val surSelection get() = p.surSelection
    /** La police du texte : celle du système, ou l'IBM VGA 8x16 en EGA. */
    val police: FontFamily get() = if (p.ega) POLICE_IBM else FontFamily.Default
}

/** La police IBM VGA 8x16 (PxPlus, de « The Ultimate Oldschool PC Font Pack », CC BY-SA 4.0). */
private val POLICE_IBM = FontFamily(Font(R.font.pxplus_ibm_vga_8x16))

// ---- Les formes : `rayon()` du bureau, des coins carrés en EGA ----

/** Le rayon d'un coin : celui demandé, ou un coin carré en EGA. */
fun rayon(r: Dp): Dp = if (P.carre) 0.dp else r

/** Un rectangle arrondi de rayon [r] (carré en EGA). */
fun forme(r: Dp): Shape = if (P.carre) RectangleShape else RoundedCornerShape(r)

/** Un rond (avatar, pastille) : carré en EGA. */
val formeRonde: Shape get() = if (P.carre) RectangleShape else CircleShape
/** Une pilule (jour, réactions, boutons) : carrée en EGA. */
val formePilule: Shape get() = if (P.carre) RectangleShape else RoundedCornerShape(50)

/** Les quatre coins d'une bulle (carrés en EGA et Sirène). */
fun formeBulle(hautGauche: Dp, hautDroit: Dp, basDroit: Dp, basGauche: Dp): Shape =
    if (P.carre) RectangleShape else RoundedCornerShape(hautGauche, hautDroit, basDroit, basGauche)

/**
 * Une carte (`verre()` du bureau) : un aplat, son liseré (blanc translucide au
 * verre, gris en EGA, aucun en sombre), coins de rayon [r] (carrés en EGA).
 */
fun Modifier.carte(r: Dp, fond: Color = P.surface, bord: Color = P.bordCarte): Modifier {
    // Sirène : un champ en creux (liseré sombre en haut à gauche, clair en bas à droite).
    if (P.sirene) return background(fond).relief(enfonce = true)
    val f = forme(r)
    return clip(f).background(fond).let { if (bord.alpha > 0f) it.border(1.dp, bord, f) else it }
}

/**
 * Le rayon commun des champs de saisie (barre d'écriture, recherche, nouveau
 * message, « Quoi de neuf ? », réglages…) : une pilule de [HAUT_CHAMP] ; sur
 * plusieurs lignes, le même rayon, comme la barre d'écriture qui grandit.
 * Carré en EGA et en Sirène (via [forme]).
 */
val HAUT_CHAMP = 44.dp
val RAYON_CHAMP = HAUT_CHAMP / 2

/** La forme des champs (aussi pour les OutlinedTextField). */
val formeChamp: Shape get() = forme(RAYON_CHAMP)

/** Le fond, le contour, la hauteur et le rayon communs des champs arrondis. */
fun Modifier.champRond(): Modifier = heightIn(min = HAUT_CHAMP).carte(RAYON_CHAMP)

/**
 * Un champ posé sur une surface surélevée (feuille, dialogue, carte) : la
 * surface y a déjà le ton de [champRond], qui s'y perdait. Sombre : un ton plus
 * clair et un liseré ; Verre : le fond teinté et un liseré. Live, Sirène et EGA
 * détachent déjà leurs champs (blanc bordé, creux, liseré gris).
 */
private val sombreSeul get() = !P.clair && !P.ega
private val verreSeul get() = P.clair && !P.live && !P.sirene
private val champAPart get() = sombreSeul || verreSeul
val fondChampSurface: Color get() = when {
    sombreSeul -> c(0x3a3a3c)
    verreSeul -> P.fond
    else -> P.surface
}
val bordChampSurface: Color get() = when {
    sombreSeul -> c(0x4a4a4e)
    verreSeul -> Color.Black.copy(alpha = 0.12f)
    else -> P.bordCarte
}
fun Modifier.champSurface(): Modifier = heightIn(min = HAUT_CHAMP).carte(RAYON_CHAMP, fondChampSurface, bordChampSurface)

/** Les couleurs communes des OutlinedTextField (jeton Discord, Bluesky, connexion…) : même ton que [champSurface]. */
@Composable
fun couleursChamp(): TextFieldColors = if (champAPart) {
    OutlinedTextFieldDefaults.colors(
        focusedBorderColor = P.accent, cursorColor = P.accent, unfocusedBorderColor = bordChampSurface,
        focusedContainerColor = fondChampSurface, unfocusedContainerColor = fondChampSurface,
    )
} else {
    OutlinedTextFieldDefaults.colors(focusedBorderColor = P.accent, cursorColor = P.accent)
}

/** Une barre flottante : ombre douce, ou fenêtre EGA cadrée à ombre portée dure. */
fun Modifier.flottant(r: Dp): Modifier {
    val f = forme(r)
    return if (P.ega) {
        drawBehind { drawRect(c(EGA_GRIS), topLeft = Offset(3.dp.toPx(), 3.dp.toPx()), size = size) }
            .background(c(EGA_NOIR)).border(1.dp, c(EGA_GRIS_CLAIR), f)
    } else if (P.sirene) {
        // Une fenêtre Windows 2000 : grise, en relief, ombre portée nette.
        drawBehind { drawRect(c(0x404040).copy(alpha = 0.45f), topLeft = Offset(2.dp.toPx(), 2.dp.toPx()), size = size) }
            .background(P.menu).relief(enfonce = false)
    } else {
        shadow(8.dp, f).clip(f).background(P.menu).let { if (P.bordCarte.alpha > 0f) it.border(1.dp, P.bordCarte, f) else it }
    }
}

/**
 * Le fond de la fenêtre : l'aplat du thème et, au verre, ses trois halos de
 * couleur flous (`halo()` du bureau : un disque dont seule l'ombre très floue
 * se voit). Leurs places suivent l'écran, d'après une fenêtre de bureau de 1280 × 820.
 */
fun Modifier.fondFenetre(): Modifier {
    val pal = Apparence.palette
    // Live : le ciel de la maquette (160°, du bleu franc en haut au bleu très pâle en bas).
    if (pal.live) return drawBehind {
        drawRect(
            Brush.linearGradient(
                0f to c(0x5f9fe8), 0.45f to c(0x9cc7f4), 1f to c(0xd7e9fb),
                start = Offset.Zero, end = Offset(size.width * 0.36f, size.height),
            ),
        )
    }
    val b = background(pal.fond)
    if (pal.halos.isEmpty()) return b
    return b.drawBehind {
        val echelle = sqrt(size.width * size.height) / sqrt(1280f * 820f)
        for (h in pal.halos) {
            val r = h.taille / 2f
            // Le centre du disque, ramené à la taille de l'écran.
            val centre = Offset((h.x + r) / 1280f * size.width, (h.y + r) / 820f * size.height)
            val a = (h.opacite * 1.9f).coerceAtMost(1f)
            val rayonPx = r * echelle
            // Un disque de rayon r flouté sur r : plein au centre, à moitié
            // sur son bord, éteint à deux rayons.
            val pinceau = Brush.radialGradient(
                0f to h.couleur.copy(alpha = a * 0.95f),
                0.25f to h.couleur.copy(alpha = a * 0.84f),
                0.5f to h.couleur.copy(alpha = a * 0.5f),
                0.75f to h.couleur.copy(alpha = a * 0.16f),
                1f to h.couleur.copy(alpha = 0f),
                center = centre,
                radius = rayonPx * 2f,
            )
            drawCircle(pinceau, radius = rayonPx * 2f, center = centre)
        }
    }
}

// ---- Les contrôles ----

/**
 * L'interrupteur du bureau (`ligne_interrupteur`) : 44 × 26, piste d'accent
 * (dégradé au verre, bleu en EGA) allumé, grise et bien visible éteint,
 * pastille blanche ; carré en EGA.
 */
@Composable
fun Interrupteur(actif: Boolean, changer: (Boolean) -> Unit) {
    val decalage by animateDpAsState(
        if (actif) 18.dp else 0.dp, if (P.sec) snap() else spring(), label = "interrupteur",
    )
    Box(
        Modifier.size(44.dp, 26.dp).clip(formePilule)
            .let { if (actif) it.background(P.degrade) else it.background(P.interrupteurEteint) }
            .clickable { changer(!actif) }
            .padding(3.dp),
    ) {
        Box(
            Modifier.padding(start = decalage).size(20.dp)
                .let { if (P.carre) it else it.shadow(2.dp, CircleShape) }
                .clip(formeRonde).background(if (P.sirene) c(GRIS_2000) else Color.White)
                .let { if (P.sirene) it.relief(enfonce = false, epaisseur = 1.5.dp) else it },
        )
    }
}

/** La forme des boutons de Material (pilule, ou carrée en EGA). */
val formeBouton: Shape get() = if (P.carre) RectangleShape else CircleShape

/** En EGA, la couleur EGA foncée la plus proche (avatar sans photo, comme `ega_fond`). */
fun fondAvatar(couleur: Color): Color {
    if (!P.ega) return couleur
    val parmi = listOf(0x0000aaL, 0x00aa00L, 0x00aaaaL, 0xaa0000L, 0xaa00aaL, 0xaa5500L, 0x555555L)
    return c(parmi.minBy { x ->
        val r = ((x shr 16) and 0xff) / 255f - couleur.red
        val g = ((x shr 8) and 0xff) / 255f - couleur.green
        val b = (x and 0xff) / 255f - couleur.blue
        r * r + g * g + b * b
    })
}

/**
 * Les barres système suivent le thème : transparentes (l'app dessine dessous,
 * de la couleur de l'écran), icônes sombres sur les thèmes clairs (Verre, Live,
 * Sirène), claires sinon.
 */
@Composable
private fun BarresSysteme() {
    val activite = LocalContext.current as? ComponentActivity ?: return
    val clair = P.clair
    DisposableEffect(clair) {
        val transparent = android.graphics.Color.TRANSPARENT
        val style = if (clair) SystemBarStyle.light(transparent, transparent) else SystemBarStyle.dark(transparent)
        activite.enableEdgeToEdge(statusBarStyle = style, navigationBarStyle = style)
        // Pas de voile imposé sous la barre de navigation à trois boutons.
        if (android.os.Build.VERSION.SDK_INT >= 29) activite.window.isNavigationBarContrastEnforced = false
        onDispose {}
    }
}

/** La typographie de Material, toute en police IBM pour l'EGA. */
private val TYPO_EGA: Typography by lazy {
    val base = Typography()
    fun TextStyle.ibm() = copy(fontFamily = POLICE_IBM)
    Typography(
        displayLarge = base.displayLarge.ibm(), displayMedium = base.displayMedium.ibm(), displaySmall = base.displaySmall.ibm(),
        headlineLarge = base.headlineLarge.ibm(), headlineMedium = base.headlineMedium.ibm(), headlineSmall = base.headlineSmall.ibm(),
        titleLarge = base.titleLarge.ibm(), titleMedium = base.titleMedium.ibm(), titleSmall = base.titleSmall.ibm(),
        bodyLarge = base.bodyLarge.ibm(), bodyMedium = base.bodyMedium.ibm(), bodySmall = base.bodySmall.ibm(),
        labelLarge = base.labelLarge.ibm(), labelMedium = base.labelMedium.ibm(), labelSmall = base.labelSmall.ibm(),
    )
}
private val TYPO = Typography()

/** Toutes les formes de Material carrées, pour l'EGA. */
private val CARRE = RoundedCornerShape(0.dp)
private val FORMES_EGA = Shapes(extraSmall = CARRE, small = CARRE, medium = CARRE, large = CARRE, extraLarge = CARRE)
private val FORMES = Shapes()

@Composable
fun JasetteTheme(contenu: @Composable () -> Unit) {
    BarresSysteme()
    val couleurs = if (P.clair) {
        lightColorScheme(
            primary = P.accent, onPrimary = Color.White, secondary = P.expediteur,
            background = P.fond, onBackground = P.texteFort, surface = P.menu, onSurface = P.texteFort,
            surfaceVariant = P.champ, onSurfaceVariant = P.texte,
            surfaceContainer = P.menu, surfaceContainerHigh = P.menu, surfaceContainerHighest = P.menu,
            surfaceContainerLow = P.menu, surfaceContainerLowest = P.menu,
            outline = P.textePale, outlineVariant = P.traitFin, error = P.danger,
        )
    } else {
        darkColorScheme(
            primary = P.accent, onPrimary = if (P.ega) Color.Black else Color.White, secondary = P.expediteur,
            background = P.fond, onBackground = P.texteFort, surface = P.fond, onSurface = P.texteFort,
            surfaceVariant = P.surface, onSurfaceVariant = P.texte,
            surfaceContainer = P.menu, surfaceContainerHigh = P.menu, surfaceContainerHighest = P.champ,
            outline = if (P.ega) P.texte else P.traitFin, outlineVariant = P.traitFin, error = P.danger,
        )
    }
    MaterialTheme(
        colorScheme = couleurs,
        typography = if (P.ega) TYPO_EGA else TYPO,
        shapes = if (P.carre) FORMES_EGA else FORMES,
        content = contenu,
    )
}
