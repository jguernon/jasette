package ca.jasette.android.ui

import ca.jasette.android.t
import android.app.Activity
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.interaction.collectIsPressedAsState
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.produceState
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.window.Popup
import androidx.compose.ui.window.PopupProperties
import ca.jasette.android.Coche
import ca.jasette.android.coche
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Phone
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.layout
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.app.NotificationManagerCompat
import ca.jasette.android.Appels
import ca.jasette.android.Depot
import ca.jasette.android.texteActivite
import ca.jasette.android.Marques
import ca.jasette.android.plier
import androidx.compose.foundation.border
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Search
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.style.TextDecoration
import kotlinx.coroutines.launch
import ca.jasette.android.Msg
import ca.jasette.android.Piece
import ca.jasette.android.PontService
import ca.jasette.android.Service
import ca.jasette.android.statut

/**
 * Le fil d'une conversation : bulles, images, réactions, et la barre d'envoi.
 * [volet] : affiché à droite de la liste (deux volets) ; pas de flèche retour alors.
 * [retiree] : la conversation vient d'être archivée ou supprimée par le menu.
 * [reglages] : ouvre Réglages → Comptes (conversation d'un service pas connecté).
 *
 * Une conversation marquée mais pas chargée (son service est coupé ou se
 * connecte) s'ouvre aussi : son en-tête vient du cache des marques, et le fil
 * montre les messages déjà reçus s'il y en a, sinon [FilHorsLigne].
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Fil(
    activite: Activity,
    id: String,
    retour: () -> Unit,
    volet: Boolean = false,
    retiree: (String) -> Unit = {},
    reglages: () -> Unit = {},
) {
    val conv = Depot.conv(id)
    // Ce que montre l'en-tête : la conversation, ou sa fiche gardée si elle est marquée.
    val entete = conv ?: if (estMarquee(id)) convMarquee(id) else null
    val absente = absence(id)
    val liste = Depot.messages[id].orEmpty()
    var plein by remember { mutableStateOf<String?>(null) }
    var menu by remember { mutableStateOf(false) }
    // La barre de réactions : le message et sa bulle, dessinée par-dessus le fil
    // (pas de Popup : sa position dérivait sur le Fold).
    var reaction by remember { mutableStateOf<Pair<ca.jasette.android.Msg, androidx.compose.ui.layout.LayoutCoordinates>?>(null) }
    var grilleMsg by remember { mutableStateOf<ca.jasette.android.Msg?>(null) }
    var conteneur by remember { mutableStateOf<androidx.compose.ui.layout.LayoutCoordinates?>(null) }
    val pressePapiers = LocalClipboardManager.current
    var galerie by remember { mutableStateOf(false) }
    // La fiche de la personne (FichePersonne.kt) : conversation à deux seulement.
    var fiche by remember { mutableStateOf(false) }
    val aFiche = conv != null && !conv.group && !conv.large
    // La recherche dans les messages chargés : la requête et le message courant.
    var recherche by remember { mutableStateOf(false) }
    var requete by remember { mutableStateOf("") }
    var courant by remember { mutableStateOf<String?>(null) }
    var attendPlus by remember { mutableStateOf(false) }
    val etat = rememberLazyListState()
    val scope = rememberCoroutineScope()

    // Médias ou Rechercher choisis dans le menu de la liste.
    val demande = DemandeFil.action
    LaunchedEffect(demande) {
        if (demande != null && demande.first == id) {
            if (demande.second == "medias") galerie = true else recherche = true
            DemandeFil.action = null
        }
    }

    // Les résultats, du plus récent au plus ancien.
    val q = plier(requete.trim())
    val resultats = if (!recherche || q.isEmpty()) emptyList()
        else liste.filter { plier(it.text).contains(q) }.map { it.id }.asReversed()
    fun allerA(mid: String) {
        courant = mid
        val i = liste.indexOfFirst { it.id == mid }
        if (i >= 0) scope.launch { etat.animateScrollToItem(liste.size - 1 - i) }
    }
    // Plus d'historique est arrivé : on passe au résultat plus ancien suivant.
    LaunchedEffect(resultats.size, liste.size) {
        if (attendPlus && Depot.chargement != id) {
            val k = resultats.indexOf(courant)
            if (k + 1 < resultats.size) {
                attendPlus = false
                allerA(resultats[k + 1])
            } else if (Depot.plusAnciens[id] == true) {
                Depot.plusVieux(id)
            } else {
                attendPlus = false
            }
        }
    }
    // Une nouvelle requête : le résultat le plus récent.
    LaunchedEffect(q) {
        attendPlus = false
        courant = null
        resultats.firstOrNull()?.let { allerA(it) }
    }
    fun plusAncien() {
        val k = resultats.indexOf(courant)
        if (k + 1 < resultats.size) {
            allerA(resultats[k + 1])
        } else if (Depot.plusAnciens[id] == true && q.isNotEmpty()) {
            // Au-delà des messages chargés : on charge plus d'historique.
            attendPlus = true
            Depot.plusVieux(id)
        }
    }
    fun plusRecent() {
        val k = resultats.indexOf(courant)
        if (k > 0) allerA(resultats[k - 1])
    }

    // Ouvrir : charge les messages, marque lu, retire la notification.
    DisposableEffect(id) {
        Depot.ouvrir(id)
        NotificationManagerCompat.from(activite).cancel(PontService.idNotification(id))
        onDispose { if (Depot.ouverte == id) Depot.ouverte = null }
    }
    // Ouverte avant que son service soit prêt : les messages sont demandés dès qu'il l'est.
    val pret = Depot.pret(Service.de(id))
    var etaitPret by remember { mutableStateOf(pret) }
    LaunchedEffect(pret) {
        if (pret && !etaitPret && !Depot.messages.containsKey(id)) Depot.ouvrir(id)
        etaitPret = pret
    }

    // Le Mood de la conversation (Mood.kt) : son fond sous tout le fil, source du flou
    // de l'en-tête ; ses couleurs de bulles par LocalMood. Jamais en EGA.
    val mood = moodDeConv(id)
    val flou = LocalFlou.current
    Box(Modifier.fillMaxSize()) {
    if (mood != null) Box(Modifier.matchParentSize().fondMood(mood, P.clair).sourceFlou(flou, FLOU_MOOD, z = 0.5f))
    CompositionLocalProvider(LocalMood provides mood) {
    Scaffold(
        // En deux volets, le fond du volet est déjà posé (pas de double voile au verre).
        // Avec un Mood, son fond est dessiné dessous.
        containerColor = if (volet || mood != null) Color.Transparent else P.fil,
        // L'en-tête flotte par-dessus les messages, avec le flou de ce qui passe derrière
        // (opaque en EGA) ; les messages défilent dessous (contentPadding du haut).
        topBar = { Column(Modifier.barreFloutee(SELECTION_FIL, opaque = P.barre, voile = if (P.retro) null else mood?.haut(P.clair))) {
            BarreHaut(
                navigationIcon = {
                    if (!volet) IconButton(onClick = retour) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour")) }
                },
                title = {
                    // Toucher le nom ou l'avatar ouvre la fiche de la personne ; un groupe ne change pas.
                    Row(
                        Modifier.let { if (aFiche) it.clickable { fiche = true } else it },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        if (entete != null) Avatar(entete, 36.dp, grise = absente.grise(), presence = true, fondPoint = if (P.ega) P.barre else P.fil)
                        Spacer(Modifier.width(10.dp))
                        // Le nom et le service, serrés et centrés sur l'avatar : la barre donne
                        // au titre l'interligne de titleLarge (28 sp), qui les poussait vers le haut.
                        Column(verticalArrangement = Arrangement.Center) {
                            Text(
                                entete?.name ?: "", fontSize = 17.sp, lineHeight = 21.sp, fontWeight = FontWeight.SemiBold,
                                maxLines = 1, overflow = TextOverflow.Ellipsis, style = androidx.compose.material3.LocalTextStyle.current.merge(TITRE_SERRE),
                            )
                            // « Actif il y a 12 min » d'abord (Presence.kt).
                            val activite = if (Depot.ecritDe(id) != null) t("écrit…")
                                else conv?.let { Depot.presenceDe(it) }?.let { texteActivite(it, System.currentTimeMillis()) }
                            Text(
                                (activite?.let { "$it · " } ?: "") +
                                Service.de(id).court + (if (conv?.rcs == true) " · RCS" else "") +
                                    (absente.indication()?.let { " · $it" } ?: ""),
                                fontSize = 12.sp, lineHeight = 15.sp, color = P.sousTitreBarre, style = androidx.compose.material3.LocalTextStyle.current.merge(TITRE_SERRE),
                            )
                        }
                    }
                },
                actions = {
                    if (conv != null) BoutonsAppel(activite, conv)
                    // Le menu aussi sur une conversation absente : retirer du Head ou des favoris.
                    if (entete != null) {
                        Box {
                            IconButton(onClick = { menu = true }) { Icon(Icons.Filled.MoreVert, t("Plus")) }
                            MenuConversation(
                                entete, menu, fermer = { menu = false }, retiree = retiree,
                                medias = { galerie = true },
                                rechercher = { recherche = true },
                            )
                        }
                    }
                },
                colors = barreCouleurs(if (P.ega) P.barre else Color.Transparent),
            )
            if (recherche) {
                val k = resultats.indexOf(courant)
                BarreRecherche(
                    requete, { requete = it },
                    compteur = when {
                        q.isEmpty() -> ""
                        attendPlus -> t("Recherche…")
                        resultats.isEmpty() -> t("Aucun résultat")
                        else -> "${if (k >= 0) k + 1 else 0} / ${resultats.size}"
                    },
                    plusAncien = ::plusAncien,
                    plusRecent = ::plusRecent,
                    peutAncien = k + 1 < resultats.size || (Depot.plusAnciens[id] == true && q.isNotEmpty()),
                    peutRecent = k > 0,
                    fermer = {
                        recherche = false
                        requete = ""
                    },
                )
            }
        } },
        // La saisie flotte sur les messages, comme au bureau : pas de bande, seulement
        // un dégradé vers le fond du fil sous les pastilles (les bulles s'estompent
        // au-dessus par le masque du fil). En EGA, l'aplat du fil ; en Live et Sirène,
        // leur panneau du bas (dégradé bleu pâle, gris en relief).
        bottomBar = {
            Box(
                if (P.ega) Modifier.background(P.fil)
                else if (P.retro) Modifier.fondPanneauBas()
                else Modifier.background(
                    androidx.compose.ui.graphics.Brush.verticalGradient(
                        0f to Color.Transparent, 0.5f to (mood?.bas(P.clair) ?: P.fil).copy(alpha = 0.85f),
                        1f to (mood?.bas(P.clair) ?: P.fil),
                    ),
                ),
            ) {
                Saisie(id, actif = Depot.pret(Service.de(id)) && conv?.readOnly != true)
            }
        },
    ) { marges ->
        // Le plus récent en bas : la liste est inversée.
        val inverse = liste.asReversed()
        // Les émojis seuls arrivés en direct font un pop (EmojiSeul.kt) ; pas ceux déjà là.
        val pops = remember(id) { PopsEmoji() }
        pops.observer(liste)
        LaunchedEffect(liste.lastOrNull()?.id) { if (liste.isNotEmpty()) etat.animateScrollToItem(0) }
        // La bulle « … » s'insère sous le dernier message : la liste garde sa place sur lui
        // et la bulle resterait cachée sous la saisie. Si l'on était en bas (à 80 dp près),
        // on descend doucement jusqu'à elle ; remonté dans l'historique, on ne bouge pas.
        val ecritIci = Depot.ecritDe(id) != null
        val seuilBas = with(LocalDensity.current) { 80.dp.roundToPx() }
        LaunchedEffect(ecritIci) {
            if (ecritIci && etat.firstVisibleItemIndex <= 1 &&
                (etat.firstVisibleItemIndex == 0 || etat.firstVisibleItemScrollOffset <= seuilBas)
            ) {
                etat.animateScrollToItem(0)
            }
        }
        // Les aperçus des liens (PROTOCOLE.md, §5.9) : ils arrivent quand ils sont prêts.
        LaunchedEffect(liste, ca.jasette.android.Apercus.actifs) {
            ca.jasette.android.Apercus.demander(liste, Depot.connexionServeur())
        }
        // Les messages prennent tout l'écran et défilent sous l'en-tête et la saisie : leurs
        // hauteurs deviennent un contentPadding, lu à la mesure (le clavier qui s'ouvre ne
        // recompose pas le fil).
        val margesFil = remember(marges) {
            MargesPartielles(marges, haut = true, bas = true, plusHaut = 8.dp, plusBas = 8.dp, plusCotes = 10.dp)
        }
        // Les fondus sous l'en-tête et au-dessus de la saisie : seulement s'il y a des messages
        // de ce côté (liste inversée : « en avant », ce sont les plus anciens, en haut).
        val fonduHaut = rememberApparition { etat.canScrollForward }
        val fonduBas = rememberApparition { etat.canScrollBackward }
        val densite = LocalDensity.current
        Box(Modifier.fillMaxSize()) {
        if (conv == null && liste.isEmpty() && absente.horsLigne()) {
            // Pas chargée, aucun message reçu : le service n'est pas là.
            Box(Modifier.fillMaxSize().padding(marges)) { FilHorsLigne(Service.de(id), absente, reglages) }
        } else
        LazyColumn(
            Modifier.fillMaxSize()
                // Le masque avant la source : le flou des barres voit les bulles intactes, l'écran
                // les voit s'estomper au bord de l'en-tête et de la saisie. Une seule source pour le fil.
                .fonduBords(
                    28.dp, hautVisible = fonduHaut, basVisible = fonduBas,
                    hautDebut = { with(densite) { marges.calculateTopPadding().toPx() } },
                    basDebut = { with(densite) { marges.calculateBottomPadding().toPx() } },
                )
                .sourceFlou(LocalFlou.current, FLOU_FIL),
            state = etat,
            reverseLayout = true,
            contentPadding = margesFil,
        ) {
            // Quelqu'un écrit : la bulle aux trois points, tout en bas (Ecrit.kt).
            Depot.ecritDe(id)?.let { e -> item(key = "ecrit") { BulleEcrit(e, groupe = conv?.group == true) } }
            items(inverse.size, key = { inverse[it].id }) { i ->
                val m = inverse[i]
                val avant = inverse.getOrNull(i + 1) // le message précédent dans le temps
                val apres = inverse.getOrNull(i - 1)
                Column {
                    if (avant == null || jourDe(avant.time) != jourDe(m.time)) Jour(m.time)
                    val premier = avant == null || avant.fromMe != m.fromMe || avant.sender != m.sender
                    val dernier = apres == null || apres.fromMe != m.fromMe || apres.sender != m.sender
                    Message(
                        m, groupe = conv?.group == true || conv?.large == true, nomConv = entete?.name ?: "", premier = premier, dernier = dernier,
                        surligne = if (recherche) q else "", courant = m.id == courant,
                        reagir = { coords -> reaction = m to coords },
                        pop = pops.debut(m),
                    ) { plein = it }
                }
            }
            item {
                Box(Modifier.fillMaxWidth().padding(8.dp), contentAlignment = Alignment.Center) {
                    when {
                        Depot.chargement == id -> CircularProgressIndicator(Modifier.size(24.dp), color = P.accent)
                        Depot.plusAnciens[id] == true ->
                            TextButton(shape = formeBouton, onClick = { Depot.plusVieux(id) }) { Text(t("Messages plus anciens"), color = P.accent) }
                    }
                }
            }
        }
        // La barre de réactions se place dans la zone visible, entre l'en-tête et la saisie.
        Box(Modifier.fillMaxSize().padding(marges).onGloballyPositioned { conteneur = it }) {
        reaction?.let { (m, bulle) ->
            val base = conteneur
            if (base == null || !bulle.isAttached || !base.isAttached) {
                reaction = null
                return@let
            }
            // Un voile transparent : toucher ailleurs ferme la barre.
            Box(
                Modifier.fillMaxSize().clickable(
                    interactionSource = remember { androidx.compose.foundation.interaction.MutableInteractionSource() },
                    indication = null,
                ) { reaction = null },
            )
            val haut = base.localPositionOf(bulle, androidx.compose.ui.geometry.Offset.Zero)
            val ecart = with(LocalDensity.current) { 6.dp.roundToPx() }
            Box(
                Modifier.fillMaxSize().layout { mesurable, contraintes ->
                    val p = mesurable.measure(contraintes.copy(minWidth = 0, minHeight = 0))
                    val gauche = haut.x.toInt()
                    val droite = gauche + bulle.size.width
                    val x = if (m.fromMe) droite - p.width else gauche
                    var y = haut.y.toInt() - p.height - ecart
                    if (y < 0) y = haut.y.toInt() + bulle.size.height + ecart
                    layout(contraintes.maxWidth, contraintes.maxHeight) {
                        p.place(
                            x.coerceIn(0, maxOf(0, contraintes.maxWidth - p.width)),
                            y.coerceIn(0, maxOf(0, contraintes.maxHeight - p.height)),
                        )
                    }
                },
            ) {
                BarreReactions(
                    m,
                    choisir = { e ->
                        reaction = null
                        Depot.reagir(m.conv, m.id, e)
                    },
                    plus = {
                        reaction = null
                        grilleMsg = m
                    },
                    copier = if (m.text.isNotEmpty()) {
                        {
                            reaction = null
                            pressePapiers.setText(AnnotatedString(m.text))
                        }
                    } else {
                        null
                    },
                    copierLien = ca.jasette.android.Liens.premier(m.text)?.let { u ->
                        {
                            reaction = null
                            pressePapiers.setText(AnnotatedString(u))
                        }
                    },
                )
            }
        }
        }
        }
    }
    }
    }
    grilleMsg?.let { m ->
        GrilleEmojis(fermer = { grilleMsg = null }) { e ->
            grilleMsg = null
            Depot.reagir(m.conv, m.id, e)
        }
    }

    if (galerie) Galerie(liste, fermer = { galerie = false }) { plein = it }
    if (fiche) FichePersonne(id, fermer = { fiche = false })

    plein?.let { chemin ->
        Dialog(onDismissRequest = { plein = null }, properties = DialogProperties(usePlatformDefaultWidth = false)) {
            Box(Modifier.fillMaxSize().background(Color.Black).clickable { plein = null }, contentAlignment = Alignment.Center) {
                ImageFichier(chemin, Modifier.fillMaxSize(), cote = 2048, contentScale = ContentScale.Fit)
            }
        }
    }
}

/** Téléphone et caméra : appel direct (Google) ou la conversation dans l'app officielle (Meta). */
@Composable
private fun BoutonsAppel(activite: Activity, conv: ca.jasette.android.Conv) {
    when (conv.service) {
        Service.GOOGLE -> if (!conv.group && conv.number.isNotEmpty()) {
            IconButton(onClick = { Appels.video(activite, conv.number) }) { Icon(Icones.camera, t("Appel vidéo")) }
            IconButton(onClick = { Appels.telephone(activite, conv.number) }) { Icon(Icons.Filled.Phone, t("Appeler")) }
        }
        Service.MESSENGER, Service.INSTAGRAM -> {
            val nom = conv.service.court
            IconButton(onClick = { Appels.ouvrirMeta(activite, conv) }) { Icon(Icones.camera, "${t("Appel vidéo dans")} $nom") }
            IconButton(onClick = { Appels.ouvrirMeta(activite, conv) }) { Icon(Icons.Filled.Phone, "${t("Appel dans")} $nom") }
        }
        // WhatsApp, Signal et X : pas d'appel depuis Jasette.
        else -> {}
    }
}

@Composable
private fun Jour(ms: Long) {
    Box(Modifier.fillMaxWidth().padding(vertical = 10.dp), contentAlignment = Alignment.Center) {
        Text(
            jour(ms), color = P.jourTexte, fontSize = 12.sp,
            // Sirène : une bulle d'aide jaune de Windows 2000, liseré gris.
            modifier = (if (P.sirene) Modifier.background(P.jourFond).border(1.dp, P.bordCarte) else Modifier.carte(50.dp, P.jourFond, if (P.ega) Color.Transparent else P.bordCarte))
                .padding(horizontal = 12.dp, vertical = 3.dp),
        )
    }
}

/** Le style des deux lignes du titre du fil : sans marge de police, centré dans sa ligne. */
private val TITRE_SERRE = TextStyle(
    platformStyle = androidx.compose.ui.text.PlatformTextStyle(includeFontPadding = false),
    lineHeightStyle = androidx.compose.ui.text.style.LineHeightStyle(
        alignment = androidx.compose.ui.text.style.LineHeightStyle.Alignment.Center,
        trim = androidx.compose.ui.text.style.LineHeightStyle.Trim.Both,
    ),
)

/** Les réactions rapides de la barre (appui long sur une bulle). */
private val RAPIDES = listOf("❤️", "😂", "😮", "😢", "😡", "👍")

/** La grille du « + » : des emojis courants. */
private val EMOJIS = (
    "😀 😃 😄 😁 😆 😅 🤣 😂 🙂 😉 😊 😇 🥰 😍 🤩 😘 😋 😛 😜 🤪 😝 🤗 🤭 🤫 🤔 🤐 🤨 😐 😑 😶 😏 😒 🙄 " +
        "😬 😌 😔 😪 😴 😷 🤒 🤕 🤢 🤮 🥵 🥶 🥴 😵 🤯 🤠 🥳 😎 🤓 🧐 😕 😟 🙁 😮 😯 😲 😳 🥺 😦 😧 😨 😰 😥 " +
        "😢 😭 😱 😖 😣 😞 😓 😩 😫 🥱 😤 😡 😠 🤬 😈 💀 💩 🤡 👻 👽 🤖 😺 🙈 🙉 🙊 💋 💌 💘 💝 💖 💗 💓 💞 " +
        "💕 💟 ❣️ 💔 ❤️ 🧡 💛 💚 💙 💜 🤎 🖤 🤍 💯 💢 💥 💫 💦 💨 👋 🤚 ✋ 👌 🤌 🤏 ✌️ 🤞 🤟 🤘 🤙 👈 👉 👆 " +
        "👇 ☝️ 👍 👎 ✊ 👊 🤛 🤜 👏 🙌 👐 🤲 🙏 💪 🔥 ✨ 🎉 🎊 🎂 🎁 ⭐ 🌟 ☀️ 🌈 ⚡ ❄️ 🍕 🍔 🍟 🍺 🍷 ☕ ✅ ❌ ❓ ❗"
    ).split(" ").filter { it.isNotBlank() }


/** La taille de la photo de l'auteur d'un message (groupes et espaces). */
private val PHOTO_AUTEUR = 28.dp

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun Message(
    m: Msg,
    groupe: Boolean,
    nomConv: String,
    premier: Boolean,
    dernier: Boolean,
    surligne: String,
    courant: Boolean,
    reagir: (androidx.compose.ui.layout.LayoutCoordinates) -> Unit,
    pop: Long? = null,
    agrandir: (String) -> Unit,
) {
    // « Afficher l'heure de chaque message » : l'heure (et la date) sur chaque bulle.
    val details = Marques.heureChaqueMessage
    val quand = if (details && jourDe(m.time) != java.time.LocalDate.now()) "${jour(m.time)} · ${heure(m.time)}" else heure(m.time)
    // Live et Sirène : pas de bulles, tout à gauche sous l'en-tête de la suite (Retro.kt).
    val ligne = P.retro
    val alignement = if (m.fromMe && !ligne) Alignment.End else Alignment.Start
    val c = coche(m)
    val echec = c == Coche.ECHEC
    val haptique = LocalHapticFeedback.current
    var qui by remember { mutableStateOf(false) }
    var bulle by remember { mutableStateOf<androidx.compose.ui.layout.LayoutCoordinates?>(null) }
    // L'appui long : la barre des réactions (pas sur un envoi pas encore confirmé).
    val appuiLong = {
        if (!m.local) {
            haptique.performHapticFeedback(HapticFeedbackType.LongPress)
            bulle?.let(reagir)
        }
    }
    val reessayer = { Depot.reessayer(m.conv, m.tmpId) }
    val aReactions = m.reactions.isNotEmpty()
    // Dans un groupe ou un espace, en bulles : la photo de l'auteur (§5.13) à gauche
    // de la première bulle d'une suite, une place vide alignée pour les suivantes.
    val photoAuteur = groupe && !m.fromMe && !ligne
    Row(Modifier.fillMaxWidth().padding(top = if (premier) 8.dp else 2.dp, bottom = if (aReactions) 20.dp else 0.dp)) {
    if (photoAuteur) {
        if (premier) {
            AvatarAuteur(m.sender.ifEmpty { nomConv }, Depot.avatarAuteur(m), PHOTO_AUTEUR)
        } else {
            Spacer(Modifier.width(PHOTO_AUTEUR))
        }
        Spacer(Modifier.width(6.dp))
    }
    Column(
        Modifier.weight(1f),
        horizontalAlignment = alignement,
        verticalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        if (ligne) {
            if (premier || details) {
                val qui = when {
                    m.fromMe -> monNom(ca.jasette.android.Service.de(m.conv))
                    m.sender.isNotEmpty() -> m.sender
                    else -> nomConv.ifEmpty { null }
                }
                EnteteLigne(qui, m.fromMe, quand, (12f * ca.jasette.android.Marques.tailleMessages).sp)
            }
        } else if (groupe && !m.fromMe && premier && m.sender.isNotEmpty()) {
            Text(
                m.sender, color = P.expediteur, fontSize = 12.sp, fontWeight = FontWeight.SemiBold,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        // Le corps (pièces et bulle), avec la pastille des réactions à cheval sur son bord inférieur.
        Box(if (ligne) Modifier.padding(start = retraitLigne) else Modifier) {
            Column(
                Modifier.onGloballyPositioned { bulle = it },
                horizontalAlignment = alignement,
                verticalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                for (p in m.attachments) {
                    if (p.estImage || Depot.vignettes.containsKey(p.mediaId)) {
                        PieceImage(p, envoi = c == Coche.ENVOI && m.local, appuiLong = appuiLong, agrandir = agrandir)
                    } else {
                        PieceAutre(p.name.ifEmpty { p.mime }, p, appuiLong)
                    }
                }
                if (m.attachments.isEmpty() && m.media.isNotEmpty()) PieceAutre(m.media, null, appuiLong)
                // Une carte avec titre et image dit déjà où mène le lien : l'adresse
                // sort du texte (et la bulle disparaît s'il ne restait qu'elle).
                val apercuLien = ca.jasette.android.Apercus.de(m)
                val texteSansLien = apercuLien
                    ?.takeIf { (a, img) -> a.title.isNotEmpty() && img != null && java.io.File(img).isFile }
                    ?.let { (a, _) -> ca.jasette.android.Liens.sansLien(m.text, a.url) }
                    ?: m.text
                // Un seul émoji : deux fois plus gros, sans bulle (et un pop s'il vient d'arriver).
                val seul = apercuLien == null && ca.jasette.android.emojiSeul(texteSansLien)
                if (seul) {
                    val taille = 15f * ca.jasette.android.Marques.tailleMessages * ECHELLE_EMOJI_SEUL
                    Box(
                        Modifier.let { if (courant) it.border(2.dp, P.trouve) else it }
                            .combinedClickable(onClick = { if (echec) reessayer() }, onLongClick = appuiLong)
                            .padding(horizontal = 2.dp)
                            .popElastique(pop),
                    ) {
                        Text(texteSansLien.trim(), fontSize = taille.sp, lineHeight = (taille * 1.2f).sp)
                    }
                } else if (texteSansLien.isNotEmpty() && ligne) {
                    // Sans bulle : le texte seul, la coche au bout de ma dernière ligne.
                    val contexte = LocalContext.current
                    Row(
                        Modifier.let { if (courant) it.border(2.dp, P.trouve) else it }
                            .combinedClickable(onClick = { if (echec) reessayer() }, onLongClick = appuiLong)
                            .padding(vertical = 1.dp),
                        verticalAlignment = Alignment.Bottom,
                    ) {
                        Text(
                            avecLiens(surligner(texteSansLien, surligne, courant), false) { ouvrirLien(contexte, it) },
                            color = couleurTexteLigne(m.fromMe),
                            fontSize = (15f * ca.jasette.android.Marques.tailleMessages).sp,
                            lineHeight = (20f * ca.jasette.android.Marques.tailleMessages).sp,
                            modifier = Modifier.weight(1f, fill = false),
                        )
                        if (m.fromMe && (dernier || details || echec)) {
                            Spacer(Modifier.width(4.dp))
                            Box(Modifier.padding(bottom = 3.dp)) { IconeCoche(c, surBulle = false) }
                        }
                    }
                    apercuLien?.let { (a, image) -> CarteApercu(a, image, appuiLong) }
                } else if (texteSansLien.isNotEmpty()) {
                    val rayon = 18.dp
                    val forme = if (m.fromMe) {
                        formeBulle(rayon, rayon, if (dernier) 4.dp else rayon, rayon)
                    } else {
                        formeBulle(rayon, rayon, rayon, if (dernier) 4.dp else rayon)
                    }
                    // Envoyée : le dégradé d'accent (bleu EGA) ; reçue : la surface et son liseré.
                    // Avec un Mood : ses couleurs à lui.
                    val mood = LocalMood.current
                    Column(
                        Modifier.widthIn(max = 300.dp).clip(forme)
                            .let {
                                when {
                                    mood != null && m.fromMe -> it.background(mood.envoye)
                                    mood != null -> it.background(mood.recue(P.clair))
                                    m.fromMe -> it.background(P.degrade)
                                    else -> it.background(P.surface)
                                }
                            }
                            .let {
                                when {
                                    courant -> it.border(2.dp, P.trouve, forme)
                                    !m.fromMe && P.bordBulle.alpha > 0f -> it.border(1.dp, P.bordBulle, forme)
                                    else -> it
                                }
                            }
                            .combinedClickable(onClick = { if (echec) reessayer() }, onLongClick = appuiLong)
                            .padding(horizontal = 13.dp, vertical = 8.dp),
                    ) {
                        val contexte = LocalContext.current
                        Text(
                            avecLiens(surligner(texteSansLien, surligne, courant), m.fromMe, mood?.texteEnvoye) { ouvrirLien(contexte, it) },
                            color = when {
                                mood != null && m.fromMe -> mood.texteEnvoye
                                mood != null -> mood.texteRecu(P.clair)
                                m.fromMe -> P.surAccent
                                else -> P.texteFort
                            },
                            fontSize = (15f * ca.jasette.android.Marques.tailleMessages).sp,
                            lineHeight = (20f * ca.jasette.android.Marques.tailleMessages).sp,
                        )
                    }
                    apercuLien?.let { (a, image) -> CarteApercu(a, image, appuiLong) }
                } else {
                    // Il ne restait que l'adresse (un partage Instagram, par exemple) :
                    // la carte seule, sans bulle vide.
                    apercuLien?.let { (a, image) -> CarteApercu(a, image, appuiLong) }
                }
            }
            if (aReactions) {
                Reactions(
                    m,
                    Modifier.align(if (m.fromMe && !ligne) Alignment.BottomEnd else Alignment.BottomStart)
                        .offset(x = if (m.fromMe && !ligne) (-10).dp else 10.dp, y = 19.dp),
                ) { qui = true }
            }
        }
        // Sous la bulle, comme au bureau : l'heure et la coche à la fin d'une
        // suite de messages, ou partout avec « Afficher l'heure de chaque message »
        // (toujours sous les pièces seules de mes messages).
        if (ligne) {
            // Sans bulle, l'heure est dans l'en-tête : la coche seule sous une pièce envoyée sans texte.
            if (m.fromMe && (m.text.isEmpty() || ca.jasette.android.emojiSeul(m.text)) && (dernier || details || echec)) {
                Row(Modifier.padding(start = retraitLigne, top = if (aReactions) 16.dp else 0.dp)) { IconeCoche(c, surBulle = false) }
            }
        } else if (dernier || details || echec || (m.text.isEmpty() && m.fromMe)) {
            Row(
                Modifier.padding(horizontal = 6.dp)
                    .padding(top = if (aReactions) 16.dp else 0.dp)
                    .let { if (echec) it.clickable(onClick = reessayer) else it },
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(3.dp),
            ) {
                Text(quand, color = P.textePale, fontSize = 11.sp)
                IconeCoche(c, surBulle = false)
            }
        }
        if (echec && Depot.peutReessayer(m.tmpId)) {
            Text(
                t("Échec de l'envoi · Réessayer"), color = P.danger, fontSize = 12.sp,
                modifier = Modifier.clip(forme(8.dp)).clickable(onClick = reessayer)
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            )
        }
    }
    }
    if (qui) QuiAReagi(m) { qui = false }
}

/**
 * La coche d'un de mes messages : ⏱ envoi, ✓ envoyé, ✓✓ gris distribué,
 * ✓✓ accent lu, ⚠ rouge échec (on touche la bulle pour réessayer).
 */
@Composable
private fun IconeCoche(c: Coche, surBulle: Boolean) {
    val pale = if (surBulle) P.heureEnvoyee else P.textePale
    val lu = if (surBulle) P.luSurBulle else P.accent
    val (icone, couleur, nom) = when (c) {
        Coche.AUCUNE -> return
        Coche.ENVOI -> Triple(Icones.horloge, pale, t("Envoi…"))
        Coche.ENVOYE -> Triple(Icons.Filled.Check, pale, t("Envoyé"))
        Coche.DISTRIBUE -> Triple(Icones.doubleCoche, pale, t("Distribué"))
        Coche.LU -> Triple(Icones.doubleCoche, lu, t("Lu"))
        Coche.ECHEC -> Triple(Icons.Filled.Warning, if (surBulle) P.echecSurBulle else P.danger, t("Échec"))
    }
    Icon(icone, nom, tint = couleur, modifier = Modifier.size(14.dp))
}

/** La barre flottante des réactions, au-dessus de la bulle. */
@Composable
private fun BarreReactions(m: Msg, choisir: (String) -> Unit, plus: () -> Unit, copier: (() -> Unit)?, copierLien: (() -> Unit)? = null) {
    val mienne = m.reactions.firstOrNull { it.fromMe }?.emoji
    run {
        Row(
            Modifier.flottant(26.dp)
                .padding(horizontal = 6.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            for (e in RAPIDES) {
                Box(
                    Modifier.size(42.dp).clip(formeRonde)
                        .background(if (e == mienne) (if (P.ega) P.choisi else P.accent.copy(alpha = 0.35f)) else Color.Transparent)
                        .clickable { choisir(e) },
                    contentAlignment = Alignment.Center,
                ) { Text(e, fontSize = 24.sp) }
            }
            Box(
                Modifier.size(38.dp).clip(formeRonde).background(P.choisi).clickable(onClick = plus),
                contentAlignment = Alignment.Center,
            ) { Icon(Icons.Filled.Add, t("Plus de réactions"), tint = P.texteFort, modifier = Modifier.size(20.dp)) }
            if (copier != null) {
                IconButton(onClick = copier) { Icon(Icones.copier, t("Copier le texte"), tint = P.icone) }
            }
            if (copierLien != null) {
                IconButton(onClick = copierLien) {
                    Text("🔗", fontSize = 20.sp, modifier = Modifier.semantics { contentDescription = t("Copier le lien") })
                }
            }
        }
    }
}

/** Le « + » des réactions (et « Autre… » de l'emoji rapide) : une grille simple d'emojis. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun GrilleEmojis(fermer: () -> Unit, choisir: (String) -> Unit) {
    ModalBottomSheet(onDismissRequest = fermer, containerColor = P.menu) {
        LazyVerticalGrid(
            GridCells.Adaptive(48.dp),
            Modifier.fillMaxWidth().heightIn(max = 420.dp).padding(horizontal = 8.dp),
        ) {
            items(EMOJIS) { e ->
                Box(
                    Modifier.size(48.dp).clip(formeRonde).clickable { choisir(e) },
                    contentAlignment = Alignment.Center,
                ) { Text(e, fontSize = 26.sp) }
            }
        }
        Spacer(Modifier.size(24.dp))
    }
}

/** Toucher la pastille : qui a réagi avec quoi. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun QuiAReagi(m: Msg, fermer: () -> Unit) {
    ModalBottomSheet(onDismissRequest = fermer, containerColor = P.menu) {
        Column(Modifier.fillMaxWidth().padding(bottom = 24.dp)) {
            Text(
                t("Réactions"), color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 17.sp,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 8.dp),
            )
            for (r in m.reactions.sortedByDescending { it.fromMe }) {
                Row(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(r.emoji, fontSize = 24.sp)
                    Spacer(Modifier.width(14.dp))
                    Text(
                        if (r.fromMe) t("Vous") else r.sender.ifEmpty { t("Quelqu'un") },
                        color = P.texteFort, modifier = Modifier.weight(1f),
                    )
                    if (r.fromMe) {
                        TextButton(shape = formeBouton, onClick = {
                            fermer()
                            Depot.reagir(m.conv, m.id, r.emoji)
                        }) { Text(t("Retirer"), color = P.accent) }
                    }
                }
            }
        }
    }
}

/**
 * La pastille des réactions, embarquée sur le bas de la bulle : emojis groupés
 * avec leur compte, les miennes légèrement accentuées.
 */
@Composable
private fun Reactions(m: Msg, modifier: Modifier, toucher: () -> Unit) {
    val groupes = m.reactions.groupBy { it.emoji }
    val forme = formePilule
    Row(
        modifier.clip(forme).background(if (P.clair) P.menu else P.surface)
            .border(if (P.ega) 1.dp else 1.5.dp, if (P.ega) P.bordBulle else if (P.clair) P.bordCarte else P.fil, forme).clickable(onClick = toucher)
            .padding(horizontal = 7.dp, vertical = 2.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        for ((emoji, quiLaMis) in groupes) {
            val mienne = quiLaMis.any { it.fromMe }
            Row(
                Modifier.clip(forme).background(if (mienne) (if (P.ega) P.choisi else P.accent.copy(alpha = 0.30f)) else Color.Transparent)
                    .padding(horizontal = 3.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(emoji, fontSize = 14.sp)
                if (quiLaMis.size > 1) {
                    Spacer(Modifier.width(2.dp))
                    Text(quiLaMis.size.toString(), color = if (mienne) P.texteFort else P.texte, fontSize = 12.sp)
                }
            }
        }
    }
}

/** Une image (ou la vignette d'une vidéo en cours d'envoi, avec ▶). */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun PieceImage(p: Piece, envoi: Boolean, appuiLong: () -> Unit, agrandir: (String) -> Unit) {
    val vignette = Depot.vignettes[p.mediaId]
    val chemin = if (p.estImage) Depot.medias[p.mediaId] else vignette
    val ratio = if (p.w > 0 && p.h > 0) (p.w.toFloat() / p.h).coerceIn(0.5f, 2.5f) else 1.3f
    val cadre = Modifier.widthIn(max = 260.dp).fillMaxWidth(0.7f).aspectRatio(ratio)
        .let {
            when {
                // Live : la photo dans un cadre blanc de 3 dp, avec une ombre bleutée.
                P.live -> it.shadow(4.dp, forme(6.dp), ambientColor = couleur(0x143c78), spotColor = couleur(0x143c78))
                    .clip(forme(6.dp)).background(Color.White).padding(3.dp).clip(forme(4.dp)).background(P.surface)
                // Sirène : un simple trait sombre.
                P.sirene -> it.background(P.surface).border(1.dp, couleur(0x404040))
                else -> it.carte(16.dp)
            }
        }
    Box(contentAlignment = Alignment.Center) {
        if (chemin != null) {
            ImageFichier(
                chemin,
                cadre.combinedClickable(onClick = { if (p.estImage) agrandir(chemin) }, onLongClick = appuiLong),
            )
        } else {
            Box(cadre.combinedClickable(onClick = {}, onLongClick = appuiLong), contentAlignment = Alignment.Center) {
                if (Depot.echecs.containsKey(p.mediaId)) {
                    Text(t("Image indisponible"), color = P.textePale, fontSize = 12.sp)
                } else {
                    CircularProgressIndicator(Modifier.size(22.dp), color = P.textePale, strokeWidth = 2.dp)
                }
            }
        }
        if (vignette != null && !p.estImage) {
            Icon(
                Icons.Filled.PlayArrow, null, tint = Color.White,
                modifier = Modifier.size(44.dp).clip(formeRonde).background(Color(0x88000000)).padding(6.dp),
            )
        }
        // Envoi en cours : un voile et une roue (la progression n'est pas connue).
        if (envoi && chemin != null) {
            Box(Modifier.matchParentSize().clip(forme(16.dp)).background(Color(0x66000000)), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(Modifier.size(30.dp), color = Color.White, strokeWidth = 3.dp)
            }
        }
    }
}

/**
 * Une pièce qui n'est pas une image (PDF, document…), envoyée ou reçue : un
 * appui la télécharge au besoin puis l'ouvre avec l'app du système
 * ([Depot.ouvrirPiece]). Pendant le téléchargement, une petite roue ; après un
 * échec, « Indisponible · réessayer ». Sans [piece] (le service n'a donné que
 * le nom), rien à ouvrir.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun PieceAutre(nom: String, piece: Piece?, appuiLong: () -> Unit = {}) {
    val attente = piece != null && Depot.telechargements.containsKey(piece.mediaId)
    val echec = piece != null && !attente && Depot.echecs.containsKey(piece.mediaId)
    Row(
        Modifier.carte(16.dp)
            .combinedClickable(onClick = { if (piece != null) Depot.ouvrirPiece(piece) }, onLongClick = appuiLong)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (attente) {
            CircularProgressIndicator(Modifier.size(14.dp), color = P.textePale, strokeWidth = 2.dp)
        } else {
            Text("📎", fontSize = 13.sp)
        }
        Column(Modifier.weight(1f, fill = false)) {
            Text(nom, color = P.texte, fontSize = 13.sp)
            when {
                attente -> Text(t("Téléchargement…"), color = P.textePale, fontSize = 11.sp)
                echec -> Text(t("Indisponible · réessayer"), color = P.danger, fontSize = 11.sp)
            }
        }
    }
}

/**
 * La barre d'envoi : le bouton (+) des pièces jointes, le champ arrondi et le
 * bouton d'envoi. Les pièces choisies s'affichent en vignettes au-dessus, avec
 * un ✕ pour retirer chacune, jusqu'à l'envoi.
 */
@Composable
private fun Saisie(conv: String, actif: Boolean) {
    val ctx = LocalContext.current
    var texte by remember(conv) { mutableStateOf("") }
    val pieces = remember(conv) { mutableStateListOf<Uri>() }
    var choix by remember { mutableStateOf(false) }
    var erreurEnvoi by remember(conv) { mutableStateOf<String?>(null) }
    var capture by remember { mutableStateOf<Uri?>(null) }
    fun ajouter(l: List<Uri>) {
        erreurEnvoi = null
        for (u in l) if (u !in pieces) pieces.add(u)
    }
    val galerie = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(10)) { ajouter(it) }
    val fichiers = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { ajouter(it) }
    val photo = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { ok ->
        capture?.let { if (ok) ajouter(listOf(it)) }
    }
    val video = rememberLauncherForActivityResult(ActivityResultContracts.CaptureVideo()) { ok ->
        capture?.let { if (ok) ajouter(listOf(it)) }
    }
    // Ouvert par le glissement « Répondre » : le curseur dans le champ, le clavier ouvert.
    val focusSaisie = remember { androidx.compose.ui.focus.FocusRequester() }
    val clavier = androidx.compose.ui.platform.LocalSoftwareKeyboardController.current
    val clavierPour = ca.jasette.android.Gestes.clavierPour
    LaunchedEffect(clavierPour, actif) {
        if (clavierPour == conv && actif) {
            kotlinx.coroutines.delay(150)
            runCatching { focusSaisie.requestFocus() }
            clavier?.show()
            ca.jasette.android.Gestes.clavierPour = null
        }
    }
    Column(Modifier.fillMaxWidth().navigationBarsPadding().imePadding()) {
        erreurEnvoi?.let {
            Text(it, color = P.danger, fontSize = 13.sp, modifier = Modifier.padding(horizontal = 14.dp, vertical = 4.dp))
        }
        if (pieces.isNotEmpty()) {
            LazyRow(
                Modifier.fillMaxWidth().padding(top = 8.dp),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 10.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                items(pieces.toList(), key = { it.toString() }) { u ->
                    VignetteAEnvoyer(u) { pieces.remove(u) }
                }
            }
        }
        // Les IconButton de Material se gonflent à 48 dp (zone tactile minimale) :
        // ici, le +, la pastille et le bouton de droite font tous HAUT_SAISIE
        // (44 dp, déjà une bonne cible), alignés sur la même ligne.
        androidx.compose.runtime.CompositionLocalProvider(
            androidx.compose.material3.LocalMinimumInteractiveComponentSize provides androidx.compose.ui.unit.Dp.Unspecified,
        ) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 6.dp, vertical = 8.dp),
            verticalAlignment = Alignment.Bottom,
        ) {
            Box {
                // Comme au bureau : le + dans un rond plein.
                IconButton(
                    onClick = {
                        // SMS du téléphone : l'envoi de MMS n'est pas fait.
                        if (ca.jasette.android.Depot.estSms(conv)) {
                            erreurEnvoi = t("Envoi de photos : pas encore en mode SMS")
                        } else {
                            choix = true
                        }
                    },
                    enabled = actif,
                    modifier = Modifier.size(HAUT_SAISIE).boutonTheme(HAUT_SAISIE / 2),
                ) {
                    Icon(Icons.Filled.Add, t("Joindre"), tint = if (actif) P.texteFort else P.textePale)
                }
                DropdownMenu(
                    expanded = choix, onDismissRequest = { choix = false }, containerColor = P.menu,
                    shape = forme(16.dp), border = bordMenu,
                ) {
                    DropdownMenuItem(
                        text = { Text(t("Photos et vidéos")) },
                        leadingIcon = { Icon(Icones.images, null) },
                        onClick = {
                            choix = false
                            galerie.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageAndVideo))
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(t("Appareil photo")) },
                        leadingIcon = { Icon(Icones.photo, null) },
                        onClick = {
                            choix = false
                            Envoi.uriCapture(ctx, "jpg").let {
                                capture = it
                                photo.launch(it)
                            }
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(t("Vidéo")) },
                        leadingIcon = { Icon(Icones.video, null) },
                        onClick = {
                            choix = false
                            Envoi.uriCapture(ctx, "mp4").let {
                                capture = it
                                video.launch(it)
                            }
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(t("Fichier")) },
                        leadingIcon = { Icon(Icones.fichier, null) },
                        onClick = {
                            choix = false
                            fichiers.launch(arrayOf("*/*"))
                        },
                    )
                }
            }
            Spacer(Modifier.width(8.dp))
            // Comme au bureau : une pastille avec le logo du service et
            // « Message à <nom> sur <service> ».
            val service = ca.jasette.android.Service.de(conv)
            val nomConv = Depot.conv(conv)?.name?.ifEmpty { null } ?: t("ce contact")
            // Hauteur minimale : exactement celle du + (et du bouton de droite), texte
            // centré ; sur plusieurs lignes, la pastille grandit comme avant.
            Row(
                Modifier.weight(1f).champRond()
                    .padding(start = 12.dp, end = 16.dp, top = 6.dp, bottom = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                LogoService(service, 20.dp)
                Spacer(Modifier.width(10.dp))
                Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
                    if (texte.isEmpty()) {
                        Text(
                            if (actif) t("Message à %s sur %s").format(nomConv, service.court) else t("Envoi impossible pour l'instant"),
                            color = P.textePale, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                    }
                    BasicTextField(
                        texte, { texte = it },
                        enabled = actif,
                        textStyle = TextStyle(color = P.texteFort, fontSize = 16.sp, fontFamily = P.police),
                        cursorBrush = SolidColor(P.accent),
                        maxLines = 6,
                        modifier = Modifier.fillMaxWidth().focusRequester(focusSaisie),
                    )
                }
            }
            val peut = actif && (texte.isNotBlank() || pieces.isNotEmpty())
            // L'emoji rapide de la conversation (Mood.kt), s'il y en a un : son
            // bouton prend la place de l'envoi tant que le champ est vide.
            val rapide = Marques.emojiRapide(conv)
            val envoi: @Composable () -> Unit = { IconButton(
                onClick = {
                    val l = pieces.toList()
                    val tx = texte.trim()
                    pieces.clear()
                    texte = ""
                    erreurEnvoi = null
                    if (l.isEmpty()) {
                        Depot.envoyer(conv, tx)
                    } else {
                        Envoi.envoyer(ctx, conv, l, tx) { erreurEnvoi = it }
                    }
                },
                enabled = peut,
                modifier = Modifier.size(HAUT_SAISIE)
                    .let { if (peut) it.boutonEnvoi() else it.clip(formeRonde).carte(HAUT_SAISIE / 2) },
            ) {
                Icon(Icons.AutoMirrored.Filled.Send, t("Envoyer"), tint = if (peut) P.surAccent else P.textePale)
            } }
            if (rapide == null) {
                // Le bouton d'envoi n'apparaît que lorsqu'il y a quelque chose à envoyer.
                if (peut) Spacer(Modifier.width(8.dp))
                if (peut) envoi()
            } else {
                Spacer(Modifier.width(8.dp))
                // Un fondu court quand l'emoji et l'envoi se relaient.
                androidx.compose.animation.Crossfade(
                    peut, label = "emoji-rapide",
                    animationSpec = if (P.sec) androidx.compose.animation.core.snap() else androidx.compose.animation.core.tween(160),
                ) {
                    if (it) envoi() else BoutonEmojiRapide(rapide, actif) { Depot.envoyer(conv, rapide, emojiRapide = true) }
                }
            }
        }
        }
    }
}

/** La hauteur de la barre d'envoi : le +, la pastille (au moins) et le bouton de droite. */
private val HAUT_SAISIE = 44.dp

/**
 * Le bouton rond de l'emoji rapide : un appui l'envoie seul, comme un message
 * texte (même chemin, mêmes coches). Il s'enfonce un peu sous le doigt.
 */
@Composable
private fun BoutonEmojiRapide(emoji: String, actif: Boolean, envoyer: () -> Unit) {
    val source = remember { androidx.compose.foundation.interaction.MutableInteractionSource() }
    val presse by source.collectIsPressedAsState()
    val echelle by animateFloatAsState(if (presse) 0.82f else 1f, label = "emoji-rapide-presse")
    val haptique = LocalHapticFeedback.current
    Box(
        Modifier.size(HAUT_SAISIE).boutonTheme(HAUT_SAISIE / 2)
            .clickable(source, indication = null, enabled = actif, onClickLabel = t("Envoyer")) {
                haptique.performHapticFeedback(HapticFeedbackType.TextHandleMove)
                envoyer()
            },
        contentAlignment = Alignment.Center,
    ) {
        EmojiCentre(
            emoji, 24f,
            Modifier.graphicsLayer {
                scaleX = echelle
                scaleY = echelle
                alpha = if (actif) 1f else 0.45f
            },
        )
    }
}

/**
 * Un emoji vraiment centré dans son rond : sans le rembourrage de la police
 * et avec une hauteur de ligne égale à la taille, coupée en haut et en bas
 * (sinon il tombe un peu bas).
 */
@Composable
internal fun EmojiCentre(emoji: String, taille: Float, modifier: Modifier = Modifier) {
    Text(
        emoji,
        modifier = modifier,
        style = androidx.compose.ui.text.TextStyle(
            fontSize = taille.sp,
            lineHeight = taille.sp,
            platformStyle = androidx.compose.ui.text.PlatformTextStyle(includeFontPadding = false),
            lineHeightStyle = androidx.compose.ui.text.style.LineHeightStyle(
                alignment = androidx.compose.ui.text.style.LineHeightStyle.Alignment.Center,
                trim = androidx.compose.ui.text.style.LineHeightStyle.Trim.Both,
            ),
        ),
    )
}

/** La vignette d'une pièce choisie, avant l'envoi, avec son ✕. */
@Composable
private fun VignetteAEnvoyer(u: Uri, retirer: () -> Unit) {
    val ctx = LocalContext.current
    val image by produceState<ImageBitmap?>(null, u) { value = Envoi.vignette(ctx, u) }
    val nom = remember(u) { Envoi.nom(ctx, u) }
    val video = remember(u) { ctx.contentResolver.getType(u)?.startsWith("video/") == true }
    Box(Modifier.size(72.dp)) {
        val cadre = Modifier.fillMaxSize().carte(12.dp)
        val img = image
        if (img != null) {
            androidx.compose.foundation.Image(img, nom, cadre, contentScale = ContentScale.Crop)
        } else {
            Box(cadre.padding(6.dp), contentAlignment = Alignment.Center) {
                Text("📎 $nom", color = P.texte, fontSize = 10.sp, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
        }
        if (video) {
            Icon(
                Icons.Filled.PlayArrow, null, tint = Color.White,
                modifier = Modifier.align(Alignment.Center).size(26.dp).clip(formeRonde).background(Color(0x88000000)),
            )
        }
        Icon(
            Icons.Filled.Close, t("Retirer"), tint = Color.White,
            modifier = Modifier.align(Alignment.TopEnd).padding(3.dp).size(20.dp).clip(formeRonde)
                .background(Color(0xAA000000)).clickable(onClick = retirer).padding(2.dp),
        )
    }
}


/**
 * Le texte avec les passages qui correspondent à [q] (déjà « plié ») surlignés,
 * plus fort dans le message [courant].
 */
private fun surligner(texte: String, q: String, courant: Boolean): AnnotatedString {
    if (q.isEmpty()) return AnnotatedString(texte)
    // Le texte plié, caractère par caractère, avec l'indice d'origine de chacun.
    val plie = StringBuilder()
    val origine = ArrayList<Int>()
    texte.forEachIndexed { i, ch ->
        for (x in plier(ch.toString())) {
            plie.append(x)
            origine.add(i)
        }
    }
    val jaune = P.trouve
    val style = if (courant || P.ega) SpanStyle(background = jaune, color = Color.Black) else SpanStyle(background = jaune.copy(alpha = 0.35f))
    return buildAnnotatedString {
        append(texte)
        var d = plie.indexOf(q)
        while (d >= 0) {
            val f = d + q.length - 1
            addStyle(style, origine[d], origine[f] + 1)
            d = plie.indexOf(q, d + q.length)
        }
    }
}

/** Les adresses du texte, soulignées et cliquables (le surlignage de la recherche reste). */
private fun avecLiens(texte: AnnotatedString, surAccent: Boolean, surMood: Color? = null, ouvrir: (String) -> Unit): AnnotatedString {
    val liens = ca.jasette.android.Liens.trouver(texte.text)
    if (liens.isEmpty()) return texte
    val couleur = when {
        surAccent && surMood != null -> surMood
        surAccent -> P.surAccent
        P.ega -> couleur(EGA_CYAN_VIF)
        P.clair -> P.accent
        else -> couleur(0x6b9bff)
    }
    val style = TextLinkStyles(SpanStyle(color = couleur, textDecoration = TextDecoration.Underline))
    return buildAnnotatedString {
        append(texte)
        for ((r, u) in liens) addLink(LinkAnnotation.Clickable(u, style) { ouvrir(u) }, r.first, r.last + 1)
    }
}

/** Ouvre une adresse dans le navigateur par défaut. */
private fun ouvrirLien(ctx: android.content.Context, u: String) {
    try {
        ctx.startActivity(
            android.content.Intent(android.content.Intent.ACTION_VIEW, Uri.parse(u))
                .addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK),
        )
    } catch (_: Exception) {
    }
}

/**
 * La carte d'aperçu d'un lien, sous la bulle : l'image (16:9), le titre en
 * gras (2 lignes), la description (2 lignes) et le domaine. Un toucher ouvre
 * le lien ; l'appui long garde le menu du message.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun CarteApercu(a: ca.jasette.android.LienApercu, image: String?, appuiLong: () -> Unit) {
    val contexte = LocalContext.current
    Column(
        Modifier.widthIn(max = 280.dp).fillMaxWidth(0.78f).carte(16.dp)
            .combinedClickable(onClick = { ouvrirLien(contexte, a.url) }, onLongClick = appuiLong),
    ) {
        if (image != null && java.io.File(image).isFile) {
            // Coupée aux coins du haut de la carte (sinon l'image déborde des coins arrondis).
            ImageFichier(
                image,
                Modifier.fillMaxWidth().aspectRatio(16f / 9f)
                    .clip(formeBulle(16.dp, 16.dp, 0.dp, 0.dp)),
                cote = 720,
            )
        }
        Column(Modifier.padding(horizontal = 12.dp, vertical = 9.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            if (a.title.isNotEmpty()) {
                Text(a.title, color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 14.sp, lineHeight = 18.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            if (a.description.isNotEmpty()) {
                Text(a.description, color = P.texte, fontSize = 12.5.sp, lineHeight = 16.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            val domaine = a.domaine
            val pied = if (a.site.isEmpty() || a.site.equals(domaine, ignoreCase = true)) domaine else "${a.site} · $domaine"
            Text("🔗 $pied", color = P.textePale, fontSize = 11.5.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** Le champ de recherche en haut du fil : compteur « 2 / 7 », précédent et suivant. */
@Composable
private fun BarreRecherche(
    texte: String,
    changer: (String) -> Unit,
    compteur: String,
    plusAncien: () -> Unit,
    plusRecent: () -> Unit,
    peutAncien: Boolean,
    peutRecent: Boolean,
    fermer: () -> Unit,
) {
    val focus = remember { androidx.compose.ui.focus.FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    Row(
        // Sans fond : elle est dans l'en-tête flottant, flouté (opaque en EGA).
        Modifier.fillMaxWidth().padding(start = 12.dp, end = 4.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Row(
            Modifier.weight(1f).champRond()
                .padding(horizontal = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(Icons.Filled.Search, null, tint = P.textePale, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(8.dp))
            Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
                if (texte.isEmpty()) Text(t("Rechercher dans la conversation"), color = P.textePale, fontSize = 15.sp)
                BasicTextField(
                    texte, changer, singleLine = true,
                    textStyle = TextStyle(color = P.texteFort, fontSize = 15.sp, fontFamily = P.police),
                    cursorBrush = SolidColor(P.accent),
                    keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Search),
                    keyboardActions = androidx.compose.foundation.text.KeyboardActions(onSearch = { plusAncien() }),
                    modifier = Modifier.fillMaxWidth().focusRequester(focus),
                )
            }
            if (compteur.isNotEmpty()) Text(compteur, color = P.textePale, fontSize = 13.sp)
        }
        IconButton(onClick = plusAncien, enabled = peutAncien) {
            Icon(Icons.Filled.KeyboardArrowUp, t("Résultat précédent"), tint = if (peutAncien) P.texteFort else P.choisi)
        }
        IconButton(onClick = plusRecent, enabled = peutRecent) {
            Icon(Icons.Filled.KeyboardArrowDown, t("Résultat suivant"), tint = if (peutRecent) P.texteFort else P.choisi)
        }
        IconButton(onClick = fermer) { Icon(Icons.Filled.Close, t("Fermer"), tint = P.icone) }
    }
}

/**
 * Les médias du fil (« Médias » du menu) : une grille des images des messages
 * chargés, puis les autres fichiers. Toucher une image l'ouvre en grand.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Galerie(liste: List<Msg>, fermer: () -> Unit, agrandir: (String) -> Unit) {
    val pieces = liste.asReversed().flatMap { it.attachments }.distinctBy { it.mediaId }
    val images = pieces.filter { it.estImage }
    val autres = pieces.filter { !it.estImage }
    LaunchedEffect(images.size) { images.forEach { Depot.telecharger(it) } }
    Dialog(onDismissRequest = fermer, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Scaffold(
            modifier = Modifier.fondFenetre(),
            containerColor = P.ecran,
            topBar = {
                BarreHaut(
                    navigationIcon = { IconButton(onClick = fermer) { Icon(Icons.Filled.Close, t("Fermer")) } },
                    title = { Text(t("Médias"), fontWeight = FontWeight.Bold) },
                    colors = barreCouleurs(),
                )
            },
        ) { marges ->
            if (pieces.isEmpty()) {
                Text(
                    t("Aucun média dans les messages chargés."), color = P.textePale,
                    modifier = Modifier.padding(marges).padding(24.dp),
                )
            }
            LazyVerticalGrid(
                GridCells.Adaptive(110.dp),
                Modifier.fillMaxSize().padding(marges).padding(horizontal = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
                verticalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                items(images, key = { it.mediaId }) { p ->
                    val chemin = Depot.medias[p.mediaId]
                    val cadre = Modifier.aspectRatio(1f).carte(6.dp)
                    if (chemin != null) {
                        ImageFichier(chemin, cadre.clickable { agrandir(chemin) }, cote = 384)
                    } else {
                        Box(cadre, contentAlignment = Alignment.Center) {
                            if (Depot.echecs.containsKey(p.mediaId)) {
                                Text("✕", color = P.textePale)
                            } else {
                                CircularProgressIndicator(Modifier.size(20.dp), color = P.textePale, strokeWidth = 2.dp)
                            }
                        }
                    }
                }
                items(autres.size, span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) }) { i ->
                    PieceAutre(autres[i].name.ifEmpty { autres[i].mime }, autres[i])
                }
            }
        }
    }
}
