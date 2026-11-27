package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Les essais partagés avec le bureau (`essais_partages` de src/groupes_auto.rs) :
 * assets/essais/groupes-auto.txt, lu tel quel des deux côtés.
 */
class GroupesAutoTest {
    private fun fichier(): File {
        // Les tests tournent dans android/app ; au cas où, on remonte jusqu'au dépôt.
        var d: File? = File("").absoluteFile
        while (d != null) {
            val f = File(d, "assets/essais/groupes-auto.txt")
            if (f.isFile) return f
            d = d.parentFile
        }
        error("assets/essais/groupes-auto.txt introuvable")
    }

    private fun sorte(c: Char) = when (c) {
        'c' -> SorteMessage.CODE
        'p' -> SorteMessage.PROMO
        'a' -> SorteMessage.AVIS
        'r' -> SorteMessage.RDV
        '$' -> SorteMessage.ARGENT
        else -> SorteMessage.AUTRE
    }

    private fun expediteur(s: String) = when (s) {
        "court" -> SorteExpediteur.COURT
        "alpha" -> SorteExpediteur.ALPHANUMERIQUE
        else -> SorteExpediteur.ORDINAIRE
    }

    @Test
    fun essaisPartages() {
        var n = 0
        for (l in fichier().readLines()) {
            if (l.isBlank() || l.startsWith("#")) continue
            val c = l.split('\t').map { it.replace("\\n", "\n") }
            when (c[0]) {
                "M" -> {
                    val attendu = when (c[1]) {
                        "code" -> SorteMessage.CODE
                        "promo" -> SorteMessage.PROMO
                        "avis" -> SorteMessage.AVIS
                        "rdv" -> SorteMessage.RDV
                        "argent" -> SorteMessage.ARGENT
                        else -> SorteMessage.AUTRE
                    }
                    assertEquals("message : ${c[2]}", attendu, sorteMessage(c[2]))
                }
                "X" -> assertEquals("expéditeur : $l", expediteur(c[1]), sorteExpediteur(c[2], c.getOrElse(3) { "" }))
                "G" -> {
                    val drapeaux = c[4].split(',')
                    val e = EntreeGroupe(
                        sms = c[2] == "sms",
                        expediteur = expediteur(c[3]),
                        exclue = "exclue" in drapeaux,
                        jAiEcrit = "ecrit" in drapeaux,
                        sortes = c.getOrElse(5) { "" }.map(::sorte),
                    )
                    val attendu = lireCorrection(c[1])!!.groupe
                    assertEquals("classement : $l", attendu, classer(e))
                }
                "F" -> {
                    val correction = c[2].takeIf { it != "-" }
                    val detecte = lireCorrection(c[3])!!.groupe
                    assertEquals("final : $l", lireCorrection(c[1])!!.groupe, groupeFinal(correction, detecte))
                }
                else -> error("ligne inconnue : ${c[0]}")
            }
            n++
        }
        assertTrue("trop peu d'essais ($n)", n > 40)
    }

    @Test
    fun codesQuinzeMinutes() {
        assertTrue(codesEnHaut(1_000_000, 1_000_000 + 60_000))
        assertFalse(codesEnHaut(1_000_000, 1_000_000 + CODES_EN_HAUT_MS))
        assertFalse(codesEnHaut(0, 5))
        assertFalse(codesEnHaut(2_000, 1_000))
    }
}
