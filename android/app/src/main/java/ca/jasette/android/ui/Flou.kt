package ca.jasette.android.ui

import android.os.Build
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.background
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.ripple
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import ca.jasette.android.t
import dev.chrisbanes.haze.HazeInput
import dev.chrisbanes.haze.HazeSourceSelection
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.blur.HazeBlurStyle
import dev.chrisbanes.haze.blur.HazeColorEffect
import dev.chrisbanes.haze.blur.hazeBlur
import dev.chrisbanes.haze.glass.GlassStyle
import dev.chrisbanes.haze.glass.hazeGlass
import dev.chrisbanes.haze.hazeSource
import kotlinx.coroutines.flow.collectLatest

// ---- Le flou et le verre (Haze) : barres flottantes, fondus des bords, bouton + en verre liquide ----
//
// Un seul HazeState pour toute l'app ([LocalFlou]). Les sources sont repérées par leur clé :
//  - FOND : le fond de la fenêtre (aplat et halos du verre), sous tout le reste ;
//  - LISTE : les rangées de la liste des conversations ;
//  - FIL : les messages de la conversation ouverte.
// Chaque effet ne prend que les sources de sa zone (le fond plus la sienne) : en deux volets,
// la barre de la liste n'échantillonne jamais le fil, et inversement.
// Les sources ne dessinent que leur contenu (fond transparent) ; les voiles des écrans (P.ecran,
// P.fil) restent dessinés à part, et la teinte des barres les remplace dans le flou.
// En EGA : ni flou, ni fondu, ni verre (barres opaques, coupures nettes, bouton carré).

/** L'état du flou de l'app, fourni par [App]. */
val LocalFlou = staticCompositionLocalOf<HazeState?> { null }

const val FLOU_FOND = "fond"
const val FLOU_LISTE = "liste"
const val FLOU_FIL = "fil"
/** Le fond du Mood d'une conversation (Mood.kt), sous ses messages. */
const val FLOU_MOOD = "mood"

/** Ce que voient les effets de la liste : le fond (avec, en deux volets, le Mood du fil ouvert) et les rangées (gardés comme objets : comparés par identité). */
val SELECTION_LISTE: HazeSourceSelection = HazeSourceSelection.Behind.where { it.key == FLOU_FOND || it.key == FLOU_MOOD || it.key == FLOU_LISTE }
/** Ce que voient les effets du fil : le fond et les messages. */
val SELECTION_FIL: HazeSourceSelection = HazeSourceSelection.Behind.where { it.key == FLOU_FOND || it.key == FLOU_MOOD || it.key == FLOU_FIL }

/** Une source du flou (fond, liste ou fil) ; sans état fourni, rien. */
fun Modifier.sourceFlou(etat: HazeState?, cle: String, z: Float = 1f): Modifier =
    if (etat == null) this else hazeSource(etat, zIndex = z, key = cle)

/**
 * Le voile d'une barre flottante : la couleur des menus du thème, à peine teintée
 * d'accent. [opacite] : celle posée sur le flou ; le repli sans flou est plus opaque.
 */
private fun voileBarre(opacite: Float): Color =
    lerp(P.menu, P.accent, if (P.clair) 0.07f else 0.05f).copy(alpha = opacite)

/**
 * Le style des barres : flou de 24 dp, peu de grain, le voile du thème par-dessus.
 * Sous Android 12 (API 31), Haze ne floute pas : un fond semi-opaque à la place
 * (fallbackColorEffect). Lu au dessin : changer de thème ne recompose rien.
 */
private val STYLE_BARRE = HazeBlurStyle {
    blurRadius(24.dp)
    noiseFactor(0.05f)
    colorEffects(listOf(HazeColorEffect.tint(voileBarre(if (P.clair) 0.55f else 0.68f))))
    fallbackColorEffect(HazeColorEffect.tint(voileBarre(0.94f)))
}

/**
 * Une barre flottante floutée (barre du bas, en-tête et saisie du fil) : le flou des
 * sources de [selection] derrière elle. En EGA, Live, Sirène ou sans état : [opaque] (l'aplat du thème).
 */
@Composable
fun Modifier.barreFloutee(selection: HazeSourceSelection, opaque: Color, voile: Color? = null): Modifier {
    val etat = LocalFlou.current
    if (P.plein || etat == null) return if (voile != null) background(voile.copy(alpha = 0.94f)) else if (opaque.alpha > 0f) background(opaque) else this
    if (voile == null) return hazeBlur(HazeInput.Sources(etat, selection), STYLE_BARRE)
    // Le voile d'un Mood : la couleur du haut de son fond, pour que l'en-tête s'y fonde.
    val style = remember(voile) {
        HazeBlurStyle {
            blurRadius(24.dp)
            noiseFactor(0.05f)
            colorEffects(listOf(HazeColorEffect.tint(voile.copy(alpha = if (P.clair) 0.5f else 0.6f))))
            fallbackColorEffect(HazeColorEffect.tint(voile.copy(alpha = 0.94f)))
        }
    }
    return hazeBlur(HazeInput.Sources(etat, selection), style)
}

/**
 * Une valeur de 0 à 1 qui suit [condition] avec une courte animation, sans jamais
 * recomposer l'appelant : [condition] est lue dans un snapshotFlow, et la valeur
 * ne doit être lue qu'au dessin.
 */
@Composable
fun rememberApparition(dureeMs: Int = 180, condition: () -> Boolean): State<Float> {
    val actuelle by rememberUpdatedState(condition)
    val a = remember { Animatable(if (condition()) 1f else 0f) }
    LaunchedEffect(Unit) {
        snapshotFlow { actuelle() }.collectLatest { a.animateTo(if (it) 1f else 0f, if (P.sec) snap() else tween(dureeMs)) }
    }
    return a.asState()
}

// ---- L'ombre sous la barre du haut des écrans qui défilent ----
//
// Une seule règle pour tous les écrans (boîte de réception, Favoris, Archives, vues
// par service, Réglages, Nouveau message, formulaires de connexion) : l'ombre (le
// fondu du haut de [fonduBords]) n'apparaît qu'une fois la liste défilée, et
// disparaît dès qu'on revient tout en haut, à 0 px, en 150 ms. À poser avant le
// défilement (`verticalScroll`) ou sur la LazyColumn : elle suit la fenêtre visible.
// En EGA et Sirène, comme tous les fondus : rien (coupure nette).

private const val DUREE_OMBRE_HAUT = 150
private val HAUTEUR_OMBRE_HAUT = 28.dp

/** L'ombre du haut d'une LazyColumn : seulement hors du haut (premier élément, 0 px). */
@Composable
fun Modifier.ombreHaut(etat: LazyListState): Modifier {
    val visible = rememberApparition(DUREE_OMBRE_HAUT) {
        !(etat.firstVisibleItemIndex == 0 && etat.firstVisibleItemScrollOffset == 0)
    }
    return fonduBords(HAUTEUR_OMBRE_HAUT, hautVisible = visible)
}

/** `verticalScroll` avec l'ombre du haut : le défilement vertical des écrans à barre du haut. */
@Composable
fun Modifier.defilementAvecOmbre(etat: ScrollState = rememberScrollState()): Modifier =
    ombreHaut(etat).verticalScroll(etat)

/** L'ombre du haut d'une colonne `verticalScroll(etat)` : seulement hors du haut (0 px). */
@Composable
fun Modifier.ombreHaut(etat: ScrollState): Modifier {
    val visible = rememberApparition(DUREE_OMBRE_HAUT) { etat.value != 0 }
    return fonduBords(HAUTEUR_OMBRE_HAUT, hautVisible = visible)
}

/**
 * Le fondu des bords d'une liste qui défile : un masque de transparence (DstIn) sur
 * le contenu, rendu hors écran, donc juste aussi sur le fond Verre en dégradé.
 *
 * En haut : transparent jusqu'à [hautDebut] (px, sous un en-tête flottant), puis le
 * contenu revient sur [hauteur]. En bas, pareil depuis [basDebut] (px depuis le bas).
 * [hautVisible] et [basVisible] (0 à 1) dosent chaque fondu ; à 0 partout, le calque
 * hors écran est rendu au système. Tout est lu au dessin : aucune recomposition.
 * En EGA et Sirène : rien (coupure nette).
 */
fun Modifier.fonduBords(
    hauteur: Dp,
    hautVisible: State<Float>?,
    basVisible: State<Float>? = null,
    hautDebut: () -> Float = { 0f },
    basDebut: () -> Float = { 0f },
): Modifier {
    if (P.sec) return this
    return graphicsLayer {
        val actif = (hautVisible?.value ?: 0f) > 0f || (basVisible?.value ?: 0f) > 0f
        compositingStrategy = if (actif) CompositingStrategy.Offscreen else CompositingStrategy.Auto
    }.drawWithCache {
        val f = hauteur.toPx()
        onDrawWithContent {
            drawContent()
            val h = hautVisible?.value ?: 0f
            if (h > 0f) {
                val y0 = hautDebut()
                drawRect(
                    Brush.verticalGradient(
                        listOf(Color.Black.copy(alpha = 1f - h), Color.Black),
                        startY = y0, endY = y0 + f,
                    ),
                    topLeft = Offset.Zero, size = Size(size.width, y0 + f), blendMode = BlendMode.DstIn,
                )
            }
            val b = basVisible?.value ?: 0f
            if (b > 0f) {
                val y1 = size.height - basDebut()
                drawRect(
                    Brush.verticalGradient(
                        listOf(Color.Black, Color.Black.copy(alpha = 1f - b)),
                        startY = y1 - f, endY = y1,
                    ),
                    topLeft = Offset(0f, y1 - f), size = Size(size.width, size.height - (y1 - f)),
                    blendMode = BlendMode.DstIn,
                )
            }
        }
    }
}

/**
 * Une partie des marges d'un Scaffold, lue seulement à la mesure : la barre qui
 * change de hauteur (clavier, recherche) ne recompose pas l'écran.
 */
class MargesPartielles(
    private val m: PaddingValues,
    private val haut: Boolean,
    private val bas: Boolean,
    private val plusHaut: Dp = 0.dp,
    private val plusBas: Dp = 0.dp,
    private val plusCotes: Dp = 0.dp,
    /** Les marges des côtés du Scaffold (encoches) : une seule fois, au modificateur ou au contenu. */
    private val cotes: Boolean = true,
) : PaddingValues {
    override fun calculateTopPadding() = (if (haut) m.calculateTopPadding() else 0.dp) + plusHaut
    override fun calculateBottomPadding() = (if (bas) m.calculateBottomPadding() else 0.dp) + plusBas
    override fun calculateLeftPadding(layoutDirection: LayoutDirection) =
        (if (cotes) m.calculateLeftPadding(layoutDirection) else 0.dp) + plusCotes
    override fun calculateRightPadding(layoutDirection: LayoutDirection) =
        (if (cotes) m.calculateRightPadding(layoutDirection) else 0.dp) + plusCotes
}

/**
 * Le verre liquide du bouton + : flou et réfraction de ce qui est derrière
 * (RuntimeShader, API 33+), reflet spéculaire et ombre de bord de haze-glass, une
 * pointe d'accent. Lu au dessin (thème compris).
 */
private val STYLE_PASTILLE = GlassStyle.regular.then {
    // La forme et la couleur d'avant (carré arrondi plein d'accent), avec juste
    // un peu de verre : l'accent reste presque opaque.
    shape(RoundedCornerShape(16.dp))
    tint(P.accentPlein.copy(alpha = 0.84f))
    specularIntensity(0.28f)
    edgeShadow(Color.Black.copy(alpha = 0.18f))
}

/**
 * Le bouton flottant « Nouveau message » : une pastille de verre liquide.
 *  - API 33+ : haze-glass complet (flou, réfraction, reflet) ;
 *  - API 31-32 : haze-glass choisit lui-même son rendu simplifié (verre flouté, sans réfraction) ;
 *  - sous l'API 31 : un verre givré dessiné (voile opaque à 88 %), sans flou.
 * Par-dessus, dans tous les cas : le reflet du bord supérieur et un liseré clair.
 * Une petite échelle à l'appui (lue au dessin). En EGA : le bouton carré d'origine.
 */
@Composable
fun BoutonNouveau(onClick: () -> Unit) {
    // Live et Sirène : leur bouton à eux (bleu brillant, vert en relief), voir Retro.kt.
    if (P.retro) return BoutonNouveauRetro(onClick)
    if (P.ega) {
        FloatingActionButton(onClick = onClick, containerColor = P.accentPlein, contentColor = P.surAccent, shape = RectangleShape) {
            Icon(Icons.Filled.Add, t("Nouveau message"))
        }
        return
    }
    val etat = LocalFlou.current
    val interactions = remember { MutableInteractionSource() }
    val presse by interactions.collectIsPressedAsState()
    val echelle = animateFloatAsState(
        if (presse) 0.9f else 1f,
        spring(dampingRatio = Spring.DampingRatioMediumBouncy, stiffness = Spring.StiffnessMedium),
        label = "echelle",
    )
    val verre = etat != null && Build.VERSION.SDK_INT >= 31
    val couleurIcone = P.surAccent
    val formeBouton = RoundedCornerShape(16.dp)
    Box(
        Modifier
            .graphicsLayer {
                scaleX = echelle.value
                scaleY = echelle.value
            }
            .size(56.dp)
            .shadow(
                8.dp, formeBouton,
                ambientColor = Color.Black.copy(alpha = 0.3f), spotColor = Color.Black.copy(alpha = 0.4f),
            )
            .then(
                if (verre) {
                    Modifier.hazeGlass(HazeInput.Sources(etat, SELECTION_LISTE), STYLE_PASTILLE)
                } else {
                    Modifier.background(P.accentPlein, formeBouton)
                },
            )
            .clip(formeBouton)
            .drawWithCache {
                val trait = 1.dp.toPx()
                // Le liseré : clair en haut, presque éteint au milieu, un peu de retour en bas.
                val lisere = Brush.verticalGradient(
                    0f to Color.White.copy(alpha = 0.35f),
                    0.5f to Color.White.copy(alpha = 0.05f),
                    1f to Color.White.copy(alpha = 0.12f),
                )
                // Le reflet : un croissant brillant sur le bord supérieur.
                val reflet = Brush.verticalGradient(
                    listOf(Color.White.copy(alpha = 0.18f), Color.Transparent),
                    startY = 0f, endY = size.height * 0.45f,
                )
                onDrawWithContent {
                    // Le reflet suit la forme du bouton : une bande en haut, découpée
                    // par le carré arrondi (clip), qui s'efface vers le milieu.
                    drawRect(reflet, size = Size(size.width, size.height * 0.45f))
                    val r = 16.dp.toPx() - trait / 2f
                    drawRoundRect(
                        lisere, topLeft = Offset(trait / 2f, trait / 2f),
                        size = Size(size.width - trait, size.height - trait),
                        cornerRadius = androidx.compose.ui.geometry.CornerRadius(r, r), style = Stroke(trait),
                    )
                    drawContent()
                }
            }
            .clickable(interactionSource = interactions, indication = ripple(color = couleurIcone), onClick = onClick)
            .semantics { role = Role.Button },
        contentAlignment = Alignment.Center,
    ) {
        Icon(Icons.Filled.Add, t("Nouveau message"), tint = couleurIcone, modifier = Modifier.size(28.dp))
    }
}
