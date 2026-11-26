package ca.jasette.android

import org.json.JSONObject

/*
 * Le nom et la photo choisis à la main pour une personne (crayon de la fiche,
 * ui/FichePersonne.kt) : la marque `contacts_perso` (serveur/PROTOCOLE.md
 * §5.5, « Règle des contacts perso »), comme src/personne.rs du bureau.
 * Sans serveur, elle reste sur ce téléphone, comme les autres marques.
 */

/**
 * Le nom (vide : celui du service) et la photo (vide : celle du service ; un
 * chemin du serveur en mode client, sinon un fichier de ce téléphone) d'une
 * personne ; [google] : enregistrée aussi dans les Contacts Google.
 */
data class ContactPerso(val nom: String = "", val photo: String = "", val google: Boolean = false)

object ContactsPerso {
    /** Comme le serveur : ids ≤ 512 octets ; nom ≤ 256 octets sans saut de ligne ; chemin ≤ 1024 ; 5000 au plus. */
    fun nettoyer(m: Map<String, ContactPerso>): Map<String, ContactPerso> {
        val r = LinkedHashMap<String, ContactPerso>()
        for ((id, c) in m.toSortedMap()) {
            if (id.isEmpty() || id.toByteArray().size > 512) continue
            var nom = c.nom.trim()
            if (nom.toByteArray().size > 256 || nom.any { it == '\u0000' || it == '\n' || it == '\r' }) nom = ""
            val photo = if (c.photo.toByteArray().size > 1024 || '\u0000' in c.photo) "" else c.photo
            if (nom.isEmpty() && photo.isEmpty()) continue
            if (r.size >= 5000) break
            r[id] = ContactPerso(nom, photo, c.google)
        }
        return r
    }

    /** `contacts_perso` d'une trame, avec indulgence (une entrée mal formée est sautée). */
    fun lire(o: JSONObject?): Map<String, ContactPerso> {
        if (o == null) return emptyMap()
        val r = HashMap<String, ContactPerso>()
        for (k in o.keys()) {
            val e = o.opt(k) as? JSONObject ?: continue
            r[k] = ContactPerso(e.opt("nom") as? String ?: "", e.opt("photo") as? String ?: "", e.opt("google") as? Boolean ?: false)
        }
        return nettoyer(r)
    }

    fun json(m: Map<String, ContactPerso>): JSONObject = JSONObject().also { o ->
        for ((id, c) in m) {
            val e = JSONObject()
            if (c.nom.isNotEmpty()) e.put("nom", c.nom)
            if (c.photo.isNotEmpty()) e.put("photo", c.photo)
            if (c.google) e.put("google", true)
            o.put(id, e)
        }
    }

    /**
     * L'entrée qui vaut pour [id] : la sienne, sinon celle de la principale
     * de sa personne, sinon celle d'une autre de ses conversations. Rend sa clé.
     */
    fun de(perso: Map<String, ContactPerso>, personnes: List<Personne>, id: String): Pair<String, ContactPerso>? {
        perso[id]?.let { return id to it }
        val p = personnes.firstOrNull { id in it.convs } ?: return null
        for (x in listOf(p.principal).filter { it.isNotEmpty() } + p.convs) perso[x]?.let { return x to it }
        return null
    }

    /**
     * La clé où enregistrer pour la fiche de [groupe] (l'ancre d'abord) :
     * celle de l'entrée qui vaut déjà, sinon la principale, sinon l'ancre.
     */
    fun cle(perso: Map<String, ContactPerso>, personnes: List<Personne>, groupe: List<String>): String {
        val ancre = groupe.firstOrNull() ?: return ""
        groupe.firstNotNullOfOrNull { de(perso, personnes, it) }?.let { return it.first }
        return personnes.firstOrNull { ancre in it.convs }?.principal?.takeIf { it.isNotEmpty() } ?: ancre
    }

    /**
     * Les conversations de la fiche (même numéro compris) reliées à la main
     * dans une personne, créée au besoin, sans toucher à sa principale. Une
     * conversation déjà à une autre personne y reste.
     */
    fun relierFiche(personnes: List<Personne>, groupe: List<String>, idNeuf: String): List<Personne> {
        val ancre = groupe.firstOrNull() ?: return personnes
        if (groupe.size < 2) return personnes
        val libres = groupe.filter { g -> personnes.none { g in it.convs } }
        val p = personnes.toMutableList()
        val i = p.indexOfFirst { ancre in it.convs }.takeIf { it >= 0 } ?: p.indexOfFirst { it.convs.any { c -> c in groupe } }
        if (i >= 0) p[i] = p[i].copy(convs = p[i].convs + libres) else p.add(Personne(idNeuf, libres))
        return Personnes.nettoyer(p)
    }

    /**
     * Enregistre [nom] (vide : celui du service) sous [cle] ; la photo reste
     * celle de l'entrée, sauf [photo] donnée. Sans nom ni photo, l'entrée part.
     */
    fun poser(perso: Map<String, ContactPerso>, cle: String, nom: String, photo: String?, google: Boolean): Map<String, ContactPerso> {
        val e = perso[cle] ?: ContactPerso()
        val m = perso.toMutableMap()
        m[cle] = ContactPerso(nom.trim(), photo ?: e.photo, google)
        return nettoyer(m)
    }

    /** « Rétablir » : plus d'entrée pour aucune conversation de [groupe]. */
    fun retablir(perso: Map<String, ContactPerso>, personnes: List<Personne>, groupe: List<String>): Map<String, ContactPerso> {
        val m = perso.toMutableMap()
        while (true) {
            val k = groupe.firstNotNullOfOrNull { de(m, personnes, it) }?.first ?: break
            m.remove(k)
        }
        return m
    }

    /** Le carré centré d'une image [l]×[h] : (x, y, côté). */
    fun carreCentre(l: Int, h: Int): Triple<Int, Int, Int> {
        val c = minOf(l, h)
        return Triple((l - c) / 2, (h - c) / 2, c)
    }

    /** Le facteur de sous-échantillonnage (puissance de 2) qui garde au moins [cible] px sur [cote]. */
    fun echantillon(cote: Int, cible: Int = 512): Int {
        var e = 1
        while (cote / (e * 2) >= cible) e *= 2
        return e
    }

    /**
     * La copie locale d'une photo du serveur (posée par [Depot] en mode
     * client) ; null : pas encore là. En mode local, la photo est un fichier.
     */
    @Volatile var resoudre: ((String) -> String?)? = null

    /** Le fichier à montrer pour la photo [chemin] (null s'il n'est pas lisible ici). */
    fun photoLocale(chemin: String): String? {
        if (chemin.isEmpty()) return null
        if (java.io.File(chemin).isFile) return chemin
        return resoudre?.invoke(chemin)
    }
}
