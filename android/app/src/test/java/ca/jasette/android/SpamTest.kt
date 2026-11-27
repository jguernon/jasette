package ca.jasette.android

import android.app.Application
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/** Le spam par conversation (Spam.kt, marque `spam`, PROTOCOLE.md §5.5). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class SpamTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    private fun conv(id: String, group: Boolean = false, large: Boolean = false, unread: Boolean = true) = Conv(
        id = id, name = id, preview = "Gagnez un iPhone", fromMe = false, time = 1_000L, unread = unread, group = group,
        rcs = false, color = "", members = emptyList(), readOnly = false, archived = false, e2ee = false, number = "",
        large = large,
    )

    /** « Envoyer STOP » : seulement une conversation Google Messages à deux. */
    @Test
    fun stopSeulementGoogleADeux() {
        assertTrue(stopOffert(conv("15145551234")))
        assertFalse(stopOffert(conv("15145551234", group = true)))
        assertFalse(stopOffert(conv("m:42")))
        assertFalse(stopOffert(conv("w:5551@s.whatsapp.net")))
        assertFalse(stopOffert(conv("s:+15145551234")))
        assertFalse(stopOffert(conv("t:9", large = true)))
    }

    /** « Tout vider » : seulement Google, Messenger et Instagram. */
    @Test
    fun viderSelonService() {
        val (oui, non) = aViderSpam(listOf(conv("123"), conv("m:1"), conv("i:2"), conv("w:3"), conv("t:4")))
        assertEquals(listOf("123", "m:1", "i:2"), oui.map { it.id })
        assertEquals(listOf("w:3", "t:4"), non.map { it.id })
    }

    /** Le filtre : jamais de notification pour le spam, même sans sourdine et pour un nouveau message. */
    @Test
    fun filtreNotification() {
        val spam = mapOf("123" to 5L)
        assertFalse(notificationPermiseDans(spam, mapOf("123" to 0L), "123", 1_000L))
        assertFalse(notificationPermiseDans(spam, emptyMap(), "123", Long.MAX_VALUE))
        assertTrue(notificationPermiseDans(spam, emptyMap(), "m:1", 1_000L))
        assertEquals(mapOf("a" to 1L), nettoyerSpam(mapOf("a" to 1L, "" to 2L, "b" to -1L)))
        assertEquals(mapOf("a" to 3L), lireSpam(JSONObject("""{"a":3,"b":"x"}""")))
    }

    private fun trame(spam: String?, version: Int, id: Long? = null): JSONObject {
        val o = JSONObject("""{"head":[],"favoris":[],"epingles":[],"archives":[],"privees":[],"version":$version}""")
        if (spam != null) o.put("spam", JSONObject(spam))
        if (id != null) o.put("id", id)
        return o
    }

    /** Marquer (sourdine sans fin, hors Head/favoris/épinglées, hors de la boîte), synchroniser, « Ce n'est pas du spam ». */
    @Test
    fun marquerSynchroniserBoite() {
        Marques.charger(ctx)
        val envoyees = ArrayList<JSONObject>()
        var version = 3
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(trame("{}", version))
        // Le serveur accepte tout ce qui part, et le renvoie (comme recevoirMarques).
        fun accepter(): JSONObject {
            var dernier = envoyees.last().getJSONObject("marques")
            var vus = -1
            while (vus != envoyees.size) {
                vus = envoyees.size
                dernier = envoyees.last().getJSONObject("marques")
                version++
                Marques.recu(JSONObject(dernier.toString()).put("version", version))
            }
            return dernier
        }
        Depot.convs.clear()
        Depot.convs.addAll(listOf(conv("15145550000"), conv("m:1")))
        Marques.basculerFavori("15145550000")
        Marques.basculerHead("15145550000")
        Marques.basculerEpingle("15145550000")
        accepter()
        assertTrue(Marques.estFavori("15145550000"))

        Marques.marquerSpam("15145550000", 42L)
        assertFalse(Marques.estFavori("15145550000"))
        assertFalse(Marques.estDansHead("15145550000"))
        assertFalse(Marques.estEpinglee("15145550000"))
        var m = accepter()
        assertEquals(42L, m.getJSONObject("spam").getLong("15145550000"))
        assertEquals(0L, m.getJSONObject("sourdine").getLong("15145550000"))
        assertEquals(0, m.getJSONArray("favoris").length())
        // Hors de la boîte et de ses non-lus ; dans le dossier Spam ; aucune notification.
        assertEquals(listOf("m:1"), Depot.boite.map { it.id })
        assertEquals(listOf("15145550000"), Spam.liste.map { it.id })
        assertFalse(notificationPermise("15145550000"))
        assertTrue(notificationPermise("m:1"))
        // Gardé sur disque.
        Marques.charger(ctx)
        assertTrue(Marques.estSpam("15145550000"))
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(JSONObject(m.toString()).put("version", version))
        // Un serveur d'avant le spam : le nôtre reste.
        Marques.recu(JSONObject(m.toString()).put("version", version).apply { remove("spam") })
        assertTrue(Marques.estSpam("15145550000"))
        // Reçu d'un autre appareil.
        version++
        Marques.recu(JSONObject(m.toString()).put("version", version).put("spam", JSONObject("""{"15145550000":42,"m:1":7}""")))
        assertTrue(Marques.estSpam("m:1"))
        assertTrue(Depot.boite.isEmpty())
        // « Ce n'est pas du spam » : la clé part, la sourdine posée avec elle aussi.
        Marques.pasSpam("15145550000")
        m = accepter()
        assertFalse(m.getJSONObject("spam").has("15145550000"))
        assertTrue(m.getJSONObject("spam").has("m:1"))
        assertFalse(Marques.enSourdine("15145550000"))
        assertTrue(notificationPermise("15145550000"))
        assertEquals(listOf("15145550000"), Depot.boite.map { it.id })
        Marques.pasSpam("m:1")
        Depot.convs.clear()
        Marques.brancher(null, null)
    }
}
