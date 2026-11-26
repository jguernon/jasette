package ca.jasette.android.ui

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
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
import ca.jasette.android.Service
import ca.jasette.android.t

/**
 * La connexion de Telegram sur cet appareil (mode « Sur cet appareil », pont
 * embarqué) : PROTOCOLE.md §5.4.
 *
 * 1. L'application (api_id, api_hash de my.telegram.org), si elle manque.
 * 2. Le numéro (`login`), puis le code reçu dans Telegram (`login_code`), puis
 *    le mot de passe s'il y en a un (`login_password`) ; ou bien le code QR
 *    (`pair`), à scanner sur un autre appareil connecté à Telegram.
 *
 * L'écran se ferme tout seul quand Telegram est connecté.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AssociationTelegram(fini: () -> Unit) {
    var parQr by rememberSaveable { mutableStateOf(false) }
    val etat = Depot.etat(Service.TELEGRAM)
    LaunchedEffect(etat) { if (etat.associe) fini() }

    Scaffold(
        containerColor = P.ecran,
        topBar = {
            BarreHaut(
                navigationIcon = { IconButton(onClick = fini) { Icon(Icons.Filled.Close, t("Fermer")) } },
                title = { Text(t("Connecter Telegram"), fontWeight = FontWeight.Bold) },
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
            Column(Modifier.widthIn(max = 520.dp).fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                val etape = Depot.etapeTelegram
                when {
                    Depot.apiTelegram != true -> ApiTelegram()
                    etape == "code" -> EtapeCode()
                    etape == "password" -> EtapeMotDePasse()
                    etape == "attente" -> Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center) {
                        CircularProgressIndicator(color = P.accent)
                    }
                    parQr -> {
                        EtapeQr(Service.TELEGRAM)
                        TextButton(shape = formeBouton, onClick = { parQr = false; Depot.deconnecter(Service.TELEGRAM) }) {
                            Text(t("Se connecter plutôt avec mon numéro"), color = P.accent)
                        }
                    }
                    else -> {
                        EtapeNumero()
                        TextButton(shape = formeBouton, onClick = { parQr = true }) {
                            Text(t("Se connecter plutôt avec un code QR"), color = P.accent)
                        }
                    }
                }
                if (Depot.apiTelegram == true && (etape == "code" || etape == "password")) {
                    TextButton(shape = formeBouton, onClick = {
                        parQr = false
                        Depot.deconnecter(Service.TELEGRAM)
                    }) { Text(t("Recommencer"), color = P.textePale) }
                }
                Depot.erreur?.let { Text(it, color = P.danger, fontSize = 14.sp) }
            }
        }
    }
}

/** L'application Telegram de l'utilisateur : l'explication, le lien et les deux champs. */
@Composable
fun ApiTelegram() {
    val ctx = LocalContext.current
    var id by rememberSaveable { mutableStateOf("") }
    var hash by rememberSaveable { mutableStateOf("") }
    val idOk = id.isNotEmpty() && id.length <= 12 && id.all { it.isDigit() }
    val hashOk = hash.length == 32 && hash.all { it.isDigit() || it.lowercaseChar() in 'a'..'f' }
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(t("Application Telegram"), color = P.texteFort, fontWeight = FontWeight.SemiBold)
        Text(
            t("Telegram demande à chaque application son propre identifiant. Crée le tien, gratuitement : connecte-toi sur my.telegram.org avec ton numéro, ouvre « API development tools », remplis le formulaire (nom et nom court au choix), puis copie ici l'api_id et l'api_hash. Ils restent sur ce téléphone."),
            color = P.texte, fontSize = 14.sp,
        )
        TextButton(shape = formeBouton, onClick = {
            runCatching { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://my.telegram.org/apps"))) }
        }) { Text(t("Ouvrir my.telegram.org"), color = P.accent) }
        if (Depot.apiTelegram == true) {
            Text(t("Application réglée. Pour la changer, entre un autre api_id et api_hash."), color = P.textePale, fontSize = 13.sp)
        }
        OutlinedTextField(
            id, { v -> id = v.filter { it.isDigit() }.take(12) },
            label = { Text("api_id") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            shape = formeChamp,
            colors = couleursChamp(),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            hash, { v -> hash = v.trim().take(32) },
            label = { Text("api_hash") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            shape = formeChamp,
            colors = couleursChamp(),
            modifier = Modifier.fillMaxWidth(),
        )
        Button(
            onClick = {
                Depot.telegramApi(id, hash.lowercase())
                hash = ""
            },
            enabled = idOk && hashOk,
            shape = formeBouton,
            colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
        ) { Text(t("Enregistrer")) }
    }
}

@Composable
private fun EtapeNumero() {
    var numero by rememberSaveable { mutableStateOf("+1") }
    val chiffres = numero.count { it.isDigit() }
    val valide = numero.trim().startsWith("+") && chiffres in 8..15
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(t("Entre ton numéro Telegram, avec l'indicatif du pays : Telegram t'enverra un code."), color = P.texte, fontSize = 14.sp)
        OutlinedTextField(
            numero, { v -> numero = v.filter { it.isDigit() || it == '+' || it == ' ' || it == '-' }.take(22) },
            label = { Text(t("Numéro de téléphone")) },
            placeholder = { Text("+1 514 555 1234") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone),
            shape = formeChamp,
            colors = couleursChamp(),
            modifier = Modifier.fillMaxWidth(),
        )
        Button(
            onClick = { Depot.telegramNumero("+" + numero.filter { it.isDigit() }) },
            enabled = valide,
            shape = formeBouton,
            colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
        ) { Text(t("Recevoir un code")) }
    }
}

@Composable
private fun EtapeCode() {
    var code by rememberSaveable { mutableStateOf("") }
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            t("Entre le code que Telegram vient d'envoyer dans l'app Telegram (sur un autre appareil connecté), ou par SMS."),
            color = P.texte, fontSize = 14.sp,
        )
        OutlinedTextField(
            code, { v -> code = v.filter { it.isDigit() }.take(8) },
            label = { Text(t("Code")) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
            shape = formeChamp,
            colors = couleursChamp(),
            modifier = Modifier.fillMaxWidth(),
        )
        Button(
            onClick = { Depot.telegramCode(code) },
            enabled = code.length >= 4,
            shape = formeBouton,
            colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
        ) { Text(t("Continuer")) }
    }
}

@Composable
private fun EtapeMotDePasse() {
    var mdp by rememberSaveable { mutableStateOf("") }
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(t("Ce compte est protégé par un mot de passe (vérification en deux étapes)."), color = P.texte, fontSize = 14.sp)
        Depot.indiceTelegram?.let { Text("${t("Indice :")} $it", color = P.textePale, fontSize = 13.sp) }
        OutlinedTextField(
            mdp, { mdp = it },
            label = { Text(t("Mot de passe")) },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            shape = formeChamp,
            colors = couleursChamp(),
            modifier = Modifier.fillMaxWidth(),
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Button(
                onClick = {
                    Depot.telegramMotDePasse(mdp)
                    mdp = ""
                },
                enabled = mdp.isNotEmpty(),
                shape = formeBouton,
                colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
            ) { Text(t("Continuer")) }
            Spacer(Modifier.size(8.dp))
        }
    }
}
