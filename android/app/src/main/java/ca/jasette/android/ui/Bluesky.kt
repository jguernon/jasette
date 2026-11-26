package ca.jasette.android.ui

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
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
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Depot
import ca.jasette.android.Etat
import ca.jasette.android.Service
import ca.jasette.android.t

/**
 * La connexion de Bluesky sur cet appareil (pont embarqué) : l'identifiant
 * (nom d'utilisateur ou courriel) et un mot de passe d'app avec l'accès aux
 * messages privés. PROTOCOLE.md §5.4. L'écran se ferme tout seul une fois
 * connecté.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AssociationBluesky(fini: () -> Unit) {
    val ctx = LocalContext.current
    val etat = Depot.etat(Service.BLUESKY)
    LaunchedEffect(etat) { if (etat.associe) fini() }
    var ident by rememberSaveable { mutableStateOf("") }
    var mdp by rememberSaveable { mutableStateOf("") }

    Scaffold(
        containerColor = P.ecran,
        topBar = {
            BarreHaut(
                navigationIcon = { IconButton(onClick = fini) { Icon(Icons.Filled.Close, t("Fermer")) } },
                title = { Text(t("Connecter Bluesky"), fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        Column(
            Modifier.fillMaxSize().padding(marges).defilementAvecOmbre()
                .navigationBarsPadding().imePadding().padding(16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(
                Modifier.widthIn(max = 520.dp).fillMaxWidth().carte(16.dp).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                if (etat == Etat.ASSOCIATION) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) {
                        CircularProgressIndicator(color = P.accent)
                    }
                    Text(t("Connexion…"), color = P.texte, fontSize = 14.sp)
                } else {
                    Text(
                        t("Jasette lit et envoie tes messages privés Bluesky. Il lui faut un mot de passe d'app qui a accès aux messages : dans Bluesky, Réglages → Confidentialité et sécurité → Mots de passe d'app → Ajouter, en cochant « Autoriser l'accès à vos messages privés ». Ton vrai mot de passe n'est jamais demandé."),
                        color = P.texte, fontSize = 14.sp,
                    )
                    TextButton(shape = formeBouton, onClick = {
                        runCatching { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://bsky.app/settings/app-passwords"))) }
                    }) { Text(t("Ouvrir les mots de passe d'app"), color = P.accent) }
                    OutlinedTextField(
                        ident, { ident = it.trim().take(256) },
                        label = { Text(t("Nom d'utilisateur ou courriel")) },
                        placeholder = { Text("nom.bsky.social") },
                        singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
                        shape = formeChamp,
                        colors = couleursChamp(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    OutlinedTextField(
                        mdp, { mdp = it.trim().take(64) },
                        label = { Text(t("Mot de passe d'app")) },
                        placeholder = { Text("xxxx-xxxx-xxxx-xxxx") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        shape = formeChamp,
                        colors = couleursChamp(),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    Button(
                        onClick = {
                            Depot.blueskyConnexion(ident, mdp)
                            mdp = ""
                        },
                        enabled = ident.isNotEmpty() && mdp.isNotEmpty(),
                        shape = formeBouton,
                        colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                    ) { Text(t("Se connecter")) }
                }
                Depot.erreur?.let { Text(it, color = P.danger, fontSize = 14.sp) }
            }
        }
    }
}
