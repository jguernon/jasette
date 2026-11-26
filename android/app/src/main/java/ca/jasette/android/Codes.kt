package ca.jasette.android

// Les codes de vérification (2FA, connexion, confirmation) des messages
// reçus : « Votre code est 482913 », « G-123456 is your Google verification
// code », « Code : 12-34-56 », « Your code is K7Q2M9 ». Le fil montre sous la
// bulle un petit bouton qui copie le code seul (sans tirets).
//
// La même logique, pas à pas, que src/codes.rs (bureau) : toute modification
// se fait des deux côtés, avec les mêmes essais (CodesTest.kt).
//
// Il faut un mot-clé (code, vérification, OTP, PIN, NIP, 2FA, confirm…) et un
// seul code plausible : 4 à 8 chiffres (seuls, en groupes égaux « 12-34-56 »,
// « 123 456 », ou derrière un préfixe « G- »), sinon un code de 4 à 8 lettres
// majuscules et chiffres mêlés. Les numéros de téléphone, montants, dates,
// heures, années et adresses sont écartés ; dans le doute, rien.

/** Au-delà, ce n'est plus un texto de code (une infolettre qui parle de « code »). */
private const val LONGUEUR_MAX = 400

/** Les expressions où « code » ne veut pas dire un code à usage unique. */
private val FAUX_CODES = listOf(
    "code postal", "postal code", "zip code", "code promo", "promo code", "code de réduction",
    "discount code", "coupon code", "code coupon",
)

/** Les mots-clés : (racine, mot entier ?). Une racine commence un mot ; un mot entier finit aussi là. */
private val MOTS_CLES = listOf(
    "code" to false, "passcode" to false, "verif" to false, "vérif" to false, "otp" to true,
    "pin" to true, "nip" to true, "2fa" to true, "confirm" to false, "authenti" to false,
    "one-time" to false, "one time" to false, "usage unique" to false, "mot de passe temporaire" to false,
    "validation" to false,
)

/** Juste après un nombre : un montant, une durée, une distance, une adresse. */
private val MOTS_APRES = setOf(
    "$", "€", "£", "%", "dollar", "dollars", "usd", "cad", "eur", "euros", "km", "kg", "lb", "lbs", "points", "pts",
    "min", "minutes", "sec", "secondes", "seconds", "heures", "hours", "jours", "days", "rue", "avenue", "av", "ave",
    "boul", "boulevard", "chemin", "ch", "st", "street", "road", "blvd",
)

/** Juste avant un nombre (un ou deux mots plus tôt) : un compte, une carte, une commande, un suivi. */
private val MOTS_AVANT = setOf(
    "ending", "ends", "terminant", "finissant", "commande", "order", "compte", "account", "carte", "card", "facture",
    "invoice", "suivi", "tracking", "colis", "vol", "flight", "chambre",
)

private data class CandidatCode(val valeur: String, val debut: Int, val fin: Int)

private fun lettreOuChiffre(c: Char) = c.isLetterOrDigit()
private fun chiffreAscii(c: Char) = c in '0'..'9'
private fun majusculeAscii(c: Char) = c in 'A'..'Z'
private fun alnumAscii(c: Char) = chiffreAscii(c) || c in 'a'..'z' || majusculeAscii(c)

private fun motsCles(t: CharArray): List<Pair<Int, Int>> {
    val v = mutableListOf<Pair<Int, Int>>()
    for ((racine, entier) in MOTS_CLES) {
        val r = racine.toCharArray()
        if (r.size > t.size) continue
        for (i in 0..t.size - r.size) {
            if (!(r.indices.all { t[i + it] == r[it] })) continue
            if (i > 0 && lettreOuChiffre(t[i - 1])) continue
            val fin = i + r.size
            if (entier && fin < t.size && lettreOuChiffre(t[fin])) continue
            v += i to fin
        }
    }
    return v
}

private fun motApres(t: CharArray, depart: Int): String {
    var i = depart
    while (i < t.size && t[i] == ' ') i++
    if (i < t.size && !lettreOuChiffre(t[i])) return t[i].toString()
    val s = StringBuilder()
    while (i < t.size && t[i].isLetter()) s.append(t[i++])
    return s.toString()
}

private fun motsAvant(t: CharArray, i: Int): List<String> {
    val mots = mutableListOf<String>()
    var j = i
    repeat(2) {
        while (j > 0 && !t[j - 1].isLetter()) j--
        val fin = j
        while (j > 0 && t[j - 1].isLetter()) j--
        if (j == fin) return mots
        mots += String(t, j, fin - j)
    }
    return mots
}

private fun nombreIsole(t: CharArray, debut: Int, fin: Int): Boolean {
    fun chiffre(i: Int) = i < t.size && chiffreAscii(t[i])
    if (debut > 0) {
        val a = t[debut - 1]
        if (lettreOuChiffre(a) || a in "+($€£¥") return false
        if (a in ".,:/" && debut >= 2 && chiffre(debut - 2)) return false
    }
    if (fin < t.size) {
        val p = t[fin]
        if (lettreOuChiffre(p) || p in "$€£¥%)") return false
        if (p in ".,:/" && chiffre(fin + 1)) return false
    }
    if (motApres(t, fin) in MOTS_APRES) return false
    if (motsAvant(t, debut).any { it in MOTS_AVANT }) return false
    var j = debut
    while (j > 0 && t[j - 1] == ' ') j--
    if (j > 0 && t[j - 1] in "$€£") return false
    return true
}

private fun candidatsChiffres(t: CharArray, original: CharArray): List<CandidatCode> {
    val v = mutableListOf<CandidatCode>()
    var i = 0
    while (i < t.size) {
        if (!chiffreAscii(t[i]) || (i > 0 && chiffreAscii(t[i - 1]))) { i++; continue }
        val debut = i
        val groupes = mutableListOf<Pair<Int, Int>>()
        var j = i
        while (true) {
            val g = j
            while (j < t.size && chiffreAscii(t[j])) j++
            groupes += g to j
            if (j + 1 < t.size && (t[j] == '-' || t[j] == ' ') && chiffreAscii(t[j + 1])) j++ else break
        }
        val fin = j
        i = fin
        // « G-123456 », « FB-12345 » : une ou deux majuscules, un tiret, les chiffres.
        if (groupes.size == 1 && debut >= 2 && t[debut - 1] == '-') {
            var k = debut - 1
            while (k > 0 && majusculeAscii(original[k - 1])) k--
            val n = debut - 1 - k
            if (n in 1..2 && (k == 0 || !lettreOuChiffre(t[k - 1]))) {
                if (fin - debut in 4..8 && (fin >= t.size || !lettreOuChiffre(t[fin]))) {
                    v += CandidatCode(String(t, debut, fin - debut), k, fin)
                }
                continue
            }
        }
        val chiffres = groupes.joinToString("") { (a, b) -> String(t, a, b - a) }
        val n = chiffres.length
        if (groupes.size > 1) {
            val l0 = groupes[0].second - groupes[0].first
            val egaux = groupes.all { (a, b) -> b - a == l0 }
            if (egaux && l0 in 2..4 && groupes.size <= 4 && n in 4..8 && nombreIsole(t, debut, fin)) {
                v += CandidatCode(chiffres, debut, fin)
            }
            // Sinon : un téléphone, une date (2025-03-14) ; dans le doute, rien.
            continue
        }
        if (n !in 4..8 || !nombreIsole(t, debut, fin)) continue
        // Une année.
        if (n == 4 && chiffres.toInt() in 1900..2099) continue
        v += CandidatCode(chiffres, debut, fin)
    }
    return v
}

private fun candidatsAlphanumeriques(original: CharArray): List<CandidatCode> {
    val v = mutableListOf<CandidatCode>()
    var i = 0
    while (i < original.size) {
        if (!alnumAscii(original[i]) || (i > 0 && lettreOuChiffre(original[i - 1]))) { i++; continue }
        val debut = i
        while (i < original.size && alnumAscii(original[i])) i++
        // Collé à une autre lettre (accentuée) : un mot, pas un code.
        if (i < original.size && lettreOuChiffre(original[i])) continue
        val mot = String(original, debut, i - debut)
        val chiffres = mot.count { chiffreAscii(it) }
        val lettres = mot.length - chiffres
        if (mot.length in 4..8 && chiffres > 0 && lettres > 0 && mot.none { it in 'a'..'z' }) {
            v += CandidatCode(mot, debut, i)
        }
    }
    return v
}

/** Le code de vérification d'un message reçu, à copier (sans tirets) ; null s'il n'y en a pas, ou dans le doute. */
fun codeVerification(texte: String): String? {
    val original = texte.toCharArray()
    if (original.isEmpty() || texte.codePointCount(0, texte.length) > LONGUEUR_MAX) return null
    // En minuscules, caractère pour caractère (les positions restent les mêmes).
    val t = CharArray(original.size) { original[it].lowercaseChar() }
    // Les « code postal », « code promo »… ne comptent pas.
    for (f in FAUX_CODES) {
        var i = 0
        while (i + f.length <= t.size) {
            if (f.indices.all { t[i + it] == f[it] }) {
                for (k in i until i + f.length) t[k] = '_'
                i += f.length
            } else {
                i++
            }
        }
    }
    val cles = motsCles(t)
    if (cles.isEmpty()) return null
    var candidats = candidatsChiffres(t, original)
    if (candidats.isEmpty()) {
        // Le mot-clé lui-même (« 2FA ») n'est pas un code.
        candidats = candidatsAlphanumeriques(original).filter { c -> cles.none { (a, b) -> a < c.fin && c.debut < b } }
    }
    return choisir(candidats, cles)
}

/** Un seul code : lui. Plusieurs : le plus proche d'un mot-clé, s'il n'y en a qu'un à cette distance. */
private fun choisir(candidats: List<CandidatCode>, cles: List<Pair<Int, Int>>): String? {
    val premier = candidats.firstOrNull() ?: return null
    if (candidats.all { it.valeur == premier.valeur }) return premier.valeur
    fun distance(c: CandidatCode) = cles.minOf { (a, b) -> if (c.debut >= b) c.debut - b else if (a >= c.fin) a - c.fin else 0 }
    val min = candidats.minOf { distance(it) }
    val proches = candidats.filter { distance(it) == min }
    return if (proches.all { it.valeur == proches[0].valeur }) proches[0].valeur else null
}
