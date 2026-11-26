package ca.jasette.android

/*
 * L'onglet Groupes (serveur/PROTOCOLE.md §5.13) : la règle de la boîte de
 * réception pour les grands espaces (`large`), et le rangement de l'écran
 * Groupes. Rien ici ne touche à l'état de l'app : des fonctions pures,
 * testées par GroupesTest (JVM).
 */

/**
 * La règle de la boîte de réception (§5.13) : « la boîte de réception, c'est
 * le chat ». Une conversation sans `large` y va toujours (et jamais dans
 * Groupes) ; une `large` n'y va jamais, même avec une mention, une réponse à
 * moi ou une épingle : ses mentions se voient dans l'onglet Groupes.
 */
fun dansBoiteDeReception(c: Conv): Boolean = !c.large

/** Un message reçu qui demande mon attention : il me mentionne ou me répond. */
fun demandeAttention(m: Msg): Boolean = !m.fromMe && (m.mentionsMe || m.replyToMe)

/**
 * Faut-il notifier [m] ? (§5.13, point 4) Une conversation `large` ne notifie
 * que sur une mention ou une réponse à moi ; les autres, comme avant.
 */
fun notifierMessage(c: Conv?, m: Msg): Boolean =
    c == null || !c.large || demandeAttention(m)

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

/**
 * Un espace parent (§5.13, règle 3) : un serveur Discord, une communauté
 * WhatsApp, un Groupe Facebook… de n'importe quel service, et ses salons.
 */
data class EspaceParent(
    val service: Service,
    /** L'id du parent, sans préfixe (`g<guild>`, `4101@g.us`). */
    val parent: String,
    /** Son nom : le premier `parent_name` non vide, sinon celui de la conversation parente ; "" si inconnu (voir [generique]). */
    val nom: String,
    /** Les salons (conversations `large` dont c'est le `parent`), sans la conversation parente. */
    val salons: List<Conv>,
    /** La conversation parente, si elle est elle-même un grand espace de la liste (communauté WhatsApp) : en tête. */
    val tete: Conv? = null,
) {
    /** L'id préfixé du parent, comme celui d'une conversation (son icône : `avatars[id]`). */
    val id get() = service.prefixe + parent
    /** Le parent et ses salons, chacun une fois. */
    val tous: List<Conv> get() = listOfNotNull(tete) + salons
    /** Le nom à montrer quand [nom] est vide (texte français, traduit à l'affichage). */
    val generique get() = if (tous.any { it.kind == Sorte.SERVEUR }) "Serveur" else "Communauté"
    /** Les salons non lus. */
    val nonLus get() = tous.count { it.unread }
    /** Les mentions non lues, comptées une fois par salon. */
    val mentions get() = tous.sumOf { it.mentions }
    val temps get() = tous.maxOfOrNull { it.time } ?: 0L
}

/** Une ligne de la section d'un service dans l'écran Groupes. */
sealed interface LigneGroupes {
    val temps: Long
    /** Un espace sans parent (canal, supergroupe, chaîne…). */
    data class Espace(val c: Conv) : LigneGroupes {
        override val temps get() = c.time
    }
    /** Un parent (serveur, communauté) : le toucher ouvre ses salons. */
    data class Parent(val e: EspaceParent) : LigneGroupes {
        override val temps get() = e.temps
    }
}

private fun parNom(a: String, b: String) = plier(a).compareTo(plier(b))

/**
 * Les parents des grands espaces de [service] (§5.13, règle 3) : les
 * conversations `large` du même `parent` réunies ; la conversation parente,
 * si elle est dans la liste et sans parent elle-même, en [EspaceParent.tete].
 */
fun parents(service: Service, convs: List<Conv>): List<EspaceParent> {
    val grands = convs.filter { it.large && it.service == service }.distinctBy { it.id }
    val enfants = grands.filter { it.parent.isNotEmpty() }.groupBy { it.parent }
    return enfants.map { (p, salons) ->
        val tete = grands.firstOrNull { it.id == service.prefixe + p && it.parent.isEmpty() }
        val nom = salons.firstOrNull { it.parentName.isNotEmpty() }?.parentName
            ?: tete?.name?.takeIf { it.isNotEmpty() }
            ?: grands.firstOrNull { it.id == service.prefixe + p }?.name
            ?: ""
        EspaceParent(service, p, nom, salons.filter { it.id != tete?.id }, tete)
    }
}

/** Les parents de tous les services. */
fun tousLesParents(convs: List<Conv>): List<EspaceParent> =
    convs.filter { it.large }.map { it.service }.distinct().flatMap { parents(it, convs) }

/** L'espace parent d'id préfixé [id] (« d:g81 », « w:4101@g.us »), ou null. */
fun parentDe(id: String, convs: List<Conv>): EspaceParent? =
    parents(Service.de(id), convs).firstOrNull { it.id == id }

/**
 * L'espace parent (id préfixé) à ouvrir pour montrer [c] : celui de son
 * `parent`, ou le sien s'il est lui-même parent d'autres espaces ; null sinon.
 */
fun espaceDe(c: Conv, convs: List<Conv>): String? {
    if (!c.large) return null
    if (c.parent.isNotEmpty()) return c.service.prefixe + c.parent
    val sansPrefixe = c.id.removePrefix(c.service.prefixe)
    return if (convs.any { it.large && it.service == c.service && it.parent == sansPrefixe }) c.id else null
}

/**
 * Les salons d'un parent, par catégorie (§5.13) : les salons sans catégorie
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
 * Les lignes de la section d'un service (§5.13, règles génériques, aucune
 * propre à un service) : une ligne par parent (ses salons et la conversation
 * parente dessous), une ligne par espace sans parent, ensemble, des plus
 * récentes aux plus anciennes. Seules les conversations `large` comptent.
 */
fun lignesService(service: Service, convs: List<Conv>): List<LigneGroupes> {
    val grands = convs.filter { it.large && it.service == service }.distinctBy { it.id }
    if (grands.isEmpty()) return emptyList()
    val ps = parents(service, grands)
    val pris = HashSet<String>()
    for (p in ps) p.tous.forEach { pris.add(it.id) }
    val lignes = ps.map { LigneGroupes.Parent(it) } + grands.filter { it.id !in pris }.map { LigneGroupes.Espace(it) }
    return lignes.sortedByDescending { it.temps }
}

/** Le nombre d'espaces non lus d'un service (la section de Groupes). */
fun espacesNonLus(service: Service, convs: List<Conv>): Int = convs.count { it.large && it.service == service && it.unread }

/**
 * « Tout marquer comme lu » (§5.13) : les grands espaces de la section de
 * [service] (ou, avec [parent], id préfixé, ceux de ce parent et la
 * conversation parente) qui ont des non-lus ou des mentions, hors privées.
 */
fun aMarquerLus(
    service: Service,
    convs: List<Conv>,
    parent: String? = null,
    privee: (String) -> Boolean = { false },
): List<Conv> = convs.filter { c ->
    c.large && c.service == service && (c.unread || c.mentions > 0) && !privee(c.id) &&
        (parent == null || c.id == parent || (c.parent.isNotEmpty() && service.prefixe + c.parent == parent))
}.distinctBy { it.id }
