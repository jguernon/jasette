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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Conv
import ca.jasette.android.Langue
import ca.jasette.android.Marques
import ca.jasette.android.Spam
import ca.jasette.android.aViderSpam
import ca.jasette.android.nomAffiche
import ca.jasette.android.peutSupprimer
import ca.jasette.android.stopOffert
import ca.jasette.android.t
import ca.jasette.android.texteSuppression

/*
 * Le spam côté interface (Spam.kt) : l'entrée de menu, la fenêtre de
 * confirmation (avec « envoyer STOP » pour un SMS à deux) et Réglages > Spam.
 */

/** L'entrée de menu : « Ce n'est pas du spam » pour une conversation marquée, sinon « Marquer comme spam ». */
fun texteSpam(id: String): String = if (Marques.estSpam(id)) t("Ce n'est pas du spam") else t("Marquer comme spam")

fun iconeSpam(id: String): ImageVector = if (Marques.estSpam(id)) Icons.Filled.CheckCircle else Icones.spam

/** Le geste de l'entrée : « Ce n'est pas du spam » tout de suite, sinon la confirmation ([ouvrir]). */
fun basculerSpam(id: String, ouvrir: () -> Unit) {
    if (Marques.estSpam(id)) Marques.pasSpam(id) else ouvrir()
}

/**
 * « Marquer comme spam » : la confirmation, avec « Marquer comme spam et
 * envoyer STOP » pour une conversation Google Messages à deux. Rien n'est
 * envoyé sans ce toucher-là. [marquee] : après le marquage (la conversation
 * ouverte se referme).
 */
@Composable
fun FenetreSpam(c: Conv, fermer: () -> Unit, marquee: () -> Unit = {}) {
    @Composable
    fun Choix(icone: ImageVector, titre: String, detail: String?, couleur: Color, action: () -> Unit) {
        Row(
            Modifier.fillMaxWidth().clip(forme(12.dp)).clickable(onClick = action).padding(horizontal = 8.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(34.dp).clip(formeRonde).border(1.dp, P.traitFin, formeRonde),
                contentAlignment = Alignment.Center,
            ) { Icon(icone, null, tint = couleur, modifier = Modifier.size(18.dp)) }
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(titre, color = P.texteFort, fontWeight = FontWeight.SemiBold)
                if (detail != null) Text(detail, color = P.textePale, fontSize = 12.sp)
            }
        }
    }
    AlertDialog(
        onDismissRequest = fermer,
        containerColor = P.menu,
        title = {
            Column {
                Text(t("Marquer comme spam"), color = P.texteFort, fontWeight = FontWeight.Bold)
                Text(nomAffiche(c), color = P.textePale, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    t("Elle quitte la boîte de réception, ne notifie plus et va dans Réglages > Spam."),
                    color = P.textePale, fontSize = 13.sp, modifier = Modifier.padding(start = 8.dp, end = 8.dp, bottom = 6.dp),
                )
                Choix(Icones.spam, t("Marquer comme spam"), null, P.texte) {
                    Spam.marquer(c, stop = false)
                    fermer()
                    marquee()
                }
                if (stopOffert(c)) {
                    Choix(Icones.bloquer, t("Marquer comme spam et envoyer STOP"), t("Envoie le message STOP à ce numéro pour te désinscrire."), P.texte) {
                        Spam.marquer(c, stop = true)
                        fermer()
                        marquee()
                    }
                }
            }
        },
        confirmButton = { TextButton(shape = formeBouton, onClick = fermer) { Text(t("Annuler"), color = P.texte) } },
    )
}

/**
 * Réglages > Spam : le dossier. Chaque conversation (avatar, nom, aperçu,
 * date du marquage) s'ouvre d'un toucher ([ouvrir]) ; « Ce n'est pas du
 * spam » ; « Supprimer » (la confirmation de « Supprimer la conversation »),
 * là où le service le permet ; « Tout vider », avec le nombre.
 */
@Composable
fun SectionSpam(ouvrir: (String) -> Unit) {
    val liste = Spam.liste
    var aSupprimer by remember { mutableStateOf<Conv?>(null) }
    var vider by remember { mutableStateOf(false) }
    if (liste.isEmpty()) {
        Groupe {
            Text(
                t("Aucune conversation marquée comme spam. « Marquer comme spam » est dans le menu d'une conversation, sous « Classer… »."),
                color = P.textePale, fontSize = 14.sp, modifier = Modifier.padding(16.dp),
            )
        }
        return
    }
    val (oui, non) = aViderSpam(liste)
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            if (Langue.anglais) "${liste.size} conversation(s) · no notifications, out of the inbox"
            else "${liste.size} conversation(s) · sans notification, hors de la boîte",
            color = P.textePale, fontSize = 13.sp, modifier = Modifier.weight(1f),
        )
        if (oui.isNotEmpty()) {
            TextButton(shape = formeBouton, onClick = { vider = true }) { Text(t("Tout vider"), color = P.danger) }
        }
    }
    Groupe {
        liste.forEachIndexed { i, c ->
            if (i > 0) HorizontalDivider(Modifier.padding(start = 70.dp), color = P.traitFin)
            Column(Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 10.dp)) {
                Row(
                    Modifier.fillMaxWidth().clip(forme(12.dp)).clickable { ouvrir(c.id) },
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Avatar(c, 44.dp, badge = true)
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text(
                            nomAffiche(c), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
                            maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                        val quand = Marques.spam[c.id] ?: 0L
                        val marque = if (quand > 0) (if (Langue.anglais) "Marked ${heureListe(quand)}" else "Marquée ${heureListe(quand)}") else ""
                        Text(
                            listOf(marque, c.preview).filter { it.isNotEmpty() }.joinToString(" · "),
                            color = P.textePale, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                Row(Modifier.fillMaxWidth().padding(start = 50.dp), horizontalArrangement = Arrangement.End) {
                    TextButton(shape = formeBouton, onClick = { Marques.pasSpam(c.id) }) {
                        Icon(Icons.Filled.CheckCircle, null, tint = P.accent, modifier = Modifier.size(18.dp))
                        Spacer(Modifier.width(6.dp))
                        Text(t("Ce n'est pas du spam"), color = P.accent)
                    }
                    if (c.peutSupprimer) {
                        TextButton(shape = formeBouton, onClick = { aSupprimer = c }) {
                            Icon(Icons.Filled.Delete, null, tint = P.danger, modifier = Modifier.size(18.dp))
                            Spacer(Modifier.width(6.dp))
                            Text(t("Supprimer"), color = P.danger)
                        }
                    }
                }
            }
        }
    }
    if (non.isNotEmpty()) {
        Text(
            if (Langue.anglais) "${non.size} conversation(s) can't be deleted from Jasette for their service: they stay here, silent. Delete them in the service's app if needed."
            else "${non.size} conversation(s) ne peuvent pas être supprimée(s) depuis Jasette pour leur service : elles restent ici, sans bruit. Supprime-les dans l'app du service au besoin.",
            color = P.textePale, fontSize = 12.sp, modifier = Modifier.padding(horizontal = 6.dp),
        )
    }

    // La même confirmation que « Supprimer la conversation » (MenuConversation).
    aSupprimer?.let { c ->
        AlertDialog(
            onDismissRequest = { aSupprimer = null },
            containerColor = P.menu,
            title = { Text(t("Supprimer la conversation ?"), color = P.texteFort) },
            text = { Text(c.texteSuppression, color = P.texte) },
            confirmButton = {
                TextButton(shape = formeBouton, onClick = {
                    aSupprimer = null
                    ca.jasette.android.Depot.supprimer(c.id)
                }) { Text(t("Supprimer"), color = P.danger) }
            },
            dismissButton = { TextButton(shape = formeBouton, onClick = { aSupprimer = null }) { Text(t("Annuler"), color = P.texte) } },
        )
    }
    if (vider) {
        AlertDialog(
            onDismissRequest = { vider = false },
            containerColor = P.menu,
            title = {
                Text(
                    if (Langue.anglais) "Delete ${oui.size} conversation(s)?" else "Supprimer ${oui.size} conversation(s) ?",
                    color = P.texteFort,
                )
            },
            text = {
                Text(
                    (if (Langue.anglais) "They will be deleted from their service (Google Messages, Messenger, Instagram), on all your devices. This is permanent."
                    else "Elles seront supprimées de leur service (Google Messages, Messenger, Instagram), sur tous tes appareils. C'est définitif.") +
                        if (non.isEmpty()) "" else if (Langue.anglais) "\n\n${non.size} other(s) stay in Spam: their service can't delete from Jasette."
                        else "\n\n${non.size} autre(s) reste(nt) dans Spam : leur service ne permet pas de supprimer depuis Jasette.",
                    color = P.texte,
                )
            },
            confirmButton = {
                TextButton(shape = formeBouton, onClick = {
                    vider = false
                    Spam.vider()
                }) { Text(t("Tout vider"), color = P.danger) }
            },
            dismissButton = { TextButton(shape = formeBouton, onClick = { vider = false }) { Text(t("Annuler"), color = P.texte) } },
        )
    }
}
