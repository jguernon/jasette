package ca.jasette.android.ui

import android.app.Activity
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.FloatingActionButton
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.layout.imePadding
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.focus.focusRequester
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.key
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Marques
import ca.jasette.android.Service
import ca.jasette.android.t
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Phone

/** Les écrans de l'app (pas de bibliothèque de navigation : une pile toute simple). */
sealed interface Ecran {
    /** La liste, avec la conversation ouverte (`ouverte` dans [App]) en plein écran ou à droite. */
    data object Liste : Ecran
    data object Reglages : Ecran
    /** Réglages → Serveur Jasette (aussi ouvert par « Client » à l'accueil). */
    data object Serveur : Ecran
    /** La fenêtre de connexion d'un service ; [plateforme] : messenger, facebook ou instagram. */
    data class Connexion(val service: Service, val plateforme: String) : Ecran
}

/** Ce que montre la liste (`enum Vue` du bureau). */
sealed interface Vue {
    data object Tout : Vue
    data object Favoris : Vue
    data object Archives : Vue
    data class DuService(val service: Service) : Vue
}

/** Écran large (Fold déplié, tablette) : le rail à gauche plutôt que la barre du bas. */
const val LARGEUR_RAIL = 600

@Composable
fun App(activite: Activity) {
    var pile by remember { mutableStateOf(listOf<Ecran>(Ecran.Liste)) }
    var vue by remember { mutableStateOf<Vue>(Vue.Tout) }
    // La conversation ouverte : à droite en deux volets, sinon en plein écran.
    // Elle survit au pliage ou au dépliage (l'activité n'est pas recréée).
    var ouverte by remember { mutableStateOf<String?>(null) }
    val ecran = pile.last()
    val largeur = LocalConfiguration.current.screenWidthDp
    val large = largeur >= LARGEUR_RAIL
    val volets = large && Marques.deuxVolets
    fun aller(e: Ecran) {
        pile = pile + e
    }
    fun retour() {
        if (pile.size > 1) pile = pile.dropLast(1) else if (ouverte != null && !volets) ouverte = null
    }
    BackHandler(enabled = pile.size > 1 || (ouverte != null && !volets)) { retour() }

    fun choisirVue(v: Vue) {
        vue = v
        pile = listOf(Ecran.Liste)
        if (!volets) ouverte = null
    }

    // Une notification touchée (ou une nouvelle conversation) : on ouvre le fil.
    val aOuvrir = Depot.aOuvrir
    LaunchedEffect(aOuvrir) {
        if (aOuvrir != null) {
            pile = listOf(Ecran.Liste)
            vue = Vue.Tout
            ouverte = aOuvrir
            Depot.aOuvrir = null
        }
    }

    val liste: @Composable () -> Unit = {
        Liste(
            vue = vue,
            choisie = if (volets) ouverte else null,
            large = large,
            ouvrir = { ouverte = it },
            retiree = { if (ouverte == it) ouverte = null },
            choisirVue = ::choisirVue,
            reglages = { aller(Ecran.Reglages) },
            serveur = { aller(Ecran.Serveur) },
            connecter = { s, p -> aller(Ecran.Connexion(s, p)) },
        )
    }

    Row(Modifier.fillMaxSize().background(P.fond)) {
        if (large && ecran !is Ecran.Connexion) {
            Rail(
                vue = vue,
                enReglages = ecran == Ecran.Reglages || ecran == Ecran.Serveur,
                choisir = ::choisirVue,
                reglages = { if (ecran != Ecran.Reglages) pile = listOf(Ecran.Liste, Ecran.Reglages) },
            )
        }
        Box(Modifier.weight(1f).fillMaxHeight()) {
            when (ecran) {
                Ecran.Liste -> {
                    val id = ouverte
                    when {
                        volets -> Row(Modifier.fillMaxSize()) {
                            // La liste au milieu (320 à 380 dp), le fil prend le reste.
                            val l = (largeur * 0.4f).coerceIn(320f, 380f).dp
                            Box(Modifier.width(l).fillMaxHeight()) { liste() }
                            VerticalDivider(color = P.traitFin)
                            Box(Modifier.weight(1f).fillMaxHeight().background(P.fil)) {
                                if (id != null) {
                                    key(id) { Fil(activite, id, retour = { ouverte = null }, volet = true, retiree = { ouverte = null }) }
                                } else {
                                    VoletVide()
                                }
                            }
                        }
                        id != null -> key(id) { Fil(activite, id, retour = { ouverte = null }, retiree = { ouverte = null }) }
                        else -> liste()
                    }
                }
                Ecran.Reglages -> Reglages(
                    retour = ::retour,
                    connecter = { s, p -> aller(Ecran.Connexion(s, p)) },
                    serveur = { aller(Ecran.Serveur) },
                )
                Ecran.Serveur -> ServeurJasette(retour = ::retour)
                is Ecran.Connexion -> Connexion(ecran.service, ecran.plateforme, fini = ::retour)
            }
        }
    }
}

/** Le volet de droite sans conversation ouverte. */
@Composable
private fun VoletVide() {
    Column(
        Modifier.fillMaxSize(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            androidx.compose.ui.res.painterResource(ca.jasette.android.R.drawable.ic_notif), null,
            tint = P.choisi, modifier = Modifier.size(72.dp),
        )
        Spacer(Modifier.size(14.dp))
        Text(t("Choisissez une conversation"), color = P.textePale, fontSize = 15.sp)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Liste(
    vue: Vue,
    choisie: String?,
    large: Boolean,
    ouvrir: (String) -> Unit,
    retiree: (String) -> Unit,
    choisirVue: (Vue) -> Unit,
    reglages: () -> Unit,
    serveur: () -> Unit,
    connecter: (Service, String) -> Unit,
) {
    var reorganiser by remember { mutableStateOf(false) }
    var filtre by remember { mutableStateOf("") }
    var nouveau by remember { mutableStateOf(false) }
    LaunchedEffect(vue) {
        reorganiser = false
        if (vue == Vue.Archives) Depot.chargerArchives()
    }
    val titre = when (vue) {
        Vue.Tout -> t("Boîte de réception")
        Vue.Favoris -> t("Favoris")
        Vue.Archives -> t("Archives")
        is Vue.DuService -> vue.service.titre
    }
    Scaffold(
        containerColor = P.fond,
        topBar = {
            TopAppBar(
                title = {
                    Text(titre, fontWeight = FontWeight.Bold, fontSize = 22.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                },
                actions = {
                    if (vue == Vue.Favoris && Marques.favoris.size > 1) {
                        IconButton(onClick = { reorganiser = !reorganiser }) {
                            if (reorganiser) {
                                Icon(Icons.Filled.Check, t("Terminé"), tint = P.accent)
                            } else {
                                Icon(Icones.reorganiser, t("Réorganiser"), tint = P.icone)
                            }
                        }
                    }
                    // Sur grand écran, les réglages sont en bas du rail.
                    if (!large) {
                        IconButton(onClick = reglages) { Icon(Icons.Filled.Settings, t("Réglages"), tint = P.icone) }
                    }
                },
                colors = barreCouleurs(),
            )
        },
        bottomBar = { if (!large) BarreBas(vue, choisirVue) },
        floatingActionButton = {
            if (!reorganiser) {
                FloatingActionButton(onClick = { nouveau = true }, containerColor = P.accent, contentColor = P.texteFort) {
                    Icon(Icons.Filled.Add, t("Nouveau message"))
                }
            }
        },
    ) { marges ->
        val source = if (vue == Vue.Archives) Depot.archives else Depot.convs
        val rang = { c: Conv -> Marques.epingles.indexOf(c.id).let { if (it < 0) Int.MAX_VALUE else it } }
        // La barre de recherche filtre par nom, membres et aperçu (comme `correspond`).
        val f = ca.jasette.android.plier(filtre.trim())
        val filtrees = source.filter { c ->
            val dansVue = when (vue) {
                Vue.Favoris -> Marques.estFavori(c.id)
                is Vue.DuService -> c.service == vue.service
                else -> true
            }
            dansVue && (f.isEmpty() || ca.jasette.android.plier(c.name).contains(f) ||
                ca.jasette.android.plier(c.preview).contains(f) ||
                c.members.any { ca.jasette.android.plier(it).contains(f) })
        }
        // Les épinglées d'abord, dans l'ordre d'épinglage ; les favoris dans leur ordre.
        val visibles = if (vue == Vue.Favoris) {
            filtrees.sortedBy { Marques.favoris.indexOf(it.id) }
        } else {
            filtrees.sortedBy(rang)
        }
        val nbEpingles = if (vue == Vue.Favoris) 0 else visibles.count { rang(it) != Int.MAX_VALUE }
        // Le Head : la grille des favoris en haut de la boîte de réception.
        val head = if (vue == Vue.Tout && f.isEmpty()) Marques.head.mapNotNull { id -> Depot.convs.firstOrNull { it.id == id } } else emptyList()
        val reorg = reorganiser && vue == Vue.Favoris
        LazyColumn(Modifier.fillMaxSize().padding(marges)) {
            item { ChampFiltre(filtre) { filtre = it } }
            item { Bannieres(reglages) }
            if (vue == Vue.Tout && accueilVisible()) item { Accueil(serveur, connecter) }
            val vide = when {
                visibles.isNotEmpty() -> null
                f.isNotEmpty() -> t("Aucun résultat")
                vue == Vue.Archives && Depot.archivesEnAttente -> t("Chargement des archives…")
                vue == Vue.Archives -> t("Aucune conversation archivée")
                vue == Vue.Favoris && Marques.favoris.isEmpty() ->
                    t("Aucun favori : ajoutez-en par un appui long sur une conversation.")
                accueilVisible() -> null
                source.isEmpty() -> {
                    val connecte = Service.entries.any { Depot.etat(it).associe }
                    if (connecte) t("Chargement des conversations…") else t("Aucun compte connecté.")
                }
                else -> t("Aucune conversation")
            }
            if (vide != null) {
                item { Text(vide, color = P.textePale, modifier = Modifier.fillMaxWidth().padding(32.dp)) }
            }
            if (head.isNotEmpty()) item(key = "head") { Head(head, choisie, ouvrir, retiree) }
            itemsIndexed(visibles, key = { _, c -> c.id }) { i, c ->
                Column {
                    // Un peu d'air sous les épinglées.
                    if (nbEpingles > 0 && i == nbEpingles) {
                        HorizontalDivider(Modifier.padding(horizontal = 16.dp, vertical = 6.dp), color = P.traitFin)
                    }
                    Rangee(
                        c,
                        choisie = c.id == choisie,
                        epinglee = i < nbEpingles,
                        badge = vue !is Vue.DuService,
                        reorganiser = reorg,
                        retiree = retiree,
                    ) { ouvrir(c.id) }
                }
            }
            // De la place pour le bouton flottant.
            item { Spacer(Modifier.size(80.dp)) }
        }
    }
    if (nouveau) NouveauMessage(fermer = { nouveau = false }, ouvrir = ouvrir)
}

/** La barre de recherche en haut de la liste. */
@Composable
private fun ChampFiltre(texte: String, changer: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp).clip(RoundedCornerShape(20.dp))
            .background(P.surface).padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Filled.Search, null, tint = P.textePale, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
            if (texte.isEmpty()) Text(t("Rechercher"), color = P.textePale, fontSize = 15.sp)
            BasicTextField(
                texte, changer, singleLine = true,
                textStyle = TextStyle(color = P.texteFort, fontSize = 15.sp),
                cursorBrush = SolidColor(P.accent),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (texte.isNotEmpty()) {
            Icon(
                Icons.Filled.Close, t("Effacer"), tint = P.textePale,
                modifier = Modifier.size(18.dp).clickable { changer("") },
            )
        }
    }
}

/**
 * Nouveau message (`suggestions()` du bureau) : « À : » propose les
 * conversations connues, les personnes trouvées par Messenger et Instagram
 * (commande `search`), et un SMS par Google Messages au numéro tapé. Chaque
 * suggestion porte le logo de son service, pour choisir quand c'est ambigu.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NouveauMessage(fermer: () -> Unit, ouvrir: (String) -> Unit) {
    var texte by remember { mutableStateOf("") }
    val focus = remember { androidx.compose.ui.focus.FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    LaunchedEffect(texte) { Depot.chercherContacts(texte) }
    fun finir() {
        Depot.chercherContacts("")
        fermer()
    }
    val q = ca.jasette.android.plier(texte.trim())
    val numero = ca.jasette.android.estNumero(texte.trim())
    androidx.compose.ui.window.Dialog(
        onDismissRequest = ::finir,
        properties = androidx.compose.ui.window.DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Scaffold(
            containerColor = P.fond,
            topBar = {
                TopAppBar(
                    navigationIcon = { IconButton(onClick = ::finir) { Icon(Icons.Filled.Close, t("Fermer")) } },
                    title = { Text(t("Nouveau message"), fontWeight = FontWeight.Bold) },
                    colors = barreCouleurs(),
                )
            },
        ) { marges ->
            LazyColumn(Modifier.fillMaxSize().padding(marges).imePadding()) {
                item {
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp).clip(RoundedCornerShape(14.dp))
                            .background(P.surface).padding(horizontal = 14.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(t("À :"), color = P.textePale, fontSize = 16.sp)
                        Spacer(Modifier.width(10.dp))
                        Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
                            if (texte.isEmpty()) Text(t("Nom ou numéro de téléphone"), color = P.textePale, fontSize = 16.sp)
                            BasicTextField(
                                texte, { texte = it }, singleLine = true,
                                textStyle = TextStyle(color = P.texteFort, fontSize = 16.sp),
                                cursorBrush = SolidColor(P.accent),
                                modifier = Modifier.fillMaxWidth().focusRequester(focus),
                            )
                        }
                    }
                }
                if (q.isEmpty()) return@LazyColumn
                // Les conversations qui existent déjà (pas les groupes).
                val existantes = (Depot.convs + Depot.archives).filter { c ->
                    !c.group && (ca.jasette.android.plier(c.name).contains(q) ||
                        c.members.any { ca.jasette.android.plier(it).contains(q) })
                }.distinctBy { it.id }.take(6)
                items(existantes, key = { "c-" + it.id }) { c ->
                    Suggestion(c.name.ifEmpty { t("Sans nom") }, c.service.titre, c.service, c) {
                        finir()
                        ouvrir(c.id)
                    }
                }
                // Les personnes trouvées par Messenger ou Instagram, sans conversation ici.
                val trouvees = Depot.resultatsMeta.filter { (id, _) -> Depot.conv(id) == null }.take(8)
                items(trouvees, key = { "m-" + it.first }) { (id, nom) ->
                    val s = Service.de(id)
                    val detail = if (s == Service.INSTAGRAM) t("Nouvelle conversation Instagram") else t("Nouvelle conversation Messenger")
                    Suggestion(nom, detail, s, null) {
                        Depot.nouvelleConversation(s, id)
                        finir()
                    }
                }
                // Un numéro : un SMS par Google Messages.
                if (numero && Depot.etat(Service.GOOGLE).associe) {
                    item(key = "sms") {
                        Suggestion(texte.trim(), t("Nouveau message par Google Messages"), Service.GOOGLE, null) {
                            Depot.nouvelleConversation(Service.GOOGLE, texte.trim())
                            finir()
                        }
                    }
                }
                if (existantes.isEmpty() && trouvees.isEmpty() && !numero) {
                    item {
                        val cherche = q.length >= 2 && Service.META.any { Depot.pret(it) }
                        Text(
                            if (cherche) t("Recherche…") else t("Aucun résultat"),
                            color = P.textePale, modifier = Modifier.padding(24.dp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun Suggestion(nom: String, detail: String, s: Service, conv: Conv?, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (conv != null) {
            Avatar(conv, 42.dp)
        } else {
            androidx.compose.foundation.Image(
                androidx.compose.ui.res.painterResource(iconeService(s)), null,
                Modifier.size(42.dp).clip(formeLogo(42.dp)),
            )
        }
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text(nom, color = P.texteFort, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(detail, color = P.textePale, fontSize = 13.sp, maxLines = 1)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun barreCouleurs() = TopAppBarDefaults.topAppBarColors(
    containerColor = P.fond, scrolledContainerColor = P.fond,
    titleContentColor = P.texteFort, navigationIconContentColor = P.icone, actionIconContentColor = P.icone,
)

/** Les bandeaux en haut de la liste : erreurs, association Google, NIP Messenger, aucun compte. */
@Composable
private fun Bannieres(reglages: () -> Unit) {
    Column(Modifier.padding(horizontal = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Depot.erreur?.let { e ->
            Bandeau(couleur = P.danger) {
                Text(e, color = P.texteFort, fontSize = 14.sp, modifier = Modifier.weight(1f))
                IconButton(onClick = { Depot.erreur = null }) { Icon(Icons.Filled.Close, t("Fermer"), tint = P.icone) }
            }
        }
        Depot.emoji?.let { e ->
            Bandeau(couleur = P.accent) {
                Text(e, fontSize = 40.sp)
                Spacer(Modifier.width(12.dp))
                Text(
                    t("Touche cet emoji dans Google Messages sur ton téléphone"),
                    color = P.texteFort, modifier = Modifier.weight(1f),
                )
            }
        }
        if (Depot.besoinNip) {
            Bandeau(couleur = P.accent, onClick = reglages) {
                Icon(Icons.Filled.Lock, null, tint = P.accent)
                Spacer(Modifier.width(12.dp))
                Text(
                    t("Messenger : entre ton NIP pour restaurer l'historique chiffré"),
                    color = P.texteFort, modifier = Modifier.weight(1f),
                )
            }
        }
        // Mode client sans aucun service connecté sur le serveur.
        if (Depot.serveur != null && aucunCompte()) {
            Bandeau(couleur = P.accent, onClick = reglages) {
                Text(
                    t("Aucun service n'est connecté sur ton serveur Jasette. Connecte-les sur le serveur."),
                    color = P.texteFort, modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
fun Bandeau(
    couleur: androidx.compose.ui.graphics.Color,
    onClick: (() -> Unit)? = null,
    contenu: @Composable androidx.compose.foundation.layout.RowScope.() -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().padding(top = 4.dp).clip(RoundedCornerShape(14.dp)).background(P.surface)
            .let { if (onClick != null) it.clickable(onClick = onClick) else it }
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(couleur))
        Spacer(Modifier.width(12.dp))
        contenu()
    }
}

/** Aucun compte connecté (et tous les ponts ont dit leur état). */
private fun aucunCompte() =
    Service.entries.none { Depot.etat(it).associe || Depot.etat(it) == ca.jasette.android.Etat.DEMARRAGE }

/** L'accueil s'affiche tant qu'aucun compte n'est connecté, hors mode client. */
private fun accueilVisible() = Depot.serveur == null && aucunCompte()

/**
 * L'accueil du premier lancement, comme au bureau : d'abord le mode (Client,
 * recommandé sur Android, ou Autonome), puis, en Autonome, le premier service.
 * Un sélecteur de langue en haut : le changer met tout l'accueil à jour.
 */
@Composable
private fun Accueil(serveur: () -> Unit, connecter: (Service, String) -> Unit) {
    Column(
        Modifier.fillMaxWidth().padding(12.dp).clip(RoundedCornerShape(18.dp)).background(P.surface).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                t("Bienvenue dans Jasette"), color = P.texteFort, fontSize = 20.sp, fontWeight = FontWeight.Bold,
                modifier = Modifier.weight(1f),
            )
        }
        SelecteurLangue()
        if (!Depot.accueilAutonome) {
            Text(t("Comment utiliser Jasette ici ?"), color = P.texte, fontSize = 14.sp)
            Groupe {
                ChoixRangee(
                    icone = Icons.Filled.Home,
                    titre = t("Client"),
                    badge = t("Recommandé"),
                    detail = t("Se brancher à ton serveur Jasette, qui garde les sessions. Sur Android, Google Messages ne peut se connecter que de cette façon."),
                    actif = false,
                    chevron = true,
                ) {
                    Depot.choixClient = true
                    Depot.erreurServeur = null
                    serveur()
                }
                androidx.compose.material3.HorizontalDivider(color = P.fond)
                ChoixRangee(
                    icone = Icons.Filled.Phone,
                    titre = t("Autonome"),
                    detail = t("Ce téléphone se connecte lui-même à tes services (Messenger, Instagram)."),
                    actif = false,
                    chevron = true,
                ) { Depot.accueilAutonome = true }
            }
        } else {
            Text(t("Connecte un premier service :"), color = P.texte, fontSize = 14.sp)
            Groupe {
                for ((i, s) in Service.entries.withIndex()) {
                    if (i > 0) androidx.compose.material3.HorizontalDivider(color = P.fond)
                    Row(
                        Modifier.fillMaxWidth().clickable { connecter(s, s.cle) }.padding(horizontal = 16.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        androidx.compose.foundation.Image(
                            androidx.compose.ui.res.painterResource(iconeService(s)), null,
                            Modifier.size(34.dp).clip(formeLogo(34.dp)),
                        )
                        Spacer(Modifier.width(14.dp))
                        Text(s.titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                        Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = P.textePale)
                    }
                }
            }
            TextButton(onClick = { Depot.accueilAutonome = false }) { Text(t("Changer de mode"), color = P.textePale) }
        }
    }
}
