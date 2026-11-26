package ca.jasette.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
 * La connexion de Discord (PROTOCOLE.md §5.4) : un code QR à scanner avec
 * l'app Discord d'un autre appareil (`pair`), ou, en second choix, le jeton
 * du compte collé à la main (`login`). L'écran se ferme tout seul une fois
 * connecté.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AssociationDiscord(fini: () -> Unit) {
    var parQr by rememberSaveable { mutableStateOf(false) }
    var jeton by rememberSaveable { mutableStateOf("") }
    val etat = Depot.etat(Service.DISCORD)
    LaunchedEffect(etat) { if (etat.associe) fini() }

    Scaffold(
        containerColor = P.ecran,
        topBar = {
            BarreHaut(
                navigationIcon = { IconButton(onClick = fini) { Icon(Icons.Filled.Close, t("Fermer")) } },
                title = { Text(t("Connecter Discord"), fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        Column(
            Modifier.fillMaxSize().padding(marges).verticalScroll(rememberScrollState())
                .navigationBarsPadding().imePadding().padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(Modifier.widthIn(max = 520.dp).fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                val lu = Depot.qrLuDiscord
                when {
                    lu != null -> Column(
                        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        CircularProgressIndicator(color = P.accent)
                        Text(
                            t("Code lu. Confirme la connexion sur l'autre appareil, dans Discord.") + if (lu.isNotEmpty()) " ($lu)" else "",
                            color = P.texte, fontSize = 14.sp,
                        )
                    }
                    parQr -> {
                        // EtapeQr envoie « pair » ; un nouveau « pair » remplace le précédent.
                        EtapeQr(Service.DISCORD)
                        TextButton(shape = formeBouton, onClick = { parQr = false; Depot.deconnecter(Service.DISCORD) }) {
                            Text(t("Recommencer"), color = P.accent)
                        }
                    }
                    etat == Etat.ASSOCIATION || etat == Etat.CONNEXION -> Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) {
                        CircularProgressIndicator(color = P.accent)
                    }
                    else -> Column(
                        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Text(
                            t("Jasette lit et envoie tes messages privés Discord (pas les salons des serveurs). Le plus simple : un code QR à scanner avec l'app Discord d'un autre appareil."),
                            color = P.texte, fontSize = 14.sp,
                        )
                        Button(
                            onClick = { parQr = true },
                            shape = formeBouton,
                            colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                        ) { Text(t("Se connecter avec un code QR")) }
                        Text(
                            t("Ou colle le jeton de ton compte (Discord dans un navigateur d'ordinateur → outils de développement → Réseau → en-tête « Authorization »). Ne le donne à personne : il ouvre ton compte."),
                            color = P.texte, fontSize = 14.sp,
                        )
                        OutlinedTextField(
                            jeton, { jeton = it.trim().trim('"').take(200) },
                            label = { Text(t("Jeton Discord")) },
                            singleLine = true,
                            visualTransformation = PasswordVisualTransformation(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                            shape = formeChamp,
                            colors = couleursChamp(),
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Button(
                            onClick = {
                                Depot.discordJeton(jeton)
                                jeton = ""
                            },
                            enabled = jeton.length >= 50 && jeton.none { it.isWhitespace() },
                            shape = formeBouton,
                            colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                        ) { Text(t("Se connecter")) }
                    }
                }
                Text(
                    t("Discord n'autorise pas officiellement les clients tiers : utilise Jasette à tes risques. Jasette n'agit que sur tes actions."),
                    color = P.textePale, fontSize = 12.sp,
                )
                Depot.erreur?.let { Text(it, color = P.danger, fontSize = 14.sp) }
            }
        }
    }
}
