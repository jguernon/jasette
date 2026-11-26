package ca.jasette.android.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.NavigationRailItemDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Marques
import ca.jasette.android.Service
import ca.jasette.android.t
import ca.jasette.android.peutArchiver
import ca.jasette.android.peutSupprimer
import ca.jasette.android.refusArchiverSupprimer
import ca.jasette.android.texteSuppression

/**
 * Une action à faire dans le fil qui s'ouvre, demandée par le menu d'une
 * conversation de la liste : (id, "medias" ou "recherche").
 */
object DemandeFil {
    var action by mutableStateOf<Pair<String, String>?>(null)
}

/** Le menu de la liste : Médias et Rechercher ouvrent d'abord le fil. */
@Composable
fun MenuDeListe(c: Conv, ouvert: Boolean, fermer: () -> Unit, retiree: (String) -> Unit, ouvrir: () -> Unit) {
    MenuConversation(
        c, ouvert, fermer, retiree,
        medias = {
            DemandeFil.action = c.id to "medias"
            ouvrir()
        },
        rechercher = {
            DemandeFil.action = c.id to "recherche"
            ouvrir()
        },
    )
}

/** Le nombre de conversations non lues d'une vue (la pastille du rail). */
fun nonLus(v: Vue): Int = when (v) {
    // Les services cachés (Réglages > Comptes) ne comptent pas, sauf sur leur propre icône.
    // L'Inbox ne compte que ce qu'elle montre (pas les grands espaces gardés dans Groupes).
    Vue.Tout -> Depot.inbox.count { it.unread && !ServicesCaches.cache(it.service) }
    Vue.Favoris -> Marques.favoris.count { id ->
        !Marques.estPrivee(id) && !ServicesCaches.cache(Service.de(id)) && Depot.conv(id)?.unread == true
    }
    Vue.Archives -> Depot.toutesArchives.count { it.unread && !ServicesCaches.cache(it.service) }
    is Vue.DuService -> Depot.inbox.count { it.service == v.service && it.unread }
    // Groupes : les grands espaces non lus (PROTOCOLE.md §5.13).
    Vue.Groupes -> Depot.groupes.count { it.unread }
    Vue.Privees -> Depot.privees.count { it.unread }
}

/** Les services connectés, dans l'ordre du rail du bureau. */
fun servicesConnectes(): List<Service> = Depot.services.filter { Depot.etat(it).associe }

/** Une icône avec la pastille du nombre de non-lus. */
@Composable
private fun AvecPastille(n: Int, contenu: @Composable () -> Unit) {
    BadgedBox(badge = {
        if (n > 0) Pastille(if (n > 99) "99+" else "$n")
    }) { contenu() }
}

/** La pastille d'un nombre (non-lus) : l'accent plein, texte blanc ; carrée en EGA. */
@Composable
fun Pastille(texte: String) {
    Box(
        Modifier.heightIn(min = 16.dp).widthIn(min = 16.dp).clip(formePilule).background(P.accentPlein)
            .padding(horizontal = 4.dp),
        contentAlignment = Alignment.Center,
    ) { Text(texte, color = P.surAccent, fontSize = 11.sp, lineHeight = 16.sp) }
}

/**
 * L'icône d'un élément du rail ou de la barre du bas. En EGA, l'indicateur de
 * Material (une pilule) est remplacé par un rectangle bleu, carré comme le reste.
 */
@Composable
private fun IconeNav(choisi: Boolean, contenu: @Composable () -> Unit) {
    if (!P.ega) return contenu()
    Box(
        Modifier.background(if (choisi) P.choisi else androidx.compose.ui.graphics.Color.Transparent)
            .padding(horizontal = 20.dp, vertical = 4.dp),
        contentAlignment = Alignment.Center,
    ) { contenu() }
}

/** Les couleurs du rail : l'indicateur de Material, sauf en EGA (voir [IconeNav]). */
private fun indicateur() = if (P.ega) androidx.compose.ui.graphics.Color.Transparent else P.selection

/**
 * Le logo d'un service. [gris] : un service caché de la boîte de réception,
 * désaturé à 40 % d'opacité ; en EGA, gris et opaque (pas de transparence).
 */
@Composable
fun LogoService(s: Service, taille: androidx.compose.ui.unit.Dp, modifier: Modifier = Modifier, gris: Boolean = false) {
    val filtre = if (gris) {
        androidx.compose.ui.graphics.ColorFilter.colorMatrix(androidx.compose.ui.graphics.ColorMatrix().apply { setToSaturation(0f) })
    } else {
        null
    }
    Image(
        painterResource(iconeService(s)), s.court,
        modifier.size(taille).clip(formeLogo(taille)).let { if (gris && !P.ega) it.alpha(0.4f) else it },
        colorFilter = filtre,
    )
}

/**
 * Le rail de gauche sur grand écran (`fn rail` du bureau) : Inbox, Favoris,
 * Archives, un trait, une tuile par service connecté, et Réglages en bas.
 */
@Composable
fun Rail(vue: Vue, enReglages: Boolean, choisir: (Vue) -> Unit, reglages: () -> Unit) {
    val couleurs = NavigationRailItemDefaults.colors(
        selectedIconColor = P.surSelection, unselectedIconColor = P.icone,
        selectedTextColor = P.texteFort, unselectedTextColor = P.textePale, indicatorColor = indicateur(),
    )
    NavigationRail(
        containerColor = if (P.retro) androidx.compose.ui.graphics.Color.Transparent else P.liste,
        modifier = Modifier.width(76.dp).fondRail(),
    ) {
        Spacer(Modifier.height(8.dp))
        val fixes = listOf(
            Triple(Vue.Tout, Icones.inbox, t("Boîte de réception")),
            Triple(Vue.Favoris, Icons.Filled.Star, t("Favoris")),
            Triple(Vue.Groupes, Icones.groupes, t("Canaux")),
            Triple(Vue.Archives, Icones.archive, t("Archives")),
        )
        for ((v, ic, nom) in fixes) {
            NavigationRailItem(
                selected = !enReglages && vue == v,
                onClick = { choisir(v) },
                icon = { IconeNav(!enReglages && vue == v) { AvecPastille(nonLus(v)) { Icon(ic, nom) } } },
                colors = couleurs,
            )
        }
        HorizontalDivider(Modifier.width(32.dp).padding(vertical = 8.dp), color = P.choisi)
        for (s in servicesConnectes()) {
            val v = Vue.DuService(s)
            NavigationRailItem(
                selected = !enReglages && vue == v,
                onClick = { choisir(v) },
                icon = {
                    IconeNav(!enReglages && vue == v) {
                        AvecPastille(nonLus(v)) { LogoService(s, 30.dp, gris = ServicesCaches.cache(s)) }
                    }
                },
                colors = couleurs,
            )
        }
        Spacer(Modifier.weight(1f))
        NavigationRailItem(
            selected = enReglages,
            onClick = reglages,
            icon = { IconeNav(enReglages) { Icon(Icons.Filled.Settings, t("Réglages")) } },
            colors = couleurs,
        )
        // Mon avatar, en bas à gauche comme au bureau.
        BoutonAvatarMoi(36.dp)
        Spacer(Modifier.height(8.dp))
    }
}

/**
 * La barre du bas sur écran étroit (Fold plié) : Inbox, Favoris, Groupes,
 * Archives et Moi. Les vues par service se choisissent par les puces en haut
 * de la boîte de réception ([PucesInbox]) ; cacher un service de la boîte se
 * fait dans Réglages > Comptes (« Dans la boîte de réception »).
 */
@Composable
fun BarreBas(vue: Vue, choisir: (Vue) -> Unit) {
    val couleurs = NavigationBarItemDefaults.colors(
        selectedIconColor = P.surSelection, unselectedIconColor = P.icone,
        selectedTextColor = P.texteFort, unselectedTextColor = P.textePale, indicatorColor = indicateur(),
    )
    // Elle flotte par-dessus la liste, qui défile dessous : un flou de ce qui passe derrière,
    // teinté du thème (fond semi-opaque sous l'API 31). En EGA : l'aplat noir, opaque.
    NavigationBar(
        containerColor = androidx.compose.ui.graphics.Color.Transparent,
        tonalElevation = 0.dp,
        // Live et Sirène : leur panneau du bas, opaque (dégradé bleu pâle, gris en relief).
        modifier = if (P.retro) Modifier.fondPanneauBas() else Modifier.barreFloutee(SELECTION_LISTE, opaque = P.liste),
    ) {
        val fixes = listOf(
            Triple(Vue.Tout, Icones.inbox, t("Inbox")),
            Triple(Vue.Favoris, Icons.Filled.Star, t("Favoris")),
            Triple(Vue.Groupes, Icones.groupes, t("Canaux")),
            Triple(Vue.Archives, Icones.archive, t("Archives")),
        )
        for ((v, ic, nom) in fixes) {
            // La vue d'un service fait partie de l'Inbox (ses puces).
            val choisi = vue == v || (v == Vue.Tout && vue is Vue.DuService)
            NavigationBarItem(
                selected = choisi,
                onClick = { choisir(v) },
                icon = { IconeNav(choisi) { AvecPastille(nonLus(v)) { Icon(ic, null) } } },
                label = { Text(nom, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                colors = couleurs,
            )
        }
        // Mon avatar, en bas à droite.
        var menuAvatar by remember { mutableStateOf(false) }
        NavigationBarItem(
            selected = false,
            onClick = { menuAvatar = true },
            icon = { AvatarMoiAvecNote(26.dp) },
            label = { Text(t("Moi"), maxLines = 1) },
            colors = couleurs,
        )
        if (menuAvatar) MenuAvatar { menuAvatar = false }
    }
}

/**
 * Les puces en haut de la boîte de réception (écran étroit, plus d'un service
 * connecté) : Tout, puis chaque service connecté avec ses non-lus. Elles
 * remplacent l'ancienne feuille Services de la barre du bas ; elles défilent
 * avec la liste. Un service caché de la boîte (Réglages > Comptes) a son logo grisé.
 */
@Composable
fun PucesInbox(vue: Vue, choisir: (Vue) -> Unit) {
    val liste = servicesConnectes()
    Row(
        Modifier.fillMaxWidth().horizontalScroll(androidx.compose.foundation.rememberScrollState())
            .padding(horizontal = 12.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Puce(vue == Vue.Tout, true, { choisir(Vue.Tout) }) { c ->
            Text(t("Tout"), color = c, fontSize = 14.sp, fontWeight = if (vue == Vue.Tout) FontWeight.SemiBold else null)
        }
        for (s in liste) {
            val v = Vue.DuService(s)
            val actif = vue == v
            Puce(actif, true, { choisir(if (actif) Vue.Tout else v) }) { c ->
                LogoService(s, 18.dp, gris = ServicesCaches.cache(s))
                Text(s.court, color = c, fontSize = 14.sp, fontWeight = if (actif) FontWeight.SemiBold else null, maxLines = 1)
                val n = nonLus(v)
                if (n > 0) Pastille(if (n > 99) "99+" else "$n")
            }
        }
    }
}

/**
 * Le « Head » : la grille des favoris en haut de la boîte de réception.
 *
 * La grille (Réglages → Apparence, `Marques.headGrille`) : 3 à 6 colonnes,
 * autant de rangées qu'il faut. Les cases se partagent la largeur et les
 * avatars rapetissent au besoin jusqu'à un minimum lisible.
 *
 * Tous les ids du Head y sont, chargés ou non : une conversation absente
 * (service coupé ou pas encore prêt) vient de [ca.jasette.android.CacheMarques],
 * grisée, avec « déconnecté » ou « chargement… » (voir [absence]).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun Head(favoris: List<Conv>, choisie: String?, ouvrir: (String) -> Unit, retiree: (String) -> Unit) {
    val colonnes = ca.jasette.android.Marques.colonnes(ca.jasette.android.Marques.headGrille)
    // Les notes Instagram des contacts (§5.12) : relues au plus toutes les 15 minutes (Notes.demanderListe).
    androidx.compose.runtime.LaunchedEffect(Unit) {
        while (true) {
            ca.jasette.android.Notes.demanderListe()
            kotlinx.coroutines.delay(60_000L)
        }
    }
    // Une note dans le Head : toutes les cases gardent la place de sa bulle (rangées alignées).
    val espaceNote = favoris.any { ca.jasette.android.Notes.deContact(it.id) != null }
    Column {
        androidx.compose.foundation.layout.BoxWithConstraints(
            Modifier.fillMaxWidth().padding(horizontal = 6.dp, vertical = 4.dp),
        ) {
            // La case : la largeur partagée, jamais moins que le minimum lisible.
            val case = maxOf(maxWidth / colonnes, CASE_HEAD_MIN)
            // L'avatar suit la case (54 dp pour 80 dp), entre 30 et 54 dp.
            val avatar = (case * (54f / 80f)).coerceIn(30.dp, 54.dp)
            val police = Taille.liste(if (case < 64.dp) 10f else if (case < 76.dp) 11f else 12f)
            // Autant de rangées qu'il faut : tous les Head sont montrés.
            Column {
                for (rangee in favoris.chunked(colonnes)) {
                    Row {
                        for (c in rangee) key(c.id) { CaseHead(c, choisie, case, avatar, police, ouvrir, retiree, espaceNote) }
                    }
                }
            }
        }
        HorizontalDivider(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), color = P.traitFin)
    }
}

/** La plus petite case du Head (avatar de 30 dp et prénom en 10 sp encore lisibles). */
private val CASE_HEAD_MIN = 46.dp

/** Une case du Head : l'avatar (point si non lu) et le prénom ; appui long, le menu. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun CaseHead(
    c: Conv,
    choisie: String?,
    largeur: androidx.compose.ui.unit.Dp,
    avatar: androidx.compose.ui.unit.Dp,
    police: androidx.compose.ui.unit.TextUnit,
    ouvrir: (String) -> Unit,
    retiree: (String) -> Unit,
    espaceNote: Boolean = false,
) {
    var menu by remember { mutableStateOf(false) }
    val absente = absence(c.id)
    val note = ca.jasette.android.Notes.deContact(c.id)
    // Jamais vue (ni chargée, ni en cache) : pas de nom, seulement l'avatar générique et le logo.
    val nom = if (c.name.isEmpty() && absente != null) "" else c.name.ifEmpty { t("Sans nom") }
    val prenom = if (c.group) nom else nom.split(Regex("\\s+")).firstOrNull() ?: nom
    // Le point des non-lus suit la taille de l'avatar (13 dp pour 54 dp).
    val point = (avatar * (13f / 54f)).coerceAtLeast(9.dp)
    Box {
        Column(
            Modifier.width(largeur).clip(forme(14.dp))
                .background(if (c.id == choisie) P.selection else androidx.compose.ui.graphics.Color.Transparent)
                .combinedClickable(onClick = { ouvrir(c.id) }, onLongClick = { menu = true })
                .padding(top = if (espaceNote) 8.dp + police.value.dp * 1.6f else 8.dp, bottom = 8.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Box {
                Avatar(c, avatar, grise = absente.grise(), presence = true)
                if (c.unread) {
                    Box(
                        Modifier.align(Alignment.TopEnd).size(point).clip(formeRonde).background(P.fond)
                            .padding(2.dp).clip(formeRonde).background(P.accent),
                    )
                }
            }
            Spacer(Modifier.height(5.dp))
            Text(
                prenom, fontSize = police, maxLines = 1, overflow = TextOverflow.Ellipsis,
                textAlign = TextAlign.Center,
                color = if (c.id == choisie) P.surSelection else if (c.unread) P.texteFort else if (absente.grise()) P.textePale else P.texte,
                modifier = Modifier.padding(horizontal = 4.dp),
            )
            absente.indication()?.let {
                Text(
                    it, fontSize = police * 0.85f, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    textAlign = TextAlign.Center, color = P.textePale, fontStyle = FontStyle.Italic,
                    modifier = Modifier.padding(horizontal = 4.dp),
                )
            }
        }
        // La note Instagram du contact, en petite bulle au-dessus de l'avatar, comme dans Instagram.
        if (note != null) {
            BulleNoteHead(
                note.text, largeur - 6.dp, police * 0.85f,
                Modifier.align(Alignment.TopCenter).padding(top = 2.dp),
            )
        }
        MenuDeListe(c, menu, fermer = { menu = false }, retiree = retiree, ouvrir = { ouvrir(c.id) })
    }
}

/** Une conversation de la liste ; un appui long ouvre son menu. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun Rangee(
    c: Conv,
    choisie: Boolean,
    epinglee: Boolean,
    badge: Boolean,
    reorganiser: Boolean,
    retiree: (String) -> Unit,
    onClick: () -> Unit,
) {
    var menu by remember { mutableStateOf(false) }
    // Pas chargée (favori ou épinglée d'un service absent) : la fiche du cache, grisée.
    val absente = absence(c.id)
    val indication = absente.indication()
    // Sirène : la rangée choisie en bleu marine, tout son texte en blanc.
    val blanc = choisie && P.sirene
    Box {
        Row(
            Modifier.fillMaxWidth()
                .background(if (choisie) P.selection else androidx.compose.ui.graphics.Color.Transparent)
                .combinedClickable(onClick = onClick, onLongClick = { menu = true })
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // Le logo du service toujours en pastille sur une conversation absente.
            Avatar(c, 52.dp, badge = badge || absente != null, grise = absente.grise(), presence = true)
            Spacer(Modifier.width(14.dp))
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        c.name.ifEmpty { c.members.joinToString(", ") },
                        color = if (choisie) P.surSelection else if (absente.grise()) P.texte else P.texteFort,
                        fontWeight = if (c.unread) FontWeight.Bold else FontWeight.SemiBold,
                        maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f), fontSize = Taille.liste(16f),
                    )
                    if (c.e2ee) Icon(Icons.Filled.Lock, t("Chiffré"), tint = P.textePale, modifier = Modifier.size(14.dp))
                    if (epinglee && !reorganiser) {
                        Spacer(Modifier.width(4.dp))
                        Icon(Icones.epingle, t("Épinglée"), tint = P.textePale, modifier = Modifier.size(14.dp))
                    }
                    if (!reorganiser) {
                        Spacer(Modifier.width(6.dp))
                        if (indication != null) {
                            Text(indication, color = P.textePale, fontSize = Taille.liste(12f), fontStyle = FontStyle.Italic)
                        } else {
                            Text(heureListe(c.time), color = if (blanc) P.surSelection else if (c.unread) P.accent else P.textePale, fontSize = Taille.liste(12f))
                        }
                    }
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    // Quelqu'un écrit : « écrit… » en couleur d'accent (Ecrit.kt).
                    if (Depot.ecritDe(c.id) != null) {
                        ApercuEcrit(Taille.liste(14f), if (blanc) P.surSelection else P.accent, Modifier.weight(1f))
                    } else Text(
                        (if (c.fromMe && c.preview.isNotEmpty()) t("Vous : ") else "") + c.preview.replace('\n', ' '),
                        color = if (blanc) P.surSelection else if (c.unread) P.texteFort else P.apercu, fontSize = Taille.liste(14f),
                        maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                    )
                    if (c.unread) {
                        Spacer(Modifier.width(8.dp))
                        Box(Modifier.size(10.dp).clip(formeRonde).background(if (blanc) P.surSelection else P.accent))
                    }
                }
            }
            if (reorganiser) {
                // Réorganiser les favoris : monter ou descendre d'un cran.
                val i = Marques.favoris.indexOf(c.id)
                IconButton(onClick = { Marques.deplacerFavori(c.id, -1) }, enabled = i > 0) {
                    Icon(Icons.Filled.KeyboardArrowUp, t("Monter"), tint = if (i > 0) P.texteFort else P.choisi)
                }
                IconButton(onClick = { Marques.deplacerFavori(c.id, 1) }, enabled = i < Marques.favoris.size - 1) {
                    Icon(
                        Icons.Filled.KeyboardArrowDown, t("Descendre"),
                        tint = if (i < Marques.favoris.size - 1) P.texteFort else P.choisi,
                    )
                }
            }
        }
        MenuDeListe(c, menu, fermer = { menu = false }, retiree = retiree, ouvrir = onClick)
    }
}

/**
 * Le menu d'une conversation (`menu_conversation` du bureau), le même dans la
 * liste (appui long) et dans l'en-tête du fil (« … ») : Head, favoris,
 * épingler, médias, archiver et supprimer (selon `peutArchiver` et
 * `peutSupprimer` ; supprimer demande une confirmation), puis rechercher.
 */
@Composable
fun MenuConversation(
    c: Conv,
    ouvert: Boolean,
    fermer: () -> Unit,
    retiree: (String) -> Unit,
    medias: () -> Unit,
    rechercher: () -> Unit,
) {
    var confirmer by remember { mutableStateOf(false) }
    var feuilleMood by remember { mutableStateOf(false) }
    // Une conversation absente (affichée depuis le cache des marques) : seules ses
    // marques se changent ; archiver ou supprimer attend que son service la charge.
    val chargee = Depot.conv(c.id) != null
    val archivable = c.peutArchiver && chargee
    val supprimable = c.peutSupprimer && chargee
    val archivee = Depot.estArchivee(c)
    val epinglee = Marques.estEpinglee(c.id)
    val favori = Marques.estFavori(c.id)
    val dansHead = Marques.estDansHead(c.id)

    @Composable
    fun Item(texte: String, icone: ImageVector, couleur: androidx.compose.ui.graphics.Color = P.texteFort, actif: Boolean = true, action: () -> Unit) {
        DropdownMenuItem(
            text = { Text(texte, color = if (actif) couleur else P.textePale) },
            leadingIcon = { Icon(icone, null, tint = if (actif) couleur else P.textePale) },
            enabled = actif,
            onClick = {
                fermer()
                action()
            },
        )
    }

    DropdownMenu(
        expanded = ouvert, onDismissRequest = fermer, containerColor = P.menu,
        shape = forme(16.dp), border = bordMenu,
    ) {
        Item(if (dansHead) t("Retirer du Head") else t("Ajouter au Head"), Icones.grille) { Marques.basculerHead(c.id) }
        Item(if (favori) t("Retirer des favoris") else t("Ajouter aux favoris"), Icons.Filled.Star) { Marques.basculerFavori(c.id) }
        Item(if (epinglee) t("Désépingler") else t("Épingler en haut"), Icones.epingle) { Marques.basculerEpingle(c.id) }
        // Liste privée : seulement si elle est activée sur cet appareil.
        if (ca.jasette.android.Prive.actif) {
            val privee = Marques.estPrivee(c.id)
            Item(if (privee) t("Retirer des privées") else t("Rendre privée"), Icons.Filled.Lock) {
                Marques.rendrePrivee(c.id, !privee)
                retiree(c.id)
            }
        }
        Item(t("Médias"), Icones.images, action = medias)
        // Le Mood (couleurs des bulles et fond du fil, et l'emoji rapide) ; l'EGA
        // n'a pas de mood : son item ne choisit que l'emoji rapide.
        if (P.ega) Item(t("Emoji rapide"), Icones.sourire) { feuilleMood = true }
        else Item(t("Mood"), Icones.palette) { feuilleMood = true }
        Item(
            if (archivee) t("Désarchiver") else t("Archiver"),
            if (archivee) Icones.desarchiver else Icones.archive,
            actif = archivable,
        ) {
            Depot.archiver(c.id, !archivee)
            retiree(c.id)
        }
        c.refusArchiverSupprimer?.let { refus ->
            Text(
                refus, color = P.textePale, fontSize = 12.sp,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp).width(220.dp),
            )
        }
        HorizontalDivider(Modifier.padding(horizontal = 8.dp, vertical = 4.dp), color = P.traitFin)
        Item(t("Supprimer"), Icons.Filled.Delete, couleur = P.danger, actif = supprimable) { confirmer = true }
        HorizontalDivider(Modifier.padding(horizontal = 8.dp, vertical = 4.dp), color = P.traitFin)
        Item(t("Rechercher"), Icons.Filled.Search, action = rechercher)
    }

    if (feuilleMood) FeuilleMood(c.id, c.name) { feuilleMood = false }

    if (confirmer) {
        AlertDialog(
            onDismissRequest = { confirmer = false },
            containerColor = P.menu,
            title = { Text(t("Supprimer la conversation ?"), color = P.texteFort) },
            text = {
                Text(
                    c.texteSuppression,
                    color = P.texte,
                )
            },
            confirmButton = {
                TextButton(shape = formeBouton, onClick = {
                    confirmer = false
                    Depot.supprimer(c.id)
                    retiree(c.id)
                }) { Text(t("Supprimer"), color = P.danger) }
            },
            dismissButton = { TextButton(shape = formeBouton, onClick = { confirmer = false }) { Text(t("Annuler"), color = P.texte) } },
        )
    }
}
