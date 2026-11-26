package ca.jasette.android

/**
 * « Nouveau message » : la puce de service choisie, le mode groupe et les
 * personnes choisies (id préfixé, nom). Comme `ChoixNouveau` du bureau
 * (src/nouveau.rs) : toujours « Tout » à l'ouverture, un seul service par
 * groupe (celui de la première personne choisie), commande `create_group`.
 *
 * [sms] : Google passe par les SMS du téléphone (PontSms), qui ne créent pas
 * de groupe.
 */
data class ChoixNouveau(
    val filtre: Service? = null,
    val groupe: Boolean = false,
    val choisis: List<Pair<String, String>> = emptyList(),
    val sms: Boolean = false,
) {
    /** Le service sait-il créer un groupe ici ? X et Bluesky, non. */
    fun creeGroupes(s: Service) = creeGroupes(s, sms)

    /** Le service du groupe en cours : celui de la première personne choisie. */
    val verrou: Service? get() = choisis.firstOrNull()?.let { Service.de(it.first) }

    /**
     * Une puce (null : « Tout ») peut-elle être touchée ? Une fois une personne
     * choisie, seulement celle de son service.
     */
    fun pucePermise(s: Service?): Boolean {
        val v = verrou
        return v == null || s == v
    }

    /** Choisit une puce ; « Tout » ou un service sans groupes fait sortir du mode groupe. */
    fun avecFiltre(s: Service?): ChoixNouveau {
        if (!pucePermise(s)) return this
        val c = copy(filtre = s)
        return if (c.groupe && !c.groupeOffert) c.copy(groupe = false, choisis = emptyList()) else c
    }

    /** « Créer un groupe » : seulement sur la puce d'un service qui sait créer des groupes (jamais « Tout »). */
    val groupeOffert: Boolean get() = filtre?.let { creeGroupes(it) } ?: false

    /** Entre en mode groupe, ou en sort (les personnes choisies sont oubliées). */
    fun basculerGroupe(): ChoixNouveau = when {
        groupe -> copy(groupe = false, choisis = emptyList())
        groupeOffert -> copy(groupe = true)
        else -> this
    }

    fun estChoisi(id: String) = choisis.any { it.first == id }

    /**
     * Ajoute une personne, ou l'enlève si elle l'était déjà. La première fixe
     * le service du groupe (sa puce est choisie). Rend `this` si elle est
     * refusée (autre service, groupe, service sans groupes).
     */
    fun basculer(id: String, nom: String): ChoixNouveau {
        if (estChoisi(id)) return copy(choisis = choisis.filter { it.first != id })
        val s = Service.de(id)
        if (idGroupe(id) || !creeGroupes(s) || (verrou != null && verrou != s) || choisis.size >= 50) return this
        return copy(choisis = choisis + (id to nom), filtre = s)
    }

    /** Les résultats (et la recherche dans les ponts) de ce service sont-ils montrés ? */
    fun montre(s: Service) = (filtre == null || filtre == s) && (!groupe || creeGroupes(s))

    /** Les membres pour `create_group` : ids sans le préfixe du service. */
    val membres: List<String> get() = choisis.map { (id, _) -> id.removePrefix(Service.de(id).prefixe) }

    /** Le bouton « Créer » : au moins deux personnes. */
    val pret: Boolean get() = groupe && choisis.size >= 2

    companion object {
        /** Les ponts qui savent créer un groupe (commande `create_group`, §5.6 bis du protocole). */
        fun creeGroupes(s: Service, sms: Boolean): Boolean =
            s != Service.X && s != Service.BLUESKY && s != Service.REDDIT && !(s == Service.GOOGLE && sms)

        /** Le service prend-il un nom de groupe à la création ? Instagram, non. */
        fun nommeGroupes(s: Service): Boolean = s != Service.INSTAGRAM

        /**
         * Un résultat de `search` (id préfixé) qui désigne un groupe, pas une
         * personne : il ne peut pas être membre d'un nouveau groupe.
         */
        fun idGroupe(id: String): Boolean {
            val s = Service.de(id)
            val brut = id.removePrefix(s.prefixe)
            return when (s) {
                Service.WHATSAPP -> brut.endsWith("@g.us") || brut.endsWith("@newsletter") || brut.endsWith("@broadcast")
                // Un groupe Signal : son identifiant en base64 (44 caractères), jamais un UUID.
                Service.SIGNAL -> brut.length == 44 && '-' !in brut && !brut.startsWith("+")
                // Telegram : g<n> (groupe simple) ou c<n> (canal, supergroupe).
                Service.TELEGRAM -> brut.startsWith("g") || brut.startsWith("c")
                else -> false
            }
        }
    }
}
