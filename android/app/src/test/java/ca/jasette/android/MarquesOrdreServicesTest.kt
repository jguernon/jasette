package ca.jasette.android

import android.app.Application
import android.content.Context
import ca.jasette.android.ui.ServicesCaches
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/**
 * La marque `ordre_services` (Marques.kt, PROTOCOLE.md §5.5) : l'ordre choisi
 * avant le partage (préférences « affichage ») est repris, envoyé une fois au
 * serveur qui n'en a pas ; ensuite la marque fait foi.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class MarquesOrdreServicesTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    private fun trame(ordre: List<String>?, version: Int, id: Long? = null): JSONObject {
        val o = JSONObject("""{"head":[],"favoris":["a"],"epingles":[],"archives":[],"privees":[],"version":$version}""")
        if (ordre != null) o.put("ordre_services", JSONArray(ordre))
        if (id != null) o.put("id", id)
        return o
    }

    private fun ordreEnvoye(t: JSONObject): List<String> =
        t.getJSONObject("marques").getJSONArray("ordre_services").let { a -> List(a.length()) { a.getString(it) } }

    @Test
    fun migrationPuisMarque() {
        // L'ordre d'avant le partage, gardé par ServicesCaches.
        ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).edit()
            .putString("ordre_services", "signal,google,signal,").commit()
        Marques.charger(ctx)
        assertEquals(listOf("signal", "google"), Marques.ordreServices)
        assertEquals(listOf(Service.SIGNAL, Service.GOOGLE), ServicesCaches.ordonnes().take(2))

        val envoyees = ArrayList<JSONObject>()
        // Un serveur d'avant la marque : l'ordre reste ici, rien ne part pour lui.
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(trame(null, 3))
        assertEquals(listOf("signal", "google"), Marques.ordreServices)
        assertTrue(envoyees.isEmpty())

        // Le serveur passe à jour, sans ordre encore : le nôtre part, une fois.
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(trame(emptyList(), 3))
        assertEquals(1, envoyees.size)
        assertEquals(listOf("signal", "google"), ordreEnvoye(envoyees.last()))
        val id = envoyees.last().getJSONObject("marques").getLong("id")
        Marques.recu(trame(listOf("signal", "google"), 4, id))
        assertEquals(1, envoyees.size)

        // Gardé dans les marques : relu tel quel, même si « affichage » change.
        ctx.getSharedPreferences("affichage", Context.MODE_PRIVATE).edit()
            .putString("ordre_services", "x").commit()
        Marques.charger(ctx)
        assertEquals(listOf("signal", "google"), Marques.ordreServices)

        // Un autre appareil change l'ordre : la marque fait foi, rien ne repart.
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(trame(listOf("x", "telegram", "x", ""), 5))
        assertEquals(listOf("x", "telegram"), Marques.ordreServices)
        assertEquals(listOf(Service.X, Service.TELEGRAM), ServicesCaches.ordonnes().take(2))
        assertEquals(1, envoyees.size)

        // Les flèches : l'ordre complet part au serveur.
        ServicesCaches.echanger(Service.X, Service.TELEGRAM)
        val l = ordreEnvoye(envoyees.last())
        assertEquals(Service.entries.size, l.size)
        assertEquals(listOf("telegram", "x"), l.take(2))
        assertFalse(Marques.ordreServices.isEmpty())
        Marques.brancher(null, null)
    }

    @Test
    fun sansServeurOrdreLocal() {
        Marques.charger(ctx)
        assertTrue(Marques.ordreServices.isEmpty())
        Marques.brancher(null, null)
        ServicesCaches.echanger(Service.GOOGLE, Service.MESSENGER)
        Marques.charger(ctx)
        assertEquals(listOf(Service.MESSENGER, Service.GOOGLE), ServicesCaches.ordonnes().take(2))
    }
}
