package ca.jasette.android.ui

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import ca.jasette.android.Contacts
import ca.jasette.android.Depot
import ca.jasette.android.PontSms
import ca.jasette.android.t

/**
 * « Utiliser les SMS du téléphone » (Réglages, carte Google Messages, mode
 * autonome) : une courte explication, puis la boîte du système pour les SMS,
 * les MMS et les contacts (les noms). Lire et envoyer accordés : Google
 * Messages passe aux SMS du téléphone (Depot.utiliserSms, PontSms).
 */
@Composable
fun rememberDemandeSms(): () -> Unit {
    val ctx = LocalContext.current
    var expliquer by remember { mutableStateOf(false) }
    val lanceur = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { r ->
        val accorde = { p: String -> r[p] ?: (ctx.checkSelfPermission(p) == android.content.pm.PackageManager.PERMISSION_GRANTED) }
        if (r.containsKey(Manifest.permission.READ_CONTACTS)) {
            Contacts.demarrer(ctx)
            Contacts.apresDemande(accorde(Manifest.permission.READ_CONTACTS))
        }
        if (accorde(Manifest.permission.READ_SMS) && accorde(Manifest.permission.SEND_SMS)) {
            Depot.utiliserSms(ctx, true)
        } else {
            Depot.erreur = t("Sans l'accès aux SMS, Jasette ne peut pas lire ni envoyer tes messages texte. Tu peux l'accorder dans les paramètres d'Android (Jasette › Autorisations).")
        }
    }
    if (expliquer) {
        AlertDialog(
            onDismissRequest = { expliquer = false },
            containerColor = P.menu,
            shape = forme(20.dp),
            title = { Text(t("SMS du téléphone"), color = P.texteFort) },
            text = {
                Text(
                    t("Jasette va lire les SMS et MMS de ce téléphone pour les afficher, et envoyer tes réponses par SMS. Android va demander l'accès aux SMS et aux contacts (pour les noms). Rien ne quitte le téléphone."),
                    color = P.texte,
                )
            },
            confirmButton = {
                TextButton(shape = formeBouton, onClick = {
                    expliquer = false
                    lanceur.launch(PontSms.PERMISSIONS)
                }) { Text(t("Continuer"), color = P.accent) }
            },
            dismissButton = {
                TextButton(shape = formeBouton, onClick = { expliquer = false }) { Text(t("Plus tard"), color = P.texteFort) }
            },
        )
    }
    return { expliquer = true }
}
