package ca.jasette.android

import android.app.Application
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/** La marque `contacts_perso` dans la synchronisation des marques (Marques.kt, PROTOCOLE.md §5.5). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class MarquesContactsPersoTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    private fun trame(perso: String?, version: Int, id: Long? = null): JSONObject {
        val o = JSONObject("""{"head":[],"favoris":[],"epingles":[],"archives":[],"privees":[],"version":$version}""")
        if (perso != null) o.put("contacts_perso", JSONObject(perso))
        if (id != null) o.put("id", id)
        return o
    }

    @Test
    fun synchronisation() {
        Marques.charger(ctx)
        val envoyees = ArrayList<JSONObject>()
        Marques.brancher("empreinte") { envoyees += it; true }
        // Reçue du serveur : posée ici, gardée sur disque.
        Marques.recu(trame("""{"123":{"nom":"Maman","photo":"/srv/media/contacts/ab.jpg"}}""", 3))
        assertEquals(ContactPerso("Maman", "/srv/media/contacts/ab.jpg"), Marques.contactsPerso["123"])
        Marques.charger(ctx)
        assertEquals("Maman", Marques.contactsPerso["123"]?.nom)
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(trame("""{"123":{"nom":"Maman","photo":"/srv/media/contacts/ab.jpg"}}""", 3))
        // Changée ici (avec un lien) : l'objet complet part au serveur.
        Marques.changerContactsPerso(mapOf("w:1" to ContactPerso("Steph")), listOf(Personne("p1", listOf("w:1", "m:2"))))
        val m = envoyees.last().getJSONObject("marques")
        assertEquals("""{"w:1":{"nom":"Steph"}}""", m.getJSONObject("contacts_perso").toString())
        assertEquals(1, m.getJSONArray("personnes").length())
        // Un serveur d'avant `contacts_perso` : les nôtres restent.
        Marques.recu(trame(null, 4, id = m.getLong("id")))
        assertEquals("Steph", Marques.contactsPerso["w:1"]?.nom)
        assertTrue("123" !in Marques.contactsPerso)
        Marques.brancher(null, null)
    }
}
