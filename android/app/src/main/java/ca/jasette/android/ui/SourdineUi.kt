package ca.jasette.android.ui

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
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.DureeSourdine
import ca.jasette.android.Marques
import ca.jasette.android.t
import kotlinx.coroutines.delay

/*
 * La sourdine côté interface (Sourdine.kt) : l'entrée de menu, la fenêtre des
 * durées et la cloche barrée.
 */

/** L'entrée de menu pour [ids] : « Réactiver les notifications » si l'une est en sourdine, sinon « Mettre en sourdine… ». */
fun texteSourdine(ids: Collection<String>): String =
    if (ids.any { Marques.enSourdine(it) }) t("Réactiver les notifications") else t("Mettre en sourdine…")

/**
 * Le geste de cette entrée : réactive tout de suite, ou demande d'ouvrir la
 * fenêtre des durées ([ouvrir]).
 */
fun basculerSourdine(ids: Collection<String>, ouvrir: () -> Unit) {
    if (ids.any { Marques.enSourdine(it) }) Marques.poserSourdine(ids, null) else ouvrir()
}

/**
 * La cloche barrée d'une conversation en sourdine (rien sinon). À l'échéance,
 * la sourdine échue s'efface d'elle-même, et l'icône avec elle.
 */
@Composable
fun IconeSourdine(id: String, taille: Dp, teinte: Color = P.textePale, modifier: Modifier = Modifier) {
    val echeance = Marques.sourdine[id] ?: return
    if (!Marques.enSourdine(id)) return
    if (echeance > 0) {
        LaunchedEffect(id, echeance) {
            delay((echeance - System.currentTimeMillis()).coerceAtLeast(0L) + 500L)
            Marques.elaguerSourdine()
        }
    }
    Icon(Icones.sourdine, t("En sourdine"), tint = teinte, modifier = modifier.size(taille))
}

/**
 * « Mettre en sourdine » : 1 heure, 8 heures, 24 heures, 1 semaine ou
 * « Jusqu'à ce que je la réactive ». Un appui pose la sourdine de [ids]
 * (marque partagée `sourdine`) et ferme.
 */
@Composable
fun FenetreSourdine(ids: Collection<String>, nom: String, fermer: () -> Unit) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = fermer,
        containerColor = P.menu,
        title = {
            Column {
                Text(t("Mettre en sourdine"), color = P.texteFort, fontWeight = FontWeight.Bold)
                Text(nom, color = P.textePale, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    t("Les messages arrivent comme d'habitude, sans notification."),
                    color = P.textePale, fontSize = 13.sp, modifier = Modifier.padding(start = 8.dp, end = 8.dp, bottom = 6.dp),
                )
                for (d in DureeSourdine.entries) {
                    Row(
                        Modifier.fillMaxWidth().clip(forme(12.dp))
                            .clickable {
                                Marques.poserSourdine(ids, d.echeance(System.currentTimeMillis()))
                                fermer()
                            }
                            .padding(horizontal = 8.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            Modifier.size(34.dp).clip(formeRonde).border(1.dp, P.traitFin, formeRonde),
                            contentAlignment = Alignment.Center,
                        ) {
                            Icon(
                                if (d == DureeSourdine.TOUJOURS) Icones.sourdine else Icones.horloge, null,
                                tint = P.texte, modifier = Modifier.size(18.dp),
                            )
                        }
                        Spacer(Modifier.width(12.dp))
                        Text(t(d.libelle), color = P.texteFort, fontWeight = FontWeight.SemiBold)
                    }
                }
            }
        },
        confirmButton = { TextButton(shape = formeBouton, onClick = fermer) { Text(t("Annuler"), color = P.texte) } },
    )
}
