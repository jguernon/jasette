package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** version.json (paquets/empaqueter.sh) et la comparaison des versions. */
class VersionDispoTest {
    private val sha = "e1bf67ebe922b0a1a8b0e79b1b6a27d8fe2f90af8ce323c049de195f851bec48"

    private fun json(
        code: String = "10112",
        url: String = "https://jonathanguernon.net/jasette/telecharger/jasette-1.0.0.apk?v=10112",
        sha256: String = sha,
        reste: String = """, "versionName": "1.0.0 (112)", "taille": 56623104, "notes": {"fr": "Mise à jour", "en": "Update"}""",
    ) = """{"versionCode": $code, "url": "$url", "sha256": "$sha256"$reste}"""

    @Test
    fun litUnFichierComplet() {
        val v = VersionDispo.lire(json())
        assertNotNull(v)
        v!!
        assertEquals(10112L, v.versionCode)
        assertEquals("1.0.0 (112)", v.versionName)
        assertEquals(sha, v.sha256)
        assertEquals(56623104L, v.taille)
        assertEquals("Mise à jour", v.notes(anglais = false))
        assertEquals("Update", v.notes(anglais = true))
    }

    @Test
    fun champsFacultatifs() {
        val v = VersionDispo.lire(json(reste = ""))!!
        assertEquals("10112", v.versionName)
        assertEquals(0L, v.taille)
        assertEquals("", v.notes(anglais = false))
    }

    @Test
    fun notesDansLAutreLangueSiAbsentes() {
        val v = VersionDispo.lire(json(reste = """, "notes": {"fr": "Seulement en français"}"""))!!
        assertEquals("Seulement en français", v.notes(anglais = true))
    }

    @Test
    fun shaEnMajusculesAccepte() {
        assertEquals(sha, VersionDispo.lire(json(sha256 = sha.uppercase()))!!.sha256)
    }

    @Test
    fun refuseLesFichiersDouteux() {
        assertNull(VersionDispo.lire("pas du json"))
        assertNull(VersionDispo.lire("{}"))
        assertNull(VersionDispo.lire(json(code = "0")))
        assertNull(VersionDispo.lire(json(code = "-3")))
        assertNull(VersionDispo.lire(json(sha256 = "abc")))
        assertNull(VersionDispo.lire(json(sha256 = "z".repeat(64))))
        // Pas de HTTP en clair, pas d'autre hôte.
        assertNull(VersionDispo.lire(json(url = "http://jonathanguernon.net/jasette/telecharger/jasette.apk")))
        assertNull(VersionDispo.lire(json(url = "https://exemple.com/jasette.apk")))
        assertNull(VersionDispo.lire(json(url = "https://jonathanguernon.net.exemple.com/jasette.apk")))
        assertNull(VersionDispo.lire(json(url = "https://jonathanguernon.net:8443/jasette.apk")))
        assertNull(VersionDispo.lire(json(url = "https://moi@jonathanguernon.net/jasette.apk")))
    }

    @Test
    fun adresseSure() {
        assertTrue(VersionDispo.adresseSure(VersionDispo.ADRESSE_VERSION))
        assertTrue(VersionDispo.adresseSure("HTTPS://JonathanGuernon.net/jasette/telecharger/x.apk?v=2"))
        assertFalse(VersionDispo.adresseSure("ftp://jonathanguernon.net/x.apk"))
        assertFalse(VersionDispo.adresseSure("https://jonathanguernon.net"))
        assertFalse(VersionDispo.adresseSure(""))
    }

    @Test
    fun comparaisonDesVersions() {
        val v = VersionDispo.lire(json(code = "10112"))
        assertTrue(VersionDispo.plusRecente(v, 1000L))
        assertTrue(VersionDispo.plusRecente(v, 10111L))
        assertFalse(VersionDispo.plusRecente(v, 10112L))
        assertFalse(VersionDispo.plusRecente(v, 10200L))
        assertFalse(VersionDispo.plusRecente(null, 0L))
        // Grands versionCode (Long) : pas de débordement.
        val grand = VersionDispo.lire(json(code = "3000000000"))
        assertTrue(VersionDispo.plusRecente(grand, Int.MAX_VALUE.toLong()))
    }
}
