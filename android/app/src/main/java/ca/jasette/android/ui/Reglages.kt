package ca.jasette.android.ui

import android.content.Intent
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.ui.graphics.nativeCanvas
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
import androidx.compose.ui.draw.alpha
import androidx.compose.foundation.border
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
import ca.jasette.android.Langue
import ca.jasette.android.t
import androidx.compose.foundation.layout.imePadding
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Phone
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.saveable.rememberSaveable

/** Les réglages : les comptes (connecter, déconnecter) et le NIP Messenger. */
/** Les réglages : les comptes (ou le serveur Jasette), le mode, la langue, l'icône. */
@OptIn(ExperimentalMaterial3Api::class, androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun Reglages(retour: () -> Unit, connecter: (Service, String) -> Unit, serveur: () -> Unit) {
    val ctx = LocalContext.current
    Scaffold(
        containerColor = P.ecran,
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = retour) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour")) } },
                title = { Text(t("Réglages"), fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        Column(
            Modifier.fillMaxSize().padding(marges).verticalScroll(rememberScrollState()).navigationBarsPadding()
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text(t("Comptes"), color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            val cfg = Depot.serveur
            if (cfg != null) {
                // Mode client : les services se connectent sur le serveur, pas ici.
                CarteServeur(cfg.adresse)
                Depot.emoji?.let { e -> BandeauEmoji(e) }
                if (Depot.besoinNip) Nip()
            } else {
                // Sur cet appareil : les ponts embarqués dans l'APK (WhatsApp, Signal
                // et X seulement si leur pont a été compilé, voir Depot.services).
                for (s in Depot.services) {
                    Carte(s) {
                        val etat = Depot.etat(s)
                        if (etat.associe || etat == Etat.ASSOCIATION) {
                            OutlinedButton(shape = formeBouton, onClick = { Depot.deconnecter(s) }) { Text(t("Déconnecter"), color = P.danger) }
                        } else {
                            when (s) {
                                Service.GOOGLE -> BoutonAccent(t("Se connecter")) { connecter(s, "google") }
                                Service.MESSENGER -> {
                                    BoutonAccent(t("Avec Messenger")) { connecter(s, "messenger") }
                                    OutlinedButton(shape = formeBouton, onClick = { connecter(s, "facebook") }) { Text(t("Avec Facebook")) }
                                }
                                Service.INSTAGRAM -> BoutonAccent(t("Se connecter")) { connecter(s, "instagram") }
                                // WhatsApp : par numéro de téléphone (défaut) ou code QR (Association.kt).
                                Service.WHATSAPP -> {
                                    BoutonAccent(t("Avec un numéro de téléphone")) { connecter(s, "telephone") }
                                    OutlinedButton(shape = formeBouton, onClick = { connecter(s, "qr") }) { Text(t("Code QR")) }
                                }
                                Service.SIGNAL -> BoutonAccent(t("Lier avec un code QR")) { connecter(s, "qr") }
                                // X : la fenêtre web de x.com, puis le code X Chat (Connexion.kt).
                                Service.X -> BoutonAccent(t("Se connecter")) { connecter(s, "x") }
                            }
                        }
                    }
                    if (s == Service.GOOGLE) Depot.emoji?.let { e -> BandeauEmoji(e) }
                    if (s == Service.MESSENGER && Depot.besoinNip) Nip()
                }
            }
            Depot.erreur?.let { Text(it, color = P.danger, fontSize = 14.sp) }

            Spacer(Modifier.height(12.dp))
            Text(t("Serveur Jasette"), color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            Groupe {
                ChoixRangee(
                    icone = Icons.Filled.Home,
                    titre = if (cfg != null) t("Utiliser mon serveur Jasette") else t("Sur cet appareil"),
                    detail = if (cfg != null) cfg.adresse else t("Jasette se connecte elle-même à chaque service."),
                    actif = false,
                    chevron = true,
                    onClick = serveur,
                )
            }

            Spacer(Modifier.height(12.dp))
            Text(t("Langue"), color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            ChoixLangue()

            Spacer(Modifier.height(12.dp))
            Text(t("Apparence"), color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            ChoixTheme()
            ChoixGrilleHead()
            ChoixHeures()
            ChoixContacts()
            ChoixTeinte()
            ChoixVolets()

            Spacer(Modifier.height(12.dp))
            Text(t("Gestes"), color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            ChoixGestes()

            Spacer(Modifier.height(12.dp))
            Text(t("Icône de l'app"), color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            ChoixIcone()

        }
    }
}

@Composable
private fun BandeauEmoji(e: String) {
    Bandeau(P.accent) {
        Text(e, fontSize = 40.sp)
        Spacer(Modifier.width(12.dp))
        Text(t("Touche cet emoji dans Google Messages sur ton téléphone"), color = P.texteFort)
    }
}

/** Mode client : à la place des services, la carte du serveur. */
@Composable
private fun CarteServeur(adresse: String) {
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Filled.Home, null, tint = P.accent, modifier = Modifier.size(32.dp))
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    "${t("Votre serveur")}${t(" : ")}$adresse",
                    color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 16.sp,
                )
            }
        }
        // Écran étroit (Fold plié) : le service sur une ligne, l'état et le compte sur la suivante.
        val etroit = androidx.compose.ui.platform.LocalConfiguration.current.screenWidthDp < 600
        for (s in Depot.services) {
            val etat = Depot.etat(s)
            // WhatsApp, Signal et X s'associent depuis Jasette sur l'ordinateur (QR, code, cookies).
            val aConnecter = s.serveurSeulement && (etat == Etat.NON_ASSOCIE || etat == Etat.ASSOCIATION)
            Row(verticalAlignment = if (etroit) Alignment.Top else Alignment.CenterVertically) {
                androidx.compose.foundation.layout.Box(
                    Modifier.padding(top = if (etroit) 6.dp else 0.dp).size(8.dp).clip(formeRonde).background(couleurEtat(etat)),
                )
                Spacer(Modifier.width(8.dp))
                val nom = Depot.noms[s]?.takeIf { etat.associe }
                if (etroit) {
                    Column {
                        Text(s.court, color = P.texteFort, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
                        Text(listOfNotNull(etat.texte, nom).joinToString(" · "), color = P.texte, fontSize = 13.sp)
                        if (aConnecter) Text(t("Connecte-le depuis Jasette sur ton ordinateur"), color = P.textePale, fontSize = 12.sp)
                    }
                } else {
                    Column {
                        Text("${s.court}${t(" : ")}${etat.texte}${nom?.let { " · $it" } ?: ""}", color = P.texte, fontSize = 13.sp)
                        if (aConnecter) Text(t("Connecte-le depuis Jasette sur ton ordinateur"), color = P.textePale, fontSize = 12.sp)
                    }
                }
            }
        }
        Text(t("Les services se connectent et se déconnectent sur le serveur."), color = P.textePale, fontSize = 12.sp)
    }
}

/** « Ne plus l'utiliser », avec confirmation : on ne quitte pas son serveur par accident. */
@Composable
private fun BoutonQuitterServeur() {
    var confirmer by remember { mutableStateOf(false) }
    OutlinedButton(shape = formeBouton, onClick = { confirmer = true }) { Text(t("Ne plus l'utiliser"), color = P.danger) }
    if (confirmer) ConfirmerQuitterServeur { confirmer = false }
}

@Composable
private fun ConfirmerQuitterServeur(fermer: () -> Unit) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = fermer,
        containerColor = P.menu,
        title = { Text(t("Ne plus utiliser ce serveur ?"), color = P.texteFort) },
        text = {
            Text(
                t("Cet appareil se débranchera du serveur Jasette. Tes services restent connectés sur le serveur ; pour revenir, il faudra jumeler de nouveau avec un nouveau code."),
                color = P.texte,
            )
        },
        confirmButton = {
            androidx.compose.material3.TextButton(shape = formeBouton, onClick = {
                fermer()
                Depot.utiliserCetAppareil()
            }) { Text(t("Ne plus l'utiliser"), color = P.danger) }
        },
        dismissButton = { androidx.compose.material3.TextButton(shape = formeBouton, onClick = fermer) { Text(t("Annuler"), color = P.texteFort) } },
    )
}

/** Un groupe de rangées sur fond de carte. */
@Composable
fun Groupe(contenu: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit) {
    Column(Modifier.fillMaxWidth().carte(16.dp), content = contenu)
}

/** Une rangée à choisir : icône, titre, détail, coche si [actif]. */
@Composable
fun ChoixRangee(
    icone: androidx.compose.ui.graphics.vector.ImageVector,
    titre: String,
    detail: String,
    actif: Boolean,
    chevron: Boolean = false,
    badge: String? = null,
    enabled: Boolean = true,
    onClick: () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().clickable(enabled = enabled, onClick = onClick).padding(horizontal = 16.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icone, null, tint = if (actif || chevron) P.accent else P.textePale, modifier = Modifier.size(24.dp))
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                if (badge != null) {
                    Spacer(Modifier.width(8.dp))
                    Text(
                        badge, color = P.surAccent, fontSize = 11.sp, fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.clip(forme(8.dp)).background(P.accentPlein)
                            .padding(horizontal = 7.dp, vertical = 2.dp),
                    )
                }
            }
            Text(detail, color = P.textePale, fontSize = 13.sp)
        }
        if (actif) Icon(Icons.Filled.Check, null, tint = P.accent, modifier = Modifier.size(20.dp))
        if (chevron) Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = P.textePale)
    }
}

/** Réglages → Langue : Automatique (celle du téléphone), Français ou English. */
@Composable
private fun ChoixLangue() {
    val choix = Langue.choix
    Groupe {
        for ((valeur, titre) in listOf(
            "auto" to t("Automatique"),
            "fr" to "Français",
            "en" to "English",
        )) {
            Row(
                Modifier.fillMaxWidth().clickable { Langue.choisir(valeur) }.padding(horizontal = 16.dp, vertical = 13.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f)) {
                    Text(titre, color = P.texteFort, fontSize = 15.sp)
                    if (valeur == "auto") {
                        Text(
                            "${t("Celle du téléphone")} (${if (Langue.telephoneFrancais()) "Français" else "English"})",
                            color = P.textePale, fontSize = 13.sp,
                        )
                    }
                }
                if (choix == valeur) Icon(Icons.Filled.Check, null, tint = P.accent, modifier = Modifier.size(20.dp))
            }
        }
    }
}

/** Le petit sélecteur de l'accueil : « Français / English ». */
@Composable
fun SelecteurLangue() {
    val anglais = Langue.anglais
    Row(
        Modifier.carte(999.dp, if (P.clair) P.champ else P.fond).padding(3.dp),
        horizontalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        for ((en, nom) in listOf(false to "Français", true to "English")) {
            val actif = anglais == en
            Text(
                nom,
                color = if (actif) P.surAccent else P.texte,
                fontWeight = if (actif) FontWeight.SemiBold else FontWeight.Normal,
                fontSize = 14.sp,
                modifier = Modifier.clip(formePilule)
                    .background(if (actif) P.accentPlein else androidx.compose.ui.graphics.Color.Transparent)
                    .clickable {
                        // La langue du téléphone reste « automatique ».
                        Langue.choisir(if (en != Langue.telephoneFrancais()) "auto" else if (en) "en" else "fr")
                    }
                    .padding(horizontal = 14.dp, vertical = 7.dp),
            )
        }
    }
}

/** Réglages → Serveur Jasette : « Sur cet appareil » ou « Utiliser mon serveur Jasette ». */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServeurJasette(retour: () -> Unit) {
    // Un jumelage réussi : retour à l'écran d'avant.
    LaunchedEffect(Depot.jumelageReussi) {
        if (Depot.jumelageReussi) {
            Depot.jumelageReussi = false
            retour()
        }
    }
    val cfg = Depot.serveur
    val occupe = Depot.serveurOccupe
    Scaffold(
        containerColor = P.ecran,
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = retour) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour")) } },
                title = { Text(t("Serveur Jasette"), fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        var confirmerQuitter by remember { mutableStateOf(false) }
        Column(
            Modifier.fillMaxSize().padding(marges).verticalScroll(rememberScrollState()).navigationBarsPadding()
                .imePadding().padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Groupe {
                ChoixRangee(
                    icone = Icons.Filled.Phone,
                    titre = t("Sur cet appareil"),
                    detail = t("Jasette se connecte elle-même à chaque service."),
                    actif = cfg == null && !Depot.choixClient,
                    enabled = !occupe,
                ) { if (cfg != null) confirmerQuitter = true else Depot.utiliserCetAppareil() }
                if (confirmerQuitter) ConfirmerQuitterServeur { confirmerQuitter = false }
                androidx.compose.material3.HorizontalDivider(color = P.filet)
                ChoixRangee(
                    icone = Icons.Filled.Home,
                    titre = t("Utiliser mon serveur Jasette"),
                    detail = t("Tous les services passent par ton serveur : rien à connecter ici. Google Messages fonctionne ainsi."),
                    actif = cfg != null || Depot.choixClient,
                    enabled = !occupe && cfg == null,
                ) {
                    Depot.choixClient = true
                    Depot.erreurServeur = null
                }
            }
            when {
                cfg != null -> {
                    Groupe {
                        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(t("Votre serveur"), color = P.texte, fontSize = 13.sp)
                            Text(cfg.adresse, color = P.texteFort, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
                            Text("${t("Cet appareil")}${t(" : ")}${cfg.nomAppareil}", color = P.texte, fontSize = 13.sp)
                            Text(
                                t("Pour changer de serveur, choisis « Sur cet appareil », puis jumelle de nouveau."),
                                color = P.textePale, fontSize = 12.sp,
                            )
                        }
                    }
                    BoutonQuitterServeur()
                }
                Depot.choixClient -> FormulaireClient(occupe)
            }
            if (occupe) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    androidx.compose.material3.CircularProgressIndicator(Modifier.size(18.dp), color = P.accent, strokeWidth = 2.dp)
                    Spacer(Modifier.width(10.dp))
                    Text(t("Un instant…"), color = P.texte)
                }
            }
            Depot.erreurServeur?.let { Text(it, color = P.danger, fontSize = 14.sp) }
        }
    }
}

/** Le jumelage : adresse et code, puis la comparaison du code de vérification. */
@Composable
private fun FormulaireClient(occupe: Boolean) {
    val v = Depot.verifClient
    Groupe {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (v != null) {
                Text(
                    "${t("Le serveur")} ${v.adresse} ${t("affiche-t-il ce code de vérification ?")}",
                    color = P.texte, fontSize = 15.sp,
                )
                Text(
                    "${v.codeVerification.take(3)} ${v.codeVerification.drop(3)}",
                    color = P.texteFort, fontSize = 34.sp, fontWeight = FontWeight.Bold,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    Button(
                        onClick = { Depot.jumelerClient() }, enabled = !occupe,
                        colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                        shape = formeBouton,
                    ) { Text(t("Oui, jumeler")) }
                    OutlinedButton(shape = formeBouton, onClick = { Depot.refuserVerification() }, enabled = !occupe) {
                        Text(t("Non"), color = P.danger)
                    }
                }
            } else {
                var adresse by rememberSaveable { mutableStateOf("") }
                var code by rememberSaveable { mutableStateOf("") }
                Text(
                    t("Sur le serveur : jasette-serveur --jumeler (ou, au bureau, Réglages > Serveur Jasette > Jumeler un appareil). Entre ici son adresse et le code affiché."),
                    color = P.texte, fontSize = 14.sp,
                )
                OutlinedTextField(
                    adresse, { v -> adresse = v.filterNot { it.isWhitespace() }.take(200) },
                    label = { Text(t("Adresse du serveur")) },
                    placeholder = { Text(t("nom ou IP (port 47800 par défaut)")) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                    colors = OutlinedTextFieldDefaults.colors(focusedBorderColor = P.accent, cursorColor = P.accent),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    code, { c -> code = c.filter { it.isDigit() }.take(6) },
                    label = { Text(t("Code à 6 chiffres")) },
                    singleLine = true,
                    // Pas « mot de passe » : un code à usage unique, que les gestionnaires n'ont pas à retenir.
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number, autoCorrectEnabled = false),
                    colors = OutlinedTextFieldDefaults.colors(focusedBorderColor = P.accent, cursorColor = P.accent),
                    modifier = Modifier.fillMaxWidth(),
                )
                Button(
                    onClick = { Depot.verifierClient(adresse, code) },
                    enabled = !occupe && adresse.isNotBlank() && code.length == 6,
                    colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                        shape = formeBouton,
                ) { Text(t("Continuer")) }
            }
        }
    }
}

@Composable
internal fun BoutonAccent(texte: String, onClick: () -> Unit) {
    Button(
        onClick = onClick, shape = formeBouton,
        colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
    ) { Text(texte) }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun Carte(s: Service, boutons: @Composable () -> Unit) {
    val etat = Depot.etat(s)
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Image(painterResource(iconeService(s)), null, Modifier.size(40.dp).clip(formeLogo(40.dp)))
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(s.titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 16.sp)
                Depot.noms[s]?.let { if (etat.associe) Text(it, color = P.texte, fontSize = 13.sp) }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    androidx.compose.foundation.layout.Box(
                        Modifier.size(8.dp).clip(formeRonde).background(couleurEtat(etat)),
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
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(t("Historique chiffré de Messenger"), color = P.texteFort, fontWeight = FontWeight.SemiBold)
        Text(
            t("Entre le NIP de 6 chiffres choisi dans Messenger pour restaurer tes anciennes conversations chiffrées."),
            color = P.texte, fontSize = 14.sp,
        )
        OutlinedTextField(
            nip, { v -> nip = v.filter { it.isDigit() }.take(6) },
            label = { Text(t("NIP")) },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
            colors = OutlinedTextFieldDefaults.colors(focusedBorderColor = P.accent, cursorColor = P.accent),
        )
        Depot.erreurNip?.let { Text(it, color = P.danger, fontSize = 13.sp) }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(
                onClick = { Depot.envoyerNip(nip) }, enabled = nip.length == 6,
                colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                        shape = formeBouton,
            ) { Text(t("Restaurer")) }
            TextButton(shape = formeBouton, onClick = { Depot.envoyerNip("") }) { Text(t("Pas maintenant"), color = P.textePale) }
        }
    }
}

/** Les deux lanceurs du manifeste : un seul est actif à la fois. */
private const val LANCEUR_SOMBRE = "ca.jasette.android.LanceurSombre"
private const val LANCEUR_BLANC = "ca.jasette.android.LanceurBlanc"

private fun iconeBlanche(ctx: android.content.Context): Boolean =
    ctx.packageManager.getComponentEnabledSetting(android.content.ComponentName(ctx, LANCEUR_BLANC)) ==
        android.content.pm.PackageManager.COMPONENT_ENABLED_STATE_ENABLED

/** Change l'icône du lanceur : active un alias, désactive l'autre (sans fermer l'app). */
private fun choisirIcone(ctx: android.content.Context, blanche: Boolean) {
    val pm = ctx.packageManager
    val activer = android.content.pm.PackageManager.COMPONENT_ENABLED_STATE_ENABLED
    val desactiver = android.content.pm.PackageManager.COMPONENT_ENABLED_STATE_DISABLED
    val garder = android.content.pm.PackageManager.DONT_KILL_APP
    // Activer d'abord la nouvelle : il y a toujours au moins un lanceur.
    pm.setComponentEnabledSetting(
        android.content.ComponentName(ctx, if (blanche) LANCEUR_BLANC else LANCEUR_SOMBRE), activer, garder,
    )
    pm.setComponentEnabledSetting(
        android.content.ComponentName(ctx, if (blanche) LANCEUR_SOMBRE else LANCEUR_BLANC), desactiver, garder,
    )
}

@Composable
private fun ChoixIcone() {
    val ctx = LocalContext.current
    var blanche by remember { mutableStateOf(iconeBlanche(ctx)) }
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp), modifier = Modifier.padding(vertical = 8.dp)) {
        for ((valeur, nom, res) in listOf(
            Triple(false, t("Sombre"), ca.jasette.android.R.mipmap.ic_launcher),
            Triple(true, t("Blanche"), ca.jasette.android.R.mipmap.ic_launcher_blanc),
        )) {
            val choisie = blanche == valeur
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier
                    .clip(forme(16.dp))
                    .background(if (choisie) P.accent.copy(alpha = 0.18f) else androidx.compose.ui.graphics.Color.Transparent)
                    .clickable { choisirIcone(ctx, valeur); blanche = valeur }
                    .padding(12.dp),
            ) {
                // Le rendu adaptatif ne passe pas par painterResource : l'image vient du lanceur.
                val dessin = remember(res) { androidx.core.content.ContextCompat.getDrawable(ctx, res) }
                androidx.compose.foundation.Canvas(Modifier.size(64.dp)) {
                    dessin?.let {
                        it.setBounds(0, 0, size.width.toInt(), size.height.toInt())
                        drawContext.canvas.nativeCanvas.let { c -> it.draw(c) }
                    }
                }
                Spacer(Modifier.height(6.dp))
                Text(nom, color = if (choisie) P.texteFort else P.texte, fontSize = 13.sp)
            }
        }
    }
    Text(
        t("Le lanceur peut prendre quelques secondes à montrer la nouvelle icône."),
        color = P.textePale,
        fontSize = 12.sp,
    )
}

/** Réglages → Affichage : deux volets (liste + fil) sur grand écran, Automatique ou Désactivé. */
@Composable
private fun ChoixVolets() {
    val oui = ca.jasette.android.Marques.deuxVolets
    Text(t("Affichage en deux volets sur grand écran"), color = P.textePale, fontSize = 13.sp)
    Groupe {
        ChoixRangee(
            icone = Icones.volets,
            titre = t("Automatique"),
            detail = t("La liste et la conversation côte à côte quand l'écran est large (téléphone déplié, tablette)."),
            actif = oui,
        ) { ca.jasette.android.Marques.choisirDeuxVolets(true) }
        androidx.compose.material3.HorizontalDivider(color = P.filet)
        ChoixRangee(
            icone = Icones.pleinEcran,
            titre = t("Désactivé"),
            detail = t("La liste, puis la conversation en plein écran, comme sur un téléphone plié."),
            actif = !oui,
        ) { ca.jasette.android.Marques.choisirDeuxVolets(false) }
    }
}

/** Réglages → Apparence : « Afficher l'heure de chaque message » (comme `details_heures` du bureau). */
@Composable
private fun ChoixHeures() {
    val oui = ca.jasette.android.Marques.heureChaqueMessage
    Groupe {
        Row(
            Modifier.fillMaxWidth().clickable { ca.jasette.android.Marques.choisirHeureChaqueMessage(!oui) }
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(t("Afficher l'heure de chaque message"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                Text(t("Chaque bulle montre son heure, et sa date si ce n'est pas aujourd'hui."), color = P.textePale, fontSize = 13.sp)
            }
            Spacer(Modifier.width(12.dp))
            // L'interrupteur du bureau : piste d'accent allumé, grise et visible éteint.
            Interrupteur(oui) { ca.jasette.android.Marques.choisirHeureChaqueMessage(it) }
        }
    }
}

/**
 * Réglages → Apparence : les trois thèmes du bureau (`Section::Apparence`),
 * chacun avec sa vignette, son nom et son détail, la coche sur le thème choisi.
 */
@Composable
private fun ChoixTheme() {
    val bordVignette = when {
        P.clair -> androidx.compose.ui.graphics.Color.White.copy(alpha = 0.9f)
        P.ega -> couleur(EGA_GRIS)
        else -> couleur(0x48484a)
    }
    val teinte = Apparence.teinte
    Groupe {
        LigneTheme(
            Theme.SOMBRE, t("Sombre"), t("Aplats gris et fil noir, bulles plates."),
            Modifier.border(1.dp, bordVignette, forme(14.dp)).background(
                androidx.compose.ui.graphics.Brush.horizontalGradient(
                    0f to couleur(0x252527), 0.5f to couleur(0x252527), 0.5f to couleur(0x000000), 1f to couleur(0x000000),
                ),
            ),
        ) {}
        androidx.compose.material3.HorizontalDivider(color = P.filet)
        LigneTheme(
            Theme.VERRE, t("Verre"), t("Verre clair sur halos pastel."),
            Modifier.border(1.dp, bordVignette, forme(14.dp)).background(
                androidx.compose.ui.graphics.Brush.linearGradient(
                    listOf(couleur(teinte.accent).copy(alpha = 0.5f), couleur(teinte.halo).copy(alpha = 0.6f)),
                    androidx.compose.ui.geometry.Offset.Zero, androidx.compose.ui.geometry.Offset.Infinite,
                ),
            ),
        ) {}
        androidx.compose.material3.HorizontalDivider(color = P.filet)
        // L'aperçu EGA : une petite fenêtre DOS, barre bleue, lignes de texte.
        LigneTheme(
            Theme.EGA, t("EGA"), t("Seize couleurs et police IBM, comme un PC de 1984."),
            Modifier.background(couleur(EGA_NOIR)).border(1.dp, couleur(EGA_GRIS_CLAIR), forme(14.dp)),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                androidx.compose.foundation.layout.Box(Modifier.fillMaxWidth().height(9.dp).background(couleur(EGA_BLEU)))
                Column(Modifier.padding(start = 5.dp, top = 1.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    for ((l, c) in listOf(28 to EGA_JAUNE, 38 to EGA_GRIS_CLAIR, 22 to EGA_CYAN_VIF, 32 to EGA_MAGENTA_VIF)) {
                        androidx.compose.foundation.layout.Box(Modifier.width(l.dp).height(3.dp).background(couleur(c)))
                    }
                }
            }
        }
    }
}

/** Une rangée du choix de thème : vignette 54 dp, nom, détail, coche. */
@Composable
private fun LigneTheme(
    theme: Theme,
    titre: String,
    detail: String,
    vignette: Modifier,
    contenu: @Composable () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().clickable { Apparence.choisirTheme(theme) }.padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Les coins de la vignette : 14 dp, carrés en EGA (`rayon(14.)` du bureau).
        androidx.compose.foundation.layout.Box(Modifier.size(54.dp).clip(forme(14.dp)).then(vignette)) { contenu() }
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
            Text(detail, color = P.textePale, fontSize = 13.sp)
        }
        if (Apparence.theme == theme) Icon(Icons.Filled.Check, null, tint = P.accent, modifier = Modifier.size(20.dp))
    }
}

/**
 * Réglages → Apparence → Teinte du verre : une pastille ronde par teinte, la
 * choisie cerclée de son accent. Hors du thème Verre, les pastilles sont
 * estompées et un mot explique comment les activer.
 */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun ChoixTeinte() {
    val actif = Apparence.theme == Theme.VERRE
    val choisie = Apparence.teinte
    Text(
        if (actif) "${t("Teinte du verre")} · ${t(choisie.nom)}" else t("Teinte du verre"),
        color = P.textePale, fontSize = 13.sp,
    )
    Groupe {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            FlowRow(
                Modifier.alpha(if (actif) 1f else 0.35f),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                for (te in TEINTES) {
                    val elle = actif && te.id == choisie.id
                    androidx.compose.foundation.layout.Box(
                        Modifier.size(38.dp).clip(formeRonde)
                            .border(2.dp, if (elle) couleur(te.accent) else androidx.compose.ui.graphics.Color.Transparent, formeRonde)
                            .clickable(enabled = actif) { Apparence.choisirTeinte(te) },
                        contentAlignment = Alignment.Center,
                    ) {
                        androidx.compose.foundation.layout.Box(
                            Modifier.size(28.dp).clip(formeRonde)
                                .background(
                                    androidx.compose.ui.graphics.Brush.linearGradient(
                                        listOf(hsl(te.angle, 0.75f, 0.86f), couleur(te.accent)),
                                        androidx.compose.ui.geometry.Offset.Zero, androidx.compose.ui.geometry.Offset.Infinite,
                                    ),
                                )
                                .border(
                                    1.dp,
                                    when {
                                        P.clair -> androidx.compose.ui.graphics.Color.White.copy(alpha = 0.9f)
                                        P.ega -> couleur(EGA_GRIS)
                                        else -> couleur(0x48484a)
                                    },
                                    formeRonde,
                                ),
                        )
                    }
                }
            }
            if (!actif) {
                Text(t("Choisissez le thème Verre pour changer de teinte."), color = P.textePale, fontSize = 12.sp)
            }
        }
    }
}

/**
 * Réglages → Apparence → Grille du Head : 3, 4, 5 ou 6 de large,
 * chaque choix avec son petit schéma. Propre à cet appareil (Marques.headGrille).
 */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun ChoixGrilleHead() {
    val choisie = ca.jasette.android.Marques.headGrille
    Text(t("Grille du Head"), color = P.textePale, fontSize = 13.sp)
    Groupe {
        FlowRow(
            Modifier.fillMaxWidth().padding(12.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            for (g in ca.jasette.android.Marques.GRILLES) {
                val actif = g == choisie
                Column(
                    Modifier.width(80.dp).clip(forme(12.dp))
                        .border(if (actif) 2.dp else 1.dp, if (actif) P.accent else P.filet, forme(12.dp))
                        .clickable { ca.jasette.android.Marques.choisirHeadGrille(g) }
                        .padding(vertical = 8.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    SchemaGrille(g, if (actif) P.accent else P.textePale)
                    Spacer(Modifier.height(6.dp))
                    Text(
                        t("%d de large").format(ca.jasette.android.Marques.colonnes(g)),
                        color = if (actif) P.texteFort else P.texte, fontSize = 12.sp,
                        fontWeight = if (actif) FontWeight.SemiBold else FontWeight.Normal,
                        maxLines = 1,
                    )
                }
            }
        }
        Text(
            t("Le nombre d'avatars par rangée ; il y a autant de rangées qu'il faut pour tout le Head."),
            color = P.textePale, fontSize = 12.sp,
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 12.dp),
        )
    }
}

/** Le schéma d'une grille : un rond par case, sur deux rangées. */
@Composable
private fun SchemaGrille(grille: String, couleur: androidx.compose.ui.graphics.Color) {
    val colonnes = ca.jasette.android.Marques.colonnes(grille)
    androidx.compose.foundation.Canvas(Modifier.size(width = 56.dp, height = 34.dp)) {
        // Les colonnes choisies, sur deux rangées (la hauteur suit le Head).
        val rangees = 2
        val cote = minOf(size.width / colonnes, size.height / rangees)
        val r = cote * 0.36f
        val x0 = (size.width - cote * colonnes) / 2
        val y0 = (size.height - cote * rangees) / 2
        for (y in 0 until rangees) for (x in 0 until colonnes) {
            drawCircle(
                couleur, r,
                androidx.compose.ui.geometry.Offset(x0 + cote * (x + 0.5f), y0 + cote * (y + 0.5f)),
            )
        }
    }
}
