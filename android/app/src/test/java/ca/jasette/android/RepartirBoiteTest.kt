package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Test

/** La boîte réorganisée en rangées de groupes (RegroupementAuto.kt, `repartir`). */
class RepartirBoiteTest {
    private fun conv(id: String, time: Long, preview: String = "") = Conv(
        id = id, name = id, preview = preview, fromMe = false, time = time, unread = true, group = false, rcs = false,
        color = "", members = emptyList(), readOnly = false, archived = false, e2ee = false, number = "",
    )

    private fun ids(l: List<LigneBoite>) = l.map {
        when (it) {
            is LigneBoite.Conversation -> it.conv.id
            is LigneBoite.Rangee -> "[" + it.groupe.cle + ":" + it.membres.joinToString(",") { c -> c.id } + "]"
        }
    }

    @Test
    fun rangeesALHeureDuPlusRecent() {
        val epinglee = conv("e", 10)
        val l = listOf(epinglee, conv("a", 900), conv("p1", 800), conv("b", 700), conv("p2", 600), conv("v", 500), conv("c", 400))
        val g = mapOf("p1" to GroupeAuto.PROMOS, "p2" to GroupeAuto.PROMOS, "v" to GroupeAuto.AVIS)
        val r = repartir(l, 1, 1_000_000) { g[it.id] }
        assertEquals(listOf("e", "a", "[promos:p1,p2]", "b", "[avis:v]", "c"), ids(r))
    }

    @Test
    fun codesEnHautQuinzeMinutes() {
        val maintenant = 10_000_000L
        val code = conv("k", maintenant - 60_000, "Votre code est 482913")
        val l = listOf(conv("e", 1), conv("a", maintenant), code)
        val g = mapOf("k" to GroupeAuto.CODES)
        // Un code d'il y a une minute : sous les épinglées, devant plus récent que lui.
        assertEquals(listOf("e", "[codes:k]", "a"), ids(repartir(l, 1, maintenant) { g[it.id] }))
        // Après 15 minutes : à sa place, par l'heure.
        assertEquals(listOf("e", "a", "[codes:k]"), ids(repartir(l, 1, maintenant + CODES_EN_HAUT_MS) { g[it.id] }))
        assertEquals("482913", dernierCode(listOf(code))?.second)
    }
}
