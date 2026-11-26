package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** « Partager ce que j'écoute » : l'anti-rafale, les textes, MusicBrainz, la sélection (MoteurMusique.kt). */
class MoteurMusiqueTest {
    private val min = 60_000L
    private val t0 = 1_800_000_000_000L
    private val daft = Ecoute("One More Time", "Daft Punk")
    private val cure = Ecoute("Friday I'm in Love", "The Cure")
    private val texte: (Ecoute) -> String? = { texteMusique(it, "artiste", null, false, 60) }

    private fun MoteurMusique.a(m: Long, e: Ecoute?, musicale: Boolean = true, format: String = "artiste") =
        pas(t0 + m * min, e, format, musicale, true, texte)

    @Test
    fun premierePublicationToutDeSuite() {
        val m = MoteurMusique()
        assertEquals(MoteurMusique.Action.Publier("🎵 Écoute Daft Punk"), m.a(0, daft, musicale = false))
    }

    @Test
    fun auPlusUneFoisParQuinzeMinutes() {
        val m = MoteurMusique()
        var n = 0
        // Une chanson différente chaque minute pendant une heure.
        for (i in 0L until 60L) if (m.a(i, if (i % 2 == 0L) daft else cure) is MoteurMusique.Action.Publier) n++
        assertTrue("publications : $n", n <= 4)
    }

    @Test
    fun rienSiLeTexteNeChangePas() {
        val m = MoteurMusique()
        m.a(0, daft, musicale = false)
        for (i in 1L..60L) assertNull(m.a(i, daft))
    }

    @Test
    fun republieSiLaNoteActuelleNestPlusMusicale() {
        val m = MoteurMusique()
        m.a(0, daft, musicale = false)
        // Une note écrite à la main est arrivée : la musique repasse par-dessus, 15 minutes après.
        assertNull(m.a(5, daft, musicale = false))
        assertEquals(MoteurMusique.Action.Publier("🎵 Écoute Daft Punk"), m.a(15, daft, musicale = false))
    }

    @Test
    fun retientCeQuiAJoueLePlusLongtemps() {
        val m = MoteurMusique()
        m.a(0, daft, musicale = false)
        // 10 minutes de The Cure, puis 2 minutes de Daft Punk à la fin de la période.
        for (i in 1L..12L) m.a(i, cure)
        m.a(13, daft)
        m.a(14, daft)
        assertEquals(MoteurMusique.Action.Publier("🎵 Écoute The Cure"), m.a(15, daft))
    }

    @Test
    fun pasDeffacementALaPauseMaisApresVingtMinutesUneFois() {
        val m = MoteurMusique()
        m.a(0, daft, musicale = false)
        for (i in 1L..19L) assertNull(m.a(i, null))
        assertEquals(MoteurMusique.Action.Effacer, m.a(20, null))
        for (i in 21L..90L) assertNull(m.a(i, null))
    }

    @Test
    fun rienSansService() {
        val m = MoteurMusique()
        assertNull(m.pas(t0, daft, "artiste", false, false, texte))
    }

    @Test
    fun attendLeGenre() {
        val m = MoteurMusique()
        assertNull(m.pas(t0, daft, "genre", false, true) { null })
        val p = m.pas(t0 + min, daft, "genre", false, true) { texteMusique(it, "genre", "rock", false, 60) }
        assertEquals(MoteurMusique.Action.Publier("🎵 Écoute du rock !"), p)
    }

    @Test
    fun textes() {
        val balado = Ecoute("Épisode 2000", "Spotify Studios", "The Joe Rogan Experience", balado = true)
        assertEquals("🎵 Écoute Daft Punk", texteMusique(daft, "artiste", null, false, 60))
        assertEquals("🎵 Listening to Daft Punk", texteMusique(daft, "artiste", null, true, 60))
        assertEquals("🎙️ Écoute The Joe Rogan Experience", texteMusique(balado, "artiste", null, false, 60))
        assertEquals("🎵 Écoute de la pop !", texteMusique(daft, "genre", "pop", false, 60))
        assertEquals("🎵 Écoute de l'électro !", texteMusique(daft, "genre", "electronic", false, 60))
        assertEquals("🎵 Listening to R&B!", texteMusique(daft, "genre", "rnb", true, 60))
        assertEquals("🎙️ Écoute un podcast", texteMusique(balado, "genre", null, false, 60))
        assertEquals("🎵 Écoute Daft Punk", texteMusique(daft, "genre", null, false, 60))
        assertEquals("🎵 One More Time — Daft Punk", texteMusique(daft, "titre", null, false, 60))
        val long = texteMusique(Ecoute("Un titre beaucoup trop long pour une note Instagram", "Un artiste au nom très long"), "titre", null, false, 60)
        assertTrue(longueurNote(long) <= 60)
        assertTrue(long.endsWith("…"))
    }

    @Test
    fun tronquerParPointsDeCode() {
        assertEquals("🎵🎵…", tronquerNote("🎵🎵🎵🎵", 3))
        assertEquals("abc", tronquerNote("abc", 3))
    }

    @Test
    fun ecouteSpotifyReconnaitLesBalados() {
        assertEquals(Ecoute("Get Lucky", "Daft Punk"), ecouteSpotify("Get Lucky", "Daft Punk", "Random Access Memories", "spotify:track:69kOkLUCkxIZYexIgSG8rq"))
        val b = ecouteSpotify("#2000", "Joe Rogan", "The Joe Rogan Experience", "spotify:episode:abc")!!
        assertTrue(b.balado)
        assertEquals("The Joe Rogan Experience", b.emission)
        assertNull(ecouteSpotify("", null, null, null))
    }

    @Test
    fun genreDeMusicBrainz() {
        // Forme d'une réponse de /ws/2/artist/?query=…&fmt=json (abrégée).
        val json = """{"created":"2026-11-26T00:00:00Z","count":1,"offset":0,"artists":[{"id":"056e4f3e-d505-4dad-8ec1-d04f521cbb56",
            "type":"Group","score":100,"name":"Daft Punk","sort-name":"Daft Punk","country":"FR",
            "tags":[{"count":3,"name":"house"},{"count":12,"name":"electronic"},{"count":1,"name":"french"}]}]}"""
        assertEquals("electronic", genreMusicBrainz(json))
        assertNull(genreMusicBrainz("""{"artists":[{"name":"X"}]}"""))
        assertNull(genreMusicBrainz("""{"artists":[]}"""))
        assertNull(genreMusicBrainz("pas du json"))
    }

    @Test
    fun selectionDesServices() {
        val compat = listOf("messenger", "instagram", "telegram")
        assertEquals(listOf("messenger", "telegram"), basculerNotesService(listOf("tout"), "instagram", compat))
        assertEquals(listOf("messenger", "telegram", "instagram"), basculerNotesService(listOf("messenger", "telegram"), "instagram", compat))
        // Le dernier ne se décoche pas.
        assertEquals(listOf("telegram"), basculerNotesService(listOf("telegram"), "telegram", compat))
        assertEquals(listOf("tout"), servicesNotes(listOf()))
        assertEquals(listOf("tout"), servicesNotes(listOf("instagram", "tout")))
        assertEquals(listOf("instagram"), servicesNotes(listOf("instagram", "instagram", "")))
    }
}
