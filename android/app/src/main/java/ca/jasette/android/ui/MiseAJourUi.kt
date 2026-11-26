package ca.jasette.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.Langue
import ca.jasette.android.MiseAJour
import ca.jasette.android.VersionDispo
import ca.jasette.android.t

/** « 54 Mo » (ou « 54 MB ») ; vide si la taille est inconnue. */
private fun taille(octets: Long): String {
    if (octets <= 0) return ""
    val mo = (octets + 512 * 1024) / (1024 * 1024)
    return if (Langue.anglais) "$mo MB" else "$mo Mo"
}

/** Le retour des réglages d'Android (« installer des apps inconnues ») relance l'installation. */
@Composable
private fun RetourAutorisation() {
    val ctx = LocalContext.current
    androidx.lifecycle.compose.LifecycleEventEffect(androidx.lifecycle.Lifecycle.Event.ON_RESUME) {
        MiseAJour.auRetour(ctx)
    }
}

/** La carte « Jasette X est disponible », en haut de la liste (seulement en release). */
@Composable
fun CarteMiseAJour() {
    if (!MiseAJour.carteVisible) return
    val d = MiseAJour.disponible ?: return
    RetourAutorisation()
    Column(
        Modifier.fillMaxWidth().padding(top = 4.dp).carte(14.dp).padding(horizontal = 14.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        ContenuMiseAJour(d, plusTard = true)
    }
}

/** Le titre, les notes, l'état (progression, erreur) et les boutons. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ContenuMiseAJour(d: VersionDispo, plusTard: Boolean) {
    val ctx = LocalContext.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        androidx.compose.foundation.layout.Box(
            Modifier.size(8.dp).clip(formeRonde).background(P.accent),
        )
        Spacer(Modifier.width(12.dp))
        Text(
            t("Jasette %s est disponible").replace("%s", d.versionName),
            color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
        )
    }
    val notes = d.notes(Langue.anglais)
    if (notes.isNotBlank()) {
        Text(notes, color = P.texte, fontSize = 13.sp, maxLines = 6, overflow = TextOverflow.Ellipsis)
    }
    when (val e = MiseAJour.etat) {
        is MiseAJour.Etat.Telechargement -> {
            Text(
                t("Téléchargement…") + if (e.fraction >= 0f) " ${(e.fraction * 100).toInt()} %" else "",
                color = P.textePale, fontSize = 13.sp,
            )
            if (e.fraction >= 0f) {
                LinearProgressIndicator(
                    progress = { e.fraction }, modifier = Modifier.fillMaxWidth(),
                    color = P.accent, trackColor = P.filet,
                )
            } else {
                LinearProgressIndicator(Modifier.fillMaxWidth(), color = P.accent, trackColor = P.filet)
            }
        }
        MiseAJour.Etat.Verification ->
            Text(t("Vérification de l'empreinte SHA-256 et de la signature…"), color = P.textePale, fontSize = 13.sp)
        MiseAJour.Etat.Installation ->
            Text(t("Android demande de confirmer l'installation."), color = P.textePale, fontSize = 13.sp)
        MiseAJour.Etat.Permission -> {
            Text(
                t("Pour installer la mise à jour, Android doit permettre à Jasette d'installer des apps. Active « Autoriser cette source » pour Jasette, puis reviens ici."),
                color = P.texteFort, fontSize = 13.sp,
            )
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                BoutonAccent(t("Ouvrir les réglages")) { MiseAJour.ouvrirAutorisation(ctx) }
            }
            return
        }
        is MiseAJour.Etat.Erreur -> Text(e.message, color = P.danger, fontSize = 13.sp)
        else -> {}
    }
    val occupe = MiseAJour.etat.let {
        it is MiseAJour.Etat.Telechargement || it == MiseAJour.Etat.Verification || it == MiseAJour.Etat.Installation
    }
    if (!occupe) {
        val detail = listOfNotNull(
            taille(d.taille).ifEmpty { null },
            if (MiseAJour.reseauMobile(ctx)) t("réseau mobile") else null,
        ).joinToString(" · ")
        if (detail.isNotEmpty()) Text(detail, color = P.textePale, fontSize = 12.sp)
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            BoutonAccent(t("Mettre à jour")) { MiseAJour.mettreAJour(ctx) }
            if (plusTard) {
                TextButton(shape = formeBouton, onClick = { MiseAJour.plusTard() }) { Text(t("Plus tard"), color = P.texte) }
            }
        }
    }
}

/** Réglages → À propos : la version installée et « Chercher une mise à jour ». */
@Composable
fun AProposMiseAJour() {
    val ctx = LocalContext.current
    RetourAutorisation()
    Column(
        Modifier.fillMaxWidth().carte(16.dp).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            "Jasette",
            color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 16.sp,
        )
        Text(
            "${t("Créé par")}${t(" : ")}Jonathan Guernon",
            color = P.texte, fontSize = 13.sp,
        )
        Text(
            "${t("Version installée")}${t(" : ")}${MiseAJour.nomInstalle}",
            color = P.textePale, fontSize = 13.sp,
        )
        if (MiseAJour.debug) {
            Text(
                t("Version de développement (debug, installée par adb) : elle ne se met pas à jour elle-même."),
                color = P.texte, fontSize = 13.sp,
            )
        }
        val d = MiseAJour.disponible
        val e = MiseAJour.etat
        when {
            d != null && MiseAJour.debug -> Text(
                t("La version release %s est publiée sur jonathanguernon.net/jasette. Pour l'utiliser : désinstalle cette version de développement, puis installe l'APK du site. Les mises à jour suivantes se feront dans l'app.")
                    .replace("%s", d.versionName),
                color = P.texteFort, fontSize = 13.sp,
            )
            d != null -> ContenuMiseAJour(d, plusTard = false)
            e == MiseAJour.Etat.Recherche -> Text(t("Recherche…"), color = P.textePale, fontSize = 13.sp)
            e == MiseAJour.Etat.AJour -> Text(t("Jasette est à jour."), color = P.succes, fontSize = 13.sp)
            e is MiseAJour.Etat.Erreur -> Text(e.message, color = P.danger, fontSize = 13.sp)
        }
        val occupe = e is MiseAJour.Etat.Telechargement || e == MiseAJour.Etat.Verification ||
            e == MiseAJour.Etat.Installation || e == MiseAJour.Etat.Recherche
        if (d == null || MiseAJour.debug) {
            OutlinedButton(
                shape = formeBouton, enabled = !occupe,
                onClick = { MiseAJour.verifier(ctx, manuel = true) },
            ) { Text(t("Chercher une mise à jour"), color = P.texteFort) }
        }
    }
}
