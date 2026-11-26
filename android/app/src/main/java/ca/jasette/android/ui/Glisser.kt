package ca.jasette.android.ui

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxState
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.ActionGeste
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Gestes
import ca.jasette.android.peutArchiver
import ca.jasette.android.peutSupprimer
import ca.jasette.android.refusArchiverSupprimer
import ca.jasette.android.texteSuppression
import ca.jasette.android.t
import kotlinx.coroutines.launch
import kotlin.math.abs

/** La flèche de réponse (« reply » de Material Symbols ; absente de material-icons-core). */
private val iconeRepondre: ImageVector by lazy {
    ImageVector.Builder("reply", 24.dp, 24.dp, 24f, 24f, autoMirror = true)
        .addPath(addPathNodes("M10,9V5l-7,7 7,7v-4.1c5,0 8.5,1.6 11,5.1 -1,-5 -4,-10 -11,-11z"), fill = SolidColor(Color.Black))
        .build()
}

/** Ce qu'une action rend possible pour cette conversation (sinon : grisée, message dans la barre). */
private fun possible(a: ActionGeste, c: Conv) = when (a) {
    ActionGeste.ARCHIVER -> c.peutArchiver
    ActionGeste.SUPPRIMER -> c.peutSupprimer
    else -> true
}

/**
 * Une rangée de la liste qu'on peut glisser à droite ou à gauche, comme Gmail :
 * le fond révélé prend la couleur et l'icône de l'action réglée dans
 * Réglages → Gestes. Un côté réglé sur « Rien » ne glisse pas ; [actif] faux
 * (mode Réorganiser des favoris) désactive tout.
 *
 * [annoncer] montre un message dans la barre du bas, avec « Annuler » si on
 * lui passe de quoi défaire.
 */
@Composable
fun RangeeGlissable(
    c: Conv,
    actif: Boolean,
    ouvrir: () -> Unit,
    retiree: (String) -> Unit,
    annoncer: (String, (() -> Unit)?) -> Unit,
    /** Vrai dans la vue Archives : le geste désarchive toujours ; ailleurs, il archive toujours. */
    dansArchives: Boolean = false,
    contenu: @Composable () -> Unit,
) {
    val ctx = LocalContext.current
    remember { Gestes.charger(ctx) }
    val droite = Gestes.droite
    val gauche = Gestes.gauche
    if (!actif || (droite == ActionGeste.RIEN && gauche == ActionGeste.RIEN)) {
        contenu()
        return
    }
    val scope = rememberCoroutineScope()
    val haptique = LocalHapticFeedback.current
    val densite = LocalDensity.current
    var confirmer by remember { mutableStateOf(false) }
    // Le seuil : un bon tiers de la largeur, pour ne rien déclencher par mégarde.
    // Pas de rememberSwipeToDismissBoxState : il est « saveable », et la liste
    // le restaurait en position « glissée » quand la rangée revenait (archivée
    // puis désarchivée) : le geste se redéclenchait tout seul, en boucle.
    // Un état neuf à chaque apparition, toujours au repos.
    val etat = remember(c.id, dansArchives) {
        SwipeToDismissBoxState(SwipeToDismissBoxValue.Settled, densite, positionalThreshold = { it * 0.38f })
    }
    // Vrai seulement pendant un vrai geste du doigt (pas une restauration).
    var aGlisse by remember(c.id, dansArchives) { mutableStateOf(false) }
    LaunchedEffect(etat) {
        snapshotFlow { etat.dismissDirection }.collect { if (it != SwipeToDismissBoxValue.Settled) aGlisse = true }
    }

    // Un petit coup au moment où le seuil est franchi (pas au retour en deçà).
    LaunchedEffect(etat) {
        snapshotFlow { etat.targetValue }.collect {
            if (it != SwipeToDismissBoxValue.Settled) haptique.performHapticFeedback(HapticFeedbackType.GestureThresholdActivate)
        }
    }

    fun remettre() {
        scope.launch { etat.reset() }
    }

    fun agir(a: ActionGeste) {
        when (a) {
            ActionGeste.RIEN -> remettre()
            ActionGeste.REPONDRE -> {
                Gestes.clavierPour = c.id
                ouvrir()
                remettre()
            }
            ActionGeste.ARCHIVER -> {
                if (!c.peutArchiver) {
                    annoncer(c.refusArchiverSupprimer ?: "", null)
                } else {
                    val deja = dansArchives
                    Depot.archiver(c.id, !deja)
                    retiree(c.id)
                    annoncer(if (deja) t("Conversation désarchivée") else t("Conversation archivée")) {
                        // Annuler : la commande inverse.
                        Depot.archiver(c.id, deja)
                    }
                }
                // La rangée quitte la liste ; si elle reste (refus), elle revient en place.
                remettre()
            }
            ActionGeste.SUPPRIMER -> {
                if (!c.peutSupprimer) {
                    annoncer(c.refusArchiverSupprimer ?: "", null)
                } else {
                    confirmer = true
                }
                remettre()
            }
        }
    }

    SwipeToDismissBox(
        state = etat,
        enableDismissFromStartToEnd = droite != ActionGeste.RIEN,
        enableDismissFromEndToStart = gauche != ActionGeste.RIEN,
        onDismiss = { v ->
            if (!aGlisse) { remettre(); return@SwipeToDismissBox }
            aGlisse = false
            when (v) {
                SwipeToDismissBoxValue.StartToEnd -> agir(droite)
                SwipeToDismissBoxValue.EndToStart -> agir(gauche)
                SwipeToDismissBoxValue.Settled -> {}
            }
        },
        backgroundContent = {
            val sens = etat.dismissDirection
            val a = when (sens) {
                SwipeToDismissBoxValue.StartToEnd -> droite
                SwipeToDismissBoxValue.EndToStart -> gauche
                SwipeToDismissBoxValue.Settled -> null
            }
            val decalage = abs(runCatching { etat.requireOffset() }.getOrDefault(0f))
            // L'icône grossit un peu une fois le seuil franchi.
            val echelle by animateFloatAsState(if (etat.targetValue != SwipeToDismissBoxValue.Settled) 1.2f else 1f, label = "echelle")
            if (a != null && a != ActionGeste.RIEN && decalage > 0f) {
                val debut = sens == SwipeToDismissBoxValue.StartToEnd
                val ok = possible(a, c)
                val (couleur, icone, teinte) = apparence(a, dansArchives, ok)
                val largeur = with(densite) { decalage.toDp() }
                Box(Modifier.fillMaxSize(), contentAlignment = if (debut) Alignment.CenterStart else Alignment.CenterEnd) {
                    // Seule la partie découverte est peinte (le verre laisse voir au travers).
                    Box(
                        Modifier.fillMaxHeight().width(largeur).padding(vertical = 2.dp).clip(forme(12.dp)).background(couleur),
                        contentAlignment = if (debut) Alignment.CenterStart else Alignment.CenterEnd,
                    ) {
                        Box(
                            Modifier.wrapContentWidth(if (debut) Alignment.Start else Alignment.End, unbounded = true).width(72.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            Icon(
                                icone, null, tint = teinte,
                                modifier = Modifier.size(26.dp).graphicsLayer { scaleX = echelle; scaleY = echelle },
                            )
                        }
                    }
                }
            }
        },
    ) {
        contenu()
    }

    if (confirmer) ConfirmerSuppression(c, fermer = { confirmer = false }, retiree = retiree)
}

/** La couleur du fond, l'icône et sa teinte d'une action ; grise si l'action est impossible ici. */
@Composable
private fun apparence(a: ActionGeste, archivee: Boolean, ok: Boolean): Triple<Color, ImageVector, Color> {
    val gris = if (P.ega) Color(0xFF555555) else Color(0xFF8E8E93)
    return when (a) {
        ActionGeste.REPONDRE -> Triple(P.accentPlein, iconeRepondre, P.surAccent)
        ActionGeste.ARCHIVER -> Triple(
            if (ok) P.succes else gris,
            if (archivee) Icones.desarchiver else Icones.archive,
            // Le vert vif de l'EGA porte mieux une icône noire.
            if (ok && P.ega) Color.Black else Color.White,
        )
        ActionGeste.SUPPRIMER -> Triple(if (ok) P.danger else gris, Icons.Filled.Delete, Color.White)
        ActionGeste.RIEN -> Triple(Color.Transparent, Icons.Filled.Close, Color.Transparent)
    }
}

/** La même confirmation que « Supprimer la conversation » du menu. */
@Composable
private fun ConfirmerSuppression(c: Conv, fermer: () -> Unit, retiree: (String) -> Unit) {
    AlertDialog(
        onDismissRequest = fermer,
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
                fermer()
                Depot.supprimer(c.id)
                retiree(c.id)
            }) { Text(t("Supprimer"), color = P.danger) }
        },
        dismissButton = { TextButton(shape = formeBouton, onClick = fermer) { Text(t("Annuler"), color = P.texte) } },
    )
}

// ---- Réglages → Gestes ----

/** Réglages → Gestes : l'action de chaque côté, quatre choix par côté. */
@Composable
fun ChoixGestes() {
    val ctx = LocalContext.current
    remember { Gestes.charger(ctx) }
    Text(t("Glisser vers la droite"), color = P.textePale, fontSize = 13.sp)
    ChoixUnGeste(Gestes.droite) { Gestes.choisirDroite(it) }
    Text(t("Glisser vers la gauche"), color = P.textePale, fontSize = 13.sp)
    ChoixUnGeste(Gestes.gauche) { Gestes.choisirGauche(it) }
}

@Composable
private fun ChoixUnGeste(choisi: ActionGeste, choisir: (ActionGeste) -> Unit) {
    Groupe {
        Row(Modifier.fillMaxWidth().padding(6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            for (a in ActionGeste.entries) {
                val actif = a == choisi
                val (couleur, icone, teinte) = if (a == ActionGeste.RIEN) {
                    Triple(P.choisi, Icons.Filled.Close, P.texteFort)
                } else {
                    apparence(a, archivee = false, ok = true)
                }
                Column(
                    Modifier.weight(1f).clip(forme(12.dp))
                        .background(if (actif) P.choisi else Color.Transparent)
                        .clickable { choisir(a) }
                        .padding(vertical = 10.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    // Une pastille aux couleurs du fond révélé, pour reconnaître l'action.
                    Box(
                        Modifier.size(34.dp).clip(formeRonde).background(if (a == ActionGeste.RIEN) Color.Transparent else couleur),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(icone, null, tint = if (a == ActionGeste.RIEN) P.textePale else teinte, modifier = Modifier.size(20.dp))
                    }
                    Text(
                        nomGeste(a), fontSize = 13.sp, textAlign = TextAlign.Center, maxLines = 1,
                        color = if (actif) P.texteFort else P.texte,
                        fontWeight = if (actif) FontWeight.Bold else FontWeight.Normal,
                    )
                }
            }
        }
    }
}

private fun nomGeste(a: ActionGeste) = when (a) {
    ActionGeste.REPONDRE -> t("Répondre")
    ActionGeste.ARCHIVER -> t("Archiver")
    ActionGeste.SUPPRIMER -> t("Supprimer")
    ActionGeste.RIEN -> t("Rien")
}
