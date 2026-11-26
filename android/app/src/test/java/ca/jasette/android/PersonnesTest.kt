package ca.jasette.android

import org.json.JSONArray
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** La fiche de la personne : mêmes cas que src/personne.rs du bureau. */
class PersonnesTest {
    @Test
    fun numerosQuebecois() {
        for (n in listOf("(418) 555-1234", "418-555-1234", "418 555 1234", "+1 418 555 1234", "1-418-555-1234", "+14185551234", "1 (418) 555-1234", "001 418 555 1234")) {
            assertEquals(n, "+14185551234", e164(n))
        }
        assertEquals("+33612345678", e164("+33 6 12 34 56 78"))
        assertNull(e164("555-12"))
        assertNull(e164("Mon Amour"))
        assertNull(e164("32665"))
    }

    @Test
    fun numerosDesServices() {
        assertEquals("+14185551234", numeroDe("123", "(418) 555-1234", null, "Mon Amour"))
        assertEquals("+14185551234", numeroDe("123", "", "418-555-1234", "Mon Amour"))
        assertEquals("+14185551234", numeroDe("w:14185551234@s.whatsapp.net", "", null, "Steph"))
        assertNull(numeroDe("w:123456789012345@lid", "", null, "Steph"))
        assertEquals("+14185551234", numeroDe("s:uuid", "+14185551234", null, "S"))
        assertEquals("+14185551234", numeroDe("t:u42", "+14185551234", null, "T"))
        assertNull(numeroDe("m:99", "+14185551234", null, "M"))
    }

    @Test
    fun noms() {
        assertTrue(nomsProches("Stephanie Legendre", "Stéphanie Legendre"))
        assertTrue(nomsProches("Stephanie Legendre", "LEGENDRE, Stéphanie"))
        assertTrue(nomsProches("Steph Legendre", "Stéphanie Legendre"))
        assertTrue(nomsProches("Stephanie Legendre", "Stephanie Legendree"))
        assertFalse(nomsProches("Stephanie Legendre", "Stephanie Tremblay"))
        assertFalse(nomsProches("Mon Amour", "Stephanie Legendre"))
        assertFalse(nomsProches("Al", "Al"))
        assertFalse(nomsProches("418-555-1234", "418-555-1234"))
        assertFalse(nomsProches("Marc Roy", "Marc Rioux"))
    }

    private val wa = "w:14185551234@s.whatsapp.net"
    private val base = listOf(
        Candidat("123", "Mon Amour", "+14185551234"),
        Candidat(wa, "Steph 💕", "+14185551234"),
        Candidat("m:99", "Stephanie Legendre", null),
        Candidat("i:42", "Stéphanie Legendre", null),
        Candidat("m:7", "Marc Tremblay", null),
    )

    @Test
    fun regrouperEtSuggerer() {
        val g = regrouper("123", base, emptyList(), emptyList(), emptyList())
        assertEquals(listOf("123", wa), g.ids)
        assertEquals(LienFiche.NUMERO, g.membres[1].second)
        assertTrue(g.suggestions.isEmpty())
        val contacts = listOf("Stephanie Legendre" to "+14185551234")
        assertEquals(listOf("m:99" to Raison.CONTACT, "i:42" to Raison.CONTACT), regrouper("123", base, emptyList(), emptyList(), contacts).suggestions)
        assertEquals(listOf("i:42" to Raison.NOM), regrouper("m:99", base, emptyList(), emptyList(), emptyList()).suggestions)
    }

    @Test
    fun lierDelierRefuser() {
        var (p, r) = lier(emptyList(), emptyList(), regrouper("123", base, emptyList(), emptyList(), emptyList()).ids, "m:99", "p1")
        assertEquals(listOf(Personne("p1", listOf("123", "m:99"))), p)
        var g = regrouper("m:99", base, p, r, emptyList())
        assertEquals(listOf("m:99", "123", wa), g.ids)
        assertEquals(listOf("i:42" to Raison.NOM), g.suggestions)
        r = refuser(r, g.ids, "i:42")
        assertEquals(3, r.size)
        assertTrue(regrouper("m:99", base, p, r, emptyList()).suggestions.isEmpty())
        // Délier WhatsApp : le même numéro ne le ramène plus.
        delier(p, r, regrouper("123", base, p, r, emptyList()).ids, wa).let { p = it.first; r = it.second }
        assertEquals(listOf("123", "m:99"), regrouper("123", base, p, r, emptyList()).ids)
        // Le relier à la main efface ses refus.
        lier(p, r, regrouper("123", base, p, r, emptyList()).ids, wa, "p2").let { p = it.first; r = it.second }
        assertEquals(1, p.size)
        assertEquals(3, regrouper(wa, base, p, r, emptyList()).ids.size)
        // Nom principal.
        g = regrouper("123", base, p, r, emptyList())
        p = choisirPrincipal(p, g.ids, "m:99", "p3")
        assertEquals("m:99", p[0].principal)
    }

    @Test
    fun marqueLueEtNettoyee() {
        val a = JSONArray("""[{"id":"p1","convs":["123","m:9","123"],"principal":"m:9"},{"id":4},"x",{"id":"p2","convs":["m:9","i:4"]}]""")
        val l = Personnes.lire(a)
        assertEquals(listOf(Personne("p1", listOf("123", "m:9"), "m:9")), l)
        assertEquals(listOf("123" to "m:9"), Personnes.lireRefus(JSONArray("""[["m:9","123"],["123","m:9"],["a"]]""")))
        // Union : même id, la nôtre ; une conversation à une seule personne.
        val u = Personnes.unir(l, listOf(Personne("p1", listOf("a", "b")), Personne("p3", listOf("m:9", "w:1", "t:2"))))
        assertEquals(listOf(Personne("p1", listOf("123", "m:9"), "m:9"), Personne("p3", listOf("w:1", "t:2"))), u)
        // Aller-retour JSON.
        assertEquals(u, Personnes.lire(Personnes.json(u)))
    }

    @Test
    fun principalAffiche() {
        val w = "w:14185551234@s.whatsapp.net"
        val g = listOf("105", w, "m:2005")
        var p = listOf(Personne("p1", g))
        // Sans principal : chacune garde son nom et sa photo.
        assertEquals(null, principalDe(p, w))
        p = listOf(Personne("p1", g, "105"))
        assertEquals("105", principalDe(p, w))
        assertEquals("105", principalDe(p, "m:2005"))
        assertEquals(null, principalDe(p, "105"))
        assertEquals(null, principalDe(p, "i:3005"))
        // Délier rend à la conversation les siens.
        p = delier(p, emptyList(), g, "m:2005").first
        assertEquals(null, principalDe(p, "m:2005"))
        assertEquals("105", principalDe(p, w))
        // La principale déliée : plus de principal pour les autres.
        p = delier(p, emptyList(), g, "105").first
        assertEquals(null, principalDe(p, w))
    }
}
