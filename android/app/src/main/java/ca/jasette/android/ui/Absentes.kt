package ca.jasette.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.ColorMatrix
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ca.jasette.android.CacheMarques
import ca.jasette.android.Conv
import ca.jasette.android.Depot
import ca.jasette.android.Etat
import ca.jasette.android.Marques
import ca.jasette.android.Service
import ca.jasette.android.t

/**
 * Les conversations marquées (Head, favoris, épingles) dont la conversation
 * n'est pas chargée dans [Depot] : on les montre quand même, depuis
 * [CacheMarques], pour qu'une marque ne disparaisse jamais de l'écran parce
 * que son service est coupé ou pas encore prêt.
 */
enum class Absence {
    /** Le service est prêt : la conversation est simplement hors de la liste chargée (plus ancienne). */
    PRETE,
    /** Le service est en train de se connecter. */
    CHARGEMENT,
    /** Le service est déconnecté, arrêté, ou n'est plus offert par le serveur. */
    DECONNECTE,
}

/** Pourquoi la conversation [id] n'est pas chargée, ou null si elle l'est. */
fun absence(id: String): Absence? {
    if (Depot.conv(id) != null) return null
    val s = Service.de(id)
    if (s !in Depot.services) return Absence.DECONNECTE
    return when (Depot.etat(s)) {
        Etat.PRET -> Absence.PRETE
        Etat.DEMARRAGE, Etat.CONNEXION, Etat.RECONNEXION -> Absence.CHARGEMENT
        else -> Absence.DECONNECTE
    }
}

/** Grisé : le service n'est pas là (une conversation plus ancienne d'un service prêt ne l'est pas). */
fun Absence?.grise() = this == Absence.CHARGEMENT || this == Absence.DECONNECTE

/** La petite indication sous une conversation absente, ou null. */
fun Absence?.indication(): String? = when (this) {
    Absence.CHARGEMENT -> t("chargement…")
    Absence.DECONNECTE -> t("déconnecté")
    else -> null
}

/** Un id marqué est-il dans le Head, les favoris ou les épingles ? */
fun estMarquee(id: String) = Marques.estDansHead(id) || Marques.estFavori(id) || Marques.estEpinglee(id)

/**
 * La conversation à montrer pour un id marqué : la vraie si elle est chargée,
 * sinon sa fiche du cache, sinon (jamais vue) une conversation sans nom, que
 * l'avatar générique et le logo du service (déduit du préfixe de l'id) montrent.
 */
fun convMarquee(id: String): Conv = Depot.conv(id) ?: CacheMarques.conv(id) ?: Conv(
    id = id, name = "", preview = "", fromMe = false, time = 0, unread = false, group = false, rcs = false,
    color = "", members = emptyList(), readOnly = false, archived = false, e2ee = false, number = "",
)

/** Les épinglées absentes de la liste chargée (ajoutées en haut de la boîte de réception). */
fun epinglesAbsentes(): List<Conv> = Marques.epingles.filter { Depot.conv(it) == null }.map { convMarquee(it) }

/** Le filtre qui grise une photo (saturation nulle, un peu pâlie). */
val filtreGris: ColorFilter by lazy {
    ColorFilter.colorMatrix(ColorMatrix().apply { setToSaturation(0f) })
}

/**
 * Le fil d'une conversation absente, sans message en cache : « <Service>
 * n'est pas connecté » (ou se connecte), et un bouton vers Réglages → Comptes.
 */
@Composable
fun FilHorsLigne(service: Service, absence: Absence?, reglages: () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        LogoService(service, 56.dp)
        Spacer(Modifier.size(16.dp))
        Text(
            if (absence == Absence.CHARGEMENT) "${service.court} ${t("se connecte…")}"
            else "${service.court} ${t("n'est pas connecté")}",
            color = P.texteFort, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, textAlign = TextAlign.Center,
        )
        Spacer(Modifier.size(6.dp))
        Text(
            t("Les messages de cette conversation s'afficheront dès que le service sera connecté."),
            color = P.textePale, fontSize = 14.sp, textAlign = TextAlign.Center,
        )
        Spacer(Modifier.size(20.dp))
        BoutonAccent(t("Réglages → Comptes"), reglages)
    }
}
