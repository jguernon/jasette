package ca.jasette.android

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

/**
 * Le regroupement automatique de la boîte de réception, côté app : les
 * réglages (Réglages > « Regrouper automatiquement », cinq cases décochées
 * par défaut, PAR APPAREIL comme « Dans la boîte de réception »), ce que la
 * détection (GroupesAuto.kt) sait de chaque conversation, et la boîte
 * réorganisée en rangées ([repartir]).
 *
 * Promotions (case cochée) : aucune notification, pas dans les non-lus ni
 * dans Android Auto. Codes, Rendez-vous, Argent et Avis gardent leurs
 * notifications.
 */
object RegroupementAuto {
    private const val PREFS = "affichage"

    var codes by mutableStateOf(false)
        private set
    var promos by mutableStateOf(false)
        private set
    var rdv by mutableStateOf(false)
        private set
    var argent by mutableStateOf(false)
        private set
    var avis by mutableStateOf(false)
        private set

    fun charger(ctx: Context) {
        val p = ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        codes = p.getBoolean("groupe_codes", false)
        promos = p.getBoolean("groupe_promos", false)
        avis = p.getBoolean("groupe_avis", false)
        // Rendez-vous et Argent sont sortis d'Avis (2026-11) : jamais choisis
        // sur cet appareil, ils suivent la case Avis.
        rdv = p.getBoolean("groupe_rdv", avis)
        argent = p.getBoolean("groupe_argent", avis)
    }

    fun actif(g: GroupeAuto): Boolean = when (g) {
        GroupeAuto.CODES -> codes
        GroupeAuto.PROMOS -> promos
        GroupeAuto.RDV -> rdv
        GroupeAuto.ARGENT -> argent
        GroupeAuto.AVIS -> avis
    }

    /** Au moins une case cochée. */
    val unActif: Boolean get() = codes || promos || rdv || argent || avis

    fun choisir(ctx: Context, g: GroupeAuto, oui: Boolean) {
        when (g) {
            GroupeAuto.CODES -> codes = oui
            GroupeAuto.PROMOS -> promos = oui
            GroupeAuto.RDV -> rdv = oui
            GroupeAuto.ARGENT -> argent = oui
            GroupeAuto.AVIS -> avis = oui
        }
        ctx.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putBoolean("groupe_${g.cle}", oui).apply()
    }

    /** La sorte de chaque texte déjà vu (la détection des codes n'est pas gratuite). */
    private val sortesVues = object : LinkedHashMap<String, SorteMessage>(256, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, SorteMessage>?) = size > 4000
    }

    fun sorte(texte: String): SorteMessage = synchronized(sortesVues) {
        sortesVues.getOrPut(texte) { sorteMessage(texte) }
    }

    /**
     * Un contact : relié à une fiche, nommé à la main, dans les contacts du
     * téléphone, ou (Google) un nom à lettres sur un vrai numéro — Google
     * Messages ne montre un nom que pour un contact du téléphone.
     */
    fun contact(c: Conv): Boolean {
        if (contactPerso(c) != null || Marques.personnes.any { c.id in it.convs }) return true
        if (c.service != Service.GOOGLE) return false
        if (c.number.isNotEmpty() && Contacts.connu(c.number)) return true
        return Contacts.cleDe(c.number).isNotEmpty() && c.name.any { it.isLetter() }
    }

    /** Jamais classée : contact, favori, Head, épinglée, groupe, grand espace, privée. */
    fun exclue(c: Conv): Boolean =
        c.group || c.large || Marques.estFavori(c.id) || Marques.estDansHead(c.id) || Marques.estEpinglee(c.id) ||
            Marques.estPrivee(c.id) || contact(c)

    /** Ce que la détection sait de [c] : ses messages reçus connus, sinon son aperçu. */
    fun entree(c: Conv, msgs: List<Msg>? = Depot.messages[c.id]): EntreeGroupe {
        val recus = msgs.orEmpty().filter { !it.fromMe && it.text.isNotBlank() }.takeLast(MESSAGES_GROUPE_MAX)
        val sortes = if (recus.isNotEmpty()) recus.map { sorte(it.text) }
        else if (!c.fromMe && c.preview.isNotBlank()) listOf(sorte(c.preview)) else emptyList()
        return EntreeGroupe(
            sms = c.service == Service.GOOGLE,
            expediteur = sorteExpediteur(c.name, c.number),
            exclue = exclue(c),
            jAiEcrit = c.fromMe || msgs.orEmpty().any { it.fromMe },
            sortes = sortes,
        )
    }

    /** Jamais classée, même corrigée à la main : groupe, grand espace, favori, Head, épinglée, privée. */
    fun horsClassement(c: Conv): Boolean =
        c.group || c.large || Marques.estFavori(c.id) || Marques.estDansHead(c.id) || Marques.estEpinglee(c.id) ||
            Marques.estPrivee(c.id)

    /**
     * Son groupe, case cochée ou non : la correction à la main, sinon la
     * détection (qui laisse aussi de côté les contacts et les conversations où
     * j'ai écrit). Sert aussi à l'avatar (la liste, les notifications, Android
     * Auto) : un expéditeur automatique montre l'icône de son groupe même sans
     * regroupement.
     */
    fun groupe(c: Conv): GroupeAuto? =
        if (horsClassement(c)) null else groupeFinal(Marques.groupeAuto(c.id), classer(entree(c)))

    /** Son groupe si sa case est cochée (sinon null : elle reste dans la boîte). */
    fun groupeActif(c: Conv): GroupeAuto? {
        if (!unActif) return null
        return groupe(c)?.takeIf { actif(it) }
    }

    /** Une promotion regroupée : ni notification, ni non-lu, ni Android Auto. */
    fun promo(c: Conv?): Boolean = c != null && promos && groupeActif(c) == GroupeAuto.PROMOS
}

/** Une ligne de la boîte de réception : une conversation, ou la rangée d'un groupe. */
sealed interface LigneBoite {
    data class Conversation(val conv: Conv) : LigneBoite
    data class Rangee(val groupe: GroupeAuto, val membres: List<Conv>) : LigneBoite
}

/** Le dernier code reçu d'un groupe : (conversation, code), la plus récente qui en a un. */
fun dernierCode(membres: List<Conv>): Pair<Conv, String>? =
    membres.sortedByDescending { it.time }.firstNotNullOfOrNull { c ->
        if (c.fromMe) null else codeVerification(c.preview)?.let { c to it }
    }

/**
 * La boîte réorganisée : les [nbEpingles] premières (épinglées) restent en
 * tête ; les conversations d'un groupe ([groupeDe] non null) deviennent une
 * seule rangée, placée à l'heure de son membre le plus récent ; la rangée
 * Codes monte juste sous les épinglées pendant 15 minutes après un code.
 */
fun repartir(visibles: List<Conv>, nbEpingles: Int, maintenant: Long, groupeDe: (Conv) -> GroupeAuto?): List<LigneBoite> {
    val tete = visibles.take(nbEpingles).map { LigneBoite.Conversation(it) }
    val reste = visibles.drop(nbEpingles)
    val membres = LinkedHashMap<GroupeAuto, MutableList<Conv>>()
    val seules = ArrayList<Pair<Long, LigneBoite>>()
    for (c in reste) {
        val g = groupeDe(c)
        if (g == null) seules += c.time to LigneBoite.Conversation(c) else membres.getOrPut(g) { ArrayList() } += c
    }
    for ((g, l) in membres) {
        val code = if (g == GroupeAuto.CODES) dernierCode(l)?.first?.time ?: 0L else 0L
        val cle = if (g == GroupeAuto.CODES && codesEnHaut(code, maintenant)) Long.MAX_VALUE else l.maxOf { it.time }
        seules += cle to LigneBoite.Rangee(g, l.sortedByDescending { it.time })
    }
    return tete + seules.sortedByDescending { it.first }.map { it.second }
}
