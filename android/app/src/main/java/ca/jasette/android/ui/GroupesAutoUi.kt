package ca.jasette.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.GroupeAuto
import ca.jasette.android.dernierCode
import ca.jasette.android.nomAffiche
import ca.jasette.android.t

/** Le grand emoji d'un groupe, en guise d'avatar : la taille de l'avatar normal, sur un fond doux. */
@Composable
fun AvatarGroupe(g: GroupeAuto, taille: Dp) {
    val fond = Color(0xFF000000L or g.fond)
    Box(
        Modifier.size(taille).clip(formeRonde).background(fond.copy(alpha = if (P.clair) 0.20f else 0.32f)),
        contentAlignment = Alignment.Center,
    ) {
        // L'emoji occupe ~60 % du rond.
        val px = with(LocalDensity.current) { (taille * echelleEmoji(g)).toSp() }
        Text(g.emoji, fontSize = px, lineHeight = px)
    }
}

/** Le titre d'une rangée de groupe : « Codes · 3 nouveaux », « Promotions · 12 ». */
fun titreGroupe(g: GroupeAuto, membres: List<Conv>): String {
    val nom = t(g.titre)
    if (g == GroupeAuto.PROMOS) return "$nom · ${membres.size}"
    val n = membres.count { it.unread }
    return when (n) {
        0 -> nom
        1 -> "$nom · 1 ${t("nouveau")}"
        else -> "$nom · $n ${t("nouveaux")}"
    }
}

/** L'aperçu d'une rangée de groupe (Codes : « Google : 482913 » ; Promotions : les noms). */
fun apercuGroupe(g: GroupeAuto, membres: List<Conv>): String {
    val recent = membres.maxByOrNull { it.time } ?: return ""
    return when (g) {
        GroupeAuto.CODES -> dernierCode(membres)?.let { (c, code) -> "${nomAffiche(c)}${t(" : ")}$code" }
            ?: "${nomAffiche(recent)}${t(" : ")}${recent.preview}"
        GroupeAuto.PROMOS -> membres.sortedByDescending { it.time }.joinToString(", ") { nomAffiche(it) }
        GroupeAuto.AVIS -> "${nomAffiche(recent)}${t(" : ")}${ca.jasette.android.traduirePont(recent.preview)}"
    }.replace('\n', ' ')
}

/** La rangée d'un groupe dans la boîte de réception ; la toucher l'ouvre. */
@Composable
fun RangeeGroupe(g: GroupeAuto, membres: List<Conv>, ouvrir: () -> Unit) {
    val nonLue = g != GroupeAuto.PROMOS && membres.any { it.unread }
    val temps = membres.maxOfOrNull { it.time } ?: 0L
    Row(
        Modifier.fillMaxWidth().clickable(onClick = ouvrir).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        AvatarGroupe(g, 52.dp)
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    titreGroupe(g, membres), color = P.texteFort,
                    fontWeight = if (nonLue) FontWeight.Bold else FontWeight.SemiBold,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f), fontSize = Taille.liste(16f),
                )
                Spacer(Modifier.width(6.dp))
                Text(heureListe(temps), color = if (nonLue) P.accent else P.textePale, fontSize = Taille.liste(12f))
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    apercuGroupe(g, membres),
                    color = if (nonLue) P.texteFort else P.apercu, fontSize = Taille.liste(14f),
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                )
                if (g == GroupeAuto.CODES) dernierCode(membres)?.let { (_, code) ->
                    Spacer(Modifier.width(8.dp))
                    BoutonCopierCode(code)
                }
                if (nonLue) {
                    Spacer(Modifier.width(8.dp))
                    Box(Modifier.size(10.dp).clip(formeRonde).background(P.accent))
                }
            }
        }
    }
}

/** « Copier » le dernier code, directement sur la rangée Codes (« Copié » ensuite). */
@Composable
private fun BoutonCopierCode(code: String) {
    val pressePapiers = LocalClipboardManager.current
    var copie by remember(code) { mutableStateOf(false) }
    LaunchedEffect(copie) {
        if (copie) {
            kotlinx.coroutines.delay(1600)
            copie = false
        }
    }
    val teinte = if (copie) P.succes else P.texte
    Row(
        Modifier.clip(forme(999.dp))
            .background(P.surface)
            .border(1.dp, P.traitFin, forme(999.dp))
            .clickable {
                pressePapiers.setText(AnnotatedString(code))
                copie = true
            }
            .padding(horizontal = 10.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Icon(if (copie) Icons.Filled.Check else Icones.copier, null, tint = teinte, modifier = Modifier.size(14.dp))
        Text(if (copie) t("Copié") else t("Copier"), color = teinte, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
    }
}

/** L'en-tête d'un groupe ouvert : retour, son nom, et « Tout marquer comme lu » (action explicite). */
@Composable
fun EnteteGroupe(g: GroupeAuto, membres: List<Conv>, fermer: () -> Unit) {
    val nonLues = membres.filter { it.unread }.map { it.id }
    Row(
        Modifier.fillMaxWidth().padding(start = 4.dp, end = 8.dp, top = 2.dp, bottom = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = fermer) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour"), tint = P.texteFort) }
        Text(
            "${g.emoji} ${t(g.titre)}", color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 18.sp,
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
        )
        TextButton(onClick = { Depot.marquerTousLus(nonLues) }, enabled = nonLues.isNotEmpty(), shape = formeBouton) {
            Text(t("Tout marquer comme lu"), color = if (nonLues.isNotEmpty()) P.accent else P.textePale)
        }
    }
}

/**
 * La taille d'un emoji de groupe, en part de l'avatar. Les glyphes de la police
 * d'emoji n'ont pas les mêmes marges : le cadenas 🔐 remplit presque tout son
 * carré, le colis et les étiquettes beaucoup moins. On les égalise à l'œil.
 */
private fun echelleEmoji(g: GroupeAuto): Float = when (g) {
    GroupeAuto.CODES -> 0.5f
    else -> 0.6f
}
