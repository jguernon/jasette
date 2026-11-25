package ca.jasette.android.ui

import android.content.Intent
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Depot
import ca.jasette.android.Etat
import ca.jasette.android.PontService
import ca.jasette.android.Service

/** Les réglages : les comptes (connecter, déconnecter) et le NIP Messenger. */
@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun Reglages(retour: () -> Unit, connecter: (Service, String) -> Unit) {
    val ctx = LocalContext.current
    Scaffold(
        containerColor = P.fond,
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = retour) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Retour") } },
                title = { Text("Réglages", fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        Column(
            Modifier.fillMaxSize().padding(marges).verticalScroll(rememberScrollState()).navigationBarsPadding()
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text("Comptes", color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            for (s in Service.entries) {
                Carte(s) {
                    val etat = Depot.etat(s)
                    if (etat.associe || etat == Etat.ASSOCIATION) {
                        OutlinedButton(onClick = { Depot.deconnecter(s) }) { Text("Déconnecter", color = P.danger) }
                    } else {
                        when (s) {
                            Service.GOOGLE -> BoutonAccent("Se connecter") { connecter(s, "google") }
                            Service.MESSENGER -> {
                                BoutonAccent("Avec Messenger") { connecter(s, "messenger") }
                                OutlinedButton(onClick = { connecter(s, "facebook") }) { Text("Avec Facebook") }
                            }
                            Service.INSTAGRAM -> BoutonAccent("Se connecter") { connecter(s, "instagram") }
                        }
                    }
                }
                if (s == Service.GOOGLE) {
                    Depot.emoji?.let { e ->
                        Bandeau(P.accent) {
                            Text(e, fontSize = 40.sp)
                            Spacer(Modifier.width(12.dp))
                            Text("Touche cet emoji dans Google Messages sur ton téléphone", color = P.texteFort)
                        }
                    }
                }
                if (s == Service.MESSENGER && Depot.besoinNip) Nip()
            }
            Depot.erreur?.let { Text(it, color = P.danger, fontSize = 14.sp) }

            Spacer(Modifier.height(12.dp))
            TextButton(onClick = {
                ctx.startService(Intent(ctx, PontService::class.java).setAction(PontService.ACTION_ARRETER))
                (ctx as? android.app.Activity)?.finishAffinity()
            }) { Text("Quitter Jasette (arrête les ponts)", color = P.textePale) }
        }
    }
}

@Composable
private fun BoutonAccent(texte: String, onClick: () -> Unit) {
    Button(onClick = onClick, colors = ButtonDefaults.buttonColors(containerColor = P.accent)) { Text(texte) }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun Carte(s: Service, boutons: @Composable () -> Unit) {
    val etat = Depot.etat(s)
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(P.surface).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Image(painterResource(iconeService(s)), null, Modifier.size(40.dp).clip(CircleShape))
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(s.titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
                Depot.noms[s]?.let { if (etat.associe) Text(it, color = P.texte, fontSize = 13.sp) }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    androidx.compose.foundation.layout.Box(
                        Modifier.size(8.dp).clip(CircleShape).background(couleurEtat(etat)),
                    )
                    Spacer(Modifier.width(6.dp))
                    Text(etat.texte, color = P.textePale, fontSize = 13.sp)
                }
            }
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { boutons() }
    }
}

/** Le NIP Messenger (6 chiffres) pour restaurer l'historique chiffré. */
@Composable
private fun Nip() {
    var nip by remember { mutableStateOf("") }
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(P.surface).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text("Historique chiffré de Messenger", color = P.texteFort, fontWeight = FontWeight.SemiBold)
        Text(
            "Entre le NIP de 6 chiffres choisi dans Messenger pour restaurer tes anciennes conversations chiffrées.",
            color = P.texte, fontSize = 14.sp,
        )
        OutlinedTextField(
            nip, { v -> nip = v.filter { it.isDigit() }.take(6) },
            label = { Text("NIP") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
            colors = OutlinedTextFieldDefaults.colors(focusedBorderColor = P.accent, cursorColor = P.accent),
        )
        Depot.erreurNip?.let { Text(it, color = P.danger, fontSize = 13.sp) }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(
                onClick = { Depot.envoyerNip(nip) }, enabled = nip.length == 6,
                colors = ButtonDefaults.buttonColors(containerColor = P.accent),
            ) { Text("Restaurer") }
            TextButton(onClick = { Depot.envoyerNip("") }) { Text("Pas maintenant", color = P.textePale) }
        }
    }
}
