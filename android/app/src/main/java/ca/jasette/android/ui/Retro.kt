package ca.jasette.android.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.windowInsetsTopHeight
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarColors
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Depot
import ca.jasette.android.Etat
import ca.jasette.android.Service
import ca.jasette.android.t

// ---- Live et Sirène : les deux thèmes clairs « années 2000 » ----
//
// Live (maquette Main.dc.html) : un ciel bleu en dégradé, une barre de titre bleue
// brillante (un reflet en haut, une coupure nette au milieu), des boutons en pilule
// blanc → bleu pâle, les photos dans un cadre vert brillant, l'état en bonhomme.
// Sirène (maquette Sirene.dc.html) : Windows 2000. Gris #d4d0c8, reliefs (clair en
// haut à gauche, sombre en bas à droite), champs et listes en creux sur blanc, barre
// de titre verte, coins carrés, l'état en goutte, aucune animation fluide.
// Dans les deux, le fil n'a pas de bulles : un en-tête par suite de messages
// (« Camille dit (19:24) : » en Live, le nom en couleur et l'heure en Sirène),
// le texte dessous, en retrait.

/** Le bleu brillant de Live (bouton Envoyer, boutons pleins) : reflet en haut, coupure au milieu. */
val BRILLANT_LIVE: Brush = Brush.verticalGradient(
    0f to couleur(0x9fd0ff), 0.48f to couleur(0x4a96ec), 0.52f to couleur(0x2f7ee0), 1f to couleur(0x5aa6f2),
)

/** La barre de titre de Live : la même brillance, coupure franche au milieu. */
private val BARRE_LIVE: Brush = Brush.verticalGradient(
    0f to couleur(0x7db6f2), 0.5f to couleur(0x3a86e0), 0.5f to couleur(0x2a6fcf), 1f to couleur(0x4d97ea),
)

/** La barre de titre de Sirène : du vert sombre au vert clair, de gauche à droite. */
private val BARRE_SIRENE: Brush = Brush.horizontalGradient(listOf(couleur(0x0d5c1f), couleur(0x3aa64a)))

/** Le cadre vert brillant des photos de Live. */
private val VERT_LIVE: Brush = Brush.verticalGradient(listOf(couleur(0xb7f08f), couleur(0x4bb52a)))

/** Les boutons en pilule de Live : blanc vers bleu pâle. */
private val PILULE_LIVE: Brush = Brush.verticalGradient(listOf(Color.White, couleur(0xd6e7fb)))

/**
 * Le relief de Windows 2000 (Sirène), dessiné par-dessus le contenu : en relief,
 * un liseré clair en haut et à gauche, sombre en bas et à droite ; en creux, l'inverse.
 */
fun Modifier.relief(
    enfonce: Boolean,
    epaisseur: Dp = 2.dp,
    clair: Color = Color.White,
    sombre: Color = if (enfonce) couleur(0x808080) else couleur(0x404040),
): Modifier = drawWithContent {
    drawContent()
    val e = epaisseur.toPx()
    val (hautGauche, basDroit) = if (enfonce) sombre to clair else clair to sombre
    drawRect(hautGauche, Offset.Zero, Size(size.width, e))
    drawRect(hautGauche, Offset.Zero, Size(e, size.height))
    drawRect(basDroit, Offset(0f, size.height - e), Size(size.width, e))
    drawRect(basDroit, Offset(size.width - e, 0f), Size(e, size.height))
}

/** Un trait d'un côté (haut, bas ou droite), dessiné par-dessus le contenu. */
private fun Modifier.trait(couleur: Color, cote: Char, decalage: Dp = 0.dp): Modifier = drawWithContent {
    drawContent()
    val e = 1.dp.toPx()
    val d = decalage.toPx()
    when (cote) {
        'h' -> drawRect(couleur, Offset(0f, d), Size(size.width, e))
        'b' -> drawRect(couleur, Offset(0f, size.height - e - d), Size(size.width, e))
        else -> drawRect(couleur, Offset(size.width - e - d, 0f), Size(e, size.height))
    }
}

/** Le fond de la barre de titre : bleu brillant (Live), vert (Sirène). */
private fun Modifier.fondBarreTitre(): Modifier = when {
    P.live -> background(BARRE_LIVE).trait(couleur(0x2a63b4), 'b')
    P.sirene -> background(BARRE_SIRENE)
    else -> this
}

/** Le panneau du bas (barre de navigation, barre d'envoi) : dégradé bleu pâle (Live), gris en relief (Sirène). */
fun Modifier.fondPanneauBas(): Modifier = when {
    P.live -> background(Brush.verticalGradient(listOf(couleur(0xe6f1fd), couleur(0xd4e6fa)))).trait(couleur(0x94bbe8), 'h')
    P.sirene -> background(couleur(GRIS_2000)).trait(couleur(0x808080), 'h').trait(Color.White, 'h', 1.dp)
    else -> background(P.liste)
}

/** Le rail de gauche (grand écran) : comme le rail de la maquette Live, gris en Sirène. */
fun Modifier.fondRail(): Modifier = when {
    P.live -> background(Brush.verticalGradient(listOf(couleur(0xd6e8fb), couleur(0xb9d6f6)))).trait(couleur(0x94bbe8), 'd')
    P.sirene -> background(couleur(GRIS_2000)).trait(couleur(0x808080), 'd')
    else -> this
}

/**
 * Un bouton secondaire (le +, l'emoji rapide, les touches du code) : pilule
 * blanc → bleu pâle à liseré bleu (Live), gris en relief (Sirène), sinon une carte.
 */
fun Modifier.boutonTheme(r: Dp): Modifier = when {
    P.live -> {
        val f = RoundedCornerShape(r)
        clip(f).background(PILULE_LIVE).border(1.dp, couleur(0x7aa6dc), f)
    }
    P.sirene -> background(couleur(GRIS_2000)).relief(enfonce = false)
    else -> carte(r)
}

/** Le bouton Envoyer : bleu brillant (Live), vert en relief (Sirène), sinon le dégradé d'accent. */
fun Modifier.boutonEnvoi(): Modifier = when {
    P.live -> clip(CircleShape).background(BRILLANT_LIVE).border(1.dp, couleur(0x2f6fc4), CircleShape)
    P.sirene -> background(couleur(0x2e9a3f)).relief(enfonce = false, clair = couleur(0x8fe39a), sombre = couleur(0x0b4a15))
    else -> clip(formeRonde).background(P.degrade)
}

/** Le liseré des menus déroulants : gris clair en EGA, bleu en Live, gris en Sirène. */
val bordMenu: BorderStroke?
    get() = when {
        P.ega -> BorderStroke(1.dp, P.bordBulle)
        P.live -> BorderStroke(1.dp, couleur(0x7aa6dc))
        P.sirene -> BorderStroke(1.dp, couleur(0x808080))
        else -> null
    }

/**
 * Le cadre d'un avatar de [taille] : cadre vert brillant aux coins arrondis
 * (Live), carré en creux (Sirène), rond ailleurs (carré en EGA). Le contenu
 * (photo ou initiales) se dessine dedans, déjà découpé.
 */
fun Modifier.cadreAvatar(taille: Dp): Modifier = when {
    P.live -> {
        val dehors = RoundedCornerShape(taille * 0.16f)
        val marge = (taille * 0.055f).coerceAtLeast(2.dp)
        size(taille).shadow(2.dp, dehors, ambientColor = couleur(0x1e5a14), spotColor = couleur(0x1e5a14))
            .clip(dehors).background(VERT_LIVE).padding(marge).clip(RoundedCornerShape(taille * 0.11f))
    }
    P.sirene -> size(taille).relief(enfonce = true, epaisseur = if (taille >= 40.dp) 2.dp else 1.dp)
    else -> size(taille).clip(formeRonde)
}

/**
 * La barre du haut des écrans. En Live et Sirène : la barre d'état sur une bande
 * claire (ses icônes restent foncées), puis la barre de titre dessinée (bleu
 * brillant ou vert) avec le titre et les icônes en blanc. Ailleurs, la TopAppBar
 * de Material telle quelle.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BarreHaut(
    title: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    navigationIcon: @Composable () -> Unit = {},
    actions: @Composable RowScope.() -> Unit = {},
    colors: TopAppBarColors = barreCouleurs(),
) {
    if (!P.retro) {
        TopAppBar(title = title, modifier = modifier, navigationIcon = navigationIcon, actions = actions, colors = colors)
        return
    }
    Column(modifier.background(P.bandeau)) {
        Spacer(Modifier.fillMaxWidth().windowInsetsTopHeight(WindowInsets.statusBars))
        // Le titre de Live a la petite ombre de la maquette (text-shadow).
        val typo = MaterialTheme.typography
        val titre = if (P.live) typo.titleLarge.copy(shadow = Shadow(couleur(0x001e50).copy(alpha = 0.5f), Offset(0f, 2f), 2f)) else typo.titleLarge
        MaterialTheme(typography = typo.copy(titleLarge = titre)) {
            TopAppBar(
                title = title,
                modifier = Modifier.fondBarreTitre(),
                navigationIcon = navigationIcon,
                actions = actions,
                windowInsets = TopAppBarDefaults.windowInsets.only(WindowInsetsSides.Horizontal),
                colors = barreCouleurs(Color.Transparent),
            )
        }
    }
}

/**
 * Le bouton « Nouveau message » de Live (rond bleu brillant) et de Sirène
 * (carré vert en relief).
 */
@Composable
fun BoutonNouveauRetro(onClick: () -> Unit) {
    val m = if (P.live) {
        Modifier.size(56.dp).shadow(6.dp, CircleShape, ambientColor = couleur(0x0a285a), spotColor = couleur(0x0a285a)).boutonEnvoi()
    } else {
        Modifier.size(52.dp).boutonEnvoi()
    }
    Box(m.clickable(onClickLabel = t("Nouveau message"), onClick = onClick), contentAlignment = Alignment.Center) {
        Icon(Icons.Filled.Add, t("Nouveau message"), tint = Color.White, modifier = Modifier.size(28.dp))
    }
}

// ---- L'état d'un service : bonhomme (Live), goutte (Sirène), point ailleurs ----

private val CHEMIN_CORPS: Path = PathParser().parsePathString("M3.5 21c0-4.6 3.8-7.3 8.5-7.3s8.5 2.7 8.5 7.3z").toPath()
private val CHEMIN_GOUTTE: Path = PathParser().parsePathString("M12 3c3 3 5 6 5 9a5 5 0 0 1-10 0c0-3 2-6 5-9z").toPath()

/** Les teintes (tête, corps, trait) du bonhomme de Live, et (remplissage, trait) de la goutte de Sirène. */
private enum class Niveau(val tete: Long, val corps: Long, val trait: Long, val goutte: Long, val bordGoutte: Long) {
    VERT(0xbff0a8, 0x7fd45f, 0x2f8a22, 0x5fd06a, 0x0b4a15),
    ORANGE(0xffe2a8, 0xf7b844, 0xc07a12, 0xffd34d, 0x8a6200),
    ROUGE(0xffc2b8, 0xef6a52, 0xb32a17, 0xff7a6a, 0x8a1a0a),
    GRIS(0xe3e8ee, 0xc4ccd6, 0x8a97a8, 0xd84040, 0x6a0a0a),
}

private fun niveau(e: Etat) = when (e) {
    Etat.PRET -> Niveau.VERT
    Etat.TELEPHONE_HORS_LIGNE, Etat.NON_ASSOCIE -> Niveau.ROUGE
    Etat.ARRETE -> Niveau.GRIS
    else -> Niveau.ORANGE
}

/** La taille de la pastille d'état : un point de 8 dp, ou un bonhomme ou une goutte de 14 dp. */
val taillePastilleEtat: Dp get() = if (P.retro) 14.dp else 8.dp

/** La pastille de l'état d'un service : bonhomme (Live), goutte (Sirène), point de couleur ailleurs. */
@Composable
fun PastilleEtat(etat: Etat, modifier: Modifier = Modifier) {
    if (!P.retro) {
        Box(modifier.size(8.dp).clip(formeRonde).background(couleurEtat(etat)))
        return
    }
    val n = niveau(etat)
    val live = P.live
    Canvas(modifier.size(14.dp)) {
        scale(size.width / 24f, size.height / 24f, pivot = Offset.Zero) {
            val trait = Stroke(1.4f)
            if (live) {
                drawPath(CHEMIN_CORPS, couleur(n.corps))
                drawPath(CHEMIN_CORPS, couleur(n.trait), style = trait)
                drawCircle(couleur(n.tete), 4.5f, Offset(12f, 8f))
                drawCircle(couleur(n.trait), 4.5f, Offset(12f, 8f), style = trait)
            } else {
                drawPath(CHEMIN_GOUTTE, couleur(n.goutte))
                drawPath(CHEMIN_GOUTTE, couleur(n.bordGoutte), style = trait)
            }
        }
    }
}

// ---- Le fil sans bulles ----

/** Le retrait du texte sous l'en-tête d'une suite de messages. */
val retraitLigne: Dp get() = if (P.live) 14.dp else 10.dp

/** Le texte d'un message : bleu foncé pour moi en Live, noir en Sirène. */
fun couleurTexteLigne(moi: Boolean): Color = when {
    P.live -> if (moi) couleur(0x0b3c8a) else couleur(0x1d2f4a)
    else -> couleur(0x000000)
}

/** Mon nom pour le fil : celui du compte du service (prénom), sinon null (pas une adresse ni un numéro). */
fun monNom(service: Service): String? =
    Depot.noms[service]?.trim()?.takeIf { it.isNotEmpty() && '@' !in it && it.count { c -> c.isLetter() } >= 2 }

/**
 * L'en-tête d'une suite de messages : « Camille dit (19:24) : » en gris (Live),
 * ou le nom en couleur, rouge pour l'autre et bleu pour moi, puis l'heure en
 * gris (Sirène). [qui] : le nom (null pour moi sans nom connu).
 */
@Composable
fun EnteteLigne(qui: String?, moi: Boolean, quand: String, taille: TextUnit = 12.sp) {
    if (P.live) {
        val prenom = qui?.split(Regex("\\s+"))?.firstOrNull { it.isNotEmpty() }
        Text(
            if (prenom != null) t("%s dit (%s) :").format(prenom, quand) else t("Moi (%s) :").format(quand),
            color = couleur(0x5d7088), fontSize = taille,
        )
    } else {
        Text(
            buildAnnotatedString {
                withStyle(SpanStyle(color = if (moi) couleur(0x1838c0) else couleur(0xc01818), fontWeight = FontWeight.Bold)) {
                    append(qui ?: t("Moi"))
                }
                withStyle(SpanStyle(color = couleur(0x6a6a6a))) { append(" ($quand)") }
            },
            fontSize = taille * 1.15f,
        )
    }
}

/** Un message d'exemple sans bulle (Réglages, Mood) : l'en-tête, puis le texte en retrait. */
@Composable
fun LigneExemple(qui: String?, moi: Boolean, texte: String, taille: TextUnit, quand: String = "19:24") {
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        EnteteLigne(qui, moi, quand)
        Text(texte, color = couleurTexteLigne(moi), fontSize = taille, modifier = Modifier.padding(start = retraitLigne))
    }
}

/** La même chose en miniature (cartes du Mood) : un trait pour l'en-tête, un pour le texte. */
@Composable
fun LigneMiniature(moi: Boolean) {
    val nom = when {
        P.live -> couleur(0x8a9bb0)
        moi -> couleur(0x1838c0)
        else -> couleur(0xc01818)
    }
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Box(Modifier.size(if (moi) 18.dp else 24.dp, 3.dp).background(nom))
        Box(Modifier.padding(start = 4.dp).size(if (moi) 30.dp else 40.dp, 4.dp).background(SolidColor(couleurTexteLigne(moi)), alpha = 0.7f))
    }
}
