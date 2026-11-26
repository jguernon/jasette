package ca.jasette.android

import org.json.JSONArray
import org.json.JSONObject

/*
 * La fiche de la personne (ui/FichePersonne.kt), comme src/personne.rs du
 * bureau : les mêmes règles de correspondance, faites ici, localement, sans
 * aucune requête aux services (serveur/PROTOCOLE.md §5.5, « Règle des
 * personnes ») :
 * - sûre : le même numéro de téléphone (Google, WhatsApp, Signal, Telegram) ;
 * - suggérée : un nom très proche, ou le nom exact d'un contact du téléphone
 *   qui porte le numéro d'une conversation de la personne.
 * Les liens faits à la main (marque `personnes`) et les refus (marque
 * `personnes_refus`) sont partagés par le serveur Jasette (Marques.kt).
 */

/** Des conversations à deux, reliées à la main, qui sont la même personne. */
data class Personne(val id: String, val convs: List<String>, val principal: String = "")

/** Une paire « Pas la même personne », rangée (le plus petit id d'abord). */
typealias Refus = Pair<String, String>

object Personnes {
    /**
     * Comme le serveur : ids de personne uniques (64 octets au plus), chaque
     * conversation à une seule personne, 32 conversations au plus, au moins
     * deux, `principal` parmi elles ; 2000 personnes au plus.
     */
    fun nettoyer(l: List<Personne>): List<Personne> {
        val r = ArrayList<Personne>()
        val vues = HashSet<String>()
        for (p in l) {
            if (p.id.isEmpty() || p.id.toByteArray().size > 64 || r.any { it.id == p.id }) continue
            val convs = ArrayList<String>()
            for (c in p.convs) {
                if (c.isEmpty() || c.toByteArray().size > 512 || c in vues || c in convs) continue
                if (convs.size >= 32) break
                convs.add(c)
            }
            if (convs.size < 2) continue
            vues.addAll(convs)
            r.add(Personne(p.id, convs, if (p.principal in convs) p.principal else ""))
            if (r.size >= 2000) break
        }
        return r
    }

    /** Comme le serveur : paires de deux ids différents, rangées, sans doublon, 5000 au plus. */
    fun nettoyerRefus(l: List<Refus>): List<Refus> {
        val r = ArrayList<Refus>()
        for ((a, b) in l) {
            if (a.isEmpty() || b.isEmpty() || a == b) continue
            val p = ranger(a, b)
            if (p !in r) r.add(p)
            if (r.size >= 5000) break
        }
        return r
    }

    fun ranger(a: String, b: String): Refus = if (b < a) b to a else a to b

    /**
     * Union : les personnes de [dabord] (même id : la sienne, entière), puis
     * celles de [ensuite] qu'il n'a pas, moins les conversations déjà reliées.
     */
    fun unir(dabord: List<Personne>, ensuite: List<Personne>): List<Personne> {
        val prises = dabord.flatMap { it.convs }.toSet()
        val r = dabord.toMutableList()
        for (p in ensuite) {
            if (dabord.any { it.id == p.id }) continue
            r.add(p.copy(convs = p.convs.filter { it !in prises }))
        }
        return nettoyer(r)
    }

    /** `personnes` d'une trame, avec indulgence (un élément mal formé est sauté). */
    fun lire(a: JSONArray?): List<Personne> {
        if (a == null) return emptyList()
        val r = ArrayList<Personne>()
        for (i in 0 until a.length()) {
            val o = a.opt(i) as? JSONObject ?: continue
            val id = o.opt("id") as? String ?: continue
            val convs = o.optJSONArray("convs") ?: continue
            r.add(Personne(id, (0 until convs.length()).mapNotNull { convs.opt(it) as? String }, o.opt("principal") as? String ?: ""))
        }
        return nettoyer(r)
    }

    /** `personnes_refus` d'une trame : des paires `[a, b]`. */
    fun lireRefus(a: JSONArray?): List<Refus> {
        if (a == null) return emptyList()
        val r = ArrayList<Refus>()
        for (i in 0 until a.length()) {
            val p = a.opt(i) as? JSONArray ?: continue
            if (p.length() != 2) continue
            val x = p.opt(0) as? String ?: continue
            val y = p.opt(1) as? String ?: continue
            r.add(x to y)
        }
        return nettoyerRefus(r)
    }

    fun json(l: List<Personne>): JSONArray = JSONArray().also { a ->
        for (p in l) {
            val o = JSONObject().put("id", p.id).put("convs", JSONArray(p.convs))
            if (p.principal.isNotEmpty()) o.put("principal", p.principal)
            a.put(o)
        }
    }

    fun jsonRefus(l: List<Refus>): JSONArray = JSONArray().also { a -> for ((x, y) in l) a.put(JSONArray().put(x).put(y)) }

    /** Un id neuf de personne : « p », l'heure en ms, et un peu de hasard. */
    fun nouvelId(): String = "p${System.currentTimeMillis()}${"%04x".format((Math.random() * 0xffff).toInt())}"
}

// ---- La correspondance ----

/**
 * Un numéro en E.164, à la nord-américaine pour les numéros sans indicatif :
 * `(418) 555-1234`, `418-555-1234`, `1-418-555-1234`, `+1 418 555 1234` →
 * `+14185551234`. Null sous 7 chiffres ou au-delà de 15, ou s'il y a des lettres.
 */
fun e164(s: String): String? {
    val t = s.trim()
    if (t.any { it.isLetter() }) return null
    var chiffres = t.filter { it in '0'..'9' }
    val plus = t.startsWith("+") || t.startsWith("00")
    if (t.startsWith("00")) chiffres = chiffres.drop(2)
    if (chiffres.length < 7 || chiffres.length > 15) return null
    return when {
        plus -> "+$chiffres"
        chiffres.length == 10 -> "+1$chiffres"
        else -> "+$chiffres"
    }
}

/**
 * Le numéro (E.164) d'une conversation à deux, s'il est connu sans rien
 * demander : Google (`number`, sinon le membre ou le nom s'ils sont un
 * numéro), WhatsApp (l'id `<numéro>@s.whatsapp.net`), Signal et Telegram
 * (`number` du pont ; Signal : un id `+…`). Les autres services n'en ont pas.
 */
fun numeroDe(id: String, numero: String, membre: String?, nom: String): String? {
    val svc = Service.de(id)
    val brut = id.removePrefix(svc.prefixe)
    return when (svc) {
        Service.GOOGLE -> e164(numero) ?: membre?.takeIf { estNumero(it) }?.let { e164(it) } ?: nom.takeIf { estNumero(it) }?.let { e164(it) }
        Service.WHATSAPP -> if (brut.endsWith("@s.whatsapp.net")) e164("+" + brut.removeSuffix("@s.whatsapp.net")) else null
        Service.SIGNAL -> e164(numero) ?: brut.takeIf { it.startsWith("+") }?.let { e164(it) }
        Service.TELEGRAM -> e164(numero)
        else -> null
    }
}

/** Les mots d'un nom, pliés (sans accents ni casse), sans ponctuation, rangés. */
fun mots(nom: String): List<String> =
    plier(nom).map { if (it.isLetterOrDigit()) it else ' ' }.joinToString("").split(' ').filter { it.isNotEmpty() }.sorted()

private fun distance(a: String, b: String): Int {
    var ligne = IntArray(b.length + 1) { it }
    for (i in a.indices) {
        val n = IntArray(b.length + 1)
        n[0] = i + 1
        for (j in b.indices) n[j + 1] = if (a[i] == b[j]) ligne[j] else 1 + minOf(ligne[j], ligne[j + 1], n[j])
        ligne = n
    }
    return ligne[b.length]
}

/**
 * Deux noms très proches ? Mêmes mots (accents, casse, ponctuation et ordre
 * ignorés) ; ou, pour deux noms d'au moins deux mots, un mot abrégé (4 lettres
 * au moins) ou une faute de frappe (une lettre par tranche de 8). Un nom de
 * moins de 4 lettres ou un numéro ne se compare pas.
 */
fun nomsProches(a: String, b: String): Boolean {
    if (estNumero(a) || estNumero(b)) return false
    val ma = mots(a)
    val mb = mots(b)
    if (ma.isEmpty() || mb.isEmpty() || ma.sumOf { it.length } < 4 || mb.sumOf { it.length } < 4) return false
    if (ma == mb) return true
    if (ma.size < 2 || ma.size != mb.size) return false
    val restants = mb.toMutableList()
    var egaux = 0
    var abrege = true
    for (m in ma) {
        val i = restants.indexOf(m)
        if (i >= 0) {
            restants.removeAt(i)
            egaux++
            continue
        }
        val k = restants.indexOfFirst { r -> minOf(r.length, m.length) >= 4 && (r.startsWith(m) || m.startsWith(r)) }
        if (k >= 0) restants.removeAt(k) else { abrege = false; break }
    }
    if (abrege && egaux >= 1) return true
    val ja = ma.joinToString(" ")
    val jb = mb.joinToString(" ")
    val tolere = minOf(ja.length, jb.length) / 8
    return tolere >= 1 && distance(ja, jb) <= tolere
}

/** Une conversation candidate : à deux, pas un grand espace. */
data class Candidat(val id: String, val nom: String, val numero: String?)

/** Pourquoi une conversation est sur la fiche. */
enum class LienFiche { ANCRE, MAIN, NUMERO }

/** Pourquoi une conversation est suggérée. */
enum class Raison { CONTACT, NOM }

/** La personne autour d'une conversation : ses conversations (l'ancre d'abord) et les suggestions. */
data class Groupe(val membres: List<Pair<String, LienFiche>>, val suggestions: List<Pair<String, Raison>>) {
    val ids get() = membres.map { it.first }
}

private fun refuse(refus: List<Refus>, a: String, b: String) = refus.any { (x, y) -> (x == a && y == b) || (x == b && y == a) }

/**
 * La personne de [ancre], parmi [cands] : les liens à la main d'abord, puis
 * le même numéro, de proche en proche ; une conversation refusée avec une des
 * conversations déjà prises n'entre pas. Puis les suggestions (6 au plus).
 * [contacts] : (nom, numéro E.164) des contacts du téléphone.
 */
fun regrouper(ancre: String, cands: List<Candidat>, personnes: List<Personne>, refus: List<Refus>, contacts: List<Pair<String, String>>): Groupe {
    val parId = cands.associateBy { it.id }
    if (ancre !in parId) return Groupe(emptyList(), emptyList())
    val membres = mutableListOf(ancre to LienFiche.ANCRE)
    var i = 0
    while (i < membres.size) {
        val x = membres[i].first
        i++
        val voisins = ArrayList<Pair<String, LienFiche>>()
        for (p in personnes) if (x in p.convs) voisins.addAll(p.convs.filter { it != x }.map { it to LienFiche.MAIN })
        val n = parId[x]?.numero
        if (n != null) voisins.addAll(cands.filter { it.id != x && it.numero == n }.map { it.id to LienFiche.NUMERO })
        for ((y, lien) in voisins) {
            if (y !in parId || membres.any { it.first == y } || membres.any { refuse(refus, it.first, y) }) continue
            membres.add(y to lien)
        }
    }
    val numeros = membres.mapNotNull { parId[it.first]?.numero }.toSet()
    val noms = membres.mapNotNull { parId[it.first]?.nom }
    val nomsContacts = contacts.filter { it.second in numeros }.map { mots(it.first) }.filter { it.isNotEmpty() }
    val sugg = ArrayList<Pair<String, Raison>>()
    for (c in cands) {
        if (membres.any { it.first == c.id } || membres.any { refuse(refus, it.first, c.id) } || personnes.any { c.id in it.convs }) continue
        val m = mots(c.nom)
        if (!estNumero(c.nom) && m.isNotEmpty() && m in nomsContacts) sugg.add(c.id to Raison.CONTACT)
        else if (noms.any { nomsProches(it, c.nom) }) sugg.add(c.id to Raison.NOM)
    }
    return Groupe(membres, sugg.sortedBy { it.second == Raison.NOM }.take(6))
}

/**
 * « Lier » (ou « Oui » sur une suggestion) : [x] rejoint la personne de la
 * fiche ([groupe] : ses conversations, l'ancre d'abord). Retirée de toute
 * autre personne ; la personne est créée au besoin ; les refus entre [x] et
 * la fiche sont effacés.
 */
fun lier(personnes: List<Personne>, refus: List<Refus>, groupe: List<String>, x: String, idNeuf: String): Pair<List<Personne>, List<Refus>> {
    val ancre = groupe.firstOrNull() ?: return personnes to refus
    if (x in groupe) return personnes to refus
    val p = personnes.map { it.copy(convs = it.convs - x) }.toMutableList()
    val i = p.indexOfFirst { ancre in it.convs }.takeIf { it >= 0 } ?: p.indexOfFirst { it.convs.any { c -> c in groupe } }
    if (i >= 0) p[i] = p[i].copy(convs = p[i].convs + x) else p.add(Personne(idNeuf, listOf(ancre, x)))
    val r = refus.filterNot { (a, b) -> (a == x && b in groupe) || (b == x && a in groupe) }
    return Personnes.nettoyer(p) to r
}

/** « Délier » [x] : retirée de sa personne, et un refus entre elle et chaque autre conversation de la fiche. */
fun delier(personnes: List<Personne>, refus: List<Refus>, groupe: List<String>, x: String): Pair<List<Personne>, List<Refus>> =
    Personnes.nettoyer(personnes.map { it.copy(convs = it.convs - x) }) to refuser(refus, groupe, x)

/** « Pas la même personne » : un refus entre [x] et chaque conversation de la fiche. */
fun refuser(refus: List<Refus>, groupe: List<String>, x: String): List<Refus> =
    Personnes.nettoyerRefus(refus + groupe.filter { it != x }.map { Personnes.ranger(it, x) })

/**
 * La conversation principale de la personne de [id], si un principal est choisi
 * (et toujours lié) : son nom et sa photo valent alors pour toutes.
 */
fun principalDe(personnes: List<Personne>, id: String): String? {
    val p = personnes.firstOrNull { id in it.convs } ?: return null
    return p.principal.takeIf { it.isNotEmpty() && it != id && it in p.convs }
}

/** La conversation principale de la personne de [conv] (jamais pour un groupe). */
fun principaleDe(conv: Conv): Conv? {
    if (conv.group || conv.large) return null
    return principalDe(Marques.personnes, conv.id)?.let { Depot.conv(it) }?.takeIf { !it.group }
}

/**
 * Le nom montré partout pour [conv] : celui de la principale de sa personne,
 * sinon le sien (vide s'il n'en a pas). La recherche trouve aussi le nom d'origine.
 */
fun nomAffiche(conv: Conv): String = principaleDe(conv)?.name?.trim()?.takeIf { it.isNotEmpty() } ?: conv.name

/** « Utiliser ce nom et cette photo » : [x] devient le nom principal de la personne (créée au besoin). */
fun choisirPrincipal(personnes: List<Personne>, groupe: List<String>, x: String, idNeuf: String): List<Personne> {
    val ancre = groupe.firstOrNull() ?: return personnes
    val p = personnes.toMutableList()
    var i = p.indexOfFirst { ancre in it.convs }.takeIf { it >= 0 } ?: p.indexOfFirst { it.convs.any { c -> c in groupe } }
    if (i < 0) {
        p.add(Personne(idNeuf, groupe.filter { g -> personnes.none { g in it.convs } }))
        i = p.size - 1
    }
    val convs = if (x in p[i].convs) p[i].convs else p[i].convs + x
    p[i] = p[i].copy(convs = convs, principal = x)
    return Personnes.nettoyer(p)
}
