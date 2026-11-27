package ca.jasette.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.offset
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
import androidx.compose.material.icons.filled.Clear
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
import ca.jasette.android.Marques
import ca.jasette.android.GroupeAuto
import ca.jasette.android.dernierCode
import ca.jasette.android.nomAffiche
import ca.jasette.android.t

/** L'icône d'un groupe (glyphe Material, comme les tuiles des Réglages). */
fun iconeGroupe(g: GroupeAuto): androidx.compose.ui.graphics.vector.ImageVector = when (g) {
    GroupeAuto.CODES -> Icones.cle
    GroupeAuto.PROMOS -> Icones.etiquette
    GroupeAuto.RDV -> Icones.calendrier
    GroupeAuto.ARGENT -> Icones.billets
    GroupeAuto.AVIS -> Icones.colis
}

/** L'avatar d'un groupe : un rond de sa couleur, avec son icône blanche. */
@Composable
fun AvatarGroupe(g: GroupeAuto, taille: Dp) {
    Box(
        Modifier.size(taille).clip(formeRonde).background(Color(0xFF000000L or if (P.ega) g.fondEga else g.fond)),
        contentAlignment = Alignment.Center,
    ) {
        Icon(iconeGroupe(g), null, tint = Color.White, modifier = Modifier.size(taille * 0.52f))
    }
}

/** Le titre d'une rangée de groupe : le nom seul ; les non-lus sont dans la pastille de l'avatar. */
@Suppress("UNUSED_PARAMETER")
fun titreGroupe(g: GroupeAuto, membres: List<Conv>): String = t(g.titre)

/** L'aperçu d'une rangée de groupe (Codes : « Google : 482913 » ; Promotions : les noms). */
fun apercuGroupe(g: GroupeAuto, membres: List<Conv>): String {
    val recent = membres.maxByOrNull { it.time } ?: return ""
    return when (g) {
        GroupeAuto.CODES -> dernierCode(membres)?.let { (c, code) -> "${nomAffiche(c)}${t(" : ")}$code" }
            ?: "${nomAffiche(recent)}${t(" : ")}${recent.preview}"
        GroupeAuto.PROMOS -> membres.sortedByDescending { it.time }.joinToString(", ") { nomAffiche(it) }
        GroupeAuto.RDV, GroupeAuto.ARGENT, GroupeAuto.AVIS -> "${nomAffiche(recent)}${t(" : ")}${ca.jasette.android.traduirePont(recent.preview)}"
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
        val nonLus = membres.count { it.unread }
        Box {
            AvatarGroupe(g, 52.dp)
            // La pastille des non-lus, comme celles de la barre de navigation.
            if (nonLus > 0) Box(Modifier.align(Alignment.TopEnd).offset(x = 3.dp, y = (-3).dp)) {
                Pastille(if (nonLus > 99) "99+" else nonLus.toString())
            }
        }
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
        AvatarGroupe(g, 30.dp)
        Spacer(Modifier.width(10.dp))
        Text(
            t(g.titre), color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 18.sp,
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
        )
        TextButton(onClick = { Depot.marquerTousLus(nonLues) }, enabled = nonLues.isNotEmpty(), shape = formeBouton) {
            Text(t("Tout marquer comme lu"), color = if (nonLues.isNotEmpty()) P.accent else P.textePale)
        }
    }
}

/**
 * « Classer… » (menu d'appui long d'une conversation) : les cinq groupes, le
 * groupe actuel coché, et « Ne pas regrouper ». Un appui corrige à la main
 * (marque partagée `groupes_auto`, comme « Ce n'est pas … ») et ferme.
 */
@Composable
fun FenetreClasser(c: Conv, fermer: () -> Unit) {
    val actuel = ca.jasette.android.RegroupementAuto.groupe(c)
    androidx.compose.material3.AlertDialog(
        onDismissRequest = fermer,
        containerColor = P.menu,
        title = {
            Column {
                Text(if (actuel != null) t("Reclasser") else t("Classer"), color = P.texteFort, fontWeight = FontWeight.Bold)
                Text(nomAffiche(c), color = P.textePale, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                for (g in GroupeAuto.entries + listOf<GroupeAuto?>(null)) {
                    val coche = g != null && g == actuel
                    val texte = if (g != null) t(g.nomLong) else t("Ne pas regrouper")
                    val precision = if (g != null) g.precision?.let { t(it) } else actuel?.let { t(it.pasUn) }
                    Row(
                        Modifier.fillMaxWidth().clip(forme(12.dp))
                            .then(if (coche) Modifier.background(P.choisi) else Modifier)
                            .clickable {
                                Marques.corrigerGroupe(c.id, g?.cle ?: "aucun")
                                fermer()
                            }
                            .padding(horizontal = 8.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        if (g != null) AvatarGroupe(g, 34.dp)
                        else Box(
                            Modifier.size(34.dp).clip(formeRonde).border(1.dp, P.traitFin, formeRonde),
                            contentAlignment = Alignment.Center,
                        ) { Icon(Icons.Filled.Clear, null, tint = P.textePale, modifier = Modifier.size(18.dp)) }
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f)) {
                            Text(texte, color = P.texteFort, fontWeight = FontWeight.SemiBold)
                            if (precision != null) Text(precision, color = P.textePale, fontSize = 12.sp)
                        }
                        if (coche) Icon(Icons.Filled.Check, null, tint = P.accent, modifier = Modifier.size(20.dp))
                    }
                }
            }
        },
        confirmButton = { TextButton(shape = formeBouton, onClick = fermer) { Text(t("Annuler"), color = P.texte) } },
    )
}
