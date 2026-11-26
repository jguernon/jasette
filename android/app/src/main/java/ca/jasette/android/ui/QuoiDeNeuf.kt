package ca.jasette.android.ui

import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Depot
import ca.jasette.android.Marques
import ca.jasette.android.Musique
import ca.jasette.android.Notes
import ca.jasette.android.Service
import ca.jasette.android.basculerNotesService
import ca.jasette.android.longueurNote
import ca.jasette.android.t

/*
 * « Quoi de neuf ? » (serveur/PROTOCOLE.md §5.12) : la bulle de l'écran
 * « Moi » (ma note, Publier, Effacer), le sélecteur « Publier sur » (marque
 * `notes_services`, le même dans Réglages > Quoi de neuf), la section des
 * Réglages et la petite bulle des notes des contacts dans le Head.
 */

/** La bulle « Quoi de neuf ? » : ma note actuelle, un champ, Publier et Effacer, puis un résultat par service. */
@Composable
fun BulleQuoiDeNeuf() {
    val actuelle = Notes.actuelle()
    var texte by rememberSaveable { mutableStateOf("") }
    val choisis = Notes.choisis()
    val limite = Notes.limite(choisis)
    val reste = limite - longueurNote(texte.trim())
    val enCours = Notes.resultats.values.any { it.enCours }
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)
            .clip(forme(20.dp)).background(P.carte).border(1.dp, P.bordCarte, forme(20.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(t("Quoi de neuf ?"), color = P.texteFort, fontWeight = FontWeight.Bold, fontSize = 18.sp)
        if (actuelle != null) {
            // La note actuelle, en bulle comme sur Instagram.
            Box(
                Modifier.clip(forme(16.dp)).background(P.surface).border(1.dp, P.bordBulle, forme(16.dp))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            ) { Text(actuelle.text, color = P.texteFort, fontSize = 15.sp) }
            if (actuelle.source == "musique") {
                Text(t("🎵 La musique remplace ta note pendant que tu écoutes"), color = P.textePale, fontSize = 12.sp)
            }
        } else {
            Text(t("Aucune note pour l'instant."), color = P.textePale, fontSize = 13.sp)
        }
        // Le champ rond commun (champRond, Theme.kt) ; sur trois lignes, il garde son rayon.
        Box(
            Modifier.fillMaxWidth().champRond()
                .let { if (reste < 0) it.border(1.dp, P.danger, formeChamp) else it }
                .padding(horizontal = 16.dp, vertical = 11.dp),
            contentAlignment = Alignment.CenterStart,
        ) {
            if (texte.isEmpty()) Text(t("Partage une pensée…"), color = P.textePale, fontSize = 15.sp, maxLines = 1)
            BasicTextField(
                texte, { texte = it }, maxLines = 3,
                textStyle = TextStyle(color = P.texteFort, fontSize = 15.sp, fontFamily = P.police),
                cursorBrush = SolidColor(P.accent),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        Text(
            if (choisis.isEmpty()) t("Aucun service compatible connecté.")
            else "$reste ${t("caractères restants")}",
            color = if (reste < 0) P.danger else P.textePale, fontSize = 12.sp,
            modifier = Modifier.padding(horizontal = 16.dp),
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            Button(
                onClick = {
                    if (Notes.publier(texte).isNotEmpty()) texte = ""
                },
                enabled = !enCours && choisis.isNotEmpty() && texte.isNotBlank() && reste >= 0,
                colors = ButtonDefaults.buttonColors(containerColor = P.accentPlein, contentColor = P.surAccent),
                shape = formeBouton,
            ) { Text(t("Publier")) }
            TextButton(onClick = { Notes.effacer() }, enabled = !enCours && actuelle != null, shape = formeBouton) {
                Text(t("Effacer"), color = if (actuelle != null) P.danger else P.textePale)
            }
        }
        // Après la publication : ✓ ou l'erreur, par service.
        for ((s, r) in Notes.resultats.toList().sortedBy { it.first.ordinal }) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                LogoService(s, 16.dp)
                when {
                    r.enCours -> Text("${s.court} : ${t("publication…")}", color = P.textePale, fontSize = 13.sp)
                    r.erreur == null -> {
                        Text(s.court, color = P.texte, fontSize = 13.sp)
                        Icon(Icons.Filled.Check, null, tint = P.succes, modifier = Modifier.size(16.dp))
                    }
                    else -> Text("${s.court} : ${r.erreur}", color = P.danger, fontSize = 13.sp)
                }
            }
        }
    }
}

/** Le nom d'une sélection `notes_services` : « Tout », ou les services choisis. */
private fun nomSelection(): String {
    val sel = Marques.notesServices
    if (sel == listOf("tout")) return t("Tout")
    val noms = sel.mapNotNull { Service.deCle(it)?.court ?: it.replaceFirstChar { c -> c.uppercase() } }
    return noms.joinToString(", ").ifEmpty { t("Tout") }
}

/**
 * « Publier sur : Tout ▸ », replié par défaut ; ouvert : la case « Tout »,
 * une case par service compatible connecté, et l'audience Instagram. Écrit
 * la marque `notes_services` (la même dans « Moi » et dans les Réglages).
 */
@Composable
fun SelecteurServicesNotes(marge: Dp = 16.dp) {
    var ouvert by rememberSaveable { mutableStateOf(false) }
    val compatibles = Notes.compatibles()
    val sel = Marques.notesServices
    val tout = sel == listOf("tout")
    Column(Modifier.fillMaxWidth()) {
        LigneRepliable("${t("Publier sur :")} ${nomSelection()}", ouvert, marge) { ouvert = !ouvert }
        if (!ouvert) return@Column
        CaseCochee(t("Tout"), t("Tous les services compatibles connectés, et ceux à venir"), tout, marge) {
            Marques.choisirNotesServices(listOf("tout"))
        }
        if (compatibles.isEmpty()) {
            Text(
                t("Aucun service compatible connecté (Instagram, Messenger, WhatsApp, Telegram, Bluesky…)."),
                color = P.textePale, fontSize = 13.sp, modifier = Modifier.padding(horizontal = marge + 34.dp, vertical = 4.dp),
            )
        }
        for (s in compatibles) {
            val detail = when (s.cle) {
                "instagram", "messenger" -> t("Note, 60 caractères, 24 h")
                "whatsapp" -> t("Remplace « Infos » pendant 24 h")
                "telegram" -> t("Remplace la bio, 70 caractères")
                "bluesky" -> t("Remplace la description du profil")
                "discord" -> t("Statut personnalisé")
                else -> ""
            }
            CaseCochee(s.court, detail, tout || s.cle in sel, marge, logo = s) {
                Marques.choisirNotesServices(basculerNotesService(sel, s.cle, compatibles.map { it.cle }))
            }
        }
        if (compatibles.any { it == Service.INSTAGRAM } && (tout || "instagram" in sel)) {
            Text(
                t("Audience Instagram"), color = P.texte, fontSize = 13.sp, fontWeight = FontWeight.SemiBold,
                modifier = Modifier.padding(start = marge, top = 6.dp),
            )
            Row(Modifier.padding(horizontal = marge), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for ((v, nom) in listOf(0 to t("Abonnés mutuels"), 1 to t("Amis proches"))) {
                    val actif = Notes.audience == v
                    Box(
                        Modifier.clip(formePilule)
                            .background(if (actif) P.accentPlein else P.surface)
                            .border(1.dp, if (actif) P.accentPlein else P.bordCarte, formePilule)
                            .clickable { Notes.choisirAudience(v) }
                            .padding(horizontal = 12.dp, vertical = 6.dp),
                    ) { Text(nom, color = if (actif) P.surAccent else P.texte, fontSize = 13.sp) }
                }
            }
        }
    }
}

/** Une ligne repliable : « titre ▸ » (« ▾ » ouverte). */
@Composable
fun LigneRepliable(titre: String, ouvert: Boolean, marge: Dp = 16.dp, basculer: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = basculer).padding(horizontal = marge, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            titre, color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false),
        )
        Spacer(Modifier.width(6.dp))
        Text(if (ouvert) "▾" else "▸", color = P.textePale, fontSize = 15.sp)
    }
}

/** Une case à cocher (carrée en EGA), avec le logo du service. */
@Composable
private fun CaseCochee(titre: String, detail: String, coche: Boolean, marge: Dp, logo: Service? = null, basculer: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = basculer).padding(horizontal = marge, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val f = if (P.carre) RectangleShape else forme(5.dp)
        Box(
            Modifier.size(20.dp).clip(f).background(if (coche) P.accentPlein else P.surface)
                .border(2.dp, if (coche) P.accentPlein else P.textePale, f),
            contentAlignment = Alignment.Center,
        ) { if (coche) Icon(Icons.Filled.Check, null, tint = P.surAccent, modifier = Modifier.size(15.dp)) }
        Spacer(Modifier.width(14.dp))
        if (logo != null) {
            LogoService(logo, 20.dp)
            Spacer(Modifier.width(8.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(titre, color = P.texteFort, fontSize = 15.sp)
            if (detail.isNotEmpty()) Text(detail, color = P.textePale, fontSize = 12.sp)
        }
    }
}

/**
 * Réglages → Quoi de neuf : les notes des contacts, le partage de Spotify
 * (sur cet appareil, l'accès aux notifications, le format) et le sélecteur
 * « Publier sur ».
 */
@Composable
fun SectionQuoiDeNeuf() {
    val ctx = LocalContext.current
    Groupe {
        Row(
            Modifier.fillMaxWidth().clickable { Marques.choisirNotesContacts(!Marques.notesContacts) }
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(t("Afficher les notes de mes contacts"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                Text(t("Les notes Instagram, en petite bulle au-dessus des avatars du Head."), color = P.textePale, fontSize = 13.sp)
            }
            Spacer(Modifier.width(12.dp))
            Interrupteur(Marques.notesContacts) { Marques.choisirNotesContacts(it) }
        }
    }
    val ici = Musique.actifIci()
    val ailleurs = Marques.notesMusique.isNotEmpty() && !ici
    Groupe {
        Row(
            Modifier.fillMaxWidth().clickable { Musique.activer(!ici) }.padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(t("Partager ce que j'écoute sur Spotify"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                Text(
                    when {
                        ailleurs -> "${t("Activé sur")} ${Marques.notesMusique}. ${t("L'activer ici le reprend.")}"
                        else -> t("Ta note devient ce que tu écoutes, au plus une fois par 15 minutes. Un seul appareil publie.")
                    },
                    color = P.textePale, fontSize = 13.sp,
                )
            }
            Spacer(Modifier.width(12.dp))
            Interrupteur(ici) { Musique.activer(it) }
        }
        if (ici) {
            // Sans l'accès aux notifications, Android ne montre pas la session de Spotify.
            val acces = remember(Musique.branche) { Musique.accesAccorde(ctx) }
            if (!acces) {
                Column(Modifier.padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text(
                        t("Pour savoir ce que joue Spotify, Android demande l'accès aux notifications pour Jasette. Jasette n'y lit aucune notification : seulement le titre et l'artiste de Spotify, sur ce téléphone."),
                        color = P.averti, fontSize = 13.sp,
                    )
                    BoutonAccent(t("Autoriser l'accès")) { Musique.ouvrirAcces(ctx) }
                }
            }
            for ((v, nom, detail) in listOf(
                Triple("artiste", t("Artiste ou émission"), t("« 🎵 Écoute Daft Punk »")),
                Triple("genre", t("Genre"), t("Le genre est trouvé sur MusicBrainz à partir du nom de l'artiste.")),
                Triple("titre", t("Titre de la chanson"), t("« 🎵 Titre — Artiste »")),
            )) {
                val actif = Marques.notesMusiqueFormat == v
                Row(
                    Modifier.fillMaxWidth().clickable { Marques.choisirNotesMusiqueFormat(v) }
                        .padding(horizontal = 16.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    val f = if (P.carre) RectangleShape else formeRonde
                    Box(
                        Modifier.size(20.dp).clip(f).border(2.dp, if (actif) P.accent else P.textePale, f),
                        contentAlignment = Alignment.Center,
                    ) { if (actif) Box(Modifier.size(10.dp).clip(f).background(P.accent)) }
                    Spacer(Modifier.width(14.dp))
                    Column(Modifier.weight(1f)) {
                        Text(nom, color = P.texteFort, fontSize = 15.sp)
                        Text(detail, color = P.textePale, fontSize = 12.sp)
                    }
                }
            }
        }
    }
    Groupe { SelecteurServicesNotes() }
}

/** La note d'un contact au-dessus de son avatar dans le Head : petite bulle, texte tronqué. */
@Composable
fun BulleNoteHead(texte: String, largeur: Dp, police: TextUnit, modifier: Modifier = Modifier) {
    Box(
        modifier.widthIn(max = largeur).clip(forme(10.dp)).background(P.carte)
            .border(1.dp, P.bordCarte, forme(10.dp)).padding(horizontal = 6.dp, vertical = 2.dp),
    ) {
        Text(
            texte, color = P.texteFort, fontSize = police, maxLines = 2, overflow = TextOverflow.Ellipsis,
            textAlign = TextAlign.Center, lineHeight = police * 1.1f,
        )
    }
}
