package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class NouveauGroupeTest {
    @Test
    fun pucesEtGroupe() {
        var c = ChoixNouveau()
        assertTrue("pas de groupe sur « Tout »", c.filtre == null && !c.groupeOffert)
        c = c.avecFiltre(Service.X)
        assertFalse("X ne crée pas de groupe", c.groupeOffert)
        assertSame(c, c.basculerGroupe())
        c = c.avecFiltre(Service.WHATSAPP).basculerGroupe()
        assertTrue(c.groupe)
        assertTrue(!c.montre(Service.X) && c.montre(Service.WHATSAPP))
        // « Tout » fait sortir du mode groupe.
        assertFalse(c.avecFiltre(null).groupe)
        // La première personne fixe le service.
        c = c.basculer("w:15145550101@s.whatsapp.net", "Ana")
        assertEquals(Service.WHATSAPP, c.filtre)
        assertTrue(!c.pucePermise(null) && !c.pucePermise(Service.SIGNAL))
        assertSame(c, c.avecFiltre(Service.SIGNAL))
        assertSame("un seul service par groupe", c, c.basculer("s:+15145550102", "Bob"))
        assertSame("un groupe n'est pas une personne", c, c.basculer("w:1203630000@g.us", "Un groupe"))
        c = c.basculer("w:+15145550103", "+15145550103")
        assertTrue(c.pret)
        assertEquals(listOf("15145550101@s.whatsapp.net", "+15145550103"), c.membres)
        // La toucher de nouveau l'enlève ; plus personne : les puces reviennent.
        c = c.basculer("w:15145550101@s.whatsapp.net", "")
        assertFalse(c.pret)
        c = c.basculer("w:+15145550103", "")
        assertTrue(c.choisis.isEmpty() && c.pucePermise(null) && c.pucePermise(Service.SIGNAL))
        // Sortir du mode groupe oublie tout le monde.
        c = c.avecFiltre(Service.MESSENGER).basculer("m:100", "Cy").basculerGroupe()
        assertTrue(!c.groupe && c.choisis.isEmpty())
    }

    @Test
    fun smsDuTelephone() {
        val c = ChoixNouveau(sms = true).avecFiltre(Service.GOOGLE)
        assertFalse("les SMS du téléphone ne créent pas de groupe", c.groupeOffert)
        assertTrue(ChoixNouveau().avecFiltre(Service.GOOGLE).groupeOffert)
    }

    @Test
    fun idsDeGroupe() {
        assertTrue(ChoixNouveau.idGroupe("w:1203630000@g.us"))
        assertFalse(ChoixNouveau.idGroupe("w:15145550101@s.whatsapp.net"))
        assertTrue(ChoixNouveau.idGroupe("t:g123") && ChoixNouveau.idGroupe("t:c123") && !ChoixNouveau.idGroupe("t:u123"))
        assertTrue(ChoixNouveau.idGroupe("s:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="))
        assertFalse(ChoixNouveau.idGroupe("s:6f1c2c43-6c1f-4d0b-9e0c-0a1b2c3d4e5f"))
        assertFalse(ChoixNouveau.idGroupe("m:100") || ChoixNouveau.idGroupe("5145550101"))
        assertFalse(ChoixNouveau.nommeGroupes(Service.INSTAGRAM))
    }
}
