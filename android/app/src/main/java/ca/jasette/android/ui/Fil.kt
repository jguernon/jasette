package ca.jasette.android.ui

import ca.jasette.android.t
import android.app.Activity
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
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
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
import ca.jasette.android.Msg
import ca.jasette.android.Piece
import ca.jasette.android.PontService
import ca.jasette.android.Service
import ca.jasette.android.statut

/** Le fil d'une conversation : bulles, images, réactions, et la barre d'envoi. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Fil(activite: Activity, id: String, retour: () -> Unit) {
    val conv = Depot.convs.firstOrNull { it.id == id }
    val liste = Depot.messages[id].orEmpty()
    var plein by remember { mutableStateOf<String?>(null) }

    // Ouvrir : charge les messages, marque lu, retire la notification.
    DisposableEffect(id) {
        Depot.ouvrir(id)
        NotificationManagerCompat.from(activite).cancel(PontService.idNotification(id))
        onDispose { if (Depot.ouverte == id) Depot.ouverte = null }
    }

    Scaffold(
        containerColor = P.fil,
        topBar = {
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = retour) { Icon(Icons.AutoMirrored.Filled.ArrowBack, t("Retour")) }
                },
                title = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        if (conv != null) Avatar(conv, 36.dp)
                        Spacer(Modifier.width(10.dp))
                        Column {
                            Text(
                                conv?.name ?: "", fontSize = 17.sp, fontWeight = FontWeight.SemiBold,
                                maxLines = 1, overflow = TextOverflow.Ellipsis,
                            )
                            Text(
                                Service.de(id).court + if (conv?.rcs == true) " · RCS" else "",
                                fontSize = 12.sp, color = P.textePale,
                            )
                        }
                    }
                },
                actions = { if (conv != null) BoutonsAppel(activite, conv) },
                colors = barreCouleurs(),
            )
        },
        bottomBar = { Saisie(id, actif = Depot.pret(Service.de(id)) && conv?.readOnly != true) },
    ) { marges ->
        val etat = rememberLazyListState()
        // Le plus récent en bas : la liste est inversée.
        val inverse = liste.asReversed()
        LaunchedEffect(liste.lastOrNull()?.id) { if (liste.isNotEmpty()) etat.animateScrollToItem(0) }
        LazyColumn(
            Modifier.fillMaxSize().padding(marges).background(P.fil),
            state = etat,
            reverseLayout = true,
            contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 10.dp, vertical = 8.dp),
        ) {
            items(inverse.size, key = { inverse[it].id }) { i ->
                val m = inverse[i]
                val avant = inverse.getOrNull(i + 1) // le message précédent dans le temps
                val apres = inverse.getOrNull(i - 1)
                Column {
                    if (avant == null || jourDe(avant.time) != jourDe(m.time)) Jour(m.time)
                    val premier = avant == null || avant.fromMe != m.fromMe || avant.sender != m.sender
                    val dernier = apres == null || apres.fromMe != m.fromMe || apres.sender != m.sender
                    Message(m, groupe = conv?.group == true, premier = premier, dernier = dernier) { plein = it }
                }
            }
            item {
                Box(Modifier.fillMaxWidth().padding(8.dp), contentAlignment = Alignment.Center) {
                    when {
                        Depot.chargement == id -> CircularProgressIndicator(Modifier.size(24.dp), color = P.accent)
                        Depot.plusAnciens[id] == true ->
                            TextButton(onClick = { Depot.plusVieux(id) }) { Text(t("Messages plus anciens")) }
                    }
                }
            }
        }
    }

    plein?.let { chemin ->
        Dialog(onDismissRequest = { plein = null }, properties = DialogProperties(usePlatformDefaultWidth = false)) {
            Box(Modifier.fillMaxSize().background(P.fil).clickable { plein = null }, contentAlignment = Alignment.Center) {
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
        else -> {
            val nom = conv.service.court
            IconButton(onClick = { Appels.ouvrirMeta(activite, conv) }) { Icon(Icones.camera, "${t("Appel vidéo dans")} $nom") }
            IconButton(onClick = { Appels.ouvrirMeta(activite, conv) }) { Icon(Icons.Filled.Phone, "${t("Appel dans")} $nom") }
        }
    }
}

@Composable
private fun Jour(ms: Long) {
    Box(Modifier.fillMaxWidth().padding(vertical = 10.dp), contentAlignment = Alignment.Center) {
        Text(
            jour(ms), color = P.texteFort, fontSize = 12.sp,
            modifier = Modifier.clip(RoundedCornerShape(50)).background(P.jourFond).padding(horizontal = 12.dp, vertical = 3.dp),
        )
    }
}

@Composable
private fun Message(m: Msg, groupe: Boolean, premier: Boolean, dernier: Boolean, agrandir: (String) -> Unit) {
    val alignement = if (m.fromMe) Alignment.End else Alignment.Start
    Column(
        Modifier.fillMaxWidth().padding(top = if (premier) 8.dp else 2.dp),
        horizontalAlignment = alignement,
        verticalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        if (groupe && !m.fromMe && premier && m.sender.isNotEmpty()) {
            Text(
                m.sender, color = P.expediteur, fontSize = 12.sp, fontWeight = FontWeight.SemiBold,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        for (p in m.attachments) {
            if (p.estImage) PieceImage(p, agrandir) else PieceAutre(p.name.ifEmpty { p.mime })
        }
        if (m.attachments.isEmpty() && m.media.isNotEmpty()) PieceAutre(m.media)
        val echec = statut(m.status) == t("Échec")
        if (m.text.isNotEmpty()) {
            val rayon = 18.dp
            val forme = if (m.fromMe) {
                RoundedCornerShape(rayon, rayon, if (dernier) 4.dp else rayon, rayon)
            } else {
                RoundedCornerShape(rayon, rayon, rayon, if (dernier) 4.dp else rayon)
            }
            Column(
                Modifier.widthIn(max = 300.dp).clip(forme).background(if (m.fromMe) P.accent else P.surface)
                    .padding(horizontal = 13.dp, vertical = 8.dp),
            ) {
                Text(m.text, color = if (m.fromMe) P.texteFort else P.texteFort, fontSize = 15.sp)
                Text(
                    heure(m.time) + (if (m.fromMe) statut(m.status).let { if (it.isEmpty()) "" else " · $it" } else ""),
                    color = if (echec) P.danger else if (m.fromMe) P.heureEnvoyee else P.heureRecue,
                    fontSize = 11.sp, modifier = Modifier.align(Alignment.End),
                )
            }
        } else if (dernier || echec) {
            Text(
                heure(m.time) + if (echec) " · ${t("Échec")}" else "",
                color = if (echec) P.danger else P.textePale, fontSize = 11.sp,
                modifier = Modifier.padding(horizontal = 6.dp),
            )
        }
        if (m.reactions.isNotEmpty()) Reactions(m)
    }
}

/** Les réactions groupées par emoji, sous la bulle. */
@Composable
private fun Reactions(m: Msg) {
    val groupes = m.reactions.groupBy { it.emoji }
    Row(
        Modifier.padding(horizontal = 6.dp).clip(RoundedCornerShape(50)).background(P.surface)
            .padding(horizontal = 8.dp, vertical = 2.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        for ((emoji, qui) in groupes) {
            Text(emoji, fontSize = 15.sp)
            if (qui.size > 1) Text(qui.size.toString(), color = P.texte, fontSize = 12.sp)
        }
    }
}

@Composable
private fun PieceImage(p: Piece, agrandir: (String) -> Unit) {
    val chemin = Depot.medias[p.mediaId]
    val ratio = if (p.w > 0 && p.h > 0) (p.w.toFloat() / p.h).coerceIn(0.5f, 2.5f) else 1.3f
    val cadre = Modifier.widthIn(max = 260.dp).fillMaxWidth(0.7f).aspectRatio(ratio)
        .clip(RoundedCornerShape(16.dp)).background(P.surface)
    if (chemin != null) {
        ImageFichier(chemin, cadre.clickable { agrandir(chemin) })
    } else {
        Box(cadre, contentAlignment = Alignment.Center) {
            if (Depot.echecs.containsKey(p.mediaId)) {
                Text(t("Image indisponible"), color = P.textePale, fontSize = 12.sp)
            } else {
                CircularProgressIndicator(Modifier.size(22.dp), color = P.textePale, strokeWidth = 2.dp)
            }
        }
    }
}

@Composable
private fun PieceAutre(nom: String) {
    Text(
        "📎 $nom", color = P.texte, fontSize = 13.sp,
        modifier = Modifier.clip(RoundedCornerShape(16.dp)).background(P.surface).padding(horizontal = 12.dp, vertical = 8.dp),
    )
}

/** La barre d'envoi : un champ arrondi et le bouton d'envoi. */
@Composable
private fun Saisie(conv: String, actif: Boolean) {
    var texte by remember(conv) { mutableStateOf("") }
    Row(
        Modifier.fillMaxWidth().background(P.fil).navigationBarsPadding().imePadding()
            .padding(horizontal = 10.dp, vertical = 8.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        Box(
            Modifier.weight(1f).heightIn(min = 44.dp).clip(RoundedCornerShape(22.dp)).background(P.surface)
                .padding(horizontal = 16.dp, vertical = 11.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            if (texte.isEmpty()) Text(if (actif) t("Message") else t("Envoi impossible pour l'instant"), color = P.textePale)
            BasicTextField(
                texte, { texte = it },
                enabled = actif,
                textStyle = TextStyle(color = P.texteFort, fontSize = 16.sp),
                cursorBrush = SolidColor(P.accent),
                maxLines = 6,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        Spacer(Modifier.width(8.dp))
        val peut = actif && texte.isNotBlank()
        IconButton(
            onClick = {
                Depot.envoyer(conv, texte)
                texte = ""
            },
            enabled = peut,
            modifier = Modifier.size(44.dp).clip(CircleShape).background(if (peut) P.accent else P.surface),
        ) {
            Icon(Icons.AutoMirrored.Filled.Send, t("Envoyer"), tint = if (peut) P.texteFort else P.textePale)
        }
    }
}
