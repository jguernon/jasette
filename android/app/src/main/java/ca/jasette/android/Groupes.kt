package ca.jasette.android

/*
 * L'onglet Groupes (serveur/PROTOCOLE.md §5.13) : la règle de la boîte de
 * réception pour les grands espaces (`large`), et le rangement de l'écran
 * Groupes. Rien ici ne touche à l'état de l'app : des fonctions pures,
 * testées par GroupesTest (JVM).
 */

/**
 * La règle de la boîte de réception (§5.13, point 2). Une conversation sans
 * `large` y va toujours (et jamais dans Groupes). Une `large` y va aussi si
 * elle est montrée exprès (marque `groupes_inbox`, [montree]), épinglée, ou si
 * elle demande mon attention : `mentions` > 0, ou une mention ou une réponse
 * reçue depuis la dernière lecture ([attention], gardée en mémoire).
 */
fun dansBoiteDeReception(c: Conv, montree: Boolean, epinglee: Boolean, attention: Boolean): Boolean =
    !c.large || montree || epinglee || attention || c.mentions > 0

/** Un message reçu qui demande mon attention : il me mentionne ou me répond. */
fun demandeAttention(m: Msg): Boolean = !m.fromMe && (m.mentionsMe || m.replyToMe)

/**
 * Faut-il notifier [m] ? (§5.13, point 3) Une conversation `large` hors de la
 * boîte de réception ([dansBoite] faux) ne notifie que sur une mention ou une
 * réponse à moi ; les autres, comme avant.
 */
fun notifierMessage(c: Conv?, m: Msg, dansBoite: Boolean): Boolean =
    c == null || !c.large || dansBoite || demandeAttention(m)

/** Les sortes de grands espaces connues (`kind`, §5.13). Une autre valeur : icône générique. */
object Sorte {
    const val SERVEUR = "server"
    const val CANAL = "channel"
    const val SUPERGROUPE = "supergroup"
    const val COMMUNAUTE = "community"
    const val GROUPE_COMMUNAUTE = "community_group"
    const val CHAINE = "newsletter"
    const val DISCUSSION_COMMUNAUTE = "community_chat"
    const val DIFFUSION = "broadcast"
}

/** Un espace parent (serveur Discord) et ses salons. */
data class EspaceParent(
    val service: Service,
    /** L'id du parent, sans préfixe (`g<guild>`). */
    val parent: String,
    val nom: String,
    val salons: List<Conv>,
) {
    /** L'id préfixé du parent, comme celui d'une conversation (pour un avatar à initiales). */
    val id get() = service.prefixe + parent
    /** Les salons non lus. */
    val nonLus get() = salons.count { it.unread }
    /** Les mentions non lues, tous salons confondus. */
    val mentions get() = salons.sumOf { it.mentions }
    val temps get() = salons.maxOfOrNull { it.time } ?: 0L
}

/** Une ligne de la section d'un service dans l'écran Groupes. */
sealed interface LigneGroupes {
    /** Un sous-titre (texte français, traduit à l'affichage : « Chaînes », « Canaux »…). */
    data class Titre(val texte: String) : LigneGroupes
    /** Un en-tête sans traduction : le nom d'une communauté absente de la liste. */
    data class Entete(val nom: String) : LigneGroupes
    /** Un espace (salon, canal, groupe) ; [retrait] : rattaché à la communauté au-dessus. */
    data class Espace(val c: Conv, val retrait: Boolean = false) : LigneGroupes
    /** Un serveur Discord : le toucher ouvre ses salons. */
    data class Serveur(val e: EspaceParent) : LigneGroupes
}

private fun parNom(a: String, b: String) = plier(a).compareTo(plier(b))

/** Les plus récentes d'abord. */
private fun recentes(l: List<Conv>) = l.sortedByDescending { it.time }

/**
 * Les serveurs Discord (`parent` des salons `large`), par nom. Le nom vient
 * de `parent_name` (le premier non vide), sinon l'id.
 */
fun serveurs(service: Service, convs: List<Conv>): List<EspaceParent> =
    convs.filter { it.large && it.parent.isNotEmpty() }.groupBy { it.parent }.map { (p, salons) ->
        EspaceParent(service, p, salons.firstOrNull { it.parentName.isNotEmpty() }?.parentName ?: p, salons)
    }.sortedWith { a, b -> parNom(a.nom, b.nom).let { if (it != 0) it else a.parent.compareTo(b.parent) } }

/**
 * Les salons d'un serveur, par catégorie (§5.13) : les salons sans catégorie
 * en tête, puis les catégories par `section_order` (puis nom) ; dans chacune,
 * par `order`, puis nom. Rend (catégorie, salons) ; "" pour « sans catégorie ».
 */
fun rangerSalons(salons: List<Conv>): List<Pair<String, List<Conv>>> {
    val parSalon = Comparator<Conv> { a, b ->
        a.order.compareTo(b.order).let { if (it != 0) it else parNom(a.name, b.name) }
    }
    val groupes = salons.groupBy { it.section }
    val sections = groupes.keys.sortedWith { a, b ->
        when {
            a.isEmpty() && b.isEmpty() -> 0
            a.isEmpty() -> -1
            b.isEmpty() -> 1
            else -> {
                val oa = groupes[a]!!.minOf { it.sectionOrder }
                val ob = groupes[b]!!.minOf { it.sectionOrder }
                oa.compareTo(ob).let { if (it != 0) it else parNom(a, b) }
            }
        }
    }
    return sections.map { it to groupes[it]!!.sortedWith(parSalon) }
}

/**
 * Les lignes de la section d'un service (§5.13, « Présentation ») :
 * - Discord : les serveurs ;
 * - WhatsApp : chaque communauté et ses groupes (le groupe d'annonces
 *   d'abord), puis les chaînes ;
 * - Telegram : les canaux, puis les supergroupes ;
 * - Messenger et Instagram : les canaux de diffusion, puis les discussions de
 *   communauté.
 * Le reste (sorte inconnue, ou sans parent) suit, des plus récents aux plus
 * anciens. Seules les conversations `large` de [convs] comptent.
 */
fun lignesService(service: Service, convs: List<Conv>): List<LigneGroupes> {
    val grands = convs.filter { it.large && it.service == service }
    if (grands.isEmpty()) return emptyList()
    val lignes = ArrayList<LigneGroupes>()
    val pris = HashSet<String>()
    fun section(titre: String?, l: List<Conv>) {
        if (l.isEmpty()) return
        if (titre != null) lignes.add(LigneGroupes.Titre(titre))
        for (c in l) {
            lignes.add(LigneGroupes.Espace(c))
            pris.add(c.id)
        }
    }
    when (service) {
        Service.DISCORD -> {
            for (e in serveurs(service, grands)) {
                lignes.add(LigneGroupes.Serveur(e))
                e.salons.forEach { pris.add(it.id) }
            }
        }
        Service.WHATSAPP -> {
            val communautes = grands.filter { it.kind == Sorte.COMMUNAUTE }
            val enfants = grands.filter { it.kind != Sorte.COMMUNAUTE && it.parent.isNotEmpty() && it.kind != Sorte.CHAINE }
            // Chaque communauté : la conversation parente si elle est là, sinon son nom.
            val cles = LinkedHashSet<String>()
            for (c in recentes(communautes)) cles.add(c.id.removePrefix(service.prefixe))
            for (c in recentes(enfants)) cles.add(c.parent)
            val ordre = cles.sortedWith { a, b ->
                val na = communautes.firstOrNull { it.id == service.prefixe + a }?.name
                    ?: enfants.firstOrNull { it.parent == a }?.parentName ?: a
                val nb = communautes.firstOrNull { it.id == service.prefixe + b }?.name
                    ?: enfants.firstOrNull { it.parent == b }?.parentName ?: b
                parNom(na, nb)
            }
            for (cle in ordre) {
                val parente = communautes.firstOrNull { it.id == service.prefixe + cle }
                val siens = enfants.filter { it.parent == cle }
                if (parente != null) {
                    lignes.add(LigneGroupes.Espace(parente))
                    pris.add(parente.id)
                } else {
                    lignes.add(LigneGroupes.Entete(siens.firstOrNull { it.parentName.isNotEmpty() }?.parentName ?: cle))
                }
                // Le groupe d'annonces d'abord, puis les autres par activité.
                for (c in siens.sortedWith(compareByDescending<Conv> { it.announce }.thenByDescending { it.time })) {
                    lignes.add(LigneGroupes.Espace(c, retrait = true))
                    pris.add(c.id)
                }
            }
            section("Chaînes", recentes(grands.filter { it.kind == Sorte.CHAINE }))
        }
        Service.TELEGRAM -> {
            section("Canaux", recentes(grands.filter { it.kind == Sorte.CANAL }))
            section("Supergroupes", recentes(grands.filter { it.kind == Sorte.SUPERGROUPE }))
        }
        Service.MESSENGER, Service.INSTAGRAM -> {
            section("Canaux de diffusion", recentes(grands.filter { it.kind == Sorte.DIFFUSION }))
            section("Discussions de communauté", recentes(grands.filter { it.kind == Sorte.DISCUSSION_COMMUNAUTE }))
        }
        else -> {}
    }
    val reste = recentes(grands.filter { it.id !in pris })
    // Un titre « Autres » seulement s'il y a déjà des sections au-dessus.
    section(if (lignes.isEmpty()) null else "Autres", reste)
    return lignes
}

/** Le nombre d'espaces non lus d'un service (la section de Groupes). */
fun espacesNonLus(service: Service, convs: List<Conv>): Int = convs.count { it.large && it.service == service && it.unread }
