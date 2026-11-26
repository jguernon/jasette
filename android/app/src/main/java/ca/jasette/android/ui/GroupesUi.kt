package ca.jasette.android.ui

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
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
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.EspaceParent
import ca.jasette.android.LigneGroupes
import ca.jasette.android.Marques
import ca.jasette.android.Service
import ca.jasette.android.Sorte
import ca.jasette.android.aMarquerLus
import ca.jasette.android.espacesNonLus
import ca.jasette.android.lignesService
import ca.jasette.android.parentDe
import ca.jasette.android.rangerSalons
import ca.jasette.android.t

/**
 * Les sections repliées de l'onglet Groupes (clés des services). Propre à
 * l'appareil (préférences « affichage »), jamais synchronisé.
 */
object GroupesReplies {
    var replies by mutableStateOf(emptySet<String>())
        private set
    private var charge = false

    private fun charger(ctx: Context) {
        if (charge) return
        charge = true
        replies = ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE)
            .getStringSet("groupes_replies", emptySet())?.toSet() ?: emptySet()
    }

    fun replie(ctx: Context, s: Service): Boolean {
        charger(ctx)
        return s.cle in replies
    }

    fun basculer(ctx: Context, s: Service) {
        charger(ctx)
        replies = if (s.cle in replies) replies - s.cle else replies + s.cle
        ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).edit()
            .putStringSet("groupes_replies", replies.toSet()).apply()
    }
}

/**
 * L'onglet Groupes (PROTOCOLE.md §5.13) : les grands espaces (`large`), une
 * section repliable par service, rangée par les règles génériques (aucune
 * propre à un service) : une ligne par parent (serveur, communauté…), une
 * par espace sans parent. Un parent ([espace], son id préfixé : « d:g81 »,
 * « w:4101@g.us ») ouvre la liste de ses salons, par catégorie. Un espace ou
 * un salon touché s'ouvre dans le fil habituel ([ouvrir]).
 */
@Composable
fun Groupes(
    choisie: String?,
    large: Boolean,
    espace: String?,
    ouvrirEspace: (String?) -> Unit,
    ouvrir: (String) -> Unit,
    retiree: (String) -> Unit,
    choisirVue: (Vue) -> Unit,
    reglages: (SectionReglages?) -> Unit,
) {
    val tous = Depot.groupes
    // Le parent ouvert : retrouvé à chaque fois (ses salons changent).
    val ouvert = espace?.let { parentDe(it, tous) }
    // « Tout marquer comme lu » : la sélection à confirmer (nom de la section ou du parent, espaces).
    var aConfirmer by remember { mutableStateOf<Pair<String, List<Conv>>?>(null) }
    BackHandler(enabled = espace != null) { ouvrirEspace(null) }
    Scaffold(
        containerColor = P.ecran,
        topBar = {
            BarreHaut(
                navigationIcon = {
                    if (espace != null) {
                        IconButton(onClick = { ouvrirEspace(null) }) {
                            Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour"), tint = P.iconeBarre)
                        }
                    }
                },
                title = {
                    if (ouvert != null) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            TuileParent(ouvert, 32.dp)
                            Spacer(Modifier.width(10.dp))
                            Text(
                                nomParent(ouvert), fontWeight = FontWeight.Bold, fontSize = 20.sp,
                                maxLines = 1, overflow = TextOverflow.Ellipsis,
                            )
                        }
                    } else {
                        Text(
                            t("Groupes"), fontWeight = FontWeight.Bold, fontSize = 22.sp,
                            maxLines = 1, overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                actions = {
                    if (ouvert != null) {
                        val lus = aMarquerLus(ouvert.service, tous, ouvert.id)
                        if (lus.isNotEmpty()) {
                            IconButton(onClick = { aConfirmer = nomParent(ouvert) to lus }) {
                                Icon(Icones.toutLu, t("Tout marquer comme lu"), tint = P.iconeBarre)
                            }
                        }
                    }
                    // Sur grand écran, les réglages sont en bas du rail.
                    if (!large) {
                        IconButton(onClick = { reglages(null) }) { Icon(Icons.Filled.Settings, t("Réglages"), tint = P.iconeBarre) }
                    }
                },
                colors = barreCouleurs(),
            )
        },
        // La barre du bas flotte par-dessus la liste, floutée (voir BarreBas).
        bottomBar = { if (!large) BarreBas(Vue.Groupes, choisirVue) },
    ) { marges ->
        val margesHaut = remember(marges) { MargesPartielles(marges, haut = true, bas = false) }
        val margesBas = remember(marges) { MargesPartielles(marges, haut = false, bas = true, cotes = false) }
        val etat = remember(espace) { PositionsListes.de(if (espace == null) "groupes" else "groupes:$espace") }
        val ctx = LocalContext.current
        LazyColumn(
            Modifier.fillMaxSize().padding(margesHaut).ombreHaut(etat).sourceFlou(LocalFlou.current, FLOU_LISTE),
            state = etat,
            contentPadding = margesBas,
        ) {
            if (espace != null) {
                // Les salons d'un parent, par catégorie (sans catégorie en tête) ;
                // la conversation parente (communauté) d'abord.
                val salons = ouvert?.salons.orEmpty()
                val tete = ouvert?.tete
                if (tete != null) {
                    item(key = tete.id) { RangeeEspace(tete, tete.id == choisie, false, retiree) { ouvrir(tete.id) } }
                }
                if (salons.isEmpty() && tete == null) {
                    item { Text(t("Aucun salon"), color = P.textePale, modifier = Modifier.fillMaxWidth().padding(32.dp)) }
                }
                for ((section, liste) in rangerSalons(salons)) {
                    if (section.isNotEmpty()) item(key = "s:$section") { TitreSection(section.uppercase()) }
                    items(liste, key = { it.id }) { c -> RangeeSalon(c, c.id == choisie, retiree) { ouvrir(c.id) } }
                }
            } else {
                // Les services dans l'ordre de l'app ; ceux sans grand espace n'ont pas de section.
                val services = (Depot.services + tous.map { it.service }).distinct().filter { s -> tous.any { it.service == s } }
                if (services.isEmpty()) {
                    item {
                        Column(Modifier.fillMaxWidth().padding(32.dp)) {
                            Text(t("Aucun groupe"), color = P.texteFort, fontWeight = FontWeight.SemiBold)
                            Spacer(Modifier.size(6.dp))
                            Text(
                                t("Les serveurs, canaux et communautés de vos services apparaissent ici. Les groupes de personnes restent dans la boîte de réception."),
                                color = P.textePale, fontSize = 14.sp,
                            )
                        }
                    }
                }
                for (s in services) {
                    val replie = GroupesReplies.replie(ctx, s)
                    item(key = "service:${s.cle}") {
                        val lus = aMarquerLus(s, tous)
                        EnteteService(
                            s, espacesNonLus(s, tous), replie,
                            marquerLus = if (lus.isEmpty()) null else ({ aConfirmer = s.titre to lus }),
                        ) { GroupesReplies.basculer(ctx, s) }
                    }
                    if (replie) continue
                    val lignes = lignesService(s, tous)
                    items(lignes, key = { l -> cleLigne(s, l) }) { l ->
                        when (l) {
                            is LigneGroupes.Espace -> RangeeEspace(l.c, l.c.id == choisie, false, retiree) { ouvrir(l.c.id) }
                            is LigneGroupes.Parent -> RangeeParent(l.e) { ouvrirEspace(l.e.id) }
                        }
                    }
                }
            }
            // De la place sous la dernière rangée.
            item { Spacer(Modifier.size(24.dp)) }
        }
    }
    aConfirmer?.let { (nom, lus) ->
        ConfirmerToutLu(nom, lus.size, fermer = { aConfirmer = null }) {
            aConfirmer = null
            Depot.marquerTousLus(lus.map { it.id })
        }
    }
}

/** Le nom d'un parent : le sien, sinon « Serveur » ou « Communauté ». */
private fun nomParent(e: EspaceParent): String = e.nom.ifEmpty { t(e.generique) }

/** La confirmation de « Tout marquer comme lu » (§5.13), dans les couleurs du thème. */
@Composable
private fun ConfirmerToutLu(nom: String, n: Int, fermer: () -> Unit, confirmer: () -> Unit) {
    val texte = if (n == 1) {
        t("Le salon non lu de %s sera marqué comme lu.").replace("%s", nom)
    } else {
        t("Les %d salons non lus de %s seront marqués comme lus.").replace("%d", "$n").replace("%s", nom)
    }
    AlertDialog(
        onDismissRequest = fermer,
        containerColor = P.menu,
        title = { Text(t("Tout marquer comme lu ?"), color = P.texteFort) },
        text = { Text(texte, color = P.texte) },
        confirmButton = {
            TextButton(shape = formeBouton, onClick = confirmer) { Text(t("Tout marquer comme lu"), color = P.accent) }
        },
        dismissButton = { TextButton(shape = formeBouton, onClick = fermer) { Text(t("Annuler"), color = P.texte) } },
    )
}

/** Une clé stable et unique pour chaque ligne (LazyColumn). */
private fun cleLigne(s: Service, l: LigneGroupes): String = when (l) {
    is LigneGroupes.Espace -> l.c.id
    is LigneGroupes.Parent -> "p:${l.e.id}"
}

/**
 * L'en-tête repliable d'un service : son logo, son nom, ses espaces non lus,
 * « Tout marquer comme lu » ([marquerLus], absent sans non-lus), la flèche.
 */
@Composable
private fun EnteteService(s: Service, nonLus: Int, replie: Boolean, marquerLus: (() -> Unit)?, basculer: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = basculer).padding(start = 16.dp, end = 12.dp, top = 14.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        LogoService(s, 26.dp, gris = ServicesCaches.cache(s))
        Spacer(Modifier.width(12.dp))
        Text(
            s.titre, color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 16.sp,
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
        )
        if (nonLus > 0) {
            Pastille(if (nonLus > 99) "99+" else "$nonLus")
            Spacer(Modifier.width(4.dp))
        }
        if (marquerLus != null) {
            IconButton(onClick = marquerLus, modifier = Modifier.size(36.dp)) {
                Icon(Icones.toutLu, t("Tout marquer comme lu"), tint = P.textePale, modifier = Modifier.size(20.dp))
            }
            Spacer(Modifier.width(2.dp))
        }
        Icon(
            Icons.Filled.KeyboardArrowDown, if (replie) t("Déplier") else t("Replier"), tint = P.textePale,
            modifier = Modifier.size(22.dp).rotate(if (replie) -90f else 0f),
        )
    }
}

/** Un sous-titre (catégorie, « Chaînes », nom d'une communauté). */
@Composable
private fun TitreSection(texte: String) {
    Text(
        texte, color = P.textePale, fontSize = 12.sp, fontWeight = FontWeight.SemiBold, maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.fillMaxWidth().padding(start = 20.dp, end = 16.dp, top = 12.dp, bottom = 4.dp),
    )
}

/** L'icône d'une sorte d'espace : haut-parleur (diffusion), personnes (supergroupe, communauté), # (salon). */
private fun iconeSorte(c: Conv): ImageVector = when {
    c.kind == Sorte.SERVEUR -> if (c.announce) Icones.diffusion else Icones.diese
    c.kind == Sorte.CANAL || c.kind == Sorte.CHAINE || c.kind == Sorte.DIFFUSION || c.announce -> Icones.diffusion
    else -> Icones.groupes
}

/** Les mentions (« @3 ») ou l'attention (« @ ») d'un espace, en pastille. */
private fun texteMentions(c: Conv): String? = when {
    c.mentions > 0 -> "@" + if (c.mentions > 99) "99+" else "${c.mentions}"
    Depot.attention[c.id] == true -> "@"
    else -> null
}

/** Le côté droit d'une rangée : l'heure, puis la pastille des mentions ou le point des non-lus. */
@Composable
private fun EtatNonLu(c: Conv, choisie: Boolean) {
    val blanc = choisie && P.sirene
    val m = texteMentions(c)
    if (m != null) {
        Spacer(Modifier.width(8.dp))
        Pastille(m)
    } else if (c.unread) {
        Spacer(Modifier.width(8.dp))
        Box(Modifier.size(10.dp).clip(formeRonde).background(if (blanc) P.surSelection else P.accent))
    }
}

/**
 * Un espace (canal, supergroupe, communauté, chaîne…) : son avatar, l'icône
 * de sa sorte, son nom et son dernier message ; [retrait] : un groupe d'une
 * communauté, décalé sous elle. Appui long : le menu de la conversation.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun RangeeEspace(c: Conv, choisie: Boolean, retrait: Boolean, retiree: (String) -> Unit, onClick: () -> Unit) {
    var menu by remember { mutableStateOf(false) }
    val blanc = choisie && P.sirene
    Box {
        Row(
            Modifier.fillMaxWidth()
                .background(if (choisie) P.selection else Color.Transparent)
                .combinedClickable(onClick = onClick, onLongClick = { menu = true })
                .padding(start = if (retrait) 40.dp else 16.dp, end = 16.dp, top = 8.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            val taille: Dp = if (retrait) 40.dp else 46.dp
            Avatar(c, taille, badge = false)
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(iconeSorte(c), null, tint = if (blanc) P.surSelection else P.textePale, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.width(4.dp))
                    Text(
                        c.name.ifEmpty { t("Sans nom") },
                        color = if (choisie) P.surSelection else P.texteFort,
                        fontWeight = if (c.unread) FontWeight.Bold else FontWeight.SemiBold,
                        maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f), fontSize = Taille.liste(15f),
                    )
                    Spacer(Modifier.width(6.dp))
                    Text(
                        heureListe(c.time), color = if (blanc) P.surSelection else if (c.unread) P.accent else P.textePale,
                        fontSize = Taille.liste(12f),
                    )
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        (if (c.fromMe && c.preview.isNotEmpty()) t("Vous : ") else "") + c.preview.replace('\n', ' '),
                        color = if (blanc) P.surSelection else if (c.unread) P.texteFort else P.apercu, fontSize = Taille.liste(14f),
                        maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                    )
                    EtatNonLu(c, choisie)
                }
            }
        }
        MenuDeListe(c, menu, fermer = { menu = false }, retiree = retiree, ouvrir = onClick)
    }
}

/** Un parent (serveur, communauté…) : son icône (ou ses initiales), son nom, ses salons non lus et ses mentions. */
@Composable
private fun RangeeParent(e: EspaceParent, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        TuileParent(e, 46.dp)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(
                nomParent(e), color = P.texteFort, fontWeight = if (e.nonLus > 0) FontWeight.Bold else FontWeight.SemiBold,
                maxLines = 1, overflow = TextOverflow.Ellipsis, fontSize = Taille.liste(15f),
            )
            val n = e.tous.size
            val detail = (if (n == 1) t("1 salon") else "$n ${t("salons")}") +
                if (e.nonLus > 0) " · ${e.nonLus} ${t("non lus")}" else ""
            Text(detail, color = P.textePale, fontSize = Taille.liste(13f), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (e.mentions > 0) {
            Spacer(Modifier.width(8.dp))
            Pastille("@" + if (e.mentions > 99) "99+" else "${e.mentions}")
        } else if (e.nonLus > 0) {
            Spacer(Modifier.width(8.dp))
            Box(Modifier.size(10.dp).clip(formeRonde).background(P.accent))
        }
        Spacer(Modifier.width(4.dp))
        Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = P.textePale)
    }
}

/**
 * La tuile d'un parent : son icône si le pont en a envoyé une (`avatar` du
 * parent, gardé sous son id préfixé « d:g81 »), sinon ses initiales.
 */
@Composable
private fun TuileParent(e: EspaceParent, taille: Dp) {
    val photo = Depot.avatars[e.id]
    val cadre = Modifier.cadreAvatar(taille)
    if (photo != null) {
        ImageFichier(photo, cadre.background(P.surface), cote = 256, pixels = if (P.retro) PIXELS_RETRO else 0)
        return
    }
    val lettres = nomParent(e).split(Regex("\\s+")).mapNotNull { m -> m.firstOrNull { it.isLetterOrDigit() } }.take(2)
        .joinToString("").uppercase().ifEmpty { "#" }
    Box(cadre.background(fondAvatar(P.avatarDefaut)), contentAlignment = Alignment.Center) {
        Text(lettres, color = Color.White, fontSize = (taille.value * 0.36f).sp, fontWeight = FontWeight.SemiBold)
    }
}

/** Un salon d'un parent : l'icône de sa sorte, son nom, ses mentions ou son point de non-lus. Appui long : le menu. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun RangeeSalon(c: Conv, choisie: Boolean, retiree: (String) -> Unit, onClick: () -> Unit) {
    var menu by remember { mutableStateOf(false) }
    val blanc = choisie && P.sirene
    Box {
        Row(
            Modifier.fillMaxWidth()
                .background(if (choisie) P.selection else Color.Transparent)
                .combinedClickable(onClick = onClick, onLongClick = { menu = true })
                .padding(horizontal = 16.dp, vertical = 11.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                iconeSorte(c), null, tint = if (blanc) P.surSelection else if (c.unread) P.texteFort else P.textePale,
                modifier = Modifier.size(20.dp),
            )
            Spacer(Modifier.width(12.dp))
            Text(
                c.name.ifEmpty { t("Sans nom") },
                color = if (choisie) P.surSelection else if (c.unread) P.texteFort else P.texte,
                fontWeight = if (c.unread) FontWeight.Bold else FontWeight.Normal,
                maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f), fontSize = Taille.liste(15f),
            )
            EtatNonLu(c, choisie)
        }
        MenuDeListe(c, menu, fermer = { menu = false }, retiree = retiree, ouvrir = onClick)
    }
}
