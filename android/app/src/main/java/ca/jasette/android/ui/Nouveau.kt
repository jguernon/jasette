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
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.ChoixNouveau
import ca.jasette.android.Service
import ca.jasette.android.t

/*
 * « Nouveau message » (NouveauMessage dans App.kt) : les puces de service,
 * « Créer un groupe », les personnes choisies et « Créer ». Comme
 * `entete_nouveau` du bureau (src/nouveau.rs).
 */

/** Une puce : choisie (accent), permise, ou désactivée (pâle ; pas de transparence en EGA). */
@Composable
private fun Puce(actif: Boolean, permise: Boolean, onClick: () -> Unit, contenu: @Composable RowScope.(Color) -> Unit) {
    val couleur = when {
        actif -> P.accent
        permise -> P.texteFort
        else -> P.textePale
    }
    Row(
        Modifier.height(34.dp).clip(formePilule)
            .background(if (actif) P.choisi else P.champ)
            .border(1.dp, if (actif) P.accent else P.traitFin, formePilule)
            .let { if (permise) it.clickable(onClick = onClick) else it }
            .let { if (!permise && !P.ega) it.alpha(0.45f) else it }
            .padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) { contenu(couleur) }
}

/** Les puces de service : Tout (par défaut), puis chaque service connecté. Défilent à l'horizontale. */
@Composable
fun PucesServices(choix: ChoixNouveau, connectes: List<Service>, choisir: (Service?) -> Unit) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Puce(choix.filtre == null, choix.pucePermise(null), { choisir(null) }) { c ->
            Text(t("Tout"), color = c, fontSize = 14.sp, fontWeight = if (choix.filtre == null) FontWeight.SemiBold else null)
        }
        for (s in connectes) {
            val actif = choix.filtre == s
            Puce(actif, choix.pucePermise(s), { choisir(s) }) { c ->
                Image(painterResource(iconeService(s)), null, Modifier.size(18.dp).clip(formeLogo(18.dp)))
                Text(s.court, color = c, fontSize = 14.sp, fontWeight = if (actif) FontWeight.SemiBold else null, maxLines = 1)
            }
        }
    }
}

/**
 * « Créer un groupe » : bascule le mode groupe. Désactivé, avec « Pas encore
 * offert pour ce service », sur la puce d'un service qui ne sait pas créer de
 * groupe. Dessous, une courte explication quand des puces sont désactivées.
 */
@Composable
fun LigneCreerGroupe(choix: ChoixNouveau, connectes: List<Service>, basculer: () -> Unit) {
    val offert = choix.groupeOffert
    Column {
        Row(
            Modifier.fillMaxWidth().let { if (choix.groupe) it.background(P.choisi) else it }
                .let { if (offert) it.clickable(onClick = basculer) else it }
                .let { if (!offert && !P.ega) it.alpha(0.5f) else it }
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                Modifier.size(42.dp).clip(formeRonde).background(if (choix.groupe) P.accentPlein else P.champ),
                contentAlignment = Alignment.Center,
            ) {
                Icon(painterResource(ca.jasette.android.R.drawable.ic_groupe), null, tint = if (choix.groupe) P.surAccent else P.accent, modifier = Modifier.size(24.dp))
            }
            Spacer(Modifier.width(14.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    t("Créer un groupe"), color = if (offert) P.texteFort else P.textePale,
                    fontWeight = FontWeight.SemiBold, maxLines = 1,
                )
                val sous = when {
                    !offert -> t("Pas encore offert pour ce service")
                    choix.groupe && choix.choisis.isEmpty() -> t("Choisis les personnes du groupe")
                    else -> null
                }
                if (sous != null) Text(sous, color = P.textePale, fontSize = 13.sp, maxLines = 1)
            }
            if (choix.groupe) Icon(Icons.Filled.Close, t("Fermer"), tint = P.textePale, modifier = Modifier.size(20.dp))
        }
        val explication = if (choix.verrou != null) t("Un groupe reste sur un seul service") else null
        if (explication != null) {
            Text(
                explication, color = P.textePale, fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
    }
}

/** Les personnes choisies, en puces sous la recherche ; ✕ pour en enlever une. */
@Composable
fun PucesChoisis(choix: ChoixNouveau, enlever: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 12.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        for ((id, nom) in choix.choisis) {
            Puce(actif = true, permise = true, onClick = { enlever(id) }) { c ->
                Box(
                    Modifier.size(24.dp).clip(formeRonde).background(P.avatarDefaut),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(initialesCourtes(nom), color = Color.White, fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
                }
                Text(nom, color = c, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 150.dp))
                Icon(Icons.Filled.Close, t("Retirer"), tint = c, modifier = Modifier.size(16.dp))
            }
        }
    }
}

private fun initialesCourtes(nom: String): String =
    nom.split(Regex("\\s+")).mapNotNull { m -> m.firstOrNull { it.isLetterOrDigit() } }.take(2).joinToString("").uppercase()

/** Le nom du groupe (si le service le permet) et « Créer · n » (au moins deux personnes). */
@Composable
fun BarreCreer(choix: ChoixNouveau, nom: String, changerNom: (String) -> Unit, creer: () -> Unit) {
    val s = choix.verrou ?: return
    val pret = choix.pret
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (ChoixNouveau.nommeGroupes(s)) {
            Box(
                Modifier.weight(1f).carte(14.dp).padding(horizontal = 14.dp, vertical = 11.dp),
                contentAlignment = Alignment.CenterStart,
            ) {
                if (nom.isEmpty()) Text(t("Nom du groupe (facultatif)"), color = P.textePale, fontSize = 15.sp, maxLines = 1)
                BasicTextField(
                    nom, changerNom, singleLine = true,
                    textStyle = TextStyle(color = P.texteFort, fontSize = 15.sp, fontFamily = P.police),
                    cursorBrush = SolidColor(P.accent),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        } else {
            Text(
                if (pret) "" else t("Choisis au moins deux personnes"), color = P.textePale, fontSize = 13.sp,
                modifier = Modifier.weight(1f),
            )
        }
        Spacer(Modifier.width(10.dp))
        Box(
            Modifier.height(42.dp).clip(formePilule).background(if (pret) P.accentPlein else P.champ)
                .let { if (pret) it.clickable(onClick = creer) else it }
                .padding(horizontal = 18.dp),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                "${t("Créer")} · ${choix.choisis.size}", color = if (pret) P.surAccent else P.textePale,
                fontWeight = FontWeight.SemiBold, fontSize = 15.sp,
            )
        }
    }
}

/** La coche d'une personne en mode groupe : un rond vide, ou plein avec ✓. */
@Composable
fun Coche(oui: Boolean) {
    Box(
        Modifier.size(24.dp).clip(formeRonde)
            .let { if (oui) it.background(P.accentPlein) else it.border(2.dp, P.traitFin, formeRonde) },
        contentAlignment = Alignment.Center,
    ) {
        if (oui) Icon(Icons.Filled.Check, null, tint = P.surAccent, modifier = Modifier.size(16.dp))
    }
}
