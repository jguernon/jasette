package ca.jasette.android.ui

import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.MutableTransitionState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.slideInVertically
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Marques
import ca.jasette.android.Partage
import ca.jasette.android.Service
import ca.jasette.android.plier
import ca.jasette.android.t
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/** Combien de temps on attend qu'un service se connecte (app froide) avant de dire qu'il ne l'est pas. */
private const val ATTENTE_CONNEXION_MS = 40_000L

private enum class Etape { ATTENTE, ENVOI, ENVOYE, ECHEC }

/** Une conversation choisie, pendant l'envoi : ses envois (tmp_id) et ce qui a mal tourné. */
private class Cible(val conv: String) {
    /** Remis au Depot (les tmp_id sont connus). */
    var lance by mutableStateOf(false)
    /** Les fichiers sont en train d'être copiés : on ne ferme pas encore. */
    var copie by mutableStateOf(false)
    var tmps by mutableStateOf<List<String>>(emptyList())
    /** Le service ne s'est pas connecté à temps : Réessayer relance tout. */
    var erreurConnexion by mutableStateOf<String?>(null)
    /** Des fichiers refusés (taille, lecture) : on le dit, sans pouvoir réessayer. */
    var erreurCopie by mutableStateOf<String?>(null)
}

/** L'état d'un envoi, d'après les bulles locales de [Depot] (« ENVOI », « ÉCHEC », puis confirmées). */
private fun etape(c: Cible): Etape {
    if (c.erreurConnexion != null) return Etape.ECHEC
    if (!c.lance) return Etape.ATTENTE
    val liste = Depot.messages[c.conv].orEmpty()
    var enCours = false
    for (tmp in c.tmps) {
        // Absente : remplacée par le message confirmé du service.
        val m = liste.firstOrNull { it.tmpId == tmp } ?: continue
        if (m.status == "ÉCHEC") return Etape.ECHEC
        if (m.local) enCours = true
    }
    if (enCours) return Etape.ENVOI
    return if (c.erreurCopie != null) Etape.ECHEC else Etape.ENVOYE
}

private fun reessayable(c: Cible): Boolean =
    c.erreurConnexion != null || c.tmps.any { tmp ->
        Depot.peutReessayer(tmp) && Depot.messages[c.conv].orEmpty().any { it.tmpId == tmp && it.status == "ÉCHEC" }
    }

private fun nomDe(c: Conv) = c.name.ifEmpty { c.members.joinToString(", ") }.ifEmpty { c.service.court }

/**
 * L'écran de partage, monté par-dessus l'app d'origine : l'aperçu de ce qui est
 * partagé, un message facultatif, la recherche, les conversations récentes (une
 * ou plusieurs, cochées) et le bouton Envoyer ; puis le suivi de l'envoi.
 */
@Composable
fun EcranPartage(partage: Partage, choisie: String?, fermer: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val avecFichiers = partage.uris.isNotEmpty()
    // Choisie par un raccourci de partage direct : déjà cochée (sauf si son service refuse les fichiers).
    val refusInitial = choisie != null && avecFichiers && !Service.de(choisie).fichiers
    val choix = remember {
        mutableStateListOf<String>().apply { if (choisie != null && !refusInitial) add(choisie) }
    }
    var message by remember { mutableStateOf("") }
    var cibles by remember { mutableStateOf<List<Cible>?>(null) }
    val apparition = remember { MutableTransitionState(false).apply { targetState = true } }

    fun texteAEnvoyer() = listOf(message.trim(), partage.texte).filter { it.isNotEmpty() }.joinToString("\n")

    fun lancer(c: Cible) {
        c.erreurConnexion = null
        c.erreurCopie = null
        c.lance = false
        c.tmps = emptyList()
        scope.launch {
            val s = Service.de(c.conv)
            val pret = withTimeoutOrNull(ATTENTE_CONNEXION_MS) { snapshotFlow { Depot.pret(s) }.first { it } }
            if (pret == null) {
                c.erreurConnexion = t("%s n'est pas connecté pour l'instant.").format(s.court)
                return@launch
            }
            val texte = texteAEnvoyer()
            if (!avecFichiers) {
                c.tmps = listOfNotNull(Depot.envoyer(c.conv, texte))
                c.lance = true
            } else {
                c.copie = true
                ca.jasette.android.ui.Envoi.envoyer(
                    ctx, c.conv, partage.uris, texte,
                    envoyes = {
                        c.tmps = it
                        c.lance = true
                        c.copie = false
                    },
                ) { c.erreurCopie = it }
            }
        }
    }

    fun envoyer() {
        val l = choix.toList().filter { !Marques.estPrivee(it) && (!avecFichiers || Service.de(it).fichiers) }
        if (l.isEmpty()) return
        val nouvelles = l.map { Cible(it) }
        cibles = nouvelles
        nouvelles.forEach { lancer(it) }
    }

    val enSuivi = cibles
    val copieEnCours = enSuivi?.any { it.copie } == true
    // Pendant la copie des fichiers, on reste (la permission de lire le partage tient à cette fenêtre).
    BackHandler(enabled = copieEnCours) {}

    Box(
        Modifier.fillMaxSize().clickable(remember { MutableInteractionSource() }, indication = null) {
            if (enSuivi == null) fermer()
        },
        contentAlignment = Alignment.BottomCenter,
    ) {
        AnimatedVisibility(apparition, enter = slideInVertically { it / 3 } + fadeIn()) {
            val haut = RoundedCornerShape(topStart = rayon(24.dp), topEnd = rayon(24.dp))
            Column(
                Modifier.fillMaxWidth().fillMaxHeight(0.92f)
                    .clip(haut).fondFenetre()
                    .let { if (P.bordCarte.alpha > 0f) it.border(1.dp, P.bordCarte, haut) else it }
                    // Les touches dans la feuille ne la ferment pas.
                    .clickable(remember { MutableInteractionSource() }, indication = null) {}
                    .navigationBarsPadding().imePadding(),
            ) {
                // La poignée et le titre.
                Box(Modifier.fillMaxWidth().padding(top = 8.dp), contentAlignment = Alignment.Center) {
                    Box(Modifier.size(36.dp, 4.dp).clip(formePilule).background(P.textePale.copy(alpha = 0.5f)))
                }
                Row(
                    Modifier.fillMaxWidth().padding(start = 18.dp, end = 6.dp, top = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        t("Partager dans Jasette"), color = P.titre, fontSize = 20.sp, fontWeight = FontWeight.Bold,
                        modifier = Modifier.weight(1f),
                    )
                    IconButton(onClick = fermer, enabled = !copieEnCours) {
                        Icon(Icons.Filled.Close, t("Fermer"), tint = P.icone)
                    }
                }
                if (enSuivi == null) {
                    Choix(
                        partage, choix, refusInitial, message, { message = it },
                        peutEnvoyer = choix.isNotEmpty() && (texteAEnvoyer().isNotEmpty() || avecFichiers),
                        envoyer = { envoyer() },
                    )
                } else {
                    Suivi(enSuivi, reessayer = { c ->
                        if (c.erreurConnexion != null) {
                            lancer(c)
                        } else {
                            val liste = Depot.messages[c.conv].orEmpty()
                            c.tmps.filter { tmp -> liste.any { it.tmpId == tmp && it.status == "ÉCHEC" } }
                                .forEach { Depot.reessayer(c.conv, it) }
                        }
                    }, fermer = fermer, copieEnCours = copieEnCours)
                }
            }
        }
    }
}

/** Avant l'envoi : l'aperçu, le message, la recherche, les conversations et le bouton Envoyer. */
@Composable
private fun androidx.compose.foundation.layout.ColumnScope.Choix(
    partage: Partage,
    choix: MutableList<String>,
    refusInitial: Boolean,
    message: String,
    changerMessage: (String) -> Unit,
    peutEnvoyer: Boolean,
    envoyer: () -> Unit,
) {
    val avecFichiers = partage.uris.isNotEmpty()
    var filtre by remember { mutableStateOf("") }
    Apercu(partage)
    // Le message facultatif.
    Champ(message, changerMessage, t("Ajouter un message (facultatif)"), lignes = 4, icone = null)
    Champ(filtre, { filtre = it }, t("Rechercher une conversation"), lignes = 1, icone = Icons.Filled.Search)
    if (refusInitial) {
        Avis(t("Bluesky ne permet pas d'envoyer des fichiers en message privé. Choisis une autre conversation."))
    }

    // Les conversations : la boîte de réception (récentes d'abord) ; la recherche
    // fouille aussi les archives. Jamais les privées, ni celles en lecture seule.
    val q = plier(filtre.trim())
    val toutes = (Depot.boite + Depot.toutesArchives).distinctBy { it.id }
        .filter { !it.readOnly && !Marques.estPrivee(it.id) }
    val liste = if (q.isEmpty()) {
        val dabord = choix.mapNotNull { id -> toutes.firstOrNull { it.id == id } }
        (dabord + Depot.boite.filter { !it.readOnly && !Marques.estPrivee(it.id) }).distinctBy { it.id }
    } else {
        toutes.filter { c ->
            plier(c.name).contains(q) || c.members.any { plier(it).contains(q) } || plier(c.service.court).contains(q)
        }
    }
    Text(
        if (q.isEmpty()) t("Conversations récentes") else t("Résultats"),
        color = P.textePale, fontSize = 13.sp, fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = 18.dp, top = 10.dp, bottom = 4.dp),
    )
    Box(Modifier.weight(1f).fillMaxWidth()) {
        if (liste.isEmpty()) {
            Text(
                if (q.isNotEmpty()) t("Aucun résultat")
                else t("Aucune conversation pour l'instant. Ouvre Jasette pour connecter tes messageries."),
                color = P.textePale, fontSize = 14.sp, modifier = Modifier.padding(18.dp),
            )
        } else {
            LazyColumn(Modifier.fillMaxSize()) {
                items(liste, key = { it.id }) { c ->
                    val refusee = avecFichiers && !c.service.fichiers
                    LigneConv(c, c.id in choix, refusee) {
                        if (c.id in choix) choix.remove(c.id) else choix.add(c.id)
                    }
                }
            }
        }
    }

    // Les choisies (dont celles hors de la liste filtrée), puis Envoyer.
    if (choix.size > 1) {
        LazyRow(
            Modifier.fillMaxWidth().padding(top = 6.dp),
            contentPadding = PaddingValues(horizontal = 14.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            items(choix.toList(), key = { it }) { id ->
                val c = Depot.conv(id)
                Row(
                    Modifier.clip(formePilule).background(P.choisi).clickable { choix.remove(id) }
                        .padding(start = 4.dp, end = 10.dp, top = 4.dp, bottom = 4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (c != null) Avatar(c, 24.dp, badge = false)
                    Spacer(Modifier.width(6.dp))
                    Text(c?.let { nomDe(it) } ?: id, color = P.texteFort, fontSize = 13.sp, maxLines = 1)
                    Spacer(Modifier.width(4.dp))
                    Icon(Icons.Filled.Close, t("Retirer"), tint = P.textePale, modifier = Modifier.size(14.dp))
                }
            }
        }
    }
    val libelle = when {
        choix.isEmpty() -> t("Choisis une conversation")
        choix.size == 1 -> t("Envoyer à %s").format(Depot.conv(choix[0])?.let { nomDe(it) } ?: "…")
        else -> t("Envoyer à %d conversations").format(choix.size)
    }
    GrosBouton(libelle, Icons.AutoMirrored.Filled.Send, actif = peutEnvoyer, onClick = envoyer)
}

/** L'aperçu de ce qui est partagé : les vignettes des fichiers, le texte ou le lien. */
@Composable
private fun Apercu(p: Partage) {
    if (p.uris.isNotEmpty()) {
        LazyRow(
            Modifier.fillMaxWidth().padding(top = 6.dp),
            contentPadding = PaddingValues(horizontal = 14.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(p.uris, key = { it.toString() }) { VignettePartage(it) }
        }
    }
    if (p.texte.isNotEmpty()) {
        val lien = Regex("^https?://\\S+$").matches(p.texte)
        Column(
            Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 6.dp).carte(16.dp)
                .padding(horizontal = 12.dp, vertical = 10.dp),
        ) {
            if (p.sujet.isNotEmpty() && p.sujet != p.texte) {
                Text(
                    p.sujet, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 14.sp,
                    maxLines = 2, overflow = TextOverflow.Ellipsis,
                )
            }
            Text(
                (if (lien) "🔗 " else "") + p.texte,
                color = if (lien) P.accent else P.texte, fontSize = 14.sp,
                maxLines = 4, overflow = TextOverflow.Ellipsis,
            )
        }
    }
    if (p.vide) {
        Text(
            t("Rien à partager : écris un message."), color = P.textePale, fontSize = 14.sp,
            modifier = Modifier.padding(horizontal = 18.dp, vertical = 6.dp),
        )
    }
}

/** La vignette d'un fichier partagé (image ou vidéo), sinon son nom. */
@Composable
private fun VignettePartage(u: Uri) {
    val ctx = LocalContext.current
    val image by produceState<ImageBitmap?>(null, u) { value = Envoi.vignette(ctx, u) }
    val nom = remember(u) { Envoi.nom(ctx, u) }
    val cadre = Modifier.size(76.dp).carte(12.dp)
    val img = image
    if (img != null) {
        Image(img, nom, cadre, contentScale = ContentScale.Crop)
    } else {
        Box(cadre.padding(6.dp), contentAlignment = Alignment.Center) {
            Text("📎 $nom", color = P.texte, fontSize = 10.sp, maxLines = 4, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** Un champ arrondi (message ou recherche), comme la barre de recherche de la liste. */
@Composable
private fun Champ(
    texte: String,
    changer: (String) -> Unit,
    indice: String,
    lignes: Int,
    icone: androidx.compose.ui.graphics.vector.ImageVector?,
) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 4.dp).champRond()
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icone != null) {
            Icon(icone, null, tint = P.textePale, modifier = Modifier.size(18.dp))
            Spacer(Modifier.width(8.dp))
        }
        Box(Modifier.weight(1f), contentAlignment = Alignment.CenterStart) {
            if (texte.isEmpty()) Text(indice, color = P.textePale, fontSize = 15.sp, maxLines = 1)
            BasicTextField(
                texte, changer, singleLine = lignes == 1, maxLines = lignes,
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

/** Un avis bien visible (service qui refuse les fichiers). */
@Composable
private fun Avis(texte: String) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 4.dp)
            .carte(14.dp, fond = P.danger.copy(alpha = 0.14f), bord = P.danger.copy(alpha = 0.5f))
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Filled.Warning, null, tint = P.danger, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Text(texte, color = P.texteFort, fontSize = 13.sp)
    }
}

/** Une conversation : avatar (badge du service), nom, service, et sa case à cocher. */
@Composable
private fun LigneConv(c: Conv, choisie: Boolean, refusee: Boolean, basculer: () -> Unit) {
    val etat = Depot.etat(c.service)
    Row(
        Modifier.fillMaxWidth()
            .background(if (choisie) P.choisi else Color.Transparent)
            .clickable(enabled = !refusee, onClick = basculer)
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .alpha(if (refusee) 0.55f else 1f),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Avatar(c, 46.dp)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(
                nomDe(c), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 16.sp,
                maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                LogoService(c.service, 14.dp)
                Spacer(Modifier.width(6.dp))
                val (texte, couleur) = when {
                    refusee -> t("%s ne prend pas les fichiers").format(c.service.court) to P.danger
                    etat != ca.jasette.android.Etat.PRET -> "${c.service.court} · ${etat.texte}" to P.textePale
                    else -> c.service.court to P.textePale
                }
                Text(texte, color = couleur, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        Spacer(Modifier.width(10.dp))
        CochePartage(choisie, refusee)
    }
}

/** La case ronde (carrée en EGA) des autres messageries. */
@Composable
private fun CochePartage(choisie: Boolean, refusee: Boolean) {
    val m = Modifier.size(24.dp).clip(formeRonde)
    if (choisie) {
        Box(m.background(P.degrade), contentAlignment = Alignment.Center) {
            Icon(Icons.Filled.Check, t("Choisie"), tint = P.surAccent, modifier = Modifier.size(18.dp))
        }
    } else {
        Box(m.border(2.dp, if (refusee) P.traitFin else P.textePale, formeRonde))
    }
}

/** Le gros bouton du bas (Envoyer, Réessayer), au dégradé de l'accent. */
@Composable
private fun GrosBouton(
    libelle: String,
    icone: androidx.compose.ui.graphics.vector.ImageVector,
    actif: Boolean,
    onClick: () -> Unit,
) {
    val f = forme(26.dp)
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = 10.dp).heightIn(min = 52.dp)
            .clip(f)
            .let { if (actif) it.background(P.degrade) else it.background(P.champ) }
            .clickable(enabled = actif, onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        Icon(icone, null, tint = if (actif) P.surAccent else P.textePale, modifier = Modifier.size(20.dp))
        Spacer(Modifier.width(10.dp))
        Text(
            libelle, color = if (actif) P.surAccent else P.textePale, fontSize = 16.sp, fontWeight = FontWeight.Bold,
            maxLines = 1, overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Après Envoyer : l'état de chaque envoi, Réessayer si besoin, et la fermeture une fois tout parti. */
@Composable
private fun androidx.compose.foundation.layout.ColumnScope.Suivi(
    cibles: List<Cible>,
    reessayer: (Cible) -> Unit,
    fermer: () -> Unit,
    copieEnCours: Boolean,
) {
    val etapes = cibles.map { etape(it) }
    val fini = etapes.all { it == Etape.ENVOYE }
    val echec = etapes.any { it == Etape.ECHEC }
    val enCours = etapes.any { it == Etape.ATTENTE || it == Etape.ENVOI }
    // Tout est parti : on le montre un instant, puis on revient à l'app d'origine.
    LaunchedEffect(fini) {
        if (fini) {
            delay(900)
            fermer()
        }
    }
    Column(
        Modifier.fillMaxWidth().padding(vertical = 18.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(Modifier.size(64.dp), contentAlignment = Alignment.Center) {
            when {
                fini -> Box(Modifier.size(64.dp).clip(formeRonde).background(P.succes), contentAlignment = Alignment.Center) {
                    Icon(Icons.Filled.Check, null, tint = Color.White, modifier = Modifier.size(40.dp))
                }
                enCours -> CircularProgressIndicator(color = P.accent, modifier = Modifier.size(48.dp))
                else -> Box(Modifier.size(64.dp).clip(formeRonde).background(P.danger), contentAlignment = Alignment.Center) {
                    Icon(Icons.Filled.Warning, null, tint = Color.White, modifier = Modifier.size(36.dp))
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        Text(
            when {
                fini -> t("Envoyé")
                enCours -> t("Envoi en cours…")
                else -> t("Échec de l'envoi")
            },
            color = P.titre, fontSize = 20.sp, fontWeight = FontWeight.Bold,
        )
    }
    LazyColumn(Modifier.weight(1f).fillMaxWidth()) {
        items(cibles, key = { it.conv }) { c ->
            val e = etape(c)
            val conv = Depot.conv(c.conv)
            Row(
                Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (conv != null) Avatar(conv, 40.dp) else Spacer(Modifier.size(40.dp))
                Spacer(Modifier.width(12.dp))
                Column(Modifier.weight(1f)) {
                    Text(
                        conv?.let { nomDe(it) } ?: Service.de(c.conv).court, color = P.texteFort,
                        fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    )
                    val s = Service.de(c.conv)
                    val (texte, couleur) = when (e) {
                        Etape.ATTENTE ->
                            (if (Depot.pret(s)) t("Préparation…") else t("Connexion à %s…").format(s.court)) to P.textePale
                        Etape.ENVOI -> t("Envoi…") to P.textePale
                        Etape.ENVOYE -> t("Envoyé") to P.succes
                        Etape.ECHEC -> (c.erreurConnexion ?: c.erreurCopie ?: t("Échec de l'envoi")) to P.danger
                    }
                    Text(texte, color = couleur, fontSize = 13.sp)
                }
                Spacer(Modifier.width(8.dp))
                when (e) {
                    Etape.ATTENTE, Etape.ENVOI ->
                        CircularProgressIndicator(color = P.accent, strokeWidth = 2.dp, modifier = Modifier.size(20.dp))
                    Etape.ENVOYE -> Icon(Icons.Filled.Check, null, tint = P.succes, modifier = Modifier.size(22.dp))
                    Etape.ECHEC -> if (reessayable(c)) {
                        IconButton(onClick = { reessayer(c) }) {
                            Icon(Icons.Filled.Refresh, t("Réessayer"), tint = P.accent)
                        }
                    } else {
                        Icon(Icons.Filled.Warning, null, tint = P.danger, modifier = Modifier.size(22.dp))
                    }
                }
            }
        }
    }
    if (etapes.any { it == Etape.ENVOI } && !copieEnCours) {
        Text(
            t("Tu peux fermer : l'envoi continue en arrière-plan."), color = P.textePale, fontSize = 13.sp,
            modifier = Modifier.padding(horizontal = 18.dp),
        )
    }
    if (echec && !enCours && cibles.any { reessayable(it) }) {
        GrosBouton(t("Réessayer"), Icons.Filled.Refresh, actif = true) { cibles.filter { reessayable(it) }.forEach(reessayer) }
    }
    if (!fini) {
        Box(Modifier.fillMaxWidth().padding(bottom = 8.dp), contentAlignment = Alignment.Center) {
            androidx.compose.material3.TextButton(onClick = fermer, enabled = !copieEnCours) {
                Text(t("Fermer"), color = if (copieEnCours) P.textePale else P.texteFort, fontSize = 15.sp)
            }
        }
    }
}
