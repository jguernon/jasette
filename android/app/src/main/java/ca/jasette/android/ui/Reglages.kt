package ca.jasette.android.ui

import android.content.Intent
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.ui.text.style.TextOverflow
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import kotlin.math.roundToInt
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
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
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material.icons.filled.Phone
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Edit
import androidx.compose.runtime.key

/**
 * Les sections des Réglages, dans l'ordre du bureau (`enum Section` de
 * src/main.rs), plus « Conversations », propre à Android : couleur de la
 * tuile (et son équivalent EGA, comme `ega_fond` du bureau).
 */
enum class SectionReglages(val nom: String, val couleur: Long, val couleurEga: Long) {
    COMPTES("Comptes", 0x8b5cf6, 0xaa00aa),
    NOTIFICATIONS("Notifications", 0x16a34a, 0x00aa00),
    SONS("Sons", 0xf59e0b, 0xaa5500),
    QUOI_DE_NEUF("Quoi de neuf", 0x06b6d4, 0x00aaaa),
    APPARENCE("Apparence", 0xec4899, 0xaa00aa),
    CONVERSATIONS("Conversations", 0x14b8a6, 0x00aaaa),
    SERVEUR("Serveur Jasette", 0x8b5cf6, 0xaa00aa),
    PRIVE("Liste privée", 0xf59e0b, 0xaa5500),
    LANGUE("Langue", 0x0ea5e9, 0x00aaaa),
    APROPOS("À propos", 0x64748b, 0x555555),
}

private fun iconeSection(s: SectionReglages): androidx.compose.ui.graphics.vector.ImageVector = when (s) {
    SectionReglages.COMPTES -> Icones.arobase
    SectionReglages.NOTIFICATIONS -> Icons.Filled.Notifications
    SectionReglages.SONS -> Icones.volume
    SectionReglages.QUOI_DE_NEUF -> Icons.Filled.Edit
    SectionReglages.APPARENCE -> Icones.palette
    SectionReglages.CONVERSATIONS -> Icones.forum
    SectionReglages.SERVEUR -> Icones.serveur
    SectionReglages.PRIVE -> Icons.Filled.Lock
    SectionReglages.LANGUE -> Icones.langue
    SectionReglages.APROPOS -> Icons.Filled.Info
}

/** Le petit résumé à droite d'une section (« 8 connectés », la version…), ou null. */
private fun resumeSection(s: SectionReglages): String? = when (s) {
    SectionReglages.COMPTES -> Depot.services.count { Depot.etat(it).associe }.let { n ->
        if (n == 1) t("1 connecté") else "$n ${t("connectés")}"
    }
    SectionReglages.SONS -> ca.jasette.android.Sons.JEUX.firstOrNull { it.first == ca.jasette.android.Marques.sonsConversation }
        ?.let { t(it.second) }
    SectionReglages.QUOI_DE_NEUF -> if (ca.jasette.android.Musique.actifIci()) t("Spotify") else null
    SectionReglages.CONVERSATIONS -> ca.jasette.android.Marques.cacherApresJours.takeIf { it > 0 }?.let { nomCacherApres(it) }
    SectionReglages.SERVEUR -> Depot.serveur?.adresse ?: t("Sur cet appareil")
    SectionReglages.LANGUE -> when (Langue.choix) {
        "fr" -> "Français"
        "en" -> "English"
        else -> t("Automatique")
    }
    SectionReglages.APROPOS -> ca.jasette.android.MiseAJour.nomInstalle.takeIf { it.isNotEmpty() }
    else -> null
}

/**
 * Les réglages, en sections comme au bureau : d'abord la liste des sections
 * (icône et couleur, un résumé à droite), un toucher ouvre la page de la
 * section (flèche ou geste de retour : la liste). Sur grand écran en deux
 * volets ([Marques.deuxVolets]) : la liste à gauche, la section à droite.
 * [section] : la section ouverte (null : la liste), gardée par [App] pour
 * survivre à un écran de connexion ; une bannière, un fil hors ligne y mettent
 * Comptes d'emblée. [choisir] la change.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Reglages(
    retour: () -> Unit,
    connecter: (Service, String) -> Unit,
    serveur: () -> Unit,
    section: SectionReglages? = null,
    choisir: (SectionReglages?) -> Unit = {},
) {
    val largeur = androidx.compose.ui.platform.LocalConfiguration.current.screenWidthDp
    val volets = largeur >= LARGEUR_RAIL && ca.jasette.android.Marques.deuxVolets
    // « Serveur Jasette » a son propre écran (aussi ouvert par l'accueil).
    fun ouvrir(s: SectionReglages) {
        if (s == SectionReglages.SERVEUR) serveur() else choisir(s)
    }
    // Téléphone : le retour d'une section ramène à la liste des sections.
    BackHandler(enabled = !volets && section != null) { choisir(null) }
    val dansSection = !volets && section != null
    Scaffold(
        containerColor = P.ecran,
        topBar = {
            BarreHaut(
                navigationIcon = {
                    IconButton(onClick = { if (dansSection) choisir(null) else retour() }) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour"))
                    }
                },
                title = { Text(if (dansSection) t(section.nom) else t("Réglages"), fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        if (volets) {
            // Deux volets : la section choisie (Comptes par défaut) à droite.
            val montree = section ?: SectionReglages.COMPTES
            Row(Modifier.fillMaxSize().padding(marges)) {
                Column(
                    Modifier.width(320.dp).fillMaxHeight().defilementAvecOmbre().navigationBarsPadding()
                        .padding(16.dp),
                ) {
                    ListeSections(choisie = montree, chevrons = false, ouvrir = ::ouvrir)
                }
                androidx.compose.material3.VerticalDivider(color = P.traitFin)
                key(montree) {
                    PageSection(montree, Modifier.weight(1f).fillMaxHeight(), connecter, serveur)
                }
            }
        } else if (section != null) {
            key(section) { PageSection(section, Modifier.fillMaxSize().padding(marges), connecter, serveur) }
        } else {
            Column(
                Modifier.fillMaxSize().padding(marges).defilementAvecOmbre().navigationBarsPadding()
                    .padding(16.dp),
            ) {
                ListeSections(choisie = null, chevrons = true, ouvrir = ::ouvrir)
            }
        }
    }
}

/** La liste des sections : une tuile de couleur avec son icône, le nom, le résumé. */
@Composable
private fun ListeSections(choisie: SectionReglages?, chevrons: Boolean, ouvrir: (SectionReglages) -> Unit) {
    Groupe {
        SectionReglages.entries.forEachIndexed { i, s ->
            if (i > 0) androidx.compose.material3.HorizontalDivider(Modifier.padding(start = 60.dp), color = P.traitFin)
            Row(
                Modifier.fillMaxWidth()
                    .let { if (s == choisie) it.background(P.choisi) else it }
                    .clickable { ouvrir(s) }
                    .padding(horizontal = 14.dp, vertical = 11.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(
                    Modifier.size(32.dp).clip(forme(9.dp)).background(couleur(if (P.ega) s.couleurEga else s.couleur)),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(iconeSection(s), null, tint = androidx.compose.ui.graphics.Color.White, modifier = Modifier.size(19.dp))
                }
                Spacer(Modifier.width(14.dp))
                // Le titre garde toujours une seule ligne ; le résumé prend la
                // place qui reste et se coupe (…) au besoin.
                Text(
                    t(s.nom), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
                    maxLines = 1, softWrap = false,
                )
                Spacer(Modifier.width(10.dp))
                Text(
                    resumeSection(s) ?: "", color = P.textePale, fontSize = 13.sp, maxLines = 1,
                    overflow = TextOverflow.Ellipsis, textAlign = androidx.compose.ui.text.style.TextAlign.End,
                    modifier = Modifier.weight(1f),
                )
                if (chevrons || s == SectionReglages.SERVEUR) {
                    Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = P.textePale)
                }
            }
        }
    }
}

/** La page d'une section : ses réglages, sur leur propre défilement. */
@Composable
private fun PageSection(
    s: SectionReglages,
    modifier: Modifier,
    connecter: (Service, String) -> Unit,
    serveur: () -> Unit,
) {
    Column(
        modifier.defilementAvecOmbre().navigationBarsPadding().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        when (s) {
            SectionReglages.COMPTES -> SectionComptes(connecter)
            SectionReglages.NOTIFICATIONS -> SectionNotifications()
            SectionReglages.SONS -> {
                ChoixSons()
                CurseurVolume()
            }
            SectionReglages.QUOI_DE_NEUF -> SectionQuoiDeNeuf()
            SectionReglages.APPARENCE -> {
                ChoixTheme()
                ChoixTeinte()
                ChoixGrilleHead()
                ChoixTailleMessages()
                ChoixHeures()
                ChoixPastilles()
                ChoixApercus()
                ChoixContacts()
                ChoixVolets()
                Spacer(Modifier.height(4.dp))
                Titre(t("Icône de l'app"))
                ChoixIcone()
            }
            SectionReglages.CONVERSATIONS -> {
                ChoixGarderArchives()
                ChoixCacherApres()
                ChoixPresence()
                Spacer(Modifier.height(4.dp))
                Titre(t("Gestes"))
                ChoixGestes()
            }
            SectionReglages.SERVEUR -> {
                val cfg = Depot.serveur
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
            }
            SectionReglages.PRIVE -> ReglagesPrive()
            SectionReglages.LANGUE -> ChoixLangue()
            SectionReglages.APROPOS -> AProposMiseAJour()
        }
    }
}

@Composable
private fun Titre(texte: String) {
    Text(texte, color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
}

/** Réglages → Comptes : les services (ou la carte du serveur), puis ceux montrés dans la boîte. */
@Composable
private fun SectionComptes(connecter: (Service, String) -> Unit) {
    val ctx = LocalContext.current
    val demanderSms = rememberDemandeSms()
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
            // Google Messages, mode autonome : ou bien les SMS du téléphone (PontSms).
            val noteSms = if (s == Service.GOOGLE && (Depot.smsActif || !Depot.etat(s).associe)) {
                t("SMS et MMS de cet appareil, sans compte Google. Les messages RCS (chat) restent dans l'app Messages.")
            } else {
                null
            }
            Carte(s, noteSms) {
                val etat = Depot.etat(s)
                if (etat.associe || etat == Etat.ASSOCIATION) {
                    OutlinedButton(shape = formeBouton, onClick = { Depot.deconnecter(s) }) { Text(t("Déconnecter"), color = P.danger) }
                } else {
                    when (s) {
                        Service.GOOGLE -> {
                            BoutonAccent(t("Se connecter")) {
                                // Les SMS étaient choisis (accès retiré depuis) : retour au pont Google.
                                if (Depot.modeSms) Depot.utiliserSms(ctx, false)
                                connecter(s, "google")
                            }
                            OutlinedButton(shape = formeBouton, onClick = demanderSms) { Text(t("Utiliser les SMS du téléphone")) }
                        }
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
                        // Telegram : numéro et code, ou code QR (Telegram.kt).
                        Service.TELEGRAM -> BoutonAccent(t("Se connecter")) { connecter(s, "") }
                        // Bluesky : identifiant et mot de passe d'app (Bluesky.kt).
                        Service.BLUESKY -> BoutonAccent(t("Se connecter")) { connecter(s, "") }
                        // Discord : code QR ou jeton (Discord.kt).
                        Service.DISCORD -> BoutonAccent(t("Connecter Discord")) { connecter(s, "") }
                        // Reddit : la fenêtre web de reddit.com (Connexion.kt).
                        Service.REDDIT -> BoutonAccent(t("Se connecter")) { connecter(s, "reddit") }
                        // LinkedIn : la fenêtre web de linkedin.com (Connexion.kt).
                        Service.LINKEDIN -> BoutonAccent(t("Se connecter")) { connecter(s, "linkedin") }
                    }
                }
            }
            if (s == Service.GOOGLE) Depot.emoji?.let { e -> BandeauEmoji(e) }
            if (s == Service.MESSENGER && Depot.besoinNip) Nip()
            // Telegram pas encore connecté : son application (api_id, api_hash).
            if (s == Service.TELEGRAM && !Depot.etat(s).associe) ApiTelegram()
        }
    }
    Depot.erreur?.let { Text(it, color = P.danger, fontSize = 14.sp) }
    ServicesDansLaBoite()
    RegrouperAutomatiquement()
}

/**
 * Réglages → Comptes → « Dans la boîte de réception » : les services cachés
 * et leur ordre ([ServicesCaches] ; le seul endroit sur Android depuis
 * l'onglet Groupes, et la même section du bureau). Les services cachés sont
 * propres à l'appareil ; l'ordre est partagé (marque `ordre_services`).
 */
@Composable
private fun ServicesDansLaBoite() {
    val liste = servicesConnectes()
    if (liste.isEmpty()) return
    val ctx = LocalContext.current
    Spacer(Modifier.height(4.dp))
    Titre(t("Dans la boîte de réception"))
    Groupe {
        for ((i, s) in liste.withIndex()) {
            val montre = !ServicesCaches.cache(s)
            Row(
                Modifier.fillMaxWidth().clickable { ServicesCaches.montrer(ctx, s, !montre) }
                    .padding(start = 16.dp, end = 8.dp, top = 6.dp, bottom = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                LogoService(s, 30.dp, gris = !montre)
                Spacer(Modifier.width(14.dp))
                Text(
                    s.titre, color = if (montre) P.texteFort else P.textePale, fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                // Monter ou descendre d'un cran (comme les favoris en mode
                // Réorganiser) : l'ordre vaut partout où les services sont en liste.
                val haut = i > 0
                val bas = i < liste.size - 1
                IconButton(onClick = { ServicesCaches.echanger(s, liste[i - 1]) }, enabled = haut) {
                    Icon(Icons.Filled.KeyboardArrowUp, t("Monter"), tint = if (haut) P.texteFort else P.choisi)
                }
                IconButton(onClick = { ServicesCaches.echanger(s, liste[i + 1]) }, enabled = bas) {
                    Icon(Icons.Filled.KeyboardArrowDown, t("Descendre"), tint = if (bas) P.texteFort else P.choisi)
                }
                androidx.compose.material3.Checkbox(
                    checked = montre,
                    onCheckedChange = { ServicesCaches.montrer(ctx, s, it) },
                    colors = androidx.compose.material3.CheckboxDefaults.colors(
                        checkedColor = P.accent, uncheckedColor = P.textePale, checkmarkColor = P.surAccent,
                    ),
                )
            }
        }
    }
    Text(
        t("Les services décochés sont cachés de la boîte de réception, des Head et des favoris. Rien n'est oublié : recochez-les pour les retrouver."),
        color = P.textePale, fontSize = 12.sp,
    )
}

/**
 * Réglages → Comptes → « Regrouper automatiquement » : trois cases (Codes,
 * Promotions, Avis), décochées par défaut, propres à l'appareil
 * ([ca.jasette.android.RegroupementAuto]).
 */
@Composable
private fun RegrouperAutomatiquement() {
    val ctx = LocalContext.current
    Spacer(Modifier.height(4.dp))
    Titre(t("Regrouper automatiquement"))
    Groupe {
        for ((g, texte) in listOf(
            ca.jasette.android.GroupeAuto.CODES to t("Codes de vérification"),
            ca.jasette.android.GroupeAuto.PROMOS to t("Promotions"),
            ca.jasette.android.GroupeAuto.AVIS to t("Avis (livraisons, rendez-vous, banque, factures)"),
        )) {
            val coche = ca.jasette.android.RegroupementAuto.actif(g)
            Row(
                Modifier.fillMaxWidth().clickable { ca.jasette.android.RegroupementAuto.choisir(ctx, g, !coche) }
                    .padding(start = 16.dp, end = 8.dp, top = 4.dp, bottom = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                AvatarGroupe(g, 28.dp)
                Spacer(Modifier.width(12.dp))
                Text(texte, color = P.texteFort, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                androidx.compose.material3.Checkbox(
                    checked = coche,
                    onCheckedChange = { ca.jasette.android.RegroupementAuto.choisir(ctx, g, it) },
                    colors = androidx.compose.material3.CheckboxDefaults.colors(
                        checkedColor = P.accent, uncheckedColor = P.textePale, checkmarkColor = P.surAccent,
                    ),
                )
            }
        }
    }
    Text(
        t("Jasette range les expéditeurs automatiques dans une seule rangée, sur cet appareil, sans rien envoyer ailleurs. Les contacts, les favoris, les épinglées, les groupes et les conversations où vous avez écrit ne sont jamais regroupés."),
        color = P.textePale, fontSize = 12.sp,
    )
}

/**
 * Réglages → Notifications : Android les règle lui-même (canaux, sons,
 * bulles) ; un bouton ouvre la page de Jasette dans les réglages du système.
 */
@Composable
private fun SectionNotifications() {
    val ctx = LocalContext.current
    Groupe {
        ChoixRangee(
            icone = Icons.Filled.Notifications,
            titre = t("Notifications d'Android"),
            detail = t("Sons, vibrations, bulles et notifications sur l'écran verrouillé se règlent dans Android."),
            actif = false,
            chevron = true,
        ) {
            val intent = Intent(android.provider.Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                .putExtra(android.provider.Settings.EXTRA_APP_PACKAGE, ctx.packageName)
            try {
                ctx.startActivity(intent)
            } catch (_: Exception) {
            }
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
    var details by rememberSaveable { mutableStateOf(false) }
    // L'horloge du résumé : les 30 s d'une reconnexion, les 10 s d'une coupure du serveur.
    var maintenant by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(Unit) {
        while (true) {
            kotlinx.coroutines.delay(1000)
            maintenant = System.currentTimeMillis()
        }
    }
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
        // À la place de l'état de chaque service : une ligne si tout va bien,
        // sinon une par service en difficulté (voir resumeServices).
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            for ((texte, couleur, ok) in resumeServices(maintenant)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (ok != null) {
                        Icon(if (ok) Icons.Filled.Check else Icons.Filled.Warning, null, tint = couleur, modifier = Modifier.size(16.dp))
                        Spacer(Modifier.width(6.dp))
                    }
                    Text(texte, color = couleur, fontSize = 14.sp)
                }
            }
        }
        // « Détails » : l'ancienne liste complète, repliée par défaut.
        Row(
            Modifier.clip(RoundedCornerShape(6.dp)).clickable { details = !details }.padding(vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(t("Détails"), color = P.textePale, fontSize = 13.sp)
            Icon(
                if (details) Icons.Filled.KeyboardArrowDown else Icons.AutoMirrored.Filled.KeyboardArrowRight,
                null, tint = P.textePale, modifier = Modifier.size(16.dp),
            )
        }
        if (details) {
            // Écran étroit (Fold plié) : le service sur une ligne, l'état et le compte sur la suivante.
            val etroit = androidx.compose.ui.platform.LocalConfiguration.current.screenWidthDp < 600
            for (s in Depot.services) {
                val etat = Depot.etat(s)
                // WhatsApp, Signal et X s'associent depuis Jasette sur l'ordinateur (QR, code, cookies).
                val aConnecter = s.serveurSeulement && (etat == Etat.NON_ASSOCIE || etat == Etat.ASSOCIATION)
                Row(verticalAlignment = if (etroit) Alignment.Top else Alignment.CenterVertically) {
                    PastilleEtat(etat, Modifier.padding(top = if (etroit) (6.dp - (taillePastilleEtat - 8.dp) / 2).coerceAtLeast(1.dp) else 0.dp))
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
}

/**
 * Le résumé des services du serveur : (texte, couleur, réussite) ; réussite
 * null pour une ligne neutre. Ne comptent que les services vus connectés
 * (Depot.connus : dans cette session, ou dont des conversations sont gardées
 * en cache), offerts par le serveur et pas cachés : un service jamais associé
 * ne fait pas un problème. Une (re)connexion n'en est un qu'après 30 s.
 */
@Composable
private fun resumeServices(maintenant: Long): List<Triple<String, androidx.compose.ui.graphics.Color, Boolean?>> {
    val coupe = Depot.serveurCoupeDepuis
    if (coupe > 0 && maintenant - coupe >= 10_000) return listOf(Triple(t("Serveur Jasette injoignable"), P.danger, false))
    val soucis = ArrayList<Triple<String, androidx.compose.ui.graphics.Color, Boolean?>>()
    var enCours = false
    for (s in Depot.services) {
        if (Depot.connus[s] != true || ServicesCaches.cache(s)) continue
        val etat = Depot.etat(s)
        val long = maintenant - (Depot.etatDepuis[s] ?: maintenant) >= 30_000
        when {
            etat == Etat.PRET || etat == Etat.ASSOCIATION -> {}
            etat == Etat.ARRETE -> soucis += Triple(t("%s : pont arrêté.").replace("%s", s.court), P.danger, false)
            etat == Etat.NON_ASSOCIE -> soucis += Triple(t("Votre %s ne semble pas connecté.").replace("%s", s.court), P.danger, false)
            !long -> enCours = true
            etat == Etat.TELEPHONE_HORS_LIGNE -> soucis += Triple(t("Votre %s ne semble pas connecté.").replace("%s", s.court), P.danger, false)
            else -> soucis += Triple(t("%s se reconnecte…").replace("%s", s.court), P.averti, false)
        }
    }
    return when {
        soucis.isNotEmpty() -> soucis
        enCours -> listOf(Triple(t("Vérification de vos services…"), P.textePale, null))
        else -> listOf(Triple(t("Tous vos services sont en ligne."), P.succes, true))
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
/** Réglages > Sons : un bouton radio par jeu ; le choisir le fait entendre (Sons.kt). */
@Composable
private fun ChoixSons() {
    val choix = ca.jasette.android.Marques.sonsConversation
    Text(
        t("Dans la conversation ouverte : à l'envoi, à la réception et quand on vous écrit."),
        color = P.textePale, fontSize = 13.sp, modifier = Modifier.padding(horizontal = 4.dp),
    )
    Groupe {
        for ((valeur, nom) in ca.jasette.android.Sons.JEUX) {
            val actif = choix == valeur
            Row(
                Modifier.fillMaxWidth().clickable {
                    ca.jasette.android.Marques.choisirSons(valeur)
                    ca.jasette.android.Sons.apercu(valeur)
                }.padding(horizontal = 16.dp, vertical = 13.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                val forme = if (P.carre) androidx.compose.ui.graphics.RectangleShape else formeRonde
                Box(
                    Modifier.size(20.dp).clip(forme).border(2.dp, if (actif) P.accent else P.textePale, forme),
                    contentAlignment = Alignment.Center,
                ) {
                    if (actif) Box(Modifier.size(10.dp).clip(forme).background(P.accent))
                }
                Spacer(Modifier.width(14.dp))
                Text(t(nom), color = P.texteFort, fontSize = 15.sp, modifier = Modifier.weight(1f))
            }
        }
    }
}

/**
 * Réglages > Sons : le volume de cet appareil, une piste aimantée aux crans
 * de `Sons.CRANS_VOLUME`, ses encoches dessous (des blocs de pixels en EGA),
 * un haut-parleur et la valeur. Chaque cran relâché fait entendre le son reçu.
 */
@Composable
private fun CurseurVolume() {
    val crans = ca.jasette.android.Sons.CRANS_VOLUME
    val volume = ca.jasette.android.Sons.volume
    val choisi = ca.jasette.android.Sons.cran(volume)
    val n = crans.size
    val carre = P.carre || P.retro
    val accent = P.accent
    val vide = P.filet
    val pale = P.textePale
    val bordBouton = if (carre) P.texteFort else androidx.compose.ui.graphics.Color.White
    Text(
        t("Volume sur cet appareil. Chaque cran relâché fait entendre le son reçu."),
        color = P.textePale, fontSize = 13.sp, modifier = Modifier.padding(start = 4.dp, end = 4.dp, top = 10.dp),
    )
    Groupe {
        Row(
            Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 10.dp, bottom = 6.dp),
            verticalAlignment = Alignment.Top,
        ) {
            Icon(
                when {
                    volume == 0 -> Icones.volumeMuet
                    volume <= 75 -> Icones.volumeBas
                    else -> Icones.volume
                },
                null, tint = if (volume > 0) P.texteFort else P.textePale,
                modifier = Modifier.padding(top = 4.dp).size(20.dp),
            )
            Spacer(Modifier.width(8.dp))
            Column(Modifier.weight(1f)) {
                androidx.compose.foundation.Canvas(
                    Modifier.fillMaxWidth().height(38.dp).pointerInput(n) {
                        fun indice(x: Float) = (x / (size.width.toFloat() / n)).toInt().coerceIn(0, n - 1)
                        awaitEachGesture {
                            val bas = awaitFirstDown()
                            ca.jasette.android.Sons.choisirVolume(crans[indice(bas.position.x)])
                            bas.consume()
                            while (true) {
                                val ev = awaitPointerEvent()
                                val c = ev.changes.firstOrNull { it.id == bas.id } ?: break
                                if (!c.pressed) break
                                ca.jasette.android.Sons.choisirVolume(crans[indice(c.position.x)])
                                c.consume()
                            }
                            // Relâché : l'aperçu, le son reçu du jeu choisi.
                            ca.jasette.android.Sons.jouer(
                                ca.jasette.android.Marques.sonsConversation, ca.jasette.android.Sons.Son.RECU,
                            )
                        }
                    },
                ) {
                    val pas = size.width / n
                    fun cx(i: Int) = pas * (i + 0.5f)
                    val y = 14.dp.toPx()
                    val epaisseur = 4.dp.toPx()
                    val coin = if (carre) androidx.compose.ui.geometry.CornerRadius.Zero
                    else androidx.compose.ui.geometry.CornerRadius(epaisseur / 2)
                    drawRoundRect(vide, androidx.compose.ui.geometry.Offset(cx(0), y - epaisseur / 2),
                        androidx.compose.ui.geometry.Size(cx(n - 1) - cx(0), epaisseur), coin)
                    drawRoundRect(accent, androidx.compose.ui.geometry.Offset(cx(0), y - epaisseur / 2),
                        androidx.compose.ui.geometry.Size(cx(choisi) - cx(0), epaisseur), coin)
                    // Les encoches, sous la piste ; plus longues aux centaines.
                    val haut = 28.dp.toPx()
                    for (i in 0 until n) {
                        val l = (if (carre) 4 else 2).dp.toPx()
                        val h = (if (crans[i] % 100 == 0) 8 else 4).dp.toPx()
                        drawRoundRect(
                            if (i <= choisi) accent else pale,
                            androidx.compose.ui.geometry.Offset(cx(i) - l / 2, haut),
                            androidx.compose.ui.geometry.Size(l, h),
                            if (carre) androidx.compose.ui.geometry.CornerRadius.Zero else androidx.compose.ui.geometry.CornerRadius(l / 2),
                        )
                    }
                    // Le bouton, sur le cran choisi : rond, ou carré (EGA, rétro).
                    val r = 10.dp.toPx()
                    val trait = 2.dp.toPx()
                    val centre = androidx.compose.ui.geometry.Offset(cx(choisi), y)
                    if (carre) {
                        drawRect(bordBouton, androidx.compose.ui.geometry.Offset(centre.x - r, y - r), androidx.compose.ui.geometry.Size(2 * r, 2 * r))
                        drawRect(accent, androidx.compose.ui.geometry.Offset(centre.x - r + trait, y - r + trait),
                            androidx.compose.ui.geometry.Size(2 * (r - trait), 2 * (r - trait)))
                    } else {
                        drawCircle(androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.18f), r + 1.dp.toPx(), centre.copy(y = y + 1.dp.toPx()))
                        drawCircle(bordBouton, r, centre)
                        drawCircle(accent, r - trait, centre)
                    }
                }
                Row(Modifier.fillMaxWidth()) {
                    for (v in crans) {
                        Box(Modifier.weight(1f), contentAlignment = Alignment.Center) {
                            if (v % 100 == 0) Text("$v", color = P.textePale, fontSize = 10.sp)
                        }
                    }
                }
            }
            Spacer(Modifier.width(8.dp))
            Text(
                if (volume > 0) "$volume %" else t("Muet"),
                color = if (volume > 0) P.accent else P.textePale,
                fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
                textAlign = androidx.compose.ui.text.style.TextAlign.End,
                modifier = Modifier.width(56.dp).padding(top = 2.dp),
            )
        }
    }
}

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
            BarreHaut(
                navigationIcon = { IconButton(onClick = retour) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour")) } },
                title = { Text(t("Serveur Jasette"), fontWeight = FontWeight.Bold) },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        var confirmerQuitter by remember { mutableStateOf(false) }
        Column(
            Modifier.fillMaxSize().padding(marges).defilementAvecOmbre().navigationBarsPadding()
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
                    ConnexionMcp(cfg.adresse)
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

/** « Connexion MCP (Claude) » : la connexion MCP du serveur, pour brancher Claude. */
@Composable
private fun ConnexionMcp(adresse: String) {
    LaunchedEffect(adresse) { Depot.actionMcp("etat") }
    val e = Depot.mcp
    val occupe = Depot.mcpOccupe
    val ctx = LocalContext.current
    var copie by remember { mutableStateOf<String?>(null) }
    fun copier(v: String) {
        val cm = ctx.getSystemService(android.content.ClipboardManager::class.java) ?: return
        cm.setPrimaryClip(android.content.ClipData.newPlainText("Jasette", v))
        copie = v
    }
    @Composable
    fun Copiable(v: String) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(v, color = P.texteFort, fontSize = 13.sp, modifier = Modifier.weight(1f))
            TextButton(onClick = { copier(v) }) { Text(if (copie == v) t("Copié") else t("Copier"), color = P.accent) }
        }
    }
    Text(t("Connexion MCP (Claude)"), color = P.texte, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
    Groupe {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (e == null || !e.ouvert) {
                Text(
                    t("Branchez Claude (claude.ai ou Claude Code) sur les messageries de ce serveur : il pourra lister vos conversations, les lire et y chercher. Lire ne marque rien comme lu."),
                    color = P.texte, fontSize = 13.sp,
                )
                Text(t("Fermée par défaut. Un jeton secret protège l'accès ; Fermer le révoque."), color = P.textePale, fontSize = 12.sp)
                Button(
                    onClick = { Depot.actionMcp("ouvrir") }, enabled = !occupe && (e != null || Depot.mcpErreur != null),
                    shape = formeBouton,
                ) { Text(t("Ouvrir ma connexion MCP")) }
            } else {
                val hote = runCatching { ca.jasette.android.hotePort(adresse).first }.getOrDefault("")
                val urls = e.urls(hote)
                Text(t("Adresse du point MCP :"), color = P.texte, fontSize = 13.sp)
                urls.forEach { Copiable(it) }
                Text(t("Jeton (secret, comme un mot de passe) :"), color = P.texte, fontSize = 13.sp)
                Copiable(e.jeton)
                if (!e.ecoute) {
                    Text("${t("Le serveur n'a pas pu ouvrir le port MCP :")} ${e.erreur ?: ""}", color = P.danger, fontSize = 13.sp)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    OutlinedButton(onClick = { Depot.actionMcp("regenerer") }, enabled = !occupe, shape = formeBouton) {
                        Text(t("Régénérer le jeton"))
                    }
                    OutlinedButton(onClick = { Depot.actionMcp("fermer") }, enabled = !occupe, shape = formeBouton) {
                        Text(t("Fermer"), color = P.danger)
                    }
                }
                Text(t("Claude Code"), color = P.texteFort, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
                Text(t("Sur une machine qui joint le serveur, dans un terminal :"), color = P.textePale, fontSize = 12.sp)
                Copiable("claude mcp add --transport http jasette ${urls.firstOrNull() ?: ""} --header \"Authorization: Bearer ${e.jeton}\"")
                Text(t("claude.ai"), color = P.texteFort, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
                Text(
                    t("claude.ai joint le serveur depuis Internet : il lui faut une adresse HTTPS publique qui mène au port MCP. Au choix (à installer vous-même) : Tailscale Funnel (tailscale funnel PORT), Cloudflare Tunnel (cloudflared), ou un mandataire inverse (Caddy, nginx) avec un certificat.")
                        .replace("PORT", e.port.toString()),
                    color = P.textePale, fontSize = 12.sp,
                )
                Text(
                    t("claude.ai n'envoie pas d'en-tête choisi : donnez-lui l'URL secrète ci-dessous (Réglages > Connecteurs > Ajouter un connecteur personnalisé), en remplaçant le nom public. Elle contient le jeton : gardez-la pour vous."),
                    color = P.textePale, fontSize = 12.sp,
                )
                Copiable("https://${t("VOTRE-NOM-PUBLIC")}/mcp/${e.jeton}")
            }
            Depot.mcpErreur?.let { Text(it, color = P.danger, fontSize = 13.sp) }
        }
    }
    if (e != null && e.ouvert) {
        Groupe {
            Row(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 13.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(t("Permettre à Claude d'envoyer des messages"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                    Text(
                        t("Désactivé par défaut. Seulement dans une conversation existante, au plus 5 envois par minute et 30 par heure."),
                        color = P.textePale, fontSize = 13.sp,
                    )
                }
                Spacer(Modifier.width(12.dp))
                Interrupteur(e.envoi) { if (!occupe) Depot.actionMcp("envoi", it) }
            }
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
                    shape = formeChamp,
                    colors = couleursChamp(),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    code, { c -> code = c.filter { it.isDigit() }.take(6) },
                    label = { Text(t("Code à 6 chiffres")) },
                    singleLine = true,
                    // Pas « mot de passe » : un code à usage unique, que les gestionnaires n'ont pas à retenir.
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number, autoCorrectEnabled = false),
                    shape = formeChamp,
                    colors = couleursChamp(),
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
private fun Carte(s: Service, note: String? = null, boutons: @Composable () -> Unit) {
    val etat = Depot.etat(s)
    // Les SMS du téléphone se montrent comme Google Messages (même logo), avec leur propre état.
    val texteEtat = if (s == Service.GOOGLE && Depot.smsActif && etat.associe) t("SMS du téléphone") else etat.texte
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
                    PastilleEtat(etat)
                    Spacer(Modifier.width(6.dp))
                    Text(texteEtat, color = P.textePale, fontSize = 13.sp)
                }
            }
        }
        note?.let { Text(it, color = P.texte, fontSize = 13.sp) }
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
            shape = formeChamp,
            colors = couleursChamp(),
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

/** Réglages → Apparence : les pastilles des onglets Canaux et Archives (comme au bureau). */
@Composable
private fun ChoixPastilles() {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    Groupe {
        LignePastille(
            t("Pastille sur l'onglet Canaux"),
            t("Le nombre d'espaces non lus, et le @ d'une mention, sur l'onglet Canaux."),
            Pastilles.canaux,
        ) { Pastilles.choisirCanaux(ctx, it) }
        LignePastille(
            t("Pastille sur l'onglet Archives"),
            t("Le nombre de conversations archivées non lues sur l'onglet Archives."),
            Pastilles.archives,
        ) { Pastilles.choisirArchives(ctx, it) }
    }
}

@Composable
private fun LignePastille(titre: String, detail: String, oui: Boolean, changer: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable { changer(!oui) }.padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
            Text(detail, color = P.textePale, fontSize = 13.sp)
        }
        Spacer(Modifier.width(12.dp))
        Interrupteur(oui) { changer(it) }
    }
}

/**
 * Réglages → Conversations : « Garder les conversations archivées hors de la
 * boîte de réception » (désactivé par défaut ; marque `garder_archives`,
 * partagée par le serveur Jasette s'il y en a un).
 */
@Composable
private fun ChoixGarderArchives() {
    val oui = ca.jasette.android.Marques.garderArchives
    Groupe {
        Row(
            Modifier.fillMaxWidth().clickable { ca.jasette.android.Marques.choisirGarderArchives(!oui) }
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(t("Garder les conversations archivées hors de la boîte de réception"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                Text(t("Un nouveau message ne les ramène plus dans la boîte de réception."), color = P.textePale, fontSize = 13.sp)
            }
            Spacer(Modifier.width(12.dp))
            Interrupteur(oui) { ca.jasette.android.Marques.choisirGarderArchives(it) }
        }
    }
}

/**
 * Réglages → Conversations : « Afficher qui est en ligne » (activé par défaut ;
 * marque `presence_en_ligne`, partagée par le serveur Jasette s'il y en a un).
 */
@Composable
private fun ChoixPresence() {
    val oui = ca.jasette.android.Marques.presenceEnLigne
    Groupe {
        Row(
            Modifier.fillMaxWidth().clickable { ca.jasette.android.Marques.choisirPresenceEnLigne(!oui) }
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(t("Afficher qui est en ligne et qui écrit"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                Text(t("Point vert sur les contacts actifs et « … » quand on vous écrit."), color = P.textePale, fontSize = 13.sp)
            }
            Spacer(Modifier.width(12.dp))
            Interrupteur(oui) { ca.jasette.android.Marques.choisirPresenceEnLigne(it) }
        }
    }
}

/** Le nom d'un cran de [CACHER_APRES_JOURS] : « Tout afficher », « 7 jours », « 6 mois »… */
private fun nomCacherApres(jours: Int): String = when (jours) {
    0 -> t("Tout afficher")
    182 -> t("6 mois")
    365 -> t("1 an")
    else -> "$jours ${t("jours")}"
}

/**
 * Réglages → Conversations : « Cacher les conversations plus anciennes de la
 * boîte de réception », un curseur à crans (tout afficher par défaut ; marque
 * `cacher_apres_jours`, partagée par le serveur Jasette s'il y en a un).
 */
@Composable
private fun ChoixCacherApres() {
    val crans = ca.jasette.android.CACHER_APRES_JOURS
    val jours = ca.jasette.android.Marques.cacherApresJours
    // Une valeur hors crans (venue d'ailleurs) : le cran le plus proche.
    val indice = crans.indices.minByOrNull { kotlin.math.abs(crans[it] - jours) } ?: 0
    Groupe {
        Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 10.dp, bottom = 4.dp)) {
            Row(verticalAlignment = Alignment.Top) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(
                        t("Cacher les conversations plus anciennes de la boîte de réception"),
                        color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
                    )
                    Text(
                        t("Selon leur dernier message. Les épinglées et les non lues restent ; Favoris, Archives et la recherche les montrent toujours."),
                        color = P.textePale, fontSize = 13.sp,
                    )
                }
                Spacer(Modifier.width(12.dp))
                Text(
                    nomCacherApres(crans[indice]), color = if (jours > 0) P.accent else P.textePale,
                    fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
                )
            }
            androidx.compose.material3.Slider(
                value = indice.toFloat(),
                onValueChange = { ca.jasette.android.Marques.choisirCacherApresJours(crans[it.roundToInt().coerceIn(0, crans.lastIndex)]) },
                valueRange = 0f..crans.lastIndex.toFloat(),
                steps = crans.size - 2,
                colors = androidx.compose.material3.SliderDefaults.colors(
                    thumbColor = P.accent, activeTrackColor = P.accent, inactiveTrackColor = P.filet,
                    activeTickColor = P.surAccent.copy(alpha = 0.6f), inactiveTickColor = P.textePale.copy(alpha = 0.5f),
                ),
            )
        }
    }
}

/** Réglages → Apparence : « Aperçus des liens » (activé par défaut ; ceux du service restent affichés). */
@Composable
private fun ChoixApercus() {
    val oui = ca.jasette.android.Apercus.actifs
    Groupe {
        Row(
            Modifier.fillMaxWidth().clickable { ca.jasette.android.Apercus.choisir(!oui) }
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(t("Aperçus des liens"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                Text(
                    t("Jasette va chercher l'image et le titre des liens (par le serveur Jasette s'il y en a un). Les aperçus envoyés par le service restent affichés."),
                    color = P.textePale, fontSize = 13.sp,
                )
            }
            Spacer(Modifier.width(12.dp))
            Interrupteur(oui) { ca.jasette.android.Apercus.choisir(it) }
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
        P.sirene -> couleur(0x808080)
        P.live -> couleur(0x94bbe8)
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
        androidx.compose.material3.HorizontalDivider(color = P.filet)
        // L'aperçu Live : le ciel, la barre bleue brillante, une photo au cadre vert.
        LigneTheme(
            Theme.LIVE, "Live", t("Bleu ciel brillant, photos au cadre vert, comme une messagerie des années 2000."),
            Modifier.border(1.dp, bordVignette, forme(14.dp)).background(
                androidx.compose.ui.graphics.Brush.linearGradient(
                    listOf(couleur(0x5f9fe8), couleur(0x9cc7f4), couleur(0xd7e9fb)),
                    androidx.compose.ui.geometry.Offset.Zero, androidx.compose.ui.geometry.Offset(20f, 160f),
                ),
            ),
        ) {
            Column {
                androidx.compose.foundation.layout.Box(
                    Modifier.fillMaxWidth().height(13.dp).background(
                        androidx.compose.ui.graphics.Brush.verticalGradient(
                            0f to couleur(0x7db6f2), 0.5f to couleur(0x3a86e0), 0.5f to couleur(0x2a6fcf), 1f to couleur(0x4d97ea),
                        ),
                    ),
                )
                Row(Modifier.padding(5.dp), verticalAlignment = Alignment.CenterVertically) {
                    androidx.compose.foundation.layout.Box(
                        Modifier.size(16.dp).clip(androidx.compose.foundation.shape.RoundedCornerShape(3.dp))
                            .background(androidx.compose.ui.graphics.Brush.verticalGradient(listOf(couleur(0xb7f08f), couleur(0x4bb52a))))
                            .padding(2.dp).background(couleur(0xf08a5d)),
                    )
                    Spacer(Modifier.width(4.dp))
                    Column(verticalArrangement = Arrangement.spacedBy(3.dp)) {
                        androidx.compose.foundation.layout.Box(Modifier.width(18.dp).height(3.dp).background(couleur(0x12335e)))
                        androidx.compose.foundation.layout.Box(Modifier.width(13.dp).height(3.dp).background(couleur(0x4d6c92)))
                    }
                }
            }
        }
        androidx.compose.material3.HorizontalDivider(color = P.filet)
        // L'aperçu Sirène : une fenêtre grise de Windows 2000, barre de titre verte, liste en creux.
        LigneTheme(
            Theme.SIRENE, "Sirène", t("Gris de Windows 2000, barre de titre verte, coins carrés."),
            Modifier.background(couleur(GRIS_2000)).relief(enfonce = false),
        ) {
            Column(Modifier.padding(2.dp)) {
                androidx.compose.foundation.layout.Box(
                    Modifier.fillMaxWidth().height(10.dp).background(
                        androidx.compose.ui.graphics.Brush.horizontalGradient(listOf(couleur(0x0d5c1f), couleur(0x3aa64a))),
                    ),
                )
                Column(
                    Modifier.padding(3.dp).fillMaxWidth().background(androidx.compose.ui.graphics.Color.White)
                        .relief(enfonce = true, epaisseur = 1.dp).padding(start = 4.dp, top = 4.dp, bottom = 4.dp),
                    verticalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    for ((l, c) in listOf(20 to 0xc01818L, 30 to 0x404040L, 16 to 0x1838c0L, 26 to 0x404040L)) {
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
                                        P.sirene -> couleur(0x808080)
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
 * Réglages → Apparence → Grosseur du texte des messages : un gradateur à crans
 * et un aperçu en direct (une bulle reçue, une envoyée). Propre à cet appareil.
 */
@Composable
private fun ChoixTailleMessages() {
    val crans = listOf(0.85f, 0.92f, 1f, 1.1f, 1.2f, 1.32f, 1.45f, 1.6f)
    val actuelle = ca.jasette.android.Marques.tailleMessages
    val indice = crans.indices.minByOrNull { kotlin.math.abs(crans[it] - actuelle) } ?: 2
    Text(t("Grosseur du texte des messages"), color = P.textePale, fontSize = 13.sp)
    Groupe {
        Column(Modifier.fillMaxWidth().padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("A", color = P.textePale, fontSize = 12.sp)
                androidx.compose.material3.Slider(
                    value = indice.toFloat(),
                    onValueChange = { ca.jasette.android.Marques.choisirTailleMessages(crans[it.roundToInt().coerceIn(0, crans.lastIndex)]) },
                    valueRange = 0f..crans.lastIndex.toFloat(),
                    steps = crans.size - 2,
                    colors = androidx.compose.material3.SliderDefaults.colors(
                        thumbColor = P.accent, activeTrackColor = P.accent, inactiveTrackColor = P.filet,
                        activeTickColor = P.surAccent.copy(alpha = 0.6f), inactiveTickColor = P.textePale.copy(alpha = 0.5f),
                    ),
                    modifier = Modifier.weight(1f).padding(horizontal = 10.dp),
                )
                Text("A", color = P.textePale, fontSize = 20.sp)
            }
            Column(
                // Le fond du fil ; en Verre, la teinte du verre (le fil est un voile blanc,
                // la bulle reçue ne s'y verrait pas sur la carte blanche).
                Modifier.fillMaxWidth().clip(forme(14.dp)).background(if (P.clair && !P.retro) P.filet else P.fil)
                    .let { if (P.retro) it.border(1.dp, P.traitFin, forme(14.dp)) else it }.padding(12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                val taille = (15f * actuelle).sp
                val ligne = (20f * actuelle).sp
                // Live et Sirène : le fil sans bulles.
                if (P.retro) {
                    LigneExemple("Camille", moi = false, t("On se voit au chalet vendredi ?"), taille, "19:24")
                    LigneExemple(null, moi = true, t("Oui ! J'apporte les guimauves."), taille, "19:25")
                    return@Column
                }
                Text(
                    t("On se voit au chalet vendredi ?"), color = P.texteFort, fontSize = taille, lineHeight = ligne,
                    // Comme dans le fil : le liseré de la bulle reçue (Verre, EGA).
                    modifier = Modifier.clip(forme(18.dp)).background(P.surface)
                        .let { if (P.bordBulle.alpha > 0f) it.border(1.dp, P.bordBulle, forme(18.dp)) else it }
                        .padding(horizontal = 13.dp, vertical = 8.dp),
                )
                Text(
                    t("Oui ! J'apporte les guimauves."), color = P.surAccent, fontSize = taille, lineHeight = ligne,
                    modifier = Modifier.align(Alignment.End).clip(forme(18.dp)).background(P.degrade)
                        .padding(horizontal = 13.dp, vertical = 8.dp),
                )
            }
            Text(t("Texte des messages : %d %%").format((actuelle * 100).roundToInt()), color = P.textePale, fontSize = 12.sp)
            // Juste sous le gradateur : la liste des conversations plus grosse.
            val ctx = androidx.compose.ui.platform.LocalContext.current
            Row(
                Modifier.fillMaxWidth().clip(forme(10.dp)).clickable { Taille.choisir(ctx, !Taille.listeGrosse) }
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                androidx.compose.material3.Checkbox(
                    modifier = Modifier.padding(end = 4.dp),
                    checked = Taille.listeGrosse, onCheckedChange = { Taille.choisir(ctx, it) },
                    colors = androidx.compose.material3.CheckboxDefaults.colors(
                        checkedColor = P.accent, uncheckedColor = P.textePale, checkmarkColor = P.surAccent,
                    ),
                )
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(
                        t("Liste des conversations plus grosse"), color = P.texteFort, fontSize = 15.sp,
                        lineHeight = 18.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        t("Noms, aperçus et Head plus gros"), color = P.textePale, fontSize = 12.sp,
                        lineHeight = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
    Spacer(Modifier.height(4.dp))
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
        // Les quatre choix sur une seule ligne, qui se partagent la largeur.
        Row(
            Modifier.fillMaxWidth().padding(12.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            for (g in ca.jasette.android.Marques.GRILLES) {
                val actif = g == choisie
                Column(
                    Modifier.weight(1f).clip(forme(12.dp))
                        .border(if (actif) 2.dp else 1.dp, if (actif) P.accent else P.filet, forme(12.dp))
                        .clickable { ca.jasette.android.Marques.choisirHeadGrille(g) }
                        .padding(vertical = 8.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    SchemaGrille(g, if (actif) P.accent else P.textePale)
                    Spacer(Modifier.height(6.dp))
                    Text(
                        t("%d de large").format(ca.jasette.android.Marques.colonnes(g)),
                        color = if (actif) P.texteFort else P.texte, fontSize = 11.sp,
                        fontWeight = if (actif) FontWeight.SemiBold else FontWeight.Normal,
                        maxLines = 1, softWrap = false,
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
    androidx.compose.foundation.Canvas(Modifier.size(width = 50.dp, height = 30.dp)) {
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
