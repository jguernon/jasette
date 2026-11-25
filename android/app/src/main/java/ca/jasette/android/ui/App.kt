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
import androidx.compose.foundation.lazy.items
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
import ca.jasette.android.Service
import ca.jasette.android.t
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Phone

/** Les écrans de l'app (pas de bibliothèque de navigation : une pile toute simple). */
sealed interface Ecran {
    data object Liste : Ecran
    data class Fil(val conv: String) : Ecran
    data object Reglages : Ecran
    /** Réglages → Serveur Jasette (aussi ouvert par « Client » à l'accueil). */
    data object Serveur : Ecran
    /** La fenêtre de connexion d'un service ; [plateforme] : messenger, facebook ou instagram. */
    data class Connexion(val service: Service, val plateforme: String) : Ecran
}

@Composable
fun App(activite: Activity) {
    var pile by remember { mutableStateOf(listOf<Ecran>(Ecran.Liste)) }
    val ecran = pile.last()
    fun aller(e: Ecran) {
        pile = pile + e
    }
    fun retour() {
        if (pile.size > 1) pile = pile.dropLast(1)
    }
    BackHandler(enabled = pile.size > 1) { retour() }

    // Une notification touchée (ou une nouvelle conversation) : on ouvre le fil.
    val aOuvrir = Depot.aOuvrir
    LaunchedEffect(aOuvrir) {
        if (aOuvrir != null) {
            pile = listOf(Ecran.Liste, Ecran.Fil(aOuvrir))
            Depot.aOuvrir = null
        }
    }

    Box(Modifier.fillMaxSize().background(P.fond)) {
        when (ecran) {
            Ecran.Liste -> Liste(
                ouvrir = { aller(Ecran.Fil(it)) },
                reglages = { aller(Ecran.Reglages) },
                serveur = { aller(Ecran.Serveur) },
                connecter = { s, p -> aller(Ecran.Connexion(s, p)) },
            )
            is Ecran.Fil -> Fil(activite, ecran.conv, retour = ::retour)
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

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Liste(
    ouvrir: (String) -> Unit,
    reglages: () -> Unit,
    serveur: () -> Unit,
    connecter: (Service, String) -> Unit,
) {
    Scaffold(
        containerColor = P.fond,
        topBar = {
            TopAppBar(
                title = { Text("Jasette", fontWeight = FontWeight.Bold, fontSize = 22.sp) },
                actions = {
                    IconButton(onClick = reglages) { Icon(Icons.Filled.Settings, t("Réglages"), tint = P.icone) }
                },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        LazyColumn(Modifier.fillMaxSize().padding(marges)) {
            item { Bannieres(reglages) }
            if (accueilVisible()) item { Accueil(serveur, connecter) }
            if (Depot.convs.isEmpty() && !accueilVisible()) {
                item {
                    val connecte = Service.entries.any { Depot.etat(it).associe }
                    Text(
                        if (connecte) t("Chargement des conversations…") else t("Aucun compte connecté."),
                        color = P.textePale,
                        modifier = Modifier.fillMaxWidth().padding(32.dp),
                    )
                }
            }
            items(Depot.convs, key = { it.id }) { c -> Rangee(c) { ouvrir(c.id) } }
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

@Composable
private fun Rangee(c: Conv, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Avatar(c, 52.dp)
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    c.name.ifEmpty { c.members.joinToString(", ") }, color = P.texteFort,
                    fontWeight = if (c.unread) FontWeight.Bold else FontWeight.SemiBold,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f), fontSize = 16.sp,
                )
                if (c.e2ee) Icon(Icons.Filled.Lock, t("Chiffré"), tint = P.textePale, modifier = Modifier.size(14.dp))
                Spacer(Modifier.width(6.dp))
                Text(heureListe(c.time), color = if (c.unread) P.accent else P.textePale, fontSize = 12.sp)
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    (if (c.fromMe && c.preview.isNotEmpty()) t("Vous : ") else "") + c.preview.replace('\n', ' '),
                    color = if (c.unread) P.texteFort else P.apercu, fontSize = 14.sp,
                    maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
                )
                if (c.unread) {
                    Spacer(Modifier.width(8.dp))
                    Box(Modifier.size(10.dp).clip(CircleShape).background(P.accent))
                }
            }
        }
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
                            Modifier.size(34.dp).clip(CircleShape),
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
