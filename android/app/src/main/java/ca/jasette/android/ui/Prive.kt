package ca.jasette.android.ui

import android.app.Activity
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import ca.jasette.android.Marques
import ca.jasette.android.Prive
import ca.jasette.android.t
import kotlinx.coroutines.delay

/**
 * Le pavé du code à 6 chiffres : six points, les touches 0 à 9 et l'effacement.
 * [verifie] : le code est comparé au code posé (l'attente après 5 erreurs s'applique).
 * [code] est appelé avec les 6 chiffres ; il rend un message d'erreur, ou null.
 */
@Composable
fun PaveCode(
    titre: String,
    sousTitre: String,
    verifie: Boolean,
    annuler: (() -> Unit)?,
    messageInitial: String? = null,
    code: (String) -> String?,
) {
    var saisie by remember { mutableStateOf("") }
    var message by remember { mutableStateOf(messageInitial) }
    // Le compte à rebours de l'attente, une fois par seconde.
    var attente by remember { mutableLongStateOf(if (verifie) Prive.attente() else 0L) }
    LaunchedEffect(verifie) {
        while (verifie) {
            attente = Prive.attente()
            delay(1000)
        }
    }
    fun chiffre(d: Char) {
        if (verifie && attente > 0) return
        if (saisie.length < Prive.CHIFFRES) saisie += d
        if (saisie.length == Prive.CHIFFRES) {
            val s = saisie
            saisie = ""
            message = code(s)
            if (verifie) attente = Prive.attente()
        }
    }
    Column(
        Modifier.fillMaxSize().background(P.ecran).padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(Icons.Filled.Lock, null, tint = P.accent, modifier = Modifier.size(36.dp))
        Spacer(Modifier.height(12.dp))
        Text(titre, color = P.texteFort, fontSize = 20.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(6.dp))
        Text(sousTitre, color = P.textePale, fontSize = 14.sp, textAlign = TextAlign.Center)
        Spacer(Modifier.height(20.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            for (i in 0 until Prive.CHIFFRES) {
                val plein = i < saisie.length
                Box(
                    Modifier.size(14.dp).clip(CircleShape)
                        .border(2.dp, P.accent, CircleShape)
                        .background(if (plein) P.accent else androidx.compose.ui.graphics.Color.Transparent),
                )
            }
        }
        Spacer(Modifier.height(12.dp))
        val texte = if (verifie && attente > 0) {
            t("Trop d'essais : attends") + " $attente s"
        } else {
            message ?: ""
        }
        Text(texte, color = P.danger, fontSize = 13.sp, modifier = Modifier.height(20.dp))
        Spacer(Modifier.height(12.dp))
        val rangees = listOf("123", "456", "789", " 0<")
        for (r in rangees) {
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp), modifier = Modifier.padding(vertical = 6.dp)) {
                for (ch in r) {
                    Box(
                        Modifier.size(72.dp, 56.dp).clip(forme(16.dp))
                            .then(if (ch == ' ') Modifier else Modifier.background(P.menu).clickable {
                                if (ch == '<') saisie = saisie.dropLast(1) else chiffre(ch)
                            }),
                        contentAlignment = Alignment.Center,
                    ) {
                        if (ch != ' ') {
                            Text(
                                if (ch == '<') "⌫" else ch.toString(),
                                color = P.texteFort, fontSize = 22.sp, fontWeight = FontWeight.SemiBold,
                            )
                        }
                    }
                }
            }
        }
        if (annuler != null) {
            Spacer(Modifier.height(10.dp))
            TextButton(onClick = annuler, shape = formeBouton) { Text(t("Annuler"), color = P.texte) }
        }
    }
}

/**
 * L'écran de [ca.jasette.android.PriveActivity] (le lanceur au cadenas) : le
 * pavé, puis la vue Privées. Se reverrouille après 1 minute sans toucher l'écran.
 */
@Composable
fun EcranPrive(activite: Activity) {
    if (!Prive.actif) {
        Column(
            Modifier.fillMaxSize().background(P.ecran).padding(32.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Text(t("La Liste privée n'est pas activée."), color = P.texteFort, textAlign = TextAlign.Center)
            TextButton(onClick = { activite.finish() }, shape = formeBouton) { Text(t("Fermer"), color = P.accent) }
        }
        return
    }
    if (!Prive.deverrouille) {
        PaveCode(
            titre = t("Conversations privées"),
            sousTitre = t("Entre ton code"),
            verifie = true,
            annuler = { activite.finish() },
        ) { c -> if (Prive.verifier(c)) { Prive.deverrouiller(); null } else t("Code incorrect") }
        return
    }
    LaunchedEffect(Unit) {
        while (Prive.deverrouille) {
            delay(1000)
            if (Prive.expiree()) Prive.verrouiller()
        }
    }
    App(activite, prive = true)
}

/** Où en est le réglage de la Liste privée. */
private enum class EtapeReglage { NOUVEAU, CONFIRMER, ANCIEN, CHANGER, CHANGER_CONFIRMER, DESACTIVER, QUE_FAIRE, CONFIRMER_REMETTRE }

/**
 * Réglages → Liste privée : désactivée par défaut ; l'activer demande un code
 * (deux fois) ; changer le code demande l'ancien ; la désactiver, le code, puis
 * (s'il reste des privées) quoi en faire.
 */
@Composable
fun ReglagesPrive() {
    val ctx = LocalContext.current
    var etape by remember { mutableStateOf<EtapeReglage?>(null) }
    var premier by remember { mutableStateOf("") }
    var erreur by remember { mutableStateOf<String?>(null) }
    val n = Marques.privees.size
    Groupe {
        Row(
            Modifier.fillMaxWidth().clickable {
                etape = if (Prive.actif) EtapeReglage.DESACTIVER else EtapeReglage.NOUVEAU
            }.padding(horizontal = 16.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(t("Liste privée"), color = P.texteFort, fontWeight = FontWeight.SemiBold, fontSize = 15.sp)
                Text(
                    t("Des conversations cachées partout dans Jasette, qu'on rouvre par l'icône « Jasette privé » et un code à 6 chiffres. Le code reste sur cet appareil."),
                    color = P.textePale, fontSize = 13.sp,
                )
            }
            Spacer(Modifier.width(12.dp))
            Interrupteur(Prive.actif) { etape = if (Prive.actif) EtapeReglage.DESACTIVER else EtapeReglage.NOUVEAU }
        }
        if (Prive.actif) {
            HorizontalDivider(color = P.filet)
            ChoixRangee(
                icone = Icons.Filled.Refresh,
                titre = t("Changer le code"),
                detail = "$n " + t("conversation(s) privée(s)"),
                actif = false,
                chevron = true,
            ) { etape = EtapeReglage.ANCIEN }
        }
    }
    Text(
        if (!Prive.actif && n > 0) {
            "$n " + t("conversation(s) rendue(s) privée(s) sur un autre appareil reste(nt) cachée(s) ici. Active la Liste privée pour les voir.")
        } else {
            t("« Rendre privée » apparaît dans le menu d'une conversation. Une conversation privée ne donne aucune notification. La vue se reverrouille en arrière-plan, ou après 1 minute sans toucher l'écran. La liste des privées est partagée par le serveur Jasette ; le code, jamais.")
        },
        color = P.textePale, fontSize = 12.sp,
    )

    val e = etape ?: return
    Dialog(
        onDismissRequest = { etape = null },
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Box(Modifier.fillMaxSize().background(P.ecran)) {
            when (e) {
                EtapeReglage.NOUVEAU, EtapeReglage.CHANGER -> PaveCode(
                    if (e == EtapeReglage.NOUVEAU) t("Liste privée") else t("Changer le code"),
                    t("Choisis un code à 6 chiffres"), verifie = false, annuler = { etape = null }, messageInitial = erreur,
                ) { c ->
                    erreur = null
                    premier = c
                    etape = if (e == EtapeReglage.NOUVEAU) EtapeReglage.CONFIRMER else EtapeReglage.CHANGER_CONFIRMER
                    null
                }
                EtapeReglage.CONFIRMER, EtapeReglage.CHANGER_CONFIRMER -> PaveCode(
                    t("Liste privée"), t("Entre le code une 2e fois"), verifie = false, annuler = { etape = null },
                ) { c ->
                    if (c != premier) {
                        etape = if (e == EtapeReglage.CONFIRMER) EtapeReglage.NOUVEAU else EtapeReglage.CHANGER
                        erreur = t("Les deux codes diffèrent : recommence.")
                        null
                    } else {
                        Prive.poserCode(ctx, c)
                        premier = ""
                        etape = null
                        null
                    }
                }
                EtapeReglage.ANCIEN -> PaveCode(
                    t("Changer le code"), t("Entre le code actuel"), verifie = true, annuler = { etape = null },
                ) { c -> if (Prive.verifier(c)) { etape = EtapeReglage.CHANGER; null } else t("Code incorrect") }
                EtapeReglage.DESACTIVER -> PaveCode(
                    t("Désactiver la liste"), t("Entre ton code"), verifie = true, annuler = { etape = null },
                ) { c ->
                    if (!Prive.verifier(c)) {
                        t("Code incorrect")
                    } else {
                        if (Marques.privees.isEmpty()) {
                            Prive.desactiver(ctx)
                            etape = null
                        } else {
                            etape = EtapeReglage.QUE_FAIRE
                        }
                        null
                    }
                }
                EtapeReglage.QUE_FAIRE, EtapeReglage.CONFIRMER_REMETTRE -> Column(
                    Modifier.fillMaxSize().padding(28.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(14.dp, Alignment.CenterVertically),
                ) {
                    Icon(Icons.Filled.Lock, null, tint = P.accent, modifier = Modifier.size(36.dp))
                    Text(t("Désactiver la liste"), color = P.texteFort, fontSize = 20.sp, fontWeight = FontWeight.Bold)
                    Text(
                        t("Il reste des conversations privées :") + " $n. " + t("Qu'en fait-on ?"),
                        color = P.texte, textAlign = TextAlign.Center,
                    )
                    if (e == EtapeReglage.QUE_FAIRE) {
                        BoutonAccent(t("Les remettre dans l'app (conseillé)")) { etape = EtapeReglage.CONFIRMER_REMETTRE }
                        TextButton(onClick = {
                            Prive.desactiver(ctx)
                            etape = null
                        }, shape = formeBouton) { Text(t("Les garder cachées"), color = P.danger) }
                        Text(
                            t("Cachées, elles ne se verront plus avant de réactiver la Liste privée."),
                            color = P.textePale, fontSize = 12.sp, textAlign = TextAlign.Center,
                        )
                    } else {
                        BoutonAccent(t("Confirmer : elles redeviennent normales")) {
                            Marques.remettrePrivees()
                            Prive.desactiver(ctx)
                            etape = null
                        }
                    }
                    TextButton(onClick = { etape = null }, shape = formeBouton) { Text(t("Annuler"), color = P.texte) }
                }
            }
        }
    }
}
