package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/** Un aperçu du service sans image (reel Facebook dans Messenger) prend celle que Jasette a cherchée. */
class ApercuSansImageTest {
    private val service = LienApercu(
        url = "https://www.facebook.com/share/r/1ALWXsFjZY/", title = "Reel de Camille", description = "", site = "facebook.com",
    )

    @Test
    fun prendLImageEtGardeLeTitre() {
        assertTrue(service.sansImage)
        assertSame(service, service.completePar(null))
        val cherche = LienApercu(
            url = service.url, title = "Autre titre", description = "Une description", site = "", image = "/cache/apercus/abc.jpg",
        )
        val a = service.completePar(cherche)
        assertEquals("Reel de Camille", a.title)
        assertEquals("Une description", a.description)
        assertEquals("facebook.com", a.site)
        assertEquals("/cache/apercus/abc.jpg", a.image)
    }

    @Test
    fun uneImageDuServiceSuffit() {
        assertFalse(service.copy(imageUrl = "https://exemple.ca/i.jpg").sansImage)
        assertFalse(service.copy(image = "/cache/i.jpg").sansImage)
    }
}
