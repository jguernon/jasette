package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Test

/** Les mêmes essais que `ordre_des_services` de src/main.rs (bureau). */
class OrdreServicesTest {
    private fun cles(l: List<Service>) = l.map { it.cle }

    @Test
    fun ordreParDefaut() {
        assertEquals(Service.entries.toList(), Service.ordonnes(emptyList()))
        // Parmi les services présents, sans ordre choisi : leur ordre à eux.
        assertEquals(Service.LOCAUX, Service.ordonnes(emptyList(), Service.LOCAUX))
    }

    @Test
    fun ordreChoisiNomInconnuEtRepete() {
        val l = Service.ordonnes(listOf("signal", "inconnu", "google", "signal", "x"))
        assertEquals(11, l.size)
        assertEquals(listOf("signal", "google", "x", "messenger"), cles(l.take(4)))
        assertEquals(Service.LINKEDIN, l.last())
    }

    @Test
    fun nouveauServiceALaFin() {
        // Un ordre complet enregistré avant LinkedIn : LinkedIn va à la fin.
        val sans = Service.entries.reversed().filter { it != Service.LINKEDIN }.map { it.cle }
        val l = Service.ordonnes(sans)
        assertEquals(Service.REDDIT, l.first())
        assertEquals(Service.LINKEDIN, l.last())
    }

    @Test
    fun seulementParmiLesPresents() {
        val l = Service.ordonnes(listOf("whatsapp", "instagram", "google"), Service.LOCAUX)
        assertEquals(listOf(Service.INSTAGRAM, Service.GOOGLE, Service.MESSENGER), l)
    }
}
