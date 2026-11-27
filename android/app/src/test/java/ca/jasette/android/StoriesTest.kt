package ca.jasette.android

import android.app.Application
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/** Les stories des contacts (Stories.kt, Marques.kt, PROTOCOLE.md §5.15). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class StoriesTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    @Test
    fun cercle() {
        val m = 1_000_000L
        assertTrue(cercleVisible(500, m + 1, false, null, m))
        // Échue, déjà vue, ou déjà ouverte depuis Jasette : rien.
        assertFalse(cercleVisible(500, m, false, null, m))
        assertFalse(cercleVisible(500, m + 1, true, null, m))
        assertFalse(cercleVisible(500, m + 1, false, 500, m))
        // Une story plus récente que celle ouverte : le cercle revient.
        assertTrue(cercleVisible(600, m + 1, false, 500, m))
        // Heure inconnue (Facebook) : visible jusqu'à ce qu'elle soit ouverte (1).
        assertTrue(cercleVisible(0, m + 1, false, null, m))
        assertFalse(cercleVisible(0, m + 1, false, 1, m))
    }

    @Test
    fun purgeEtUnion() {
        val m = 10 * STORIES_OUVERTES_DUREE
        val vieux = m - STORIES_OUVERTES_DUREE - 1
        assertEquals(mapOf("i:1" to m), nettoyerStoriesOuvertes(mapOf("i:1" to m, "i:2" to vieux, "" to m), m))
        assertEquals(
            mapOf("i:1" to m, "m:3" to m - 5),
            unirStoriesOuvertes(mapOf("i:1" to m - 1, "m:3" to m - 5), mapOf("i:1" to m), m),
        )
    }

    private fun trame(stories: Boolean?, ouvertes: String?, version: Int, id: Long? = null): JSONObject {
        val o = JSONObject("""{"head":[],"favoris":[],"epingles":[],"archives":[],"privees":[],"version":$version}""")
        if (stories != null) o.put("stories", stories)
        if (ouvertes != null) o.put("stories_ouvertes", JSONObject(ouvertes))
        if (id != null) o.put("id", id)
        return o
    }

    @Test
    fun synchronisation() {
        Marques.charger(ctx)
        val envoyees = ArrayList<JSONObject>()
        Marques.brancher("empreinte") { envoyees += it; true }
        val maint = System.currentTimeMillis()
        Marques.recu(trame(true, """{"i:42":${maint - 1000}}""", 3))
        assertTrue(Marques.stories)
        // Un événement du pont Instagram (ids déjà préfixés) : la story de i:42 est plus récente.
        Stories.recevoir(
            JSONObject().put("ev", "stories").put(
                "items",
                org.json.JSONArray().put(
                    JSONObject().put("conv", "i:42").put("latest", maint).put("until", maint + 3_600_000)
                        .put("seen", false).put("link", "https://www.instagram.com/stories/camille/").put("service", "instagram"),
                ),
            ),
            Service.INSTAGRAM,
        )
        val s = Stories.active("i:42")
        assertNotNull(s)
        // Ouverte : le cercle s'efface, et la marque complète part au serveur.
        Marques.storyOuverte("i:42", s!!.latest)
        assertNull(Stories.active("i:42"))
        val m = envoyees.last().getJSONObject("marques")
        assertEquals(maint, m.getJSONObject("stories_ouvertes").getLong("i:42"))
        assertTrue(m.getBoolean("stories"))
        // Un serveur d'avant §5.15 : les nôtres restent.
        Marques.recu(trame(null, null, 4, id = m.getLong("id")))
        assertTrue(Marques.stories)
        assertEquals(maint, Marques.storiesOuvertes["i:42"])
        // Décoché : tout est oublié.
        Marques.choisirStories(false)
        Stories.regle(false)
        assertNull(Stories.active("i:42"))
        Marques.brancher(null, null)
    }
}
