package ca.jasette.android

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Le crayon de la fiche : la marque `contacts_perso` (mêmes cas que src/personne.rs) et le choix du contact Google visé. */
class ContactsPersoTest {
    private val w = "w:14185551234@s.whatsapp.net"

    @Test
    fun nettoyageEtFil() {
        val m = ContactsPerso.lire(
            JSONObject("""{"123":{"nom":" Maman ","photo":"/srv/media/contacts/ab.jpg","google":true},"m:9":{},"x":3,"s:1":{"nom":"a\nb","photo":"/p.jpg"}}"""),
        )
        assertEquals(2, m.size)
        assertEquals(ContactPerso("Maman", "/srv/media/contacts/ab.jpg", true), m["123"])
        // Un nom à saut de ligne est vidé ; la photo reste.
        assertEquals(ContactPerso("", "/p.jpg"), m["s:1"])
        // Aller-retour : seuls les champs utiles partent.
        val o = ContactsPerso.json(m)
        val e = o.getJSONObject("123")
        assertEquals(listOf("Maman", "/srv/media/contacts/ab.jpg", true), listOf(e.get("nom"), e.get("photo"), e.get("google")))
        assertFalse(o.getJSONObject("s:1").has("google"))
        assertEquals(m, ContactsPerso.lire(o))
        assertTrue(ContactsPerso.nettoyer(mapOf("w:1" to ContactPerso("é".repeat(200)))).isEmpty())
    }

    @Test
    fun cleEtApplication() {
        var perso = emptyMap<String, ContactPerso>()
        val p = listOf(Personne("p1", listOf("123", w, "m:9"), principal = w))
        val g = listOf("123", w)
        // Rien encore : la clé est la principale.
        assertNull(ContactsPerso.de(perso, p, "123"))
        assertEquals(w, ContactsPerso.cle(perso, p, g))
        // Posée sur la principale : vaut pour toutes les conversations de la personne, pas pour les autres.
        perso = ContactsPerso.poser(perso, w, "  Maman ", null, false)
        assertEquals("Maman", perso[w]?.nom)
        for (id in listOf("123", w, "m:9")) assertEquals(id, w, ContactsPerso.de(perso, p, id)?.first)
        assertNull(ContactsPerso.de(perso, p, "i:4"))
        // Une entrée sur la conversation elle-même l'emporte.
        assertEquals("Steph", ContactsPerso.de(perso + ("m:9" to ContactPerso("Steph")), p, "m:9")?.second?.nom)
        // La clé existante est gardée, même depuis une autre conversation.
        assertEquals(w, ContactsPerso.cle(perso, p, listOf("m:9")))
        // Une photo ; vider le nom la garde ; sans rien, l'entrée part.
        perso = ContactsPerso.poser(perso, w, "Maman", "/m/contacts/a.jpg", true)
        perso = ContactsPerso.poser(perso, w, "", null, true)
        assertEquals(ContactPerso("", "/m/contacts/a.jpg", true), perso[w])
        assertTrue(ContactsPerso.poser(perso, w, "", "", false).isEmpty())
        // « Rétablir » : plus rien pour la fiche, même si deux entrées la touchent.
        val deux = perso + ("123" to ContactPerso("Mon Amour"))
        assertTrue(ContactsPerso.retablir(deux, p, g).isEmpty())
        // Sans personne ni principale : l'ancre.
        assertEquals("123", ContactsPerso.cle(emptyMap(), emptyList(), g))
    }

    @Test
    fun relierLaFiche() {
        // Même numéro : reliées à la main, sans principale.
        val p = ContactsPerso.relierFiche(emptyList(), listOf("123", w), "p1")
        assertEquals(listOf(Personne("p1", listOf("123", w))), p)
        // Seule : rien à relier.
        assertTrue(ContactsPerso.relierFiche(emptyList(), listOf("m:9"), "p2").isEmpty())
        // Une personne existante gagne les conversations libres, garde sa principale.
        val r = ContactsPerso.relierFiche(listOf(Personne("p3", listOf("123", "m:7"), "m:7")), listOf("123", w), "p4")
        assertEquals(listOf(Personne("p3", listOf("123", "m:7", w), "m:7")), r)
    }

    @Test
    fun recadrage() {
        assertEquals(Triple(150, 0, 600), ContactsPerso.carreCentre(900, 600))
        assertEquals(Triple(0, 100, 300), ContactsPerso.carreCentre(300, 500))
        assertEquals(1, ContactsPerso.echantillon(800))
        assertEquals(2, ContactsPerso.echantillon(1024))
        assertEquals(4, ContactsPerso.echantillon(3000))
        assertEquals(1, ContactsPerso.echantillon(100))
    }

    private fun num(n: String) = NumeroTel(n, Contacts.chiffres(n), Contacts.cle(Contacts.chiffres(n)), e164(n), true)

    @Test
    fun contactVise() {
        val contacts = listOf(
            ContactTel(7, "Stéphanie L.", listOf(num("+1 418 555 1234")), null, null),
            ContactTel(3, "Mon Amour", listOf(num("418-555-1234")), null, null),
            ContactTel(9, "Marc", listOf(num("+15145550000")), null, null),
        )
        // Sans numéro : rien dans le carnet.
        assertEquals(CibleContact.SansNumero, ContactsGoogle.cible(emptyList(), contacts, "Maman", "moi@gmail.com"))
        // Deux contacts portent le numéro : celui qui a déjà ce nom, sinon le plus ancien.
        assertEquals(CibleContact.Existant(3, "Mon Amour"), ContactsGoogle.cible(listOf("+14185551234"), contacts, "mon amour", null))
        assertEquals(CibleContact.Existant(3, "Mon Amour"), ContactsGoogle.cible(listOf("+14185551234"), contacts, "Maman", null))
        // Le premier numéro qu'un contact porte gagne.
        assertEquals(CibleContact.Existant(9, "Marc"), ContactsGoogle.cible(listOf("+33612345678", "+15145550000"), contacts, "M", null))
        // Personne : un nouveau contact, pour le premier numéro, dans le compte donné.
        assertEquals(CibleContact.Nouveau("+33612345678", "moi@gmail.com"), ContactsGoogle.cible(listOf("+33612345678"), contacts, "Zoé", "moi@gmail.com"))
    }

    @Test
    fun numerosDeLaPersonne() {
        fun conv(id: String, number: String = "") = Conv(
            id = id, name = "X", preview = "", fromMe = false, time = 0, unread = false, group = false, rcs = false,
            color = "", members = emptyList(), readOnly = false, archived = false, e2ee = false, number = number,
            large = false, announce = false,
        )
        val convs = listOf(conv("m:9"), conv("123", "(418) 555-1234"), conv(w), conv("s:+15145550000")).associateBy { it.id }
        // Messenger n'en a pas ; Google et WhatsApp, le même (une seule fois) ; Signal, le sien.
        assertEquals(listOf("+14185551234", "+15145550000"), ContactsGoogle.numeros(listOf("m:9", "123", w, "s:+15145550000", "i:inconnue")) { convs[it] })
        assertTrue(ContactsGoogle.numeros(listOf("m:9")) { convs[it] }.isEmpty())
    }

    @Test
    fun comptesEtEntreesVenuesDAilleurs() {
        assertEquals("b@gmail.com", ContactsGoogle.comptePrincipal(mapOf("a@gmail.com" to 3, "b@gmail.com" to 40)))
        assertEquals("a@gmail.com", ContactsGoogle.comptePrincipal(mapOf("b@gmail.com" to 0, "a@gmail.com" to 0)))
        assertNull(ContactsGoogle.comptePrincipal(emptyMap()))
        val perso = mapOf("123" to ContactPerso("Maman", "", true), "m:9" to ContactPerso("Steph"))
        // Seules les entrées « google » pas encore appliquées.
        assertEquals(listOf("123"), ContactsGoogle.aAppliquer(perso, emptySet()).map { it.first })
        val faite = ContactsGoogle.signature("123", perso.getValue("123"))
        assertTrue(ContactsGoogle.aAppliquer(perso, setOf(faite)).isEmpty())
        // Un nouveau nom : à réappliquer.
        assertEquals(1, ContactsGoogle.aAppliquer(mapOf("123" to ContactPerso("Mamie", "", true)), setOf(faite)).size)
        // Le groupe d'une clé : sa personne, la clé d'abord.
        assertEquals(listOf(w, "123", "m:9"), ContactsGoogle.groupeDe(w, listOf(Personne("p1", listOf("123", w, "m:9"), w))))
        assertEquals(listOf("123"), ContactsGoogle.groupeDe("123", emptyList()))
    }
}
