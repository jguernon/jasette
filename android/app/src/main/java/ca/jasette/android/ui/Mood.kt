package ca.jasette.android.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import ca.jasette.android.Marques
import ca.jasette.android.t

// ---- Le « Mood » d'une conversation : couleurs des bulles et fond du fil ----
//
// Comme les moods du bureau : mêmes ids, mêmes couleurs, mêmes
// dérivations. Choisi par conversation (menu ⋮ > Mood), gardé dans [Marques.moods]
// et partagé par le serveur Jasette s'il y en a un (champ `moods`, §5.5).
// Deux variantes de fond : Sombre (profond, assombri) et Verre (clair, pastel).
// L'EGA n'en a pas : un mood enregistré y est ignoré. En Live et Sirène, dont le
// fil n'a pas de bulles, le mood ne fait que teinter discrètement le fond du fil
// (un dégradé très pâle de ses deux premières couleurs, sans taches).

/**
 * Un mood : [nomFr]/[nomEn], le dégradé de mes bulles ([envoyeA] → [envoyeB])
 * et son texte ([texte]), puis trois couleurs vives ([t1], [t2], [t3]) d'où
 * viennent le fond (dégradé et taches floues) et la teinte des bulles reçues.
 */
class Mood(
    val id: String,
    val nomFr: String,
    val nomEn: String,
    val envoyeA: Long,
    val envoyeB: Long,
    val texte: Long,
    val t1: Long,
    val t2: Long,
    val t3: Long,
) {
    val nom: String get() = if (ca.jasette.android.Langue.anglais) nomEn else nomFr

    /** Mes bulles : dégradé à 135°. */
    val envoye: Brush get() = Brush.linearGradient(listOf(couleur(envoyeA), couleur(envoyeB)), Offset.Zero, Offset.Infinite)
    val texteEnvoye: Color get() = couleur(texte)
    /** L'heure et les coches posées sur ma bulle. */
    val heureEnvoyee: Color get() = couleur(texte).copy(alpha = 0.7f)

    /** La bulle reçue : une teinte légère du mood. */
    fun recue(clair: Boolean): Color =
        if (clair) mix(t1, 0xffffffL, 0.86f).copy(alpha = 0.85f) else mix(t1, 0x1c1c1eL, 0.80f)
    /** Le texte d'une bulle reçue. */
    fun texteRecu(clair: Boolean): Color = if (clair) P.texteFort else Color.White

    /** Le haut et le bas du dégradé du fond. */
    fun haut(clair: Boolean): Color = if (clair) mix(t1, 0xffffffL, 0.80f) else mix(t1, 0x000000L, 0.84f)
    fun bas(clair: Boolean): Color = if (clair) mix(t2, 0xffffffL, 0.84f) else mix(t2, 0x000000L, 0.88f)
}

val MOODS = listOf(
    Mood("rose_bebe", "Rose bébé", "Baby pink", 0xffb8d2, 0xf7a1c6, 0x4a1130, 0xff9ec4, 0xffc4dc, 0xe9a8ff),
    Mood("amour", "Amour", "Love", 0xe11d48, 0xbe123c, 0xffffff, 0xff3b5c, 0xff7a8a, 0xb0174f),
    Mood("bleu_bebe", "Bleu bébé", "Baby blue", 0xaedcff, 0x8ec5ff, 0x0d2a4a, 0x8ecbff, 0xbde3ff, 0xa9b6ff),
    Mood("vert_pomme", "Vert pomme", "Apple green", 0x9be15d, 0x6fd14a, 0x123508, 0x9be15d, 0xc6f27a, 0x4fd18b),
    Mood("lavande", "Lavande", "Lavender", 0x7c5cf0, 0x6544dc, 0xffffff, 0xb69cff, 0xd4c2ff, 0x8fa8ff),
    Mood("peche", "Pêche", "Peach", 0xffbf98, 0xffa07e, 0x4a1d08, 0xffb38a, 0xffd3b0, 0xff9aa2),
    Mood("menthe", "Menthe", "Mint", 0x0c8470, 0x0b7a76, 0xffffff, 0x6ee7c5, 0xa7f3d0, 0x5ccfe6),
    Mood("soleil", "Soleil", "Sunshine", 0xffd84a, 0xffb92e, 0x3d2a00, 0xffd93d, 0xffe98a, 0xffa94d),
    Mood("ocean", "Océan", "Ocean", 0x0a78b8, 0x0b5aa0, 0xffffff, 0x1fb6ff, 0x3ad1c6, 0x2a5bd7),
    Mood("aurore", "Aurore", "Aurora", 0x7c3aed, 0xc0267a, 0xffffff, 0xa78bfa, 0xf472b6, 0x60a5fa),
    Mood("nuit", "Nuit étoilée", "Starry night", 0x3346c8, 0x5233b8, 0xffffff, 0x3949ab, 0x5e35b1, 0x1e88e5),
    Mood("coucher", "Coucher de soleil", "Sunset", 0xcc3f0c, 0xbe2a5a, 0xffffff, 0xff8a3d, 0xff5e7e, 0x8b5cf6),
)

/** Le mood d'un id ; null pour « Par défaut », un id inconnu, ou en EGA. */
fun moodDe(id: String?): Mood? = if (id == null || P.ega) null else MOODS.firstOrNull { it.id == id }

/** Le mood de la conversation [conv] (null en EGA ou sans mood). */
fun moodDeConv(conv: String): Mood? = moodDe(Marques.mood(conv))

/** Le mood du fil en cours de dessin (null : les couleurs du thème). */
val LocalMood = staticCompositionLocalOf<Mood?> { null }

/** Interpolation RVB, canal par canal : [t] = 0 donne [a], 1 donne [b]. */
fun mix(a: Long, b: Long, t: Float): Color {
    fun canal(x: Long, d: Int) = ((x shr d) and 0xff).toFloat()
    fun m(d: Int) = (canal(a, d) + (canal(b, d) - canal(a, d)) * t) / 255f
    return Color(m(16), m(8), m(0))
}

/**
 * Le fond d'un mood : un dégradé vertical doux, puis trois taches floues
 * (dégradés radiaux superposés, couleur → transparent). [clair] : la variante
 * Verre (pastel), sinon Sombre (profond).
 *
 * [gauche] : en deux volets (Fold déplié), la boîte couvre la liste et le fil, et
 * le fil commence à [gauche]. Le dégradé reste dans le fil ; les taches, placées
 * d'après le fil, débordent sous la liste. Il faut le dessiner AVANT la liste
 * (dessous) : dessiné par le fil, après elle, il la voilait. Découpé à sa boîte.
 */
fun Modifier.fondMood(m: Mood, clair: Boolean, gauche: Dp = 0.dp): Modifier =
    if (P.retro) fondMoodDiscret(m, gauche) else clipToBounds().drawBehind {
    val gx = gauche.toPx().coerceIn(0f, size.width)
    val w = size.width - gx
    drawRect(Brush.verticalGradient(listOf(m.haut(clair), m.bas(clair))), topLeft = Offset(gx, 0f), size = Size(w, size.height))
    val r = w * 0.75f
    for ((c, a, x, y) in listOf(
        // Les opacités du bureau (src/mood.rs, PLACES) : Sombre, puis Verre.
        Tache(m.t1, if (clair) 0.62f else 0.55f, 0.15f, 0.20f),
        Tache(m.t2, if (clair) 0.55f else 0.45f, 0.85f, 0.55f),
        Tache(m.t3, if (clair) 0.50f else 0.42f, 0.35f, 0.92f),
    )) {
        val alpha = a
        val centre = Offset(gx + w * x, size.height * y)
        drawCircle(
            Brush.radialGradient(
                0f to couleur(c).copy(alpha = alpha),
                0.55f to couleur(c).copy(alpha = alpha * 0.45f),
                1f to couleur(c).copy(alpha = 0f),
                center = centre, radius = r,
            ),
            radius = r, center = centre,
        )
    }
}

/**
 * Live et Sirène : le fond blanc du fil à peine teinté du mood (texte foncé toujours
 * lisible), plus marqué en haut qu'en bas, comme le fil de Live (#f4f9ff → blanc).
 * Seulement dans le fil (à partir de [gauche]) : la liste garde son fond.
 */
private fun Modifier.fondMoodDiscret(m: Mood, gauche: Dp): Modifier = drawBehind {
    val force = if (P.live) 0.76f else 0.82f
    val gx = gauche.toPx().coerceIn(0f, size.width)
    drawRect(
        Brush.verticalGradient(listOf(mix(m.t1, 0xffffffL, force), mix(m.t2, 0xffffffL, force + 0.07f))),
        topLeft = Offset(gx, 0f), size = Size(size.width - gx, size.height),
    )
}

private data class Tache(val c: Long, val a: Float, val x: Float, val y: Float)

/** Les emojis rapides proposés (« Aucun » vient avant, « Autre… » après). */
val EMOJIS_RAPIDES = listOf("👍", "❤️", "😂", "😍", "🥰", "🔥", "🎉", "😘", "🤗", "👋", "🌹", "✨", "🙏", "😎", "💯", "😊")

/**
 * La feuille « Mood » d'une conversation : aperçu en direct, puis la grille des
 * moods et la rangée « Emoji rapide » (aucun par défaut). En EGA, qui n'a pas
 * de mood, elle n'a que l'emoji rapide (item « Emoji rapide » du menu ⋮).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FeuilleMood(conv: String, nom: String, fermer: () -> Unit) {
    val actuel = Marques.mood(conv)
    var choix by remember { mutableStateOf(actuel) }
    var emoji by remember { mutableStateOf(Marques.emojiRapide(conv)) }
    var autre by remember { mutableStateOf(false) }
    val ega = P.ega
    val etat = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    fun appliquer(id: String?) {
        if (!ega) Marques.choisirMood(conv, id)
        Marques.choisirEmojiRapide(conv, emoji)
        fermer()
    }
    if (autre) GrilleEmojis(fermer = { autre = false }) { e ->
        emoji = e
        autre = false
    }
    ModalBottomSheet(onDismissRequest = fermer, sheetState = etat, containerColor = P.menu) {
        Column(
            Modifier.fillMaxWidth().navigationBarsPadding().verticalScroll(rememberScrollState())
                .padding(horizontal = 18.dp).padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(if (ega) t("Emoji rapide") else t("Mood"), color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 22.sp)
                    if (nom.isNotEmpty()) {
                        Text(nom, color = P.textePale, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
                Box(
                    Modifier.boutonEnvoi().clickable { appliquer(choix) }
                        .padding(horizontal = 18.dp, vertical = 9.dp),
                ) { Text(t("Appliquer"), color = P.surAccent, fontWeight = FontWeight.SemiBold, fontSize = 14.sp) }
            }
            Apercu(moodDe(choix), emoji)
            // La grille : « Par défaut », puis les moods, trois par rangée.
            val cases: List<Mood?> = if (ega) emptyList() else listOf<Mood?>(null) + MOODS
            for (rangee in cases.chunked(3)) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    for (m in rangee) {
                        CarteMood(m, choisi = m?.id == choix, Modifier.weight(1f)) {
                            // Toucher la carte déjà choisie : appliquer et fermer.
                            if (m?.id == choix) appliquer(choix) else choix = m?.id
                        }
                    }
                    repeat(3 - rangee.size) { Spacer(Modifier.weight(1f)) }
                }
            }
            RangeeEmojiRapide(emoji, choisir = { emoji = it }, autre = { autre = true })
        }
    }
}

/** La rangée « Emoji rapide » : Aucun, les emojis courants (et celui choisi ailleurs), Autre…. */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun RangeeEmojiRapide(emoji: String?, choisir: (String?) -> Unit, autre: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(t("Emoji rapide"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
        Text(
            t("Un bouton à droite de la zone d'écriture l'envoie d'un appui quand le champ est vide."),
            color = P.textePale, fontSize = 13.sp,
        )
        @Composable
        fun Pastille(choisi: Boolean, toucher: () -> Unit, contenu: @Composable () -> Unit) {
            val bord by animateColorAsState(
                if (choisi) P.accent else Color.Transparent, if (P.sec) snap() else spring(), label = "bord-emoji",
            )
            Box(
                Modifier.height(44.dp).widthIn(min = 44.dp).clip(formeRonde)
                    .let { if (choisi) it.background(P.surface) else it }
                    .border(2.dp, bord, formeRonde)
                    .clickable(onClick = toucher).padding(horizontal = 6.dp),
                contentAlignment = Alignment.Center,
            ) { contenu() }
        }
        val liste = if (emoji != null && emoji !in EMOJIS_RAPIDES) EMOJIS_RAPIDES + emoji else EMOJIS_RAPIDES
        androidx.compose.foundation.layout.FlowRow(
            horizontalArrangement = Arrangement.spacedBy(4.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Pastille(emoji == null, { choisir(null) }) {
                Text(t("Aucun"), color = P.texteFort, fontSize = 14.sp, modifier = Modifier.padding(horizontal = 8.dp))
            }
            for (e in liste) Pastille(emoji == e, { choisir(e) }) { EmojiCentre(e, 24f) }
            Pastille(false, autre) {
                Text(t("Autre…"), color = P.texteFort, fontSize = 14.sp, modifier = Modifier.padding(horizontal = 8.dp))
            }
        }
    }
}

/** La saisie en miniature : le bouton de l'emoji rapide à droite (champ vide), sinon celui d'envoi. */
@Composable
private fun MiniSaisie(emoji: String?) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(36.dp).boutonTheme(18.dp), contentAlignment = Alignment.Center) {
            Icon(Icons.Filled.Add, null, tint = P.texteFort, modifier = Modifier.size(18.dp))
        }
        Spacer(Modifier.width(6.dp))
        Box(
            Modifier.weight(1f).height(36.dp).carte(18.dp).padding(horizontal = 12.dp),
            contentAlignment = Alignment.CenterStart,
        ) { Text("Message", color = P.textePale, fontSize = 14.sp) }
        Spacer(Modifier.width(6.dp))
        if (emoji != null) {
            Box(Modifier.size(36.dp).let { if (P.retro) it.boutonTheme(18.dp) else it.carte(18.dp, bord = P.accent) }, contentAlignment = Alignment.Center) {
                EmojiCentre(emoji, 19f)
            }
        } else {
            Box(Modifier.size(36.dp).boutonEnvoi(), contentAlignment = Alignment.Center) {
                Icon(Icons.AutoMirrored.Filled.Send, null, tint = P.surAccent, modifier = Modifier.size(17.dp))
            }
        }
    }
}

/** L'aperçu en direct : le fond du mood (ou du thème), trois bulles d'exemple et la saisie. */
@Composable
private fun Apercu(m: Mood?, emoji: String?) {
    val clair = P.clair
    val forme = forme(22.dp)
    if (P.ega) {
        Box(Modifier.fillMaxWidth().border(1.dp, P.traitFin, forme).padding(12.dp)) { MiniSaisie(emoji) }
        return
    }
    Box(
        Modifier.fillMaxWidth().height(246.dp).clip(forme)
            .let { if (m != null) it.fondMood(m, clair) else it.background(if (clair && !P.retro) P.fond else P.fil) }
            .let {
                when {
                    P.sirene -> it.relief(enfonce = true)
                    P.live -> it.border(1.dp, P.bordCarte, forme)
                    else -> it.border(1.dp, if (clair) Color.White.copy(alpha = 0.7f) else P.traitFin, forme)
                }
            }
            .padding(14.dp),
    ) {
        if (P.retro) {
            // Sans bulles, comme le fil de Live et Sirène.
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                LigneExemple("Camille", moi = false, t("Tu viens au chalet vendredi ?"), 15.sp, "19:24")
                LigneExemple(null, moi = true, t("Oui ! J'apporte les guimauves 🔥"), 15.sp, "19:25")
                LigneExemple("Camille", moi = false, t("Parfait, on t'attend !"), 15.sp, "19:26")
                Spacer(Modifier.weight(1f))
                MiniSaisie(emoji)
            }
            return@Box
        }
        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            BulleExemple(t("Tu viens au chalet vendredi ?"), moi = false, m, 15.sp)
            BulleExemple(t("Oui ! J'apporte les guimauves 🔥"), moi = true, m, 15.sp)
            BulleExemple(t("Parfait, on t'attend !"), moi = false, m, 15.sp)
            Spacer(Modifier.height(4.dp))
            MiniSaisie(emoji)
        }
    }
}

@Composable
private fun BulleExemple(texte: String, moi: Boolean, m: Mood?, taille: androidx.compose.ui.unit.TextUnit, petite: Boolean = false) {
    val clair = P.clair
    if (P.retro && petite) {
        LigneMiniature(moi)
        return
    }
    val r = if (petite) 7.dp else 18.dp
    val pointe = if (petite) 2.dp else 4.dp
    val forme = if (moi) formeBulle(r, r, pointe, r) else formeBulle(r, r, r, pointe)
    Row(Modifier.fillMaxWidth(), horizontalArrangement = if (moi) Arrangement.End else Arrangement.Start) {
        Box(
            Modifier.widthIn(max = if (petite) 60.dp else 240.dp).clip(forme)
                .let {
                    when {
                        moi && m != null -> it.background(m.envoye)
                        moi -> it.background(P.degrade)
                        m != null -> it.background(m.recue(clair))
                        else -> it.background(P.surface)
                    }
                }
                .let { if (!moi && clair && P.bordBulle.alpha > 0f) it.border(1.dp, P.bordBulle, forme) else it }
                .padding(horizontal = if (petite) 0.dp else 12.dp, vertical = if (petite) 0.dp else 7.dp)
                .let { if (petite) it.width(if (moi) 34.dp else 42.dp).height(11.dp) else it },
        ) {
            if (!petite) {
                Text(
                    texte, fontSize = taille,
                    color = when {
                        moi && m != null -> m.texteEnvoye
                        moi -> P.surAccent
                        m != null -> m.texteRecu(clair)
                        else -> P.texteFort
                    },
                )
            }
        }
    }
}

/** Une carte de la grille : le fond en miniature, une bulle reçue et une envoyée, le nom dessous. */
@Composable
private fun CarteMood(m: Mood?, choisi: Boolean, modifier: Modifier, toucher: () -> Unit) {
    val clair = P.clair
    val forme = forme(18.dp)
    val bord by animateColorAsState(
        if (choisi) P.accent else Color.Transparent, if (P.sec) snap() else spring(), label = "bord-mood",
    )
    Column(modifier.clip(forme(20.dp)).clickable(onClick = toucher).padding(4.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Box(
            Modifier.fillMaxWidth().aspectRatio(0.82f).border(2.5.dp, bord, forme).padding(4.dp).clip(forme(14.dp))
                .let { if (m != null) it.fondMood(m, clair) else it.background(if (clair && !P.retro) P.fond else P.fil) }
                .let { if (m == null || P.retro) it.border(1.dp, P.traitFin, forme(14.dp)) else it }
                .padding(horizontal = 8.dp, vertical = 10.dp),
        ) {
            Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(5.dp)) {
                BulleExemple("", moi = false, m, 1.sp, petite = true)
                BulleExemple("", moi = true, m, 1.sp, petite = true)
            }
            if (choisi) {
                Box(
                    Modifier.align(Alignment.TopEnd).size(20.dp).clip(CircleShape).background(P.accent),
                    contentAlignment = Alignment.Center,
                ) { Icon(Icons.Filled.Check, null, tint = Color.White, modifier = Modifier.size(13.dp)) }
            }
        }
        Text(
            m?.nom ?: t("Par défaut"), color = if (choisi) P.texteFort else P.texte, fontSize = 12.5.sp,
            fontWeight = if (choisi) FontWeight.SemiBold else FontWeight.Normal,
            maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center,
            modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
        )
    }
}
