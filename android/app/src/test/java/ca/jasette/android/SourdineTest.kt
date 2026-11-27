package ca.jasette.android

import android.app.Application
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.time.LocalDateTime

/** La sourdine par conversation (Sourdine.kt, marque `sourdine`, PROTOCOLE.md §5.5). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class SourdineTest {
    private val ctx: Application get() = RuntimeEnvironment.getApplication()

    @Test
    fun durees() {
        val m = 1_000_000L
        assertEquals(m + 3_600_000L, DureeSourdine.HEURE.echeance(m))
        assertEquals(m + 8 * 3_600_000L, DureeSourdine.HUIT_HEURES.echeance(m))
        assertEquals(m + 24 * 3_600_000L, DureeSourdine.JOUR.echeance(m))
        assertEquals(m + 7 * 24 * 3_600_000L, DureeSourdine.SEMAINE.echeance(m))
        assertEquals(0L, DureeSourdine.TOUJOURS.echeance(m))
    }

    @Test
    fun activeEtNettoyage() {
        val s = mapOf("m:1" to 0L, "w:2" to 5_000L, "i:3" to 100L, "" to 0L, "x:4" to -3L)
        assertTrue(sourdineActive(s, "m:1", Long.MAX_VALUE))
        assertTrue(sourdineActive(s, "w:2", 4_999L))
        assertFalse(sourdineActive(s, "w:2", 5_000L))
        assertFalse(sourdineActive(s, "z:9", 0L))
        assertEquals(mapOf("m:1" to 0L, "w:2" to 5_000L), nettoyerSourdine(s, 200L))
        assertEquals(0L, echeanceSourdine(s, listOf("w:2", "m:1"), 200L))
        assertEquals(5_000L, echeanceSourdine(s, listOf("w:2", "i:3"), 200L))
        assertNull(echeanceSourdine(s, listOf("i:3"), 200L))
    }

    @Test
    fun libelles() {
        val m = LocalDateTime.of(2026, 11, 27, 9, 30) // un vendredi
        assertEquals("En sourdine", libelleSourdineA(null, m, false))
        assertEquals("En sourdine jusqu'à demain 14 h 00", libelleSourdineA(LocalDateTime.of(2026, 11, 28, 14, 0), m, false))
        assertEquals("En sourdine jusqu'à lundi 8 h 05", libelleSourdineA(LocalDateTime.of(2026, 11, 30, 8, 5), m, false))
        assertEquals("En sourdine jusqu'au 4 déc. 9 h 30", libelleSourdineA(LocalDateTime.of(2026, 12, 4, 9, 30), m, false))
        assertEquals("Muted until tomorrow 2:00 PM", libelleSourdineA(LocalDateTime.of(2026, 11, 28, 14, 0), m, true))
    }

    private fun trame(sourdine: String?, version: Int, id: Long? = null): JSONObject {
        val o = JSONObject("""{"head":[],"favoris":[],"epingles":[],"archives":[],"privees":[],"version":$version}""")
        if (sourdine != null) o.put("sourdine", JSONObject(sourdine))
        if (id != null) o.put("id", id)
        return o
    }

    /** Le filtre des notifications et la synchronisation de la marque. */
    @Test
    fun filtreEtSynchronisation() {
        Marques.charger(ctx)
        val envoyees = ArrayList<JSONObject>()
        Marques.brancher("empreinte") { envoyees += it; true }
        val maint = System.currentTimeMillis()
        // Reçue du serveur (une échue tombe) : pas de notification pour m:1 ni w:2.
        Marques.recu(trame("""{"m:1":0,"w:2":${maint + 3_600_000},"i:3":${maint - 1}}""", 3))
        assertFalse(notificationPermise("m:1"))
        assertFalse(notificationPermise("w:2"))
        assertTrue(notificationPermise("i:3"))
        assertTrue(notificationPermise("s:5"))
        // L'échéance passée : la notification revient, sans rien faire.
        assertTrue(notificationPermise("w:2", maint + 3_600_001))
        // Gardée sur disque.
        Marques.charger(ctx)
        assertTrue(Marques.enSourdine("m:1"))
        Marques.brancher("empreinte") { envoyees += it; true }
        Marques.recu(trame("""{"m:1":0,"w:2":${maint + 3_600_000}}""", 3))
        // Posée ici (la fiche : plusieurs conversations), puis réactivée : l'objet complet part.
        Marques.poserSourdine(listOf("t:7", "b:8"), 0L)
        var m = envoyees.last().getJSONObject("marques")
        assertEquals(4, m.getJSONObject("sourdine").length())
        Marques.recu(trame(m.getJSONObject("sourdine").toString(), 4, id = m.getLong("id")))
        Marques.poserSourdine(listOf("m:1"), null)
        m = envoyees.last().getJSONObject("marques")
        assertFalse(m.getJSONObject("sourdine").has("m:1"))
        assertTrue(notificationPermise("m:1"))
        // Un serveur d'avant la sourdine : la nôtre reste.
        Marques.recu(trame(null, 5, id = m.getLong("id")))
        assertTrue(Marques.enSourdine("t:7"))
        // L'élagage local : une sourdine échue s'en va.
        Marques.sourdine["x:9"] = maint + 10
        assertTrue(Marques.elaguerSourdine(maint + 11))
        assertFalse(Marques.sourdine.containsKey("x:9"))
        Marques.brancher(null, null)
    }
}
