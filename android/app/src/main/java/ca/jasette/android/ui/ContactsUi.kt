package ca.jasette.android.ui

import android.Manifest
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Contacts
import ca.jasette.android.Service
import ca.jasette.android.t

/**
 * La demande d'accès aux contacts : une courte explication, puis la boîte du
 * système. Rend la fonction qui la lance ; [resultat] reçoit la réponse
 * (vrai tout de suite si l'accès est déjà accordé).
 */
@Composable
fun rememberDemandeContacts(resultat: (Boolean) -> Unit = {}): () -> Unit {
    val ctx = LocalContext.current
    val rappel by rememberUpdatedState(resultat)
    var expliquer by remember { mutableStateOf(false) }
    val lanceur = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        Contacts.apresDemande(ok)
        rappel(ok)
    }
    if (expliquer) {
        ExplicationContacts(
            continuer = {
                expliquer = false
                lanceur.launch(Manifest.permission.READ_CONTACTS)
            },
            annuler = {
                expliquer = false
                rappel(false)
            },
        )
    }
    return {
        Contacts.demarrer(ctx)
        if (Contacts.permis) rappel(true) else expliquer = true
    }
}

/** L'explication montrée avant la boîte du système. */
@Composable
private fun ExplicationContacts(continuer: () -> Unit, annuler: () -> Unit) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = annuler,
        containerColor = P.menu,
        shape = forme(20.dp),
        title = { Text(t("Accès aux contacts"), color = P.texteFort) },
        text = {
            Text(
                t("Jasette peut montrer les photos de tes contacts et les proposer dans « Nouveau message ». Tes contacts restent sur ce téléphone : ils ne sont jamais envoyés au serveur ni aux services."),
                color = P.texte,
            )
        },
        confirmButton = {
            TextButton(shape = formeBouton, onClick = continuer) { Text(t("Continuer"), color = P.accent) }
        },
        dismissButton = {
            TextButton(shape = formeBouton, onClick = annuler) { Text(t("Plus tard"), color = P.texteFort) }
        },
    )
}

/** Ouvre la page de Jasette dans les paramètres du système (pour accorder l'accès refusé). */
fun ouvrirParametresApp(ctx: Context) {
    val i = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", ctx.packageName, null))
        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    runCatching { ctx.startActivity(i) }
}

/**
 * Réglages → Apparence : « Utiliser les photos de mes contacts » et, en
 * dessous, « Aussi pour Messenger et Instagram (par le nom) ».
 */
@Composable
fun ChoixContacts() {
    val ctx = LocalContext.current
    val demander = rememberDemandeContacts { ok -> if (ok) Contacts.choisirPhotos(true) }
    val photos = Contacts.photosActives
    val meta = Contacts.metaActives
    Groupe {
        LigneInterrupteur(
            t("Utiliser les photos de mes contacts"),
            t("La photo d'un contact du téléphone remplace celle de Google Messages, par le numéro."),
            photos,
        ) { oui ->
            if (!oui) Contacts.choisirPhotos(false)
            else if (Contacts.permis) Contacts.choisirPhotos(true)
            else demander()
        }
        HorizontalDivider(color = P.filet)
        Box(Modifier.alpha(if (photos) 1f else 0.45f)) {
            LigneInterrupteur(
                t("Aussi pour Messenger et Instagram (par le nom)"),
                t("Seulement quand un seul contact porte exactement le nom de la conversation."),
                meta,
                actif = photos,
            ) { Contacts.choisirMeta(it) }
        }
        if (Contacts.refusee) {
            HorizontalDivider(color = P.filet)
            Row(
                Modifier.fillMaxWidth().padding(start = 16.dp, end = 8.dp, top = 6.dp, bottom = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.size(8.dp).clip(formeRonde).background(P.danger))
                Spacer(Modifier.width(10.dp))
                Text(t("Accès aux contacts refusé"), color = P.texteFort, fontSize = 14.sp, modifier = Modifier.weight(1f))
                TextButton(shape = formeBouton, onClick = { ouvrirParametresApp(ctx) }) {
                    Text(t("Paramètres"), color = P.accent)
                }
            }
        }
    }
}

@Composable
private fun LigneInterrupteur(
    titre: String,
    detail: String,
    oui: Boolean,
    actif: Boolean = true,
    changer: (Boolean) -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().clickable(enabled = actif) { changer(!oui) }
            .padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
            Text(detail, color = P.textePale, fontSize = 13.sp)
        }
        Spacer(Modifier.width(12.dp))
        Interrupteur(oui) { if (actif) changer(it) }
    }
}

/** La photo d'un contact du téléphone, ronde, avec le badge Google Messages (ou le logo seul sans photo). */
@Composable
fun PhotoContact(photo: String?, taille: Dp) {
    if (photo == null) {
        androidx.compose.foundation.Image(
            painterResource(iconeService(Service.GOOGLE)), null,
            Modifier.size(taille).clip(formeLogo(taille)),
        )
        return
    }
    Box(Modifier.size(taille)) {
        ImageFichier(photo, Modifier.size(taille).clip(formeRonde).background(P.surface), cote = 256)
        val b = taille * 0.36f
        androidx.compose.foundation.Image(
            painterResource(iconeService(Service.GOOGLE)), null,
            Modifier.align(Alignment.BottomEnd).size(b).border(2.dp, P.fond, formeLogo(b))
                .padding(1.dp).clip(formeLogo(b)),
        )
    }
}
