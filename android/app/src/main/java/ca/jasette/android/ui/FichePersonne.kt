package ca.jasette.android.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Candidat
import ca.jasette.android.Contacts
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Groupe
import ca.jasette.android.Langue
import ca.jasette.android.LienFiche
import ca.jasette.android.Marques
import ca.jasette.android.Notes
import ca.jasette.android.Personnes
import ca.jasette.android.Raison
import ca.jasette.android.Service
import ca.jasette.android.choisirPrincipal
import ca.jasette.android.delier
import ca.jasette.android.e164
import ca.jasette.android.lier
import ca.jasette.android.mots
import ca.jasette.android.numeroDe
import ca.jasette.android.plier
import ca.jasette.android.refuser
import ca.jasette.android.regrouper
import ca.jasette.android.t
import ca.jasette.android.texteActivite

/*
 * La fiche de la personne (comme src/personne.rs du bureau) : toucher le nom
 * ou l'avatar dans l'en-tête d'une conversation à deux l'ouvre. Grand avatar,
 * sa pensée (note « Quoi de neuf ? »), les autres services où elle est ;
 * toucher une ligne ouvre cette conversation (« changer de service »).
 * Correspondance locale : Personnes.kt.
 */

/** Les conversations à deux connues (pas les grands espaces) ; les privées seulement avec une privée. */
private fun candidats(ancre: String): List<Candidat> {
    val prive = Marques.estPrivee(ancre)
    return (Depot.convs + Depot.archives).asSequence()
        .filter { !it.group && !it.large && Marques.estPrivee(it.id) == prive }
        .distinctBy { it.id }
        .map { Candidat(it.id, it.name, numeroDe(it.id, it.number, it.members.firstOrNull(), it.name)) }
        .toList()
}

/** (nom, numéro E.164) des contacts du téléphone. */
private fun contactsTel(): List<Pair<String, String>> =
    Contacts.liste.flatMap { c -> c.numeros.mapNotNull { n -> (n.e164?.let { e164(it) } ?: e164(n.brut))?.let { c.nom to it } } }

fun groupePersonne(ancre: String): Groupe =
    regrouper(ancre, candidats(ancre), Marques.personnes, Marques.personnesRefus, contactsTel())

/** Le nom de la personne : celui de la conversation principale, sinon celui de l'ancre. */
private fun nomPersonne(g: Groupe): String {
    val ids = g.ids
    val principal = Marques.personnes.firstOrNull { it.principal.isNotEmpty() && it.convs.any { c -> c in ids } }
        ?.principal?.takeIf { it in ids }
    return Depot.conv(principal ?: ids.firstOrNull() ?: "")?.name?.takeIf { it.isNotEmpty() } ?: t("Sans nom")
}

/** La conversation dont la photo est la meilleure : un contact du téléphone (grande photo), sinon un avatar du service. */
private fun meilleurePhoto(g: Groupe): Conv? {
    val convs = g.ids.mapNotNull { Depot.conv(it) }
    return convs.firstOrNull { Contacts.photoPour(it, grande = true) != null }
        ?: convs.firstOrNull { Depot.avatarDe(it) != null }
        ?: convs.firstOrNull()
}

private fun restant(ms: Long): String {
    val min = (ms.coerceAtLeast(0) + 59_999) / 60_000
    return if (min >= 60) "${min / 60} h" else "${min.coerceAtLeast(1)} min"
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FichePersonne(ancre: String, fermer: () -> Unit) {
    val etat = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    var lierOuvert by remember { mutableStateOf(false) }
    val g = groupePersonne(ancre)
    val nom = nomPersonne(g)
    fun groupeIds() = groupePersonne(ancre).ids
    ModalBottomSheet(onDismissRequest = fermer, sheetState = etat, containerColor = P.menu) {
        Column(
            Modifier.fillMaxWidth().navigationBarsPadding().verticalScroll(rememberScrollState())
                .padding(horizontal = 18.dp).padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            // La pensée, en bulle au-dessus de la tête.
            val note = if (lierOuvert) null else g.ids.firstNotNullOfOrNull { id -> Notes.deContact(id)?.let { id to it } }
            if (note != null) Pensee(Service.de(note.first), note.second.text, note.second.time)
            val photo = meilleurePhoto(g)
            val enLigne = g.ids.any { id -> Depot.conv(id)?.let { Depot.presenceDe(it)?.online } == true }
            val taille: Dp = if (lierOuvert) 72.dp else 140.dp
            if (photo != null) {
                Box(Modifier.size(taille)) {
                    Avatar(photo.copy(name = nom), taille, badge = false)
                    if (enLigne) {
                        val d = taille * 0.19f
                        val carre = P.ega || P.retro
                        Box(
                            Modifier.align(Alignment.BottomStart).padding(start = if (carre) 0.dp else taille * 0.06f, bottom = if (carre) 0.dp else taille * 0.06f)
                                .size(d).clip(if (carre) androidx.compose.ui.graphics.RectangleShape else androidx.compose.foundation.shape.CircleShape)
                                .background(P.menu).padding(3.dp)
                                .clip(if (carre) androidx.compose.ui.graphics.RectangleShape else androidx.compose.foundation.shape.CircleShape)
                                .background(if (P.ega) Color(0xff55ff55) else if (P.retro) Color(0xff2fb52a) else Color(0xff31c45a)),
                        )
                    }
                }
            }
            Text(nom, color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 24.sp, textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp))
            // Les noms sur les autres services, s'ils diffèrent.
            val vus = mutableListOf(mots(nom))
            for (id in g.ids) {
                val c = Depot.conv(id) ?: continue
                val m = mots(c.name)
                if (c.name.isEmpty() || m in vus) continue
                vus.add(m)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Image(painterResource(iconeService(c.service)), null, Modifier.size(14.dp).clip(formeLogo(14.dp)))
                    Spacer(Modifier.width(6.dp))
                    Text(
                        if (Langue.anglais) "“${c.name}” on ${c.service.titre}" else "« ${c.name} » sur ${c.service.titre}",
                        color = P.textePale, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            Spacer(Modifier.height(6.dp))
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (lierOuvert) {
                    ChoixLier(g.ids, retour = { lierOuvert = false }) { x ->
                        val (p, r) = lier(Marques.personnes, Marques.personnesRefus, groupeIds(), x, Personnes.nouvelId())
                        Marques.changerPersonnes(p, r)
                        lierOuvert = false
                    }
                } else {
                    Titre(t("Même personne sur"))
                    val seule = g.membres.size == 1
                    for ((id, lien) in g.membres) {
                        val c = Depot.conv(id) ?: continue
                        val detail = when (lien) {
                            LienFiche.ANCRE -> null
                            LienFiche.MAIN -> t("Lié à la main")
                            LienFiche.NUMERO -> t("Même numéro")
                        }
                        LigneConv(c, active = id == ancre, detail = detail, toucher = {
                            fermer()
                            if (id != ancre) Depot.aOuvrir = id
                        }) {
                            // Toute ligne qui n'est pas déjà la principale : même avec un nom
                            // pareil, la photo peut différer.
                            val estPrincipale = Marques.personnes.any { it.principal == id && id in it.convs }
                            if (!seule && !estPrincipale) {
                                PetitBouton(t("Utiliser ce nom et cette photo"), false) {
                                    Marques.changerPersonnes(choisirPrincipal(Marques.personnes, groupeIds(), id, Personnes.nouvelId()), Marques.personnesRefus)
                                }
                            }
                            if (lien != LienFiche.ANCRE) {
                                PetitBouton(t("Délier"), false) {
                                    val (p, r) = delier(Marques.personnes, Marques.personnesRefus, groupeIds(), id)
                                    Marques.changerPersonnes(p, r)
                                }
                            }
                        }
                    }
                    if (seule) {
                        Text(t("Aucune autre conversation trouvée pour cette personne."), color = P.textePale, fontSize = 13.sp, modifier = Modifier.padding(horizontal = 8.dp))
                    }
                    if (g.suggestions.isNotEmpty()) {
                        Spacer(Modifier.height(8.dp))
                        Titre(t("Est-ce la même personne ?"))
                        for ((id, raison) in g.suggestions) {
                            val c = Depot.conv(id) ?: continue
                            val pourquoi = when (raison) {
                                Raison.CONTACT -> t("Nom d'un contact du téléphone qui a ce numéro")
                                Raison.NOM -> t("Nom très proche")
                            }
                            Box(Modifier.fillMaxWidth().border(1.dp, P.traitFin, forme(14.dp))) {
                                LigneConv(c, active = false, detail = pourquoi, toucher = null) {
                                    PetitBouton(t("Oui"), true) {
                                        val (p, r) = lier(Marques.personnes, Marques.personnesRefus, groupeIds(), id, Personnes.nouvelId())
                                        Marques.changerPersonnes(p, r)
                                    }
                                    PetitBouton(t("Pas la même personne"), false) {
                                        Marques.changerPersonnes(Marques.personnes, refuser(Marques.personnesRefus, groupeIds(), id))
                                    }
                                }
                            }
                        }
                    }
                    Spacer(Modifier.height(6.dp))
                    Row(
                        Modifier.fillMaxWidth().clip(forme(14.dp)).clickable { lierOuvert = true }.padding(horizontal = 10.dp, vertical = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Icon(Icons.Filled.Add, null, tint = P.accent, modifier = Modifier.size(20.dp))
                        Spacer(Modifier.width(10.dp))
                        Text(t("Lier une autre conversation…"), color = P.accent, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                    }
                }
            }
        }
    }
}

@Composable
private fun Titre(texte: String) {
    Text(texte, color = P.textePale, fontWeight = FontWeight.Bold, fontSize = 12.sp, modifier = Modifier.padding(start = 4.dp, top = 4.dp))
}

/** La bulle de pensée : le texte en grand, le service et le temps restant, deux petits ronds vers la tête. */
@Composable
private fun Pensee(svc: Service, texte: String, temps: Long) {
    val reste = if (temps > 0) temps + 24 * 3600_000L - System.currentTimeMillis() else 0L
    val legende = if (reste > 0) "${svc.court} · ${restant(reste)}" else svc.court
    val fond = if (P.ega) Color.Black else P.surface
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Column(
            Modifier.widthIn(max = 300.dp).clip(forme(22.dp)).background(fond).border(1.dp, P.traitFin, forme(22.dp))
                .padding(horizontal = 18.dp, vertical = 10.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(texte, color = P.texteFort, fontSize = 19.sp, lineHeight = 25.sp, textAlign = TextAlign.Center)
            Text(legende, color = P.textePale, fontSize = 11.sp)
        }
        Box(Modifier.padding(start = 34.dp, top = 3.dp).size(11.dp).clip(formeRonde).background(fond).border(1.dp, P.traitFin, formeRonde))
        Box(Modifier.padding(start = 56.dp, top = 1.dp).size(6.dp).clip(formeRonde).background(fond).border(1.dp, P.traitFin, formeRonde))
    }
}

@Composable
private fun PetitBouton(texte: String, plein: Boolean, toucher: () -> Unit) {
    Box(
        Modifier.clip(formePilule).let { if (plein) it.background(P.accentPlein) else it.background(P.champ).border(1.dp, P.traitFin, formePilule) }
            .clickable(onClick = toucher).padding(horizontal = 12.dp, vertical = 6.dp),
    ) { Text(texte, color = if (plein) P.surAccent else P.texteFort, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, maxLines = 1) }
}

/**
 * Une conversation de la fiche : avatar et badge du service, nom, dernier
 * message ou présence, date, non-lu ; [boutons] sous le détail.
 */
@Composable
private fun LigneConv(c: Conv, active: Boolean, detail: String?, toucher: (() -> Unit)?, boutons: @Composable () -> Unit = {}) {
    Row(
        Modifier.fillMaxWidth().clip(forme(14.dp))
            .let { if (active) it.background(P.choisi).border(1.dp, P.accent, forme(14.dp)) else it }
            .let { if (toucher != null) it.clickable(onClick = toucher) else it }
            .padding(horizontal = 10.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Avatar(c, 44.dp, presence = true, fondPoint = if (active) P.choisi else P.menu, propre = true)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    c.name.ifEmpty { t("Sans nom") }, color = P.texteFort, fontSize = 15.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    fontWeight = if (c.unread) FontWeight.Bold else FontWeight.SemiBold, modifier = Modifier.weight(1f, fill = false),
                )
                Spacer(Modifier.width(6.dp))
                Text(c.service.court, color = P.textePale, fontSize = 12.sp, maxLines = 1)
                if (active) {
                    Spacer(Modifier.width(6.dp))
                    Text(
                        t("Ici"), color = P.accent, fontSize = 11.sp, fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.clip(formePilule).background(P.accent.copy(alpha = 0.15f)).padding(horizontal = 6.dp),
                    )
                }
            }
            val activite = Depot.presenceDe(c)?.let { texteActivite(it, System.currentTimeMillis()) }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    activite ?: c.preview.replace('\n', ' ').ifEmpty { c.service.court },
                    color = if (c.unread) P.texteFort else P.textePale, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (c.time > 0) Text(heureListe(c.time), color = P.textePale, fontSize = 12.sp)
            }
            if (detail != null) Text(detail, color = P.textePale, fontSize = 11.sp, maxLines = 1)
            Row(Modifier.horizontalScroll(rememberScrollState()).padding(top = 2.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) { boutons() }
        }
        if (c.unread) {
            Spacer(Modifier.width(8.dp))
            Box(Modifier.size(10.dp).clip(formeRonde).background(P.accent))
        }
    }
}

/** « Lier une autre conversation… » : recherche, puces par service, conversations à deux pas déjà sur la fiche. */
@Composable
private fun ChoixLier(groupe: List<String>, retour: () -> Unit, choisir: (String) -> Unit) {
    var texte by remember { mutableStateOf("") }
    var filtre by remember { mutableStateOf<Service?>(null) }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(
            Icons.AutoMirrored.Filled.ArrowBack, t("Retour"), tint = P.texteFort,
            modifier = Modifier.size(36.dp).clip(formeRonde).clickable(onClick = retour).padding(8.dp),
        )
        Text(t("Lier une autre conversation…"), color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 16.sp)
    }
    ChampFiltre(texte, focaliser = false, surSurface = true) { texte = it }
    val cands = (Depot.convs + Depot.archives).filter { !it.group && !it.large && it.id !in groupe && !Marques.estPrivee(it.id) }.distinctBy { it.id }
    val services = Service.entries.filter { s -> cands.any { it.service == s } }
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically,
    ) {
        Puce(filtre == null, true, { filtre = null }) { c -> Text(t("Tout"), color = c, fontSize = 14.sp) }
        for (s in services) {
            Puce(filtre == s, true, { filtre = s }) { c ->
                Image(painterResource(iconeService(s)), null, Modifier.size(18.dp).clip(formeLogo(18.dp)))
                if (filtre == s) Text(s.court, color = c, fontSize = 14.sp, maxLines = 1)
            }
        }
    }
    val q = plier(texte.trim())
    val chiffres = q.filter { it.isDigit() }
    val liste = cands.filter { filtre == null || it.service == filtre }
        .filter { q.isEmpty() || plier(it.name).contains(q) || (chiffres.length >= 3 && (it.number.filter { d -> d.isDigit() }.contains(chiffres) || it.id.contains(chiffres))) }
        .sortedByDescending { it.time }.take(40)
    if (liste.isEmpty()) Text(t("Aucune conversation"), color = P.textePale, fontSize = 13.sp, modifier = Modifier.padding(8.dp))
    for (c in liste) LigneConv(c, active = false, detail = null, toucher = { choisir(c.id) })
}
