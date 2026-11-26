package ca.jasette.android.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Depot
import ca.jasette.android.Service
import ca.jasette.android.t
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.common.BitMatrix
import com.google.zxing.qrcode.QRCodeWriter

/**
 * L'association de WhatsApp ou de Signal sur cet appareil (mode « Sur cet
 * appareil », pont embarqué) : PROTOCOLE.md §5.4.
 *
 * - WhatsApp : par défaut « Associer avec un numéro de téléphone »
 *   (`pair_phone`, puis `ev pair_code`, le code à taper dans WhatsApp) ; le
 *   code QR (`pair`, puis `ev qr`) reste offert, pour un autre appareil.
 * - Signal : le code QR seulement (appareil lié).
 *
 * [mode] : « qr » ouvre directement le code QR ; sinon, le numéro (WhatsApp).
 * L'écran se ferme tout seul quand le service est associé.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Association(service: Service, mode: String, fini: () -> Unit) {
    var parQr by rememberSaveable { mutableStateOf(service == Service.SIGNAL || mode == "qr") }
    val etat = Depot.etat(service)

    // Associé : le pont passe à `paired` puis `ready`, on revient aux réglages.
    LaunchedEffect(etat) { if (etat.associe) fini() }

    Scaffold(
        containerColor = P.ecran,
        topBar = {
            BarreHaut(
                navigationIcon = { IconButton(onClick = fini) { Icon(Icons.Filled.Close, t("Fermer")) } },
                title = { Text(if (service == Service.SIGNAL) t("Lier Signal") else t("Associer WhatsApp"), fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        Column(
            Modifier.fillMaxSize().padding(marges).defilementAvecOmbre()
                .navigationBarsPadding().imePadding().padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(
                Modifier.widthIn(max = 520.dp).fillMaxWidth(),
                verticalArrangement = Arrangement.spacedBy(14.dp),
            ) {
                if (parQr) {
                    EtapeQr(service)
                    if (service == Service.WHATSAPP) {
                        TextButton(shape = formeBouton, onClick = { parQr = false }) {
                            Text(t("Associer plutôt avec un numéro de téléphone"), color = P.accent)
                        }
                    }
                } else {
                    EtapeTelephone()
                    TextButton(shape = formeBouton, onClick = { parQr = true }) {
                        Text(t("Afficher plutôt un code QR (pour un autre appareil)"), color = P.accent)
                    }
                }
                Depot.erreur?.let { Text(it, color = P.danger, fontSize = 14.sp) }
            }
        }
    }
}

/** Le code QR : `pair` au premier affichage, puis chaque `ev qr` le renouvelle. */
@Composable
internal fun EtapeQr(service: Service) {
    LaunchedEffect(service) { Depot.associerQr(service) }
    val code = Depot.qr[service]
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            if (service == Service.SIGNAL) {
                t("Scanne-le avec un autre téléphone qui a Signal (Paramètres → Appareils liés). Les anciens messages ne sont pas récupérés.")
            } else if (service == Service.TELEGRAM) {
                t("Scanne-le avec un autre appareil qui a Telegram : Paramètres → Appareils → Connecter un appareil.")
            } else if (service == Service.DISCORD) {
                t("Scanne-le avec un autre appareil qui a Discord : Paramètres → Scanner le code QR.")
            } else {
                t("Scanne-le avec le téléphone qui a WhatsApp : Appareils connectés → Connecter un appareil.")
            },
            color = P.texte, fontSize = 14.sp,
        )
        Box(
            // Toujours noir sur blanc, avec sa marge blanche : les lecteurs de QR l'exigent.
            Modifier.size(260.dp).clip(forme(12.dp)).background(Color.White).padding(14.dp),
            contentAlignment = Alignment.Center,
        ) {
            if (code == null) {
                CircularProgressIndicator(color = P.accent)
            } else {
                val matrice = remember(code) { matriceQr(code) }
                if (matrice != null) CodeQr(matrice, Modifier.fillMaxSize())
            }
        }
        Text(t("Le code se renouvelle tout seul."), color = P.textePale, fontSize = 12.sp)
    }
}

/** Le texte en matrice QR (zxing), ou null s'il ne s'encode pas. */
private fun matriceQr(texte: String): BitMatrix? = try {
    QRCodeWriter().encode(texte, BarcodeFormat.QR_CODE, 0, 0, mapOf(EncodeHintType.MARGIN to 0))
} catch (_: Exception) {
    null
}

/** Dessine la matrice : un carré noir par module. */
@Composable
private fun CodeQr(m: BitMatrix, modifier: Modifier) {
    Canvas(modifier) {
        val n = maxOf(m.width, m.height)
        val cote = minOf(size.width, size.height) / n
        val x0 = (size.width - cote * m.width) / 2
        val y0 = (size.height - cote * m.height) / 2
        for (y in 0 until m.height) for (x in 0 until m.width) {
            if (m.get(x, y)) {
                drawRect(Color.Black, Offset(x0 + x * cote, y0 + y * cote), Size(cote + 0.5f, cote + 0.5f))
            }
        }
    }
}

/** WhatsApp : le numéro (avec l'indicatif du pays), puis le code à taper dans WhatsApp. */
@Composable
private fun EtapeTelephone() {
    val ctx = LocalContext.current
    var numero by rememberSaveable { mutableStateOf("+1") }
    var demande by rememberSaveable { mutableStateOf(false) }
    val code = Depot.codeAssociation[Service.WHATSAPP]
    // Le numéro au format international : + et 8 à 15 chiffres.
    val chiffres = numero.count { it.isDigit() }
    val valide = numero.trim().startsWith("+") && chiffres in 8..15

    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(t("Associer avec un numéro de téléphone"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
        if (code == null) {
            Text(
                t("Entre le numéro du compte WhatsApp, avec l'indicatif du pays. WhatsApp te donnera un code à taper sur ce téléphone-là."),
                color = P.texte, fontSize = 14.sp,
            )
            OutlinedTextField(
                numero, { v -> numero = v.filter { it.isDigit() || it == '+' || it == ' ' || it == '-' }.take(22) },
                label = { Text(t("Numéro de téléphone")) },
                placeholder = { Text("+1 514 555 1234") },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone),
                colors = OutlinedTextFieldDefaults.colors(focusedBorderColor = P.accent, cursorColor = P.accent),
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Button(
                    onClick = {
                        demande = true
                        Depot.associerTelephone("+" + numero.filter { it.isDigit() })
                    },
                    enabled = valide && !demande,
                    shape = formeBouton,
                    colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                ) { Text(t("Obtenir le code")) }
                if (demande && Depot.erreur == null) {
                    Spacer(Modifier.size(12.dp))
                    CircularProgressIndicator(color = P.accent, modifier = Modifier.size(22.dp), strokeWidth = 2.dp)
                }
            }
            // Une erreur (numéro refusé, délai) : on peut redemander.
            if (demande && Depot.erreur != null) {
                TextButton(shape = formeBouton, onClick = { demande = false }) { Text(t("Réessayer"), color = P.accent) }
            }
        } else {
            Text(
                code, color = P.texteFort, fontSize = 40.sp, fontWeight = FontWeight.Bold,
                fontFamily = FontFamily.Monospace, letterSpacing = 3.sp, textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            )
            OutlinedButton(
                shape = formeBouton,
                onClick = { copier(ctx, code) },
                modifier = Modifier.align(Alignment.CenterHorizontally),
            ) { Text(t("Copier")) }
            Text(
                t("Dans WhatsApp : Appareils connectés → Connecter un appareil → Associer avec un numéro de téléphone"),
                color = P.texte, fontSize = 14.sp,
            )
            Text(t("Puis tape ce code. Le code expire après quelques minutes."), color = P.textePale, fontSize = 13.sp)
            Spacer(Modifier.height(2.dp))
            TextButton(shape = formeBouton, onClick = {
                demande = false
                Depot.codeAssociation.remove(Service.WHATSAPP)
            }) { Text(t("Changer de numéro"), color = P.textePale) }
        }
    }
}

private fun copier(ctx: Context, texte: String) {
    val cm = ctx.getSystemService(ClipboardManager::class.java) ?: return
    cm.setPrimaryClip(ClipData.newPlainText("WhatsApp", texte))
}
