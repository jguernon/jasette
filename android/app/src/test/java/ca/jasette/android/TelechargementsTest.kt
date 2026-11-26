package ca.jasette.android

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TelechargementsTest {
    @Test
    fun pasDeBoucleApresUnEchec() {
        assertTrue(aDemanderMedia("dc:1", dejaLa = false, enCours = false, echoue = false))
        assertFalse(aDemanderMedia("dc:1", dejaLa = false, enCours = false, echoue = true))
        assertFalse(aDemanderMedia("dc:1", dejaLa = true, enCours = false, echoue = false))
        assertFalse(aDemanderMedia("dc:1", dejaLa = false, enCours = true, echoue = false))
        assertFalse(aDemanderMedia("", dejaLa = false, enCours = false, echoue = false))
    }
}
