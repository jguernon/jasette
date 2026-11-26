package ca.jasette.android.ui

import org.junit.Assert.assertEquals
import org.junit.Test

/** Le rail : combien de tuiles de service tiennent, et le « ••• » seulement en cas de débordement. */
class RailTuilesTest {
    private val tuile = 60f
    private val marge = 12f

    @Test
    fun toutTient() {
        // 8 services × 60 dp + 12 de marge = 492 dp.
        assertEquals(TuilesRail(8, false), tuilesRail(8, 492f, tuile, marge))
        assertEquals(TuilesRail(8, false), tuilesRail(8, 900f, tuile, marge))
        assertEquals(TuilesRail(0, false), tuilesRail(0, 10f, tuile, marge))
    }

    @Test
    fun debordement() {
        // Un dp de moins : 7 places, donc 6 services et le « ••• » (les deux derniers partent).
        assertEquals(TuilesRail(6, true), tuilesRail(8, 491f, tuile, marge))
        // Fold déplié, rail d'environ 440 dp de libre : 7 places pour 9 services.
        assertEquals(TuilesRail(6, true), tuilesRail(9, 440f, tuile, marge))
        // Très bas (rotation) : une seule place, le « ••• » seul.
        assertEquals(TuilesRail(0, true), tuilesRail(5, 80f, tuile, marge))
        assertEquals(TuilesRail(0, true), tuilesRail(5, 0f, tuile, marge))
    }

    @Test
    fun pastilleDesCaches() {
        assertEquals(7, nonLusCaches(listOf(1, 0, 2, 3, 4), 3))
        assertEquals(0, nonLusCaches(listOf(1, 2), 2))
    }
}
