package ca.jasette.android.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** La mosaïque des groupes sans image : mêmes places que le bureau (src/main.rs, mosaique). */
class MosaiqueTest {
    @Test
    fun places() {
        assertEquals(0, placesMosaique(0).places.size)
        assertEquals(1f, placesMosaique(1).cote)
        val deux = placesMosaique(2)
        assertEquals(0.66f, deux.cote, 1e-6f)
        assertEquals(listOf(0f to 0f), deux.places.take(1))
        assertEquals(0.34f, deux.places[1].first, 1e-6f)
        val trois = placesMosaique(3)
        assertEquals(3, trois.places.size)
        assertEquals(0.23f, trois.places[0].first, 1e-6f) // centrée en haut
        assertEquals(0f, trois.places[0].second, 1e-6f)
        val quatre = placesMosaique(4)
        assertEquals(0.5f, quatre.cote, 1e-6f)
        assertEquals(listOf(0f to 0f, 0.5f to 0f, 0f to 0.5f, 0.5f to 0.5f), quatre.places)
        // Au-delà de quatre : la grille de quatre.
        assertEquals(4, placesMosaique(7).places.size)
        // Chaque petit avatar reste dans le grand.
        for (n in 1..4) {
            val p = placesMosaique(n)
            for ((x, y) in p.places) {
                assert(x >= 0f && y >= 0f && x + p.cote <= 1.0001f && y + p.cote <= 1.0001f)
            }
        }
    }

    @Test
    fun initiales() {
        assertEquals("JO", initialesMembre("Justine Ouellet"))
        assertEquals("S", initialesMembre("Steph 💕"))
        assertNull(initialesMembre("+1 418 555 1234"))
        assertNull(initialesMembre(""))
    }
}
