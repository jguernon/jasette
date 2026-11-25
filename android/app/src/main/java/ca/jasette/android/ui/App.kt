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
import androidx.compose.material.icons.filled.Search
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
import ca.jasette.android.ContactTrouve
import ca.jasette.android.Contacts
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Marques
import ca.jasette.android.Service
import ca.jasette.android.t
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Phone
import kotlinx.coroutines.launch

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

    // Les contacts du téléphone : relus à chaque retour au premier plan (sans
    // jamais demander la permission ici ; seulement s'elle est déjà accordée).
    androidx.lifecycle.compose.LifecycleEventEffect(androidx.lifecycle.Lifecycle.Event.ON_START) {
        Contacts.auPremierPlan(activite)
    }

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

    // L'état du flou (Haze) partagé par toute l'app ; le fond de la fenêtre en est la
    // première source (aplat et halos du verre, sous les barres floutées).
    val flou = dev.chrisbanes.haze.rememberHazeState()
    Box(Modifier.fillMaxSize()) {
    Box(Modifier.fillMaxSize().sourceFlou(flou, FLOU_FOND, z = 0f).fondFenetre())
    androidx.compose.runtime.CompositionLocalProvider(LocalFlou provides flou) {
    Row(Modifier.fillMaxSize()) {
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
                                    key(id) {
                                        Fil(
                                            activite, id, retour = { ouverte = null }, volet = true, retiree = { ouverte = null },
                                            reglages = { aller(Ecran.Reglages) },
                                        )
                                    }
                                } else {
                                    VoletVide()
                                }
                            }
                        }
                        id != null -> key(id) {
                            Fil(activite, id, retour = { ouverte = null }, retiree = { ouverte = null }, reglages = { aller(Ecran.Reglages) })
                        }
                        else -> liste()
                    }
                }
                Ecran.Reglages -> Reglages(
                    retour = ::retour,
                    connecter = { s, p -> aller(Ecran.Connexion(s, p)) },
                    serveur = { aller(Ecran.Serveur) },
                )
                Ecran.Serveur -> ServeurJasette(retour = ::retour)
                // WhatsApp et Signal : code QR ou code à taper ; les autres : la fenêtre web (X : puis son code).
                // Telegram : numéro et code, ou code QR (Telegram.kt).
                is Ecran.Connexion -> if (ecran.service == Service.TELEGRAM) {
                    AssociationTelegram(fini = ::retour)
                } else if (ecran.service == Service.WHATSAPP || ecran.service == Service.SIGNAL) {
                    Association(ecran.service, ecran.plateforme, fini = ::retour)
                } else {
                    Connexion(ecran.service, ecran.plateforme, fini = ::retour)
                }
            }
        }
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
    // La barre du bas des glissements : « Conversation archivée · Annuler », etc.
    val barreMessages = remember { androidx.compose.material3.SnackbarHostState() }
    val portee = androidx.compose.runtime.rememberCoroutineScope()
    fun annoncer(message: String, annuler: (() -> Unit)?) {
        portee.launch {
            barreMessages.currentSnackbarData?.dismiss()
            val r = barreMessages.showSnackbar(
                message, actionLabel = if (annuler != null) t("Annuler") else null,
                duration = androidx.compose.material3.SnackbarDuration.Short,
            )
            if (r == androidx.compose.material3.SnackbarResult.ActionPerformed) annuler?.invoke()
        }
    }
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
        containerColor = P.ecran,
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
        // La barre du bas flotte par-dessus la liste, floutée (voir BarreBas).
        bottomBar = { if (!large) BarreBas(vue, choisirVue) },
        snackbarHost = {
            androidx.compose.material3.SnackbarHost(barreMessages) { d ->
                androidx.compose.material3.Snackbar(
                    d, shape = forme(12.dp), containerColor = P.menu, contentColor = P.texteFort,
                    actionColor = P.accent,
                )
            }
        },
        floatingActionButton = {
            // Le + en verre liquide (carré EGA en EGA), voir Flou.kt.
            if (!reorganiser) BoutonNouveau { nouveau = true }
        },
    ) { marges ->
        // Les archives des services et celles de Jasette, ensemble ; ailleurs, sans elles.
        // Les favoris et les épinglées sont tous montrés, chargés ou non : ceux dont la
        // conversation est absente (service coupé, pas encore prêt) viennent du cache des marques.
        val source = when (vue) {
            Vue.Archives -> Depot.toutesArchives
            Vue.Favoris -> Marques.favoris.map { convMarquee(it) }
            else -> Depot.boite + epinglesAbsentes()
        }
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
        // Le Head : la grille en haut de la boîte de réception, tous ses ids (chargés ou du cache).
        val head = if (vue == Vue.Tout && f.isEmpty()) Marques.head.map { convMarquee(it) } else emptyList()
        val reorg = reorganiser && vue == Vue.Favoris
        LaunchedEffect(f) { Depot.chercherContacts(if (f.length >= 2) filtre.trim() else "") }
        // La liste défile sous la barre du bas (floutée) : la marge du bas devient un
        // contentPadding, le dernier élément reste accessible. Les marges sont lues à la mesure.
        val margesHaut = remember(marges) { MargesPartielles(marges, haut = true, bas = false) }
        val margesBas = remember(marges) { MargesPartielles(marges, haut = false, bas = true, cotes = false) }
        // Le fondu du haut, seulement une fois la liste défilée (lu au dessin, sans recomposer).
        val etatListe = androidx.compose.foundation.lazy.rememberLazyListState()
        val defilee = rememberApparition { etatListe.firstVisibleItemIndex > 0 || etatListe.firstVisibleItemScrollOffset > 0 }
        LazyColumn(
            Modifier.fillMaxSize().padding(margesHaut)
                // Le masque d'abord, la source ensuite : le flou de la barre voit les rangées intactes.
                .fonduBords(28.dp, hautVisible = defilee)
                .sourceFlou(LocalFlou.current, FLOU_LISTE),
            state = etatListe,
            contentPadding = margesBas,
        ) {
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
                    val connecte = Depot.services.any { Depot.etat(it).associe }
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
                    // Glisser à droite ou à gauche (Réglages → Gestes) ; pas en mode Réorganiser.
                    RangeeGlissable(
                        c,
                        // Pas de glissement (archiver, supprimer) sur une conversation absente.
                        actif = !reorg && Depot.conv(c.id) != null,
                        ouvrir = { ouvrir(c.id) },
                        retiree = retiree,
                        annoncer = ::annoncer,
                    ) {
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
            }
            // La recherche : aussi les personnes trouvées par Messenger, Instagram, WhatsApp et Signal
            // (la liste ne contient que les conversations récentes).
            if (f.length >= 2) {
                // Les contacts du téléphone d'abord (avec leur photo), puis ceux des ponts sans doublon de numéro.
                val tel = contactsTelephone(filtre)
                val trouvees = personnesPonts(tel)
                if (trouvees.isNotEmpty() || tel.isNotEmpty()) {
                    item(key = "personnes") {
                        Text(
                            t("Personnes"), color = P.textePale, fontSize = 13.sp, fontWeight = FontWeight.SemiBold,
                            modifier = Modifier.padding(start = 16.dp, top = 14.dp, bottom = 4.dp),
                        )
                    }
                    items(tel, key = { "t-" + it.cle }) { c ->
                        Suggestion(c.nom, "${t("Contact")} · ${c.numero}", Service.GOOGLE, null, image = { PhotoContact(c.photo, 42.dp) }) {
                            filtre = ""
                            Depot.nouvelleConversation(Service.GOOGLE, c.envoi)
                        }
                    }
                    items(trouvees, key = { "p-" + it.first }) { (id, nom) ->
                        val s = Service.de(id)
                        val detail = detailPersonne(s, id)
                        Suggestion(nom, detail, s, null) {
                            filtre = ""
                            Depot.nouvelleConversation(s, id)
                        }
                    }
                } else if (Depot.services.any { Depot.pret(it) } && visibles.isEmpty()) {
                    item { Text(t("Recherche…"), color = P.textePale, modifier = Modifier.padding(start = 16.dp, top = 8.dp)) }
                }
            }
            // De la place pour le bouton flottant.
            item { Spacer(Modifier.size(80.dp)) }
        }
    }
    if (nouveau) NouveauMessage(fermer = { nouveau = false }, ouvrir = ouvrir)
}

/**
 * Le détail d'une personne trouvée : « Contact · numéro » pour un contact du
 * téléphone (Google Messages), sinon la nouvelle conversation dans son service.
 */
private fun detailPersonne(s: Service, id: String): String = when (s) {
    Service.GOOGLE -> "${t("Contact")} · ${Depot.numerosContacts[id] ?: id}"
    Service.INSTAGRAM -> t("Nouvelle conversation Instagram")
    Service.MESSENGER -> t("Nouvelle conversation Messenger")
    Service.WHATSAPP -> t("Nouvelle conversation WhatsApp")
    Service.SIGNAL -> t("Nouvelle conversation Signal")
    Service.X -> t("Nouvelle conversation X")
    Service.TELEGRAM -> t("Nouvelle conversation Telegram")
}

/**
 * Les contacts du téléphone qui correspondent à [texte] (lus sur place, jamais
 * envoyés) : seulement si Google Messages est connecté, et sans ceux qui ont
 * déjà une conversation Google à deux.
 */
private fun contactsTelephone(texte: String): List<ContactTrouve> {
    if (!Contacts.permis || !Depot.etat(Service.GOOGLE).associe) return emptyList()
    val existants = (Depot.convs + Depot.archives)
        .filter { !it.group && it.service == Service.GOOGLE }
        .map { Contacts.cleDe(it.number) }.filter { it.isNotEmpty() }.toSet()
    return Contacts.chercher(texte).filter { it.cle !in existants }
}

/** Les personnes trouvées par les ponts, sans conversation ici ni doublon (par numéro) d'un contact du téléphone. */
private fun personnesPonts(tel: List<ContactTrouve>): List<Pair<String, String>> {
    val cles = tel.map { it.cle }.toSet()
    return Depot.resultatsMeta.filter { (id, _) ->
        Depot.conv(id) == null &&
            !(Service.de(id) == Service.GOOGLE && Contacts.cleDe(Depot.numerosContacts[id] ?: id) in cles)
    }.take(8)
}

/** La barre de recherche en haut de la liste. */
@Composable
private fun ChampFiltre(texte: String, changer: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp).carte(20.dp)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Filled.Search, null, tint = P.textePale, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
            if (texte.isEmpty()) Text(t("Rechercher"), color = P.textePale, fontSize = 15.sp)
            BasicTextField(
                texte, changer, singleLine = true,
                textStyle = TextStyle(color = P.texteFort, fontSize = 15.sp, fontFamily = P.police),
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
 * conversations connues, les personnes trouvées par Messenger, Instagram,
 * WhatsApp et Signal (commande `search`), et un SMS par Google Messages au numéro tapé. Chaque
 * suggestion porte le logo de son service, pour choisir quand c'est ambigu.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NouveauMessage(fermer: () -> Unit, ouvrir: (String) -> Unit) {
    var texte by remember { mutableStateOf("") }
    val focus = remember { androidx.compose.ui.focus.FocusRequester() }
    val ctx = androidx.compose.ui.platform.LocalContext.current
    // La première ouverture propose l'accès aux contacts (avec une courte explication).
    val demanderContacts = rememberDemandeContacts()
    LaunchedEffect(Unit) {
        focus.requestFocus()
        Contacts.demarrer(ctx)
        if (Contacts.proposerAuNouveau()) demanderContacts()
    }
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
            modifier = Modifier.fondFenetre(),
            containerColor = P.ecran,
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
                        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp).carte(14.dp)
                            .padding(horizontal = 14.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(t("À :"), color = P.textePale, fontSize = 16.sp)
                        Spacer(Modifier.width(10.dp))
                        Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
                            if (texte.isEmpty()) Text(t("Nom ou numéro de téléphone"), color = P.textePale, fontSize = 16.sp)
                            BasicTextField(
                                texte, { texte = it }, singleLine = true,
                                textStyle = TextStyle(color = P.texteFort, fontSize = 16.sp, fontFamily = P.police),
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
                // Les contacts du téléphone (« Contact · numéro », avec leur photo).
                val tel = contactsTelephone(texte)
                items(tel, key = { "t-" + it.cle }) { c ->
                    Suggestion(c.nom, "${t("Contact")} · ${c.numero}", Service.GOOGLE, null, image = { PhotoContact(c.photo, 42.dp) }) {
                        Depot.nouvelleConversation(Service.GOOGLE, c.envoi)
                        finir()
                    }
                }
                // Les personnes trouvées par les ponts, sans conversation ici ni doublon d'un contact du téléphone.
                val trouvees = personnesPonts(tel)
                items(trouvees, key = { "m-" + it.first }) { (id, nom) ->
                    val s = Service.de(id)
                    val detail = detailPersonne(s, id)
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
                if (existantes.isEmpty() && tel.isEmpty() && trouvees.isEmpty() && !numero) {
                    item {
                        val cherche = q.length >= 2 && Depot.services.any { Depot.pret(it) }
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
private fun Suggestion(
    nom: String,
    detail: String,
    s: Service,
    conv: Conv?,
    image: (@Composable () -> Unit)? = null,
    onClick: () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (image != null) {
            image()
        } else if (conv != null) {
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
fun barreCouleurs(fond: androidx.compose.ui.graphics.Color = P.barre) = TopAppBarDefaults.topAppBarColors(
    containerColor = fond, scrolledContainerColor = fond,
    titleContentColor = P.titre, navigationIconContentColor = P.icone, actionIconContentColor = P.icone,
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
        Modifier.fillMaxWidth().padding(top = 4.dp).carte(14.dp)
            .let { if (onClick != null) it.clickable(onClick = onClick) else it }
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).clip(formeRonde).background(couleur))
        Spacer(Modifier.width(12.dp))
        contenu()
    }
}

/** Aucun compte connecté (et tous les ponts ont dit leur état). */
private fun aucunCompte() =
    Depot.services.none { Depot.etat(it).associe || Depot.etat(it) == ca.jasette.android.Etat.DEMARRAGE }

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
        Modifier.fillMaxWidth().padding(12.dp).carte(18.dp).padding(16.dp),
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
                androidx.compose.material3.HorizontalDivider(color = P.filet)
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
                for ((i, s) in Depot.services.withIndex()) {
                    if (i > 0) androidx.compose.material3.HorizontalDivider(color = P.filet)
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
            TextButton(shape = formeBouton, onClick = { Depot.accueilAutonome = false }) { Text(t("Changer de mode"), color = P.textePale) }
        }
    }
}
