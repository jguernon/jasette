package ca.jasette.android

import android.app.Application
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/** La marque `groupes_auto` dans la synchronisation des marques (Marques.kt, PROTOCOLE.md §5.5). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class MarquesGroupesAutoTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    private fun trame(groupes: String?, version: Int, id: Long? = null): JSONObject {
        val o = JSONObject("""{"head":[],"favoris":[],"epingles":[],"archives":[],"privees":[],"version":$version}""")
        if (groupes != null) o.put("groupes_auto", JSONObject(groupes))
        if (id != null) o.put("id", id)
        return o
    }

    @Test
    fun synchronisation() {
        Marques.charger(ctx)
        val envoyees = ArrayList<JSONObject>()
        Marques.brancher("empreinte") { envoyees += it; true }
        // Reçue du serveur : posée ici (valeur démesurée ignorée), gardée sur disque.
        Marques.recu(trame("""{"123":"promos","t:9":"aucun","x:1":"${"z".repeat(40)}"}""", 3))
        assertEquals("promos", Marques.groupeAuto("123"))
        assertNull(Marques.groupeAuto("x:1"))
        Marques.charger(ctx)
        assertEquals("aucun", Marques.groupeAuto("t:9"))
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(trame("""{"123":"promos","t:9":"aucun"}""", 3))
        // Corrigée ici : l'objet complet part au serveur ; une valeur inconnue est refusée.
        Marques.corrigerGroupe("123", "codes")
        Marques.corrigerGroupe("456", "nimporte")
        val m = envoyees.last().getJSONObject("marques")
        assertEquals("codes", m.getJSONObject("groupes_auto").getString("123"))
        assertEquals(2, m.getJSONObject("groupes_auto").length())
        // Un serveur d'avant `groupes_auto` : les nôtres restent.
        Marques.recu(trame(null, 4, id = m.getLong("id")))
        assertEquals("codes", Marques.groupeAuto("123"))
        Marques.oublier("123")
        assertNull(Marques.groupeAuto("123"))
        Marques.brancher(null, null)
    }
}
