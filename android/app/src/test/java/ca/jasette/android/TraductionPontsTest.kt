package ca.jasette.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Les mêmes essais que src/ponts_langue.rs (bureau) : les deux traductions doivent s'accorder. */
class TraductionPontsTest {
    private fun en(s: String) = traduirePontEn(s)
    private fun service(s: String) = traduirePontEn(s, service = true)

    @Test
    fun exacts() {
        assertEquals("🚫 Message removed", en("🚫 Message retiré"))
        assertEquals("Message removed", en("Message retiré"))
        assertEquals("🚫 Message deleted", en("🚫 Message supprimé"))
        assertEquals("🔒 Encrypted conversation", en("🔒 Conversation chiffrée"))
        assertEquals("📎 Attachment", en("📎 Pièce jointe"))
        assertEquals("🎬 Video", en("🎬 Vidéo"))
        assertEquals("Salut !", en("Salut !"))
        assertEquals("Message retiré hier", en("Message retiré hier"))
    }

    @Test
    fun debuts() {
        assertEquals("↗️ Forwarded message", en("↗️ Message transféré"))
        assertEquals("↗️ Forwarded message\nallo", en("↗️ Message transféré\nallo"))
        assertEquals("↗️ Forwarded message allo", en("↗️ Message transféré allo"))
        assertEquals("↗️ Message transférés", en("↗️ Message transférés"))
        assertEquals("↗ Forwarded from Bob\nsalut", en("↗ Transféré de Bob\nsalut"))
        assertEquals(
            "📊 Poll: Pizza ?\n• Oui\n• Non\n(vote in Signal)",
            en("📊 Sondage : Pizza ?\n• Oui\n• Non\n(vote dans Signal)"),
        )
        assertEquals("⏱ Disappearing messages: 1h0m0s", en("⏱ Messages éphémères : 1h0m0s"))
        assertEquals("Message request · Message removed", en("Demande de message · Message retiré"))
        assertEquals("Message request", en("Demande de message"))
    }

    @Test
    fun apercuSurUneLigne() {
        assertEquals("📊 Pizza ? • Oui • Non (vote on the phone)", en("📊 Pizza ? • Oui • Non (vote sur le téléphone)"))
    }

    @Test
    fun citations() {
        assertEquals("↪ Message deleted\nOK", en("↪ Message supprimé\nOK"))
        assertEquals("↪ You: 📷 Photo\nbelle", en("↪ Vous : 📷 Photo\nbelle"))
        assertEquals("↪ You: on se voit ?\noui", en("↪ Vous : on se voit ?\noui"))
        assertEquals("↪ Alice: 🎤 Voice message\nreçu", en("↪ Alice : 🎤 Message vocal\nreçu"))
        assertEquals("↪ Alice : salut\nyo", en("↪ Alice : salut\nyo"))
    }

    @Test
    fun formes() {
        assertEquals("Alice left the group", en("Alice a quitté le groupe"))
        assertEquals("Someone pinned a message", en("Quelqu'un a épinglé un message"))
        assertEquals("📞 Bob started a call", en("📞 Bob a lancé un appel"))
        assertEquals("Alice created the group “Souper”", en("Alice a créé le groupe « Souper »"))
        assertEquals("Channel “Nouvelles” created", en("Canal « Nouvelles » créé"))
        assertEquals("(unsupported message: open WhatsApp)", en("(message non pris en charge : ouvre WhatsApp)"))
        assertEquals("Signal conversation", en("Conversation Signal"))
        assertEquals("Conversation intéressante", en("Conversation intéressante"))
        assertEquals("Alice removed the group photo", en("Alice a retiré la photo du groupe"))
        assertEquals(" a quitté le groupe", en(" a quitté le groupe"))
    }

    @Test
    fun formesDeService() {
        assertEquals("Alice added Bob, someone", service("Alice a ajouté Bob, quelqu'un"))
        assertEquals("Someone removed Bob", service("Quelqu'un a retiré Bob"))
        assertEquals("Alice renamed the group: Chalet", service("Alice a renommé le groupe : Chalet"))
        assertEquals("Alice removed the group photo", service("Alice a retiré la photo du groupe"))
        assertEquals("Maman a ajouté du sel", en("Maman a ajouté du sel"))
    }

    @Test
    fun systeme() {
        assertEquals("🚫 Message retiré", texteSysteme("deleted"))
        assertNull(texteSysteme("inconnu"))
        assertNull(texteSysteme(""))
        for ((_, fr) in PONT_SYSTEME) assertNotEquals("« $fr » sans traduction", fr, en(fr))
    }

    @Test
    fun msgDeSysteme() {
        val o = org.json.JSONObject()
            .put("id", "1").put("conv", "d:1").put("text", "🚫 Message supprimé").put("system", "deleted")
        assertEquals("🚫 Message retiré", Msg.de(o).text)
        o.put("system", "plus_tard")
        assertEquals("🚫 Message supprimé", Msg.de(o).text)
    }
}
