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

/** Les écrans de l'app (pas de bibliothèque de navigation : une pile toute simple). */
sealed interface Ecran {
    data object Liste : Ecran
    data class Fil(val conv: String) : Ecran
    data object Reglages : Ecran
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
            Ecran.Liste -> Liste(ouvrir = { aller(Ecran.Fil(it)) }, reglages = { aller(Ecran.Reglages) })
            is Ecran.Fil -> Fil(activite, ecran.conv, retour = ::retour)
            Ecran.Reglages -> Reglages(retour = ::retour, connecter = { s, p -> aller(Ecran.Connexion(s, p)) })
            is Ecran.Connexion -> Connexion(ecran.service, ecran.plateforme, fini = ::retour)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Liste(ouvrir: (String) -> Unit, reglages: () -> Unit) {
    Scaffold(
        containerColor = P.fond,
        topBar = {
            TopAppBar(
                title = { Text("Jasette", fontWeight = FontWeight.Bold, fontSize = 22.sp) },
                actions = {
                    IconButton(onClick = reglages) { Icon(Icons.Filled.Settings, "Réglages", tint = P.icone) }
                },
                colors = barreCouleurs(),
            )
        },
    ) { marges ->
        LazyColumn(Modifier.fillMaxSize().padding(marges)) {
            item { Bannieres(reglages) }
            if (Depot.convs.isEmpty()) {
                item {
                    val connecte = Service.entries.any { Depot.etat(it).associe }
                    Text(
                        if (connecte) "Chargement des conversations…" else "Aucun compte connecté.",
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
                IconButton(onClick = { Depot.erreur = null }) { Icon(Icons.Filled.Close, "Fermer", tint = P.icone) }
            }
        }
        Depot.emoji?.let { e ->
            Bandeau(couleur = P.accent) {
                Text(e, fontSize = 40.sp)
                Spacer(Modifier.width(12.dp))
                Text(
                    "Touche cet emoji dans Google Messages sur ton téléphone",
                    color = P.texteFort, modifier = Modifier.weight(1f),
                )
            }
        }
        if (Depot.besoinNip) {
            Bandeau(couleur = P.accent, onClick = reglages) {
                Icon(Icons.Filled.Lock, null, tint = P.accent)
                Spacer(Modifier.width(12.dp))
                Text(
                    "Messenger : entre ton NIP pour restaurer l'historique chiffré",
                    color = P.texteFort, modifier = Modifier.weight(1f),
                )
            }
        }
        val aucun = Service.entries.none { Depot.etat(it).associe || Depot.etat(it) == ca.jasette.android.Etat.DEMARRAGE }
        if (aucun) {
            Bandeau(couleur = P.accent, onClick = reglages) {
                Text("Connecte Google Messages, Messenger ou Instagram", color = P.texteFort, modifier = Modifier.weight(1f))
                TextButton(onClick = reglages) { Text("Comptes") }
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
                if (c.e2ee) Icon(Icons.Filled.Lock, "Chiffré", tint = P.textePale, modifier = Modifier.size(14.dp))
                Spacer(Modifier.width(6.dp))
                Text(heureListe(c.time), color = if (c.unread) P.accent else P.textePale, fontSize = 12.sp)
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    (if (c.fromMe && c.preview.isNotEmpty()) "Vous : " else "") + c.preview.replace('\n', ' '),
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
